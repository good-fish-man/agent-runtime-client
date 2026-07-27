package user

import (
	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/user"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
)

// SysUserDto converts between DTOs and the domain entity.
type SysUserDto struct{}

// NewSysUserDto builds the assembler.
func NewSysUserDto() *SysUserDto { return &SysUserDto{} }

func (a *SysUserDto) D2ECreateSysUser(d *dto.CreateSysUserReq) *entity.SysUser {
	var en entity.SysUser
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysUserDto) D2EDeleteSysUser(d *dto.DelSysUsersReq) *entity.SysUser {
	return &entity.SysUser{Ulid: d.Ulid, DeletedBy: d.DeletedBy}
}

func (a *SysUserDto) D2EUpdateSysUser(d *dto.UpdateSysUserReq) *entity.SysUser {
	var en entity.SysUser
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysUserDto) D2ELoginSysUser(d *dto.LoginReq) *entity.SysUser {
	var en entity.SysUser
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysUserDto) E2DCreateSysUser(en *entity.SysUser) *dto.CreateSysUserRsp {
	return &dto.CreateSysUserRsp{Ulid: en.Ulid}
}

func (a *SysUserDto) E2DFindSysUserRsp(en *entity.SysUser) *dto.FindSysUserRsp {
	var d dto.FindSysUserRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *SysUserDto) E2DGetSysUsers(ens []*entity.SysUser) []*dto.FindSysUserRsp {
	out := make([]*dto.FindSysUserRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFindSysUserRsp(en))
	}
	return out
}
