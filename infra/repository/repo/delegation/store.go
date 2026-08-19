package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	log "github.com/good-fish-man/logx"
)

var (
	ErrRevisionConflict    = repository.ErrRevisionConflict
	ErrAttemptOwned        = repository.ErrAttemptOwned
	ErrStaleAttempt        = repository.ErrStaleAttempt
	ErrBudgetExceeded      = repository.ErrBudgetExceeded
	ErrReservationClosed   = repository.ErrReservationClosed
	ErrIdempotencyConflict = repository.ErrIdempotencyConflict
)

const maxOptimisticRetries = 20

type Store struct{ data *data.Data }

func NewStore(value *data.Data) *Store { return &Store{data: value} }

var _ repository.Store = (*Store)(nil)

func (s *Store) CreateAcceptedDelegation(ctx context.Context, value entity.AcceptedDelegation) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.Proposal
		result := tx.Where("proposal_id = ? AND owner_id = ?", value.Proposal.ProposalID, value.Proposal.OwnerID).Limit(1).Find(&existing)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			if existing.InputHash == value.Proposal.InputHash && existing.Status == entity.ProposalAccepted {
				return nil
			}
			return ErrIdempotencyConflict
		}
		proposal := proposalRow(value.Proposal)
		proposal.Status = entity.ProposalAccepted
		if err := tx.Create(&proposal).Error; err != nil {
			return err
		}
		decision := decisionRow(value.Decision)
		if err := tx.Create(&decision).Error; err != nil {
			return err
		}
		if err := tx.Create(&po.DelegatedOutcome{
			OutcomeID: value.DelegatedOutcome.ID, OwnerID: value.DelegatedOutcome.OwnerID,
			TaskStepID: value.DelegatedOutcome.TaskStepID, DefinitionHash: value.DelegatedOutcome.DefinitionHash,
			Content: value.DelegatedOutcome.Content, CreatedAt: millis(value.DelegatedOutcome.CreatedAt),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&po.SubagentSpec{
			SpecID: value.SubagentSpec.ID, OwnerID: value.SubagentSpec.OwnerID,
			TaskStepID: value.SubagentSpec.TaskStepID, DefinitionHash: value.SubagentSpec.DefinitionHash,
			Content: value.SubagentSpec.Content, CreatedAt: millis(value.SubagentSpec.CreatedAt),
		}).Error; err != nil {
			return err
		}
		run := runRow(value.Run)
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.CreateAcceptedDelegation")
}

func (s *Store) FindRun(ctx context.Context, ownerID, runID string) (*entity.Run, error) {
	var row po.Run
	result := s.data.DB(ctx).Where("owner_id = ? AND run_id = ?", ownerID, runID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationStore.FindRun")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := runEntity(row)
	return &value, nil
}

func (s *Store) ListRecoverableRuns(ctx context.Context, now time.Time, limit int) ([]entity.Run, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	ready := []string{entity.RunCreated, entity.RunAdmitted, entity.RunQueued, entity.RunWaitingRetry}
	orphanable := []string{entity.RunRunning, entity.RunWaitingObservation, entity.RunWaitingUser, entity.RunWaitingDevice}
	terminal := []string{entity.RunCompleted, entity.RunFailed, entity.RunCancelled, entity.RunExpired}
	var rows []po.Run
	err := s.data.DB(ctx).
		Where("status IN ? OR (status IN ? AND active_attempt_id = ?) OR (status NOT IN ? AND deadline > 0 AND deadline <= ?)", ready, orphanable, "", terminal, millis(now)).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, log.WrapError(err, "DelegationStore.ListRecoverableRuns")
	}
	result := make([]entity.Run, 0, len(rows))
	for _, row := range rows {
		result = append(result, runEntity(row))
	}
	return result, nil
}

func (s *Store) RecoverRun(ctx context.Context, ownerID, runID string, expectedRevision int64, status, reason string, at time.Time, event entity.Event) error {
	if status != entity.RunWaitingRetry && status != entity.RunExpired {
		return fmt.Errorf("recover run target status %q is not allowed", status)
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var run po.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND owner_id = ?", runID, ownerID).Take(&run).Error; err != nil {
			return err
		}
		if run.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isRunTerminal(run.Status) {
			return nil
		}
		if run.ActiveAttemptID != "" {
			return ErrAttemptOwned
		}
		updates := map[string]any{
			"status": status, "revision": expectedRevision + 1, "updated_at": millis(at),
		}
		if status == entity.RunExpired {
			updates["terminal_reason"] = reason
		}
		updated := tx.Model(&po.Run{}).Where("run_id = ? AND owner_id = ? AND revision = ? AND active_attempt_id = ?", runID, ownerID, expectedRevision, "").Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationStore.RecoverRun")
}

