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
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

func TestEvidenceGatedRetrievalExpiryConflictAndOwnerIsolation(t *testing.T) {
	service := newKnowledgeService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	first, conflicts, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{
		Claim:    entity.Claim{Subject: "foreign-license-conversion", Predicate: "requires", Value: "official translation", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .95, TimeSensitive: true, ValidUntil: &future},
		Evidence: []entity.Evidence{officialEvidence("e-1", "owner-1", "https://example.jp/license", "An official translation is required", now)},
	})
	if err != nil || len(conflicts) != 0 || first.Status != knowledgev1.ClaimActive {
		t.Fatalf("first claim=%+v conflicts=%+v error=%v", first, conflicts, err)
	}
	second, conflicts, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{
		Claim:    entity.Claim{Subject: "foreign-license-conversion", Predicate: "requires", Value: "no translation", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .9},
		Evidence: []entity.Evidence{officialEvidence("e-2", "owner-1", "https://example.jp/notice", "A newer notice says no translation", now)},
	})
	if err != nil || second.Status != knowledgev1.ClaimContradicted || len(conflicts) != 1 {
		t.Fatalf("second claim=%+v conflicts=%+v error=%v", second, conflicts, err)
	}
	past := now.Add(-time.Hour)
	if _, _, err := service.CreateClaim(ctx, "owner-1", CreateClaimRequest{
		Claim:    entity.Claim{Subject: "office", Predicate: "open", Value: "today", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .8, TimeSensitive: true, ValidUntil: &past},
		Evidence: []entity.Evidence{officialEvidence("e-3", "owner-1", "https://example.jp/hours", "Office hours", now)},
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
	_, _, err := service.CreateClaim(context.Background(), "owner-1", CreateClaimRequest{
		Claim:    entity.Claim{Subject: "page", Predicate: "shows", Value: "available", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Confidence: .5},
		Evidence: []entity.Evidence{{SourceType: knowledgev1.SourcePageObservation, Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, Title: "Observed page", URI: "https://example.com", Accessible: true, Excerpt: "Available", Authority: .5, Freshness: 1, ObservedAt: now}},
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
	candidate, err := service.CreateOntologyCandidate(ctx, "owner-1", CreateOntologyCandidateRequest{
		PackID: packs[0].PackID, BaseVersion: "1.0.0", Version: "1.1.0", EvidenceRefs: []string{"e-reviewed"}, EvaluationID: "eval-offline-1",
		ValidationRules: []knowledgev1.ValidationRule{{SubjectType: "Claim", Predicate: "supports", ValueType: "Evidence", Required: true}}, Definition: map[string]any{"entities": []string{"Claim", "Evidence", "Source"}},
	})
	if err != nil || candidate.Status != knowledgev1.OntologyReviewRequired {
		t.Fatalf("candidate=%+v error=%v", candidate, err)
	}
	approved, err := service.ReviewOntologyCandidate(ctx, "owner-1", "reviewer-1", candidate.CandidateID, ReviewOntologyCandidateRequest{Approved: true, ExpectedRevision: candidate.Revision})
	if err != nil || approved.Proposed.ApprovedBy != "reviewer-1" {
		t.Fatalf("approved=%+v error=%v", approved, err)
	}
	request := CreateOntologyMigrationRequest{CandidateID: candidate.CandidateID, FromVersion: "1.0.0", ToVersion: "1.1.0", Plan: []string{"add Source entity"}}
	if _, err := service.CreateOntologyMigration(ctx, "owner-1", "reviewer-1", request); err == nil {
		t.Fatal("production migration ran without migration tool")
	}
	request.ToolExecution = true
	if _, err := service.CreateOntologyMigration(ctx, "owner-1", "reviewer-1", request); err != nil {
		t.Fatalf("approved tool migration failed: %v", err)
	}
	packs, err = service.ListOntologyPacks(ctx, "owner-1")
	if err != nil || len(packs) != 1 || packs[0].Current != "1.1.0" {
		t.Fatalf("ontology pointer was not advanced atomically: packs=%+v error=%v", packs, err)
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
	return entity.Evidence{Schema: entity.Schema, EvidenceID: id, OwnerID: owner, Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal, SourceType: knowledgev1.SourceOfficial, Title: "Official guidance", URI: uri, Accessible: true, Excerpt: excerpt, Authority: 1, Freshness: 1, ObservedAt: now}
}
