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
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/delegation"
	log "github.com/good-fish-man/logx"
)

var orchestratorNow = time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)

func TestRunOnceRecoversExpiredAttemptAndFencesLateResult(t *testing.T) {
	orchestrator, store, _ := newOrchestratorTestFixture(t, nil)
	ctx := context.Background()
	bundle := orchestratorAcceptedBundle("run-recover", orchestratorNow.Add(time.Hour))
	if err := store.CreateAcceptedDelegation(ctx, bundle); err != nil {
		t.Fatal(err)
	}
	persistOrchestratorManifest(t, store, bundle.Run.RunID)
	attempt := orchestratorAttempt("attempt-expired", bundle.Run.RunID, entity.AttemptRunning, orchestratorNow.Add(-time.Second))
	if err := store.AcquireAttempt(ctx, attempt, 1, orchestratorEvent("attempt-started", bundle.Run.RunID, 2)); err != nil {
		t.Fatal(err)
	}
	report, err := orchestrator.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.RecoveredAttempts != 1 || report.ReadyRuns != 1 {
		t.Fatalf("unexpected recovery report: %+v", report)
	}
	run, err := store.FindRun(ctx, "owner-1", bundle.Run.RunID)
	if err != nil || run == nil || run.Status != entity.RunWaitingRetry || run.ActiveAttemptID != "" {
		t.Fatalf("recovered run = %+v err=%v", run, err)
	}
	late := store.CompleteAttempt(ctx, "owner-1", bundle.Run.RunID, attempt.AttemptID, 1, entity.AttemptCompleted, "late", orchestratorNow.Add(time.Second), orchestratorEvent("late", bundle.Run.RunID, 4))
	if !errors.Is(late, repository.ErrStaleAttempt) {
		t.Fatalf("late completion error = %v", late)
	}
}

func TestRunOnceRecoversEveryRestartBoundary(t *testing.T) {
	tests := []struct {
		name            string
		runStatus       string
		attemptStatus   string
		withAttempt     bool
		cancelRequested bool
		wantRunStatus   string
		wantRecovered   int
		wantReady       int
		wantRequeued    int
	}{
		{name: "created remains ready", runStatus: entity.RunCreated, wantRunStatus: entity.RunCreated, wantReady: 1},
		{name: "running attempt times out", runStatus: entity.RunRunning, attemptStatus: entity.AttemptRunning, withAttempt: true, wantRunStatus: entity.RunWaitingRetry, wantRecovered: 1, wantReady: 1},
		{name: "waiting observation attempt times out", runStatus: entity.RunWaitingObservation, attemptStatus: entity.AttemptWaitingObservation, withAttempt: true, wantRunStatus: entity.RunWaitingRetry, wantRecovered: 1, wantReady: 1},
		{name: "cancel requested attempt is abandoned", runStatus: entity.RunRunning, attemptStatus: entity.AttemptCancelRequested, withAttempt: true, cancelRequested: true, wantRunStatus: entity.RunCancelled, wantRecovered: 1},
		{name: "orphan waiting observation is requeued", runStatus: entity.RunWaitingObservation, wantRunStatus: entity.RunWaitingRetry, wantRequeued: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			orchestrator, store, db := newOrchestratorTestFixture(t, nil)
			ctx := context.Background()
			bundle := orchestratorAcceptedBundle("run-boundary", orchestratorNow.Add(time.Hour))
			bundle.Run.Status = test.runStatus
			if err := store.CreateAcceptedDelegation(ctx, bundle); err != nil {
				t.Fatal(err)
			}
			persistOrchestratorManifest(t, store, bundle.Run.RunID)
			if test.withAttempt {
				attempt := orchestratorAttempt("attempt-boundary", bundle.Run.RunID, entity.AttemptStarting, orchestratorNow.Add(-time.Second))
				if err := store.AcquireAttempt(ctx, attempt, 1, orchestratorEvent("attempt-started", bundle.Run.RunID, 2)); err != nil {
					t.Fatal(err)
				}
				updates := map[string]any{"status": test.runStatus}
				if test.cancelRequested {
					updates["cancel_requested_at"] = orchestratorNow.Add(-time.Second).UnixMilli()
				}
				if err := db.Model(&po.Run{}).Where("run_id = ?", bundle.Run.RunID).Updates(updates).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&po.Attempt{}).Where("attempt_id = ?", attempt.AttemptID).Update("status", test.attemptStatus).Error; err != nil {
					t.Fatal(err)
				}
			}
			report, err := orchestrator.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if report.RecoveredAttempts != test.wantRecovered || report.ReadyRuns != test.wantReady || report.RequeuedRuns != test.wantRequeued {
				t.Fatalf("recovery report = %+v", report)
			}
			run, err := store.FindRun(ctx, "owner-1", bundle.Run.RunID)
			if err != nil || run == nil || run.Status != test.wantRunStatus {
				t.Fatalf("run = %+v err=%v, want status %s", run, err, test.wantRunStatus)
			}
		})
	}
}

