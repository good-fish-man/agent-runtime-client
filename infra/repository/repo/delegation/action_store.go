package delegation

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

var _ repository.ActionStore = (*Store)(nil)

func (s *Store) CreateActionChain(ctx context.Context, value entity.ActionChain) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.ActionProposal
		found := tx.Where("action_proposal_id = ? AND owner_id = ?", value.Proposal.ActionProposalID, value.Proposal.OwnerID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.InputHash == value.Proposal.InputHash {
				return nil
			}
			return repository.ErrIdempotencyConflict
		}
		rows := []any{
			actionProposalRow(value.Proposal), planCandidateRow(value.Plan),
			executionContextRow(value.ExecutionContext), actionPolicyDecisionRow(value.Policy),
			actionPlanRunRow(value.PlanRun), governedActionAttemptRow(value.Attempt),
		}
		for _, row := range rows {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.CreateActionChain")
}

func (s *Store) AcquireActionLease(ctx context.Context, lease entity.ResourceLease, expectedVersion, currentVersion string, at time.Time, event entity.Event) error {
	if expectedVersion != currentVersion {
		return repository.ErrResourceStale
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&po.ResourceLease{}).
			Where("resource_ref = ? AND status = ? AND expires_at <= ?", lease.ResourceRef, entity.LeaseActive, millis(at)).
			Updates(map[string]any{"status": entity.LeaseExpired, "active_key": nil, "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
		var attempt po.GovernedActionAttempt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("action_attempt_id = ? AND owner_id = ?", lease.ActionAttemptID, lease.OwnerID).Take(&attempt).Error; err != nil {
			return err
		}
		if attempt.Status == entity.ActionLeased || attempt.Status == entity.ActionExecuting {
			if attempt.ResourceLeaseID == lease.LeaseID {
				return nil
			}
			return repository.ErrResourceBusy
		}
		if attempt.Status != entity.ActionPolicyAllowed {
			return fmt.Errorf("action attempt %s cannot acquire a lease from %s", attempt.ActionAttemptID, attempt.Status)
		}
		row := resourceLeaseRow(lease)
		if lease.Mode == entity.LeaseExclusiveWrite {
			activeKey := lease.ResourceRef
			row.ActiveKey = &activeKey
		}
		if err := tx.Create(&row).Error; err != nil {
			if isDuplicate(err) {
				return repository.ErrResourceBusy
			}
			return err
		}
		updated := tx.Model(&po.GovernedActionAttempt{}).
			Where("action_attempt_id = ? AND owner_id = ? AND revision = ? AND status = ?", attempt.ActionAttemptID, attempt.OwnerID, attempt.Revision, entity.ActionPolicyAllowed).
			Updates(map[string]any{"status": entity.ActionLeased, "resource_lease_id": lease.LeaseID, "revision": attempt.Revision + 1, "updated_at": millis(at)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return repository.ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationStore.AcquireActionLease")
}

func (s *Store) CompleteActionChain(ctx context.Context, value entity.ActionCompletion) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		recordedAt := value.RecordedAt
		if recordedAt.IsZero() {
			recordedAt = value.EndedAt
		}
		var attempt po.GovernedActionAttempt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("action_attempt_id = ? AND owner_id = ?", value.ActionAttemptID, value.OwnerID).Take(&attempt).Error; err != nil {
			return err
		}
		if actionAttemptTerminal(attempt.Status) {
			if attempt.Status == value.AttemptStatus && attempt.ObservationID == value.ObservationID {
				return nil
			}
			return repository.ErrIdempotencyConflict
		}
		var run po.ActionPlanRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_run_id = ? AND owner_id = ?", value.PlanRunID, value.OwnerID).Take(&run).Error; err != nil {
			return err
		}
		if value.ResourceLeaseID != "" {
			updated := tx.Model(&po.ResourceLease{}).
				Where("lease_id = ? AND owner_id = ? AND action_attempt_id = ? AND status = ?", value.ResourceLeaseID, value.OwnerID, value.ActionAttemptID, entity.LeaseActive).
				Updates(map[string]any{"status": entity.LeaseReleased, "active_key": nil, "expires_at": millis(recordedAt), "revision": gorm.Expr("revision + 1")})
			if updated.Error != nil {
				return updated.Error
			}
		}
		attemptUpdate := tx.Model(&po.GovernedActionAttempt{}).
			Where("action_attempt_id = ? AND owner_id = ? AND revision = ?", value.ActionAttemptID, value.OwnerID, attempt.Revision).
			Updates(map[string]any{
				"status": value.AttemptStatus, "observation_id": value.ObservationID,
				"resource_version_after": value.ResourceVersionAfter, "error_chain": value.ErrorChain,
				"content": value.AttemptContent, "revision": attempt.Revision + 1,
				"updated_at": millis(recordedAt), "ended_at": millis(value.EndedAt),
			})
		if attemptUpdate.Error != nil {
			return attemptUpdate.Error
		}
		if attemptUpdate.RowsAffected != 1 {
			return repository.ErrRevisionConflict
		}
		runUpdate := tx.Model(&po.ActionPlanRun{}).
			Where("plan_run_id = ? AND owner_id = ? AND revision = ?", value.PlanRunID, value.OwnerID, run.Revision).
			Updates(map[string]any{"status": value.PlanStatus, "content": value.PlanContent, "revision": run.Revision + 1, "ended_at": millis(value.EndedAt)})
		if runUpdate.Error != nil {
			return runUpdate.Error
		}
		if runUpdate.RowsAffected != 1 {
			return repository.ErrRevisionConflict
		}
		if value.Verification.VerificationID != "" {
			verification := actionVerificationRow(value.Verification)
			if err := tx.Create(&verification).Error; err != nil && !isDuplicate(err) {
				return err
			}
		}
		if run.SubagentAttemptID != "" && value.ObservationID != "" {
			var sequence int64
			if err := tx.Model(&po.DecisionTurn{}).Where("attempt_id = ?", run.SubagentAttemptID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
				return err
			}
			turn := po.DecisionTurn{
				TurnID: "turn-" + ulid.New(), AttemptID: run.SubagentAttemptID, OwnerID: value.OwnerID,
				Sequence: sequence + 1, DecisionType: dso.DecisionReceiveObservation, Content: value.AttemptContent, CreatedAt: millis(recordedAt),
			}
			if err := tx.Create(&turn).Error; err != nil {
				return err
			}
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.CompleteActionChain")
}

func (s *Store) FindActionAttempt(ctx context.Context, ownerID, attemptID string) (*entity.GovernedActionAttemptRecord, error) {
	var row po.GovernedActionAttempt
	result := s.data.DB(ctx).Where("owner_id = ? AND action_attempt_id = ?", ownerID, attemptID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationStore.FindActionAttempt")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := governedActionAttemptEntity(row)
	return &value, nil
}

func actionAttemptTerminal(status string) bool {
	return status == entity.ActionSucceeded || status == entity.ActionFailed || status == entity.ActionCancelled || status == entity.ActionUnknownOutcome || status == entity.ActionPolicyDenied
}

func actionProposalRow(value entity.ActionProposalRecord) *po.ActionProposal {
	return &po.ActionProposal{ActionProposalID: value.ActionProposalID, OwnerID: value.OwnerID, GoalID: value.GoalID, TaskStepID: value.TaskStepID, DecisionTurnID: value.DecisionTurnID, SubagentRunID: value.SubagentRunID, SubagentAttemptID: value.SubagentAttemptID, Capability: value.Capability, Operation: value.Operation, ResourceRef: value.ResourceRef, ResourceVersion: value.ResourceVersion, InputHash: value.InputHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func planCandidateRow(value entity.PlanCandidateRecord) *po.PlanCandidate {
	return &po.PlanCandidate{PlanCandidateID: value.PlanCandidateID, OwnerID: value.OwnerID, TaskStepID: value.TaskStepID, ActionProposalID: value.ActionProposalID, ResourceRef: value.ResourceRef, ResourceVersion: value.ResourceVersion, DefinitionHash: value.DefinitionHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func executionContextRow(value entity.ExecutionContextRecord) *po.ExecutionContext {
	return &po.ExecutionContext{ExecutionContextID: value.ExecutionContextID, OwnerID: value.OwnerID, TaskStepID: value.TaskStepID, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func actionPolicyDecisionRow(value entity.ActionPolicyDecisionRecord) *po.ActionPolicyDecision {
	return &po.ActionPolicyDecision{PolicyDecisionID: value.PolicyDecisionID, OwnerID: value.OwnerID, PlanCandidateID: value.PlanCandidateID, ActionProposalID: value.ActionProposalID, WorldReadSetHash: value.WorldReadSetHash, InputHash: value.InputHash, PolicyVersion: value.PolicyVersion, Decision: value.Decision, Content: value.Content, DecidedAt: millis(value.DecidedAt), ExpiresAt: millis(value.ExpiresAt)}
}

func actionPlanRunRow(value entity.ActionPlanRunRecord) *po.ActionPlanRun {
	return &po.ActionPlanRun{PlanRunID: value.PlanRunID, OwnerID: value.OwnerID, PlanCandidateID: value.PlanCandidateID, ExecutionContextID: value.ExecutionContextID, SubagentRunID: value.SubagentRunID, SubagentAttemptID: value.SubagentAttemptID, Status: value.Status, Revision: value.Revision, Content: value.Content, StartedAt: millis(value.StartedAt), EndedAt: millis(value.EndedAt)}
}

func governedActionAttemptRow(value entity.GovernedActionAttemptRecord) *po.GovernedActionAttempt {
	return &po.GovernedActionAttempt{ActionAttemptID: value.ActionAttemptID, OwnerID: value.OwnerID, PlanRunID: value.PlanRunID, PlanCandidateID: value.PlanCandidateID, PolicyDecisionID: value.PolicyDecisionID, ActionProposalID: value.ActionProposalID, ResourceLeaseID: value.ResourceLeaseID, ObservationID: value.ObservationID, ResourceVersionBefore: value.ResourceVersionBefore, ResourceVersionAfter: value.ResourceVersionAfter, Status: value.Status, Revision: value.Revision, ErrorChain: value.ErrorChain, Content: value.Content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt), EndedAt: millis(value.EndedAt)}
}

func governedActionAttemptEntity(row po.GovernedActionAttempt) entity.GovernedActionAttemptRecord {
	return entity.GovernedActionAttemptRecord{ActionAttemptID: row.ActionAttemptID, OwnerID: row.OwnerID, PlanRunID: row.PlanRunID, PlanCandidateID: row.PlanCandidateID, PolicyDecisionID: row.PolicyDecisionID, ActionProposalID: row.ActionProposalID, ResourceLeaseID: row.ResourceLeaseID, ObservationID: row.ObservationID, ResourceVersionBefore: row.ResourceVersionBefore, ResourceVersionAfter: row.ResourceVersionAfter, Status: row.Status, Revision: row.Revision, ErrorChain: row.ErrorChain, Content: row.Content, CreatedAt: fromMillis(row.CreatedAt), UpdatedAt: fromMillis(row.UpdatedAt), EndedAt: fromMillis(row.EndedAt)}
}

func resourceLeaseRow(value entity.ResourceLease) po.ResourceLease {
	return po.ResourceLease{LeaseID: value.LeaseID, OwnerID: value.OwnerID, RunID: value.RunID, ResourceRef: value.ResourceRef, ResourceVersion: value.ResourceVersion, Mode: value.Mode, ActionAttemptID: value.ActionAttemptID, OwnerInstanceID: value.OwnerInstanceID, Status: value.Status, AcquiredAt: millis(value.AcquiredAt), ExpiresAt: millis(value.ExpiresAt), HeartbeatAt: millis(value.HeartbeatAt), Revision: value.Revision}
}

func actionVerificationRow(value entity.ActionVerificationRecord) po.ActionVerification {
	return po.ActionVerification{VerificationID: value.VerificationID, OwnerID: value.OwnerID, OutcomeID: value.OutcomeID, PlanRunID: value.PlanRunID, ActionAttemptID: value.ActionAttemptID, EffectClauseID: value.EffectClauseID, Status: value.Status, Confidence: value.Confidence, EvidenceRefs: value.EvidenceRefs, Content: value.Content, VerifiedAt: millis(value.VerifiedAt)}
}
