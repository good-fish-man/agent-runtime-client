package skill

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/skill"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/skill"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// SysSkillSvc is the SysSkill domain service.
type SysSkillSvc struct {
	repo *repo.SysSkillRepo
}

// NewSysSkillSvc builds the domain service over the shared data handle.
func NewSysSkillSvc(d *data.Data) *SysSkillSvc {
	return &SysSkillSvc{repo: repo.NewSysSkillRepo(d)}
}

func (s *SysSkillSvc) Create(ctx context.Context, en *entity.SysSkill) (string, error) {
	return s.repo.Create(ctx, en)
}

func (s *SysSkillSvc) Delete(ctx context.Context, en *entity.SysSkill) error {
	return s.repo.Delete(ctx, en)
}

func (s *SysSkillSvc) Update(ctx context.Context, en *entity.SysSkill) error {
	return s.repo.Update(ctx, en)
}

func (s *SysSkillSvc) FindById(ctx context.Context, ulid string) (*entity.SysSkill, error) {
	return s.repo.FindById(ctx, ulid)
}

func (s *SysSkillSvc) FindAll(ctx context.Context, queries []*query.Query) ([]*entity.SysSkill, error) {
	return s.repo.FindAll(ctx, queries)
}

func (s *SysSkillSvc) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.SysSkill, *query.PageData, error) {
	return s.repo.FindPage(ctx, queries, reqPage, reqSort)
}

func (s *SysSkillSvc) FindByName(ctx context.Context, name string) (*entity.SysSkill, error) {
	return s.repo.FindByName(ctx, name)
}

func (s *SysSkillSvc) FindByNameAndType(ctx context.Context, name string, skillType string) (*entity.SysSkill, error) {
	return s.repo.FindByNameAndType(ctx, name, skillType)
}
