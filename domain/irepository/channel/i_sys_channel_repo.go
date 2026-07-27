package channel

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/channel"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysChannelRepo is the persistence port for SysChannel.
type ISysChannelRepo interface {
	Create(ctx context.Context, en *entity.SysChannel) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysChannel) (err error)
	Update(ctx context.Context, en *entity.SysChannel) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysChannel, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysChannel, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysChannel, rspPage *query.PageData, err error)
	FindByCode(ctx context.Context, code string) (en *entity.SysChannel, err error)
}
