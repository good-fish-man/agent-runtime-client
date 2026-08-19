package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	log "github.com/good-fish-man/logx"
)

const (
	defaultScanInterval = 5 * time.Second
	defaultLeaseTTL     = 30 * time.Second
	defaultScanLimit    = 500
)

type Config struct {
	InstanceID   string
	ScanInterval time.Duration
	LeaseTTL     time.Duration
	ScanLimit    int
}

type EventPublisher interface {
	Publish(context.Context, entity.Event) error
}

type EventPublisherFunc func(context.Context, entity.Event) error

func (f EventPublisherFunc) Publish(ctx context.Context, event entity.Event) error {
	return f(ctx, event)
}

type RecoveryReport struct {
	RecoveredAttempts int
	RequeuedRuns      int
	ExpiredRuns       int
	ReadyRuns         int
	FencedResults     int
	PublishedEvents   int
}

// Orchestrator is the sole durable delegation authority. Runtime workers may
// execute an admitted attempt, but ownership, recovery, cancellation, budget,
// and event publication always pass through this service and its Store.
type Orchestrator struct {
	store     repository.Store
	config    Config
	publisher EventPublisher
	now       func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewOrchestrator(store repository.Store, config Config, publisher EventPublisher) *Orchestrator {
	if config.InstanceID == "" {
		config.InstanceID = "delegation-orchestrator-" + ulid.New()
	}
	if config.ScanInterval <= 0 {
		config.ScanInterval = defaultScanInterval
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaultLeaseTTL
	}
	if config.ScanLimit <= 0 || config.ScanLimit > 1000 {
		config.ScanLimit = defaultScanLimit
	}
	if publisher == nil {
		publisher = EventPublisherFunc(func(context.Context, entity.Event) error { return nil })
	}
	return &Orchestrator{store: store, config: config, publisher: publisher, now: func() time.Time { return time.Now().UTC() }}
}

func (o *Orchestrator) Start(parent context.Context) error {
	if o == nil || o.store == nil {
		return fmt.Errorf("delegation orchestrator is not configured")
	}
	o.mu.Lock()
	if o.cancel != nil {
		o.mu.Unlock()
		return nil
	}
	o.mu.Unlock()
	if _, err := o.RunOnce(parent); err != nil {
		return log.WrapError(err, "DelegationOrchestrator.Start.recover")
	}
	ctx, cancel := context.WithCancel(parent)
	o.mu.Lock()
	if o.cancel != nil {
		o.mu.Unlock()
		cancel()
		return nil
	}
	o.cancel = cancel
	o.mu.Unlock()
	o.wg.Add(1)
	go o.loop(ctx)
	return nil
}

func (o *Orchestrator) Stop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	cancel := o.cancel
	o.cancel = nil
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.wg.Wait()
}

func (o *Orchestrator) loop(ctx context.Context) {
	defer o.wg.Done()
	ticker := time.NewTicker(o.config.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := o.RunOnce(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Errorw(ctx, "delegation recovery scan failed", "error_chain", log.FormatError(err))
				continue
			}
			if report.RecoveredAttempts+report.RequeuedRuns+report.ExpiredRuns+report.FencedResults > 0 {
				log.Infow(ctx, "delegation recovery scan completed",
					"recovered_attempts", report.RecoveredAttempts,
					"requeued_runs", report.RequeuedRuns,
					"expired_runs", report.ExpiredRuns,
					"fenced_results", report.FencedResults,
				)
			}
		}
	}
}

