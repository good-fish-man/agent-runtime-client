package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	controlpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	deploymentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	experiencepo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/experience"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	deploymentrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/deployment"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
)

func TestPromotionLifecycleManifestAndRollback(t *testing.T) {
	service, db := newDeploymentTestServiceWithDB(t)
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
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{
		BuildID: build.BuildID, CanaryPercent: 100,
		Thresholds: entity.CanaryThresholds{MinimumSuccessRate: 0.9, MaximumP95LatencyMS: 1000, MaximumAverageCostMicros: 1000, MinimumSafetyScore: 1, MaximumInterventionRate: 0.1, MinimumSamples: 1},
	})
	if err != nil {
		t.Fatalf("ProposePromotion() error = %v", err)
	}
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	shadow, err := service.EvaluateShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-shadow", Input: map[string]any{"goal": "verify release"}})
	if err != nil || !shadow.NoExternalSideEffects || shadow.ExecutedActionCount != 0 {
		t.Fatalf("EvaluateShadow() = %+v, %v", shadow, err)
	}
	if shadow.ProductionBuildID != baseline.BuildID || shadow.CandidateBuildID != build.BuildID || shadow.InputDigest == "" || len(shadow.Checks) == 0 {
		t.Fatalf("shadow provenance is incomplete: %+v", shadow)
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
	canaryManifest, err := service.CreateRunManifest(ctx, "owner-1", RunManifestInput{
		TaskID: "task-canary-1", AgentID: "agent-1", ModelConfigVersion: "model-v1", KnowledgeSnapshot: "knowledge-v1",
		Budget: entity.RunBudget{MaxTokens: 1000, MaxCostMicros: 1000, MaxDurationMS: 1000, MaxActions: 5},
	})
	if err != nil || canaryManifest.AgentBuildID != build.BuildID || canaryManifest.ExposureID == "" {
		t.Fatalf("canary manifest = %+v, %v", canaryManifest, err)
	}
	if _, _, err := service.RecordRunOutcome(ctx, "owner-1", canaryManifest.ManifestID, RunOutcome{Succeeded: true, LatencyMS: 500, CostMicros: 100, SafetyScore: 1}); err != nil {
		t.Fatalf("RecordRunOutcome() error = %v", err)
	}
	if _, _, err := service.RecordRunOutcome(ctx, "owner-1", canaryManifest.ManifestID, RunOutcome{Succeeded: true, LatencyMS: 500, CostMicros: 100, SafetyScore: 1}); err == nil {
		t.Fatal("duplicate run manifest produced two canary samples")
	}
	promotion = transition(t, service, promotion, entity.StatusActive, false)

	manifest, err := service.CreateRunManifest(ctx, "owner-1", RunManifestInput{
		TaskID: "task-1", AgentID: "agent-1", ModelConfigVersion: "model-v1", KnowledgeSnapshot: "knowledge-v1",
		Budget: entity.RunBudget{MaxTokens: 1000, MaxCostMicros: 1000, MaxDurationMS: 1000, MaxActions: 5},
	})
	if err != nil || manifest.AgentBuildID != build.BuildID {
		t.Fatalf("CreateRunManifest() = %+v, %v", manifest, err)
	}
	now := time.Now().UTC().UnixMilli()
	if err := db.Create(&controlpo.Task{TaskID: "preserved-task", UserID: "owner-1", Status: "COMPLETED", Revision: 1, Metadata: `{}`, Result: `{}`, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&controlpo.WorldState{TaskID: "preserved-task", Revision: 1, State: `{"page":"ready"}`, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&experiencepo.Experience{ExperienceID: "preserved-experience", OwnerID: "owner-1", TaskID: "preserved-task", Schema: "athena.experience.v1", Status: "CAPTURED", Outcome: "SUCCESS", Sensitivity: "INTERNAL", RetentionDays: 30, DeleteAt: now + 86400000, TraceID: "trace-preserved", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
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
	for label, query := range map[string]*gorm.DB{
		"task":       db.Model(&controlpo.Task{}).Where("task_id = ?", "preserved-task"),
		"world":      db.Model(&controlpo.WorldState{}).Where("task_id = ?", "preserved-task"),
		"experience": db.Model(&experiencepo.Experience{}).Where("experience_id = ?", "preserved-experience"),
	} {
		var count int64
		if err := query.Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("rollback did not preserve %s: count=%d error=%v", label, count, err)
		}
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
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	if _, err := service.EvaluateShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-risk-shadow", Input: map[string]any{"goal": "risk gate"}}); err != nil {
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
	promotion, _ := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 100, Thresholds: entity.CanaryThresholds{MinimumSuccessRate: 0.9, MaximumP95LatencyMS: 1000, MaximumAverageCostMicros: 1000, MinimumSafetyScore: 1, MaximumInterventionRate: 0.1, MinimumSamples: 4}})
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	if _, err := service.EvaluateShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-stop-shadow", Input: map[string]any{"goal": "stop gate"}}); err != nil {
		t.Fatal(err)
	}
	promotion = transition(t, service, promotion, entity.StatusCanary, false)
	var metric *entity.CanaryMetric
	var updated *entity.Promotion
	started := time.Now()
	for index := 0; index < 4; index++ {
		manifest, manifestErr := service.CreateRunManifest(ctx, "owner-1", RunManifestInput{
			TaskID: fmt.Sprintf("task-stop-%d", index), AgentID: "agent-stop", ModelConfigVersion: "model-v1", KnowledgeSnapshot: "knowledge-v1",
			Budget: entity.RunBudget{MaxTokens: 1000, MaxCostMicros: 1000, MaxDurationMS: 1000, MaxActions: 5},
		})
		if manifestErr != nil {
			t.Fatal(manifestErr)
		}
		metric, updated, err = service.RecordCanarySample(ctx, "owner-1", promotion.PromotionID, CanarySampleRequest{ManifestID: manifest.ManifestID, Succeeded: index < 3, LatencyMS: 500, CostMicros: 100, SafetyScore: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err != nil || !metric.StopTriggered || updated == nil || updated.Status != entity.StatusPaused {
		t.Fatalf("metric=%+v promotion=%+v error=%v", metric, updated, err)
	}
	if elapsed := time.Since(started); elapsed >= time.Minute {
		t.Fatalf("canary stop exceeded one-minute gate: %v", elapsed)
	}
}

func TestBuildRejectsUnapprovedArtifactVersion(t *testing.T) {
	service := newDeploymentTestService(t)
	_, err := service.CreateBuild(context.Background(), "owner-1", "owner-1", CreateBuildRequest{AgentID: "agent-1", SkillVersions: map[string]string{"skill.safe": "9.9.9"}})
	if err == nil {
		t.Fatal("unapproved skill version was accepted")
	}
}

func TestResolveRuntimeArtifactsLoadsExactReviewedDefinition(t *testing.T) {
	service, db := newDeploymentTestServiceWithDB(t)
	definition := learningv2.SkillDefinition{
		ID: "skill.safe", Version: "1.0.0", Description: "Use an already registered browser capability.",
		OwnerID: "owner-1", Visibility: learningv2.VisibilityPrivate, LifecycleState: learningv2.LifecycleApproved,
		RequiredCapabilities: []string{"browser.open"},
		TaskGraphTemplate:    learningv2.TaskGraphTemplate{Steps: []learningv2.TaskStep{{ID: "step-1", Capability: "browser.open", Operation: "open"}}},
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(encoded))
	if err := db.Model(&learningpo.SkillVersion{}).Where("version_id = ?", "skill-version-1").Updates(map[string]any{"definition": string(encoded), "checksum": checksum}).Error; err != nil {
		t.Fatal(err)
	}
	build, err := service.CreateBuild(context.Background(), "owner-1", "owner-1", CreateBuildRequest{
		AgentID: "agent-artifacts", Version: "0.5.0", SkillVersions: map[string]string{"skill.safe": "1.0.0"},
		PromptTemplateVersions: map[string]string{"system": "prompt-v1"}, RiskLevel: entity.RiskR1,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := entity.RunManifest{
		Schema: entity.Schema, ManifestID: "manifest-artifacts", TaskID: "task-artifacts", OwnerID: "owner-1", AgentID: build.AgentID,
		AgentBuildID: build.BuildID, ModelConfigVersion: "model-v1", UserScope: "owner-1", KnowledgeSnapshot: "knowledge-v1",
		Budget: entity.RunBudget{MaxTokens: 1000, MaxDurationMS: 30000, MaxActions: 5}, BuildChecksum: build.Checksum, CreatedAt: time.Now().UTC(),
	}
	bundle, err := service.ResolveRuntimeArtifacts(context.Background(), "owner-1", manifest)
	if err != nil {
		t.Fatalf("ResolveRuntimeArtifacts() error = %v", err)
	}
	if len(bundle.Skills) != 1 || bundle.Skills[0].Definition.ID != "skill.safe" || bundle.Skills[0].Reference.VersionID != "skill-version-1" {
		t.Fatalf("bundle = %+v", bundle)
	}

	if err := db.Model(&learningpo.SkillVersion{}).Where("version_id = ?", "skill-version-1").Update("definition", `{"id":"tampered"}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveRuntimeArtifacts(context.Background(), "owner-1", manifest); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("tampered artifact error = %v", err)
	}
}

func TestBuildResolvesTeamArtifactOnlyInsideAuthenticatedOrganization(t *testing.T) {
	service, db := newDeploymentTestServiceWithDB(t)
	now := time.Now().UTC().UnixMilli()
	evaluation, _ := json.Marshal(learningv2.EvaluationSummary{RunID: "evaluation-team-1", SampleSize: 10, SuccessRate: 1, SafetyScore: 1, Passed: true})
	if err := db.Create(&learningpo.Candidate{
		CandidateID: "candidate-team-skill", OwnerID: "owner-team", Kind: learningv2.CandidateSkill,
		Status: learningv2.LifecycleApproved, Definition: `{}`, Evidence: `{}`, Evaluation: string(evaluation),
		ReviewedBy: "reviewer-team", ReviewedAt: now, Revision: 2, TraceID: "trace-team", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&learningpo.Skill{
		SkillID: "skill.team", OwnerID: "owner-team", OrganizationID: "org-1", LatestVersion: "1.0.0",
		Status: learningv2.LifecycleApproved, Visibility: learningv2.VisibilityTeam, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&learningpo.SkillVersion{
		VersionID: "skill-team-version-1", SkillID: "skill.team", Version: "1.0.0", OwnerID: "owner-team",
		CandidateID: "candidate-team-skill", Definition: `{}`, Checksum: strings.Repeat("b", 64), TraceID: "trace-team", CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	request := CreateBuildRequest{AgentID: "agent-team", Version: "0.5.0", SkillVersions: map[string]string{"skill.team": "1.0.0"}, PromptTemplateVersions: map[string]string{"system": "prompt-v1"}, RiskLevel: entity.RiskR1}
	teamContext := authctx.WithOrganizationID(context.Background(), "org-1")
	build, err := service.CreateBuild(teamContext, "owner-member", "owner-member", request)
	if err != nil || len(build.ArtifactApprovals) != 1 || build.ArtifactApprovals[0].ArtifactID != "skill.team" {
		t.Fatalf("same-organization build = %+v, %v", build, err)
	}
	otherContext := authctx.WithOrganizationID(context.Background(), "org-2")
	if _, err := service.CreateBuild(otherContext, "owner-outsider", "owner-outsider", request); err == nil {
		t.Fatal("cross-organization TEAM artifact was accepted")
	}
}

func TestExperimentOptOutPersistsAcrossPromotions(t *testing.T) {
	service := newDeploymentTestService(t)
	ctx := context.Background()
	_, _ = service.EnsureBaselineBuild(ctx, "owner-1", "owner-1", "agent-optout", "prompt-current")
	first := prepareCanaryPromotion(t, service, "agent-optout", "0.5.1")
	if _, err := service.Exposure(ctx, "owner-1", "agent-optout"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetExperimentOptOut(ctx, "owner-1", "agent-optout", true); err != nil {
		t.Fatal(err)
	}
	first = transition(t, service, first, entity.StatusPaused, false)
	_ = transition(t, service, first, entity.StatusRetired, false)
	_ = prepareCanaryPromotion(t, service, "agent-optout", "0.5.2")
	exposure, err := service.Exposure(ctx, "owner-1", "agent-optout")
	if err != nil || !exposure.OptedOut || exposure.Variant != entity.VariantControl {
		t.Fatalf("new promotion ignored user opt-out: %+v, %v", exposure, err)
	}
}

func TestRollbackRejectsUndeployedPromotion(t *testing.T) {
	service := newDeploymentTestService(t)
	ctx := context.Background()
	_, _ = service.EnsureBaselineBuild(ctx, "owner-1", "owner-1", "agent-invalid-rollback", "prompt-current")
	build, err := service.CreateBuild(ctx, "owner-1", "owner-1", CreateBuildRequest{AgentID: "agent-invalid-rollback", Version: "0.5.0", PromptTemplateVersions: map[string]string{"system": "prompt-v2"}, RiskLevel: entity.RiskR1})
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Rollback(ctx, "owner-1", "owner-1", promotion.PromotionID, RollbackRequest{ExpectedRevision: promotion.Revision}); err == nil {
		t.Fatal("PROPOSED promotion was rolled back")
	}
}

func TestShadowEvaluatorCannotHideExternalEffects(t *testing.T) {
	base := newDeploymentTestService(t)
	service := NewServiceWithShadowEvaluator(base.store, effectfulShadowEvaluator{})
	ctx := context.Background()
	_, _ = service.EnsureBaselineBuild(ctx, "owner-1", "owner-1", "agent-shadow", "prompt-current")
	build, err := service.CreateBuild(ctx, "owner-1", "owner-1", CreateBuildRequest{AgentID: "agent-shadow", Version: "0.5.0", PromptTemplateVersions: map[string]string{"system": "prompt-v2"}, RiskLevel: entity.RiskR1})
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	if _, err := service.EvaluateShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "task-effect", Input: map[string]any{"goal": "must remain read only"}}); err == nil {
		t.Fatal("effectful shadow evaluator was accepted")
	}
	items, err := service.ListShadowResults(ctx, "owner-1", promotion.PromotionID, 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("unsafe shadow was persisted: %+v, %v", items, err)
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

func prepareCanaryPromotion(t *testing.T, service *Service, agentID, version string) *entity.Promotion {
	t.Helper()
	ctx := context.Background()
	build, err := service.CreateBuild(ctx, "owner-1", "owner-1", CreateBuildRequest{AgentID: agentID, Version: version, PromptTemplateVersions: map[string]string{"system": "prompt-" + version}, RiskLevel: entity.RiskR1})
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := service.ProposePromotion(ctx, "owner-1", CreatePromotionRequest{BuildID: build.BuildID, CanaryPercent: 100})
	if err != nil {
		t.Fatal(err)
	}
	promotion = transition(t, service, promotion, entity.StatusReviewed, false)
	promotion = transition(t, service, promotion, entity.StatusShadow, false)
	if _, err := service.EvaluateShadow(ctx, "owner-1", promotion.PromotionID, ShadowRequest{TaskID: "shadow-" + version, Input: map[string]any{"goal": "validate " + version}}); err != nil {
		t.Fatal(err)
	}
	return transition(t, service, promotion, entity.StatusCanary, false)
}

func newDeploymentTestService(t *testing.T) *Service {
	t.Helper()
	service, _ := newDeploymentTestServiceWithDB(t)
	return service
}

func newDeploymentTestServiceWithDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:deployment-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&deploymentpo.AgentBuild{}, &deploymentpo.RunManifest{}, &deploymentpo.Promotion{}, &deploymentpo.Exposure{},
		&deploymentpo.ShadowResult{}, &deploymentpo.CanaryMetric{}, &deploymentpo.CanarySample{}, &deploymentpo.Rollback{}, &deploymentpo.Compensation{},
		&learningpo.Candidate{}, &learningpo.Skill{}, &learningpo.SkillVersion{}, &learningpo.Strategy{}, &learningpo.StrategyVersion{},
		&controlpo.Task{}, &controlpo.WorldState{}, &experiencepo.Experience{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	evaluation, _ := json.Marshal(learningv2.EvaluationSummary{RunID: "evaluation-run-1", SampleSize: 10, SuccessRate: 1, SafetyScore: 1, Passed: true})
	if err := db.Create(&learningpo.Candidate{
		CandidateID: "candidate-skill-safe", OwnerID: "owner-1", Kind: learningv2.CandidateSkill,
		Status: learningv2.LifecycleApproved, Definition: `{}`, Evidence: `{}`, Evaluation: string(evaluation),
		ReviewedBy: "reviewer-1", ReviewedAt: now, Revision: 2, TraceID: "trace-learning-1", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&learningpo.Skill{SkillID: "skill.safe", OwnerID: "owner-1", LatestVersion: "1.0.0", Status: "APPROVED_FOR_USE", Visibility: "PRIVATE", Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&learningpo.SkillVersion{
		VersionID: "skill-version-1", SkillID: "skill.safe", Version: "1.0.0", OwnerID: "owner-1",
		CandidateID: "candidate-skill-safe", Definition: `{}`, Checksum: strings.Repeat("a", 64), TraceID: "trace-learning-1", CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return NewService(deploymentrepo.NewStore(data.New(db))), db
}

type effectfulShadowEvaluator struct{}

func (effectfulShadowEvaluator) Version() string { return "effectful-test" }

func (effectfulShadowEvaluator) Evaluate(_ context.Context, input ShadowEvaluationInput) (ShadowPlan, error) {
	return ShadowPlan{
		Route: []string{"route"}, Graph: []string{"graph"}, PlannedActions: []string{"action"},
		EstimatedCostMicros: 10, RiskLevel: input.Build.RiskLevel,
		Effects: ShadowEffectCounters{NetworkRequests: 1},
	}, nil
}
