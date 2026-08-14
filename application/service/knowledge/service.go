package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/knowledge"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
	log "github.com/good-fish-man/logx"
)

const (
	defaultResultLimit = 20
	defaultTokenBudget = 12000
	defaultTimeBudget  = 3000
)

type Service struct{ store repository.Store }

func NewService(store repository.Store) *Service { return &Service{store: store} }

type CreateClaimRequest struct {
	Claim    entity.Claim      `json:"claim"`
	Evidence []entity.Evidence `json:"evidence"`
}

type RetrievalResponse struct {
	Hits           []entity.RetrievalHit  `json:"hits"`
	Contradictions []entity.Contradiction `json:"contradictions"`
	Snapshot       *entity.Snapshot       `json:"snapshot,omitempty"`
	Budget         RetrievalBudgetUsage   `json:"budget"`
}

type RetrievalBudgetUsage struct {
	Results int   `json:"results"`
	Tokens  int   `json:"tokens"`
	TimeMS  int64 `json:"time_ms"`
}

type CreateOntologyPackRequest struct {
	Name    string            `json:"name"`
	Domain  string            `json:"domain"`
	Display map[string]string `json:"display,omitempty"`
}

type CreateOntologyCandidateRequest struct {
	PackID          string                       `json:"pack_id"`
	BaseVersion     string                       `json:"base_version"`
	Version         string                       `json:"version"`
	CompatibleWith  []string                     `json:"compatible_with,omitempty"`
	ValidationRules []knowledgev1.ValidationRule `json:"validation_rules"`
	DisplayMetadata map[string]string            `json:"display_metadata,omitempty"`
	Definition      map[string]any               `json:"definition"`
	EvidenceRefs    []string                     `json:"evidence_refs"`
	EvaluationID    string                       `json:"evaluation_id"`
}

