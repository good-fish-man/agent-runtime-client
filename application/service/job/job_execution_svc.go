package job

import (
	"context"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/job"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/job"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/job"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// JobExecutionService orchestrates job execution log use cases.
type JobExecutionService struct {
	jobExecutionDto *assembler.JobExecutionDto
	jobExecutionSvc *srv.JobExecution
}

// NewJobExecutionService wires the service over the shared data handle.
func NewJobExecutionService(d *data.Data) *JobExecutionService {
	return &JobExecutionService{
		jobExecutionDto: assembler.NewJobExecutionDto(),
		jobExecutionSvc: srv.NewJobExecutionSvc(d),
	}
}

func (s *JobExecutionService) FindJobExecutionById(ctx context.Context, req *dto.FindJobExecutionByIdReq) (*dto.FindJobExecutionRsp, error) {
	en, err := s.jobExecutionSvc.FindJobExecutionById(ctx, req.Ulid)
	if err != nil {
		return nil, err
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("job execution not found or deleted")
	}
	return s.jobExecutionDto.E2DFindJobExecutionRsp(en), nil
}

func (s *JobExecutionService) FindJobExecutionByAgentId(ctx context.Context, req *dto.FindJobExecutionByAgentIdReq) (*dto.FindJobExecutionListRsp, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	ens, err := s.jobExecutionSvc.FindByAgentId(ctx, req.AgentId, limit)
	if err != nil {
		return nil, err
	}
	return &dto.FindJobExecutionListRsp{Entries: s.jobExecutionDto.E2DGetJobExecutions(ens)}, nil
}

func (s *JobExecutionService) FindJobExecutionPage(ctx context.Context, req *dto.FindJobExecutionPageReq) (*dto.FindJobExecutionPageRsp, error) {
	ens, pageData, err := s.jobExecutionSvc.FindJobExecutionPage(ctx, req.Query, req.PageData, req.SortData)
	if err != nil {
		return nil, err
	}
	return &dto.FindJobExecutionPageRsp{Entries: s.jobExecutionDto.E2DGetJobExecutions(ens), PageData: pageData}, nil
}
