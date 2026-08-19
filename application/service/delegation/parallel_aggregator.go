package delegation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

type aggregateClaimBucket struct {
	statement    string
	alternatives map[string]*dso.ClaimAlternative
}

func AggregateParallelResults(plan dso.ParallelSpecialistPlan, branches map[string]ParallelBranchResult, at time.Time, runErr error) dso.ParallelAggregateResult {
	result := dso.ParallelAggregateResult{
		AggregateResultID: "aggregate-" + stableActionSuffix(plan.ParallelPlanID), ParallelPlanRef: plan.ParallelPlanID,
		MinimumEvidencePerClaim: plan.MinimumEvidencePerClaim, CreatedAt: at.UTC(),
	}
	buckets := make(map[string]*aggregateClaimBucket)
	evidenceByKey := make(map[string]dso.AggregateEvidence)
	evidenceAliases := make(map[string]string)
	totalEvidenceRefs := 0
	failed := 0
	for _, nodeID := range sortedBranchResultIDs(branches) {
		branch := branches[nodeID]
		if branch.ResultID != "" {
			result.SourceResultRefs = append(result.SourceResultRefs, branch.ResultID)
		}
		result.Usage = result.Usage.Add(branch.Usage)
		result.CoordinationTokens += branch.CoordinationTokens
		switch branch.Status {
		case dso.ParallelNodeFailed, dso.ParallelNodeBudgetRejected, dso.ParallelNodeCancelled, dso.ParallelNodeWaitingUser:
			failed++
		}
		if !parallelBranchContributes(branch.Status) {
			continue
		}
		for _, evidence := range branch.Evidence {
			totalEvidenceRefs++
			key := canonicalEvidenceKey(evidence)
			if key == "" {
				continue
			}
			if strings.TrimSpace(evidence.EvidenceRef) == "" {
				evidence.EvidenceRef = "evidence-" + stableActionSuffix(key)
			}
			canonicalRef := evidence.EvidenceRef
			if existing, exists := evidenceByKey[key]; exists {
				canonicalRef = existing.EvidenceRef
			} else {
				evidenceByKey[key] = evidence
			}
			registerEvidenceAliases(evidenceAliases, evidence, key, canonicalRef)
		}
	}
	for _, nodeID := range sortedBranchResultIDs(branches) {
		branch := branches[nodeID]
		if !parallelBranchContributes(branch.Status) {
			continue
		}
		for _, claim := range branch.Claims {
			key := normalizeClaimKey(claim.Key, claim.Statement)
			if key == "" {
				continue
			}
			bucket := buckets[key]
			if bucket == nil {
				bucket = &aggregateClaimBucket{statement: strings.TrimSpace(claim.Statement), alternatives: make(map[string]*dso.ClaimAlternative)}
				buckets[key] = bucket
			}
			value := strings.TrimSpace(claim.Value)
			if value == "" {
				value = strings.TrimSpace(claim.Statement)
			}
			valueKey := normalizeClaimValue(value)
			alternative := bucket.alternatives[valueKey]
			if alternative == nil {
				alternative = &dso.ClaimAlternative{Value: value}
				bucket.alternatives[valueKey] = alternative
			}
			alternative.EvidenceRefs = uniqueSorted(append(alternative.EvidenceRefs, correlateEvidenceRefs(claim.EvidenceRefs, evidenceAliases)...))
			if branch.ResultID != "" {
				alternative.ResultRefs = uniqueSorted(append(alternative.ResultRefs, branch.ResultID))
			}
		}
	}
	for _, key := range sortedAggregateClaimKeys(buckets) {
		bucket := buckets[key]
		claim := dso.AggregatedClaim{ClaimKey: key, Statement: bucket.statement}
		for _, valueKey := range sortedClaimAlternativeKeys(bucket.alternatives) {
			alternative := *bucket.alternatives[valueKey]
			claim.Alternatives = append(claim.Alternatives, alternative)
			claim.EvidenceRefs = append(claim.EvidenceRefs, alternative.EvidenceRefs...)
		}
		claim.EvidenceRefs = uniqueSorted(claim.EvidenceRefs)
		switch {
		case len(claim.Alternatives) > 1:
			claim.Status = dso.AggregateClaimConflicting
			claim.Confidence = .5
		case len(claim.EvidenceRefs) >= plan.MinimumEvidencePerClaim:
			claim.Status = dso.AggregateClaimSupported
			claim.Confidence = minFloat(1, .6+.1*float64(len(claim.EvidenceRefs)))
		default:
			claim.Status = dso.AggregateClaimInsufficient
			claim.Confidence = .3
		}
		result.Claims = append(result.Claims, claim)
	}
	for _, key := range sortedEvidenceKeys(evidenceByKey) {
		result.Evidence = append(result.Evidence, evidenceByKey[key])
	}
	if totalEvidenceRefs > 0 {
		result.DuplicateFetchRatio = float64(totalEvidenceRefs-len(evidenceByKey)) / float64(totalEvidenceRefs)
	}
	totalTokens := result.Usage.Tokens
	if totalTokens > 0 {
		result.CoordinationTokenRatio = float64(result.CoordinationTokens) / float64(totalTokens)
	}
	switch {
	case runErr != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)):
		result.Status = dso.ParallelPlanCancelled
	case len(result.Claims) == 0:
		result.Status = dso.ParallelPlanFailed
	case failed > 0 || hasNonSupportedClaim(result.Claims):
		result.Status = dso.ParallelPlanPartial
	default:
		result.Status = dso.ParallelPlanCompleted
	}
	return result
}

