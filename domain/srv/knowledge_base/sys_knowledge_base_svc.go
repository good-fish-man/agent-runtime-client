package knowledge_base

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysKnowledgeBaseSvc is the SysKnowledgeBase domain service.
type SysKnowledgeBaseSvc struct {
	repo *repo.SysKnowledgeBaseRepo
}

// NewSysKnowledgeBaseSvc builds the domain service over the shared data handle.
func NewSysKnowledgeBaseSvc(d *data.Data) *SysKnowledgeBaseSvc {
	return &SysKnowledgeBaseSvc{repo: repo.NewSysKnowledgeBaseRepo(d)}
}

func (s *SysKnowledgeBaseSvc) Create(ctx context.Context, en *entity.SysKnowledgeBase) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysKnowledgeBaseSvc) Delete(ctx context.Context, en *entity.SysKnowledgeBase) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysKnowledgeBaseSvc) Update(ctx context.Context, en *entity.SysKnowledgeBase) error {
	return s.repo.Update(ctx, en)
}

func (s *SysKnowledgeBaseSvc) FindById(ctx context.Context, ulid string) (*entity.SysKnowledgeBase, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysKnowledgeBaseSvc) FindAll(ctx context.Context, queries []*query.Query) ([]*entity.SysKnowledgeBase, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *SysKnowledgeBaseSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysKnowledgeBase, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}
