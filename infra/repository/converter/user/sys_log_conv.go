package user

import (
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/user"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
)

// E2PSysLogAdd maps a domain entity to a PO for insertion.
func E2PSysLogAdd(en *entity.SysLog) *po.SysLog {
	var p po.SysLog
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	return &p
}

// E2PSysLogDel builds the soft-delete PO patch.
func E2PSysLogDel(en *entity.SysLog) *po.SysLog {
	return &po.SysLog{DeletedAt: time.Now().UnixMilli(), DeletedBy: en.DeletedBy}
}

// E2PSysLogUpdate maps a domain entity to a PO patch for updates.
func E2PSysLogUpdate(en *entity.SysLog) *po.SysLog {
	var p po.SysLog
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	return &p
}

// P2ESysLog maps a PO back to a domain entity.
func P2ESysLog(p *po.SysLog) *entity.SysLog {
	var en entity.SysLog
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// P2ESysLogs maps a slice of POs to entities.
func P2ESysLogs(ps []*po.SysLog) []*entity.SysLog {
	ens := make([]*entity.SysLog, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2ESysLog(p))
	}
	return ens
}
