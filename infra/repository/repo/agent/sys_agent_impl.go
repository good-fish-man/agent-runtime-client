package agent

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/agent"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/agent"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/agent"
	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

var _ irepo.ISysAgentRepo = (*SysAgentRepo)(nil)

// SysAgentRepo is the gorm-backed implementation of ISysAgentRepo.
type SysAgentRepo struct {
	data *data.Data
}

// NewSysAgentRepo constructs the repository with the shared data handle.
func NewSysAgentRepo(d *data.Data) *SysAgentRepo {
	return &SysAgentRepo{data: d}
}

func (r *SysAgentRepo) Create(ctx context.Context, en *entity.SysAgent) (string, error) {
	p := converter.E2PAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", errtrace.Wrap(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *SysAgentRepo) Delete(ctx context.Context, en *entity.SysAgent) error {
	patch := converter.E2PDel(en)
	return errtrace.Wrap(r.data.DB(ctx).Model(&po.SysAgent{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysAgentRepo) Update(ctx context.Context, en *entity.SysAgent) error {
	patch := converter.E2PUpdate(en)
	db := r.data.DB(ctx).Model(&po.SysAgent{}).Where("ulid = ?", en.Ulid)
	if err := db.Updates(patch).Error; err != nil {
		return errtrace.Wrap(err, "Repository")
	}
	// GORM skips empty struct fields; update separately so users can remove a binding.
	return errtrace.Wrap(db.Updates(map[string]any{"embedding_model": en.EmbeddingModel, "image_model": en.ImageModel}).Error, "Repository")
}

// UpdateEnabled updates only the enabled flag and updated_at timestamp.
func (r *SysAgentRepo) UpdateEnabled(ctx context.Context, ulid string, enabled bool) error {
	return r.data.DB(ctx).Model(&po.SysAgent{}).Where("ulid = ?", ulid).Updates(map[string]any{
		"enabled":    enabled,
		"updated_at": time.Now().UnixMilli(),
	}).Error
}

func (r *SysAgentRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.SysAgent, error) {
	var p po.SysAgent
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ?", ulid).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysAgentRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysAgent, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	ps := make([]*po.SysAgent, 0)
	db := r.data.DB(ctx).Model(&po.SysAgent{}).Select(query.SelectFields(selectArgs...))
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("ulid desc").Find(&ps).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2EList(ps), nil
}

// FindVisible returns public system agents plus agents owned by userID.
func (r *SysAgentRepo) FindVisible(ctx context.Context, userID, name string, selectArgs ...[]string) ([]*entity.SysAgent, error) {
	ps := make([]*po.SysAgent, 0)
	db := r.data.DB(ctx).Model(&po.SysAgent{}).
		Select(query.SelectFields(selectArgs...)).
		Where("deleted_at = 0 AND enabled = ? AND (is_system = ? OR created_by = ?)", true, true, userID)
	if name != "" {
		db = db.Where("name LIKE ?", "%"+name+"%")
	}
	if err := db.Order("is_system desc, ulid desc").Find(&ps).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2EList(ps), nil
}

func (r *SysAgentRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysAgent, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, errtrace.Wrap(err, "Repository")
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.SysAgent, 0)
	db := r.data.DB(ctx).Model(&po.SysAgent{})
	if where != "" {
		db = db.Where(where, values...)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, nil, errtrace.Wrap(err, "Repository")
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
		return nil, nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2EList(ps), rspPage, nil
}

func (r *SysAgentRepo) FindByName(ctx context.Context, name string) (*entity.SysAgent, error) {
	var p po.SysAgent
	if err := r.data.DB(ctx).Limit(1).Find(&p, "name = ?", name).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysAgentRepo) FindUserModel(ctx context.Context, userID, agentID string) (*entity.SysAgentUserModel, error) {
	var value po.SysAgentUserModel
	if err := r.data.DB(ctx).Where("user_id = ? AND agent_id = ?", userID, agentID).Limit(1).Find(&value).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	if value.Ulid == "" {
		return nil, nil
	}
	return &entity.SysAgentUserModel{
		Ulid: value.Ulid, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		UserID: value.UserID, AgentID: value.AgentID, Model: value.Model, EmbeddingModel: value.EmbeddingModel,
	}, nil
}

func (r *SysAgentRepo) UpsertUserModel(ctx context.Context, en *entity.SysAgentUserModel) error {
	value := &po.SysAgentUserModel{
		Ulid: en.Ulid, UserID: en.UserID, AgentID: en.AgentID,
		Model: en.Model, EmbeddingModel: en.EmbeddingModel, ImageModel: en.ImageModel,
	}
	return r.data.DB(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "agent_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"model", "embedding_model", "image_model", "updated_at"}),
	}).Create(value).Error
}
