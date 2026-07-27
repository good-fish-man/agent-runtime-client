package job

import (
	"time"

	"github.com/jinzhu/copier"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/job"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/job"
)

// E2PJobExecutionAdd maps a domain entity to a PO for insertion.
func E2PJobExecutionAdd(en *entity.JobExecution) *po.JobExecutionPO {
	var p po.JobExecutionPO
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.CreatedAt = time.Now().UnixMilli()
	return &p
}

// E2PJobExecutionDel builds the soft-delete PO patch.
func E2PJobExecutionDel(en *entity.JobExecution) *po.JobExecutionPO {
	return &po.JobExecutionPO{DeletedAt: time.Now().UnixMilli()}
}

// E2PJobExecutionUpdate maps a domain entity to a PO patch for updates.
func E2PJobExecutionUpdate(en *entity.JobExecution) *po.JobExecutionPO {
	var p po.JobExecutionPO
	if err := copier.Copy(&p, en); err != nil {
		panic(err)
	}
	p.UpdatedAt = time.Now().UnixMilli()
	return &p
}

// P2EJobExecution maps a PO back to a domain entity.
func P2EJobExecution(p *po.JobExecutionPO) *entity.JobExecution {
	var en entity.JobExecution
	if err := copier.Copy(&en, p); err != nil {
		panic(err)
	}
	return &en
}

// P2EJobExecutions maps a slice of POs to entities.
func P2EJobExecutions(ps []*po.JobExecutionPO) []*entity.JobExecution {
	ens := make([]*entity.JobExecution, 0, len(ps))
	for _, p := range ps {
		ens = append(ens, P2EJobExecution(p))
	}
	return ens
}
