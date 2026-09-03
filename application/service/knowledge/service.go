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
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
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

type Service struct {
	store    repository.Store
	embedder Embedder
	migrator OntologyMigrator
}

func NewService(store repository.Store) *Service {
	return &Service{store: store, embedder: newLocalEmbedder(), migrator: declarativeOntologyMigrator{}}
}

type CreateClaimRequest struct {
	Claim        entity.Claim `json:"claim"`
	EvidenceRefs []string     `json:"evidence_refs"`
}

type CreateUserEvidenceRequest struct {
	Title       string            `json:"title"`
	Excerpt     string            `json:"excerpt"`
	Sensitivity string            `json:"sensitivity"`
	Metadata    map[string]string `json:"metadata,omitempty"`
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

type scoredClaim struct {
	claim        entity.Claim
	score        float64
	matched      []string
	expired      bool
	relationPath []string
}

type CreateOntologyPackRequest struct {
	Name    string            `json:"name"`
	Domain  string            `json:"domain"`
	Display map[string]string `json:"display,omitempty"`
}

type CreateOntologyCandidateRequest struct {
	PackID          string                         `json:"pack_id"`
	BaseVersion     string                         `json:"base_version"`
	Version         string                         `json:"version"`
	CompatibleWith  []string                       `json:"compatible_with,omitempty"`
	ValidationRules []knowledgev1.ValidationRule   `json:"validation_rules"`
	DisplayMetadata map[string]string              `json:"display_metadata,omitempty"`
	Definition      knowledgev1.OntologyDefinition `json:"definition"`
	EvidenceRefs    []string                       `json:"evidence_refs"`
}

type ReviewOntologyCandidateRequest struct {
	Approved         bool   `json:"approved"`
	ReviewNote       string `json:"review_note"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type CreateOntologyMigrationRequest struct {
	CandidateID string                              `json:"candidate_id"`
	Plan        []knowledgev1.OntologyMigrationStep `json:"plan"`
}

type ReviewOntologyMigrationRequest struct {
	Approved         bool   `json:"approved"`
	ReviewNote       string `json:"review_note"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type ResolveContradictionRequest struct {
	Decision       string `json:"decision"`
	WinningClaimID string `json:"winning_claim_id,omitempty"`
	Note           string `json:"note"`
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
	now := time.Now().UTC()
	claim := request.Claim
	claim.Schema, claim.OwnerID, claim.OrganizationID = entity.Schema, ownerID, ""
	if claim.Scope == "" {
		claim.Scope = knowledgev1.ScopeUser
	}
	if claim.Scope != knowledgev1.ScopeUser {
		return nil, nil, apierror.ErrForbidden.WithMessage("the user knowledge endpoint can only create user-scoped claims")
	}
	if claim.Sensitivity == "" {
		claim.Sensitivity = knowledgev1.SensitivityInternal
	}
	if claim.ClaimID == "" {
		claim.ClaimID = ulid.New()
	}
	claim.Status, claim.CreatedAt, claim.UpdatedAt = knowledgev1.ClaimActive, now, now
	claim.ContradictedBy = nil
	claim.Provenance = normalizeProvenance(entity.Provenance{Producer: "athena-knowledge-service", Method: "evidence-synthesis", TraceID: claim.Provenance.TraceID, SourceTaskID: claim.Provenance.SourceTaskID}, now, claim.Subject+"\n"+claim.Predicate+"\n"+claim.Value)
	claim.EvidenceRefs = unique(append(request.EvidenceRefs, claim.EvidenceRefs...))
	if len(claim.EvidenceRefs) == 0 {
		return nil, nil, apierror.ErrBadRequest.WithMessage("a claim requires persisted evidence references")
	}
	evidence, err := s.store.FindEvidence(ctx, ownerID, claim.EvidenceRefs)
	if err != nil {
		return nil, nil, log.WrapError(err, "KnowledgeService.CreateClaim.evidence")
	}
	if len(evidence) != len(claim.EvidenceRefs) {
		return nil, nil, apierror.ErrBadRequest.WithMessage("one or more evidence references are missing or outside the user boundary")
	}
	evidenceByID := make(map[string]entity.Evidence, len(evidence))
	onlyPageObservation := true
	allStale := true
	confidenceCeiling := 0.0
	var earliestStaleAt *time.Time
	for _, item := range evidence {
		evidenceByID[item.EvidenceID] = item
		if item.SourceType != knowledgev1.SourcePageObservation {
			onlyPageObservation = false
		}
		stale := evidenceIsStale(item, now)
		if !stale {
			allStale = false
		}
		confidenceCeiling += item.Authority * freshnessAt(item, now)
		if item.StaleAt != nil && (earliestStaleAt == nil || item.StaleAt.Before(*earliestStaleAt)) {
			value := *item.StaleAt
			earliestStaleAt = &value
		}
	}
	if len(evidence) == 1 && onlyPageObservation {
		return nil, nil, apierror.ErrBadRequest.WithMessage("a single page observation cannot become knowledge without evidence processing or user confirmation")
	}
	confidenceCeiling /= float64(len(evidence))
	if claim.Confidence <= 0 || claim.Confidence > confidenceCeiling {
		claim.Confidence = confidenceCeiling
	}
	if claim.TimeSensitive {
		if earliestStaleAt == nil && claim.ValidUntil == nil {
			return nil, nil, apierror.ErrBadRequest.WithMessage("time-sensitive knowledge requires a server-derived freshness horizon")
		}
		if earliestStaleAt != nil && (claim.ValidUntil == nil || earliestStaleAt.Before(*claim.ValidUntil)) {
			value := *earliestStaleAt
			claim.ValidUntil = &value
		}
	}
	if allStale || claim.IsExpired(now) {
		claim.Status = knowledgev1.ClaimExpired
	}
	vector, err := s.embedder.Embed(ctx, claim.Subject+" "+claim.Predicate+" "+claim.Value)
	if err != nil {
		return nil, nil, log.WrapError(err, "KnowledgeService.CreateClaim.embed")
	}
	claim.SemanticVector = vector
	if err := s.validateClaimRelations(ctx, ownerID, claim); err != nil {
		return nil, nil, err
	}

	existing, err := s.store.ListClaims(ctx, ownerID, entity.ClaimFilter{Subject: claim.Subject, Predicate: claim.Predicate, Statuses: []string{knowledgev1.ClaimActive, knowledgev1.ClaimContradicted}, Limit: 200})
	if err != nil {
		return nil, nil, log.WrapError(err, "KnowledgeService.CreateClaim.findConflicts")
	}
	conflictingIDs := make([]string, 0)
	contradictions := make([]entity.Contradiction, 0)
	for _, current := range existing {
		if normalizeText(current.Value) == normalizeText(claim.Value) || !claimPeriodsOverlap(current, claim) {
			continue
		}
		conflictingIDs = append(conflictingIDs, current.ClaimID)
		claim.ContradictedBy = appendUnique(claim.ContradictedBy, current.ClaimID)
		claim.Status = knowledgev1.ClaimContradicted
		contradictions = append(contradictions, entity.Contradiction{
			Schema: entity.Schema, ContradictionID: ulid.New(), OwnerID: ownerID,
			Subject: claim.Subject, Predicate: claim.Predicate,
			ClaimIDs: []string{current.ClaimID, claim.ClaimID}, EvidenceRefs: union(current.EvidenceRefs, claim.EvidenceRefs),
			Severity: contradictionSeverity(current.Confidence, claim.Confidence), Summary: "Conflicting values for " + claim.Subject + "." + claim.Predicate,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := claim.ValidateAt(evidenceByID, now); err != nil {
		return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	for _, contradiction := range contradictions {
		if err := contradiction.Validate(); err != nil {
			return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
		}
	}
	if err := s.store.CreateKnowledge(ctx, claim, contradictions, conflictingIDs); err != nil {
		return nil, nil, log.WrapError(err, "KnowledgeService.CreateClaim.persist")
	}
	return &claim, contradictions, nil
}

func (s *Service) CreateUserEvidence(ctx context.Context, ownerID string, request CreateUserEvidenceRequest) (*entity.Evidence, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("knowledge service is not configured")
	}
	title, excerpt := strings.TrimSpace(request.Title), strings.TrimSpace(request.Excerpt)
	if title == "" || excerpt == "" {
		return nil, apierror.ErrBadRequest.WithMessage("evidence title and excerpt are required")
	}
	sensitivity := request.Sensitivity
	if sensitivity == "" {
		sensitivity = knowledgev1.SensitivityInternal
	}
	if !knowledgev1.ValidSensitivity(sensitivity) {
		return nil, apierror.ErrBadRequest.WithMessage("evidence sensitivity is invalid")
	}
	now, evidenceID := time.Now().UTC(), ulid.New()
	value := entity.Evidence{
		Schema: entity.Schema, EvidenceID: evidenceID, OwnerID: ownerID,
		Scope: knowledgev1.ScopeUser, Sensitivity: sensitivity, SourceType: knowledgev1.SourceUserConfirmation,
		Title: title, URI: "athena://user/confirmation/" + evidenceID, Accessible: true, Excerpt: excerpt,
		Authority: 1, Freshness: 1, ObservedAt: now, TrustProfile: "user-confirmation.v1", AccessVerifiedAt: now,
		Provenance: normalizeProvenance(entity.Provenance{Producer: "authenticated-user", Method: "explicit-confirmation"}, now, excerpt), Metadata: request.Metadata,
	}
	if err := value.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateEvidence(ctx, []entity.Evidence{value}); err != nil {
		return nil, log.WrapError(err, "KnowledgeService.CreateUserEvidence.persist")
	}
	return &value, nil
}

func (s *Service) Retrieve(ctx context.Context, ownerID string, query entity.RetrievalQuery) (*RetrievalResponse, error) {
	organizationIDs := []string(nil)
	if organizationID := strings.TrimSpace(authctx.OrganizationID(ctx)); organizationID != "" {
		organizationIDs = []string{organizationID}
	}
	return s.retrieveWithOrganizations(ctx, ownerID, organizationIDs, query)
}

func (s *Service) retrieveWithOrganizations(ctx context.Context, ownerID string, organizationIDs []string, query entity.RetrievalQuery) (*RetrievalResponse, error) {
	started := time.Now()
	query.OwnerID = ownerID
	query.OrganizationIDs = unique(organizationIDs)
	query = retrievalDefaults(query)
	if err := query.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	deadline := started.Add(time.Duration(query.Budget.MaxTimeMS) * time.Millisecond)
	statuses := []string{knowledgev1.ClaimActive, knowledgev1.ClaimContradicted, knowledgev1.ClaimExpired}
	if query.IncludeExpired {
		statuses = append(statuses, knowledgev1.ClaimRetracted)
	}
	claims, err := s.store.SearchClaims(ctx, ownerID, query.OrganizationIDs, entity.ClaimFilter{Subject: query.Filter.Subject, Predicate: query.Filter.Predicate, Scopes: query.Scopes, Sensitivities: allowedSensitivities(query.MaxSensitivity), Statuses: statuses, Limit: 500})
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.Retrieve.claims")
	}
	queryVector, err := s.embedder.Embed(ctx, query.Text)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.Retrieve.embed")
	}
	scoredByID := make(map[string]scoredClaim, len(claims))
	for _, claim := range claims {
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		expired := claim.IsExpired(query.AsOf)
		if (expired || claim.Status == knowledgev1.ClaimExpired || claim.Status == knowledgev1.ClaimRetracted) && !query.IncludeExpired {
			continue
		}
		score, matched := scoreClaim(query.Text, queryVector, query.Filter, claim, expired)
		if score > 0.05 {
			scoredByID[claim.ClaimID] = scoredClaim{claim: claim, score: score, matched: matched, expired: expired}
		}
	}
	if query.RelationDepth > 0 {
		if err := s.expandRelations(ctx, ownerID, query, scoredByID, query.RelationDepth, deadline); err != nil {
			return nil, err
		}
	}

	candidates := make([]scoredClaim, 0, len(scoredByID))
	for _, item := range scoredByID {
		candidates = append(candidates, item)
	}
	precomputed := make([]entity.RetrievalHit, 0, len(candidates))
	for _, candidate := range candidates {
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		evidence, findErr := s.store.FindEvidence(ctx, candidate.claim.OwnerID, candidate.claim.EvidenceRefs)
		if findErr != nil {
			return nil, log.WrapError(findErr, "KnowledgeService.Retrieve.evidence")
		}
		accessible := make([]entity.Evidence, 0, len(evidence))
		evidenceRank := 0.0
		freshCount := 0
		for _, item := range evidence {
			if evidenceAccessibleTo(item, ownerID, query.OrganizationIDs, query.MaxSensitivity) && sourceAllowed(item.SourceType, query.Filter.SourceTypes) && item.Authority >= query.MinEvidenceAuthority {
				accessible = append(accessible, item)
				freshness := freshnessAt(item, query.AsOf)
				evidenceRank += .7*item.Authority + .3*freshness
				if freshness > 0 {
					freshCount++
				}
			}
		}
		if len(accessible) == 0 {
			continue
		}
		evidenceRank /= float64(len(accessible))
		staleEvidence := freshCount == 0
		expired := candidate.expired || candidate.claim.Status == knowledgev1.ClaimExpired
		if (expired || staleEvidence) && !query.IncludeExpired {
			continue
		}
		hasConflict := len(candidate.claim.ContradictedBy) > 0 || candidate.claim.Status == knowledgev1.ClaimContradicted
		determination := determinationFor(candidate.claim, expired, staleEvidence, hasConflict)
		hit := entity.RetrievalHit{Claim: candidate.claim, Evidence: accessible, Score: clamp(.7*candidate.score + .3*evidenceRank), EvidenceScore: clamp(evidenceRank), Expired: expired, StaleEvidence: staleEvidence, HasConflict: hasConflict, Determination: determination, MatchedBy: unique(candidate.matched), RelationPath: candidate.relationPath}
		if err := hit.Validate(); err != nil && determination == knowledgev1.DeterminationFact {
			return nil, log.WrapError(err, "KnowledgeService.Retrieve.validateHit")
		}
		precomputed = append(precomputed, hit)
	}
	sort.SliceStable(precomputed, func(i, j int) bool { return precomputed[i].Score > precomputed[j].Score })
	hits := make([]entity.RetrievalHit, 0, min(query.Budget.MaxResults, len(precomputed)))
	tokenUsage := 0
	for _, hit := range precomputed {
		if len(hits) >= query.Budget.MaxResults || time.Now().After(deadline) {
			break
		}
		estimated := estimateTokens(hit.Claim.Subject + " " + hit.Claim.Predicate + " " + hit.Claim.Value)
		for _, item := range hit.Evidence {
			estimated += estimateTokens(item.Excerpt)
		}
		if tokenUsage+estimated > query.Budget.MaxTokens {
			break
		}
		tokenUsage += estimated
		hits = append(hits, hit)
	}
	contradictions, err := s.retrievalContradictions(ctx, ownerID, query, hits)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.Retrieve.contradictions")
	}
	contradictions = relevantContradictions(contradictions, hits)
	snapshot, err := s.createSnapshot(ctx, ownerID, query, hits, contradictions)
	if err != nil {
		return nil, err
	}
	return &RetrievalResponse{Hits: hits, Contradictions: contradictions, Snapshot: snapshot, Budget: RetrievalBudgetUsage{Results: len(hits), Tokens: tokenUsage, TimeMS: time.Since(started).Milliseconds()}}, nil
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
		sourceType := researchSourceType(uri)
		trustProfile, authority, maxAge := trustPolicy(sourceType)
		staleAt := now.Add(maxAge)
		// An evidence record is an immutable observation. A later research run
		// must create a fresh record even when the fetched content is unchanged.
		sum := sha256.Sum256([]byte(ownerID + "\n" + taskID + "\n" + uri + "\n" + excerpt))
		item := entity.Evidence{
			Schema: entity.Schema, EvidenceID: "evidence-" + hex.EncodeToString(sum[:])[:32], OwnerID: ownerID,
			Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, SourceType: sourceType,
			Title: title, URI: uri, Accessible: true, Excerpt: excerpt,
			Authority: authority, Freshness: 1, ObservedAt: now, StaleAt: &staleAt,
			TrustProfile: trustProfile, AccessVerifiedAt: now,
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

func trustPolicy(sourceType string) (string, float64, time.Duration) {
	switch sourceType {
	case knowledgev1.SourceOfficial:
		return "official-web.v1", .95, 30 * 24 * time.Hour
	case knowledgev1.SourceResearch:
		return "ranked-research.v1", .7, 14 * 24 * time.Hour
	case knowledgev1.SourcePageObservation:
		return "page-observation.v1", .45, 24 * time.Hour
	default:
		return "user-confirmation.v1", 1, 3650 * 24 * time.Hour
	}
}

func (s *Service) ListContradictions(ctx context.Context, ownerID string, unresolvedOnly bool, limit int) ([]entity.Contradiction, error) {
	return s.store.ListContradictions(ctx, ownerID, unresolvedOnly, limit)
}

func (s *Service) ResolveContradiction(ctx context.Context, ownerID, actorID, contradictionID string, request ResolveContradictionRequest) (*entity.Contradiction, error) {
	value, err := s.store.FindContradiction(ctx, ownerID, contradictionID)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ResolveContradiction.find")
	}
	if value == nil {
		return nil, apierror.ErrNotFound.WithMessage("knowledge contradiction not found")
	}
	if value.Resolved {
		return nil, apierror.ErrConflict.WithMessage("knowledge contradiction is already resolved")
	}
	if !knowledgev1.ValidResolutionDecision(request.Decision) || strings.TrimSpace(request.Note) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("a valid contradiction decision and review note are required")
	}
	statuses := make(map[string]string, len(value.ClaimIDs))
	switch request.Decision {
	case knowledgev1.ResolutionKeepClaim:
		if !containsValue(value.ClaimIDs, request.WinningClaimID) {
			return nil, apierror.ErrBadRequest.WithMessage("winning claim must belong to the contradiction")
		}
		for _, claimID := range value.ClaimIDs {
			statuses[claimID] = knowledgev1.ClaimRetracted
		}
		statuses[request.WinningClaimID] = knowledgev1.ClaimActive
	case knowledgev1.ResolutionUncertain:
		for _, claimID := range value.ClaimIDs {
			statuses[claimID] = knowledgev1.ClaimContradicted
		}
	case knowledgev1.ResolutionRetractAll:
		for _, claimID := range value.ClaimIDs {
			statuses[claimID] = knowledgev1.ClaimRetracted
		}
	}
	now := time.Now().UTC()
	value.Resolved = true
	value.Resolution = &entity.ContradictionResolution{Decision: request.Decision, WinningClaimID: request.WinningClaimID, Note: strings.TrimSpace(request.Note), ResolvedBy: actorID, ResolvedAt: now}
	value.UpdatedAt = now
	if err := value.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.ResolveContradiction(ctx, *value, statuses); err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ResolveContradiction.persist")
	}
	return value, nil
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
	definition := knowledgev1.OntologyDefinition{
		Entities: []knowledgev1.OntologyEntity{{ID: "Agent"}, {ID: "Task"}, {ID: "Device"}, {ID: "Claim"}, {ID: "Evidence"}},
		Relations: []knowledgev1.OntologyRelation{
			{ID: "uses", SourceType: "Agent", TargetType: "Device"},
			{ID: "observes", SourceType: "Task", TargetType: "Evidence"},
			{ID: "supports", SourceType: "Claim", TargetType: "Evidence"},
			{ID: "contradicts", SourceType: "Claim", TargetType: "Claim"},
		},
	}
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

func (s *Service) ListOntologyCandidates(ctx context.Context, ownerID string, limit int) ([]entity.OntologyCandidate, error) {
	return s.store.ListOntologyCandidates(ctx, ownerID, limit)
}

func (s *Service) CreateOntologyCandidate(ctx context.Context, ownerID string, request CreateOntologyCandidateRequest) (*entity.OntologyCandidate, error) {
	now := time.Now().UTC()
	pack, err := s.store.FindOntologyPack(ctx, ownerID, request.PackID)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.CreateOntologyCandidate.pack")
	}
	if pack == nil {
		return nil, apierror.ErrNotFound.WithMessage("ontology pack not found")
	}
	if request.BaseVersion != pack.Current {
		return nil, apierror.ErrConflict.WithMessage("ontology candidate base version does not match the active pack")
	}
	evidence, err := s.store.FindEvidence(ctx, ownerID, unique(request.EvidenceRefs))
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.CreateOntologyCandidate.evidence")
	}
	if len(evidence) == 0 || len(evidence) != len(unique(request.EvidenceRefs)) {
		return nil, apierror.ErrBadRequest.WithMessage("ontology candidate requires accessible owner-scoped evidence")
	}
	for _, item := range evidence {
		if !evidenceAccessibleTo(item, ownerID, nil, knowledgev1.SensitivityRestricted) || evidenceIsStale(item, now) {
			return nil, apierror.ErrBadRequest.WithMessage("ontology candidate evidence is inaccessible or stale")
		}
	}
	checksum, err := checksumJSON(request.Definition)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	version := entity.OntologyVersion{Schema: entity.Schema, VersionID: ulid.New(), PackID: request.PackID, OwnerID: ownerID, Version: request.Version, CompatibleWith: unique(request.CompatibleWith), ValidationRules: request.ValidationRules, DisplayMetadata: request.DisplayMetadata, Definition: request.Definition, Checksum: checksum, Status: knowledgev1.OntologyReviewRequired, CreatedAt: now}
	evaluation := evaluateOntologyOffline(version, pack.Current, now)
	status := knowledgev1.OntologyReviewRequired
	if !evaluation.Passed {
		status = knowledgev1.OntologyRejected
	}
	candidate := entity.OntologyCandidate{Schema: entity.Schema, CandidateID: ulid.New(), OwnerID: ownerID, PackID: request.PackID, BaseVersion: request.BaseVersion, Proposed: version, EvidenceRefs: unique(request.EvidenceRefs), Evaluation: evaluation, CreatedBy: ownerID, Status: status, Revision: 1, CreatedAt: now, UpdatedAt: now}
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
	if candidate.Status != knowledgev1.OntologyReviewRequired || !candidate.Evaluation.Passed {
		return nil, apierror.ErrConflict.WithMessage("ontology candidate is not awaiting a passing offline review")
	}
	if strings.TrimSpace(request.ReviewNote) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("ontology review note is required")
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
	now := time.Now().UTC()
	candidate.ReviewNote, candidate.ReviewedBy, candidate.ReviewedAt, candidate.Revision, candidate.UpdatedAt = strings.TrimSpace(request.ReviewNote), actorID, &now, request.ExpectedRevision+1, now
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
	pack, err := s.store.FindOntologyPack(ctx, ownerID, candidate.PackID)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.CreateOntologyMigration.pack")
	}
	if pack == nil || pack.Current != candidate.BaseVersion {
		return nil, apierror.ErrConflict.WithMessage("ontology pack changed after candidate approval")
	}
	now := time.Now().UTC()
	migration := entity.OntologyMigration{Schema: entity.Schema, MigrationID: ulid.New(), OwnerID: ownerID, PackID: candidate.PackID, CandidateID: candidate.CandidateID, FromVersion: candidate.BaseVersion, ToVersion: candidate.Proposed.Version, Plan: request.Plan, RequestedBy: actorID, Status: knowledgev1.MigrationReviewRequired, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := migration.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateOntologyMigration(ctx, migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func (s *Service) ReviewOntologyMigration(ctx context.Context, ownerID, actorID, migrationID string, request ReviewOntologyMigrationRequest) (*entity.OntologyMigration, error) {
	migration, err := s.store.FindOntologyMigration(ctx, ownerID, migrationID)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ReviewOntologyMigration.find")
	}
	if migration == nil {
		return nil, apierror.ErrNotFound.WithMessage("ontology migration not found")
	}
	if migration.Status != knowledgev1.MigrationReviewRequired {
		return nil, apierror.ErrConflict.WithMessage("ontology migration is not awaiting review")
	}
	if strings.TrimSpace(request.ReviewNote) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("ontology migration review note is required")
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = migration.Revision
	}
	if request.ExpectedRevision != migration.Revision {
		return nil, apierror.ErrConflict.WithMessage("ontology migration revision changed")
	}
	now := time.Now().UTC()
	migration.Status, migration.ReviewNote = knowledgev1.MigrationRejected, strings.TrimSpace(request.ReviewNote)
	if request.Approved {
		migration.Status, migration.ApprovedBy, migration.ApprovedAt = knowledgev1.MigrationApproved, actorID, &now
	}
	migration.Revision++
	migration.UpdatedAt = now
	if err := migration.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.ReviewOntologyMigration(ctx, *migration, knowledgev1.MigrationReviewRequired); err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ReviewOntologyMigration.persist")
	}
	return migration, nil
}

