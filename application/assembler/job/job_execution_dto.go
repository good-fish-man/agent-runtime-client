package job

import (
	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/job"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/job"
)

// JobExecutionDto converts between job execution DTOs and entities.
type JobExecutionDto struct{}

// NewJobExecutionDto builds the assembler.
func NewJobExecutionDto() *JobExecutionDto { return &JobExecutionDto{} }

func (a *JobExecutionDto) E2DFindJobExecutionRsp(en *entity.JobExecution) *dto.FindJobExecutionRsp {
	var d dto.FindJobExecutionRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	return &d
}

func (a *JobExecutionDto) E2DGetJobExecutions(ens []*entity.JobExecution) []*dto.FindJobExecutionRsp {
	out := make([]*dto.FindJobExecutionRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFindJobExecutionRsp(en))
	}
	return out
}
