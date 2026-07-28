package user

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
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
	value, err := s.repo.Create(ctx, en)
	return value, errtrace.Wrap(err, "SysUserSvc.CreateSysUser")
}

func (s *SysUserSvc) DeleteSysUser(ctx context.Context, en *entity.SysUser) error {
	return errtrace.Wrap(s.repo.Delete(ctx, en), "SysUserSvc.DeleteSysUser")
}

func (s *SysUserSvc) UpdateSysUser(ctx context.Context, en *entity.SysUser) error {
	return errtrace.Wrap(s.repo.Update(ctx, en), "SysUserSvc.UpdateSysUser")
}

func (s *SysUserSvc) FindSysUserById(ctx context.Context, ulid string) (*entity.SysUser, error) {
	value, err := s.repo.FindById(ctx, ulid)
	return value, errtrace.Wrap(err, "SysUserSvc.FindSysUserById")
}

func (s *SysUserSvc) FindSysUserByQuery(ctx context.Context, queries []*query.Query) (*entity.SysUser, error) {
	value, err := s.repo.FindByQuery(ctx, queries)
	return value, errtrace.Wrap(err, "SysUserSvc.FindSysUserByQuery")
}

func (s *SysUserSvc) FindSysUserAll(ctx context.Context, queries []*query.Query) ([]*entity.SysUser, error) {
	value, err := s.repo.FindAll(ctx, queries)
	return value, errtrace.Wrap(err, "SysUserSvc.FindSysUserAll")
}

func (s *SysUserSvc) FindSysUserPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysUser, *query.PageData, error) {
	values, page, err := s.repo.FindPage(ctx, queries, reqPage, reqSort)
	return values, page, errtrace.Wrap(err, "SysUserSvc.FindSysUserPage")
}
