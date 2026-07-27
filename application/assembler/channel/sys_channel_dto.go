package channel

import (
	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/channel"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/channel"
)

// SysChannelAssembler converts between DTOs and the domain entity.
type SysChannelAssembler struct{}

// NewSysChannelAssembler builds the assembler.
func NewSysChannelAssembler() *SysChannelAssembler { return &SysChannelAssembler{} }

func (a *SysChannelAssembler) D2ECreate(d *dto.CreateSysChannelReq) *entity.SysChannel {
	var en entity.SysChannel
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysChannelAssembler) D2EDelete(d *dto.DelSysChannelReq) *entity.SysChannel {
	return &entity.SysChannel{Ulid: d.Ulid}
}

func (a *SysChannelAssembler) D2EUpdate(d *dto.UpdateSysChannelReq) *entity.SysChannel {
	var en entity.SysChannel
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	if d.Enabled != nil {
		en.Enabled = *d.Enabled
	}
	if d.Sort != nil {
		en.Sort = *d.Sort
	}
	return &en
}

func (a *SysChannelAssembler) E2DFind(en *entity.SysChannel) *dto.FindSysChannelRsp {
	var d dto.FindSysChannelRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *SysChannelAssembler) E2DList(ens []*entity.SysChannel) []*dto.FindSysChannelRsp {
	out := make([]*dto.FindSysChannelRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFind(en))
	}
	return out
}
