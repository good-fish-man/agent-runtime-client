package delegation

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

var replayNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

type liveReplayStub struct{ calls atomic.Int64 }

func (s *liveReplayStub) Reexecute(context.Context, entity.ReplaySource, dso.ReplayRequest) (LiveReplayOutcome, error) {
	s.calls.Add(1)
	return LiveReplayOutcome{VerificationRefs: []string{"verification-live"}, SideEffects: true}, nil
}

func TestReplayRunnerSeparatesExactSimulationAndApprovedLiveModes(t *testing.T) {
	store, db := newReplayStore(t)
	manifest := persistReplaySource(t, store, db, "owner-a", "run-a", "observation-a")
	live := &liveReplayStub{}
	runner := NewReplayRunner(store, live)
	runner.now = func() time.Time { return replayNow }

	exact := replayRequest("replay-exact", "owner-a", "run-a", manifest, dso.ReplayExactConfig, nil)
	result, err := runner.Run(context.Background(), exact)
	if err != nil || result.Status != dso.ReplayCompleted || result.LiveSideEffects || live.calls.Load() != 0 || len(result.ArtifactBindings) != 10 {
		t.Fatalf("exact replay result=%+v calls=%d err=%v", result, live.calls.Load(), err)
	}

	simulation := replayRequest("replay-simulation", "owner-a", "run-a", manifest, dso.ReplayRecordedObservationSimulation, []string{"observation-a"})
	result, err = runner.Run(context.Background(), simulation)
	if err != nil || result.Status != dso.ReplayCompleted || result.LiveSideEffects || live.calls.Load() != 0 {
		t.Fatalf("simulation result=%+v calls=%d err=%v", result, live.calls.Load(), err)
	}

	liveRequest := replayRequest("replay-live", "owner-a", "run-a", manifest, dso.ReplayLiveReexecution, nil)
	liveRequest.LiveApprovalRef = "approval-reviewed-1"
	result, err = runner.Run(context.Background(), liveRequest)
	if err != nil || result.Status != dso.ReplayCompleted || !result.LiveSideEffects || live.calls.Load() != 1 {
		t.Fatalf("live replay result=%+v calls=%d err=%v", result, live.calls.Load(), err)
	}
	duplicate, err := runner.Run(context.Background(), liveRequest)
	if err != nil || duplicate.ContentHash != result.ContentHash || live.calls.Load() != 1 {
		t.Fatalf("duplicate live replay result=%+v calls=%d err=%v", duplicate, live.calls.Load(), err)
	}
}

func TestLiveReplayCrashAfterExternalCallRequiresReconciliation(t *testing.T) {
	store, db := newReplayStore(t)
	manifest := persistReplaySource(t, store, db, "owner-a", "run-live-crash", "observation-live-crash")
	request := replayRequest("replay-live-crash", "owner-a", "run-live-crash", manifest, dso.ReplayLiveReexecution, nil)
	request.LiveApprovalRef = "approval-reviewed-live-crash"
	live := &liveReplayStub{}
	var injected atomic.Bool
	runner := NewReplayRunner(store, live).WithFaultInjector(ReplayFaultInjectorFunc(func(_ context.Context, point string) error {
		if point == FaultAfterLiveExecution && injected.CompareAndSwap(false, true) {
			return errInjectedReplayFault
		}
		return nil
	}))
	runner.now = func() time.Time { return replayNow }
	if _, err := runner.Run(context.Background(), request); !errors.Is(err, errInjectedReplayFault) || live.calls.Load() != 1 {
		t.Fatalf("live crash calls=%d err=%v", live.calls.Load(), err)
	}
	if _, err := runner.Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "reconciliation") || live.calls.Load() != 1 {
		t.Fatalf("live retry was not fenced calls=%d err=%v", live.calls.Load(), err)
	}
}