func (s *Store) CreateBudgetAccount(ctx context.Context, account entity.BudgetAccount) error {
	content, err := encode(account)
	if err != nil {
		return err
	}
	row := po.BudgetAccount{
		BudgetRef: account.BudgetRef, OwnerID: account.OwnerID, Revision: account.Revision,
		Content: content, CreatedAt: millis(account.CreatedAt), UpdatedAt: millis(account.UpdatedAt),
	}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DelegationStore.CreateBudgetAccount")
}

func (s *Store) ReserveBudget(ctx context.Context, reservation entity.BudgetReservation, event entity.Event) error {
	if !reservation.Requested.NonNegative() {
		return ErrBudgetExceeded
	}
	for retry := 0; retry < maxOptimisticRetries; retry++ {
		err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
			var existing po.BudgetReservation
			existingResult := tx.Where("reservation_id = ?", reservation.ReservationID).Limit(1).Find(&existing)
			if existingResult.Error != nil {
				return existingResult.Error
			}
			if existingResult.RowsAffected > 0 {
				if existing.OwnerID == reservation.OwnerID && existing.BudgetRef == reservation.BudgetRef && existing.RunID == reservation.RunID {
					return nil
				}
				return ErrIdempotencyConflict
			}
			var accountRow po.BudgetAccount
			if err := tx.Where("budget_ref = ? AND owner_id = ?", reservation.BudgetRef, reservation.OwnerID).Take(&accountRow).Error; err != nil {
				return err
			}
			var account entity.BudgetAccount
			if err := json.Unmarshal([]byte(accountRow.Content), &account); err != nil {
				return err
			}
			nextReserved := account.Reserved.Add(reservation.Requested)
			if !account.Consumed.Add(nextReserved).FitsWithin(account.Total) {
				return ErrBudgetExceeded
			}
			account.Reserved = nextReserved
			account.Revision++
			account.UpdatedAt = reservation.UpdatedAt
			content, err := encode(account)
			if err != nil {
				return err
			}
			updated := tx.Model(&po.BudgetAccount{}).Where("budget_ref = ? AND owner_id = ? AND revision = ?", account.BudgetRef, account.OwnerID, accountRow.Revision).Updates(map[string]any{
				"revision": account.Revision, "content": content, "updated_at": millis(account.UpdatedAt),
			})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrRevisionConflict
			}
			reservation.Reserved = reservation.Requested
			reservation.Status = entity.BudgetReserved
			reservation.Revision = 1
			reservationContent, err := encode(reservation)
			if err != nil {
				return err
			}
			row := po.BudgetReservation{
				ReservationID: reservation.ReservationID, OwnerID: reservation.OwnerID,
				BudgetRef: reservation.BudgetRef, RunID: reservation.RunID, Status: reservation.Status,
				Revision: reservation.Revision, Content: reservationContent, ExpiresAt: millis(reservation.ExpiresAt),
				CreatedAt: millis(reservation.CreatedAt), UpdatedAt: millis(reservation.UpdatedAt),
			}
			if err := tx.Create(&row).Error; err != nil {
				if isDuplicate(err) {
					return nil
				}
				return err
			}
			return createEvent(tx, event)
		})
		if !errors.Is(err, ErrRevisionConflict) {
			return log.WrapError(err, "DelegationStore.ReserveBudget")
		}
	}
	return ErrRevisionConflict
}

func (s *Store) CommitBudget(ctx context.Context, ownerID, reservationID string, expectedRevision int64, consumed entity.BudgetAmount, at time.Time, event entity.Event) error {
	if !consumed.NonNegative() {
		return ErrBudgetExceeded
	}
	return s.transitionBudgetReservation(ctx, ownerID, reservationID, expectedRevision, consumed, entity.BudgetCommitted, at, event)
}

func (s *Store) ReleaseBudget(ctx context.Context, ownerID, reservationID string, expectedRevision int64, at time.Time, event entity.Event) error {
	return s.transitionBudgetReservation(ctx, ownerID, reservationID, expectedRevision, entity.BudgetAmount{}, entity.BudgetReleased, at, event)
}

