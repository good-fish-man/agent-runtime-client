// Package model provides the application service orchestrating SysModel use cases.
package model

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/model"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/model"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	log "github.com/good-fish-man/logx"
)

// SysModelService is the application service for model-provider configuration.
type SysModelService struct {
	asm *assembler.SysModelAssembler
	srv *srv.SysModelSvc
}

// NewSysModelService wires the service over the shared data handle.
func NewSysModelService(d *data.Data) *SysModelService {
	return &SysModelService{
		asm: assembler.NewSysModelAssembler(),
		srv: srv.NewSysModelSvc(d),
	}
}

func (s *SysModelService) CreateSysModel(ctx context.Context, req *dto.CreateSysModelReq) (*dto.CreateSysModelRsp, error) {
	if err := s.validateModelKey(ctx, req.KeyID, req.CreatedBy, req.Provider, req.BaseUrl); err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	en := s.asm.D2ECreate(req)
	ulid, err := s.srv.Create(ctx, en)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	return &dto.CreateSysModelRsp{Ulid: ulid}, nil
}

func (s *SysModelService) DeleteSysModel(ctx context.Context, req *dto.DelSysModelReq) error {
	if _, err := s.requireOwner(ctx, req.Ulid, req.UserID); err != nil {
		return log.WrapError(err, "SysModelService")
	}
	return log.WrapError(s.srv.Delete(ctx, &entity.SysModel{Ulid: req.Ulid}), "SysModelService.DeleteSysModel")
}

func (s *SysModelService) UpdateSysModel(ctx context.Context, req *dto.UpdateSysModelReq) error {
	existing, err := s.requireOwner(ctx, req.Ulid, req.UserID)
	if err != nil {
		return log.WrapError(err, "SysModelService")
	}
	provider := firstNonEmpty(req.Provider, existing.Provider)
	baseURL := firstNonEmpty(req.BaseUrl, existing.BaseUrl)
	keyID := existing.KeyID
	if req.KeyID != nil {
		keyID = strings.TrimSpace(*req.KeyID)
	}
	if err := s.validateModelKey(ctx, keyID, req.UserID, provider, baseURL); err != nil {
		return log.WrapError(err, "SysModelService")
	}
	req.KeyID = &keyID
	// Availability is controlled through the administrator-only endpoint.
	req.Status = ""
	en := s.asm.D2EUpdate(req)
	return log.WrapError(s.srv.Update(ctx, en), "SysModelService.UpdateSysModel")
}

func (s *SysModelService) UpdateSysModelEnabled(ctx context.Context, req *dto.UpdateSysModelEnabledReq) error {
	model, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return log.WrapError(err, "SysModelService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return apierror.ErrNotFound.WithMessage("model not found or deleted")
	}
	return log.WrapError(s.srv.UpdateEnabled(ctx, req.Ulid, req.UpdatedBy, *req.Enabled), "SysModelService.UpdateSysModelEnabled")
}

func (s *SysModelService) UpdateSysModelRuntimeMode(ctx context.Context, req *dto.UpdateSysModelRuntimeModeReq) error {
	model, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return log.WrapError(err, "SysModelService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return apierror.ErrNotFound.WithMessage("model not found or deleted")
	}
	if !isLocalModel(model.Provider, model.BaseUrl) {
		return apierror.ErrBadRequest.WithMessage("只有本地安装的模型支持运行模式设置")
	}
	return log.WrapError(s.srv.UpdateRuntimeMode(ctx, req.Ulid, req.UpdatedBy, req.RuntimeMode), "SysModelService.UpdateSysModelRuntimeMode")
}

func isLocalModel(provider, baseURL string) bool {
	normalized := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(provider))
	return normalized == entity.ProviderOllama || normalized == entity.ProviderDiffusers
}

func (s *SysModelService) FindSysModelAdminAll(ctx context.Context) ([]*dto.FindSysModelRsp, error) {
	ens, err := s.srv.FindAll(ctx, []*query.Query{{Key: "deleted_at", Operator: query.OpEq, Value: 0}})
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	result := s.asm.E2DList(ens)
	for _, item := range result {
		s.enrichKey(ctx, item)
	}
	s.enrichUsage(ctx, result)
	return result, nil
}

func (s *SysModelService) FindSysModelAdminByID(ctx context.Context, modelID string) (*dto.FindSysModelRsp, error) {
	model, err := s.srv.FindById(ctx, modelID)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("model not found or deleted")
	}
	result := s.asm.E2DFind(model)
	s.enrichUsage(ctx, []*dto.FindSysModelRsp{result})
	return result, nil
}

