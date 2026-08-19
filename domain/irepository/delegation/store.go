package delegation

import (
	"context"
	"errors"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

var (
	ErrRevisionConflict    = errors.New("delegation revision conflict")
	ErrAttemptOwned        = errors.New("subagent run already has an active attempt owner")
	ErrStaleAttempt        = errors.New("subagent attempt is stale or superseded")
	ErrBudgetExceeded      = errors.New("parent budget exceeded")
	ErrReservationClosed   = errors.New("budget reservation is already closed")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different content")
)

type Store interface {
	CreateAcceptedDelegation(context.Context, entity.AcceptedDelegation) error
	CreateInvocationBundle(context.Context, entity.InvocationBundle) error
	RecordDecisionTurn(context.Context, entity.DecisionTurn, entity.ModelInvocation) error
	RecordCandidateResult(context.Context, entity.CandidateResult, []entity.VerificationResult) error
	FindRun(context.Context, string, string) (*entity.Run, error)
	ListRecoverableRuns(context.Context, time.Time, int) ([]entity.Run, error)
	RecoverRun(context.Context, string, string, int64, string, string, time.Time, entity.Event) error
	CreateBudgetAccount(context.Context, entity.BudgetAccount) error
	ReserveBudget(context.Context, entity.BudgetReservation, entity.Event) error
	CommitBudget(context.Context, string, string, int64, entity.BudgetAmount, time.Time, entity.Event) error
	ReleaseBudget(context.Context, string, string, int64, time.Time, entity.Event) error
	AcquireAttempt(context.Context, entity.Attempt, int64, entity.Event) error
	HeartbeatAttempt(context.Context, string, string, string, int64, time.Time, time.Time) error
	CompleteAttempt(context.Context, string, string, string, int64, string, string, time.Time, entity.Event) error
	ListRecoverableAttempts(context.Context, time.Time, int) ([]entity.Attempt, error)
	CancelRun(context.Context, string, string, string, time.Time, entity.Event) error
	ListUnpublishedEvents(context.Context, int) ([]entity.Event, error)
	MarkEventPublished(context.Context, string, time.Time) error
}
