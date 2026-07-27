package model

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysModelSvc is the SysModel domain service.
type SysModelSvc struct {
	repo *repo.SysModelRepo
}

// NewSysModelSvc builds the domain service over the shared data handle.
func NewSysModelSvc(d *data.Data) *SysModelSvc {
	return &SysModelSvc{repo: repo.NewSysModelRepo(d)}
}

func (s *SysModelSvc) Create(ctx context.Context, en *entity.SysModel) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysModelSvc) Delete(ctx context.Context, en *entity.SysModel) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysModelSvc) Update(ctx context.Context, en *entity.SysModel) error {
	return s.repo.Update(ctx, en)
}

func (s *SysModelSvc) UpdateEnabled(ctx context.Context, ulid, updatedBy string, enabled bool) error {
	return s.repo.UpdateEnabled(ctx, ulid, updatedBy, enabled)
}

func (s *SysModelSvc) UpdateRuntimeMode(ctx context.Context, ulid, updatedBy, runtimeMode string) error {
	return s.repo.UpdateRuntimeMode(ctx, ulid, updatedBy, runtimeMode)
}

func (s *SysModelSvc) FindById(ctx context.Context, ulid string) (*entity.SysModel, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysModelSvc) FindAll(ctx context.Context, queries []*query.Query) ([]*entity.SysModel, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *SysModelSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysModel, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}

func (s *SysModelSvc) FindCatalog(ctx context.Context, modelType, provider string) ([]*entity.ModelCatalog, error) {
	return s.repo.FindCatalog(ctx, modelType, provider)
}

func (s *SysModelSvc) CreateKey(ctx context.Context, key *entity.SysModelKey) (string, error) {
	return s.repo.CreateKey(ctx, key)
}
func (s *SysModelSvc) UpdateKey(ctx context.Context, key *entity.SysModelKey) error {
	return s.repo.UpdateKey(ctx, key)
}
func (s *SysModelSvc) DeleteKey(ctx context.Context, keyID, userID string) error {
	return s.repo.DeleteKey(ctx, keyID, userID)
}
func (s *SysModelSvc) FindKeyByID(ctx context.Context, keyID string) (*entity.SysModelKey, error) {
	return s.repo.FindKeyByID(ctx, keyID)
}
func (s *SysModelSvc) FindKeysByUser(ctx context.Context, userID string) ([]*entity.SysModelKey, error) {
	return s.repo.FindKeysByUser(ctx, userID)
}
func (s *SysModelSvc) CountModelsByKey(ctx context.Context, keyID, userID string) (int64, error) {
	return s.repo.CountModelsByKey(ctx, keyID, userID)
}
