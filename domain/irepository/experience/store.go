package experience

import (
	"context"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
)

type Store interface {
	GetPreference(context.Context, string) (*entity.Preference, error)
	SavePreference(context.Context, entity.Preference) (*entity.Preference, error)
	ListPendingTerminalTasks(context.Context, int) ([]entity.PendingTask, error)
	ListReadyOwners(context.Context, string, int) ([]string, error)
	Create(context.Context, *entity.StoredExperience) (bool, error)
	Find(context.Context, string, string) (*entity.Experience, error)
	List(context.Context, string, entity.ListFilter) ([]entity.Experience, int64, error)
	SearchCandidates(context.Context, string, entity.SearchRequest, int) ([]entity.SearchCandidate, error)
	DeletePayload(context.Context, string, string, time.Time) error
	DeleteAllPayloads(context.Context, string, time.Time) (int64, error)
	PurgeExpired(context.Context, time.Time, int) (int64, error)
	Stats(context.Context, string) (*entity.Stats, error)
	ModelUsage(context.Context, string, string, time.Time, time.Time) ([]entity.ModelUsage, error)

	CreateFixture(context.Context, entity.EvaluationFixture) error
	FindFixture(context.Context, string, string) (*entity.EvaluationFixture, error)
	ListFixtures(context.Context, string, int) ([]entity.EvaluationFixture, error)
	CreateSuite(context.Context, entity.EvaluationSuite) error
	FindSuite(context.Context, string, string) (*entity.EvaluationSuite, error)
	ListSuites(context.Context, string, int) ([]entity.EvaluationSuite, error)
	CreateRun(context.Context, entity.EvaluationRun, []entity.EvaluationResult) error
	ListRuns(context.Context, string, int) ([]entity.EvaluationRun, error)
	ListResults(context.Context, string, string) ([]entity.EvaluationResult, error)
}
