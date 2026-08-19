package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

var governedLearningNow = time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)

type replayRunnerStub struct{}

func (replayRunnerStub) Run(_ context.Context, request dso.ReplayRequest) (dso.ReplayResult, error) {
	return dso.ReplayResult{Schema: dso.Schema, ReplayID: request.ReplayID, OwnerID: request.OwnerID, SourceRunRef: request.SourceRunRef, Mode: request.Mode, Status: dso.ReplayCompleted}, nil
}

type sideEffectingShadowStub struct{}

type learningConsentStub struct{ enabled bool }

func (s learningConsentStub) GetPreference(context.Context, string) (*experienceentity.Preference, error) {
	return &experienceentity.Preference{OwnerID: "owner-1", LearningEnabled: s.enabled}, nil
}

func (sideEffectingShadowStub) Evaluate(_ context.Context, _ dso.DelegationLearningCandidate, offline dso.DelegationEvaluationResult) (ShadowEvaluationOutcome, error) {
	return ShadowEvaluationOutcome{
		Baseline: offline.Baseline, Candidate: offline.Candidate, ReplayResultRefs: offline.ReplayResultRefs,
		Proof:            dso.LearningShadowProof{Mode: "PLAN_ONLY", NetworkRequests: 1, ProofDigest: strings.Repeat("a", 64)},
		EvaluatorVersion: "bad-shadow/v1",
	}, nil
}

