package model

import (
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/model"
)

// E2PAdd maps a domain entity to a PO for insertion.
func E2PAdd(en *entity.SysModel) *po.SysModel {
	var p po.SysModel
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	return &p
}

// E2PDel builds the soft-delete PO patch (sets deleted_at).
func E2PDel(en *entity.SysModel) *po.SysModel {
	return &po.SysModel{DeletedAt: time.Now().UnixMilli()}
}

// E2PUpdate maps a domain entity to a PO patch for updates.
func E2PUpdate(en *entity.SysModel) *po.SysModel {
	var p po.SysModel
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	return &p
}

// P2E maps a PO back to a domain entity.
func P2E(p *po.SysModel) *entity.SysModel {
	var en entity.SysModel
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// P2EList maps a slice of POs to entities.
func P2EList(ps []*po.SysModel) []*entity.SysModel {
	ens := make([]*entity.SysModel, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2E(p))
	}
	return ens
}

// CatalogP2E maps a model catalog PO back to a domain entity.
func CatalogP2E(p *po.ModelCatalog) *entity.ModelCatalog {
	var en entity.ModelCatalog
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// CatalogP2EList maps a slice of catalog POs to entities.
func CatalogP2EList(ps []*po.ModelCatalog) []*entity.ModelCatalog {
	ens := make([]*entity.ModelCatalog, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, CatalogP2E(p))
	}
	return ens
}

func KeyP2E(p *po.SysModelKey) *entity.SysModelKey {
	var en entity.SysModelKey
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

func KeyP2EList(ps []*po.SysModelKey) []*entity.SysModelKey {
	out := make([]*entity.SysModelKey, 0, len(ps))
	for _, p := range ps {
		out = append(out, KeyP2E(p))
	}
	return out
}