func (s *Store) transitionBudgetReservation(ctx context.Context, ownerID, reservationID string, expectedRevision int64, consumed entity.BudgetAmount, targetStatus string, at time.Time, event entity.Event) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var row po.BudgetReservation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reservation_id = ? AND owner_id = ?", reservationID, ownerID).Take(&row).Error; err != nil {
			return err
		}
		var reservation entity.BudgetReservation
		if err := json.Unmarshal([]byte(row.Content), &reservation); err != nil {
			return err
		}
		if row.Status == targetStatus {
			if targetStatus == entity.BudgetReleased || reservation.Committed == consumed {
				return nil
			}
			return ErrIdempotencyConflict
		}
		if row.Status != entity.BudgetReserved {
			return ErrReservationClosed
		}
		if row.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if targetStatus == entity.BudgetCommitted && !consumed.FitsWithin(reservation.Reserved) {
			return ErrBudgetExceeded
		}
		var accountRow po.BudgetAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("budget_ref = ? AND owner_id = ?", reservation.BudgetRef, ownerID).Take(&accountRow).Error; err != nil {
			return err
		}
		var account entity.BudgetAccount
		if err := json.Unmarshal([]byte(accountRow.Content), &account); err != nil {
			return err
		}
		account.Reserved = account.Reserved.Sub(reservation.Reserved)
		account.Consumed = account.Consumed.Add(consumed)
		if !account.Reserved.NonNegative() || !account.Consumed.Add(account.Reserved).FitsWithin(account.Total) {
			return ErrBudgetExceeded
		}
		account.Revision++
		account.UpdatedAt = at
		accountContent, err := encode(account)
		if err != nil {
			return err
		}
		accountUpdate := tx.Model(&po.BudgetAccount{}).Where("budget_ref = ? AND owner_id = ? AND revision = ?", account.BudgetRef, ownerID, accountRow.Revision).Updates(map[string]any{
			"content": accountContent, "revision": account.Revision, "updated_at": millis(at),
		})
		if accountUpdate.Error != nil {
			return accountUpdate.Error
		}
		if accountUpdate.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		reservation.Status = targetStatus
		reservation.Committed = consumed
		reservation.Revision++
		reservation.UpdatedAt = at
		reservationContent, err := encode(reservation)
		if err != nil {
			return err
		}
		reservationUpdate := tx.Model(&po.BudgetReservation{}).Where("reservation_id = ? AND owner_id = ? AND revision = ?", reservationID, ownerID, expectedRevision).Updates(map[string]any{
			"status": targetStatus, "revision": reservation.Revision, "content": reservationContent, "updated_at": millis(at),
		})
		if reservationUpdate.Error != nil {
			return reservationUpdate.Error
		}
		if reservationUpdate.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationStore.transitionBudgetReservation")
}

