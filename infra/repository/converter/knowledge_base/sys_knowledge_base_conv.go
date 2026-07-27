package knowledge_base

import (
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge_base"
)

// E2PAdd maps a domain entity to a PO for insertion.
func E2PAdd(en *entity.SysKnowledgeBase) *po.SysKnowledgeBase {
	var p po.SysKnowledgeBase
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	return &p
}

// E2PDel builds the soft-delete PO patch (sets deleted_at).
func E2PDel(en *entity.SysKnowledgeBase) *po.SysKnowledgeBase {
	return &po.SysKnowledgeBase{DeletedAt: time.Now().UnixMilli()}
}

// E2PUpdate maps a domain entity to a PO patch for updates.
func E2PUpdate(en *entity.SysKnowledgeBase) *po.SysKnowledgeBase {
	var p po.SysKnowledgeBase
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	return &p
}

// P2E maps a PO back to a domain entity.
func P2E(p *po.SysKnowledgeBase) *entity.SysKnowledgeBase {
	var en entity.SysKnowledgeBase
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// P2EList maps a slice of POs to entities.
func P2EList(ps []*po.SysKnowledgeBase) []*entity.SysKnowledgeBase {
	ens := make([]*entity.SysKnowledgeBase, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2E(p))
	}
	return ens
}
