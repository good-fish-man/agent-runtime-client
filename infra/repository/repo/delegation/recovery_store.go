package delegation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

var _ repository.RecoveryStore = (*Store)(nil)

func (s *Store) LoadReplaySource(ctx context.Context, ownerID, runID, manifestID string) (*entity.ReplaySource, error) {
	var runRow po.Run
	result := s.data.DB(ctx).Where("owner_id = ? AND run_id = ?", ownerID, runID).Limit(1).Find(&runRow)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationRecoveryStore.LoadReplaySource.findRun")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	var manifestRow po.InvocationManifest
	if err := s.data.DB(ctx).Where("owner_id = ? AND run_id = ? AND manifest_id = ?", ownerID, runID, manifestID).Take(&manifestRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.findManifest")
	}
	manifest, err := dso.DecodeInvocationManifest([]byte(manifestRow.Content))
	if err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.decodeManifest")
	}

	contextRow, err := loadReplayImmutable[po.ContextSlice](s.data.DB(ctx), "context_slice_id", manifest.ContextSliceRef, ownerID, runID)
	if err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.contextSlice")
	}
	capabilityRow, err := loadReplayImmutable[po.CapabilityView](s.data.DB(ctx), "capability_view_id", manifest.CapabilityViewRef, ownerID, runID)
	if err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.capabilityView")
	}
	actorRow, err := loadReplayImmutable[po.ActorBinding](s.data.DB(ctx), "actor_binding_id", runRow.ActorBindingID, ownerID, runID)
	if err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.actorBinding")
	}
	var specRow po.SubagentSpec
	if err := s.data.DB(ctx).Where("owner_id = ? AND spec_id = ?", ownerID, runRow.SubagentSpecID).Take(&specRow).Error; err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.subagentSpec")
	}
	var outcomeRow po.DelegatedOutcome
	if err := s.data.DB(ctx).Where("owner_id = ? AND outcome_id = ?", ownerID, runRow.DelegatedOutcomeID).Take(&outcomeRow).Error; err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.delegatedOutcome")
	}
	var verificationRows []po.VerificationResult
	if err := s.data.DB(ctx).Where("owner_id = ? AND run_id = ?", ownerID, runID).Order("verification_id ASC").Find(&verificationRows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.verifications")
	}
	verificationRefs := make([]string, 0, len(verificationRows))
	for _, row := range verificationRows {
		verificationRefs = append(verificationRefs, row.VerificationID)
	}
	var observationRows []struct {
		ObservationID string `gorm:"column:observation_id"`
	}
	if err := s.data.DB(ctx).Table(po.GovernedActionAttempt{}.TableName()+" AS action_attempt").
		Select("DISTINCT action_attempt.observation_id").
		Joins("JOIN "+po.ActionPlanRun{}.TableName()+" AS plan_run ON plan_run.plan_run_id = action_attempt.plan_run_id AND plan_run.owner_id = action_attempt.owner_id").
		Where("action_attempt.owner_id = ? AND plan_run.subagent_run_id = ? AND action_attempt.observation_id <> ?", ownerID, runID, "").
		Order("action_attempt.observation_id ASC").Scan(&observationRows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationRecoveryStore.LoadReplaySource.observations")
	}
	observationRefs := make([]string, 0, len(observationRows))
	for _, row := range observationRows {
		observationRefs = append(observationRefs, row.ObservationID)
	}

	return &entity.ReplaySource{
		Run:              runEntity(runRow),
		Manifest:         immutableFromManifest(manifestRow),
		ContextSlice:     immutableFromContext(*contextRow),
		CapabilityView:   immutableFromCapability(*capabilityRow),
		ActorBinding:     immutableFromActor(*actorRow),
		SubagentSpec:     entity.Definition{ID: specRow.SpecID, OwnerID: specRow.OwnerID, TaskStepID: specRow.TaskStepID, DefinitionHash: specRow.DefinitionHash, Content: specRow.Content, CreatedAt: fromMillis(specRow.CreatedAt)},
		DelegatedOutcome: entity.Definition{ID: outcomeRow.OutcomeID, OwnerID: outcomeRow.OwnerID, TaskStepID: outcomeRow.TaskStepID, DefinitionHash: outcomeRow.DefinitionHash, Content: outcomeRow.Content, CreatedAt: fromMillis(outcomeRow.CreatedAt)},
		ObservationRefs:  observationRefs,
		VerificationRefs: verificationRefs,
	}, nil
}