func (s *Service) ExecuteOntologyMigration(ctx context.Context, ownerID, migrationID string) (*entity.OntologyMigration, error) {
	migration, err := s.store.FindOntologyMigration(ctx, ownerID, migrationID)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ExecuteOntologyMigration.find")
	}
	if migration == nil {
		return nil, apierror.ErrNotFound.WithMessage("ontology migration not found")
	}
	if migration.Status != knowledgev1.MigrationApproved || migration.ApprovedBy == "" || migration.ApprovedAt == nil {
		return nil, apierror.ErrConflict.WithMessage("ontology migration requires explicit human approval")
	}
	candidate, err := s.store.FindOntologyCandidate(ctx, ownerID, migration.CandidateID)
	if err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ExecuteOntologyMigration.candidate")
	}
	if candidate == nil {
		return nil, apierror.ErrConflict.WithMessage("approved ontology candidate is unavailable")
	}
	receipt, executeErr := s.migrator.Execute(ctx, *migration, *candidate)
	now := time.Now().UTC()
	migration.Execution = receipt
	migration.Revision++
	migration.UpdatedAt = now
	if executeErr != nil {
		if migration.Execution == nil {
			migration.Execution = &entity.OntologyMigrationExecution{ExecutorID: "athena.ontology-migrator.v1", StartedAt: now, CompletedAt: now, Success: false, Error: executeErr.Error()}
		}
		migration.Status = knowledgev1.MigrationFailed
		if persistErr := s.store.ReviewOntologyMigration(ctx, *migration, knowledgev1.MigrationApproved); persistErr != nil {
			return nil, log.WrapError(persistErr, "KnowledgeService.ExecuteOntologyMigration.recordFailure")
		}
		return nil, log.WrapError(executeErr, "KnowledgeService.ExecuteOntologyMigration.tool")
	}
	migration.Status = knowledgev1.MigrationApplied
	if err := migration.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.ApplyOntologyMigration(ctx, *migration); err != nil {
		return nil, log.WrapError(err, "KnowledgeService.ExecuteOntologyMigration.apply")
	}
	return migration, nil
}

