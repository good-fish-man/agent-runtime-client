package learning

import (
	"context"
	"errors"
	"testing"
	"time"

	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
)

type evolutionExperienceSource struct {
	*testExperienceSource
	learningEnabled bool
}

type partiallyFailingEvolutionSource struct {
	*evolutionExperienceSource
}

func (s *partiallyFailingEvolutionSource) ListReadyOwners(context.Context, string, int) ([]string, error) {
	return []string{"owner-bad", "owner-1"}, nil
}

func (s *partiallyFailingEvolutionSource) GetPreference(ctx context.Context, ownerID string) (*experienceentity.Preference, error) {
	if ownerID == "owner-bad" {
		return nil, errors.New("corrupted preference")
	}
	return s.evolutionExperienceSource.GetPreference(ctx, ownerID)
}

func (s *evolutionExperienceSource) ListReadyOwners(context.Context, string, int) ([]string, error) {
	return []string{"owner-1"}, nil
}

func (s *evolutionExperienceSource) GetPreference(context.Context, string) (*experienceentity.Preference, error) {
	return &experienceentity.Preference{OwnerID: "owner-1", LearningEnabled: s.learningEnabled, RetentionDays: 30}, nil
}

func TestEvolutionOrchestratorProposesOnceAndWaitsForReview(t *testing.T) {
	evidence := evidenceSet("browser.navigate")
	learning, _, evaluator := newLearningTestService(t, evidence)
	source := &evolutionExperienceSource{testExperienceSource: &testExperienceSource{items: evidence}, learningEnabled: true}
	learning.experiences = source
	orchestrator := NewEvolutionOrchestrator(learning, source, EvolutionConfig{
		Enabled: true, ScanInterval: time.Hour, OwnerBatchSize: 10, ExperienceLimit: 100,
		MaxCandidatesPerScan: 5, MinimumNovelExperiences: 2,
	})

	first, err := orchestrator.ScanOwner(context.Background(), "owner-1")
	if err != nil {
		t.Fatalf("ScanOwner() error = %v", err)
	}
	if first.CandidatesProposed != 1 || len(first.CandidateIDs) != 1 || evaluator.calls != 1 {
		t.Fatalf("first scan = %+v, evaluator calls = %d", first, evaluator.calls)
	}
	candidate, _, _, err := learning.FindCandidate(context.Background(), "owner-1", first.CandidateIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if candidate == nil || candidate.Status != entity.LifecycleReviewRequired || candidate.Skill == nil {
		t.Fatalf("candidate = %+v", candidate)
	}
	if candidate.Skill.Metadata["origin"] != evolutionOrigin || candidate.Skill.Metadata["evidence_pattern"] == "" || candidate.Skill.Metadata["evidence_digest"] == "" {
		t.Fatalf("candidate metadata = %+v", candidate.Skill.Metadata)
	}

	second, err := orchestrator.ScanOwner(context.Background(), "owner-1")
	if err != nil {
		t.Fatalf("second ScanOwner() error = %v", err)
	}
	if second.CandidatesProposed != 0 || second.CandidatesSkipped != 1 || evaluator.calls != 1 {
		t.Fatalf("second scan = %+v, evaluator calls = %d", second, evaluator.calls)
	}
}

func TestEvolutionOrchestratorHonorsLearningPreference(t *testing.T) {
	evidence := evidenceSet("browser.navigate")
	learning, _, evaluator := newLearningTestService(t, evidence)
	source := &evolutionExperienceSource{testExperienceSource: &testExperienceSource{items: evidence}, learningEnabled: false}
	learning.experiences = source
	orchestrator := NewEvolutionOrchestrator(learning, source, EvolutionConfig{Enabled: true})
	result, err := orchestrator.ScanOwner(context.Background(), "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.CandidatesProposed != 0 || evaluator.calls != 0 {
		t.Fatalf("result = %+v, evaluator calls = %d", result, evaluator.calls)
	}
}

func TestEvolutionOrchestratorIsolatesOwnerFailure(t *testing.T) {
	evidence := evidenceSet("browser.navigate")
	learning, _, evaluator := newLearningTestService(t, evidence)
	base := &evolutionExperienceSource{testExperienceSource: &testExperienceSource{items: evidence}, learningEnabled: true}
	source := &partiallyFailingEvolutionSource{evolutionExperienceSource: base}
	learning.experiences = source
	orchestrator := NewEvolutionOrchestrator(learning, source, EvolutionConfig{
		Enabled: true, ScanInterval: time.Hour, OwnerBatchSize: 10, ExperienceLimit: 100,
		MaxCandidatesPerScan: 5, MinimumNovelExperiences: 2,
	})

	result, err := orchestrator.ScanOnce(context.Background())
	if err == nil {
		t.Fatal("expected aggregated owner error")
	}
	if result.OwnersScanned != 2 || result.CandidatesProposed != 1 || evaluator.calls != 1 {
		t.Fatalf("scan result = %+v, evaluator calls = %d, error = %v", result, evaluator.calls, err)
	}
}
