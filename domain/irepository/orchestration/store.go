package orchestration

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
)

type Store interface {
	CreateGoal(context.Context, entity.PersistentGoal, entity.GoalCheckpoint) error
	CreatePlannedGoal(context.Context, entity.PersistentGoal, []entity.SpecialistTask, entity.GoalCheckpoint) error
	FindGoal(context.Context, string, string) (*entity.PersistentGoal, error)
	ListGoals(context.Context, string, entity.GoalFilter) ([]entity.PersistentGoal, error)
	ListRunnableGoals(context.Context, int) ([]entity.PersistentGoal, error)
	ListTasks(context.Context, string, string) ([]entity.SpecialistTask, error)
	ListResults(context.Context, string, string, int) ([]entity.SpecialistResult, error)
	LatestCheckpoint(context.Context, string, string) (*entity.GoalCheckpoint, error)
	ListCheckpoints(context.Context, string, string, int) ([]entity.GoalCheckpoint, error)
	SaveState(context.Context, entity.PersistentGoal, []entity.SpecialistTask, *entity.SpecialistResult, entity.GoalCheckpoint, int64) error
	CreateScheduleTrigger(context.Context, string, entity.ScheduleTrigger) error
	UpdateScheduleTrigger(context.Context, string, entity.ScheduleTrigger) error
}
