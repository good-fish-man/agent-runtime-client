package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

var delegationSecretPattern = regexp.MustCompile(`(?i)(Bearer\s+[A-Za-z0-9._~+/-]{8,}|\bsk-[A-Za-z0-9_-]{12,}|-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----)`)

type RecoveryService struct {
	store repository.RecoveryStore
	now   func() time.Time
}

func NewRecoveryService(store repository.RecoveryStore) *RecoveryService {
	return &RecoveryService{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *RecoveryService) Diagnostics(ctx context.Context, startedAt, endedAt time.Time) (dso.OperationalSLOSnapshot, error) {
	if s == nil || s.store == nil {
		return dso.OperationalSLOSnapshot{}, fmt.Errorf("recovery service is not configured")
	}
	counters, err := s.store.MeasureSLO(ctx, startedAt.UTC(), endedAt.UTC())
	if err != nil {
		return dso.OperationalSLOSnapshot{}, log.WrapError(err, "DelegationRecovery.Diagnostics.measure")
	}
	availability := 1.0
	if counters.TerminalRuns > 0 {
		availability = float64(counters.TerminalRuns-counters.FailedRuns) / float64(counters.TerminalRuns)
	}
	snapshot := dso.OperationalSLOSnapshot{
		Schema: dso.Schema, WindowStartedAt: startedAt.UTC(), WindowEndedAt: endedAt.UTC(),
		TotalRuns: counters.TotalRuns, TerminalRuns: counters.TerminalRuns, FailedRuns: counters.FailedRuns,
		RecoveredAttempts: counters.RecoveredAttempts, FencedLateResults: counters.FencedLateResults,
		DuplicateConfirmedSideEffects: counters.DuplicateConfirmedSideEffects,
		Availability:                  availability, CancelPropagationP95MS: percentile95(counters.CancelPropagationMS), GeneratedAt: s.now().UTC(),
	}
	if err := snapshot.Validate(); err != nil {
		return dso.OperationalSLOSnapshot{}, log.WrapError(err, "DelegationRecovery.Diagnostics.validate")
	}
	return snapshot, nil
}

func (s *RecoveryService) ExportOwnerData(ctx context.Context, request dso.DataLifecycleRequest) ([]byte, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("recovery service is not configured")
	}
	if err := request.Validate(); err != nil || request.Operation != dso.DataLifecycleExport {
		if err == nil {
			err = fmt.Errorf("data lifecycle request is not an export")
		}
		return nil, log.WrapError(err, "DelegationRecovery.ExportOwnerData.validate")
	}
	value, err := s.store.ExportOwnerDelegationData(ctx, request.OwnerID, s.now().UTC())
	if err != nil {
		return nil, log.WrapError(err, "DelegationRecovery.ExportOwnerData.load")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if delegationSecretPattern.Match(data) {
		return nil, fmt.Errorf("owner export was blocked because secret-like material reached the export boundary")
	}
	return data, nil
}

func (s *RecoveryService) DeleteOwnerData(ctx context.Context, request dso.DataLifecycleRequest) (entity.DeletionSummary, error) {
	if s == nil || s.store == nil {
		return entity.DeletionSummary{}, fmt.Errorf("recovery service is not configured")
	}
	if err := request.Validate(); err != nil || request.Operation != dso.DataLifecycleDelete {
		if err == nil {
			err = fmt.Errorf("data lifecycle request is not a deletion")
		}
		return entity.DeletionSummary{}, log.WrapError(err, "DelegationRecovery.DeleteOwnerData.validate")
	}
	tombstone := entity.RetentionTombstone{TombstoneID: "retention-" + ulid.New(), OwnerID: request.OwnerID, Cutoff: request.Cutoff.UTC(), RequestedBy: request.RequestedBy, CreatedAt: s.now().UTC()}
	result, err := s.store.DeleteOwnerDelegationData(ctx, tombstone)
	if err != nil {
		return entity.DeletionSummary{}, log.WrapError(err, "DelegationRecovery.DeleteOwnerData.delete")
	}
	return result, nil
}

func percentile95(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := (95*len(ordered)+99)/100 - 1
	if index < 0 {
		index = 0
	}
	return ordered[index]
}