type ReviewOntologyCandidateRequest struct {
	Approved         bool   `json:"approved"`
	ReviewNote       string `json:"review_note"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type CreateOntologyMigrationRequest struct {
	CandidateID   string   `json:"candidate_id"`
	FromVersion   string   `json:"from_version"`
	ToVersion     string   `json:"to_version"`
	Plan          []string `json:"plan"`
	ToolExecution bool     `json:"tool_execution"`
}

type Belief struct {
	ClaimID     string  `json:"claim_id"`
	Statement   string  `json:"statement"`
	Confidence  float64 `json:"confidence"`
	Expired     bool    `json:"expired"`
	HasConflict bool    `json:"has_conflict"`
}

type PredictionEvaluation struct {
	Matched            bool     `json:"matched"`
	ChangedFields      []string `json:"changed_fields"`
	ExperienceEligible bool     `json:"experience_eligible"`
	PolicyMutation     bool     `json:"policy_mutation"`
}

func (s *Service) CreateClaim(ctx context.Context, ownerID string, request CreateClaimRequest) (*entity.Claim, []entity.Contradiction, error) {
	if s == nil || s.store == nil {
		return nil, nil, fmt.Errorf("knowledge service is not configured")
	}
	if len(request.Evidence) == 0 {
		return nil, nil, apierror.ErrBadRequest.WithMessage("a claim requires evidence")
	}
	now := time.Now().UTC()
	evidenceByID := make(map[string]entity.Evidence, len(request.Evidence))
	onlyPageObservation := true
	for index := range request.Evidence {
		item := &request.Evidence[index]
		item.Schema, item.OwnerID = entity.Schema, ownerID
		if item.EvidenceID == "" {
			item.EvidenceID = ulid.New()
		}
		if item.ObservedAt.IsZero() {
			item.ObservedAt = now
		}
		item.Provenance = normalizeProvenance(item.Provenance, now, item.URI+"\n"+item.Excerpt)
		if item.SourceType != knowledgev1.SourcePageObservation {
			onlyPageObservation = false
		}
		if err := item.Validate(); err != nil {
			return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
		}
		evidenceByID[item.EvidenceID] = *item
	}
	if len(request.Evidence) == 1 && onlyPageObservation {
		return nil, nil, apierror.ErrBadRequest.WithMessage("a single page observation cannot become knowledge without evidence processing or user confirmation")
	}

	claim := request.Claim
	claim.Schema, claim.OwnerID = entity.Schema, ownerID
	if claim.ClaimID == "" {
		claim.ClaimID = ulid.New()
	}
	if claim.Status == "" {
		claim.Status = knowledgev1.ClaimActive
	}
	if claim.CreatedAt.IsZero() {
		claim.CreatedAt = now
	}
	claim.UpdatedAt = now
	claim.Provenance = normalizeProvenance(claim.Provenance, now, claim.Subject+"\n"+claim.Predicate+"\n"+claim.Value)
	if len(claim.EvidenceRefs) == 0 {
		for _, item := range request.Evidence {
			claim.EvidenceRefs = append(claim.EvidenceRefs, item.EvidenceID)
		}
	}

	existing, err := s.store.ListClaims(ctx, ownerID, entity.ClaimFilter{Subject: claim.Subject, Predicate: claim.Predicate, Statuses: []string{knowledgev1.ClaimActive, knowledgev1.ClaimContradicted}, Limit: 200})
	if err != nil {
		return nil, nil, log.WrapError(err, "KnowledgeService.CreateClaim.findConflicts")
	}
	conflictingIDs := make([]string, 0)
	contradictions := make([]entity.Contradiction, 0)
	for _, current := range existing {
		if normalizeText(current.Value) == normalizeText(claim.Value) {
			continue
		}
		conflictingIDs = append(conflictingIDs, current.ClaimID)
		claim.ContradictedBy = appendUnique(claim.ContradictedBy, current.ClaimID)
		claim.Status = knowledgev1.ClaimContradicted
		contradictions = append(contradictions, entity.Contradiction{
			Schema: entity.Schema, ContradictionID: ulid.New(), OwnerID: ownerID,
			ClaimIDs: []string{current.ClaimID, claim.ClaimID}, EvidenceRefs: union(current.EvidenceRefs, claim.EvidenceRefs),
			Severity: contradictionSeverity(current.Confidence, claim.Confidence), Summary: "Conflicting values for " + claim.Subject + "." + claim.Predicate,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := claim.Validate(evidenceByID); err != nil {
		return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateKnowledge(ctx, request.Evidence, claim, contradictions, conflictingIDs); err != nil {
		return nil, nil, log.WrapError(err, "KnowledgeService.CreateClaim.persist")
	}
	return &claim, contradictions, nil
}

func (s *Service) Retrieve(ctx context.Context, ownerID string, query entity.RetrievalQuery) (*RetrievalResponse, error) {
	started := time.Now()
	query.OwnerID = ownerID
	query = retrievalDefaults(query)
	if err := query.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	deadline := started.Add(time.Duration(query.Budget.MaxTimeMS) * time.Millisecond)
	claims, err := s.store.ListClaims(ctx, ownerID, entity.ClaimFilter{Scopes: query.Scopes, Sensitivities: allowedSensitivities(query.MaxSensitivity), Statuses: []string{knowledgev1.ClaimActive, knowledgev1.ClaimContradicted, knowledgev1.ClaimExpired}, Limit: 500})
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.Retrieve.claims")
	}
	type scored struct {
		claim   entity.Claim
		score   float64
		matched []string
		expired bool
	}
	scoredClaims := make([]scored, 0, len(claims))
	for _, claim := range claims {
		if time.Now().After(deadline) {
			break
		}
		expired := claim.IsExpired(query.AsOf)
		if expired && !query.IncludeExpired {
			continue
		}
		score, matched := scoreClaim(query.Text, claim, expired)
		if score > 0.05 {
			scoredClaims = append(scoredClaims, scored{claim: claim, score: score, matched: matched, expired: expired})
		}
	}
	sort.SliceStable(scoredClaims, func(i, j int) bool { return scoredClaims[i].score > scoredClaims[j].score })

	hits := make([]entity.RetrievalHit, 0, min(query.Budget.MaxResults, len(scoredClaims)))
	tokenUsage := 0
	for _, candidate := range scoredClaims {
		if len(hits) >= query.Budget.MaxResults || time.Now().After(deadline) {
			break
		}
		evidence, findErr := s.store.FindEvidence(ctx, ownerID, candidate.claim.EvidenceRefs)
		if findErr != nil {
			return nil, log.WrapError(findErr, "KnowledgeService.Retrieve.evidence")
		}
		accessible := make([]entity.Evidence, 0, len(evidence))
		evidenceRank := 0.0
		for _, item := range evidence {
			if item.Accessible {
				accessible = append(accessible, item)
				evidenceRank += (item.Authority + item.Freshness) / 2
			}
		}
		if len(accessible) == 0 {
			continue
		}
		evidenceRank /= float64(len(accessible))
		estimated := estimateTokens(candidate.claim.Value)
		for _, item := range accessible {
			estimated += estimateTokens(item.Excerpt)
		}
		if tokenUsage+estimated > query.Budget.MaxTokens {
			break
		}
		tokenUsage += estimated
		hits = append(hits, entity.RetrievalHit{Claim: candidate.claim, Evidence: accessible, Score: clamp(.8*candidate.score + .2*evidenceRank), Expired: candidate.expired, HasConflict: len(candidate.claim.ContradictedBy) > 0 || candidate.claim.Status == knowledgev1.ClaimContradicted, MatchedBy: candidate.matched})
	}
	contradictions, err := s.store.ListContradictions(ctx, ownerID, true, 100)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.Retrieve.contradictions")
	}
	snapshot, err := s.createSnapshot(ctx, ownerID, hits)
	if err != nil {
		return nil, err
	}
	return &RetrievalResponse{Hits: hits, Contradictions: relevantContradictions(contradictions, hits), Snapshot: snapshot, Budget: RetrievalBudgetUsage{Results: len(hits), Tokens: tokenUsage, TimeMS: time.Since(started).Milliseconds()}}, nil
}

func (s *Service) ListClaims(ctx context.Context, ownerID string, limit int) ([]entity.Claim, error) {
	return s.store.ListClaims(ctx, ownerID, entity.ClaimFilter{Limit: limit})
}

func (s *Service) ListEvidence(ctx context.Context, ownerID string, limit int) ([]entity.Evidence, error) {
	return s.store.ListEvidence(ctx, ownerID, entity.EvidenceFilter{Limit: limit})
}

// CaptureResearchEvidence persists attributable sources but intentionally does
// not create claims. Evidence processing or an explicit user confirmation is
// still required before a source can become knowledge.
func (s *Service) CaptureResearchEvidence(ctx context.Context, ownerID, taskID, traceID string, snapshot map[string]any) error {
	if s == nil || s.store == nil || ownerID == "" || len(snapshot) == 0 {
		return nil
	}
	pages, ok := snapshot["pages"].([]any)
	if !ok || len(pages) == 0 {
		return nil
	}
	now := time.Now().UTC()
	items := make([]entity.Evidence, 0, len(pages))
	for _, raw := range pages {
		page, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		uri := firstString(page, "url", "canonical_url")
		title := firstString(page, "title")
		excerpt := firstString(page, "snippet", "content")
		if uri == "" || title == "" || excerpt == "" {
			continue
		}
		sum := sha256.Sum256([]byte(ownerID + "\n" + uri + "\n" + excerpt))
		item := entity.Evidence{
			Schema: entity.Schema, EvidenceID: "evidence-" + hex.EncodeToString(sum[:])[:32], OwnerID: ownerID,
			Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, SourceType: researchSourceType(uri),
			Title: title, URI: uri, Accessible: true, Excerpt: excerpt,
			Authority: boundedFloat(page["authority"], .5), Freshness: boundedFloat(page["freshness"], .5), ObservedAt: now,
			Provenance: normalizeProvenance(entity.Provenance{Producer: "agent-runtime-research", Method: "ranked-research-source", TraceID: traceID, SourceTaskID: taskID}, now, uri+"\n"+excerpt),
		}
		if err := item.Validate(); err == nil {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return log.WrapError(s.store.CreateEvidence(ctx, items), "KnowledgeService.CaptureResearchEvidence")
}

func (s *Service) ListContradictions(ctx context.Context, ownerID string, unresolvedOnly bool, limit int) ([]entity.Contradiction, error) {
	return s.store.ListContradictions(ctx, ownerID, unresolvedOnly, limit)
}

func (s *Service) ListSnapshots(ctx context.Context, ownerID string, limit int) ([]entity.Snapshot, error) {
	return s.store.ListSnapshots(ctx, ownerID, limit)
}

func (s *Service) EnsureCoreOntology(ctx context.Context, ownerID string) error {
	packs, err := s.store.ListOntologyPacks(ctx, ownerID)
	if err != nil {
		return err
	}
	for _, pack := range packs {
		if pack.Domain == "core" {
			return nil
		}
	}
	now := time.Now().UTC()
	pack := entity.OntologyPack{Schema: entity.Schema, PackID: ulid.New(), OwnerID: ownerID, Name: "Athena Core Ontology", Domain: "core", Current: "1.0.0", Display: map[string]string{"en": "Core", "zh-CN": "核心本体"}, CreatedAt: now}
	definition := map[string]any{"entities": []string{"Agent", "Task", "Device", "Claim", "Evidence"}, "relations": []string{"uses", "observes", "supports", "contradicts"}}
	checksum, _ := checksumJSON(definition)
	version := entity.OntologyVersion{Schema: entity.Schema, VersionID: ulid.New(), PackID: pack.PackID, OwnerID: ownerID, Version: "1.0.0", ValidationRules: []knowledgev1.ValidationRule{{SubjectType: "Claim", Predicate: "supports", ValueType: "Evidence", Required: true}}, Definition: definition, Checksum: checksum, Status: knowledgev1.OntologyApproved, ApprovedBy: "system-bootstrap", CreatedAt: now}
	if err := version.Validate(); err != nil {
		return err
	}
	return s.store.CreateOntologyPackWithVersion(ctx, pack, version)
}

func (s *Service) CreateOntologyPack(ctx context.Context, ownerID string, request CreateOntologyPackRequest) (*entity.OntologyPack, error) {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Domain) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("ontology name and domain are required")
	}
	pack := entity.OntologyPack{Schema: entity.Schema, PackID: ulid.New(), OwnerID: ownerID, Name: strings.TrimSpace(request.Name), Domain: strings.TrimSpace(request.Domain), Display: request.Display, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateOntologyPack(ctx, pack); err != nil {
		return nil, err
	}
	return &pack, nil
}

func (s *Service) ListOntologyPacks(ctx context.Context, ownerID string) ([]entity.OntologyPack, error) {
	if err := s.EnsureCoreOntology(ctx, ownerID); err != nil {
		return nil, err
	}
	return s.store.ListOntologyPacks(ctx, ownerID)
}

func (s *Service) CreateOntologyCandidate(ctx context.Context, ownerID string, request CreateOntologyCandidateRequest) (*entity.OntologyCandidate, error) {
	now := time.Now().UTC()
	checksum, err := checksumJSON(request.Definition)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	version := entity.OntologyVersion{Schema: entity.Schema, VersionID: ulid.New(), PackID: request.PackID, OwnerID: ownerID, Version: request.Version, CompatibleWith: request.CompatibleWith, ValidationRules: request.ValidationRules, DisplayMetadata: request.DisplayMetadata, Definition: request.Definition, Checksum: checksum, Status: knowledgev1.OntologyReviewRequired, CreatedAt: now}
	candidate := entity.OntologyCandidate{Schema: entity.Schema, CandidateID: ulid.New(), OwnerID: ownerID, PackID: request.PackID, BaseVersion: request.BaseVersion, Proposed: version, EvidenceRefs: unique(request.EvidenceRefs), EvaluationID: request.EvaluationID, Status: knowledgev1.OntologyReviewRequired, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := candidate.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateOntologyCandidate(ctx, candidate); err != nil {
		return nil, err
	}
	return &candidate, nil
}

func (s *Service) ReviewOntologyCandidate(ctx context.Context, ownerID, actorID, candidateID string, request ReviewOntologyCandidateRequest) (*entity.OntologyCandidate, error) {
	candidate, err := s.store.FindOntologyCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, apierror.ErrNotFound.WithMessage("ontology candidate not found")
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = candidate.Revision
	}
	candidate.Status = knowledgev1.OntologyRejected
	if request.Approved {
		candidate.Status = knowledgev1.OntologyApproved
		candidate.Proposed.Status = knowledgev1.OntologyApproved
		candidate.Proposed.ApprovedBy = actorID
	}
	candidate.ReviewNote, candidate.Revision, candidate.UpdatedAt = strings.TrimSpace(request.ReviewNote), request.ExpectedRevision+1, time.Now().UTC()
	if err := candidate.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	var version *entity.OntologyVersion
	if request.Approved {
		version = &candidate.Proposed
	}
	if err := s.store.ReviewOntologyCandidate(ctx, *candidate, request.ExpectedRevision, version); err != nil {
		return nil, err
	}
	return candidate, nil
}

func (s *Service) CreateOntologyMigration(ctx context.Context, ownerID, actorID string, request CreateOntologyMigrationRequest) (*entity.OntologyMigration, error) {
	candidate, err := s.store.FindOntologyCandidate(ctx, ownerID, request.CandidateID)
	if err != nil {
		return nil, err
	}
	if candidate == nil || candidate.Status != knowledgev1.OntologyApproved {
		return nil, apierror.ErrBadRequest.WithMessage("ontology migration requires an approved candidate")
	}
	if candidate.BaseVersion != request.FromVersion || candidate.Proposed.Version != request.ToVersion {
		return nil, apierror.ErrBadRequest.WithMessage("ontology migration versions must match the approved candidate")
	}
	migration := entity.OntologyMigration{Schema: entity.Schema, MigrationID: ulid.New(), OwnerID: ownerID, PackID: candidate.PackID, FromVersion: request.FromVersion, ToVersion: request.ToVersion, Plan: request.Plan, ApprovedBy: actorID, ToolExecution: request.ToolExecution, Status: knowledgev1.OntologyApplied, CreatedAt: time.Now().UTC()}
	if err := migration.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.ApplyOntologyMigration(ctx, migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func BeliefsFromHits(hits []entity.RetrievalHit) []Belief {
	result := make([]Belief, 0, len(hits))
	for _, hit := range hits {
		result = append(result, Belief{ClaimID: hit.Claim.ClaimID, Statement: hit.Claim.Subject + " " + hit.Claim.Predicate + " " + hit.Claim.Value, Confidence: hit.Claim.Confidence * hit.Score, Expired: hit.Expired, HasConflict: hit.HasConflict})
	}
	return result
}

func ComparePrediction(expected, actual map[string]any) PredictionEvaluation {
	keys := make(map[string]struct{}, len(expected)+len(actual))
	for key := range expected {
		keys[key] = struct{}{}
	}
	for key := range actual {
		keys[key] = struct{}{}
	}
	changed := make([]string, 0)
	for key := range keys {
		left, _ := json.Marshal(expected[key])
		right, _ := json.Marshal(actual[key])
		if string(left) != string(right) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return PredictionEvaluation{Matched: len(changed) == 0, ChangedFields: changed, ExperienceEligible: len(changed) > 0, PolicyMutation: false}
}

func (s *Service) createSnapshot(ctx context.Context, ownerID string, hits []entity.RetrievalHit) (*entity.Snapshot, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	claimIDs, evidenceIDs := make([]string, 0, len(hits)), make([]string, 0)
	for _, hit := range hits {
		claimIDs = append(claimIDs, hit.Claim.ClaimID)
		for _, evidence := range hit.Evidence {
			evidenceIDs = appendUnique(evidenceIDs, evidence.EvidenceID)
		}
	}
	checksum, err := checksumJSON(map[string]any{"claims": claimIDs, "evidence": evidenceIDs})
	if err != nil {
		return nil, err
	}
	snapshot := entity.Snapshot{Schema: entity.Schema, SnapshotID: ulid.New(), OwnerID: ownerID, ClaimIDs: claimIDs, EvidenceIDs: evidenceIDs, OntologyPack: "core", OntologyVer: "1.0.0", Checksum: checksum, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func retrievalDefaults(query entity.RetrievalQuery) entity.RetrievalQuery {
	if query.AsOf.IsZero() {
		query.AsOf = time.Now().UTC()
	}
	if len(query.Scopes) == 0 {
		query.Scopes = []string{knowledgev1.ScopeUser}
	}
	if query.MaxSensitivity == "" {
		query.MaxSensitivity = knowledgev1.SensitivityInternal
	}
	if query.Budget.MaxResults == 0 {
		query.Budget.MaxResults = defaultResultLimit
	}
	if query.Budget.MaxTokens == 0 {
		query.Budget.MaxTokens = defaultTokenBudget
	}
	if query.Budget.MaxTimeMS == 0 {
		query.Budget.MaxTimeMS = defaultTimeBudget
	}
	return query
}

func scoreClaim(query string, claim entity.Claim, expired bool) (float64, []string) {
	queryTokens, claimTokens := tokenVector(query), tokenVector(claim.Subject+" "+claim.Predicate+" "+claim.Value)
	keyword := overlap(queryTokens, claimTokens)
	vector := cosine(queryTokens, claimTokens)
	structured := 0.0
	matched := make([]string, 0, 4)
	normalized := normalizeText(query)
	if strings.Contains(normalized, normalizeText(claim.Subject)) {
		structured += .55
		matched = append(matched, "subject")
	}
	if strings.Contains(normalized, normalizeText(claim.Predicate)) {
		structured += .45
		matched = append(matched, "predicate")
	}
	if keyword > 0 {
		matched = append(matched, "keyword")
	}
	if vector > 0 {
		matched = append(matched, "vector")
	}
	temporal := 1.0
	if expired {
		temporal = 0
	}
	return clamp(.35*structured + .3*keyword + .25*vector + .1*temporal), unique(matched)
}

func tokenVector(value string) map[string]float64 {
	result := map[string]float64{}
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for _, field := range fields {
		if field != "" {
			result[field]++
		}
	}
	return result
}

func overlap(left, right map[string]float64) float64 {
	if len(left) == 0 {
		return 0
	}
	matched := 0
	for token := range left {
		if right[token] > 0 {
			matched++
		}
	}
	return float64(matched) / float64(len(left))
}

func cosine(left, right map[string]float64) float64 {
	dot, leftNorm, rightNorm := 0.0, 0.0, 0.0
	for token, value := range left {
		dot += value * right[token]
		leftNorm += value * value
	}
	for _, value := range right {
		rightNorm += value * value
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func allowedSensitivities(maximum string) []string {
	order := []string{knowledgev1.SensitivityPublic, knowledgev1.SensitivityInternal, knowledgev1.SensitivitySensitive, knowledgev1.SensitivityRestricted}
	return order[:knowledgev1.SensitivityRank(maximum)+1]
}

func relevantContradictions(items []entity.Contradiction, hits []entity.RetrievalHit) []entity.Contradiction {
	ids := map[string]struct{}{}
	for _, hit := range hits {
		ids[hit.Claim.ClaimID] = struct{}{}
	}
	result := make([]entity.Contradiction, 0)
	for _, item := range items {
		for _, id := range item.ClaimIDs {
			if _, ok := ids[id]; ok {
				result = append(result, item)
				break
			}
		}
	}
	return result
}

func normalizeProvenance(value entity.Provenance, now time.Time, body string) entity.Provenance {
	if value.Producer == "" {
		value.Producer = "athena-knowledge-service"
	}
	if value.Method == "" {
		value.Method = "evidence-gated-ingest"
	}
	if value.CapturedAt.IsZero() {
		value.CapturedAt = now
	}
	if value.ContentSHA256 == "" {
		sum := sha256.Sum256([]byte(body))
		value.ContentSHA256 = hex.EncodeToString(sum[:])
	}
	return value
}

func researchSourceType(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		if strings.HasSuffix(host, ".gov") || strings.Contains(host, ".go.jp") || strings.HasSuffix(host, ".europa.eu") {
			return knowledgev1.SourceOfficial
		}
	}
	return knowledgev1.SourceResearch
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boundedFloat(value any, fallback float64) float64 {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	default:
		return fallback
	}
	return clamp(number)
}

func checksumJSON(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func contradictionSeverity(left, right float64) string {
	if left >= .8 && right >= .8 {
		return "HIGH"
	}
	return "MEDIUM"
}

func estimateTokens(value string) int { return max(1, len([]rune(value))/4) }
func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func union(left, right []string) []string {
	return unique(append(append([]string{}, left...), right...))
}
func unique(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
