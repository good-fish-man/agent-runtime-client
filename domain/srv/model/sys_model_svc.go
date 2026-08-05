package model

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	log "github.com/good-fish-man/logx"
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
	value, err := s.repo.Create(ctx, en)
	return value, log.WrapError(err, "SysModelSvc.Create")
}

func (s *SysModelSvc) Delete(ctx context.Context, en *entity.SysModel) error {
	return log.WrapError(s.repo.Delete(ctx, en), "SysModelSvc.Delete")
}

func (s *SysModelSvc) Update(ctx context.Context, en *entity.SysModel) error {
	return log.WrapError(s.repo.Update(ctx, en), "SysModelSvc.Update")
}

func (s *SysModelSvc) UpdateEnabled(ctx context.Context, ulid, updatedBy string, enabled bool) error {
	return log.WrapError(s.repo.UpdateEnabled(ctx, ulid, updatedBy, enabled), "SysModelSvc.UpdateEnabled")
}

func (s *SysModelSvc) UpdateRuntimeMode(ctx context.Context, ulid, updatedBy, runtimeMode string) error {
	return log.WrapError(s.repo.UpdateRuntimeMode(ctx, ulid, updatedBy, runtimeMode), "SysModelSvc.UpdateRuntimeMode")
}

func (s *SysModelSvc) FindById(ctx context.Context, ulid string) (*entity.SysModel, error) {
	value, err := s.repo.FindById(ctx, ulid)
	return value, log.WrapError(err, "SysModelSvc.FindById")
}

func (s *SysModelSvc) FindAll(ctx context.Context, queries []*query.Query) ([]*entity.SysModel, error) {
	value, err := s.repo.FindAll(ctx, queries)
	return value, log.WrapError(err, "SysModelSvc.FindAll")
}

func (s *SysModelSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysModel, *query.PageData, error) {
	values, page, err := s.repo.FindPage(ctx, queries, reqPage, reqSort)
	return values, page, log.WrapError(err, "SysModelSvc.FindPage")
}

func (s *SysModelSvc) FindCatalog(ctx context.Context, modelType, provider string) ([]*entity.ModelCatalog, error) {
	value, err := s.repo.FindCatalog(ctx, modelType, provider)
	return value, log.WrapError(err, "SysModelSvc.FindCatalog")
}

func (s *SysModelSvc) CreateKey(ctx context.Context, key *entity.SysModelKey) (string, error) {
	value, err := s.repo.CreateKey(ctx, key)
	return value, log.WrapError(err, "SysModelSvc.CreateKey")
}
func (s *SysModelSvc) UpdateKey(ctx context.Context, key *entity.SysModelKey) error {
	return log.WrapError(s.repo.UpdateKey(ctx, key), "SysModelSvc.UpdateKey")
}
func (s *SysModelSvc) DeleteKey(ctx context.Context, keyID, userID string) error {
	return log.WrapError(s.repo.DeleteKey(ctx, keyID, userID), "SysModelSvc.DeleteKey")
}
func (s *SysModelSvc) FindKeyByID(ctx context.Context, keyID string) (*entity.SysModelKey, error) {
	value, err := s.repo.FindKeyByID(ctx, keyID)
	return value, log.WrapError(err, "SysModelSvc.FindKeyByID")
}
func (s *SysModelSvc) FindKeysByUser(ctx context.Context, userID string) ([]*entity.SysModelKey, error) {
	value, err := s.repo.FindKeysByUser(ctx, userID)
	return value, log.WrapError(err, "SysModelSvc.FindKeysByUser")
}
func (s *SysModelSvc) CountModelsByKey(ctx context.Context, keyID, userID string) (int64, error) {
	value, err := s.repo.CountModelsByKey(ctx, keyID, userID)
	return value, log.WrapError(err, "SysModelSvc.CountModelsByKey")
}
