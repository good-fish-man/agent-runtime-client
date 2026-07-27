package knowledge_base

import (
	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/knowledge_base"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
)

// SysKnowledgeBaseAssembler converts between DTOs and the domain entity.
type SysKnowledgeBaseAssembler struct{}

// NewSysKnowledgeBaseAssembler builds the assembler.
func NewSysKnowledgeBaseAssembler() *SysKnowledgeBaseAssembler { return &SysKnowledgeBaseAssembler{} }

func (a *SysKnowledgeBaseAssembler) D2ECreate(d *dto.CreateSysKnowledgeBaseReq) *entity.SysKnowledgeBase {
	var en entity.SysKnowledgeBase
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysKnowledgeBaseAssembler) D2EDelete(d *dto.DelSysKnowledgeBaseReq) *entity.SysKnowledgeBase {
	return &entity.SysKnowledgeBase{Ulid: d.Ulid}
}

func (a *SysKnowledgeBaseAssembler) D2EUpdate(d *dto.UpdateSysKnowledgeBaseReq) *entity.SysKnowledgeBase {
	var en entity.SysKnowledgeBase
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	if d.Enabled != nil {
		en.Enabled = *d.Enabled
		en.EnabledSet = true
	}
	return &en
}

func (a *SysKnowledgeBaseAssembler) E2DFind(en *entity.SysKnowledgeBase) *dto.FindSysKnowledgeBaseRsp {
	var d dto.FindSysKnowledgeBaseRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *SysKnowledgeBaseAssembler) E2DList(ens []*entity.SysKnowledgeBase) []*dto.FindSysKnowledgeBaseRsp {
	out := make([]*dto.FindSysKnowledgeBaseRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFind(en))
	}
	return out
}
