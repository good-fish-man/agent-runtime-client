// Package agent provides the application service orchestrating SysAgent use cases.
package agent

import (
	"context"
	"encoding/json"
	"strings"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/agent"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/agent"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/agent"
	modelsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/model"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// SysAgentService is the application service for agent configuration.
type SysAgentService struct {
	asm      *assembler.SysAgentAssembler
	srv      *srv.SysAgentSvc
	modelSrv *modelsrv.SysModelSvc
}

var agentSummaryFields = []string{
	"ulid",
	"created_at",
	"updated_at",
	"created_by",
	"updated_by",
	"name",
	"description",
	"icon",
	"model",
	"embedding_model",
	"image_model",
	"is_system",
	"enabled",
	"channels",
	"is_periodic",
	"cron_rule",
}

// NewSysAgentService wires the service over the shared data handle.
func NewSysAgentService(d *data.Data) *SysAgentService {
	return &SysAgentService{
		asm:      assembler.NewSysAgentAssembler(),
		srv:      srv.NewSysAgentSvc(d),
		modelSrv: modelsrv.NewSysModelSvc(d),
	}
}

func (s *SysAgentService) CreateSysAgent(ctx context.Context, req *dto.CreateSysAgentReq) (*dto.CreateSysAgentRsp, error) {
	req.IsSystem = false
	if err := s.validateModelBinding(ctx, req.CreatedBy, req.Model); err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	if err := s.validateEmbeddingBinding(ctx, req.CreatedBy, req.EmbeddingModel); err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	if err := s.validateImageBinding(ctx, req.CreatedBy, req.ImageModel); err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	en := s.asm.D2ECreate(req)
	ulid, err := s.srv.Create(ctx, en)
	if err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	return &dto.CreateSysAgentRsp{Ulid: ulid}, nil
}

func (s *SysAgentService) DeleteSysAgent(ctx context.Context, req *dto.DelSysAgentReq) error {
	existing, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if existing == nil || existing.Ulid == "" || existing.DeletedAt != 0 {
		return apierror.ErrNotFound.WithMessage("agent not found")
	}
	if existing.IsSystem {
		return apierror.ErrForbidden.WithMessage("system agent cannot be deleted")
	}
	if existing.CreatedBy != req.UserID {
		return apierror.ErrForbidden.WithMessage("只能删除自己的 Agent")
	}
	return log.WrapError(s.srv.Delete(ctx, s.asm.D2EDelete(req)), "SysAgentService.DeleteSysAgent")
}

func (s *SysAgentService) UpdateSysAgent(ctx context.Context, req *dto.UpdateSysAgentReq) error {
	existing, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if existing == nil || existing.Ulid == "" || existing.DeletedAt != 0 {
		return apierror.ErrNotFound.WithMessage("agent not found")
	}
	if existing.IsSystem {
		if err := s.validateModelBinding(ctx, req.UserID, req.Model); err != nil {
			return log.WrapError(err, "SysAgentService")
		}
		if err := s.validateEmbeddingBinding(ctx, req.UserID, req.EmbeddingModel); err != nil {
			return log.WrapError(err, "SysAgentService")
		}
		if err := s.validateImageBinding(ctx, req.UserID, req.ImageModel); err != nil {
			return log.WrapError(err, "SysAgentService")
		}
		return log.WrapError(s.srv.UpsertUserModel(ctx, &entity.SysAgentUserModel{
			UserID: req.UserID, AgentID: existing.Ulid, Model: req.Model, EmbeddingModel: req.EmbeddingModel, ImageModel: req.ImageModel,
		}), "SysAgentService.UpdateSysAgent.upsertUserModel")
	}
	if existing.CreatedBy != req.UserID {
		return apierror.ErrForbidden.WithMessage("只能修改自己的 Agent")
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = existing.Model
	}
	if err := s.validateModelBinding(ctx, req.UserID, req.Model); err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if err := s.validateEmbeddingBinding(ctx, req.UserID, req.EmbeddingModel); err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if err := s.validateImageBinding(ctx, req.UserID, req.ImageModel); err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	req.Config = preserveSensitiveConfig(existing.Config, req.Config)
	req.ConfigJson = preserveSensitiveConfig(existing.ConfigJson, req.ConfigJson)
	en := s.asm.D2EUpdate(req)
	return log.WrapError(s.srv.Update(ctx, en), "SysAgentService.UpdateSysAgent")
}

func preserveSensitiveConfig(existingRaw, incomingRaw string) string {
	if strings.TrimSpace(existingRaw) == "" || strings.TrimSpace(incomingRaw) == "" {
		return incomingRaw
	}
	var existing, incoming any
	if json.Unmarshal([]byte(existingRaw), &existing) != nil || json.Unmarshal([]byte(incomingRaw), &incoming) != nil {
		return incomingRaw
	}
	preserveSensitiveValues(existing, incoming)
	out, err := json.Marshal(incoming)
	if err != nil {
		return incomingRaw
	}
	return string(out)
}

func preserveSensitiveValues(existing, incoming any) {
	existingMap, existingOK := existing.(map[string]any)
	incomingMap, incomingOK := incoming.(map[string]any)
	if existingOK && incomingOK {
		for key, oldValue := range existingMap {
			newValue, exists := incomingMap[key]
			if isSensitiveConfigKey(key) {
				if !exists || strings.TrimSpace(stringValue(newValue)) == "" {
					incomingMap[key] = oldValue
				}
				continue
			}
			if exists {
				preserveSensitiveValues(oldValue, newValue)
			}
		}
		return
	}
	existingList, existingOK := existing.([]any)
	incomingList, incomingOK := incoming.([]any)
	if !existingOK || !incomingOK {
		return
	}
	oldByID := make(map[string]any, len(existingList))
	for _, item := range existingList {
		if id := configItemID(item); id != "" {
			oldByID[id] = item
		}
	}
	for i, item := range incomingList {
		if old, ok := oldByID[configItemID(item)]; ok {
			preserveSensitiveValues(old, item)
		} else if i < len(existingList) {
			preserveSensitiveValues(existingList[i], item)
		}
	}
}

func configItemID(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue(item["id"]))
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func isSensitiveConfigKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	switch normalized {
	case "systemprompt", "rewriteprompt", "summarizeprompt", "prompt", "instruction":
		return true
	default:
		return false
	}
}

