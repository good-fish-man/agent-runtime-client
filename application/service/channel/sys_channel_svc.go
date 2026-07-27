// Package channel provides the application service orchestrating SysChannel use cases.
package channel

import (
	"context"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/channel"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/channel"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/channel"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// SysChannelService is the application service for channel configuration.
type SysChannelService struct {
	asm *assembler.SysChannelAssembler
	srv *srv.SysChannelSvc
}

// NewSysChannelService wires the service over the shared data handle.
func NewSysChannelService(d *data.Data) *SysChannelService {
	return &SysChannelService{
		asm: assembler.NewSysChannelAssembler(),
		srv: srv.NewSysChannelSvc(d),
	}
}

func (s *SysChannelService) CreateSysChannel(ctx context.Context, req *dto.CreateSysChannelReq) (*dto.CreateSysChannelRsp, error) {
	en := s.asm.D2ECreate(req)
	ulid, err := s.srv.Create(ctx, en)
	if err != nil {
		return nil, err
	}
	return &dto.CreateSysChannelRsp{Ulid: ulid}, nil
}

func (s *SysChannelService) DeleteSysChannel(ctx context.Context, req *dto.DelSysChannelReq) error {
	return s.srv.Delete(ctx, s.asm.D2EDelete(req))
}

func (s *SysChannelService) UpdateSysChannel(ctx context.Context, req *dto.UpdateSysChannelReq) error {
	en := s.asm.D2EUpdate(req)
	return s.srv.Update(ctx, en)
}

func (s *SysChannelService) FindSysChannelById(ctx context.Context, req *dto.FindSysChannelByIdReq) (*dto.FindSysChannelRsp, error) {
	en, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return nil, err
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("channel not found or deleted")
	}
	return s.asm.E2DFind(en), nil
}

func (s *SysChannelService) FindSysChannelAll(ctx context.Context, req *dto.FindSysChannelAllReq) ([]*dto.FindSysChannelRsp, error) {
	queries := []*query.Query{{Key: "deleted_at", Operator: query.OpEq, Value: 0}}
	if req.Name != "" {
		queries = append(queries, &query.Query{Key: "name", Operator: query.OpLikePercent, Value: req.Name})
	}
	ens, err := s.srv.FindAll(ctx, queries)
	if err != nil {
		return nil, err
	}
	return s.asm.E2DList(ens), nil
}

func (s *SysChannelService) FindSysChannelPage(ctx context.Context, req *dto.FindSysChannelPageReq) (*dto.FindSysChannelPageRsp, error) {
	ens, pageData, err := s.srv.FindPage(ctx, req.Query, req.PageData, req.SortData)
	if err != nil {
		return nil, err
	}
	return &dto.FindSysChannelPageRsp{Entries: s.asm.E2DList(ens), PageData: pageData}, nil
}

func (s *SysChannelService) FindSysChannelByCode(ctx context.Context, code string) (*dto.FindSysChannelRsp, error) {
	en, err := s.srv.FindByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if en == nil {
		return nil, nil
	}
	return s.asm.E2DFind(en), nil
}
