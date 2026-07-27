package skill

import (
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/skill"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/skill"
)

// E2PAdd maps a domain entity to a PO for insertion.
func E2PAdd(en *entity.SysSkill) *po.SysSkill {
	var p po.SysSkill
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	return &p
}

// E2PDel builds the soft-delete PO patch (sets deleted_at).
func E2PDel(en *entity.SysSkill) *po.SysSkill {
	return &po.SysSkill{DeletedAt: time.Now().UnixMilli()}
}

// E2PUpdate maps a domain entity to a PO patch for updates.
func E2PUpdate(en *entity.SysSkill) *po.SysSkill {
	var p po.SysSkill
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	return &p
}

// P2E maps a PO back to a domain entity.
func P2E(p *po.SysSkill) *entity.SysSkill {
	var en entity.SysSkill
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// P2EList maps a slice of POs to entities.
func P2EList(ps []*po.SysSkill) []*entity.SysSkill {
	ens := make([]*entity.SysSkill, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2E(p))
	}
	return ens
}
