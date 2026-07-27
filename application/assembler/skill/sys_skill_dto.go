package skill

import (
	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/skill"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/skill"
)

// SysSkillAssembler converts between DTOs and the domain entity.
type SysSkillAssembler struct{}

// NewSysSkillAssembler builds the assembler.
func NewSysSkillAssembler() *SysSkillAssembler { return &SysSkillAssembler{} }

func (a *SysSkillAssembler) D2ECreate(d *dto.CreateSysSkillReq) *entity.SysSkill {
	var en entity.SysSkill
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysSkillAssembler) D2EDelete(d *dto.DelSysSkillReq) *entity.SysSkill {
	return &entity.SysSkill{Ulid: d.Ulid}
}

func (a *SysSkillAssembler) D2EUpdate(d *dto.UpdateSysSkillReq) *entity.SysSkill {
	var en entity.SysSkill
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	if d.Enabled != nil {
		en.Enabled = *d.Enabled
	}
	return &en
}

func (a *SysSkillAssembler) E2DFind(en *entity.SysSkill) *dto.FindSysSkillRsp {
	var d dto.FindSysSkillRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *SysSkillAssembler) E2DList(ens []*entity.SysSkill) []*dto.FindSysSkillRsp {
	out := make([]*dto.FindSysSkillRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFind(en))
	}
	return out
}
