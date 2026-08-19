package delegation

import (
	"context"
	"errors"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

var (
	ErrResourceBusy  = errors.New("delegation resource already has an active writer")
	ErrResourceStale = errors.New("delegation resource version is stale")
)

// ActionStore is separate from Store so the durable delegation authority can
// evolve without forcing runtime workers or test doubles onto a second path.
type ActionStore interface {
	CreateActionChain(context.Context, entity.ActionChain) error
	AcquireActionLease(context.Context, entity.ResourceLease, string, string, time.Time, entity.Event) error
	CompleteActionChain(context.Context, entity.ActionCompletion) error
	FindActionAttempt(context.Context, string, string) (*entity.GovernedActionAttemptRecord, error)
}