func TestReplayRunnerRecoversIdempotentlyAfterPersistedCrash(t *testing.T) {
	store, db := newReplayStore(t)
	manifest := persistReplaySource(t, store, db, "owner-a", "run-crash", "observation-crash")
	request := replayRequest("replay-crash", "owner-a", "run-crash", manifest, dso.ReplayExactConfig, nil)
	var injected atomic.Bool
	runner := NewReplayRunner(store, nil).WithFaultInjector(ReplayFaultInjectorFunc(func(_ context.Context, point string) error {
		if point == FaultAfterReplayPersist && injected.CompareAndSwap(false, true) {
			return errInjectedReplayFault
		}
		return nil
	}))
	runner.now = func() time.Time { return replayNow }
	if _, err := runner.Run(context.Background(), request); !errors.Is(err, errInjectedReplayFault) {
		t.Fatalf("persisted crash error=%v", err)
	}
	record, err := store.FindReplay(context.Background(), "owner-a", request.ReplayID)
	if err != nil || record == nil || record.Status != dso.ReplayRunning {
		t.Fatalf("persisted replay after crash=%+v err=%v", record, err)
	}
	result, err := runner.Run(context.Background(), request)
	if err != nil || result.Status != dso.ReplayCompleted {
		t.Fatalf("idempotent replay recovery result=%+v err=%v", result, err)
	}
}

func TestReplayRunnerRejectsCrossOwnerAndForeignObservation(t *testing.T) {
	store, db := newReplayStore(t)
	manifest := persistReplaySource(t, store, db, "owner-a", "run-owner", "observation-owner")
	runner := NewReplayRunner(store, nil)
	runner.now = func() time.Time { return replayNow }
	crossOwner := replayRequest("replay-cross", "owner-b", "run-owner", manifest, dso.ReplayExactConfig, nil)
	if _, err := runner.Run(context.Background(), crossOwner); err == nil {
		t.Fatal("cross-owner replay source was exposed")
	}
	foreignObservation := replayRequest("replay-foreign-observation", "owner-a", "run-owner", manifest, dso.ReplayRecordedObservationSimulation, []string{"observation-foreign"})
	result, err := runner.Run(context.Background(), foreignObservation)
	if err == nil || result.Status != dso.ReplayFailed || result.LiveSideEffects {
		t.Fatalf("foreign observation result=%+v err=%v", result, err)
	}
}

func TestSchedulerLeaseFencesOldOwnerAndFailsOverWithinThirtySeconds(t *testing.T) {
	store, _ := newReplayStore(t)
	ctx := context.Background()
	first, owned, err := store.AcquireSchedulerLease(ctx, "dso-scheduler", "instance-a", replayNow, 20*time.Second)
	if err != nil || !owned || first.FencingToken != 1 {
		t.Fatalf("first lease=%+v owned=%v err=%v", first, owned, err)
	}
	contended, owned, err := store.AcquireSchedulerLease(ctx, "dso-scheduler", "instance-b", replayNow.Add(time.Second), 20*time.Second)
	if err != nil || owned || contended.OwnerInstanceID != "instance-a" {
		t.Fatalf("contended lease=%+v owned=%v err=%v", contended, owned, err)
	}
	failoverAt := replayNow.Add(21 * time.Second)
	second, owned, err := store.AcquireSchedulerLease(ctx, "dso-scheduler", "instance-b", failoverAt, 20*time.Second)
	if err != nil || !owned || second.FencingToken != 2 || failoverAt.Sub(replayNow) >= 30*time.Second {
		t.Fatalf("failover lease=%+v owned=%v err=%v", second, owned, err)
	}
	if err := store.ReleaseSchedulerLease(ctx, "dso-scheduler", "instance-a", first.FencingToken, failoverAt); err == nil {
		t.Fatal("stale scheduler owner released the new owner's lease")
	}
}