func (o *Orchestrator) RunOnce(ctx context.Context) (RecoveryReport, error) {
	var report RecoveryReport
	if o == nil || o.store == nil {
		return report, fmt.Errorf("delegation orchestrator is not configured")
	}
	now := o.now().UTC()
	attempts, err := o.store.ListRecoverableAttempts(ctx, now, o.config.ScanLimit)
	if err != nil {
		return report, log.WrapError(err, "DelegationOrchestrator.RunOnce.listAttempts")
	}
	for _, attempt := range attempts {
		run, findErr := o.store.FindRun(ctx, attempt.OwnerID, attempt.RunID)
		if findErr != nil {
			return report, log.WrapError(findErr, "DelegationOrchestrator.RunOnce.findRun")
		}
		if run == nil || entity.IsRunTerminal(run.Status) || run.ActiveAttemptID != attempt.AttemptID {
			report.FencedResults++
			continue
		}
		terminalStatus := entity.AttemptTimedOut
		eventType := "SubagentAttemptTimedOut"
		if attempt.Status == entity.AttemptCancelRequested || !run.CancelRequestedAt.IsZero() {
			terminalStatus = entity.AttemptAbandoned
			eventType = "SubagentAttemptAbandoned"
		}
		event := o.runEvent(ctx, *run, eventType, run.Revision+1, attempt.AttemptID, map[string]any{
			"attempt_id": attempt.AttemptID, "previous_owner_instance_id": attempt.OwnerInstanceID,
			"lease_expired_at": attempt.LeaseExpiresAt,
		})
		completeErr := o.store.CompleteAttempt(ctx, attempt.OwnerID, attempt.RunID, attempt.AttemptID, attempt.Revision, terminalStatus, "", now, event)
		if errors.Is(completeErr, repository.ErrStaleAttempt) || errors.Is(completeErr, repository.ErrRevisionConflict) {
			report.FencedResults++
			continue
		}
		if completeErr != nil {
			return report, log.WrapError(completeErr, "DelegationOrchestrator.RunOnce.completeExpiredAttempt")
		}
		report.RecoveredAttempts++
	}

	runs, err := o.store.ListRecoverableRuns(ctx, now, o.config.ScanLimit)
	if err != nil {
		return report, log.WrapError(err, "DelegationOrchestrator.RunOnce.listRuns")
	}
	for _, run := range runs {
		if entity.IsRunTerminal(run.Status) || run.ActiveAttemptID != "" {
			continue
		}
		if !run.Deadline.IsZero() && !run.Deadline.After(now) {
			event := o.runEvent(ctx, run, "SubagentRunExpired", run.Revision+1, run.RunID, map[string]any{"deadline": run.Deadline})
			recoverErr := o.store.RecoverRun(ctx, run.OwnerID, run.RunID, run.Revision, entity.RunExpired, "deadline exceeded", now, event)
			if errors.Is(recoverErr, repository.ErrRevisionConflict) || errors.Is(recoverErr, repository.ErrAttemptOwned) {
				report.FencedResults++
				continue
			}
			if recoverErr != nil {
				return report, log.WrapError(recoverErr, "DelegationOrchestrator.RunOnce.expireRun")
			}
			report.ExpiredRuns++
			continue
		}
		switch run.Status {
		case entity.RunRunning, entity.RunWaitingObservation, entity.RunWaitingUser, entity.RunWaitingDevice:
			event := o.runEvent(ctx, run, "SubagentRunRecovered", run.Revision+1, run.RunID, map[string]any{"previous_status": run.Status})
			recoverErr := o.store.RecoverRun(ctx, run.OwnerID, run.RunID, run.Revision, entity.RunWaitingRetry, "orphan run recovered", now, event)
			if errors.Is(recoverErr, repository.ErrRevisionConflict) || errors.Is(recoverErr, repository.ErrAttemptOwned) {
				report.FencedResults++
				continue
			}
			if recoverErr != nil {
				return report, log.WrapError(recoverErr, "DelegationOrchestrator.RunOnce.requeueRun")
			}
			report.RequeuedRuns++
		default:
			report.ReadyRuns++
		}
	}

	published, err := o.publishOutbox(ctx)
	report.PublishedEvents = published
	if err != nil {
		return report, log.WrapError(err, "DelegationOrchestrator.RunOnce.publishOutbox")
	}
	return report, nil
}

func (o *Orchestrator) Accept(ctx context.Context, value entity.AcceptedDelegation) error {
	return log.WrapError(o.store.CreateAcceptedDelegation(ctx, value), "DelegationOrchestrator.Accept")
}

func (o *Orchestrator) ReserveBudget(ctx context.Context, reservation entity.BudgetReservation) error {
	event := o.aggregateEvent(ctx, reservation.OwnerID, "budget_reservation", reservation.ReservationID, "BudgetReserved", 1, reservation.RunID, map[string]any{"requested": reservation.Requested})
	return log.WrapError(o.store.ReserveBudget(ctx, reservation, event), "DelegationOrchestrator.ReserveBudget")
}

func (o *Orchestrator) CommitBudget(ctx context.Context, reservation entity.BudgetReservation, consumed entity.BudgetAmount) error {
	event := o.aggregateEvent(ctx, reservation.OwnerID, "budget_reservation", reservation.ReservationID, "BudgetCommitted", reservation.Revision+1, reservation.RunID, map[string]any{"consumed": consumed})
	return log.WrapError(o.store.CommitBudget(ctx, reservation.OwnerID, reservation.ReservationID, reservation.Revision, consumed, o.now().UTC(), event), "DelegationOrchestrator.CommitBudget")
}

func (o *Orchestrator) ReleaseBudget(ctx context.Context, reservation entity.BudgetReservation) error {
	event := o.aggregateEvent(ctx, reservation.OwnerID, "budget_reservation", reservation.ReservationID, "BudgetReleased", reservation.Revision+1, reservation.RunID, nil)
	return log.WrapError(o.store.ReleaseBudget(ctx, reservation.OwnerID, reservation.ReservationID, reservation.Revision, o.now().UTC(), event), "DelegationOrchestrator.ReleaseBudget")
}

