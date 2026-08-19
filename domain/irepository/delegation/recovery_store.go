package delegation

import (
	"context"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

type RecoveryStore interface {
	LoadReplaySource(context.Context, string, string, string) (*entity.ReplaySource, error)
	CreateReplay(context.Context, entity.ReplayRecord, entity.Event) error
	CompleteReplay(context.Context, string, string, string, string, string, string, time.Time, entity.Event) error
	FindReplay(context.Context, string, string) (*entity.ReplayRecord, error)
	AcquireSchedulerLease(context.Context, string, string, time.Time, time.Duration) (entity.SchedulerLease, bool, error)
	ReleaseSchedulerLease(context.Context, string, string, int64, time.Time) error
	MeasureSLO(context.Context, time.Time, time.Time) (entity.SLOCounters, error)
	ExportOwnerDelegationData(context.Context, string, time.Time) (entity.OwnerDelegationExport, error)
	DeleteOwnerDelegationData(context.Context, entity.RetentionTombstone) (entity.DeletionSummary, error)
}
