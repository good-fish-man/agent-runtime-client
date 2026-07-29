package runtime

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

// MediaJobRepository persists user-owned asynchronous media generations.
type MediaJobRepository interface {
	CreateMediaJob(context.Context, *entity.MediaGenerationJob) error
	UpdateMediaJob(context.Context, *entity.MediaGenerationJob) error
	FindMediaJob(context.Context, string, string) (*entity.MediaGenerationJob, error)
	ListMediaJobs(context.Context, string, string, int) ([]*entity.MediaGenerationJob, error)
	DeleteMediaJob(context.Context, string, string) error
	FailInterruptedMediaJobs(context.Context) error
}
