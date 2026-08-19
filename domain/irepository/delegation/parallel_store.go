package delegation

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

// ParallelStore persists one immutable DAG definition and its mutable run
// state. Individual specialist Run and Attempt records remain authoritative
// for model execution; this store owns only coordination state.
type ParallelStore interface {
	CreateParallelPlan(context.Context, entity.ParallelPlanBundle) error
	TransitionParallelNode(context.Context, entity.ParallelNodeTransition) error
	CompleteParallelPlan(context.Context, entity.ParallelPlanCompletion) error
	FindParallelPlan(context.Context, string, string) (*entity.ParallelPlan, []entity.ParallelNode, *entity.ParallelAggregate, error)
}
