package delegation

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	log "github.com/good-fish-man/logx"
)

func (s *Store) CreateInvocationBundle(ctx context.Context, value entity.InvocationBundle) error {
	records := []entity.ImmutableRecord{value.ContextSlice, value.CapabilityView, value.ActorBinding, value.Manifest}
	for _, record := range records {
		if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.OwnerID) == "" || strings.TrimSpace(record.RunID) == "" || strings.TrimSpace(record.ContentHash) == "" || strings.TrimSpace(record.Content) == "" || record.CreatedAt.IsZero() {
			return fmt.Errorf("invocation bundle contains an incomplete immutable record")
		}
	}
	if value.ContextSlice.OwnerID != value.Manifest.OwnerID || value.CapabilityView.OwnerID != value.Manifest.OwnerID || value.ActorBinding.OwnerID != value.Manifest.OwnerID ||
		value.ContextSlice.RunID != value.Manifest.RunID || value.CapabilityView.RunID != value.Manifest.RunID || value.ActorBinding.RunID != value.Manifest.RunID {
		return fmt.Errorf("invocation bundle crosses an owner or run boundary")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var run po.Run
		if err := tx.Where("run_id = ? AND owner_id = ?", value.Manifest.RunID, value.Manifest.OwnerID).Take(&run).Error; err != nil {
			return err
		}
		if err := createImmutableRecord(tx, value.ContextSlice, &po.ContextSlice{
			ContextSliceID: value.ContextSlice.ID, OwnerID: value.ContextSlice.OwnerID, RunID: value.ContextSlice.RunID,
			ContentHash: value.ContextSlice.ContentHash, Content: value.ContextSlice.Content, CreatedAt: millis(value.ContextSlice.CreatedAt),
		}, "context_slice_id", po.ContextSlice{}); err != nil {
			return err
		}
		if err := createImmutableRecord(tx, value.CapabilityView, &po.CapabilityView{
			CapabilityViewID: value.CapabilityView.ID, OwnerID: value.CapabilityView.OwnerID, RunID: value.CapabilityView.RunID,
			ContentHash: value.CapabilityView.ContentHash, Content: value.CapabilityView.Content, CreatedAt: millis(value.CapabilityView.CreatedAt),
		}, "capability_view_id", po.CapabilityView{}); err != nil {
			return err
		}
		if err := createImmutableRecord(tx, value.ActorBinding, &po.ActorBinding{
			ActorBindingID: value.ActorBinding.ID, OwnerID: value.ActorBinding.OwnerID, RunID: value.ActorBinding.RunID,
			ContentHash: value.ActorBinding.ContentHash, Content: value.ActorBinding.Content, CreatedAt: millis(value.ActorBinding.CreatedAt),
		}, "actor_binding_id", po.ActorBinding{}); err != nil {
			return err
		}
		return createImmutableRecord(tx, value.Manifest, &po.InvocationManifest{
			ManifestID: value.Manifest.ID, OwnerID: value.Manifest.OwnerID, RunID: value.Manifest.RunID,
			ContentHash: value.Manifest.ContentHash, Content: value.Manifest.Content, CreatedAt: millis(value.Manifest.CreatedAt),
		}, "manifest_id", po.InvocationManifest{})
	}), "DelegationStore.CreateInvocationBundle")
}

func createImmutableRecord(tx *gorm.DB, record entity.ImmutableRecord, row any, idColumn string, model any) error {
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var existing struct {
		OwnerID     string `gorm:"column:owner_id"`
		RunID       string `gorm:"column:run_id"`
		ContentHash string `gorm:"column:content_hash"`
	}
	if err := tx.Model(model).Select("owner_id", "run_id", "content_hash").Where(idColumn+" = ?", record.ID).Take(&existing).Error; err != nil {
		return err
	}
	if existing.OwnerID != record.OwnerID || existing.RunID != record.RunID || existing.ContentHash != record.ContentHash {
		return ErrIdempotencyConflict
	}
	return nil
}

