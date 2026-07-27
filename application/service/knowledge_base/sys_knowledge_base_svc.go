// Package knowledge_base provides the application service orchestrating SysKnowledgeBase use cases.
package knowledge_base

import (
	"context"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/knowledge_base"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/knowledge_base"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// SysKnowledgeBaseService is the application service for knowledge bases.
type SysKnowledgeBaseService struct {
	asm       *assembler.SysKnowledgeBaseAssembler
	srv       *srv.SysKnowledgeBaseSvc
	retrieval *srv.KnowledgeRetrievalSvc
}

// NewSysKnowledgeBaseService wires the service over the shared data handle.
func NewSysKnowledgeBaseService(d *data.Data) *SysKnowledgeBaseService {
	return &SysKnowledgeBaseService{
		asm:       assembler.NewSysKnowledgeBaseAssembler(),
		srv:       srv.NewSysKnowledgeBaseSvc(d),
		retrieval: srv.NewKnowledgeRetrievalSvc(d),
	}
}

func (s *SysKnowledgeBaseService) CreateSysKnowledgeBase(ctx context.Context, req *dto.CreateSysKnowledgeBaseReq) (*dto.CreateSysKnowledgeBaseRsp, error) {
	en := s.asm.D2ECreate(req)
	ulid, err := s.srv.Create(ctx, en)
	if err != nil {
		return nil, err
	}
	return &dto.CreateSysKnowledgeBaseRsp{Ulid: ulid}, nil
}

func (s *SysKnowledgeBaseService) DeleteSysKnowledgeBase(ctx context.Context, req *dto.DelSysKnowledgeBaseReq) error {
	return s.srv.Delete(ctx, &entity.SysKnowledgeBase{Ulid: req.Ulid})
}

func (s *SysKnowledgeBaseService) UpdateSysKnowledgeBase(ctx context.Context, req *dto.UpdateSysKnowledgeBaseReq) error {
	en := s.asm.D2EUpdate(req)
	return s.srv.Update(ctx, en)
}

func (s *SysKnowledgeBaseService) FindSysKnowledgeBaseById(ctx context.Context, req *dto.FindSysKnowledgeBaseByIdReq) (*dto.FindSysKnowledgeBaseRsp, error) {
	en, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return nil, err
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("knowledge base not found or deleted")
	}
	return s.asm.E2DFind(en), nil
}

func (s *SysKnowledgeBaseService) FindSysKnowledgeBaseAll(ctx context.Context, req *dto.FindSysKnowledgeBaseAllReq) ([]*dto.FindSysKnowledgeBaseRsp, error) {
	queries := []*query.Query{{Key: "deleted_at", Operator: query.OpEq, Value: 0}}
	ens, err := s.srv.FindAll(ctx, queries)
	if err != nil {
		return nil, err
	}
	return s.asm.E2DList(ens), nil
}

func (s *SysKnowledgeBaseService) FindSysKnowledgeBasePage(ctx context.Context, req *dto.FindSysKnowledgeBasePageReq) (*dto.FindSysKnowledgeBasePageRsp, error) {
	ens, pageData, err := s.srv.FindPage(ctx, req.Query, req.PageData, req.SortData)
	if err != nil {
		return nil, err
	}
	return &dto.FindSysKnowledgeBasePageRsp{Entries: s.asm.E2DList(ens), PageData: pageData}, nil
}

// RecallTest executes a retrieval test against the KB's configured retrieval URL.
func (s *SysKnowledgeBaseService) RecallTest(ctx context.Context, ulid string, req *dto.RecallTestReq) ([]*dto.RecallResult, error) {
	results, err := s.retrieval.RecallFromSingleKnowledgeBase(ctx, ulid, req.Query, req.TopK)
	if err != nil {
		return nil, err
	}
	out := make([]*dto.RecallResult, 0, len(results))
	for _, result := range results {
		out = append(out, &dto.RecallResult{
			Title:   result.Title,
			Content: result.Content,
			Score:   result.Score,
		})
	}
	return out, nil
}