func (o *Orchestrator) AcquireAttempt(ctx context.Context, attempt entity.Attempt) error {
	run, err := o.store.FindRun(ctx, attempt.OwnerID, attempt.RunID)
	if err != nil {
		return log.WrapError(err, "DelegationOrchestrator.AcquireAttempt.findRun")
	}
	if run == nil {
		return fmt.Errorf("subagent run %q was not found", attempt.RunID)
	}
	now := o.now().UTC()
	if attempt.OwnerInstanceID == "" {
		attempt.OwnerInstanceID = o.config.InstanceID
	}
	if attempt.StartedAt.IsZero() {
		attempt.StartedAt = now
	}
	if attempt.HeartbeatAt.IsZero() {
		attempt.HeartbeatAt = now
	}
	if attempt.LeaseExpiresAt.IsZero() {
		attempt.LeaseExpiresAt = now.Add(o.config.LeaseTTL)
	}
	if attempt.Status == "" {
		attempt.Status = entity.AttemptStarting
	}
	if attempt.Revision == 0 {
		attempt.Revision = 1
	}
	event := o.runEvent(ctx, *run, "SubagentAttemptStarted", run.Revision+1, attempt.AttemptID, map[string]any{"attempt_id": attempt.AttemptID, "owner_instance_id": attempt.OwnerInstanceID})
	return log.WrapError(o.store.AcquireAttempt(ctx, attempt, run.Revision, event), "DelegationOrchestrator.AcquireAttempt")
}

func (o *Orchestrator) HeartbeatAttempt(ctx context.Context, attempt entity.Attempt) error {
	now := o.now().UTC()
	return log.WrapError(o.store.HeartbeatAttempt(ctx, attempt.OwnerID, attempt.AttemptID, attempt.OwnerInstanceID, attempt.Revision, now, now.Add(o.config.LeaseTTL)), "DelegationOrchestrator.HeartbeatAttempt")
}

func (o *Orchestrator) CompleteAttempt(ctx context.Context, attempt entity.Attempt, status, resultID string) error {
	run, err := o.store.FindRun(ctx, attempt.OwnerID, attempt.RunID)
	if err != nil {
		return log.WrapError(err, "DelegationOrchestrator.CompleteAttempt.findRun")
	}
	if run == nil {
		return fmt.Errorf("subagent run %q was not found", attempt.RunID)
	}
	event := o.runEvent(ctx, *run, "SubagentAttempt"+status, run.Revision+1, attempt.AttemptID, map[string]any{"attempt_id": attempt.AttemptID, "result_id": resultID})
	return log.WrapError(o.store.CompleteAttempt(ctx, attempt.OwnerID, attempt.RunID, attempt.AttemptID, attempt.Revision, status, resultID, o.now().UTC(), event), "DelegationOrchestrator.CompleteAttempt")
}

func (o *Orchestrator) CancelRun(ctx context.Context, ownerID, runID, reason string) error {
	run, err := o.store.FindRun(ctx, ownerID, runID)
	if err != nil {
		return log.WrapError(err, "DelegationOrchestrator.CancelRun.findRun")
	}
	if run == nil {
		return nil
	}
	event := o.runEvent(ctx, *run, "SubagentRunCancelled", run.Revision+1, runID, map[string]any{"reason": reason})
	return log.WrapError(o.store.CancelRun(ctx, ownerID, runID, reason, o.now().UTC(), event), "DelegationOrchestrator.CancelRun")
}

func (o *Orchestrator) publishOutbox(ctx context.Context) (int, error) {
	events, err := o.store.ListUnpublishedEvents(ctx, o.config.ScanLimit)
	if err != nil {
		return 0, err
	}
	published := 0
	for _, event := range events {
		if err := o.publisher.Publish(ctx, event); err != nil {
			return published, err
		}
		if err := o.store.MarkEventPublished(ctx, event.EventID, o.now().UTC()); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}

func (o *Orchestrator) runEvent(ctx context.Context, run entity.Run, eventType string, sequence int64, causationID string, payload any) entity.Event {
	return o.aggregateEvent(ctx, run.OwnerID, "subagent_run", run.RunID, eventType, sequence, causationID, payload)
}

func (o *Orchestrator) aggregateEvent(ctx context.Context, ownerID, aggregateType, aggregateID, eventType string, sequence int64, causationID string, payload any) entity.Event {
	encoded, _ := json.Marshal(payload)
	traceID := log.ReqID(ctx)
	if traceID == "" {
		traceID = "dso-" + ulid.New()
	}
	return entity.Event{
		EventID: ulid.New(), OwnerID: ownerID, AggregateType: aggregateType, AggregateID: aggregateID,
		Sequence: sequence, Type: eventType, IdempotencyKey: fmt.Sprintf("%s:%s:%d", aggregateID, eventType, sequence),
		TraceID: traceID, CausationID: causationID, Payload: string(encoded), CreatedAt: o.now().UTC(),
	}
}