func (s *SysModelService) FindSysModelById(ctx context.Context, req *dto.FindSysModelByIdReq) (*dto.FindSysModelRsp, error) {
	en, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("model not found or deleted")
	}
	if en.CreatedBy != req.UserID {
		return nil, apierror.ErrForbidden.WithMessage("只能访问自己绑定的模型")
	}
	rsp := s.asm.E2DFind(en)
	s.enrichKey(ctx, rsp)
	s.enrichUsage(ctx, []*dto.FindSysModelRsp{rsp})
	return rsp, nil
}

func (s *SysModelService) FindSysModelAll(ctx context.Context, req *dto.FindSysModelAllReq) ([]*dto.FindSysModelRsp, error) {
	queries := []*query.Query{{Key: "deleted_at", Operator: query.OpEq, Value: 0}}
	queries = append(queries, &query.Query{Key: "created_by", Operator: query.OpEq, Value: req.UserID})
	if req.ModelType != "" {
		queries = append(queries, &query.Query{Key: "model_type", Operator: query.OpEq, Value: req.ModelType})
	}
	ens, err := s.srv.FindAll(ctx, queries)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	result := s.asm.E2DList(ens)
	for _, item := range result {
		s.enrichKey(ctx, item)
	}
	s.enrichUsage(ctx, result)
	return result, nil
}

func (s *SysModelService) FindSysModelPage(ctx context.Context, req *dto.FindSysModelPageReq) (*dto.FindSysModelPageRsp, error) {
	req.Query = append(req.Query, &query.Query{Key: "deleted_at", Operator: query.OpEq, Value: 0}, &query.Query{Key: "created_by", Operator: query.OpEq, Value: req.UserID})
	ens, pageData, err := s.srv.FindPage(ctx, req.Query, req.PageData, req.SortData)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	entries := s.asm.E2DList(ens)
	for _, item := range entries {
		s.enrichKey(ctx, item)
	}
	s.enrichUsage(ctx, entries)
	return &dto.FindSysModelPageRsp{Entries: entries, PageData: pageData}, nil
}

func (s *SysModelService) enrichUsage(ctx context.Context, items []*dto.FindSysModelRsp) {
	if len(items) == 0 {
		return
	}
	metrics, err := s.srv.FindRecentUsageMetrics(ctx, time.Now().Add(-24*time.Hour).UnixMilli())
	if err != nil {
		log.WarnwCtx(ctx, "load recent model usage failed", "error", err)
		return
	}
	applyModelUsageMetrics(items, metrics)
}

type modelUsageAggregate struct {
	requests       int64
	successes      int64
	inputTokens    int64
	outputTokens   int64
	totalTokens    int64
	latencyTotalMS int64
	latencyCount   int64
}

func applyModelUsageMetrics(items []*dto.FindSysModelRsp, metrics []entity.ModelUsageMetric) {
	byID := make(map[string]*dto.FindSysModelRsp, len(items))
	byName := make(map[string]*dto.FindSysModelRsp, len(items))
	duplicateNames := make(map[string]bool)
	for _, item := range items {
		if item == nil {
			continue
		}
		byID[modelUsageKey(item.CreatedBy, item.Ulid)] = item
		nameKey := modelUsageKey(item.CreatedBy, item.Name)
		if _, exists := byName[nameKey]; exists {
			duplicateNames[nameKey] = true
		} else {
			byName[nameKey] = item
		}
	}

	aggregates := make(map[string]*modelUsageAggregate, len(items))
	totals := make(map[string]int64)
	for _, metric := range metrics {
		var item *dto.FindSysModelRsp
		if strings.TrimSpace(metric.ModelID) != "" {
			item = byID[modelUsageKey(metric.UserID, metric.ModelID)]
		}
		if item == nil && strings.TrimSpace(metric.ModelName) != "" {
			key := modelUsageKey(metric.UserID, metric.ModelName)
			if !duplicateNames[key] {
				item = byName[key]
			}
		}
		if item == nil {
			continue
		}
		aggregate := aggregates[item.Ulid]
		if aggregate == nil {
			aggregate = &modelUsageAggregate{}
			aggregates[item.Ulid] = aggregate
		}
		aggregate.requests += metric.RequestCount
		aggregate.successes += metric.SuccessCount
		aggregate.inputTokens += metric.InputTokens
		aggregate.outputTokens += metric.OutputTokens
		aggregate.totalTokens += metric.TotalTokens
		aggregate.latencyTotalMS += metric.LatencyTotalMs
		aggregate.latencyCount += metric.LatencyCount
		totals[item.CreatedBy] += metric.RequestCount
	}

	for _, item := range items {
		if item == nil {
			continue
		}
		aggregate := aggregates[item.Ulid]
		if aggregate == nil {
			item.Usage, item.UsageRate, item.UsageCount, item.SuccessRate = 0, 0, 0, 0
			item.InputTokens, item.OutputTokens, item.TotalTokens = 0, 0, 0
			continue
		}
		item.UsageCount = aggregate.requests
		item.InputTokens = aggregate.inputTokens
		item.OutputTokens = aggregate.outputTokens
		item.TotalTokens = aggregate.totalTokens
		if aggregate.requests == 0 {
			item.Usage, item.UsageRate, item.SuccessRate = 0, 0, 0
			continue
		}
		if total := totals[item.CreatedBy]; total > 0 {
			rate := float64(aggregate.requests) * 100 / float64(total)
			item.Usage = int(math.Round(rate))
			item.UsageRate = math.Round(rate*100) / 100
		}
		item.SuccessRate = math.Round(float64(aggregate.successes)*1000/float64(aggregate.requests)) / 10
		if aggregate.latencyCount > 0 {
			item.Latency = formatModelLatency(aggregate.latencyTotalMS / aggregate.latencyCount)
		}
	}
}