func evaluateOntologyOffline(version entity.OntologyVersion, activeVersion string, now time.Time) entity.OntologyEvaluation {
	checks := []entity.OntologyEvaluationCheck{
		{Name: "typed-definition", Required: true, Passed: version.Definition.Validate() == nil},
		{Name: "version-contract", Required: true, Passed: version.Validate() == nil},
		{Name: "bounded-validation-rules", Required: true, Passed: len(version.ValidationRules) > 0 && len(version.ValidationRules) <= 512},
	}
	compatible := activeVersion == "" || containsValue(version.CompatibleWith, activeVersion)
	checks = append(checks, entity.OntologyEvaluationCheck{Name: "active-version-compatibility", Required: activeVersion != "", Passed: compatible, Detail: activeVersion})
	passed := true
	for _, check := range checks {
		if check.Required && !check.Passed {
			passed = false
		}
	}
	return entity.OntologyEvaluation{
		EvaluationID: ulid.New(), Environment: "OFFLINE", Evaluator: "athena.ontology-evaluator",
		EvaluatorVersion: "1.0.0", DefinitionSHA256: version.Checksum, Checks: checks, Passed: passed, EvaluatedAt: now,
	}
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

func (s *Service) createSnapshot(ctx context.Context, ownerID string, query entity.RetrievalQuery, hits []entity.RetrievalHit, contradictions []entity.Contradiction) (*entity.Snapshot, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	claimIDs, evidenceIDs, contradictionIDs := make([]string, 0, len(hits)), make([]string, 0), make([]string, 0, len(contradictions))
	claimDigestByID := make(map[string]map[string]string, len(hits))
	evidenceDigestByID := make(map[string]map[string]string)
	for _, hit := range hits {
		claimIDs = appendUnique(claimIDs, hit.Claim.ClaimID)
		claimDigestByID[hit.Claim.ClaimID] = map[string]string{"id": hit.Claim.ClaimID, "sha256": hit.Claim.Provenance.ContentSHA256, "determination": hit.Determination}
		for _, evidence := range hit.Evidence {
			evidenceIDs = appendUnique(evidenceIDs, evidence.EvidenceID)
			evidenceDigestByID[evidence.EvidenceID] = map[string]string{"id": evidence.EvidenceID, "sha256": evidence.Provenance.ContentSHA256}
		}
	}
	for _, contradiction := range contradictions {
		contradictionIDs = appendUnique(contradictionIDs, contradiction.ContradictionID)
	}
	sort.Strings(claimIDs)
	sort.Strings(evidenceIDs)
	sort.Strings(contradictionIDs)
	claimDigests := make([]map[string]string, 0, len(claimIDs))
	for _, claimID := range claimIDs {
		claimDigests = append(claimDigests, claimDigestByID[claimID])
	}
	evidenceDigests := make([]map[string]string, 0, len(evidenceIDs))
	for _, evidenceID := range evidenceIDs {
		evidenceDigests = append(evidenceDigests, evidenceDigestByID[evidenceID])
	}
	canonicalScopes := append([]string(nil), query.Scopes...)
	canonicalOrganizations := append([]string(nil), query.OrganizationIDs...)
	canonicalSourceTypes := append([]string(nil), query.Filter.SourceTypes...)
	sort.Strings(canonicalScopes)
	sort.Strings(canonicalOrganizations)
	sort.Strings(canonicalSourceTypes)
	querySHA, err := checksumJSON(map[string]any{
		"text": query.Text, "scopes": canonicalScopes, "organization_ids": canonicalOrganizations,
		"filter": map[string]any{"subject": query.Filter.Subject, "predicate": query.Filter.Predicate, "source_types": canonicalSourceTypes},
		"as_of":  query.AsOf, "include_expired": query.IncludeExpired, "relation_depth": query.RelationDepth,
		"min_evidence_authority": query.MinEvidenceAuthority,
	})
	if err != nil {
		return nil, err
	}
	checksum, err := checksumJSON(map[string]any{"claims": claimDigests, "evidence": evidenceDigests, "contradictions": contradictionIDs, "query_sha256": querySHA, "as_of": query.AsOf})
	if err != nil {
		return nil, err
	}
	ontologyPack, ontologyVersion := s.activeOntology(ctx, ownerID)
	snapshot := entity.Snapshot{Schema: entity.Schema, SnapshotID: ulid.New(), OwnerID: ownerID, QuerySHA256: querySHA, AsOf: query.AsOf, ClaimIDs: claimIDs, EvidenceIDs: evidenceIDs, ContradictionIDs: contradictionIDs, OntologyPack: ontologyPack, OntologyVer: ontologyVersion, Checksum: checksum, CreatedAt: time.Now().UTC()}
	if err := snapshot.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *Service) BindSnapshotToRun(ctx context.Context, ownerID, snapshotID, runManifestID string) error {
	if strings.TrimSpace(snapshotID) == "" || strings.TrimSpace(runManifestID) == "" {
		return apierror.ErrBadRequest.WithMessage("knowledge snapshot and run manifest ids are required")
	}
	return log.WrapError(s.store.BindSnapshotToRun(ctx, ownerID, snapshotID, runManifestID, time.Now().UTC().UnixMilli()), "KnowledgeService.BindSnapshotToRun")
}

func (s *Service) activeOntology(ctx context.Context, ownerID string) (string, string) {
	packs, err := s.store.ListOntologyPacks(ctx, ownerID)
	if err == nil {
		for _, pack := range packs {
			if pack.Domain == "core" && pack.Current != "" {
				return pack.PackID, pack.Current
			}
		}
	}
	return "core", "1.0.0"
}

func retrievalDefaults(query entity.RetrievalQuery) entity.RetrievalQuery {
	if query.AsOf.IsZero() {
		query.AsOf = time.Now().UTC()
	}
	if len(query.Scopes) == 0 {
		query.Scopes = []string{knowledgev1.ScopeUser, knowledgev1.ScopePublic}
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
	if query.MinEvidenceAuthority == 0 {
		query.MinEvidenceAuthority = .25
	}
	return query
}

func scoreClaim(query string, queryVector []float32, filter knowledgev1.RetrievalFilter, claim entity.Claim, expired bool) (float64, []string) {
	queryTokens, claimTokens := tokenVector(query), tokenVector(claim.Subject+" "+claim.Predicate+" "+claim.Value)
	keyword := overlap(queryTokens, claimTokens)
	vector := cosineFloat(queryVector, claim.SemanticVector)
	structured := 0.0
	matched := make([]string, 0, 4)
	normalized := normalizeText(query)
	if filter.Subject != "" && normalizeText(filter.Subject) == normalizeText(claim.Subject) {
		structured += .6
		matched = append(matched, "structured.subject")
	} else if strings.Contains(normalized, normalizeText(claim.Subject)) {
		structured += .55
		matched = append(matched, "subject")
	}
	if filter.Predicate != "" && normalizeText(filter.Predicate) == normalizeText(claim.Predicate) {
		structured += .4
		matched = append(matched, "structured.predicate")
	} else if strings.Contains(normalized, normalizeText(claim.Predicate)) {
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
	return clamp(.3*structured + .25*keyword + .35*vector + .1*temporal), unique(matched)
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

func cosineFloat(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	dot, leftNorm, rightNorm := 0.0, 0.0, 0.0
	for index, value := range left {
		rightValue := right[index]
		dot += float64(value * rightValue)
		leftNorm += float64(value * value)
		rightNorm += float64(rightValue * rightValue)
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func (s *Service) validateClaimRelations(ctx context.Context, ownerID string, claim entity.Claim) error {
	if len(claim.Relations) == 0 {
		return nil
	}
	ids := make([]string, 0, len(claim.Relations))
	for _, relation := range claim.Relations {
		ids = append(ids, relation.TargetClaimID)
	}
	targets, err := s.store.FindClaims(ctx, ownerID, unique(ids))
	if err != nil {
		return log.WrapError(err, "KnowledgeService.CreateClaim.relations")
	}
	if len(targets) != len(unique(ids)) {
		return apierror.ErrBadRequest.WithMessage("claim relation target is missing or outside the user boundary")
	}
	for _, target := range targets {
		if target.Status == knowledgev1.ClaimRetracted {
			return apierror.ErrBadRequest.WithMessage("claim relation cannot target retracted knowledge")
		}
	}
	return nil
}

func (s *Service) expandRelations(ctx context.Context, userID string, query entity.RetrievalQuery, scored map[string]scoredClaim, maxDepth int, deadline time.Time) error {
	frontier := make(map[string]scoredClaim, len(scored))
	for id, item := range scored {
		frontier[id] = item
	}
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		type relationSource struct {
			source scoredClaim
			edge   entity.ClaimRelation
		}
		byOwner := map[string][]string{}
		sources := map[string]relationSource{}
		for _, source := range frontier {
			for _, edge := range source.claim.Relations {
				if existing, ok := scored[edge.TargetClaimID]; ok && existing.score >= source.score*edge.Weight*.85 {
					continue
				}
				byOwner[source.claim.OwnerID] = appendUnique(byOwner[source.claim.OwnerID], edge.TargetClaimID)
				current, exists := sources[edge.TargetClaimID]
				if !exists || source.score*edge.Weight > current.source.score*current.edge.Weight {
					sources[edge.TargetClaimID] = relationSource{source: source, edge: edge}
				}
			}
		}
		next := map[string]scoredClaim{}
		for ownerID, ids := range byOwner {
			if time.Now().After(deadline) || ctx.Err() != nil {
				return ctx.Err()
			}
			targets, err := s.store.FindClaims(ctx, ownerID, ids)
			if err != nil {
				return log.WrapError(err, "KnowledgeService.Retrieve.relations")
			}
			for _, target := range targets {
				if !claimAccessibleTo(target, userID, query.OrganizationIDs, query.Scopes, query.MaxSensitivity) {
					continue
				}
				source := sources[target.ClaimID]
				expired := target.IsExpired(query.AsOf) || target.Status == knowledgev1.ClaimExpired
				if (expired || target.Status == knowledgev1.ClaimRetracted) && !query.IncludeExpired {
					continue
				}
				path := append(append([]string(nil), source.source.relationPath...), source.source.claim.ClaimID, source.edge.Predicate, target.ClaimID)
				item := scoredClaim{claim: target, score: clamp(source.source.score * source.edge.Weight * .85), matched: []string{"relation"}, expired: expired, relationPath: path}
				if existing, ok := scored[target.ClaimID]; ok {
					if item.score > existing.score {
						existing.score, existing.relationPath = item.score, item.relationPath
					}
					existing.matched = appendUnique(existing.matched, "relation")
					scored[target.ClaimID] = existing
				} else {
					scored[target.ClaimID] = item
				}
				next[target.ClaimID] = item
			}
		}
		frontier = next
	}
	return nil
}

func claimAccessibleTo(claim entity.Claim, userID string, organizationIDs, scopes []string, maximumSensitivity string) bool {
	if !containsValue(scopes, claim.Scope) || knowledgev1.SensitivityRank(claim.Sensitivity) > knowledgev1.SensitivityRank(maximumSensitivity) {
		return false
	}
	switch claim.Scope {
	case knowledgev1.ScopeUser:
		return claim.OwnerID == userID
	case knowledgev1.ScopeOrganization:
		return containsValue(organizationIDs, claim.OrganizationID)
	case knowledgev1.ScopePublic:
		return true
	default:
		return false
	}
}

func evidenceAccessibleTo(item entity.Evidence, userID string, organizationIDs []string, maximumSensitivity string) bool {
	if !item.Accessible || item.AccessVerifiedAt.IsZero() || knowledgev1.SensitivityRank(item.Sensitivity) > knowledgev1.SensitivityRank(maximumSensitivity) {
		return false
	}
	switch item.Scope {
	case knowledgev1.ScopeUser:
		return item.OwnerID == userID
	case knowledgev1.ScopeOrganization:
		return containsValue(organizationIDs, item.OrganizationID)
	case knowledgev1.ScopePublic:
		return true
	default:
		return false
	}
}

func sourceAllowed(value string, allowed []string) bool {
	return len(allowed) == 0 || containsValue(allowed, value)
}

func evidenceIsStale(item entity.Evidence, at time.Time) bool {
	return item.StaleAt != nil && !item.StaleAt.After(at)
}

func freshnessAt(item entity.Evidence, at time.Time) float64 {
	if item.StaleAt == nil {
		return clamp(item.Freshness)
	}
	if !item.StaleAt.After(at) {
		return 0
	}
	window := item.StaleAt.Sub(item.ObservedAt)
	if window <= 0 {
		return 0
	}
	remaining := item.StaleAt.Sub(at)
	if remaining > window {
		remaining = window
	}
	return clamp(item.Freshness * float64(remaining) / float64(window))
}

func determinationFor(claim entity.Claim, expired, staleEvidence, hasConflict bool) string {
	switch {
	case claim.Status == knowledgev1.ClaimRetracted:
		return knowledgev1.DeterminationRetracted
	case expired:
		return knowledgev1.DeterminationExpired
	case staleEvidence:
		return knowledgev1.DeterminationStaleEvidence
	case hasConflict:
		return knowledgev1.DeterminationConflicted
	default:
		return knowledgev1.DeterminationFact
	}
}

func claimPeriodsOverlap(left, right entity.Claim) bool {
	leftStart, rightStart := left.CreatedAt, right.CreatedAt
	if left.ValidFrom != nil {
		leftStart = *left.ValidFrom
	}
	if right.ValidFrom != nil {
		rightStart = *right.ValidFrom
	}
	if left.ValidUntil != nil && !left.ValidUntil.After(rightStart) {
		return false
	}
	if right.ValidUntil != nil && !right.ValidUntil.After(leftStart) {
		return false
	}
	return true
}

func (s *Service) retrievalContradictions(ctx context.Context, ownerID string, query entity.RetrievalQuery, hits []entity.RetrievalHit) ([]entity.Contradiction, error) {
	owners := []string{ownerID}
	for _, hit := range hits {
		owners = appendUnique(owners, hit.Claim.OwnerID)
	}
	result := make([]entity.Contradiction, 0)
	for _, currentOwner := range owners {
		items, err := s.store.ListContradictions(ctx, currentOwner, true, 100)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			claims, findErr := s.store.FindClaims(ctx, currentOwner, item.ClaimIDs)
			if findErr != nil {
				return nil, log.WrapError(findErr, "KnowledgeService.Retrieve.contradictionClaims")
			}
			if len(claims) != len(unique(item.ClaimIDs)) {
				continue
			}
			visible := true
			for _, claim := range claims {
				if !claimAccessibleTo(claim, ownerID, query.OrganizationIDs, query.Scopes, query.MaxSensitivity) {
					visible = false
					break
				}
			}
			if visible {
				result = append(result, item)
			}
		}
	}
	return result, nil
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
		if strings.HasSuffix(host, ".gov") || strings.HasSuffix(host, ".go.jp") || strings.HasSuffix(host, ".europa.eu") {
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
