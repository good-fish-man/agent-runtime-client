package model

import (
	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
)

// SysModelAssembler converts between DTOs and the domain entity.
type SysModelAssembler struct{}

// NewSysModelAssembler builds the assembler.
func NewSysModelAssembler() *SysModelAssembler { return &SysModelAssembler{} }

func (a *SysModelAssembler) D2ECreate(d *dto.CreateSysModelReq) *entity.SysModel {
	var en entity.SysModel
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	en.Enabled = true
	en.RuntimeMode = entity.RuntimeModeOnDemand
	return &en
}

func (a *SysModelAssembler) D2EUpdate(d *dto.UpdateSysModelReq) *entity.SysModel {
	var en entity.SysModel
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	if d.KeyID != nil {
		en.KeyID = *d.KeyID
	}
	return &en
}

func (a *SysModelAssembler) E2DFind(en *entity.SysModel) *dto.FindSysModelRsp {
	var d dto.FindSysModelRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *SysModelAssembler) E2DList(ens []*entity.SysModel) []*dto.FindSysModelRsp {
	out := make([]*dto.FindSysModelRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFind(en))
	}
	return out
}

func (a *SysModelAssembler) CatalogE2D(en *entity.ModelCatalog) *dto.FindModelCatalogRsp {
	var d dto.FindModelCatalogRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *SysModelAssembler) CatalogE2DList(ens []*entity.ModelCatalog) []*dto.FindModelCatalogRsp {
	out := make([]*dto.FindModelCatalogRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.CatalogE2D(en))
	}
	return out
}
