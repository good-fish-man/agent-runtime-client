package knowledge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge"
	repository "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/knowledge"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

func TestEvidenceGatedRetrievalExpiryConflictAndOwnerIsolation(t *testing.T) {
	service := newKnowledgeService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	persistEvidence(t, service, officialEvidence("e-1", "owner-1", "https://example.jp/license", "An official translation is required", now))
	first, conflicts, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{
		Claim: entity.Claim{Subject: "foreign-license-conversion", Predicate: "requires", Value: "official translation", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .95, TimeSensitive: true, ValidUntil: &future}, EvidenceRefs: []string{"e-1"},
	})
	if err != nil || len(conflicts) != 0 || first.Status != knowledgev1.ClaimActive {
		t.Fatalf("first claim=%+v conflicts=%+v error=%v", first, conflicts, err)
	}
	persistEvidence(t, service, officialEvidence("e-2", "owner-1", "https://example.jp/notice", "A newer notice says no translation", now))
	second, conflicts, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{
		Claim: entity.Claim{Subject: "foreign-license-conversion", Predicate: "requires", Value: "no translation", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .9}, EvidenceRefs: []string{"e-2"},
	})
	if err != nil || second.Status != knowledgev1.ClaimContradicted || len(conflicts) != 1 {
		t.Fatalf("second claim=%+v conflicts=%+v error=%v", second, conflicts, err)
	}
	past := now.Add(-time.Hour)
	persistEvidence(t, service, officialEvidence("e-3", "owner-1", "https://example.jp/hours", "Office hours", now))
	if _, _, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{
		Claim: entity.Claim{Subject: "office", Predicate: "open", Value: "today", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .8, TimeSensitive: true, ValidUntil: &past}, EvidenceRefs: []string{"e-3"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Retrieve(ctx, "owner-1", entity.RetrievalQuery{Text: "foreign license conversion requires translation", Scopes: []string{knowledgev1.ScopeUser}, MaxSensitivity: knowledgev1.SensitivityInternal})
	if err != nil || len(result.Hits) != 2 || len(result.Contradictions) != 1 || result.Snapshot == nil {
		t.Fatalf("retrieval=%+v error=%v", result, err)
	}
	for _, hit := range result.Hits {
		if len(hit.Evidence) == 0 || !hit.HasConflict || hit.Expired {
			t.Fatalf("invalid hit: %+v", hit)
		}
	}
	owner2, err := service.Retrieve(ctx, "owner-2", entity.RetrievalQuery{Text: "foreign license conversion", Scopes: []string{knowledgev1.ScopeUser}, MaxSensitivity: knowledgev1.SensitivityRestricted})
	if err != nil || len(owner2.Hits) != 0 {
		t.Fatalf("owner boundary leaked: %+v, %v", owner2, err)
	}
}

func TestSinglePageObservationCannotDirectlyCreateKnowledge(t *testing.T) {
	service := newKnowledgeService(t)
	now := time.Now().UTC()
	staleAt := now.Add(time.Hour)
	persistEvidence(t, service, entity.Evidence{Schema: entity.Schema, EvidenceID: "page-1", OwnerID: "owner-1", SourceType: knowledgev1.SourcePageObservation, Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Title: "Observed page", URI: "https://example.com", Accessible: true, Excerpt: "Available", Authority: .45, Freshness: 1, ObservedAt: now, StaleAt: &staleAt, TrustProfile: "page-observation.v1", AccessVerifiedAt: now, Provenance: testProvenance(now, "page")})
	_, _, err := service.CreateClaim(context.Background(), "owner-1", CreateClaimRequest{
		Claim: entity.Claim{Subject: "page", Predicate: "shows", Value: "available", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .5}, EvidenceRefs: []string{"page-1"},
	})
	if err == nil {
		t.Fatal("single observation became knowledge")
	}
}

func TestResearchPagesAreEvidenceOnly(t *testing.T) {
	service := newKnowledgeService(t)
	err := service.CaptureResearchEvidence(context.Background(), "owner-1", "task-1", "trace-1", map[string]any{
		"pages": []any{map[string]any{"title": "Official procedure", "url": "https://www.example.go.jp/procedure", "snippet": "Current application steps", "authority": .95, "freshness": .9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.ListEvidence(context.Background(), "owner-1", 10)
	if err != nil || len(evidence) != 1 || evidence[0].SourceType != knowledgev1.SourceOfficial {
		t.Fatalf("evidence=%+v error=%v", evidence, err)
	}
	claims, err := service.ListClaims(context.Background(), "owner-1", 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("research page was promoted directly to a claim: %+v, %v", claims, err)
	}
}

func TestResearchRefetchCreatesFreshImmutableEvidence(t *testing.T) {
	service := newKnowledgeService(t)
	snapshot := map[string]any{
		"pages": []any{map[string]any{"title": "Current notice", "url": "https://example.com/notice", "snippet": "The published rule is unchanged"}},
	}
	if err := service.CaptureResearchEvidence(context.Background(), "owner-1", "task-1", "trace-1", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := service.CaptureResearchEvidence(context.Background(), "owner-1", "task-2", "trace-2", snapshot); err != nil {
		t.Fatal(err)
	}
	evidence, err := service.ListEvidence(context.Background(), "owner-1", 10)
	if err != nil || len(evidence) != 2 {
		t.Fatalf("fresh observations were not preserved independently: evidence=%+v error=%v", evidence, err)
	}
	if evidence[0].EvidenceID == evidence[1].EvidenceID || evidence[0].Provenance.SourceTaskID == evidence[1].Provenance.SourceTaskID {
		t.Fatalf("research observations were incorrectly deduplicated: %+v", evidence)
	}
}

func TestOrganizationRetrievalUsesAuthenticatedScopeOnly(t *testing.T) {
	service := newKnowledgeService(t)
	now := time.Now().UTC()
	evidence := officialEvidence("org-evidence", "publisher", "https://example.org/policy", "Organization policy", now)
	evidence.Scope = knowledgev1.ScopeOrganization
	evidence.OrganizationID = "org-a"
	persistEvidence(t, service, evidence)
	claim := entity.Claim{
		Schema: entity.Schema, ClaimID: "org-claim", OwnerID: "publisher", OrganizationID: "org-a",
		Subject: "organization-policy", Predicate: "status", Value: "active",
		Scope: knowledgev1.ScopeOrganization, Sensitivity: knowledgev1.SensitivityInternal,
		EvidenceRefs: []string{evidence.EvidenceID}, Confidence: .9, Status: knowledgev1.ClaimActive,
		Provenance: testProvenance(now, "organization-policy\nstatus\nactive"), CreatedAt: now, UpdatedAt: now,
	}
	if err := claim.ValidateAt(map[string]entity.Evidence{evidence.EvidenceID: evidence}, now); err != nil {
		t.Fatal(err)
	}
	if err := service.store.CreateKnowledge(context.Background(), claim, nil, nil); err != nil {
		t.Fatal(err)
	}

	orgContext := authctx.WithOrganizationID(context.Background(), "org-a")
	result, err := service.Retrieve(orgContext, "viewer", entity.RetrievalQuery{
		Text: "organization policy", Scopes: []string{knowledgev1.ScopeOrganization},
		OrganizationIDs: []string{"org-attacker"}, MaxSensitivity: knowledgev1.SensitivityInternal,
	})
	if err != nil || len(result.Hits) != 1 || result.Hits[0].Claim.ClaimID != claim.ClaimID {
		t.Fatalf("same-organization knowledge was not retrieved: result=%+v error=%v", result, err)
	}
	otherContext := authctx.WithOrganizationID(context.Background(), "org-b")
	isolated, err := service.Retrieve(otherContext, "viewer", entity.RetrievalQuery{
		Text: "organization policy", Scopes: []string{knowledgev1.ScopeOrganization},
		OrganizationIDs: []string{"org-a"}, MaxSensitivity: knowledgev1.SensitivityInternal,
	})
	if err != nil || len(isolated.Hits) != 0 {
		t.Fatalf("request-controlled organization scope leaked knowledge: result=%+v error=%v", isolated, err)
	}
}

func TestResearchSourceTypeRejectsOfficialLookalikeDomain(t *testing.T) {
	if got := researchSourceType("https://www.example.go.jp.attacker.com/procedure"); got != knowledgev1.SourceResearch {
		t.Fatalf("lookalike domain was trusted as official: %s", got)
	}
	if got := researchSourceType("https://www.example.go.jp/procedure"); got != knowledgev1.SourceOfficial {
		t.Fatalf("official domain was not recognized: %s", got)
	}
}

func TestPublicRetrievalDoesNotLeakPrivateContradictionDetails(t *testing.T) {
	service := newKnowledgeService(t)
	ctx, now := context.Background(), time.Now().UTC()
	staleAt := now.Add(24 * time.Hour)
	publicEvidence := entity.Evidence{Schema: entity.Schema, EvidenceID: "e-public", OwnerID: "publisher", Scope: knowledgev1.ScopePublic, Sensitivity: knowledgev1.SensitivityPublic, SourceType: knowledgev1.SourceOfficial, Title: "Public notice", URI: "https://example.gov/notice", Accessible: true, Excerpt: "The public value is A", Authority: .95, Freshness: 1, ObservedAt: now, StaleAt: &staleAt, TrustProfile: "official-web.v1", AccessVerifiedAt: now, Provenance: testProvenance(now, "public")}
	privateEvidence := entity.Evidence{Schema: entity.Schema, EvidenceID: "e-private", OwnerID: "publisher", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, SourceType: knowledgev1.SourceUserConfirmation, Title: "Private note", URI: "athena://user/confirmation/e-private", Accessible: true, Excerpt: "The private value is B", Authority: 1, Freshness: 1, ObservedAt: now, TrustProfile: "user-confirmation.v1", AccessVerifiedAt: now, Provenance: testProvenance(now, "private")}
	persistEvidence(t, service, publicEvidence, privateEvidence)
	publicVector, err := service.embedder.Embed(ctx, "public-rule value A")
	if err != nil {
		t.Fatal(err)
	}
	publicClaim := entity.Claim{Schema: entity.Schema, ClaimID: "claim-public", OwnerID: "publisher", Subject: "public-rule", Predicate: "value", Value: "A", Scope: knowledgev1.ScopePublic, Sensitivity: knowledgev1.SensitivityPublic, EvidenceRefs: []string{publicEvidence.EvidenceID}, Confidence: .9, SemanticVector: publicVector, Status: knowledgev1.ClaimActive, Provenance: testProvenance(now, "public-claim"), CreatedAt: now, UpdatedAt: now}
	if err := service.store.CreateKnowledge(ctx, publicClaim, nil, nil); err != nil {
		t.Fatal(err)
	}
	privateClaim := entity.Claim{Schema: entity.Schema, ClaimID: "claim-private", OwnerID: "publisher", Subject: "public-rule", Predicate: "value", Value: "B", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, EvidenceRefs: []string{privateEvidence.EvidenceID}, Confidence: 1, ContradictedBy: []string{publicClaim.ClaimID}, Status: knowledgev1.ClaimContradicted, Provenance: testProvenance(now, "private-claim"), CreatedAt: now, UpdatedAt: now}
	conflict := entity.Contradiction{Schema: entity.Schema, ContradictionID: "conflict-private", OwnerID: "publisher", Subject: "public-rule", Predicate: "value", ClaimIDs: []string{publicClaim.ClaimID, privateClaim.ClaimID}, EvidenceRefs: []string{publicEvidence.EvidenceID, privateEvidence.EvidenceID}, Severity: "HIGH", Summary: "Conflicting values", CreatedAt: now, UpdatedAt: now}
	if err := service.store.CreateKnowledge(ctx, privateClaim, []entity.Contradiction{conflict}, []string{publicClaim.ClaimID}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Retrieve(ctx, "viewer", entity.RetrievalQuery{Text: "public rule value", Scopes: []string{knowledgev1.ScopePublic}, MaxSensitivity: knowledgev1.SensitivityPublic})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || !result.Hits[0].HasConflict || len(result.Contradictions) != 0 {
		t.Fatalf("private contradiction details leaked: %+v", result)
	}
}

func TestOntologyNeedsOfflineCandidateHumanReviewAndMigrationTool(t *testing.T) {
	service := newKnowledgeService(t)
	ctx := context.Background()
	if err := service.EnsureCoreOntology(ctx, "owner-1"); err != nil {
		t.Fatal(err)
	}
	packs, err := service.ListOntologyPacks(ctx, "owner-1")
	if err != nil || len(packs) != 1 {
		t.Fatalf("packs=%+v error=%v", packs, err)
	}
	activePack, activeVersion, err := service.ResolveActiveOntology(ctx, "owner-1")
	if err != nil || activePack.PackID != packs[0].PackID || activeVersion.Version != packs[0].Current || activeVersion.Status != knowledgev1.OntologyApproved {
		t.Fatalf("active ontology pack=%+v version=%+v error=%v", activePack, activeVersion, err)
	}
	persistEvidence(t, service, officialEvidence("e-reviewed", "owner-1", "https://example.go.jp/ontology", "Reviewed domain vocabulary", time.Now().UTC()))
	candidate, err := service.CreateOntologyCandidate(ctx, "owner-1", CreateOntologyCandidateRequest{
		PackID: packs[0].PackID, BaseVersion: "1.0.0", Version: "1.1.0", CompatibleWith: []string{"1.0.0"}, EvidenceRefs: []string{"e-reviewed"},
		ValidationRules: []knowledgev1.ValidationRule{{SubjectType: "Claim", Predicate: "supports", ValueType: "Evidence", Required: true}},
		Definition:      knowledgev1.OntologyDefinition{Entities: []knowledgev1.OntologyEntity{{ID: "Claim"}, {ID: "Evidence"}, {ID: "Source"}}, Relations: []knowledgev1.OntologyRelation{{ID: "supports", SourceType: "Claim", TargetType: "Evidence"}}},
	})
	if err != nil || candidate.Status != knowledgev1.OntologyReviewRequired || !candidate.Evaluation.Passed || candidate.Evaluation.Environment != "OFFLINE" {
		t.Fatalf("candidate=%+v error=%v", candidate, err)
	}
	if _, err := service.ReviewOntologyCandidate(ctx, "owner-1", "codex:gpt-5", candidate.CandidateID, ReviewOntologyCandidateRequest{Approved: true, ReviewNote: "self approve", ExpectedRevision: candidate.Revision}); err == nil {
		t.Fatal("Codex identity approved its own ontology candidate")
	}
	approved, err := service.ReviewOntologyCandidate(ctx, "owner-1", "reviewer-1", candidate.CandidateID, ReviewOntologyCandidateRequest{Approved: true, ReviewNote: "Offline checks passed", ExpectedRevision: candidate.Revision})
	if err != nil || approved.Proposed.ApprovedBy != "reviewer-1" {
		t.Fatalf("approved=%+v error=%v", approved, err)
	}
	request := CreateOntologyMigrationRequest{CandidateID: candidate.CandidateID, Plan: []knowledgev1.OntologyMigrationStep{{Operation: "ADD_ENTITY", Target: "Source"}}}
	migration, err := service.CreateOntologyMigration(ctx, "owner-1", "reviewer-1", request)
	if err != nil || migration.Status != knowledgev1.MigrationReviewRequired {
		t.Fatalf("migration=%+v error=%v", migration, err)
	}
	if _, err := service.ExecuteOntologyMigration(ctx, "owner-1", migration.MigrationID); err == nil {
		t.Fatal("unreviewed production migration executed")
	}
	migration, err = service.ReviewOntologyMigration(ctx, "owner-1", "reviewer-2", migration.MigrationID, ReviewOntologyMigrationRequest{Approved: true, ReviewNote: "Migration plan reviewed", ExpectedRevision: migration.Revision})
	if err != nil || migration.Status != knowledgev1.MigrationApproved {
		t.Fatalf("reviewed migration=%+v error=%v", migration, err)
	}
	migration, err = service.ExecuteOntologyMigration(ctx, "owner-1", migration.MigrationID)
	if err != nil || migration.Execution == nil || !migration.Execution.Success {
		t.Fatalf("approved tool migration failed: migration=%+v error=%v", migration, err)
	}
	packs, err = service.ListOntologyPacks(ctx, "owner-1")
	if err != nil || len(packs) != 1 || packs[0].Current != "1.1.0" {
		t.Fatalf("ontology pointer was not advanced atomically: packs=%+v error=%v", packs, err)
	}
}

func TestNewOntologyPackStartsAsNonExecutablePlanningBaseline(t *testing.T) {
	service := newKnowledgeService(t)
	ctx := context.Background()
	if err := service.EnsureCoreOntology(ctx, "owner-1"); err != nil {
		t.Fatal(err)
	}
	pack, err := service.CreateOntologyPack(ctx, "owner-1", CreateOntologyPackRequest{Name: "Factory operations", Domain: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	if pack.Current != "0.0.0" {
		t.Fatalf("planning baseline = %q, want 0.0.0", pack.Current)
	}
	persistEvidence(t, service, officialEvidence("factory-evidence", "owner-1", "https://example.com/factory-ontology", "Reviewed factory vocabulary", time.Now().UTC()))
	candidate, err := service.CreateOntologyCandidate(ctx, "owner-1", CreateOntologyCandidateRequest{
		PackID: pack.PackID, BaseVersion: pack.Current, Version: "1.0.0", CompatibleWith: []string{pack.Current}, EvidenceRefs: []string{"factory-evidence"},
		ValidationRules: []knowledgev1.ValidationRule{{SubjectType: "Machine", Predicate: "feeds", ValueType: "Machine", Required: false}},
		Definition: knowledgev1.OntologyDefinition{
			Entities:  []knowledgev1.OntologyEntity{{ID: "Machine"}},
			Relations: []knowledgev1.OntologyRelation{{ID: "feeds", SourceType: "Machine", TargetType: "Machine"}},
		},
	})
	if err != nil || candidate.Status != knowledgev1.OntologyReviewRequired || !candidate.Evaluation.Passed {
		t.Fatalf("planning candidate = %+v, error = %v", candidate, err)
	}
	activePack, _, err := service.ResolveActiveOntology(ctx, "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if activePack.PackID == pack.PackID {
		t.Fatal("a planned ontology pack became active without review and migration")
	}
}

func TestHybridRetrievalTraversesRelationsAndMarksStaleEvidence(t *testing.T) {
	service := newKnowledgeService(t)
	ctx, now := context.Background(), time.Now().UTC()
	persistEvidence(t, service, officialEvidence("e-target", "owner-1", "https://example.go.jp/target", "Bring an original residence certificate", now))
	target, _, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{Claim: entity.Claim{Subject: "required-document", Predicate: "name", Value: "residence certificate", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal}, EvidenceRefs: []string{"e-target"}})
	if err != nil {
		t.Fatal(err)
	}
	persistEvidence(t, service, officialEvidence("e-procedure", "owner-1", "https://example.go.jp/procedure", "Foreign license conversion procedure", now))
	procedure, _, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{Claim: entity.Claim{Subject: "foreign-license-conversion", Predicate: "has-document", Value: "required documents", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Relations: []entity.ClaimRelation{{Predicate: "requires", TargetClaimID: target.ClaimID, Weight: 1}}}, EvidenceRefs: []string{"e-procedure"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Retrieve(ctx, "owner-1", entity.RetrievalQuery{Text: "foreign license conversion", Scopes: []string{knowledgev1.ScopeUser}, MaxSensitivity: knowledgev1.SensitivityInternal, RelationDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	foundRelation := false
	for _, hit := range result.Hits {
		if hit.Claim.ClaimID == target.ClaimID && containsValue(hit.MatchedBy, "relation") && len(hit.RelationPath) > 0 {
			foundRelation = true
		}
	}
	if !foundRelation || procedure.SemanticVector == nil {
		t.Fatalf("relation traversal or vector indexing missing: %+v", result.Hits)
	}

	stale := officialEvidence("e-stale", "owner-1", "https://example.go.jp/stale", "Old opening hours", now.Add(-48*time.Hour))
	staleAt := now.Add(-24 * time.Hour)
	stale.StaleAt = &staleAt
	persistEvidence(t, service, stale)
	claim, _, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{Claim: entity.Claim{Subject: "office", Predicate: "hours", Value: "09:00-17:00", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal}, EvidenceRefs: []string{"e-stale"}})
	if err != nil || claim.Status != knowledgev1.ClaimExpired {
		t.Fatalf("stale claim=%+v error=%v", claim, err)
	}
	result, err = service.Retrieve(ctx, "owner-1", entity.RetrievalQuery{Text: "office hours", Scopes: []string{knowledgev1.ScopeUser}, MaxSensitivity: knowledgev1.SensitivityInternal, IncludeExpired: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range result.Hits {
		if hit.Claim.ClaimID == claim.ClaimID && hit.Determination == knowledgev1.DeterminationFact {
			t.Fatal("stale evidence was returned as a deterministic fact")
		}
	}
}

func TestContradictionResolutionIsAudited(t *testing.T) {
	service := newKnowledgeService(t)
	ctx, now := context.Background(), time.Now().UTC()
	persistEvidence(t, service, officialEvidence("e-left", "owner-1", "https://example.go.jp/left", "Rule A", now))
	left, _, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{Claim: entity.Claim{Subject: "rule", Predicate: "value", Value: "A", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal}, EvidenceRefs: []string{"e-left"}})
	if err != nil {
		t.Fatal(err)
	}
	persistEvidence(t, service, officialEvidence("e-right", "owner-1", "https://example.go.jp/right", "Rule B", now))
	_, contradictions, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{Claim: entity.Claim{Subject: "rule", Predicate: "value", Value: "B", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal}, EvidenceRefs: []string{"e-right"}})
	if err != nil || len(contradictions) != 1 {
		t.Fatalf("contradictions=%+v error=%v", contradictions, err)
	}
	resolved, err := service.ResolveContradiction(ctx, "owner-1", "reviewer-1", contradictions[0].ContradictionID, ResolveContradictionRequest{Decision: knowledgev1.ResolutionKeepClaim, WinningClaimID: left.ClaimID, Note: "Verified against the current official rule"})
	if err != nil || !resolved.Resolved || resolved.Resolution == nil || resolved.Resolution.ResolvedBy != "reviewer-1" {
		t.Fatalf("resolved=%+v error=%v", resolved, err)
	}
	result, err := service.Retrieve(ctx, "owner-1", entity.RetrievalQuery{Text: "rule value A", Scopes: []string{knowledgev1.ScopeUser}, MaxSensitivity: knowledgev1.SensitivityInternal})
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("resolved knowledge was not retrievable: result=%+v error=%v", result, err)
	}
	if result.Hits[0].Determination != knowledgev1.DeterminationFact || result.Hits[0].HasConflict || len(result.Hits[0].Claim.ContradictedBy) != 0 {
		t.Fatalf("winning claim retained a resolved contradiction: %+v", result.Hits[0])
	}
}

func TestPredictionErrorCreatesExperienceSignalButNeverPolicyMutation(t *testing.T) {
	result := ComparePrediction(map[string]any{"url": "a", "playing": true}, map[string]any{"url": "b", "playing": false})
	if result.Matched || !result.ExperienceEligible || result.PolicyMutation || len(result.ChangedFields) != 2 {
		t.Fatalf("unexpected prediction evaluation: %+v", result)
	}
}

func newKnowledgeService(t *testing.T) *Service {
	t.Helper()
	dsn := fmt.Sprintf("file:knowledge-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.Claim{}, &po.Evidence{}, &po.Contradiction{}, &po.Snapshot{}, &po.OntologyPack{}, &po.OntologyVersion{}, &po.OntologyCandidate{}, &po.OntologyMigration{}); err != nil {
		t.Fatal(err)
	}
	return NewService(repository.NewStore(data.New(db)))
}

func officialEvidence(id, owner, uri, excerpt string, now time.Time) entity.Evidence {
	staleAt := now.Add(30 * 24 * time.Hour)
	return entity.Evidence{Schema: entity.Schema, EvidenceID: id, OwnerID: owner, Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, SourceType: knowledgev1.SourceOfficial, Title: "Official guidance", URI: uri, Accessible: true, Excerpt: excerpt, Authority: .95, Freshness: 1, ObservedAt: now, StaleAt: &staleAt, TrustProfile: "official-web.v1", AccessVerifiedAt: now, Provenance: testProvenance(now, excerpt)}
}

func persistEvidence(t *testing.T, service *Service, values ...entity.Evidence) {
	t.Helper()
	if err := service.store.CreateEvidence(context.Background(), values); err != nil {
		t.Fatal(err)
	}
}

func testProvenance(now time.Time, body string) entity.Provenance {
	return normalizeProvenance(entity.Provenance{Producer: "test-fixture", Method: "verified-source"}, now, body)
}
