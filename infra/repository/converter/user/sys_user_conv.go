package user

import (
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
)

// E2PSysUserAdd maps a domain entity to a PO for insertion.
func E2PSysUserAdd(en *entity.SysUser) *po.SysUser {
	var p po.SysUser
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	return &p
}

// E2PSysUserDel builds the soft-delete PO patch.
func E2PSysUserDel(en *entity.SysUser) *po.SysUser {
	return &po.SysUser{DeletedAt: time.Now().UnixMilli(), DeletedBy: en.DeletedBy}
}

// E2PSysUserUpdate maps a domain entity to a PO patch for updates.
func E2PSysUserUpdate(en *entity.SysUser) *po.SysUser {
	var p po.SysUser
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	return &p
}

// P2ESysUser maps a PO back to a domain entity.
func P2ESysUser(p *po.SysUser) *entity.SysUser {
	var en entity.SysUser
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// P2ESysUsers maps a slice of POs to entities.
func P2ESysUsers(ps []*po.SysUser) []*entity.SysUser {
	ens := make([]*entity.SysUser, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2ESysUser(p))
	}
	return ens
}
