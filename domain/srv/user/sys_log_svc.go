package user

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysLogSvc is the SysLog domain service.
type SysLogSvc struct {
	repo *repo.SysLogRepo
}

// NewSysLogSvc builds the domain service over the shared data handle.
func NewSysLogSvc(d *data.Data) *SysLogSvc {
	return &SysLogSvc{repo: repo.NewSysLogRepo(d)}
}

func (s *SysLogSvc) CreateSysLog(ctx context.Context, en *entity.SysLog) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysLogSvc) DeleteSysLog(ctx context.Context, en *entity.SysLog) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysLogSvc) UpdateSysLog(ctx context.Context, en *entity.SysLog) error {
	return s.repo.Update(ctx, en)
}

func (s *SysLogSvc) FindSysLogById(ctx context.Context, ulid string) (*entity.SysLog, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysLogSvc) FindSysLogByQuery(ctx context.Context, queries []*query.Query) (*entity.SysLog, error) {
	return s.repo.FindByQuery(ctx, queries)
}

func (s *SysLogSvc) FindSysLogAll(ctx context.Context, queries []*query.Query) ([]*entity.SysLog, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *SysLogSvc) FindSysLogPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysLog, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}
