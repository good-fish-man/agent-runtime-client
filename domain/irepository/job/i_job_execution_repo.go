package job

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/job"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// IJobExecutionRepo is the persistence port for job execution logs.
type IJobExecutionRepo interface {
	Create(ctx context.Context, en *entity.JobExecution) (ulid string, err error)
	Delete(ctx context.Context, en *entity.JobExecution) (err error)
	Update(ctx context.Context, en *entity.JobExecution) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.JobExecution, err error)
	FindByQuery(ctx context.Context, queries []*query.Query) (en *entity.JobExecution, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.JobExecution, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.JobExecution, rspPage *query.PageData, err error)
	FindByAgentId(ctx context.Context, agentId string, limit int) ([]*entity.JobExecution, error)
	DeleteOldByAgentId(ctx context.Context, agentId string, keepCount int) error
	CountByAgentId(ctx context.Context, agentId string) (int, error)
	CountByStatus(ctx context.Context, status string) (int, error)
}
