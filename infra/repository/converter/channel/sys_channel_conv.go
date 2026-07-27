package channel

import (
	"encoding/json"
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/channel"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/channel"
)

// E2PAdd maps a domain entity to a PO for insertion.
func E2PAdd(en *entity.SysChannel) *po.SysChannel {
	var p po.SysChannel
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	if en.Config != nil {
		configBytes, _ := json.Marshal(en.Config)
		p.Config = string(configBytes)
	}
	return &p
}

// E2PDel builds the soft-delete PO patch (sets deleted_at).
func E2PDel(en *entity.SysChannel) *po.SysChannel {
	return &po.SysChannel{DeletedAt: time.Now().UnixMilli()}
}

// E2PUpdate maps a domain entity to a PO patch for updates.
func E2PUpdate(en *entity.SysChannel) *po.SysChannel {
	var p po.SysChannel
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	if en.Config != nil {
		configBytes, _ := json.Marshal(en.Config)
		p.Config = string(configBytes)
	}
	return &p
}

// P2E maps a PO back to a domain entity.
func P2E(p *po.SysChannel) *entity.SysChannel {
	var en entity.SysChannel
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	if p.Config != "" {
		_ = json.Unmarshal([]byte(p.Config), &en.Config)
	}
	return &en
}

// P2EList maps a slice of POs to entities.
func P2EList(ps []*po.SysChannel) []*entity.SysChannel {
	ens := make([]*entity.SysChannel, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2E(p))
	}
	return ens
}