func modelUsageKey(userID, model string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.ToLower(strings.TrimSpace(model))
}

func formatModelLatency(milliseconds int64) string {
	if milliseconds < 1000 {
		return fmt.Sprintf("%d ms", milliseconds)
	}
	return fmt.Sprintf("%.2f s", float64(milliseconds)/1000)
}

func (s *SysModelService) FindModelCatalog(ctx context.Context, req *dto.FindModelCatalogReq) ([]*dto.FindModelCatalogRsp, error) {
	ens, err := s.srv.FindCatalog(ctx, req.ModelType, req.Provider)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	return s.asm.CatalogE2DList(ens), nil
}

func (s *SysModelService) FindModelCatalogByID(ctx context.Context, catalogID string) (*dto.FindModelCatalogRsp, error) {
	items, err := s.FindModelCatalog(ctx, &dto.FindModelCatalogReq{})
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	for _, item := range items {
		if item.Ulid == catalogID {
			return item, nil
		}
	}
	return nil, apierror.ErrNotFound.WithMessage("model catalog item not found")
}

// FindDefaultModel returns the model marked category=default, or nil if none.
func (s *SysModelService) FindDefaultModel(ctx context.Context, userID string) (*dto.FindSysModelRsp, error) {
	queries := []*query.Query{
		{Key: "deleted_at", Operator: query.OpEq, Value: 0},
		{Key: "category", Operator: query.OpEq, Value: "default"},
		{Key: "created_by", Operator: query.OpEq, Value: userID},
		{Key: "enabled", Operator: query.OpEq, Value: true},
	}
	ens, err := s.srv.FindAll(ctx, queries)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	if len(ens) == 0 {
		return nil, nil
	}
	return s.asm.E2DFind(ens[0]), nil
}

func (s *SysModelService) requireOwner(ctx context.Context, modelID, userID string) (*entity.SysModel, error) {
	model, err := s.srv.FindById(ctx, modelID)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	if model == nil || model.Ulid == "" || model.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("model not found or deleted")
	}
	if model.CreatedBy != userID {
		return nil, apierror.ErrForbidden.WithMessage("只能使用自己绑定的模型")
	}
	return model, nil
}

func (s *SysModelService) CreateModelKey(ctx context.Context, req *dto.CreateModelKeyReq) (*dto.ModelKeyRsp, error) {
	key := &entity.SysModelKey{UserID: req.UserID, Name: strings.TrimSpace(req.Name), Provider: strings.TrimSpace(req.Provider), APIKey: strings.TrimSpace(req.APIKey), BaseURL: strings.TrimSpace(req.BaseURL), Enabled: true}
	id, err := s.srv.CreateKey(ctx, key)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	key.Ulid = id
	return modelKeyResponse(key, 0), nil
}

