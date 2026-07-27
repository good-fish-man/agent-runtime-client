package user

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysUserRepo is the persistence port for SysUser.
type ISysUserRepo interface {
	Create(ctx context.Context, en *entity.SysUser) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysUser) (err error)
	Update(ctx context.Context, en *entity.SysUser) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysUser, err error)
	FindByQuery(ctx context.Context, queries []*query.Query) (en *entity.SysUser, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysUser, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysUser, rspPage *query.PageData, err error)
}