func (s *SysAgentService) UpdateSysAgentEnabled(ctx context.Context, req *dto.UpdateSysAgentEnabledReq) error {
	if _, err := s.requireOwner(ctx, req.Ulid, req.UserID); err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	return log.WrapError(s.srv.UpdateEnabled(ctx, req.Ulid, req.Enabled), "SysAgentService.UpdateSysAgentEnabled")
}

func (s *SysAgentService) FindSysAgentById(ctx context.Context, req *dto.FindSysAgentByIdReq) (*dto.FindSysAgentRsp, error) {
	en, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("agent not found or deleted")
	}
	if !en.IsSystem && en.CreatedBy != req.UserID {
		return nil, apierror.ErrForbidden.WithMessage("只能访问自己的 Agent")
	}
	if en.IsSystem {
		if err := s.applyUserModel(ctx, req.UserID, en); err != nil {
			return nil, log.WrapError(err, "SysAgentService")
		}
	}
	return s.asm.E2DFind(en), nil
}

func (s *SysAgentService) FindSysAgentAll(ctx context.Context, req *dto.FindSysAgentAllReq) ([]*dto.FindSysAgentRsp, error) {
	ens, err := s.srv.FindVisible(ctx, req.UserID, req.Name, agentSummaryFields)
	if err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	for _, agent := range ens {
		if agent.IsSystem {
			if err := s.applyUserModel(ctx, req.UserID, agent); err != nil {
				return nil, log.WrapError(err, "SysAgentService")
			}
		}
	}
	return s.asm.E2DList(ens), nil
}

func (s *SysAgentService) FindSysAgentPage(ctx context.Context, req *dto.FindSysAgentPageReq) (*dto.FindSysAgentPageRsp, error) {
	req.Query = append(req.Query, &query.Query{Key: "deleted_at", Operator: query.OpEq, Value: 0}, &query.Query{Key: "created_by", Operator: query.OpEq, Value: req.UserID})
	ens, pageData, err := s.srv.FindPage(ctx, req.Query, req.PageData, req.SortData, agentSummaryFields)
	if err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	return &dto.FindSysAgentPageRsp{Entries: s.asm.E2DList(ens), PageData: pageData}, nil
}

