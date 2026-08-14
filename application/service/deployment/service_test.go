package deployment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	deploymentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	deploymentrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/deployment"
)

func TestPromotionLifecycleManifestAndRollback(t *testing.T) {
	service := newDeploymentTestService(t)
	ctx := context.Background()
	baseline, err := service.EnsureBaselineBuild(ctx, "owner-1", "owner-1", "agent-1", "prompt-current")
	if err != nil {
		t.Fatalf("EnsureBaselineBuild() error = %v", err)
	}
	build, err := service.CreateBuild(ctx, "owner-1", "owner-1", CreateBuildRequest{
		AgentID: "agent-1", Version: "0.5.0", SkillVersions: map[string]string{"skill.safe": "1.0.0"},
		PromptTemplateVersions: map[string]string{"system": "prompt-v2"}, RiskLevel: entity.RiskR1,
	})
	if err != nil {
		t.Fatalf("CreateBuild() error = %v", err)
	}
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 100, Verified: true, Recoverable: true})
	if err != nil {
		t.Fatalf("ProposePromotion() error = %v", err)
	}
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	shadow, err := service.RecordShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-shadow", ProductionRisk: entity.RiskR1, CandidateRisk: entity.RiskR1, Passed: true})
	if err != nil || !shadow.NoExternalSideEffects || shadow.ExecutedActionCount != 0 {
		t.Fatalf("RecordShadow() = %+v, %v", shadow, err)
	}
	promotion = transition(t, service, promotion, entity.StatusCanary, false)
	first, err := service.Exposure(ctx, "owner-1", "agent-1")
	if err != nil {
		t.Fatalf("Exposure() error = %v", err)
	}
	second, _ := service.Exposure(ctx, "owner-1", "agent-1")
	if first.ExposureID != second.ExposureID || first.Variant != entity.VariantCandidate {
		t.Fatalf("unstable exposure first=%+v second=%+v", first, second)
	}
	if err := service.SetExperimentOptOut(ctx, "owner-1", "agent-1", true); err != nil {
		t.Fatalf("SetExperimentOptOut(true) error = %v", err)
	}
	optedOut, _ := service.Exposure(ctx, "owner-1", "agent-1")
	if !optedOut.OptedOut || optedOut.Variant != entity.VariantControl {
		t.Fatalf("opted-out exposure = %+v", optedOut)
	}
	if err := service.SetExperimentOptOut(ctx, "owner-1", "agent-1", false); err != nil {
		t.Fatalf("SetExperimentOptOut(false) error = %v", err)
	}
	optedIn, _ := service.Exposure(ctx, "owner-1", "agent-1")
	if optedIn.OptedOut || optedIn.Variant != first.Variant {
		t.Fatalf("opted-in exposure = %+v, want stable variant %s", optedIn, first.Variant)
	}
	if _, _, err := service.RecordCanaryMetric(ctx, "owner-1", promotion.PromotionID, CanaryMetricRequest{SampleCount: 20, SuccessRate: 1, P95LatencyMS: 500, AverageCostMicros: 100, SafetyScore: 1}); err != nil {
		t.Fatalf("RecordCanaryMetric() error = %v", err)
	}
	promotion = transition(t, service, promotion, entity.StatusActive, false)

	manifest, err := service.CreateRunManifest(ctx, "owner-1", RunManifestInput{
		TaskID: "task-1", AgentID: "agent-1", ModelConfigVersion: "model-v1", KnowledgeSnapshot: "knowledge-v1",
		Budget: entity.RunBudget{MaxTokens: 1000, MaxCostMicros: 1000, MaxDurationMS: 1000, MaxActions: 5},
	})
	if err != nil || manifest.AgentBuildID != build.BuildID {
		t.Fatalf("CreateRunManifest() = %+v, %v", manifest, err)
	}
	rollback, compensations, err := service.Rollback(ctx, "owner-1", "owner-1", promotion.PromotionID, RollbackRequest{
		ExpectedRevision: promotion.Revision, Reason: "canary regression",
		Compensations: []CompensationRequest{{ActionID: "external-action-1", Instructions: "verify and reverse the external change manually"}},
	})
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if rollback.ToBuildID != baseline.BuildID || len(compensations) != 1 || compensations[0].Status != "PENDING" {
		t.Fatalf("rollback=%+v compensations=%+v", rollback, compensations)
	}
	after, err := service.CreateRunManifest(ctx, "owner-1", RunManifestInput{
		TaskID: "task-2", AgentID: "agent-1", ModelConfigVersion: "model-v1", KnowledgeSnapshot: "knowledge-v1",
		Budget: entity.RunBudget{MaxTokens: 1000, MaxCostMicros: 1000, MaxDurationMS: 1000, MaxActions: 5},
	})
	if err != nil || after.AgentBuildID != baseline.BuildID {
		t.Fatalf("post-rollback manifest = %+v, %v", after, err)
	}
}

