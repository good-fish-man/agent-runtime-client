package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
)

var testNow = time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)

func TestCreateAcceptedDelegationIsAtomicAndIdempotent(t *testing.T) {
	store, db := newTestStore(t)
	bundle := acceptedBundle("run-1")
	if err := store.CreateAcceptedDelegation(context.Background(), bundle); err != nil {
		t.Fatalf("create accepted delegation: %v", err)
	}
	if err := store.CreateAcceptedDelegation(context.Background(), bundle); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	for name, model := range map[string]any{
		"proposal": &po.Proposal{}, "decision": &po.Decision{}, "outcome": &po.DelegatedOutcome{},
		"spec": &po.SubagentSpec{}, "run": &po.Run{}, "event": &po.Event{},
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count = %d, want 1", name, count)
		}
	}
	conflict := bundle
	conflict.Proposal.InputHash = strings.Repeat("f", 64)
	if err := store.CreateAcceptedDelegation(context.Background(), conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different proposal replay error = %v", err)
	}
}

func TestConcurrentAttemptAcquisitionHasOneOwner(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	createAcceptedWithManifest(t, store, "run-1")
	var successes atomic.Int64
	var wg sync.WaitGroup
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			attempt := testAttempt(fmt.Sprintf("attempt-%d", index), "run-1", index+1, testNow.Add(time.Minute))
			err := store.AcquireAttempt(ctx, attempt, 1, event(fmt.Sprintf("attempt-event-%d", index), "run-1", int64(index+2)))
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, ErrAttemptOwned) && !errors.Is(err, ErrRevisionConflict) {
				t.Errorf("unexpected acquisition error: %v", err)
			}
		}(index)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful attempt owners = %d, want 1", successes.Load())
	}
	run, err := store.FindRun(ctx, "owner-1", "run-1")
	if err != nil || run == nil || run.ActiveAttemptID == "" || run.Revision != 2 {
		t.Fatalf("run owner state is invalid: run=%+v err=%v", run, err)
	}
}

func TestExpiredAttemptIsRecoverableAndLateResultIsFenced(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	createAcceptedWithManifest(t, store, "run-1")
	first := testAttempt("attempt-1", "run-1", 1, testNow.Add(-time.Second))
	if err := store.AcquireAttempt(ctx, first, 1, event("event-attempt-1", "run-1", 2)); err != nil {
		t.Fatal(err)
	}
	recoverable, err := store.ListRecoverableAttempts(ctx, testNow, 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].AttemptID != first.AttemptID {
		t.Fatalf("recoverable attempts = %+v err=%v", recoverable, err)
	}
	if err := store.CompleteAttempt(ctx, "owner-1", "run-1", first.AttemptID, 1, entity.AttemptFailed, "", testNow, event("event-failed-1", "run-1", 3)); err != nil {
		t.Fatal(err)
	}
	second := testAttempt("attempt-2", "run-1", 2, testNow.Add(time.Minute))
	if err := store.AcquireAttempt(ctx, second, 3, event("event-attempt-2", "run-1", 4)); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAttempt(ctx, "owner-1", "run-1", first.AttemptID, 2, entity.AttemptCompleted, "late-result", testNow.Add(time.Second), event("event-late", "run-1", 5)); !errors.Is(err, ErrStaleAttempt) {
		t.Fatalf("late result error = %v, want ErrStaleAttempt", err)
	}
}

func TestConcurrentBudgetReservationsNeverOverspend(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	account := entity.BudgetAccount{
		BudgetRef: "budget-1", OwnerID: "owner-1", Total: entity.BudgetAmount{Tokens: 100, Actions: 10},
		Revision: 1, CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.CreateBudgetAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int64
	var wg sync.WaitGroup
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			reservation := entity.BudgetReservation{
				ReservationID: fmt.Sprintf("reservation-%d", index), OwnerID: "owner-1", BudgetRef: "budget-1",
				RunID: fmt.Sprintf("run-%d", index), Requested: entity.BudgetAmount{Tokens: 10, Actions: 1},
				Status: entity.BudgetRequested, Revision: 1, ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
			}
			if err := store.ReserveBudget(ctx, reservation, event(fmt.Sprintf("budget-event-%d", index), reservation.RunID, 1)); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrBudgetExceeded) && !errors.Is(err, ErrRevisionConflict) {
				t.Errorf("unexpected budget error: %v", err)
			}
		}(index)
	}
	wg.Wait()
	if successes.Load() != 10 {
		t.Fatalf("successful reservations = %d, want 10", successes.Load())
	}
	stored := readBudgetAccount(t, db, "budget-1")
	if stored.Reserved.Tokens != 100 || stored.Reserved.Actions != 10 || !stored.Consumed.Add(stored.Reserved).FitsWithin(stored.Total) {
		t.Fatalf("budget ledger overspent or under-recorded: %+v", stored)
	}
}