func (s *SysModelService) UpdateModelKey(ctx context.Context, req *dto.UpdateModelKeyReq) error {
	existing, err := s.requireKeyOwner(ctx, req.Ulid, req.UserID)
	if err != nil {
		return log.WrapError(err, "SysModelService")
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, existing.Provider) {
		count, err := s.srv.CountModelsByKey(ctx, req.Ulid, req.UserID)
		if err != nil {
			return log.WrapError(err, "SysModelService")
		}
		if count > 0 {
			return apierror.ErrBadRequest.WithMessage("该 Key 已被模型使用，不能修改供应商")
		}
	}
	updated := &entity.SysModelKey{Ulid: req.Ulid, UserID: req.UserID, Name: req.Name, Provider: req.Provider, APIKey: req.APIKey, BaseURL: req.BaseURL, Enabled: existing.Enabled}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	return log.WrapError(s.srv.UpdateKey(ctx, updated), "SysModelService.UpdateModelKey")
}

func (s *SysModelService) DeleteModelKey(ctx context.Context, keyID, userID string) error {
	if _, err := s.requireKeyOwner(ctx, keyID, userID); err != nil {
		return log.WrapError(err, "SysModelService")
	}
	count, err := s.srv.CountModelsByKey(ctx, keyID, userID)
	if err != nil {
		return log.WrapError(err, "SysModelService")
	}
	if count > 0 {
		return apierror.ErrBadRequest.WithMessagef("该 Key 正被 %d 个模型使用，请先更换这些模型的 Key", count)
	}
	return log.WrapError(s.srv.DeleteKey(ctx, keyID, userID), "SysModelService.DeleteModelKey")
}

func (s *SysModelService) FindModelKeys(ctx context.Context, userID string) ([]*dto.ModelKeyRsp, error) {
	keys, err := s.srv.FindKeysByUser(ctx, userID)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	out := make([]*dto.ModelKeyRsp, 0, len(keys))
	for _, key := range keys {
		count, err := s.srv.CountModelsByKey(ctx, key.Ulid, userID)
		if err != nil {
			return nil, log.WrapError(err, "SysModelService")
		}
		out = append(out, modelKeyResponse(key, count))
	}
	return out, nil
}

func (s *SysModelService) validateModelKey(ctx context.Context, keyID, userID, provider, baseURL string) error {
	if strings.TrimSpace(keyID) == "" {
		if entity.RequiresAPIKey(provider, baseURL) {
			return apierror.ErrBadRequest.WithMessage("该远程模型需要选择一个模型 Key")
		}
		return nil
	}
	key, err := s.requireKeyOwner(ctx, keyID, userID)
	if err != nil {
		return log.WrapError(err, "SysModelService")
	}
	if !key.Enabled {
		return apierror.ErrBadRequest.WithMessage("选择的模型 Key 已停用")
	}
	if provider != "" && !strings.EqualFold(key.Provider, provider) {
		return apierror.ErrBadRequest.WithMessage(fmt.Sprintf("Key 供应商 %s 与模型供应商 %s 不匹配", key.Provider, provider))
	}
	return nil
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (s *SysModelService) requireKeyOwner(ctx context.Context, keyID, userID string) (*entity.SysModelKey, error) {
	key, err := s.srv.FindKeyByID(ctx, keyID)
	if err != nil {
		return nil, log.WrapError(err, "SysModelService")
	}
	if key == nil || key.Ulid == "" || key.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("model key not found")
	}
	if key.UserID != userID {
		return nil, apierror.ErrForbidden.WithMessage("只能使用自己的模型 Key")
	}
	return key, nil
}

func (s *SysModelService) enrichKey(ctx context.Context, rsp *dto.FindSysModelRsp) {
	if rsp == nil || rsp.KeyID == "" {
		return
	}
	if key, err := s.srv.FindKeyByID(ctx, rsp.KeyID); err == nil && key != nil {
		rsp.KeyName = key.Name
	}
}

func modelKeyResponse(key *entity.SysModelKey, count int64) *dto.ModelKeyRsp {
	return &dto.ModelKeyRsp{Ulid: key.Ulid, CreatedAt: key.CreatedAt, UpdatedAt: key.UpdatedAt, Name: key.Name, Provider: key.Provider, BaseURL: key.BaseURL, KeyMask: maskKey(key.APIKey), HasKey: key.APIKey != "", Enabled: key.Enabled, ModelCount: count}
}

func maskKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未设置"
	}
	if len(value) <= 8 {
		return "********"
	}
	return value[:4] + "..." + value[len(value)-4:]
}
