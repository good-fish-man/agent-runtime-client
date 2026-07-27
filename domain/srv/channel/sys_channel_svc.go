package channel

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/channel"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/channel"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysChannelSvc is the SysChannel domain service.
type SysChannelSvc struct {
	repo *repo.SysChannelRepo
}

// NewSysChannelSvc builds the domain service over the shared data handle.
func NewSysChannelSvc(d *data.Data) *SysChannelSvc {
	return &SysChannelSvc{repo: repo.NewSysChannelRepo(d)}
}

func (s *SysChannelSvc) Create(ctx context.Context, en *entity.SysChannel) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysChannelSvc) Delete(ctx context.Context, en *entity.SysChannel) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysChannelSvc) Update(ctx context.Context, en *entity.SysChannel) error {
	return s.repo.Update(ctx, en)
}

func (s *SysChannelSvc) FindById(ctx context.Context, ulid string) (*entity.SysChannel, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysChannelSvc) FindAll(ctx context.Context, queries []*query.Query) ([]*entity.SysChannel, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *SysChannelSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysChannel, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}

func (s *SysChannelSvc) FindByCode(ctx context.Context, code string) (*entity.SysChannel, error) {
	return s.repo.FindByCode(ctx, code)
}
