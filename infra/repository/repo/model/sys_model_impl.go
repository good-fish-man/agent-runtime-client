package model

import (
	"context"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/model"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/model"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	log "github.com/good-fish-man/logx"
)

var _ irepo.ISysModelRepo = (*SysModelRepo)(nil)

// SysModelRepo is the gorm-backed implementation of ISysModelRepo.
type SysModelRepo struct {
	data *data.Data
}

// NewSysModelRepo constructs the repository with the shared data handle.
func NewSysModelRepo(d *data.Data) *SysModelRepo {
	return &SysModelRepo{data: d}
}

func (r *SysModelRepo) Create(ctx context.Context, en *entity.SysModel) (string, error) {
	p := converter.E2PAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", log.WrapError(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *SysModelRepo) Delete(ctx context.Context, en *entity.SysModel) error {
	patch := converter.E2PDel(en)
	return log.WrapError(r.data.DB(ctx).Model(&po.SysModel{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysModelRepo) Update(ctx context.Context, en *entity.SysModel) error {
	patch := converter.E2PUpdate(en)
	updates := map[string]any{
		"updated_at": patch.UpdatedAt,
		"key_id":     patch.KeyID,
	}
	if patch.Name != "" {
		updates["name"] = patch.Name
	}
	if patch.Provider != "" {
		updates["provider"] = patch.Provider
	}
	if patch.BaseUrl != "" {
		updates["base_url"] = patch.BaseUrl
	}
	if patch.ModelType != "" {
		updates["model_type"] = patch.ModelType
	}
	if patch.Category != "" {
		updates["category"] = patch.Category
	}
	if patch.Status != "" {
		updates["status"] = patch.Status
	}
	if patch.Latency != "" {
		updates["latency"] = patch.Latency
	}
	if patch.ContextWindow != "" {
		updates["context_window"] = patch.ContextWindow
	}
	if patch.Capabilities != "" {
		updates["capabilities"] = patch.Capabilities
	}
	if patch.UpdatedBy != "" {
		updates["updated_by"] = patch.UpdatedBy
	}
	return log.WrapError(r.data.DB(ctx).Model(&po.SysModel{}).Where("ulid = ?", en.Ulid).Updates(updates).Error, "Repository")
}

func (r *SysModelRepo) UpdateEnabled(ctx context.Context, ulid, updatedBy string, enabled bool) error {
	return r.data.DB(ctx).Model(&po.SysModel{}).
		Where("ulid = ? AND deleted_at = 0", ulid).
		Updates(map[string]any{"enabled": enabled, "updated_by": updatedBy, "updated_at": time.Now().UnixMilli()}).Error
}

func (r *SysModelRepo) UpdateRuntimeMode(ctx context.Context, ulid, updatedBy, runtimeMode string) error {
	return r.data.DB(ctx).Model(&po.SysModel{}).
		Where("ulid = ? AND deleted_at = 0", ulid).
		Updates(map[string]any{"runtime_mode": runtimeMode, "updated_by": updatedBy, "updated_at": time.Now().UnixMilli()}).Error
}

func (r *SysModelRepo) CreateKey(ctx context.Context, key *entity.SysModelKey) (string, error) {
	p := &po.SysModelKey{UserID: key.UserID, Name: key.Name, Provider: key.Provider, APIKey: key.APIKey, BaseURL: key.BaseURL, Enabled: true}
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", log.WrapError(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *SysModelRepo) UpdateKey(ctx context.Context, key *entity.SysModelKey) error {
	updates := map[string]any{"updated_at": time.Now().UnixMilli()}
	if key.Name != "" {
		updates["name"] = key.Name
	}
	if key.Provider != "" {
		updates["provider"] = key.Provider
	}
	if key.APIKey != "" {
		updates["api_key"] = key.APIKey
	}
	if key.BaseURL != "" {
		updates["base_url"] = key.BaseURL
	}
	updates["enabled"] = key.Enabled
	return log.WrapError(r.data.DB(ctx).Model(&po.SysModelKey{}).Where("ulid = ? AND user_id = ? AND deleted_at = 0", key.Ulid, key.UserID).Updates(updates).Error, "Repository")
}

func (r *SysModelRepo) DeleteKey(ctx context.Context, keyID, userID string) error {
	return log.WrapError(r.data.DB(ctx).Model(&po.SysModelKey{}).Where("ulid = ? AND user_id = ? AND deleted_at = 0", keyID, userID).Updates(map[string]any{"deleted_at": time.Now().UnixMilli(), "enabled": false}).Error, "Repository")
}

func (r *SysModelRepo) FindKeyByID(ctx context.Context, keyID string) (*entity.SysModelKey, error) {
	var p po.SysModelKey
	if err := r.data.DB(ctx).Limit(1).Find(&p, "ulid = ?", keyID).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.KeyP2E(&p), nil
}

func (r *SysModelRepo) FindKeysByUser(ctx context.Context, userID string) ([]*entity.SysModelKey, error) {
	items := make([]*po.SysModelKey, 0)
	err := r.data.DB(ctx).Where("user_id = ? AND deleted_at = 0", userID).Order("provider asc, name asc").Find(&items).Error
	return converter.KeyP2EList(items), err
}

func (r *SysModelRepo) CountModelsByKey(ctx context.Context, keyID, userID string) (int64, error) {
	var count int64
	err := r.data.DB(ctx).Model(&po.SysModel{}).Where("key_id = ? AND created_by = ? AND deleted_at = 0", keyID, userID).Count(&count).Error
	return count, err
}

func (r *SysModelRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.SysModel, error) {
	var p po.SysModel
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ?", ulid).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysModelRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysModel, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	ps := make([]*po.SysModel, 0)
	db := r.data.DB(ctx).Model(&po.SysModel{}).Select(query.SelectFields(selectArgs...))
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("ulid desc").Find(&ps).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2EList(ps), nil
}

func (r *SysModelRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysModel, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.SysModel, 0)
	db := r.data.DB(ctx).Model(&po.SysModel{})
	if where != "" {
		db = db.Where(where, values...)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	rspPage := &query.PageData{
		PageNum:     reqPage.PageNum,
		PageSize:    reqPage.PageSize,
		TotalNumber: total,
		TotalPage:   query.CeilPageNum(total, reqPage.PageSize),
	}
	if total == 0 {
		return converter.P2EList(ps), rspPage, nil
	}

	err = db.Select(query.SelectFields(selectArgs...)).
		Order(reqSort.OrderBy("ulid")).
		Scopes(query.Paginate(reqPage.PageNum, reqPage.PageSize)).
		Find(&ps).Error
	if err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	return converter.P2EList(ps), rspPage, nil
}

// FindCatalog returns enabled model presets for frontend selection.
func (r *SysModelRepo) FindCatalog(ctx context.Context, modelType, provider string) ([]*entity.ModelCatalog, error) {
	ps := make([]*po.ModelCatalog, 0)
	db := r.data.DB(ctx).Model(&po.ModelCatalog{}).Where("enabled = ?", true)
	if strings.TrimSpace(modelType) != "" {
		db = db.Where("model_type = ?", modelType)
	}
	if strings.TrimSpace(provider) != "" {
		db = db.Where("provider = ?", provider)
	}
	if err := db.Order("sort asc, provider asc, model_family asc, model_version asc").Find(&ps).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.CatalogP2EList(ps), nil
}
