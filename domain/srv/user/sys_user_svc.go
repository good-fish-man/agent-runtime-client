package user

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysUserSvc is the SysUser domain service.
type SysUserSvc struct {
	repo *repo.SysUserRepo
}

// NewSysUserSvc builds the domain service over the shared data handle.
func NewSysUserSvc(d *data.Data) *SysUserSvc {
	return &SysUserSvc{repo: repo.NewSysUserRepo(d)}
}

func (s *SysUserSvc) CreateSysUser(ctx context.Context, en *entity.SysUser) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysUserSvc) DeleteSysUser(ctx context.Context, en *entity.SysUser) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysUserSvc) UpdateSysUser(ctx context.Context, en *entity.SysUser) error {
	return s.repo.Update(ctx, en)
}

func (s *SysUserSvc) FindSysUserById(ctx context.Context, ulid string) (*entity.SysUser, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysUserSvc) FindSysUserByQuery(ctx context.Context, queries []*query.Query) (*entity.SysUser, error) {
	return s.repo.FindByQuery(ctx, queries)
}

func (s *SysUserSvc) FindSysUserAll(ctx context.Context, queries []*query.Query) ([]*entity.SysUser, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *SysUserSvc) FindSysUserPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysUser, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}