func (s *Store) AcquireAttempt(ctx context.Context, attempt entity.Attempt, expectedRunRevision int64, event entity.Event) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var run po.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND owner_id = ?", attempt.RunID, attempt.OwnerID).Take(&run).Error; err != nil {
			return err
		}
		if run.Revision != expectedRunRevision {
			return ErrRevisionConflict
		}
		if isRunTerminal(run.Status) {
			return ErrStaleAttempt
		}
		if run.ActiveAttemptID != "" {
			var active po.Attempt
			if err := tx.Where("attempt_id = ?", run.ActiveAttemptID).Take(&active).Error; err == nil && !isAttemptTerminal(active.Status) && active.LeaseExpiresAt > millis(attempt.StartedAt) {
				return ErrAttemptOwned
			}
		}
		attemptValue := attemptRow(attempt)
		if err := tx.Create(&attemptValue).Error; err != nil {
			if isDuplicate(err) {
				var existing po.Attempt
				if findErr := tx.Where("idempotency_key = ?", attempt.IdempotencyKey).Take(&existing).Error; findErr == nil && existing.RunID == attempt.RunID && existing.InvocationManifestID == attempt.InvocationManifestID {
					return nil
				}
				return ErrIdempotencyConflict
			}
			return err
		}
		updated := tx.Model(&po.Run{}).Where("run_id = ? AND owner_id = ? AND revision = ?", run.RunID, run.OwnerID, expectedRunRevision).Updates(map[string]any{
			"status": entity.RunRunning, "active_attempt_id": attempt.AttemptID,
			"revision": expectedRunRevision + 1, "updated_at": millis(attempt.StartedAt),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationStore.AcquireAttempt")
}

func (s *Store) HeartbeatAttempt(ctx context.Context, ownerID, attemptID, instanceID string, expectedRevision int64, heartbeat, leaseExpiresAt time.Time) error {
	updated := s.data.DB(ctx).Model(&po.Attempt{}).Where(
		"attempt_id = ? AND owner_id = ? AND owner_instance_id = ? AND revision = ? AND status NOT IN ?",
		attemptID, ownerID, instanceID, expectedRevision, terminalAttemptStatuses(),
	).Updates(map[string]any{"heartbeat_at": millis(heartbeat), "lease_expires_at": millis(leaseExpiresAt), "revision": expectedRevision + 1})
	if updated.Error != nil {
		return log.WrapError(updated.Error, "DelegationStore.HeartbeatAttempt")
	}
	if updated.RowsAffected != 1 {
		return ErrStaleAttempt
	}
	return nil
}

func (s *Store) CompleteAttempt(ctx context.Context, ownerID, runID, attemptID string, expectedAttemptRevision int64, status, resultID string, endedAt time.Time, event entity.Event) error {
	if !isAttemptTerminal(status) {
		return fmt.Errorf("complete attempt requires a terminal status")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var run po.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND owner_id = ?", runID, ownerID).Take(&run).Error; err != nil {
			return err
		}
		if run.ActiveAttemptID != attemptID {
			return ErrStaleAttempt
		}
		attemptUpdate := tx.Model(&po.Attempt{}).Where("attempt_id = ? AND run_id = ? AND owner_id = ? AND revision = ? AND status NOT IN ?", attemptID, runID, ownerID, expectedAttemptRevision, terminalAttemptStatuses()).Updates(map[string]any{
			"status": status, "result_id": resultID, "ended_at": millis(endedAt), "revision": expectedAttemptRevision + 1,
		})
		if attemptUpdate.Error != nil {
			return attemptUpdate.Error
		}
		if attemptUpdate.RowsAffected != 1 {
			return ErrStaleAttempt
		}
		runStatus := entity.RunWaitingRetry
		if status == entity.AttemptCompleted {
			runStatus = entity.RunCompleted
		}
		if status == entity.AttemptAbandoned && run.CancelRequestedAt > 0 {
			runStatus = entity.RunCancelled
		}
		updates := map[string]any{"status": runStatus, "active_attempt_id": "", "revision": run.Revision + 1, "updated_at": millis(endedAt)}
		if runStatus == entity.RunCancelled || runStatus == entity.RunCompleted {
			updates["terminal_reason"] = status
		}
		runUpdate := tx.Model(&po.Run{}).Where("run_id = ? AND owner_id = ? AND revision = ? AND active_attempt_id = ?", runID, ownerID, run.Revision, attemptID).Updates(updates)
		if runUpdate.Error != nil {
			return runUpdate.Error
		}
		if runUpdate.RowsAffected != 1 {
			return ErrStaleAttempt
		}
		return createEvent(tx, event)
	}), "DelegationStore.CompleteAttempt")
}

func (s *Store) ListRecoverableAttempts(ctx context.Context, now time.Time, limit int) ([]entity.Attempt, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var rows []po.Attempt
	if err := s.data.DB(ctx).Where("status NOT IN ? AND lease_expires_at <= ?", terminalAttemptStatuses(), millis(now)).Order("lease_expires_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationStore.ListRecoverableAttempts")
	}
	result := make([]entity.Attempt, 0, len(rows))
	for _, row := range rows {
		result = append(result, attemptEntity(row))
	}
	return result, nil
}

