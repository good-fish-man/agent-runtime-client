package channel

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/channel"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/channel"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/channel"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/channel"
	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

var _ irepo.ISysChannelRepo = (*SysChannelRepo)(nil)

// SysChannelRepo is the gorm-backed implementation of ISysChannelRepo.
type SysChannelRepo struct {
	data *data.Data
}

// NewSysChannelRepo constructs the repository with the shared data handle.
func NewSysChannelRepo(d *data.Data) *SysChannelRepo {
	return &SysChannelRepo{data: d}
}

func (r *SysChannelRepo) Create(ctx context.Context, en *entity.SysChannel) (string, error) {
	p := converter.E2PAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", errtrace.Wrap(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *SysChannelRepo) Delete(ctx context.Context, en *entity.SysChannel) error {
	patch := converter.E2PDel(en)
	return errtrace.Wrap(r.data.DB(ctx).Model(&po.SysChannel{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysChannelRepo) Update(ctx context.Context, en *entity.SysChannel) error {
	patch := converter.E2PUpdate(en)
	return errtrace.Wrap(r.data.DB(ctx).Model(&po.SysChannel{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysChannelRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.SysChannel, error) {
	var p po.SysChannel
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ?", ulid).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysChannelRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysChannel, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	ps := make([]*po.SysChannel, 0)
	db := r.data.DB(ctx).Model(&po.SysChannel{}).Select(query.SelectFields(selectArgs...))
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("sort asc").Find(&ps).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2EList(ps), nil
}

func (r *SysChannelRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysChannel, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, errtrace.Wrap(err, "Repository")
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.SysChannel, 0)
	db := r.data.DB(ctx).Model(&po.SysChannel{})
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

func (r *SysChannelRepo) FindByCode(ctx context.Context, code string) (*entity.SysChannel, error) {
	var p po.SysChannel
	if err := r.data.DB(ctx).Limit(1).Find(&p, "code = ? AND deleted_at = 0", code).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	en := converter.P2E(&p)
	if en.Code == "" {
		return nil, nil
	}
	return en, nil
}