func TestRunOnceExpiresDeadlineAndPublishesTraceableEvent(t *testing.T) {
	var published []entity.Event
	publisher := EventPublisherFunc(func(_ context.Context, event entity.Event) error {
		published = append(published, event)
		return nil
	})
	orchestrator, store, _ := newOrchestratorTestFixture(t, publisher)
	ctx := log.WithReqID(context.Background(), "trace-expire")
	bundle := orchestratorAcceptedBundle("run-expired", orchestratorNow.Add(-time.Second))
	if err := store.CreateAcceptedDelegation(ctx, bundle); err != nil {
		t.Fatal(err)
	}
	report, err := orchestrator.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.ExpiredRuns != 1 || report.PublishedEvents != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(published) != 2 || published[1].TraceID != "trace-expire" || published[1].CausationID != bundle.Run.RunID {
		t.Fatalf("published events are not traceable: %+v", published)
	}
}

func TestOutboxPublisherFailureIsRetriedWithoutDuplicateState(t *testing.T) {
	var calls atomic.Int64
	fail := atomic.Bool{}
	fail.Store(true)
	publisher := EventPublisherFunc(func(context.Context, entity.Event) error {
		calls.Add(1)
		if fail.Load() {
			return errors.New("publisher unavailable")
		}
		return nil
	})
	orchestrator, store, _ := newOrchestratorTestFixture(t, publisher)
	ctx := context.Background()
	bundle := orchestratorAcceptedBundle("run-outbox", orchestratorNow.Add(time.Hour))
	if err := store.CreateAcceptedDelegation(ctx, bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.RunOnce(ctx); err == nil {
		t.Fatal("expected publisher failure")
	}
	fail.Store(false)
	report, err := orchestrator.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.PublishedEvents != 1 || calls.Load() != 2 {
		t.Fatalf("outbox retry report=%+v calls=%d", report, calls.Load())
	}
}

func TestStartIsIdempotent(t *testing.T) {
	orchestrator, _, _ := newOrchestratorTestFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := orchestrator.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Start(ctx); err != nil {
		t.Fatal(err)
	}
	orchestrator.Stop()
	orchestrator.Stop()
}

func newOrchestratorTestFixture(t *testing.T, publisher EventPublisher) (*Orchestrator, *delegationrepo.Store, *gorm.DB) {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
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
		&po.Proposal{}, &po.Decision{}, &po.DelegatedOutcome{}, &po.SubagentSpec{}, &po.ContextSlice{}, &po.CapabilityView{}, &po.ActorBinding{}, &po.InvocationManifest{},
		&po.Run{}, &po.Attempt{}, &po.DecisionTurn{}, &po.ModelInvocation{}, &po.BudgetAccount{},
		&po.BudgetReservation{}, &po.ResourceLease{}, &po.CandidateResult{}, &po.VerificationResult{}, &po.Event{}, &po.SchedulerLease{},
	); err != nil {
		t.Fatal(err)
	}
	store := delegationrepo.NewStore(data.New(db))
	orchestrator := NewOrchestrator(store, Config{InstanceID: "orchestrator-test", ScanInterval: time.Hour, LeaseTTL: time.Minute}, publisher)
	orchestrator.now = func() time.Time { return orchestratorNow }
	return orchestrator, store, db
}