func (s *Store) CreateReplay(ctx context.Context, value entity.ReplayRecord, event entity.Event) error {
	if strings.TrimSpace(value.ReplayID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.SourceRunID) == "" || strings.TrimSpace(value.SourceManifestID) == "" || strings.TrimSpace(value.RequestContent) == "" || value.CreatedAt.IsZero() || value.StartedAt.IsZero() {
		return fmt.Errorf("replay record requires identity, owner, source, request, and timestamps")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.Replay
		found := tx.Where("replay_id = ?", value.ReplayID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.OwnerID == value.OwnerID && existing.SourceRunID == value.SourceRunID && existing.SourceManifestHash == value.SourceManifestHash && existing.Mode == value.Mode && existing.RequestContent == value.RequestContent {
				return nil
			}
			return ErrIdempotencyConflict
		}
		row := replayRow(value)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return createEvent(tx, event)
	}), "DelegationRecoveryStore.CreateReplay")
}

func (s *Store) CompleteReplay(ctx context.Context, ownerID, replayID, status, resultContent, resultHash, errorRef string, endedAt time.Time, event entity.Event) error {
	if status != dso.ReplayCompleted && status != dso.ReplayFailed && status != dso.ReplayCancelled {
		return fmt.Errorf("complete replay requires a terminal status")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var row po.Replay
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_id = ? AND replay_id = ?", ownerID, replayID).Take(&row).Error; err != nil {
			return err
		}
		if row.Status == status && row.ResultHash == resultHash && row.ErrorRef == errorRef {
			return nil
		}
		if row.Status == dso.ReplayCompleted || row.Status == dso.ReplayFailed || row.Status == dso.ReplayCancelled {
			return ErrIdempotencyConflict
		}
		updated := tx.Model(&po.Replay{}).Where("owner_id = ? AND replay_id = ? AND status IN ?", ownerID, replayID, []string{dso.ReplayRequested, dso.ReplayRunning}).Updates(map[string]any{
			"status": status, "result_content": resultContent, "result_hash": resultHash,
			"error_ref": errorRef, "ended_at": millis(endedAt),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationRecoveryStore.CompleteReplay")
}

func (s *Store) FindReplay(ctx context.Context, ownerID, replayID string) (*entity.ReplayRecord, error) {
	var row po.Replay
	result := s.data.DB(ctx).Where("owner_id = ? AND replay_id = ?", ownerID, replayID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationRecoveryStore.FindReplay")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := replayEntity(row)
	return &value, nil
}

func (s *Store) AcquireSchedulerLease(ctx context.Context, leaseKey, instanceID string, now time.Time, ttl time.Duration) (entity.SchedulerLease, bool, error) {
	if strings.TrimSpace(leaseKey) == "" || strings.TrimSpace(instanceID) == "" || ttl <= 0 {
		return entity.SchedulerLease{}, false, fmt.Errorf("scheduler lease requires key, owner instance, and positive TTL")
	}
	now = now.UTC()
	var acquired entity.SchedulerLease
	owned := false
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var row po.SchedulerLease
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("lease_key = ?", leaseKey).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = po.SchedulerLease{LeaseKey: leaseKey, OwnerInstanceID: instanceID, FencingToken: 1, Status: dso.SchedulerLeaseActive, AcquiredAt: millis(now), HeartbeatAt: millis(now), ExpiresAt: millis(now.Add(ttl)), Revision: 1}
			if createErr := tx.Create(&row).Error; createErr != nil {
				if isDuplicate(createErr) {
					return ErrRevisionConflict
				}
				return createErr
			}
			acquired, owned = schedulerLeaseEntity(row), true
			return nil
		}
		if err != nil {
			return err
		}
		if row.OwnerInstanceID != instanceID && row.Status == dso.SchedulerLeaseActive && row.ExpiresAt > millis(now) {
			acquired, owned = schedulerLeaseEntity(row), false
			return nil
		}
		updates := map[string]any{"owner_instance_id": instanceID, "status": dso.SchedulerLeaseActive, "heartbeat_at": millis(now), "expires_at": millis(now.Add(ttl)), "revision": row.Revision + 1}
		if row.OwnerInstanceID != instanceID || row.Status != dso.SchedulerLeaseActive || row.ExpiresAt <= millis(now) {
			updates["fencing_token"] = row.FencingToken + 1
			updates["acquired_at"] = millis(now)
		}
		updated := tx.Model(&po.SchedulerLease{}).Where("lease_key = ? AND revision = ?", leaseKey, row.Revision).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		if token, ok := updates["fencing_token"].(int64); ok {
			row.FencingToken = token
		}
		row.OwnerInstanceID, row.Status = instanceID, dso.SchedulerLeaseActive
		row.HeartbeatAt, row.ExpiresAt, row.Revision = millis(now), millis(now.Add(ttl)), row.Revision+1
		if acquiredAt, ok := updates["acquired_at"].(int64); ok {
			row.AcquiredAt = acquiredAt
		}
		acquired, owned = schedulerLeaseEntity(row), true
		return nil
	})
	if err != nil {
		return entity.SchedulerLease{}, false, log.WrapError(err, "DelegationRecoveryStore.AcquireSchedulerLease")
	}
	return acquired, owned, nil
}

