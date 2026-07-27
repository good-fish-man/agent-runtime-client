package agent

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysAgentRepo is the persistence port for SysAgent.
type ISysAgentRepo interface {
	Create(ctx context.Context, en *entity.SysAgent) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysAgent) (err error)
	Update(ctx context.Context, en *entity.SysAgent) (err error)
	UpdateEnabled(ctx context.Context, ulid string, enabled bool) error
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysAgent, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysAgent, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysAgent, rspPage *query.PageData, err error)
	FindByName(ctx context.Context, name string) (en *entity.SysAgent, err error)
	FindUserModel(ctx context.Context, userID, agentID string) (*entity.SysAgentUserModel, error)
	UpsertUserModel(ctx context.Context, en *entity.SysAgentUserModel) error
}