func persistOrchestratorManifest(t *testing.T, store *delegationrepo.Store, runID string) {
	t.Helper()
	record := func(kind string) entity.ImmutableRecord {
		return entity.ImmutableRecord{ID: kind + "-1", OwnerID: "owner-1", RunID: runID, ContentHash: strings.Repeat(kind[:1], 64), Content: `{}`, CreatedAt: orchestratorNow}
	}
	if err := store.CreateInvocationBundle(context.Background(), entity.InvocationBundle{
		ContextSlice: record("context"), CapabilityView: record("capability"), ActorBinding: record("actor"), Manifest: record("manifest"),
	}); err != nil {
		t.Fatal(err)
	}
}

func orchestratorAcceptedBundle(runID string, deadline time.Time) entity.AcceptedDelegation {
	return entity.AcceptedDelegation{
		Proposal: entity.Proposal{
			ProposalID: "proposal-" + runID, OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1",
			InputHash: strings.Repeat("a", 64), Status: entity.ProposalSubmitted, Revision: 1,
			Content: `{}`, CreatedAt: orchestratorNow, UpdatedAt: orchestratorNow,
		},
		Decision: entity.Decision{
			DecisionID: "decision-" + runID, OwnerID: "owner-1", ProposalID: "proposal-" + runID,
			ProposalInputHash: strings.Repeat("a", 64), Decision: entity.DecisionDelegate,
			PolicyVersion: "v1", Content: `{}`, CreatedAt: orchestratorNow,
		},
		DelegatedOutcome: entity.Definition{ID: "outcome-" + runID, OwnerID: "owner-1", TaskStepID: "task-1", DefinitionHash: strings.Repeat("b", 64), Content: `{}`, CreatedAt: orchestratorNow},
		SubagentSpec:     entity.Definition{ID: "spec-" + runID, OwnerID: "owner-1", TaskStepID: "task-1", DefinitionHash: strings.Repeat("c", 64), Content: `{}`, CreatedAt: orchestratorNow},
		Run: entity.Run{
			RunID: runID, OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1",
			SubagentSpecID: "spec-" + runID, DelegatedOutcomeID: "outcome-" + runID,
			ActorBindingID: "actor-1", Status: entity.RunQueued, Revision: 1,
			Deadline: deadline, CreatedAt: orchestratorNow, UpdatedAt: orchestratorNow,
		},
		Event: orchestratorEvent("created-"+runID, runID, 1),
	}
}

func orchestratorAttempt(id, runID, status string, lease time.Time) entity.Attempt {
	return entity.Attempt{
		AttemptID: id, RunID: runID, OwnerID: "owner-1", AttemptNo: 1,
		InvocationManifestID: "manifest-1", IdempotencyKey: "idempotency-" + id,
		OwnerInstanceID: "worker-1", LeaseExpiresAt: lease, HeartbeatAt: orchestratorNow.Add(-time.Minute),
		Status: status, BudgetReservationID: "reservation-1", Revision: 1, StartedAt: orchestratorNow.Add(-time.Minute),
	}
}

func orchestratorEvent(id, aggregateID string, sequence int64) entity.Event {
	return entity.Event{
		EventID: id, OwnerID: "owner-1", AggregateType: "subagent_run", AggregateID: aggregateID,
		Sequence: sequence, Type: id, IdempotencyKey: "event-key-" + id,
		TraceID: "trace-test", CausationID: aggregateID, Payload: `{}`, CreatedAt: orchestratorNow,
	}
}