func TestBudgetCommitAndReleasePreserveLedgerAndAreIdempotent(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	account := entity.BudgetAccount{
		BudgetRef: "budget-lifecycle", OwnerID: "owner-1", Total: entity.BudgetAmount{Tokens: 100, Actions: 10},
		Revision: 1, CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.CreateBudgetAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	commitReservation := entity.BudgetReservation{
		ReservationID: "reservation-commit", OwnerID: "owner-1", BudgetRef: account.BudgetRef, RunID: "run-commit",
		Requested: entity.BudgetAmount{Tokens: 60, Actions: 6}, Status: entity.BudgetRequested, Revision: 1,
		ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.ReserveBudget(ctx, commitReservation, event("budget-reserved-commit", "reservation-commit", 1)); err != nil {
		t.Fatal(err)
	}
	consumed := entity.BudgetAmount{Tokens: 40, Actions: 4}
	if err := store.CommitBudget(ctx, "owner-1", commitReservation.ReservationID, 1, consumed, testNow.Add(time.Second), event("budget-committed", "reservation-commit", 2)); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitBudget(ctx, "owner-1", commitReservation.ReservationID, 1, consumed, testNow.Add(2*time.Second), event("budget-committed-replay", "reservation-commit", 3)); err != nil {
		t.Fatalf("idempotent commit replay: %v", err)
	}
	stored := readBudgetAccount(t, db, account.BudgetRef)
	if stored.Consumed != consumed || stored.Reserved != (entity.BudgetAmount{}) {
		t.Fatalf("commit ledger = %+v", stored)
	}

	releaseReservation := entity.BudgetReservation{
		ReservationID: "reservation-release", OwnerID: "owner-1", BudgetRef: account.BudgetRef, RunID: "run-release",
		Requested: entity.BudgetAmount{Tokens: 20, Actions: 2}, Status: entity.BudgetRequested, Revision: 1,
		ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.ReserveBudget(ctx, releaseReservation, event("budget-reserved-release", "reservation-release", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseBudget(ctx, "owner-1", releaseReservation.ReservationID, 1, testNow.Add(3*time.Second), event("budget-released", "reservation-release", 2)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseBudget(ctx, "owner-1", releaseReservation.ReservationID, 1, testNow.Add(4*time.Second), event("budget-released-replay", "reservation-release", 3)); err != nil {
		t.Fatalf("idempotent release replay: %v", err)
	}
	stored = readBudgetAccount(t, db, account.BudgetRef)
	if stored.Consumed != consumed || stored.Reserved != (entity.BudgetAmount{}) {
		t.Fatalf("release changed consumed budget or retained reservation: %+v", stored)
	}
}

func TestBudgetCommitRejectsUsageBeyondReservation(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	account := entity.BudgetAccount{BudgetRef: "budget-overuse", OwnerID: "owner-1", Total: entity.BudgetAmount{Tokens: 100}, Revision: 1, CreatedAt: testNow, UpdatedAt: testNow}
	if err := store.CreateBudgetAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	reservation := entity.BudgetReservation{
		ReservationID: "reservation-overuse", OwnerID: "owner-1", BudgetRef: account.BudgetRef, RunID: "run-overuse",
		Requested: entity.BudgetAmount{Tokens: 20}, Status: entity.BudgetRequested, Revision: 1,
		ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.ReserveBudget(ctx, reservation, event("budget-reserved-overuse", "reservation-overuse", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitBudget(ctx, "owner-1", reservation.ReservationID, 1, entity.BudgetAmount{Tokens: 30}, testNow.Add(time.Second), event("budget-overuse", "reservation-overuse", 2)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("overuse commit error = %v", err)
	}
	stored := readBudgetAccount(t, db, account.BudgetRef)
	if stored.Consumed.Tokens != 0 || stored.Reserved.Tokens != 20 {
		t.Fatalf("failed overuse commit mutated ledger: %+v", stored)
	}
}

func TestCancelRunReleasesBudgetAndResourceLeases(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	createAcceptedWithManifest(t, store, "run-1")
	account := entity.BudgetAccount{BudgetRef: "budget-1", OwnerID: "owner-1", Total: entity.BudgetAmount{Tokens: 100}, Revision: 1, CreatedAt: testNow, UpdatedAt: testNow}
	if err := store.CreateBudgetAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	reservation := entity.BudgetReservation{
		ReservationID: "reservation-1", OwnerID: "owner-1", BudgetRef: "budget-1", RunID: "run-1",
		Requested: entity.BudgetAmount{Tokens: 40}, Status: entity.BudgetRequested, Revision: 1,
		ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.ReserveBudget(ctx, reservation, event("budget-event-1", "run-1", 2)); err != nil {
		t.Fatal(err)
	}
	attempt := testAttempt("attempt-1", "run-1", 1, testNow.Add(time.Minute))
	if err := store.AcquireAttempt(ctx, attempt, 1, event("attempt-event-1", "run-1", 3)); err != nil {
		t.Fatal(err)
	}
	lease := po.ResourceLease{
		LeaseID: "lease-1", OwnerID: "owner-1", RunID: "run-1", ResourceRef: "browser-tab-1", ResourceVersion: "v1",
		Mode: "exclusive_write", ActionAttemptID: "action-1", OwnerInstanceID: "instance-1", Status: entity.LeaseActive,
		AcquiredAt: testNow.UnixMilli(), ExpiresAt: testNow.Add(time.Minute).UnixMilli(), HeartbeatAt: testNow.UnixMilli(), Revision: 1,
	}
	if err := db.Create(&lease).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.CancelRun(ctx, "owner-1", "run-1", "user_cancelled", testNow.Add(time.Second), event("cancel-event-1", "run-1", 4)); err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRun(ctx, "owner-1", "run-1")
	if err != nil || run.Status != entity.RunCancelled || run.ActiveAttemptID != "" {
		t.Fatalf("cancelled run = %+v err=%v", run, err)
	}
	var attemptRow po.Attempt
	if err := db.Where("attempt_id = ?", attempt.AttemptID).Take(&attemptRow).Error; err != nil || attemptRow.Status != entity.AttemptCancelRequested {
		t.Fatalf("attempt was not cancellation-fenced: %+v err=%v", attemptRow, err)
	}
	var leaseRow po.ResourceLease
	if err := db.Where("lease_id = ?", lease.LeaseID).Take(&leaseRow).Error; err != nil || leaseRow.Status != entity.LeaseRevoked {
		t.Fatalf("lease was not revoked: %+v err=%v", leaseRow, err)
	}
	var reservationRow po.BudgetReservation
	if err := db.Where("reservation_id = ?", reservation.ReservationID).Take(&reservationRow).Error; err != nil || reservationRow.Status != entity.BudgetReleased {
		t.Fatalf("reservation was not released: %+v err=%v", reservationRow, err)
	}
	if stored := readBudgetAccount(t, db, "budget-1"); stored.Reserved.Tokens != 0 {
		t.Fatalf("budget remained reserved after cancellation: %+v", stored)
	}
}

func TestOutboxPublicationIsIdempotent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	bundle := acceptedBundle("run-1")
	if err := store.CreateAcceptedDelegation(ctx, bundle); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListUnpublishedEvents(ctx, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("unpublished events = %+v err=%v", events, err)
	}
	if err := store.MarkEventPublished(ctx, events[0].EventID, testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkEventPublished(ctx, events[0].EventID, testNow.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	events, err = store.ListUnpublishedEvents(ctx, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("published event remained in outbox: %+v err=%v", events, err)
	}
}

func newTestStore(t *testing.T) (*Store, *gorm.DB) {
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
		&po.BudgetReservation{}, &po.ResourceLease{}, &po.CandidateResult{}, &po.VerificationResult{}, &po.Event{},
		&po.ActionProposal{}, &po.PlanCandidate{}, &po.ExecutionContext{}, &po.ActionPolicyDecision{},
		&po.ActionPlanRun{}, &po.GovernedActionAttempt{}, &po.ActionVerification{},
		&po.ParallelPlan{}, &po.ParallelNode{}, &po.ParallelAggregate{},
		&po.AdHocOverlay{}, &po.OverlayAdmission{}, &po.AdHocRunOutcome{}, &po.ProfileCandidate{},
	); err != nil {
		t.Fatal(err)
	}
	return NewStore(data.New(db)), db
}

func createAcceptedWithManifest(t *testing.T, store *Store, runID string) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateAcceptedDelegation(ctx, acceptedBundle(runID)); err != nil {
		t.Fatal(err)
	}
	record := func(kind string) entity.ImmutableRecord {
		return entity.ImmutableRecord{ID: kind + "-1", OwnerID: "owner-1", RunID: runID, ContentHash: strings.Repeat(kind[:1], 64), Content: `{}`, CreatedAt: testNow}
	}
	bundle := entity.InvocationBundle{
		ContextSlice: record("context"), CapabilityView: record("capability"), ActorBinding: record("actor"), Manifest: record("manifest"),
	}
	if err := store.CreateInvocationBundle(ctx, bundle); err != nil {
		t.Fatal(err)
	}
}

func acceptedBundle(runID string) entity.AcceptedDelegation {
	return entity.AcceptedDelegation{
		Proposal: entity.Proposal{
			ProposalID: "proposal-" + runID, OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1",
			InputHash: strings.Repeat("a", 64), Status: entity.ProposalSubmitted, Revision: 1,
			Content: `{}`, CreatedAt: testNow, UpdatedAt: testNow,
		},
		Decision: entity.Decision{
			DecisionID: "decision-" + runID, OwnerID: "owner-1", ProposalID: "proposal-" + runID,
			ProposalInputHash: strings.Repeat("a", 64), Decision: entity.DecisionDelegate,
			PolicyVersion: "v1", Content: `{}`, CreatedAt: testNow,
		},
		DelegatedOutcome: entity.Definition{ID: "outcome-" + runID, OwnerID: "owner-1", TaskStepID: "task-1", DefinitionHash: strings.Repeat("b", 64), Content: `{}`, CreatedAt: testNow},
		SubagentSpec:     entity.Definition{ID: "spec-" + runID, OwnerID: "owner-1", TaskStepID: "task-1", DefinitionHash: strings.Repeat("c", 64), Content: `{}`, CreatedAt: testNow},
		Run: entity.Run{
			RunID: runID, OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1",
			SubagentSpecID: "spec-" + runID, DelegatedOutcomeID: "outcome-" + runID,
			ActorBindingID: "actor-1", Status: entity.RunQueued, Revision: 1,
			Deadline: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
		},
		Event: event("created-"+runID, runID, 1),
	}
}

func testAttempt(id, runID string, number int, lease time.Time) entity.Attempt {
	return entity.Attempt{
		AttemptID: id, RunID: runID, OwnerID: "owner-1", AttemptNo: number,
		InvocationManifestID: "manifest-1", IdempotencyKey: "idempotency-" + id,
		OwnerInstanceID: "instance-" + id, LeaseExpiresAt: lease, HeartbeatAt: testNow,
		Status: entity.AttemptStarting, BudgetReservationID: "reservation-1", Revision: 1, StartedAt: testNow,
	}
}

func event(id, aggregateID string, sequence int64) entity.Event {
	return entity.Event{
		EventID: id, OwnerID: "owner-1", AggregateType: "subagent_run", AggregateID: aggregateID,
		Sequence: sequence, Type: id, IdempotencyKey: "event-key-" + id,
		TraceID: "trace-1", CausationID: "cause-1", Payload: `{}`, CreatedAt: testNow,
	}
}

func readBudgetAccount(t *testing.T, db *gorm.DB, budgetRef string) entity.BudgetAccount {
	t.Helper()
	var row po.BudgetAccount
	if err := db.Where("budget_ref = ?", budgetRef).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	var account entity.BudgetAccount
	if err := json.Unmarshal([]byte(row.Content), &account); err != nil {
		t.Fatal(err)
	}
	return account
}
