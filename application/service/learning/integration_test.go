package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	experiencesvc "github.com/good-fish-man/agent-runtime-client/application/service/experience"
	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	controlpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	experiencepo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/experience"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	experiencerepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/experience"
	learningrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/learning"
)

func TestCandidatePipelineUsesRealOfflineReplayAndMaterializesReviewedVersion(t *testing.T) {
	dsn := fmt.Sprintf("file:learning-integration-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&experiencepo.Experience{}, &experiencepo.ExperiencePayload{}, &experiencepo.ExperienceEventRef{},
		&experiencepo.ExperienceRedaction{}, &experiencepo.FailureClassification{},
		&experiencepo.EvaluationFixture{}, &experiencepo.EvaluationSuite{}, &experiencepo.EvaluationRun{}, &experiencepo.EvaluationResult{},
		&learningpo.Candidate{}, &learningpo.CandidateEvidence{}, &learningpo.CandidateEvaluation{},
		&learningpo.Skill{}, &learningpo.SkillVersion{}, &learningpo.Strategy{}, &learningpo.StrategyVersion{}, &learningpo.Demonstration{},
		&controlpo.CapabilityDefinition{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if err := db.Create(&controlpo.CapabilityDefinition{
		CapabilityID: "browser.navigate", OwnerID: "system", Version: "1", Operations: `["navigate"]`,
		Modalities: `[]`, InputSchema: `{}`, OutputSchema: `{}`, Risk: "R1", Enabled: true,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	experienceStore := experiencerepo.NewStore(data.New(db))
	items := evidenceSet("browser.navigate")
	for index := range items {
		items[index].Verification.Passed = items[index].Outcome == experienceentity.OutcomeSucceeded
		payload, marshalErr := json.Marshal(items[index])
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		created, createErr := experienceStore.Create(context.Background(), &experienceentity.StoredExperience{
			Experience: items[index], Payload: string(payload), SearchText: items[index].GoalSummary,
		})
		if createErr != nil || !created {
			t.Fatalf("store experience %s: created=%v error=%v", items[index].ExperienceID, created, createErr)
		}
	}
	evaluator := experiencesvc.NewService(experienceStore, nil)
	learningStore := learningrepo.NewStore(data.New(db))
	service := NewService(learningStore, experienceStore, evaluator)

	candidate, err := service.GenerateCandidate(context.Background(), "owner-1", GenerateCandidateRequest{
		Kind: entity.CandidateSkill, ID: "browser.navigate.reviewed", ExperienceIDs: []string{
			"exp-success-0", "exp-success-1", "exp-success-2", "exp-failure-1",
		},
	})
	if err != nil {
		t.Fatalf("GenerateCandidate() real integration error = %v", err)
	}
	if !candidate.Evaluation.Passed || candidate.Evaluation.Delta <= 0 || candidate.Evaluation.SampleSize != 4 {
		t.Fatalf("candidate evaluation = %+v", candidate.Evaluation)
	}
	var runCount, fixtureCount int64
	if err := db.Model(&experiencepo.EvaluationRun{}).Count(&runCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&experiencepo.EvaluationFixture{}).Count(&fixtureCount).Error; err != nil {
		t.Fatal(err)
	}
	if runCount != 1 || fixtureCount != 4 {
		t.Fatalf("persisted replay evidence: runs=%d fixtures=%d", runCount, fixtureCount)
	}
	reviewed, err := service.ReviewCandidate(context.Background(), "owner-1", "reviewer-1", candidate.CandidateID, ReviewRequest{
		Decision: "APPROVE", Note: "Reviewed four frozen fixtures, recovery condition, risk, and confidence interval.", ExpectedRevision: candidate.Revision,
	})
	if err != nil {
		t.Fatalf("ReviewCandidate() real integration error = %v", err)
	}
	if reviewed.Status != entity.LifecycleApproved {
		t.Fatalf("reviewed candidate = %+v", reviewed)
	}
	versions := make([]learningpo.SkillVersion, 0)
	if err := db.Where("candidate_id = ?", candidate.CandidateID).Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || len(versions[0].Checksum) != 64 {
		t.Fatalf("materialized immutable versions = %+v", versions)
	}
}

func TestConfirmedDemonstrationPersistsOnlySanitizedExperience(t *testing.T) {
	dsn := fmt.Sprintf("file:learning-demonstration-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&experiencepo.Experience{}, &experiencepo.ExperiencePayload{}, &experiencepo.ExperiencePreference{},
		&experiencepo.ExperienceEventRef{}, &experiencepo.ExperienceRedaction{}, &experiencepo.FailureClassification{},
		&learningpo.Demonstration{}, &controlpo.CapabilityDefinition{}, &controlpo.Task{}, &controlpo.Action{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if err := db.Create(&controlpo.CapabilityDefinition{
		CapabilityID: "browser.navigate", OwnerID: "system", Version: "1", Operations: `["navigate"]`,
		Modalities: `[]`, InputSchema: `{}`, OutputSchema: `{}`, Risk: "R1", Enabled: true,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&controlpo.Task{
		TaskID: "task-demonstration", UserID: "owner-1", Status: "COMPLETED",
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&controlpo.Action{
		ActionID: "action-demonstration", TaskID: "task-demonstration", StepID: "step-1", DeviceID: "device-1",
		CapabilityInstanceID: "device-1:browser-navigate", UserID: "owner-1", Protocol: "athena.agent.v4",
		Sequence: 1, Revision: 1, IdempotencyKey: "task-demonstration:step-1:action-demonstration",
		IssuedAt: now, Deadline: now + 60000, Capability: "browser.navigate", Operation: "navigate",
		Arguments: `{"url":"https://docs.example.test/start","password":"must-not-persist"}`, Risk: "R1", Decision: "ALLOW",
	}).Error; err != nil {
		t.Fatal(err)
	}
	experienceStore := experiencerepo.NewStore(data.New(db))
	service := NewService(learningrepo.NewStore(data.New(db)), experienceStore, experiencesvc.NewService(experienceStore, nil))
	ctx := context.Background()
	demonstration, err := service.StartDemonstration(ctx, "owner-1", StartDemonstrationRequest{
		TaskID: "task-demonstration", Title: "Open the documentation",
	})
	if err != nil {
		t.Fatal(err)
	}
	demonstration, err = service.RecordDemonstrationStep(ctx, "owner-1", demonstration.DemonstrationID, RecordDemonstrationStepRequest{
		Capability: "browser.navigate", Operation: "navigate",
	})
	if err != nil {
		t.Fatal(err)
	}
	demonstration, err = service.PreviewDemonstration(ctx, "owner-1", demonstration.DemonstrationID)
	if err != nil {
		t.Fatal(err)
	}
	demonstration, err = service.ConfirmDemonstration(ctx, "owner-1", "owner-1", demonstration.DemonstrationID)
	if err != nil {
		t.Fatal(err)
	}
	experienceID := demonstrationExperienceID(demonstration.DemonstrationID)
	generated, err := experienceStore.Find(ctx, "owner-1", experienceID)
	if err != nil || generated == nil {
		t.Fatalf("Find() = %+v, %v", generated, err)
	}
	if generated.Intent["site_scope"] != "docs.example.test" || generated.Provenance.GeneratedBy != "human-demonstration/v2" {
		t.Fatalf("generated experience = %+v", generated)
	}
	var payload experiencepo.ExperiencePayload
	if err := db.Where("experience_id = ?", experienceID).Take(&payload).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload.Content, "must-not-persist") || strings.Contains(payload.SearchText, "must-not-persist") {
		t.Fatalf("secret escaped into persisted demonstration experience: %+v", payload)
	}
	if _, err := service.ConfirmDemonstration(ctx, "owner-1", "owner-1", demonstration.DemonstrationID); err != nil {
		t.Fatalf("idempotent confirmation error = %v", err)
	}
	var count int64
	if err := db.Model(&experiencepo.Experience{}).Where("experience_id = ?", experienceID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persisted demonstration experiences = %d", count)
	}
}