func TestGovernedLearningRequiresReviewAndShadowBeforeExposure(t *testing.T) {
	service := newGovernedLearningTestService(t, nil)
	ctx := context.Background()
	candidate := proposePolicyCandidate(t, service, "owner-1")

	resolved, err := service.Resolve(ctx, "owner-1", dso.LearningCandidateDelegationPolicy, "low", "task-1")
	if err != nil || resolved.Source != "RULE_POLICY" || resolved.Candidate != nil {
		t.Fatalf("unpromoted resolution=%+v err=%v", resolved, err)
	}
	if _, err := service.StartCanary(ctx, CanaryRequest{OwnerID: "owner-1", CandidateID: candidate.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 10, ApprovedBy: "admin-1"}); !errors.Is(err, ErrGovernanceGateIncomplete) {
		t.Fatalf("canary without governance error=%v", err)
	}

	offline := evaluatePolicyCandidate(t, service, candidate)
	if !offline.Passed {
		t.Fatal("offline evaluation did not pass")
	}
	if _, err := service.StartCanary(ctx, CanaryRequest{OwnerID: "owner-1", CandidateID: candidate.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 10, ApprovedBy: "admin-1"}); !errors.Is(err, ErrGovernanceGateIncomplete) {
		t.Fatalf("canary without review error=%v", err)
	}
	if _, err := service.Review(ctx, "owner-1", candidate.CandidateID, "admin-1", dso.LearningReviewApprove, []string{"offline evidence passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartCanary(ctx, CanaryRequest{OwnerID: "owner-1", CandidateID: candidate.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 10, ApprovedBy: "admin-1"}); !errors.Is(err, ErrGovernanceGateIncomplete) {
		t.Fatalf("canary without shadow error=%v", err)
	}
	if _, err := service.EvaluateShadow(ctx, "owner-1", candidate.CandidateID); err != nil {
		t.Fatal(err)
	}
	rollout, err := service.StartCanary(ctx, CanaryRequest{OwnerID: "owner-1", CandidateID: candidate.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 10, ApprovedBy: "admin-1"})
	if err != nil || rollout.Status != dso.LearningRolloutCanary {
		t.Fatalf("governed canary=%+v err=%v", rollout, err)
	}
}

func TestGovernedLearningOptOutStopsCandidatesAndInstantlyFallsBack(t *testing.T) {
	service := newGovernedLearningTestService(t, nil)
	ctx := context.Background()
	preference, err := service.SetPreference(ctx, "owner-1", "owner-1", false, 0)
	if err != nil || preference.Enabled {
		t.Fatalf("preference=%+v err=%v", preference, err)
	}
	_, err = service.ProposeCandidate(ctx, learningCandidateInput("owner-1"))
	if !errors.Is(err, ErrDelegationLearningDisabled) {
		t.Fatalf("disabled candidate error=%v", err)
	}
	resolved, err := service.Resolve(ctx, "owner-1", dso.LearningCandidateDelegationPolicy, "low", "task-1")
	if err != nil || resolved.Source != "RULE_POLICY" {
		t.Fatalf("disabled resolution=%+v err=%v", resolved, err)
	}
	snapshot, err := service.Snapshot(ctx, "owner-1")
	if err != nil || len(snapshot.Candidates) != 0 {
		t.Fatalf("disabled learning still produced candidates=%d err=%v", len(snapshot.Candidates), err)
	}
}

func TestGovernedLearningHonorsGlobalLearningConsent(t *testing.T) {
	service := newGovernedLearningTestService(t, nil).WithOwnerLearningConsent(learningConsentStub{enabled: false})
	if _, err := service.ProposeCandidate(context.Background(), learningCandidateInput("owner-1")); !errors.Is(err, ErrDelegationLearningDisabled) {
		t.Fatalf("global learning opt-out error=%v", err)
	}
	resolved, err := service.Resolve(context.Background(), "owner-1", dso.LearningCandidateDelegationPolicy, "low", "task-1")
	if err != nil || resolved.Source != "RULE_POLICY" {
		t.Fatalf("global opt-out resolution=%+v err=%v", resolved, err)
	}
}

func TestGovernedEvolutionCreatesDeclarativeCandidatesWithoutActivation(t *testing.T) {
	service := newGovernedLearningTestService(t, nil)
	store := service.store.(*delegationrepo.Store)
	ctx := context.Background()
	scope := dso.ContextScope{AllowedClasses: []string{dso.ClassInternal}, MaxBytes: 2048}
	build, err := NewAdHocSpecialistBuilder().Build(AdHocBuildRequest{
		OwnerID: "owner-1", TaskStepRef: "task-evolution", DelegatedOutcomeRef: "outcome-evolution",
		RoleDescription: "evidence research specialist", RequestedCapabilities: []string{"internet.search"},
		RequestedContextScope: scope, ParentCapabilities: []string{"internet.search"}, ParentContextScope: scope,
		BudgetRequest: dso.BudgetAmount{Tokens: 1000, Queries: 3}, Now: governedLearningNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayContent, _ := json.Marshal(build.Overlay)
	admissionContent, _ := json.Marshal(build.Admission)
	if err := store.CreateAdHocAdmission(ctx, delegationentity.AdHocAdmissionBundle{
		Overlay:   delegationentity.AdHocOverlay{OverlayID: build.Overlay.OverlayID, OwnerID: build.Overlay.OwnerID, BaseProfileRef: build.Overlay.BaseProfileRef, ContentHash: build.Overlay.ContentHash, Status: delegationentity.AdHocOverlayAllowed, Content: string(overlayContent), ExpiresAt: build.Overlay.ExpiresAt, CreatedAt: build.Overlay.CreatedAt},
		Admission: delegationentity.OverlayAdmission{DecisionID: build.Admission.AdmissionDecisionID, OverlayID: build.Overlay.OverlayID, OwnerID: build.Overlay.OwnerID, Decision: build.Admission.Decision, PolicyVersion: build.Admission.PolicyVersion, InputHash: build.Admission.InputHash, Content: string(admissionContent), CreatedAt: build.Admission.DecidedAt},
		Event:     learningEvent(ctx, "owner-1", build.Overlay.OverlayID, "AdHocOverlayAdmissionDecided", 1, map[string]any{"fixture": true}, governedLearningNow),
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if err := store.RecordAdHocOutcome(ctx, delegationentity.AdHocRunOutcome{OutcomeID: fmt.Sprintf("outcome-%d", index), OverlayID: build.Overlay.OverlayID, OwnerID: "owner-1", RunID: fmt.Sprintf("run-%d", index), Status: delegationentity.AdHocOutcomeSuccess, EvidenceRefs: fmt.Sprintf("evidence-%d", index), CreatedAt: governedLearningNow.Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if candidate, err := maybeCreateProfileCandidate(ctx, store, build.Overlay, governedLearningNow.Add(time.Minute)); err != nil || candidate == nil {
		t.Fatalf("source profile candidate=%+v err=%v", candidate, err)
	}
	evolution := NewGovernedLearningEvolution(service, store, GovernedEvolutionConfig{Enabled: true, BatchSize: 10})
	snapshot, err := evolution.ScanOnce(ctx)
	if err != nil || snapshot.CreatedCandidates != 2 {
		t.Fatalf("evolution snapshot=%+v err=%v", snapshot, err)
	}
	learningSnapshot, err := service.Snapshot(ctx, "owner-1")
	if err != nil || len(learningSnapshot.Candidates) != 2 {
		t.Fatalf("learning candidates=%d err=%v", len(learningSnapshot.Candidates), err)
	}
	for _, record := range learningSnapshot.Candidates {
		var candidate dso.DelegationLearningCandidate
		if err := json.Unmarshal([]byte(record.Content), &candidate); err != nil || candidate.ActivationAllowed {
			t.Fatalf("candidate=%+v err=%v", candidate, err)
		}
	}
	repeat, err := evolution.ScanOnce(ctx)
	if err != nil || repeat.CreatedCandidates != 0 {
		t.Fatalf("idempotent evolution snapshot=%+v err=%v", repeat, err)
	}
}

func TestGovernedLearningRejectsShadowExternalEffects(t *testing.T) {
	service := newGovernedLearningTestService(t, sideEffectingShadowStub{})
	candidate := proposePolicyCandidate(t, service, "owner-1")
	evaluatePolicyCandidate(t, service, candidate)
	if _, err := service.Review(context.Background(), "owner-1", candidate.CandidateID, "admin-1", dso.LearningReviewApprove, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EvaluateShadow(context.Background(), "owner-1", candidate.CandidateID); err == nil || !strings.Contains(err.Error(), "zero external side effects") {
		t.Fatalf("side-effecting shadow error=%v", err)
	}
}

func TestGovernedLearningCanaryCohortPromotionAndRegressionRollback(t *testing.T) {
	service := newGovernedLearningTestService(t, nil)
	candidate := fullyGovernedCandidate(t, service, "owner-1")
	rollout, err := service.StartCanary(context.Background(), CanaryRequest{OwnerID: "owner-1", CandidateID: candidate.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 25, ApprovedBy: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := taskInsideCanary("owner-1", rollout.RolloutID, 25)
	resolved, err := service.Resolve(context.Background(), "owner-1", candidate.Kind, "low", taskID)
	if err != nil || resolved.Source != dso.LearningRolloutCanary || resolved.Candidate == nil {
		t.Fatalf("canary resolution=%+v err=%v", resolved, err)
	}
	highRisk, err := service.Resolve(context.Background(), "owner-1", candidate.Kind, "high", taskID)
	if err != nil || highRisk.Source != "RULE_POLICY" {
		t.Fatalf("high-risk canary resolution=%+v err=%v", highRisk, err)
	}

	passing := benchmarkReport(true)
	if _, err := service.RecordBenchmark(context.Background(), "owner-1", rollout.RolloutID, passing); err != nil {
		t.Fatal(err)
	}
	promoted, err := service.Promote(context.Background(), "owner-1", rollout.RolloutID, "admin-1")
	if err != nil || promoted.Status != dso.LearningRolloutPromoted {
		t.Fatalf("promoted=%+v err=%v", promoted, err)
	}
	resolved, err = service.Resolve(context.Background(), "owner-1", candidate.Kind, "medium", "outside-cohort")
	if err != nil || resolved.Source != dso.LearningRolloutPromoted {
		t.Fatalf("promoted resolution=%+v err=%v", resolved, err)
	}

	second := fullyGovernedCandidate(t, service, "owner-1")
	secondRollout, err := service.StartCanary(context.Background(), CanaryRequest{OwnerID: "owner-1", CandidateID: second.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 25, ApprovedBy: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	before := service.now()
	rolledBack, err := service.RecordBenchmark(context.Background(), "owner-1", secondRollout.RolloutID, benchmarkReport(false))
	if err != nil || rolledBack.Status != dso.LearningRolloutRolledBack || rolledBack.UpdatedAt.Sub(before) >= time.Minute {
		t.Fatalf("rollback=%+v err=%v", rolledBack, err)
	}
}

func TestPromotedLearningArtifactCanEnterAgentBuild(t *testing.T) {
	service := newGovernedLearningTestService(t, nil)
	candidate := fullyGovernedCandidate(t, service, "owner-1")
	rollout, err := service.StartCanary(context.Background(), CanaryRequest{
		OwnerID: "owner-1", CandidateID: candidate.CandidateID,
		AllowedOwnerIDs: []string{"owner-1"}, Percent: 10, ApprovedBy: "admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactID, version := learningArtifactIdentity(candidate)
	if _, err := service.ResolveBuildApprovals(context.Background(), "owner-1", map[string]string{artifactID: version}, nil); !errors.Is(err, ErrGovernanceGateIncomplete) {
		t.Fatalf("canary artifact entered AgentBuild: %v", err)
	}
	if _, err := service.RecordBenchmark(context.Background(), "owner-1", rollout.RolloutID, benchmarkReport(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Promote(context.Background(), "owner-1", rollout.RolloutID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	approvals, err := service.ResolveBuildApprovals(context.Background(), "owner-1", map[string]string{artifactID: version}, nil)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("promoted approvals=%+v err=%v", approvals, err)
	}
	approval := approvals[0]
	if approval.Kind != dso.LearningCandidateDelegationPolicy || approval.ArtifactID != artifactID || approval.Version != version || approval.VersionID != rollout.RolloutID || approval.CandidateID != candidate.CandidateID || !approval.Verified {
		t.Fatalf("approval lineage=%+v", approval)
	}
	if _, err := service.Disable(context.Background(), "owner-1", rollout.RolloutID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveBuildApprovals(context.Background(), "owner-1", map[string]string{artifactID: version}, nil); !errors.Is(err, ErrGovernanceGateIncomplete) {
		t.Fatalf("disabled artifact remained build eligible: %v", err)
	}
}

func TestExecutionRouteConsumesOnlyPromotedGovernedPolicy(t *testing.T) {
	service := newGovernedLearningTestService(t, nil)
	candidate := fullyGovernedCandidate(t, service, "owner-1")
	rollout, err := service.StartCanary(context.Background(), CanaryRequest{OwnerID: "owner-1", CandidateID: candidate.CandidateID, AllowedOwnerIDs: []string{"owner-1"}, Percent: 25, ApprovedBy: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordBenchmark(context.Background(), "owner-1", rollout.RolloutID, benchmarkReport(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Promote(context.Background(), "owner-1", rollout.RolloutID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	execution := &ExecutionService{learning: service}
	input := &runtimeentity.RunInput{Prompt: "Research the MCP protocol", Context: map[string]any{"user_id": "owner-1", "task_id": "route-task"}}
	decision := execution.applyGovernedLearningRoute(context.Background(), input, RouteDecision{Route: RouteFastPath, Reasons: []string{"rule_baseline"}})
	if decision.Route != RouteSpecialist || !strings.Contains(strings.Join(decision.Reasons, ","), candidate.CandidateID) {
		t.Fatalf("governed route=%+v", decision)
	}
	direct := &runtimeentity.RunInput{Prompt: "open youtube", Context: map[string]any{"user_id": "owner-1", "task_id": "direct-task"}}
	decision = execution.applyGovernedLearningRoute(context.Background(), direct, RouteDecision{Route: RouteFastPath})
	if decision.Route != RouteFastPath {
		t.Fatalf("direct device action was incorrectly delegated: %+v", decision)
	}
}

func newGovernedLearningTestService(t *testing.T, shadow PlanOnlyShadowEvaluator) *GovernedLearningService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&po.LearningPreference{}, &po.LearningCandidate{}, &po.LearningEvaluation{}, &po.LearningReview{}, &po.LearningRollout{}, &po.LearningBenchmark{}, &po.AdHocOverlay{}, &po.OverlayAdmission{}, &po.AdHocRunOutcome{}, &po.ProfileCandidate{}, &po.Event{}); err != nil {
		t.Fatal(err)
	}
	service := NewGovernedLearningService(delegationrepo.NewStore(data.New(db)), replayRunnerStub{}, shadow)
	clock := governedLearningNow
	service.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return service
}

func learningCandidateInput(ownerID string) LearningCandidateInput {
	suffix := ulidForTest()
	return LearningCandidateInput{
		OwnerID: ownerID, Kind: dso.LearningCandidateDelegationPolicy,
		SourceExperienceRefs: []string{"experience-" + suffix + "-1", "experience-" + suffix + "-2", "experience-" + suffix + "-3"},
		SourceRunRefs:        []string{"run-" + suffix + "-1", "run-" + suffix + "-2", "run-" + suffix + "-3"},
		PolicyArtifact: &dso.DelegationPolicyArtifact{
			ArtifactID: "policy-" + suffix, Version: "v1", DefaultFallbackRef: dso.RulePolicyFallbackRef,
			Rules: []dso.DelegationPolicyRule{{RuleID: "research", TaskClasses: []string{"research"}, RequiredCapabilities: []string{"internet.search"}, RecommendedProfileRef: "specialist-profile://research/v1", RiskCeiling: "low", MinimumComplexity: 0.6, MaximumParallelism: 3, BudgetMultiplier: 1.2}},
		},
	}
}

func proposePolicyCandidate(t *testing.T, service *GovernedLearningService, ownerID string) dso.DelegationLearningCandidate {
	t.Helper()
	candidate, err := service.ProposeCandidate(context.Background(), learningCandidateInput(ownerID))
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func evaluatePolicyCandidate(t *testing.T, service *GovernedLearningService, candidate dso.DelegationLearningCandidate) dso.DelegationEvaluationResult {
	t.Helper()
	replays := make([]dso.ReplayRequest, 3)
	for index := range replays {
		replays[index] = dso.ReplayRequest{ReplayID: fmt.Sprintf("replay-%s-%d", candidate.CandidateID, index), SourceRunRef: candidate.SourceRunRefs[index], Mode: dso.ReplayExactConfig}
	}
	evaluation, err := service.EvaluateOffline(context.Background(), OfflineEvaluationRequest{
		OwnerID: candidate.OwnerID, CandidateID: candidate.CandidateID, Replays: replays,
		Baseline: learningMetrics(0.70, 0.99, 0.80, 1200, 120), Candidate: learningMetrics(0.82, 0.99, 0.88, 1000, 105),
	})
	if err != nil {
		t.Fatal(err)
	}
	return evaluation
}

func fullyGovernedCandidate(t *testing.T, service *GovernedLearningService, ownerID string) dso.DelegationLearningCandidate {
	t.Helper()
	candidate := proposePolicyCandidate(t, service, ownerID)
	evaluatePolicyCandidate(t, service, candidate)
	if _, err := service.Review(context.Background(), ownerID, candidate.CandidateID, "admin-1", dso.LearningReviewApprove, []string{"approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EvaluateShadow(context.Background(), ownerID, candidate.CandidateID); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func benchmarkReport(passing bool) dso.DelegationBenchmarkReport {
	static := learningMetrics(0.80, 0.99, 0.85, 1000, 100)
	dynamic := learningMetrics(0.84, 0.99, 0.90, 950, 95)
	if !passing {
		dynamic.QualityScore = 0.70
		dynamic.SafetyScore = 0.90
	}
	return dso.DelegationBenchmarkReport{
		Variants: []dso.DelegationBenchmarkVariant{
			{Mode: dso.BenchmarkSingleAgent, Metrics: learningMetrics(0.70, 0.99, 0.80, 1200, 120)},
			{Mode: dso.BenchmarkStaticSpecialist, Metrics: static},
			{Mode: dso.BenchmarkDynamicDSO, Metrics: dynamic},
		},
		SafetyPassed: passing, PrimaryImprovement: "quality_score",
	}
}

func learningMetrics(quality, safety, recovery float64, latency, cost int64) dso.DelegationBenchmarkMetrics {
	return dso.DelegationBenchmarkMetrics{SampleCount: 10, QualityScore: quality, SafetyScore: safety, RecoveryRate: recovery, P95LatencyMS: latency, AverageCostMicros: cost}
}

func taskInsideCanary(ownerID, rolloutID string, percent int) string {
	for index := 0; ; index++ {
		candidate := fmt.Sprintf("task-%d", index)
		if stableLearningBucket(ownerID+"|"+candidate+"|"+rolloutID) < percent {
			return candidate
		}
	}
}

var testULIDCounter int

func ulidForTest() string {
	testULIDCounter++
	return fmt.Sprintf("%03d", testULIDCounter)
}
