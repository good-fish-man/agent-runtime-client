package agent

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/agent"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	log "github.com/good-fish-man/logx"
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
	value, err := s.repo.Create(ctx, en)
	return value, log.WrapError(err, "SysAgentSvc.Create")
}

func (s *SysAgentSvc) Delete(ctx context.Context, en *entity.SysAgent) error {
	return log.WrapError(s.repo.Delete(ctx, en), "SysAgentSvc.Delete")
}

func (s *SysAgentSvc) Update(ctx context.Context, en *entity.SysAgent) error {
	return log.WrapError(s.repo.Update(ctx, en), "SysAgentSvc.Update")
}

func (s *SysAgentSvc) UpdateEnabled(ctx context.Context, ulid string, enabled bool) error {
	return log.WrapError(s.repo.UpdateEnabled(ctx, ulid, enabled), "SysAgentSvc.UpdateEnabled")
}

func (s *SysAgentSvc) FindById(ctx context.Context, ulid string) (*entity.SysAgent, error) {
	value, err := s.repo.FindById(ctx, ulid)
	return value, log.WrapError(err, "SysAgentSvc.FindById")
}

func (s *SysAgentSvc) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysAgent, error) {
	value, err := s.repo.FindAll(ctx, queries, selectArgs...)
	return value, log.WrapError(err, "SysAgentSvc.FindAll")
}

func (s *SysAgentSvc) FindVisible(ctx context.Context, userID, name string, selectArgs ...[]string) ([]*entity.SysAgent, error) {
	value, err := s.repo.FindVisible(ctx, userID, name, selectArgs...)
	return value, log.WrapError(err, "SysAgentSvc.FindVisible")
}

func (s *SysAgentSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysAgent, *query.PageData, error) {
	values, page, err := s.repo.FindPage(ctx, queries, reqPage, reqSort, selectArgs...)
	return values, page, log.WrapError(err, "SysAgentSvc.FindPage")
}

func (s *SysAgentSvc) FindByName(ctx context.Context, name string) (*entity.SysAgent, error) {
	value, err := s.repo.FindByName(ctx, name)
	return value, log.WrapError(err, "SysAgentSvc.FindByName")
}

func (s *SysAgentSvc) FindUserModel(ctx context.Context, userID, agentID string) (*entity.SysAgentUserModel, error) {
	value, err := s.repo.FindUserModel(ctx, userID, agentID)
	return value, log.WrapError(err, "SysAgentSvc.FindUserModel")
}

func (s *SysAgentSvc) UpsertUserModel(ctx context.Context, en *entity.SysAgentUserModel) error {
	return log.WrapError(s.repo.UpsertUserModel(ctx, en), "SysAgentSvc.UpsertUserModel")
}

// FindPeriodicEnabled finds all non-deleted periodic agents.
func (s *SysAgentSvc) FindPeriodicEnabled(ctx context.Context) ([]*entity.SysAgent, error) {
	queries := []*query.Query{
		{Key: "is_periodic", Operator: query.OpEq, Value: true},
		{Key: "deleted_at", Operator: query.OpEq, Value: 0},
	}
	value, err := s.repo.FindAll(ctx, queries)
	return value, log.WrapError(err, "SysAgentSvc.FindPeriodicEnabled")
}
