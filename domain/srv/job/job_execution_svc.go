package job

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/job"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/job"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// JobExecution is the domain service for job execution logs.
type JobExecution struct {
	repo *repo.JobExecutionRepo
}

// NewJobExecutionSvc builds the domain service over the shared data handle.
func NewJobExecutionSvc(d *data.Data) *JobExecution {
	return &JobExecution{repo: repo.NewJobExecutionRepo(d)}
}

func (s *JobExecution) CreateJobExecution(ctx context.Context, en *entity.JobExecution) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *JobExecution) DeleteJobExecution(ctx context.Context, en *entity.JobExecution) error {
	return s.repo.Delete(ctx, en)
}

func (s *JobExecution) UpdateJobExecution(ctx context.Context, en *entity.JobExecution) error {
	return s.repo.Update(ctx, en)
}

func (s *JobExecution) FindJobExecutionById(ctx context.Context, ulid string) (*entity.JobExecution, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *JobExecution) FindJobExecutionByQuery(ctx context.Context, queries []*query.Query) (*entity.JobExecution, error) {
	return s.repo.FindByQuery(ctx, queries)
}

func (s *JobExecution) FindJobExecutionAll(ctx context.Context, queries []*query.Query) ([]*entity.JobExecution, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *JobExecution) FindJobExecutionPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.JobExecution, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}

func (s *JobExecution) FindByAgentId(ctx context.Context, agentId string, limit int) ([]*entity.JobExecution, error) {
	return s.repo.FindByAgentId(ctx, agentId, limit)
}

func (s *JobExecution) DeleteOldByAgentId(ctx context.Context, agentId string, keepCount int) error {
	return s.repo.DeleteOldByAgentId(ctx, agentId, keepCount)
}

func (s *JobExecution) CountByAgentId(ctx context.Context, agentId string) (int, error) {
	return s.repo.CountByAgentId(ctx, agentId)
}

func (s *JobExecution) CountByStatus(ctx context.Context, status string) (int, error) {
	return s.repo.CountByStatus(ctx, status)
}
