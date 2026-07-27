package user

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysLogRepo is the persistence port for SysLog.
type ISysLogRepo interface {
	Create(ctx context.Context, en *entity.SysLog) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysLog) (err error)
	Update(ctx context.Context, en *entity.SysLog) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysLog, err error)
	FindByQuery(ctx context.Context, queries []*query.Query) (en *entity.SysLog, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysLog, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysLog, rspPage *query.PageData, err error)
}