func (s *SysAgentService) UploadSysAgent(ctx context.Context, req *dto.UploadSysAgentReq) (*dto.CreateSysAgentRsp, error) {
	existing, err := s.srv.FindAll(ctx, []*query.Query{{Key: "name", Operator: query.OpEq, Value: req.Name}, {Key: "created_by", Operator: query.OpEq, Value: req.CreatedBy}, {Key: "deleted_at", Operator: query.OpEq, Value: 0}})
	if err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	if len(existing) > 0 {
		return nil, apierror.ErrBadRequest.WithMessage("agent name already exists")
	}

	createReq := &dto.CreateSysAgentReq{
		CreatedBy:      req.CreatedBy,
		Name:           req.Name,
		Description:    req.Description,
		Icon:           req.Icon,
		Model:          req.Model,
		EmbeddingModel: req.EmbeddingModel,
		ImageModel:     req.ImageModel,
		Config:         req.Config,
		ConfigJson:     req.ConfigJson,
		Enabled:        req.Enabled,
		IsSystem:       false,
		Channels:       req.Channels,
		IsPeriodic:     req.IsPeriodic,
		CronRule:       req.CronRule,
	}
	response, err := s.CreateSysAgent(ctx, createReq)
	return response, log.WrapError(err, "SysAgentService.UploadSysAgent")
}

func (s *SysAgentService) requireOwner(ctx context.Context, agentID, userID string) (*entity.SysAgent, error) {
	agent, err := s.srv.FindById(ctx, agentID)
	if err != nil {
		return nil, log.WrapError(err, "SysAgentService")
	}
	if agent == nil || agent.Ulid == "" || agent.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("agent not found or deleted")
	}
	if agent.IsSystem || agent.CreatedBy != userID {
		return nil, apierror.ErrForbidden.WithMessage("只能修改自己的 Agent")
	}
	return agent, nil
}

func (s *SysAgentService) validateModelBinding(ctx context.Context, userID, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return apierror.ErrModelBindingRequired.WithMessage("请为 Agent 选择模型")
	}
	model, err := s.modelSrv.FindById(ctx, modelID)
	if err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return apierror.ErrModelBindingRequired.WithMessage("Agent 绑定的模型不存在")
	}
	if model.CreatedBy != userID {
		return apierror.ErrForbidden.WithMessage("Agent 只能绑定自己的模型")
	}
	if !strings.EqualFold(model.ModelType, "llm") {
		return apierror.ErrBadRequest.WithMessage("Agent 只能绑定 LLM 模型")
	}
	return nil
}

func (s *SysAgentService) validateEmbeddingBinding(ctx context.Context, userID, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	model, err := s.modelSrv.FindById(ctx, modelID)
	if err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return apierror.ErrBadRequest.WithMessage("Agent 绑定的 Embedding 模型不存在")
	}
	if model.CreatedBy != userID {
		return apierror.ErrForbidden.WithMessage("Agent 只能绑定自己的 Embedding 模型")
	}
	if !strings.EqualFold(model.ModelType, "embedding") {
		return apierror.ErrBadRequest.WithMessage("请选择 Embedding 类型的模型")
	}
	return nil
}

func (s *SysAgentService) validateImageBinding(ctx context.Context, userID, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	model, err := s.modelSrv.FindById(ctx, modelID)
	if err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return apierror.ErrBadRequest.WithMessage("Agent 绑定的图片模型不存在")
	}
	if model.CreatedBy != userID {
		return apierror.ErrForbidden.WithMessage("Agent 只能绑定自己的图片模型")
	}
	if !strings.EqualFold(model.ModelType, "image") {
		return apierror.ErrBadRequest.WithMessage("请选择 Image 类型的模型")
	}
	return nil
}

func (s *SysAgentService) applyUserModel(ctx context.Context, userID string, agent *entity.SysAgent) error {
	binding, err := s.srv.FindUserModel(ctx, userID, agent.Ulid)
	if err != nil {
		return log.WrapError(err, "SysAgentService")
	}
	if binding == nil {
		agent.Model = ""
		agent.EmbeddingModel = ""
		agent.ImageModel = ""
		return nil
	}
	agent.Model = binding.Model
	agent.EmbeddingModel = binding.EmbeddingModel
	agent.ImageModel = binding.ImageModel
	agent.Config = modelBindingConfig(binding.Model, binding.EmbeddingModel, binding.ImageModel)
	return nil
}

func modelBindingConfig(modelID, embeddingModelID, imageModelID string) string {
	models := map[string]string{"default": modelID, "rewrite": modelID, "skill": modelID, "summarize": modelID}
	value := map[string]any{"models": models, "embeddingModel": embeddingModelID, "imageModel": imageModelID}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// FindPeriodicAgents finds all periodic agents for callers that synchronize jobs.
func (s *SysAgentService) FindPeriodicAgents(ctx context.Context) ([]*entity.SysAgent, error) {
	values, err := s.srv.FindPeriodicEnabled(ctx)
	return values, log.WrapError(err, "SysAgentService.FindPeriodicAgents")
}
