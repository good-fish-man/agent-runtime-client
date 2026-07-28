package knowledge_base

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/knowledge_base"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

var _ irepo.ISysKnowledgeBaseRepo = (*SysKnowledgeBaseRepo)(nil)

// SysKnowledgeBaseRepo is the gorm-backed implementation of ISysKnowledgeBaseRepo.
type SysKnowledgeBaseRepo struct {
	data *data.Data
}

// NewSysKnowledgeBaseRepo constructs the repository with the shared data handle.
func NewSysKnowledgeBaseRepo(d *data.Data) *SysKnowledgeBaseRepo {
	return &SysKnowledgeBaseRepo{data: d}
}

func (r *SysKnowledgeBaseRepo) Create(ctx context.Context, en *entity.SysKnowledgeBase) (string, error) {
	p := converter.E2PAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", log.WrapError(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *SysKnowledgeBaseRepo) Delete(ctx context.Context, en *entity.SysKnowledgeBase) error {
	patch := converter.E2PDel(en)
	return log.WrapError(r.data.DB(ctx).Model(&po.SysKnowledgeBase{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysKnowledgeBaseRepo) Update(ctx context.Context, en *entity.SysKnowledgeBase) error {
	patch := converter.E2PUpdate(en)
	updates := map[string]any{"updated_at": patch.UpdatedAt}
	if patch.UpdatedBy != "" {
		updates["updated_by"] = patch.UpdatedBy
	}
	if patch.Name != "" {
		updates["name"] = patch.Name
	}
	if patch.Description != "" {
		updates["description"] = patch.Description
	}
	if patch.RetrievalUrl != "" {
		updates["retrieval_url"] = patch.RetrievalUrl
	}
	if patch.Token != "" {
		updates["token"] = patch.Token
	}
	if en.EnabledSet {
		updates["enabled"] = patch.Enabled
	}
	return log.WrapError(r.data.DB(ctx).Model(&po.SysKnowledgeBase{}).Where("ulid = ?", en.Ulid).Updates(updates).Error, "Repository")
}

func (r *SysKnowledgeBaseRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.SysKnowledgeBase, error) {
	var p po.SysKnowledgeBase
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ?", ulid).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysKnowledgeBaseRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysKnowledgeBase, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	ps := make([]*po.SysKnowledgeBase, 0)
	db := r.data.DB(ctx).Model(&po.SysKnowledgeBase{}).Select(query.SelectFields(selectArgs...))
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("ulid desc").Find(&ps).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2EList(ps), nil
}

func (r *SysKnowledgeBaseRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysKnowledgeBase, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.SysKnowledgeBase, 0)
	db := r.data.DB(ctx).Model(&po.SysKnowledgeBase{})
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