func parallelBranchContributes(status string) bool {
	return status == dso.ParallelNodeCompleted || status == dso.ParallelNodePartial
}

func registerEvidenceAliases(aliases map[string]string, evidence dso.AggregateEvidence, canonicalKey, canonicalRef string) {
	for _, alias := range []string{evidence.EvidenceRef, evidence.URL, evidence.ContentHash, canonicalKey} {
		alias = strings.TrimSpace(alias)
		if alias != "" {
			aliases[alias] = canonicalRef
		}
	}
}

func correlateEvidenceRefs(refs []string, aliases map[string]string) []string {
	correlated := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if canonicalRef, ok := aliases[ref]; ok {
			correlated = append(correlated, canonicalRef)
			continue
		}
		if parsed, err := url.Parse(ref); err == nil && parsed.Host != "" {
			key := canonicalEvidenceKey(dso.AggregateEvidence{URL: ref})
			if canonicalRef, ok := aliases[key]; ok {
				correlated = append(correlated, canonicalRef)
			}
		}
	}
	return correlated
}

func canonicalEvidenceKey(evidence dso.AggregateEvidence) string {
	if raw := strings.TrimSpace(evidence.URL); raw != "" {
		parsed, err := url.Parse(raw)
		if err == nil && parsed.Host != "" {
			parsed.Fragment = ""
			query := parsed.Query()
			for key := range query {
				lower := strings.ToLower(key)
				if strings.HasPrefix(lower, "utm_") || lower == "gclid" || lower == "fbclid" {
					query.Del(key)
				}
			}
			parsed.RawQuery = query.Encode()
			parsed.Scheme = strings.ToLower(parsed.Scheme)
			parsed.Host = strings.ToLower(parsed.Host)
			if parsed.Path != "/" {
				parsed.Path = strings.TrimSuffix(parsed.Path, "/")
			}
			return parsed.String()
		}
	}
	if value := strings.TrimSpace(evidence.ContentHash); value != "" {
		return "hash:" + strings.ToLower(value)
	}
	return strings.TrimSpace(evidence.EvidenceRef)
}

func normalizeClaimKey(key, statement string) string {
	value := strings.ToLower(strings.TrimSpace(key))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(statement))
	}
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 128 {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	return "claim:" + hex.EncodeToString(digest[:8])
}

func normalizeClaimValue(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func sortedBranchResultIDs(values map[string]ParallelBranchResult) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAggregateClaimKeys(values map[string]*aggregateClaimBucket) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedClaimAlternativeKeys(values map[string]*dso.ClaimAlternative) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedEvidenceKeys(values map[string]dso.AggregateEvidence) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func hasNonSupportedClaim(claims []dso.AggregatedClaim) bool {
	for _, claim := range claims {
		if claim.Status != dso.AggregateClaimSupported {
			return true
		}
	}
	return false
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