func TestRecoveryExportDeleteAndDiagnosticsStayOwnerScoped(t *testing.T) {
	store, db := newReplayStore(t)
	manifestA := persistReplaySource(t, store, db, "owner-a", "run-export-a", "observation-a")
	_ = manifestA
	persistReplaySource(t, store, db, "owner-b", "run-export-b", "observation-b")
	service := NewRecoveryService(store)
	service.now = func() time.Time { return replayNow.Add(time.Hour) }
	exportRequest := dso.DataLifecycleRequest{Schema: dso.Schema, RequestID: "export-a", OwnerID: "owner-a", Operation: dso.DataLifecycleExport, RequestedBy: "owner-a", CreatedAt: replayNow}
	data, err := service.ExportOwnerData(context.Background(), exportRequest)
	if err != nil || !strings.Contains(string(data), "run-export-a") || strings.Contains(string(data), "run-export-b") {
		t.Fatalf("owner export=%s err=%v", data, err)
	}
	deleteRequest := dso.DataLifecycleRequest{Schema: dso.Schema, RequestID: "delete-a", OwnerID: "owner-a", Operation: dso.DataLifecycleDelete, Cutoff: replayNow.Add(2 * time.Hour), RequestedBy: "owner-a", CreatedAt: replayNow.Add(time.Hour)}
	summary, err := service.DeleteOwnerData(context.Background(), deleteRequest)
	if err != nil || summary.DeletedRows == 0 {
		t.Fatalf("deletion summary=%+v err=%v", summary, err)
	}
	ownerARun, err := store.FindRun(context.Background(), "owner-a", "run-export-a")
	if err != nil || ownerARun != nil {
		t.Fatalf("owner-a data remained: run=%+v err=%v", ownerARun, err)
	}
	ownerBRun, err := store.FindRun(context.Background(), "owner-b", "run-export-b")
	if err != nil || ownerBRun == nil {
		t.Fatalf("owner-b data was deleted: run=%+v err=%v", ownerBRun, err)
	}
	snapshot, err := service.Diagnostics(context.Background(), replayNow.Add(-time.Hour), replayNow.Add(3*time.Hour))
	if err != nil || snapshot.DuplicateConfirmedSideEffects != 0 || snapshot.CancelPropagationP95MS >= 5000 {
		t.Fatalf("diagnostics=%+v err=%v", snapshot, err)
	}
}

func newReplayStore(t *testing.T) (*delegationrepo.Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&po.Proposal{}, &po.Decision{}, &po.DelegatedOutcome{}, &po.SubagentSpec{},
		&po.ContextSlice{}, &po.CapabilityView{}, &po.ActorBinding{}, &po.InvocationManifest{},
		&po.Run{}, &po.Attempt{}, &po.DecisionTurn{}, &po.ModelInvocation{}, &po.BudgetAccount{},
		&po.BudgetReservation{}, &po.ResourceLease{}, &po.CandidateResult{}, &po.VerificationResult{}, &po.Event{},
		&po.ActionProposal{}, &po.PlanCandidate{}, &po.ExecutionContext{}, &po.ActionPolicyDecision{},
		&po.ActionPlanRun{}, &po.GovernedActionAttempt{}, &po.ActionVerification{},
		&po.Replay{}, &po.SchedulerLease{}, &po.RetentionTombstone{},
	); err != nil {
		t.Fatal(err)
	}
	return delegationrepo.NewStore(data.New(db)), db
}