func (s *Store) ReleaseSchedulerLease(ctx context.Context, leaseKey, instanceID string, fencingToken int64, at time.Time) error {
	updated := s.data.DB(ctx).Model(&po.SchedulerLease{}).Where("lease_key = ? AND owner_instance_id = ? AND fencing_token = ? AND status = ?", leaseKey, instanceID, fencingToken, dso.SchedulerLeaseActive).Updates(map[string]any{
		"status": dso.SchedulerLeaseReleased, "expires_at": millis(at), "heartbeat_at": millis(at), "revision": gorm.Expr("revision + 1"),
	})
	if updated.Error != nil {
		return log.WrapError(updated.Error, "DelegationRecoveryStore.ReleaseSchedulerLease")
	}
	if updated.RowsAffected != 1 {
		return ErrStaleAttempt
	}
	return nil
}

func (s *Store) MeasureSLO(ctx context.Context, startedAt, endedAt time.Time) (entity.SLOCounters, error) {
	if !endedAt.After(startedAt) {
		return entity.SLOCounters{}, fmt.Errorf("SLO measurement window is invalid")
	}
	db := s.data.DB(ctx)
	startMS, endMS := millis(startedAt), millis(endedAt)
	var counters entity.SLOCounters
	if err := db.Model(&po.Run{}).Where("created_at >= ? AND created_at < ?", startMS, endMS).Count(&counters.TotalRuns).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.totalRuns")
	}
	if err := db.Model(&po.Run{}).Where("created_at >= ? AND created_at < ? AND status IN ?", startMS, endMS, []string{entity.RunCompleted, entity.RunFailed, entity.RunCancelled, entity.RunExpired}).Count(&counters.TerminalRuns).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.terminalRuns")
	}
	if err := db.Model(&po.Run{}).Where("created_at >= ? AND created_at < ? AND status IN ?", startMS, endMS, []string{entity.RunFailed, entity.RunExpired}).Count(&counters.FailedRuns).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.failedRuns")
	}
	if err := db.Model(&po.Event{}).Where("created_at >= ? AND created_at < ? AND type IN ?", startMS, endMS, []string{"SubagentAttemptTimedOut", "SubagentAttemptAbandoned", "SubagentRunRecovered"}).Count(&counters.RecoveredAttempts).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.recoveredAttempts")
	}
	if err := db.Model(&po.Event{}).Where("created_at >= ? AND created_at < ? AND type = ?", startMS, endMS, "LateResultFenced").Count(&counters.FencedLateResults).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.fencedLateResults")
	}
	var duplicateRows int64
	duplicateQuery := db.Model(&po.GovernedActionAttempt{}).
		Select("action_proposal_id").
		Where("created_at >= ? AND created_at < ? AND status = ?", startMS, endMS, dso.ActionSucceeded).
		Group("action_proposal_id").
		Having("COUNT(*) > 1")
	if err := db.Table("(?) AS duplicate_actions", duplicateQuery).Count(&duplicateRows).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.duplicateSideEffects")
	}
	counters.DuplicateConfirmedSideEffects = duplicateRows
	var cancellationRows []po.Run
	if err := db.Where("created_at >= ? AND created_at < ? AND cancel_requested_at > 0", startMS, endMS).Find(&cancellationRows).Error; err != nil {
		return counters, log.WrapError(err, "DelegationRecoveryStore.MeasureSLO.cancellations")
	}
	for _, row := range cancellationRows {
		duration := row.UpdatedAt - row.CancelRequestedAt
		if duration < 0 {
			duration = 0
		}
		counters.CancelPropagationMS = append(counters.CancelPropagationMS, duration)
	}
	return counters, nil
}