func (s *Store) CancelRun(ctx context.Context, ownerID, runID, reason string, at time.Time, event entity.Event) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var run po.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND owner_id = ?", runID, ownerID).Take(&run).Error; err != nil {
			return err
		}
		if isRunTerminal(run.Status) {
			return nil
		}
		if err := tx.Model(&po.Attempt{}).Where("run_id = ? AND owner_id = ? AND status NOT IN ?", runID, ownerID, terminalAttemptStatuses()).Updates(map[string]any{
			"status": entity.AttemptCancelRequested, "revision": gorm.Expr("revision + 1"),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&po.ResourceLease{}).Where("owner_id = ? AND run_id = ? AND status = ?", ownerID, runID, entity.LeaseActive).Updates(map[string]any{"status": entity.LeaseRevoked, "expires_at": millis(at), "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
		if err := s.releaseRunReservations(tx, ownerID, runID, at); err != nil {
			return err
		}
		updated := tx.Model(&po.Run{}).Where("run_id = ? AND owner_id = ? AND revision = ?", runID, ownerID, run.Revision).Updates(map[string]any{
			"status": entity.RunCancelled, "active_attempt_id": "", "cancel_requested_at": millis(at),
			"terminal_reason": reason, "revision": run.Revision + 1, "updated_at": millis(at),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationStore.CancelRun")
}

func (s *Store) ListUnpublishedEvents(ctx context.Context, limit int) ([]entity.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []po.Event
	if err := s.data.DB(ctx).Where("published = ?", false).Order("created_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationStore.ListUnpublishedEvents")
	}
	result := make([]entity.Event, 0, len(rows))
	for _, row := range rows {
		result = append(result, eventEntity(row))
	}
	return result, nil
}

func (s *Store) MarkEventPublished(ctx context.Context, eventID string, at time.Time) error {
	updated := s.data.DB(ctx).Model(&po.Event{}).Where("event_id = ? AND published = ?", eventID, false).Updates(map[string]any{"published": true, "published_at": millis(at)})
	if updated.Error != nil {
		return log.WrapError(updated.Error, "DelegationStore.MarkEventPublished")
	}
	return nil
}

func (s *Store) releaseRunReservations(tx *gorm.DB, ownerID, runID string, at time.Time) error {
	var rows []po.BudgetReservation
	if err := tx.Where("owner_id = ? AND run_id = ? AND status = ?", ownerID, runID, entity.BudgetReserved).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var reservation entity.BudgetReservation
		if err := json.Unmarshal([]byte(row.Content), &reservation); err != nil {
			return err
		}
		var accountRow po.BudgetAccount
		if err := tx.Where("budget_ref = ? AND owner_id = ?", reservation.BudgetRef, ownerID).Take(&accountRow).Error; err != nil {
			return err
		}
		var account entity.BudgetAccount
		if err := json.Unmarshal([]byte(accountRow.Content), &account); err != nil {
			return err
		}
		account.Reserved = account.Reserved.Sub(reservation.Reserved)
		if !account.Reserved.NonNegative() {
			return fmt.Errorf("budget ledger would become negative")
		}
		account.Revision++
		account.UpdatedAt = at
		content, err := encode(account)
		if err != nil {
			return err
		}
		updated := tx.Model(&po.BudgetAccount{}).Where("budget_ref = ? AND owner_id = ? AND revision = ?", account.BudgetRef, ownerID, accountRow.Revision).Updates(map[string]any{"content": content, "revision": account.Revision, "updated_at": millis(at)})
		if updated.Error != nil || updated.RowsAffected != 1 {
			if updated.Error != nil {
				return updated.Error
			}
			return ErrRevisionConflict
		}
		reservation.Status = entity.BudgetReleased
		reservation.Revision++
		reservation.UpdatedAt = at
		reservationContent, err := encode(reservation)
		if err != nil {
			return err
		}
		if err := tx.Model(&po.BudgetReservation{}).Where("reservation_id = ? AND revision = ?", row.ReservationID, row.Revision).Updates(map[string]any{"status": entity.BudgetReleased, "revision": reservation.Revision, "content": reservationContent, "updated_at": millis(at)}).Error; err != nil {
			return err
		}
	}
	return nil
}

func createEvent(tx *gorm.DB, event entity.Event) error {
	row := eventRow(event)
	result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(&row)
	return result.Error
}

func proposalRow(value entity.Proposal) po.Proposal {
	return po.Proposal{ProposalID: value.ProposalID, OwnerID: value.OwnerID, GoalID: value.GoalID, TaskStepID: value.TaskStepID, InputHash: value.InputHash, Status: value.Status, Revision: value.Revision, Content: value.Content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
}

func decisionRow(value entity.Decision) po.Decision {
	return po.Decision{DecisionID: value.DecisionID, OwnerID: value.OwnerID, ProposalID: value.ProposalID, ProposalInputHash: value.ProposalInputHash, Decision: value.Decision, PolicyVersion: value.PolicyVersion, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func runRow(value entity.Run) po.Run {
	return po.Run{RunID: value.RunID, OwnerID: value.OwnerID, GoalID: value.GoalID, TaskStepID: value.TaskStepID, SubagentSpecID: value.SubagentSpecID, DelegatedOutcomeID: value.DelegatedOutcomeID, ActorBindingID: value.ActorBindingID, Status: value.Status, ActiveAttemptID: value.ActiveAttemptID, Revision: value.Revision, Deadline: millis(value.Deadline), CancelRequestedAt: millis(value.CancelRequestedAt), TerminalReason: value.TerminalReason, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
}

func runEntity(row po.Run) entity.Run {
	return entity.Run{RunID: row.RunID, OwnerID: row.OwnerID, GoalID: row.GoalID, TaskStepID: row.TaskStepID, SubagentSpecID: row.SubagentSpecID, DelegatedOutcomeID: row.DelegatedOutcomeID, ActorBindingID: row.ActorBindingID, Status: row.Status, ActiveAttemptID: row.ActiveAttemptID, Revision: row.Revision, Deadline: fromMillis(row.Deadline), CancelRequestedAt: fromMillis(row.CancelRequestedAt), TerminalReason: row.TerminalReason, CreatedAt: fromMillis(row.CreatedAt), UpdatedAt: fromMillis(row.UpdatedAt)}
}

func attemptRow(value entity.Attempt) po.Attempt {
	return po.Attempt{AttemptID: value.AttemptID, RunID: value.RunID, OwnerID: value.OwnerID, AttemptNo: value.AttemptNo, InvocationManifestID: value.InvocationManifestID, IdempotencyKey: value.IdempotencyKey, OwnerInstanceID: value.OwnerInstanceID, LeaseExpiresAt: millis(value.LeaseExpiresAt), HeartbeatAt: millis(value.HeartbeatAt), Status: value.Status, BudgetReservationID: value.BudgetReservationID, ResultID: value.ResultID, ErrorRef: value.ErrorRef, Revision: value.Revision, StartedAt: millis(value.StartedAt), EndedAt: millis(value.EndedAt)}
}

func attemptEntity(row po.Attempt) entity.Attempt {
	return entity.Attempt{AttemptID: row.AttemptID, RunID: row.RunID, OwnerID: row.OwnerID, AttemptNo: row.AttemptNo, InvocationManifestID: row.InvocationManifestID, IdempotencyKey: row.IdempotencyKey, OwnerInstanceID: row.OwnerInstanceID, LeaseExpiresAt: fromMillis(row.LeaseExpiresAt), HeartbeatAt: fromMillis(row.HeartbeatAt), Status: row.Status, BudgetReservationID: row.BudgetReservationID, ResultID: row.ResultID, ErrorRef: row.ErrorRef, Revision: row.Revision, StartedAt: fromMillis(row.StartedAt), EndedAt: fromMillis(row.EndedAt)}
}

func eventRow(value entity.Event) po.Event {
	return po.Event{EventID: value.EventID, OwnerID: value.OwnerID, AggregateType: value.AggregateType, AggregateID: value.AggregateID, Sequence: value.Sequence, Type: value.Type, IdempotencyKey: value.IdempotencyKey, TraceID: value.TraceID, CausationID: value.CausationID, Payload: value.Payload, Published: value.Published, CreatedAt: millis(value.CreatedAt), PublishedAt: millis(value.PublishedAt)}
}

func eventEntity(row po.Event) entity.Event {
	return entity.Event{EventID: row.EventID, OwnerID: row.OwnerID, AggregateType: row.AggregateType, AggregateID: row.AggregateID, Sequence: row.Sequence, Type: row.Type, IdempotencyKey: row.IdempotencyKey, TraceID: row.TraceID, CausationID: row.CausationID, Payload: row.Payload, Published: row.Published, CreatedAt: fromMillis(row.CreatedAt), PublishedAt: fromMillis(row.PublishedAt)}
}

func encode(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func millis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func fromMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func terminalAttemptStatuses() []string {
	return []string{entity.AttemptCompleted, entity.AttemptFailed, entity.AttemptTimedOut, entity.AttemptAbandoned}
}

func isAttemptTerminal(status string) bool { return entity.IsAttemptTerminal(status) }
func isRunTerminal(status string) bool     { return entity.IsRunTerminal(status) }

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint")
}
