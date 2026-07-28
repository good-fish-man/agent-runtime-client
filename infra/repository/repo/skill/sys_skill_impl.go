package skill

import (
	"context"

	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/skill"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/skill"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/skill"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/skill"
	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

var _ irepo.ISysSkillRepo = (*SysSkillRepo)(nil)

// SysSkillRepo is the gorm-backed implementation of ISysSkillRepo.
type SysSkillRepo struct {
	data *data.Data
}

// NewSysSkillRepo constructs the repository with the shared data handle.
func NewSysSkillRepo(d *data.Data) *SysSkillRepo {
	return &SysSkillRepo{data: d}
}

func (r *SysSkillRepo) Create(ctx context.Context, en *entity.SysSkill) (string, error) {
	p := converter.E2PAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", errtrace.Wrap(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *SysSkillRepo) Delete(ctx context.Context, en *entity.SysSkill) error {
	patch := converter.E2PDel(en)
	return errtrace.Wrap(r.data.DB(ctx).Model(&po.SysSkill{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysSkillRepo) Update(ctx context.Context, en *entity.SysSkill) error {
	patch := converter.E2PUpdate(en)
	return errtrace.Wrap(r.data.DB(ctx).Model(&po.SysSkill{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *SysSkillRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.SysSkill, error) {
	var p po.SysSkill
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ?", ulid).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysSkillRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.SysSkill, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	ps := make([]*po.SysSkill, 0)
	db := r.data.DB(ctx).Model(&po.SysSkill{}).Select(query.SelectFields(selectArgs...))
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("ulid desc").Find(&ps).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2EList(ps), nil
}

func (r *SysSkillRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.SysSkill, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, errtrace.Wrap(err, "Repository")
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.SysSkill, 0)
	db := r.data.DB(ctx).Model(&po.SysSkill{})
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

func (r *SysSkillRepo) FindByName(ctx context.Context, name string) (*entity.SysSkill, error) {
	var p po.SysSkill
	if err := r.data.DB(ctx).Limit(1).Find(&p, "name = ?", name).Error; err != nil {
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2E(&p), nil
}

func (r *SysSkillRepo) FindByNameAndType(ctx context.Context, name string, skillType string) (*entity.SysSkill, error) {
	var p po.SysSkill
	if err := r.data.DB(ctx).First(&p, "name = ? AND skill_type = ?", name, skillType).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, errtrace.Wrap(err, "Repository")
	}
	return converter.P2E(&p), nil
}
