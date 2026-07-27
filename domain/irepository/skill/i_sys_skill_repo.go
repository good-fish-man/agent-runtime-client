package skill

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/skill"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysSkillRepo is the persistence port for SysSkill.
type ISysSkillRepo interface {
	Create(ctx context.Context, en *entity.SysSkill) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysSkill) (err error)
	Update(ctx context.Context, en *entity.SysSkill) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysSkill, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysSkill, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysSkill, rspPage *query.PageData, err error)
	FindByName(ctx context.Context, name string) (en *entity.SysSkill, err error)
	FindByNameAndType(ctx context.Context, name string, skillType string) (en *entity.SysSkill, err error)
}
