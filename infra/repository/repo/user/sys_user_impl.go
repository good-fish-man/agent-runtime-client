package user

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/user"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/user"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

var _ irepo.ISysUserRepo = (*SysUserRepo)(nil)

// SysUserRepo is the gorm-backed implementation of ISysUserRepo.
type SysUserRepo struct {
	data *data.Data
}

// NewSysUserRepo constructs the repository with the shared data handle.
func NewSysUserRepo(d *data.Data) *SysUserRepo {
	return &SysUserRepo{data: d}
}

func (r *SysUserRepo) Create(ctx context.Context, en *entity.SysUser) (string, error) {
	p := converter.E2PSysUserAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", err
	}
	return p.Ulid, nil
}

func (r *SysUserRepo) Delete(ctx context.Context, en *entity.SysUser) error {
	patch := converter.E2PSysUserDel(en)
	return r.data.DB(ctx).Model(&po.SysUser{}).Where("ulid = ?", en.Ulid).Updates(patch).Error
}

func (r *SysUserRepo) Update(ctx context.Context, en *entity.SysUser) error {
	patch := converter.E2PSysUserUpdate(en)
	return r.data.DB(ctx).Model(&po.SysUser{}).Where("ulid = ?", en.Ulid).Updates(patch).Error
}

func (r *SysUserRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.SysUser, error) {
	var p po.SysUser
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ?", ulid).Error; err != nil {
		return nil, err
	}
	return converter.P2ESysUser(&p), nil
}

func (r *SysUserRepo) FindByQuery(ctx context.Context, queries []*query.Query) (*entity.SysUser, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, err
	}
	var p po.SysUser
	db := r.data.DB(ctx).Model(&po.SysUser{})
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Limit(1).Find(&p).Error; err != nil {
		return nil, err
	}
	return converter.P2ESysUser(&p), nil
}

func (r *SysUserRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysUser, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, err
	}
	ps := make([]*po.SysUser, 0)
	db := r.data.DB(ctx).Model(&po.SysUser{}).Select(query.SelectFields(selectArgs...))
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("ulid desc").Find(&ps).Error; err != nil {
		return nil, err
	}
	return converter.P2ESysUsers(ps), nil
}

func (r *SysUserRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysUser, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, err
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.SysUser, 0)
	db := r.data.DB(ctx).Model(&po.SysUser{})
	if where != "" {
		db = db.Where(where, values...)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, nil, err
	}
	rspPage := &query.PageData{
		PageNum:     reqPage.PageNum,
		PageSize:    reqPage.PageSize,
		TotalNumber: total,
		TotalPage:   query.CeilPageNum(total, reqPage.PageSize),
	}
	if total == 0 {
		return converter.P2ESysUsers(ps), rspPage, nil
	}

	err = db.Select(query.SelectFields(selectArgs...)).
		Order(reqSort.OrderBy("ulid")).
		Scopes(query.Paginate(reqPage.PageNum, reqPage.PageSize)).
		Find(&ps).Error
	if err != nil {
		return nil, nil, err
	}
	return converter.P2ESysUsers(ps), rspPage, nil
}