func (s *Store) ExportOwnerDelegationData(ctx context.Context, ownerID string, exportedAt time.Time) (entity.OwnerDelegationExport, error) {
	result := entity.OwnerDelegationExport{Schema: "athena.dso.owner-export.v1", OwnerID: ownerID, ExportedAt: exportedAt.UTC()}
	var runRows []po.Run
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at ASC").Find(&runRows).Error; err != nil {
		return result, log.WrapError(err, "DelegationRecoveryStore.ExportOwnerDelegationData.runs")
	}
	for _, row := range runRows {
		result.Runs = append(result.Runs, runEntity(row))
	}
	var replayRows []po.Replay
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at ASC").Find(&replayRows).Error; err != nil {
		return result, log.WrapError(err, "DelegationRecoveryStore.ExportOwnerDelegationData.replays")
	}
	for _, row := range replayRows {
		result.Replays = append(result.Replays, replayEntity(row))
	}
	var eventRows []po.Event
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at ASC").Find(&eventRows).Error; err != nil {
		return result, log.WrapError(err, "DelegationRecoveryStore.ExportOwnerDelegationData.events")
	}
	for _, row := range eventRows {
		result.Events = append(result.Events, eventEntity(row))
	}
	var manifestRows []po.InvocationManifest
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at ASC").Find(&manifestRows).Error; err != nil {
		return result, log.WrapError(err, "DelegationRecoveryStore.ExportOwnerDelegationData.manifests")
	}
	for _, row := range manifestRows {
		result.Manifests = append(result.Manifests, immutableFromManifest(row))
	}
	return result, nil
}

func (s *Store) DeleteOwnerDelegationData(ctx context.Context, tombstone entity.RetentionTombstone) (entity.DeletionSummary, error) {
	if strings.TrimSpace(tombstone.TombstoneID) == "" || strings.TrimSpace(tombstone.OwnerID) == "" || strings.TrimSpace(tombstone.RequestedBy) == "" || tombstone.Cutoff.IsZero() || tombstone.CreatedAt.IsZero() {
		return entity.DeletionSummary{}, fmt.Errorf("retention deletion requires tombstone, owner, requester, cutoff, and timestamp")
	}
	summary := entity.DeletionSummary{OwnerID: tombstone.OwnerID, Cutoff: tombstone.Cutoff, TombstoneID: tombstone.TombstoneID, CompletedAt: tombstone.CreatedAt}
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.RetentionTombstone
		found := tx.Where("tombstone_id = ?", tombstone.TombstoneID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.OwnerID != tombstone.OwnerID || existing.Cutoff != millis(tombstone.Cutoff) {
				return ErrIdempotencyConflict
			}
			summary.DeletedRows = existing.DeletedRows
			return nil
		}
		deleted, err := deleteOwnerPayloadRows(tx, tombstone.OwnerID, millis(tombstone.Cutoff))
		if err != nil {
			return err
		}
		summary.DeletedRows = deleted
		return tx.Create(&po.RetentionTombstone{TombstoneID: tombstone.TombstoneID, OwnerID: tombstone.OwnerID, Cutoff: millis(tombstone.Cutoff), DeletedRows: deleted, RequestedBy: tombstone.RequestedBy, CreatedAt: millis(tombstone.CreatedAt)}).Error
	})
	if err != nil {
		return entity.DeletionSummary{}, log.WrapError(err, "DelegationRecoveryStore.DeleteOwnerDelegationData")
	}
	return summary, nil
}

