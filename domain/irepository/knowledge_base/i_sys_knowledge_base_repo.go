package knowledge_base

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysKnowledgeBaseRepo is the persistence port for SysKnowledgeBase.
type ISysKnowledgeBaseRepo interface {
	Create(ctx context.Context, en *entity.SysKnowledgeBase) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysKnowledgeBase) (err error)
	Update(ctx context.Context, en *entity.SysKnowledgeBase) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysKnowledgeBase, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysKnowledgeBase, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysKnowledgeBase, rspPage *query.PageData, err error)
}