func TestR2CannotEnterAutomaticCanary(t *testing.T) {
	service := newDeploymentTestService(t)
	ctx := context.Background()
	_, _ = service.EnsureBaselineBuild(ctx, "owner-1", "owner-1", "agent-risk", "prompt-current")
	build, err := service.CreateBuild(ctx, "owner-1", "owner-1", CreateBuildRequest{AgentID: "agent-risk", Version: "0.5.0", PromptTemplateVersions: map[string]string{"system": "prompt-v2"}, RiskLevel: entity.RiskR2})
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 10, Verified: true, Recoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	if _, err := service.RecordShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-risk-shadow", ProductionRisk: entity.RiskR1, CandidateRisk: entity.RiskR2, Passed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionPromotion(ctx, "owner-1", "owner-1", promotion.PromotionID, TransitionRequest{TargetStatus: entity.StatusCanary, ExpectedRevision: promotion.Revision}); err == nil {
		t.Fatal("R2 build entered automatic canary")
	}
	if _, err := service.TransitionPromotion(ctx, "owner-1", "owner-1", promotion.PromotionID, TransitionRequest{TargetStatus: entity.StatusActive, ExpectedRevision: promotion.Revision, Explicit: true}); err != nil {
		t.Fatalf("explicit R2 activation failed: %v", err)
	}
}

func TestCanaryThresholdAutomaticallyPauses(t *testing.T) {
	service := newDeploymentTestService(t)
	ctx := context.Background()
	_, _ = service.EnsureBaselineBuild(ctx, "owner-1", "owner-1", "agent-stop", "prompt-current")
	build, err := service.CreateBuild(ctx, "owner-1", "owner-1", CreateBuildRequest{AgentID: "agent-stop", Version: "0.5.0", PromptTemplateVersions: map[string]string{"system": "prompt-v2"}, RiskLevel: entity.RiskR1})
	if err != nil {
		t.Fatal(err)
	}
	promotion, _ := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 10, Verified: true, Recoverable: true, Thresholds: entity.CanaryThresholds{MinimumSuccessRate: 0.9, MaximumP95LatencyMS: 1000, MaximumAverageCostMicros: 1000, MinimumSafetyScore: 1, MaximumInterventionRate: 0.1, MinimumSamples: 4}})
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	if _, err := service.RecordShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-stop-shadow", ProductionRisk: entity.RiskR1, CandidateRisk: entity.RiskR1, Passed: true}); err != nil {
		t.Fatal(err)
	}
	promotion = transition(t, service, promotion, entity.StatusCanary, false)
	metric, updated, err := service.RecordCanaryMetric(ctx, "owner-1", promotion.PromotionID, CanaryMetricRequest{SampleCount: 4, SuccessRate: 0.5, P95LatencyMS: 500, AverageCostMicros: 100, SafetyScore: 1})
	if err != nil || !metric.StopTriggered || updated == nil || updated.Status != entity.StatusPaused {
		t.Fatalf("metric=%+v promotion=%+v error=%v", metric, updated, err)
	}
}

func TestBuildRejectsUnapprovedArtifactVersion(t *testing.T) {
	service := newDeploymentTestService(t)
	_, err := service.CreateBuild(context.Background(), "owner-1", "owner-1", CreateBuildRequest{AgentID: "agent-1", SkillVersions: map[string]string{"skill.safe": "9.9.9"}})
	if err == nil {
		t.Fatal("unapproved skill version was accepted")
	}
}

func transition(t *testing.T, service *Service, promotion *entity.Promotion, status string, explicit bool) *entity.Promotion {
	t.Helper()
	value, err := service.TransitionPromotion(context.Background(), "owner-1", "owner-1", promotion.PromotionID, TransitionRequest{TargetStatus: status, ExpectedRevision: promotion.Revision, Explicit: explicit})
	if err != nil {
		t.Fatalf("transition %s -> %s: %v", promotion.Status, status, err)
	}
	return value
}

func newDeploymentTestService(t *testing.T) *Service {
	t.Helper()
	dsn := fmt.Sprintf("file:deployment-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&deploymentpo.AgentBuild{}, &deploymentpo.RunManifest{}, &deploymentpo.Promotion{}, &deploymentpo.Exposure{},
		&deploymentpo.ShadowResult{}, &deploymentpo.CanaryMetric{}, &deploymentpo.Rollback{}, &deploymentpo.Compensation{},
		&learningpo.Skill{}, &learningpo.Strategy{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if err := db.Create(&learningpo.Skill{SkillID: "skill.safe", OwnerID: "owner-1", LatestVersion: "1.0.0", Status: "APPROVED_FOR_USE", Visibility: "PRIVATE", Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	return NewService(deploymentrepo.NewStore(data.New(db)))
}