func persistReplaySource(t *testing.T, store *delegationrepo.Store, db *gorm.DB, ownerID, runID, observationID string) entity.ImmutableRecord {
	t.Helper()
	now := replayNow
	specHash := strings.Repeat("b", 64)
	outcomeHash := strings.Repeat("c", 64)
	accepted := entity.AcceptedDelegation{
		Proposal:         entity.Proposal{ProposalID: "proposal-" + runID, OwnerID: ownerID, GoalID: "goal-" + runID, TaskStepID: "task-" + runID, InputHash: strings.Repeat("a", 64), Status: entity.ProposalSubmitted, Revision: 1, Content: `{}`, CreatedAt: now, UpdatedAt: now},
		Decision:         entity.Decision{DecisionID: "decision-" + runID, OwnerID: ownerID, ProposalID: "proposal-" + runID, ProposalInputHash: strings.Repeat("a", 64), Decision: entity.DecisionDelegate, PolicyVersion: "policy/v1", Content: `{}`, CreatedAt: now},
		DelegatedOutcome: entity.Definition{ID: "outcome-" + runID, OwnerID: ownerID, TaskStepID: "task-" + runID, DefinitionHash: outcomeHash, Content: `{}`, CreatedAt: now},
		SubagentSpec:     entity.Definition{ID: "spec-" + runID, OwnerID: ownerID, TaskStepID: "task-" + runID, DefinitionHash: specHash, Content: `{}`, CreatedAt: now},
		Run:              entity.Run{RunID: runID, OwnerID: ownerID, GoalID: "goal-" + runID, TaskStepID: "task-" + runID, SubagentSpecID: "spec-" + runID, DelegatedOutcomeID: "outcome-" + runID, ActorBindingID: "actor-" + runID, Status: entity.RunQueued, Revision: 1, Deadline: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
		Event:            entity.Event{EventID: "event-" + runID, OwnerID: ownerID, AggregateType: "subagent_run", AggregateID: runID, Sequence: 1, Type: "DelegationAccepted", IdempotencyKey: runID + ":accepted:1", TraceID: "trace-" + runID, CausationID: "proposal-" + runID, Payload: `{}`, CreatedAt: now},
	}
	if err := store.CreateAcceptedDelegation(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	builder := NewContextBuilder()
	builder.now = func() time.Time { return now }
	contextBundle, err := builder.Build(ownerID, runID, dso.ContextScope{AllowedClasses: []string{dso.ClassInternal}, MaxBytes: 1024}, nil, ContextSource{Ref: "context://fixture", OwnerID: ownerID, SourceType: "test", TrustClass: dso.TrustInternal, Classification: dso.ClassInternal, Content: `{"fixture":true}`})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := NewArtifactResolver().Resolve(ArtifactResolveInput{OwnerID: ownerID, RunID: runID, ParentRunManifestID: "parent-manifest-1", SubagentSpecID: accepted.SubagentSpec.ID, DelegatedOutcomeID: accepted.DelegatedOutcome.ID, ActorBindingID: accepted.Run.ActorBindingID, EnvironmentRef: "test", RuntimeBuildRef: "agent-runtime/test", SpecialistProfileRef: researchProfileRef, PromptArtifactRef: researchPromptRef, AdmittedCapabilities: []string{"internet.search"}, Context: contextBundle, Model: runtimeentity.ModelConfig{Provider: "openai", Name: "gpt-test", MaxTokens: 1000}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateInvocationBundle(context.Background(), artifacts.Records); err != nil {
		t.Fatal(err)
	}
	planRunID := "plan-run-" + runID
	if err := db.Create(&po.ActionPlanRun{PlanRunID: planRunID, OwnerID: ownerID, PlanCandidateID: "plan-" + runID, ExecutionContextID: "execution-" + runID, SubagentRunID: runID, Status: dso.PlanRunCompleted, Revision: 1, Content: `{}`, StartedAt: now.UnixMilli(), EndedAt: now.Add(time.Second).UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&po.GovernedActionAttempt{ActionAttemptID: "action-attempt-" + runID, OwnerID: ownerID, PlanRunID: planRunID, PlanCandidateID: "plan-" + runID, PolicyDecisionID: "action-policy-" + runID, ActionProposalID: "action-proposal-" + runID, ObservationID: observationID, ResourceVersionBefore: "v1", ResourceVersionAfter: "v2", Status: dso.ActionSucceeded, Revision: 1, Content: `{}`, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(), EndedAt: now.UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}
	return artifacts.Records.Manifest
}

func replayRequest(id, ownerID, runID string, manifest entity.ImmutableRecord, mode string, observationRefs []string) dso.ReplayRequest {
	return dso.ReplayRequest{Schema: dso.Schema, ReplayID: id, OwnerID: ownerID, SourceRunRef: runID, SourceManifestRef: manifest.ID, SourceManifestHash: manifest.ContentHash, Mode: mode, ObservationRefs: observationRefs, RequestedBy: ownerID, CreatedAt: replayNow}
}