func loadReplayImmutable[T any](db *gorm.DB, idColumn, id, ownerID, runID string) (*T, error) {
	var row T
	if err := db.Where(idColumn+" = ? AND owner_id = ? AND run_id = ?", id, ownerID, runID).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func immutableFromManifest(row po.InvocationManifest) entity.ImmutableRecord {
	return entity.ImmutableRecord{ID: row.ManifestID, OwnerID: row.OwnerID, RunID: row.RunID, ContentHash: row.ContentHash, Content: row.Content, CreatedAt: fromMillis(row.CreatedAt)}
}

func immutableFromContext(row po.ContextSlice) entity.ImmutableRecord {
	return entity.ImmutableRecord{ID: row.ContextSliceID, OwnerID: row.OwnerID, RunID: row.RunID, ContentHash: row.ContentHash, Content: row.Content, CreatedAt: fromMillis(row.CreatedAt)}
}

func immutableFromCapability(row po.CapabilityView) entity.ImmutableRecord {
	return entity.ImmutableRecord{ID: row.CapabilityViewID, OwnerID: row.OwnerID, RunID: row.RunID, ContentHash: row.ContentHash, Content: row.Content, CreatedAt: fromMillis(row.CreatedAt)}
}

func immutableFromActor(row po.ActorBinding) entity.ImmutableRecord {
	return entity.ImmutableRecord{ID: row.ActorBindingID, OwnerID: row.OwnerID, RunID: row.RunID, ContentHash: row.ContentHash, Content: row.Content, CreatedAt: fromMillis(row.CreatedAt)}
}

func replayRow(value entity.ReplayRecord) po.Replay {
	return po.Replay{ReplayID: value.ReplayID, OwnerID: value.OwnerID, SourceRunID: value.SourceRunID, SourceManifestID: value.SourceManifestID, SourceManifestHash: value.SourceManifestHash, Mode: value.Mode, Status: value.Status, RequestedBy: value.RequestedBy, LiveApprovalRef: value.LiveApprovalRef, RequestContent: value.RequestContent, ResultContent: value.ResultContent, ResultHash: value.ResultHash, ErrorRef: value.ErrorRef, CreatedAt: millis(value.CreatedAt), StartedAt: millis(value.StartedAt), EndedAt: millis(value.EndedAt)}
}

func replayEntity(row po.Replay) entity.ReplayRecord {
	return entity.ReplayRecord{ReplayID: row.ReplayID, OwnerID: row.OwnerID, SourceRunID: row.SourceRunID, SourceManifestID: row.SourceManifestID, SourceManifestHash: row.SourceManifestHash, Mode: row.Mode, Status: row.Status, RequestedBy: row.RequestedBy, LiveApprovalRef: row.LiveApprovalRef, RequestContent: row.RequestContent, ResultContent: row.ResultContent, ResultHash: row.ResultHash, ErrorRef: row.ErrorRef, CreatedAt: fromMillis(row.CreatedAt), StartedAt: fromMillis(row.StartedAt), EndedAt: fromMillis(row.EndedAt)}
}

func schedulerLeaseEntity(row po.SchedulerLease) entity.SchedulerLease {
	return entity.SchedulerLease{LeaseKey: row.LeaseKey, OwnerInstanceID: row.OwnerInstanceID, FencingToken: row.FencingToken, Status: row.Status, AcquiredAt: fromMillis(row.AcquiredAt), HeartbeatAt: fromMillis(row.HeartbeatAt), ExpiresAt: fromMillis(row.ExpiresAt), Revision: row.Revision}
}

func deleteOwnerPayloadRows(tx *gorm.DB, ownerID string, cutoff int64) (int64, error) {
	tables := []struct {
		model      any
		timeColumn string
	}{
		{&po.Replay{}, "created_at"}, {&po.Event{}, "created_at"}, {&po.VerificationResult{}, "verified_at"},
		{&po.CandidateResult{}, "created_at"}, {&po.ModelInvocation{}, "started_at"}, {&po.DecisionTurn{}, "created_at"},
		{&po.InvocationManifest{}, "created_at"}, {&po.ActorBinding{}, "created_at"}, {&po.CapabilityView{}, "created_at"},
		{&po.ContextSlice{}, "created_at"}, {&po.Attempt{}, "started_at"}, {&po.Run{}, "created_at"},
		{&po.Decision{}, "created_at"}, {&po.Proposal{}, "created_at"}, {&po.DelegatedOutcome{}, "created_at"}, {&po.SubagentSpec{}, "created_at"},
	}
	var deleted int64
	for _, table := range tables {
		result := tx.Where("owner_id = ? AND "+table.timeColumn+" < ?", ownerID, cutoff).Delete(table.model)
		if result.Error != nil {
			return 0, result.Error
		}
		deleted += result.RowsAffected
	}
	return deleted, nil
}
