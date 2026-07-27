package agent

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/agent"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysAgentSvc is the SysAgent domain service.
type SysAgentSvc struct {
	repo *repo.SysAgentRepo
}

// NewSysAgentSvc builds the domain service over the shared data handle.
func NewSysAgentSvc(d *data.Data) *SysAgentSvc {
	return &SysAgentSvc{repo: repo.NewSysAgentRepo(d)}
}

func (s *SysAgentSvc) Create(ctx context.Context, en *entity.SysAgent) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysAgentSvc) Delete(ctx context.Context, en *entity.SysAgent) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysAgentSvc) Update(ctx context.Context, en *entity.SysAgent) error {
	return s.repo.Update(ctx, en)
}

func (s *SysAgentSvc) UpdateEnabled(ctx context.Context, ulid string, enabled bool) error {
	return s.repo.UpdateEnabled(ctx, ulid, enabled)
}

func (s *SysAgentSvc) FindById(ctx context.Context, ulid string) (*entity.SysAgent, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysAgentSvc) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysAgent, error) {
	return s.repo.FindAll(ctx, queries, selectArgs...)
}

func (s *SysAgentSvc) FindVisible(ctx context.Context, userID, name string, selectArgs ...[]string) ([]*entity.SysAgent, error) {
	return s.repo.FindVisible(ctx, userID, name, selectArgs...)
}

func (s *SysAgentSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysAgent, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort, selectArgs...)
}

func (s *SysAgentSvc) FindByName(ctx context.Context, name string) (*entity.SysAgent, error) {
	return s.repo.FindByName(ctx, name)
}

func (s *SysAgentSvc) FindUserModel(ctx context.Context, userID, agentID string) (*entity.SysAgentUserModel, error) {
	return s.repo.FindUserModel(ctx, userID, agentID)
}

func (s *SysAgentSvc) UpsertUserModel(ctx context.Context, en *entity.SysAgentUserModel) error {
	return s.repo.UpsertUserModel(ctx, en)
}

// FindPeriodicEnabled finds all non-deleted periodic agents.
func (s *SysAgentSvc) FindPeriodicEnabled(ctx context.Context) ([]*entity.SysAgent, error) {
	queries := []*query.Query{
		{Key: "is_periodic", Operator: query.OpEq, Value: true},
		{Key: "deleted_at", Operator: query.OpEq, Value: 0},
	}
	return s.repo.FindAll(ctx, queries)
}