func (s *Store) RecordDecisionTurn(ctx context.Context, turn entity.DecisionTurn, invocation entity.ModelInvocation) error {
	if strings.TrimSpace(turn.TurnID) == "" || strings.TrimSpace(turn.AttemptID) == "" || strings.TrimSpace(turn.OwnerID) == "" || turn.Sequence <= 0 || turn.CreatedAt.IsZero() {
		return fmt.Errorf("decision turn identity, owner, sequence, and timestamp are required")
	}
	if invocation.TurnID != turn.TurnID || strings.TrimSpace(invocation.InvocationID) == "" || invocation.OwnerID != turn.OwnerID || invocation.StartedAt.IsZero() {
		return fmt.Errorf("model invocation does not belong to the decision turn")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var attempt po.Attempt
		if err := tx.Where("attempt_id = ? AND owner_id = ?", turn.AttemptID, turn.OwnerID).Take(&attempt).Error; err != nil {
			return err
		}
		if isAttemptTerminal(attempt.Status) {
			return ErrStaleAttempt
		}
		var run po.Run
		if err := tx.Where("run_id = ? AND owner_id = ?", attempt.RunID, turn.OwnerID).Take(&run).Error; err != nil {
			return err
		}
		if run.ActiveAttemptID != turn.AttemptID {
			return ErrStaleAttempt
		}
		turnRow := po.DecisionTurn{
			TurnID: turn.TurnID, AttemptID: turn.AttemptID, OwnerID: turn.OwnerID, Sequence: turn.Sequence,
			DecisionType: turn.DecisionType, Content: turn.Content, CreatedAt: millis(turn.CreatedAt),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&turnRow).Error; err != nil {
			return err
		}
		invocationRow := po.ModelInvocation{
			InvocationID: invocation.InvocationID, TurnID: invocation.TurnID, OwnerID: invocation.OwnerID,
			Provider: invocation.Provider, ModelRef: invocation.ModelRef, PromptTokens: invocation.PromptTokens,
			CompletionTokens: invocation.CompletionTokens, LatencyMS: invocation.LatencyMS, Status: invocation.Status,
			Content: invocation.Content, StartedAt: millis(invocation.StartedAt), EndedAt: millis(invocation.EndedAt),
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&invocationRow).Error
	}), "DelegationStore.RecordDecisionTurn")
}

func (s *Store) RecordCandidateResult(ctx context.Context, result entity.CandidateResult, verifications []entity.VerificationResult) error {
	if strings.TrimSpace(result.ResultID) == "" || strings.TrimSpace(result.OwnerID) == "" || strings.TrimSpace(result.RunID) == "" || strings.TrimSpace(result.AttemptID) == "" || result.CreatedAt.IsZero() {
		return fmt.Errorf("candidate result identity, owner, run, attempt, and timestamp are required")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var run po.Run
		if err := tx.Where("run_id = ? AND owner_id = ?", result.RunID, result.OwnerID).Take(&run).Error; err != nil {
			return err
		}
		if run.ActiveAttemptID != result.AttemptID {
			return ErrStaleAttempt
		}
		var attempt po.Attempt
		if err := tx.Where("attempt_id = ? AND run_id = ? AND owner_id = ?", result.AttemptID, result.RunID, result.OwnerID).Take(&attempt).Error; err != nil {
			return err
		}
		if isAttemptTerminal(attempt.Status) {
			return ErrStaleAttempt
		}
		row := po.CandidateResult{
			ResultID: result.ResultID, OwnerID: result.OwnerID, RunID: result.RunID, AttemptID: result.AttemptID,
			Status: result.Status, Content: result.Content, CreatedAt: millis(result.CreatedAt),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		for _, verification := range verifications {
			if verification.OwnerID != result.OwnerID || verification.RunID != result.RunID || verification.AttemptID != result.AttemptID || strings.TrimSpace(verification.VerificationID) == "" || verification.VerifiedAt.IsZero() {
				return fmt.Errorf("verification result crosses the candidate boundary")
			}
			verificationRow := po.VerificationResult{
				VerificationID: verification.VerificationID, OwnerID: verification.OwnerID, OutcomeID: verification.OutcomeID,
				RunID: verification.RunID, AttemptID: verification.AttemptID, EffectClauseID: verification.EffectClauseID,
				Status: verification.Status, ExpectedValue: verification.ExpectedValue, ObservedValue: verification.ObservedValue,
				EvidenceRefs: verification.EvidenceRefs, Confidence: verification.Confidence, Content: verification.Content,
				VerifiedAt: millis(verification.VerifiedAt),
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&verificationRow).Error; err != nil {
				return err
			}
		}
		return nil
	}), "DelegationStore.RecordCandidateResult")
}
