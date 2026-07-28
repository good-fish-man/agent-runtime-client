// Package runtime (application/service) orchestrates request DTOs through the
// domain service: assemble -> inject trace id -> delegate. It returns domain
// entities directly (they carry json tags), so the API layer serializes them.
package runtime

import (
	"context"
	"encoding/json"
	"strings"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/runtime"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	memorysvc "github.com/good-fish-man/agent-runtime-client/application/service/memory"
	agententity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	modelentity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/runtime"
	agentsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/agent"
	modelsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/model"
	dsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// StreamFunc receives streaming events (re-exported so the API layer need not
// import the domain irepository package).
type StreamFunc = irepo.StreamFunc

// RuntimeService is the application service for runtime invocation.
type RuntimeService struct {
	svc       *dsrv.RuntimeSvc
	agentSvc  *agentsrv.SysAgentSvc
	modelSvc  *modelsrv.SysModelSvc
	memorySvc *memorysvc.Service
}

// NewRuntimeService builds the application service.
func NewRuntimeService(svc *dsrv.RuntimeSvc, agentSvc *agentsrv.SysAgentSvc, modelSvc *modelsrv.SysModelSvc, memorySvc *memorysvc.Service) *RuntimeService {
	out := &RuntimeService{svc: svc}
	out.agentSvc = agentSvc
	out.modelSvc = modelSvc
	out.memorySvc = memorySvc
	return out
}

// Run executes a non-streaming run.
func (s *RuntimeService) Run(ctx context.Context, req *dto.RunReq) (*entity.Completion, error) {
	in := assembler.ToRunInput(req)
	in.TraceID = traceID(ctx)
	if err := s.hydrateAgentConfig(ctx, in); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.injectMemories(ctx, in.Context); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.ensureUserModel(ctx, &in.Models); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.hydrateModels(ctx, in.Models); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.hydrateSubAgentModels(ctx, in.SubAgents); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	result, err := s.svc.Run(ctx, in)
	if err == nil {
		s.storeMemories(ctx, in.Context, result.Memories)
	}
	return result, err
}

// RunStream executes a streaming run.
func (s *RuntimeService) RunStream(ctx context.Context, req *dto.RunReq, emit StreamFunc) error {
	in := assembler.ToRunInput(req)
	in.TraceID = traceID(ctx)
	in.Options = withStream(in.Options)
	if err := s.hydrateAgentConfig(ctx, in); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.injectMemories(ctx, in.Context); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.ensureUserModel(ctx, &in.Models); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.hydrateModels(ctx, in.Models); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.hydrateSubAgentModels(ctx, in.SubAgents); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	return errtrace.Wrap(s.svc.RunStream(ctx, in, s.memoryAwareEmitter(ctx, in.Context, emit)), "RuntimeService.RunStream")
}

// RunAgent executes a non-streaming agent run.
func (s *RuntimeService) RunAgent(ctx context.Context, req *dto.AgentReq) (*entity.AgentResult, error) {
	in := assembler.ToAgentInput(req)
	in.TraceID = traceID(ctx)
	if err := s.hydrateAgentModels(ctx, in.Context, &in.Models); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.injectMemories(ctx, in.Context); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.ensureUserModel(ctx, &in.Models); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.hydrateModels(ctx, in.Models); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	value, err := s.svc.RunAgent(ctx, in)
	return value, errtrace.Wrap(err, "RuntimeService.RunAgent")
}

// RunAgentStream executes a streaming agent run.
func (s *RuntimeService) RunAgentStream(ctx context.Context, req *dto.AgentReq, emit StreamFunc) error {
	in := assembler.ToAgentInput(req)
	in.TraceID = traceID(ctx)
	in.Stream = true
	if err := s.hydrateAgentModels(ctx, in.Context, &in.Models); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.injectMemories(ctx, in.Context); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.ensureUserModel(ctx, &in.Models); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if err := s.hydrateModels(ctx, in.Models); err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	return errtrace.Wrap(s.svc.RunAgentStream(ctx, in, s.memoryAwareEmitter(ctx, in.Context, emit)), "RuntimeService.RunAgentStream")
}

// Resume resumes a checkpointed run.
func (s *RuntimeService) Resume(ctx context.Context, req *dto.ResumeReq) (*entity.ResumeResult, error) {
	in := assembler.ToResumeInput(req)
	in.TraceID = traceID(ctx)
	value, err := s.svc.Resume(ctx, in)
	return value, errtrace.Wrap(err, "RuntimeService.Resume")
}

// Stop stops a run.
func (s *RuntimeService) Stop(ctx context.Context, req *dto.StopReq) (*entity.StopResult, error) {
	in := assembler.ToStopInput(req)
	in.TraceID = traceID(ctx)
	value, err := s.svc.Stop(ctx, in)
	return value, errtrace.Wrap(err, "RuntimeService.Stop")
}

// Health probes runtime health.
func (s *RuntimeService) Health(ctx context.Context) (*entity.HealthStatus, error) {
	value, err := s.svc.Health(ctx, &entity.HealthInput{Service: "agent-runtime", TraceID: traceID(ctx)})
	return value, errtrace.Wrap(err, "RuntimeService.Health")
}

func withStream(o *entity.RunOptions) *entity.RunOptions {
	if o == nil {
		o = &entity.RunOptions{}
	}
	o.Stream = true
	return o
}

// traceID reads the request trace id bound to the context by the trace middleware.
func traceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(log.ReqIDKey).(string); ok {
		return v
	}
	return ""
}

type storedAgentConfig struct {
	SystemPrompt   string                        `json:"system_prompt"`
	SystemPromptUI string                        `json:"systemPrompt"`
	Models         map[string]entity.ModelConfig `json:"models"`
	Context        map[string]any                `json:"context"`
	KnowledgeBases []entity.KnowledgeBaseConfig  `json:"knowledge_bases"`
	Knowledge      []entity.KnowledgeBaseConfig  `json:"knowledge"`
	Skills         []entity.Skill                `json:"skills"`
	MCPs           []entity.MCPConfig            `json:"mcps"`
	CLIs           []entity.CLIConfig            `json:"clis"`
	A2A            []entity.A2AAgentConfig       `json:"a2a"`
	Tools          []entity.ToolConfig           `json:"tools"`
	InternalAgents []entity.InternalAgentConfig  `json:"internal_agents"`
	SubAgents      []entity.SubAgentConfig       `json:"sub_agents"`
	Options        *entity.RunOptions            `json:"options"`
	Sandbox        *entity.SandboxConfig         `json:"sandbox"`
}

func (s *RuntimeService) hydrateAgentConfig(ctx context.Context, in *entity.RunInput) error {
	if s.agentSvc == nil || in == nil {
		return nil
	}
	stripPromptContext(in.Context)
	agentID := agentIDFromContext(in.Context)
	if agentID == "" {
		return nil
	}
	agent, err := s.agentSvc.FindById(ctx, agentID)
	if err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if agent == nil || agent.Ulid == "" || agent.DeletedAt != 0 || !agent.Enabled {
		return apierror.ErrNotFound.WithMessage("agent not found or disabled")
	}
	if !agent.IsSystem && agent.CreatedBy != authctx.UserID(ctx) {
		return apierror.ErrForbidden.WithMessage("只能使用自己的 Agent")
	}
	if agent.IsSystem {
		binding, bindingErr := s.agentSvc.FindUserModel(ctx, authctx.UserID(ctx), agent.Ulid)
		if bindingErr != nil {
			return bindingErr
		}
		if binding != nil {
			agent.Model = binding.Model
			agent.EmbeddingModel = binding.EmbeddingModel
			agent.ImageModel = binding.ImageModel
		}
	}
	cfg, ok := parseStoredAgentConfig(agent.ConfigJson)
	if !ok {
		cfg, ok = parseStoredAgentConfig(agent.Config)
	}
	if ok {
		mergeStoredAgentConfig(in, cfg)
	}
	return bindStoredAgentModels(agent, modelsFromConfig(cfg), &in.Models)
}

func agentIDFromContext(ctx map[string]any) string {
	if ctx == nil {
		return ""
	}
	for _, key := range []string{"agent_id", "agentId"} {
		if v, ok := ctx[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseStoredAgentConfig(raw string) (*storedAgentConfig, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var cfg storedAgentConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, false
	}
	return &cfg, true
}

func mergeStoredAgentConfig(in *entity.RunInput, cfg *storedAgentConfig) {
	if in.Context == nil {
		in.Context = map[string]any{}
	}
	for k, v := range cfg.Context {
		if _, exists := in.Context[k]; !exists {
			in.Context[k] = v
		}
	}
	if len(in.Models) == 0 {
		in.Models = cfg.Models
	}
	if len(in.KnowledgeBases) == 0 {
		in.KnowledgeBases = firstKnowledgeBases(cfg.KnowledgeBases, cfg.Knowledge)
	}
	if len(in.Skills) == 0 {
		in.Skills = cfg.Skills
	}
	if len(in.MCPs) == 0 {
		in.MCPs = cfg.MCPs
	}
	if len(in.CLIs) == 0 {
		in.CLIs = cfg.CLIs
	}
	if len(in.A2A) == 0 {
		in.A2A = cfg.A2A
	}
	if len(in.Tools) == 0 {
		in.Tools = cfg.Tools
	}
	if len(in.InternalAgents) == 0 {
		in.InternalAgents = cfg.InternalAgents
	}
	if len(in.SubAgents) == 0 {
		in.SubAgents = cfg.SubAgents
	}
	if in.Options == nil {
		in.Options = cfg.Options
	}
	if in.Sandbox == nil {
		in.Sandbox = cfg.Sandbox
	}
	if prompt := firstNonEmpty(cfg.SystemPrompt, cfg.SystemPromptUI); prompt != "" {
		in.Context["system_prompt"] = prompt
	}
}

func firstKnowledgeBases(primary, fallback []entity.KnowledgeBaseConfig) []entity.KnowledgeBaseConfig {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}

func stripPromptContext(ctx map[string]any) {
	for _, key := range []string{"system_prompt", "systemPrompt", "rewrite_prompt", "rewritePrompt", "summarize_prompt", "summarizePrompt", "prompt", "instruction"} {
		delete(ctx, key)
	}
}

func (s *RuntimeService) hydrateModels(ctx context.Context, models map[string]entity.ModelConfig) error {
	if s.modelSvc == nil || len(models) == 0 {
		return nil
	}
	for role, cfg := range models {
		cfg.APIKey = ""
		model, err := s.resolveModel(ctx, cfg, modelTypeForRole(role))
		if err != nil {
			return errtrace.Wrap(err, "RuntimeService")
		}
		if model == nil {
			return apierror.ErrModelBindingRequired
		}
		cfg.Provider = firstNonEmpty(cfg.Provider, model.Provider)
		cfg.Name = firstNonEmpty(cfg.Name, model.Name)
		cfg.APIBase = firstNonEmpty(cfg.APIBase, model.BaseUrl)
		cfg.APIKey = model.ApiKey
		if cfg.ExtraFields == nil {
			cfg.ExtraFields = make(map[string]any)
		}
		cfg.ExtraFields["runtime_mode"] = model.RuntimeMode
		models[role] = cfg
	}
	return nil
}

func (s *RuntimeService) hydrateSubAgentModels(ctx context.Context, subAgents []entity.SubAgentConfig) error {
	for i := range subAgents {
		if subAgents[i].Model == nil {
			continue
		}
		cfg := *subAgents[i].Model
		cfg.APIKey = ""
		model, err := s.resolveModel(ctx, cfg, "llm")
		if err != nil {
			return errtrace.Wrap(err, "RuntimeService")
		}
		if model == nil {
			return apierror.ErrModelBindingRequired.WithMessage("子 Agent 绑定的模型不存在")
		}
		cfg.Provider = firstNonEmpty(cfg.Provider, model.Provider)
		cfg.Name = firstNonEmpty(cfg.Name, model.Name)
		cfg.APIBase = firstNonEmpty(cfg.APIBase, model.BaseUrl)
		cfg.APIKey = model.ApiKey
		if cfg.ExtraFields == nil {
			cfg.ExtraFields = make(map[string]any)
		}
		cfg.ExtraFields["runtime_mode"] = model.RuntimeMode
		subAgents[i].Model = &cfg
	}
	return nil
}

func (s *RuntimeService) resolveModel(ctx context.Context, cfg entity.ModelConfig, expectedType string) (*modelEntity, error) {
	userID := authctx.UserID(ctx)
	if id := modelID(cfg); id != "" {
		m, err := s.modelSvc.FindById(ctx, id)
		if err != nil {
			return nil, errtrace.Wrap(err, "RuntimeService")
		}
		if m != nil && m.DeletedAt == 0 && m.CreatedBy == userID {
			if !m.Enabled {
				return nil, apierror.ErrForbidden.WithMessage("当前模型已被管理员停用")
			}
			if err := validateRuntimeModelType(m.ModelType, expectedType); err != nil {
				return nil, errtrace.Wrap(err, "RuntimeService")
			}
			return s.resolveStoredModel(ctx, m, userID)
		}
		return nil, apierror.ErrForbidden.WithMessage("只能使用自己绑定的模型")
	}

	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, nil
	}
	queries := []*query.Query{
		{Key: "deleted_at", Operator: query.OpEq, Value: 0},
		{Key: "name", Operator: query.OpEq, Value: name},
		{Key: "created_by", Operator: query.OpEq, Value: userID},
		{Key: "enabled", Operator: query.OpEq, Value: true},
	}
	if provider := strings.TrimSpace(cfg.Provider); provider != "" {
		queries = append(queries, &query.Query{Key: "provider", Operator: query.OpEq, Value: provider})
	}
	ens, err := s.modelSvc.FindAll(ctx, queries)
	if err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if len(ens) == 0 {
		return nil, nil
	}
	m := ens[0]
	if err := validateRuntimeModelType(m.ModelType, expectedType); err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	return s.resolveStoredModel(ctx, m, userID)
}

func modelTypeForRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "embedding") {
		return "embedding"
	}
	if strings.EqualFold(strings.TrimSpace(role), "image") {
		return "image"
	}
	return "llm"
}

func validateRuntimeModelType(actual, expected string) error {
	if strings.EqualFold(actual, expected) {
		return nil
	}
	if strings.EqualFold(expected, "embedding") {
		return apierror.ErrBadRequest.WithMessage("Agent 的 Embedding 角色只能使用 Embedding 模型")
	}
	if strings.EqualFold(expected, "image") {
		return apierror.ErrBadRequest.WithMessage("Agent 的图片生成角色只能使用 Image 模型")
	}
	return apierror.ErrBadRequest.WithMessage("Agent 只能使用 LLM 模型")
}

func (s *RuntimeService) resolveStoredModel(ctx context.Context, model *modelentity.SysModel, userID string) (*modelEntity, error) {
	runtimeMode := modelentity.NormalizeRuntimeMode(model.RuntimeMode)
	if runtimeMode == modelentity.RuntimeModeOff {
		return nil, apierror.ErrForbidden.WithMessage("当前本地模型已被管理员关闭")
	}
	if model.KeyID == "" {
		if modelentity.RequiresAPIKey(model.Provider, model.BaseUrl) {
			return nil, apierror.ErrModelBindingRequired.WithMessage("远程模型尚未绑定 Key")
		}
		return &modelEntity{Provider: model.Provider, Name: model.Name, BaseUrl: model.BaseUrl, RuntimeMode: runtimeMode}, nil
	}
	apiKey := ""
	baseURL := model.BaseUrl
	key, err := s.modelSvc.FindKeyByID(ctx, model.KeyID)
	if err != nil {
		return nil, errtrace.Wrap(err, "RuntimeService")
	}
	if key == nil || key.Ulid == "" || key.DeletedAt != 0 || !key.Enabled {
		return nil, apierror.ErrModelBindingRequired.WithMessage("模型绑定的 Key 不存在或已停用")
	}
	if key.UserID != userID {
		return nil, apierror.ErrForbidden.WithMessage("只能使用自己的模型 Key")
	}
	apiKey = key.APIKey
	baseURL = firstNonEmpty(baseURL, key.BaseURL)
	if strings.TrimSpace(apiKey) == "" && modelentity.RequiresAPIKey(model.Provider, baseURL) {
		return nil, apierror.ErrModelBindingRequired.WithMessage("模型 Key 尚未设置")
	}
	return &modelEntity{Provider: model.Provider, Name: model.Name, BaseUrl: baseURL, ApiKey: apiKey, RuntimeMode: runtimeMode}, nil
}

type modelEntity struct {
	Provider    string
	Name        string
	BaseUrl     string
	ApiKey      string
	RuntimeMode string
}

func modelID(cfg entity.ModelConfig) string {
	if cfg.ExtraFields == nil {
		return ""
	}
	for _, key := range []string{"model_id", "ulid", "id"} {
		if v, ok := cfg.ExtraFields[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstNonEmpty(current, fallback string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	return fallback
}

func (s *RuntimeService) ensureUserModel(ctx context.Context, models *map[string]entity.ModelConfig) error {
	if s.modelSvc == nil {
		return apierror.ErrModelBindingRequired
	}
	if models != nil && hasConfiguredDefaultModel(*models) {
		return nil
	}
	queries := []*query.Query{
		{Key: "deleted_at", Operator: query.OpEq, Value: 0},
		{Key: "created_by", Operator: query.OpEq, Value: authctx.UserID(ctx)},
		{Key: "model_type", Operator: query.OpEq, Value: modelentity.ModelTypeLLM},
		{Key: "enabled", Operator: query.OpEq, Value: true},
		{Key: "runtime_mode", Operator: query.OpNe1, Value: modelentity.RuntimeModeOff},
	}
	items, err := s.modelSvc.FindAll(ctx, append(append([]*query.Query{}, queries...), &query.Query{Key: "category", Operator: query.OpEq, Value: "default"}))
	if err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if len(items) == 0 {
		items, err = s.modelSvc.FindAll(ctx, queries)
		if err != nil {
			return errtrace.Wrap(err, "RuntimeService")
		}
	}
	if len(items) == 0 {
		return apierror.ErrModelBindingRequired
	}
	if *models == nil {
		*models = make(map[string]entity.ModelConfig)
	}
	(*models)["default"] = entity.ModelConfig{ExtraFields: map[string]any{"model_id": items[0].Ulid}}
	return nil
}

func hasConfiguredDefaultModel(models map[string]entity.ModelConfig) bool {
	model, ok := models["default"]
	return ok && (modelID(model) != "" || strings.TrimSpace(model.Name) != "")
}

func (s *RuntimeService) hydrateAgentModels(ctx context.Context, values map[string]any, models *map[string]entity.ModelConfig) error {
	if s.agentSvc == nil {
		return nil
	}
	id := agentIDFromContext(values)
	if id == "" {
		return nil
	}
	agent, err := s.agentSvc.FindById(ctx, id)
	if err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if agent == nil || agent.Ulid == "" || agent.DeletedAt != 0 || !agent.Enabled {
		return apierror.ErrNotFound.WithMessage("agent not found or disabled")
	}
	if !agent.IsSystem && agent.CreatedBy != authctx.UserID(ctx) {
		return apierror.ErrForbidden.WithMessage("只能使用自己的 Agent")
	}
	if agent.IsSystem {
		binding, bindingErr := s.agentSvc.FindUserModel(ctx, authctx.UserID(ctx), agent.Ulid)
		if bindingErr != nil {
			return bindingErr
		}
		if binding != nil {
			agent.Model = binding.Model
			agent.EmbeddingModel = binding.EmbeddingModel
			agent.ImageModel = binding.ImageModel
		}
	}
	cfg, ok := parseStoredAgentConfig(agent.ConfigJson)
	if !ok {
		cfg, _ = parseStoredAgentConfig(agent.Config)
	}
	return bindStoredAgentModels(agent, modelsFromConfig(cfg), models)
}

func modelsFromConfig(cfg *storedAgentConfig) map[string]entity.ModelConfig {
	if cfg == nil {
		return nil
	}
	return cfg.Models
}

func bindStoredAgentModels(agent *agententity.SysAgent, configured map[string]entity.ModelConfig, target *map[string]entity.ModelConfig) error {
	if agent == nil {
		return nil
	}
	bound := make(map[string]entity.ModelConfig, len(configured)+2)
	for role, cfg := range configured {
		bound[role] = cfg
	}
	modelID := strings.TrimSpace(agent.Model)
	if modelID == "" {
		delete(bound, "default")
		*target = bound
		return nil
	}
	bound["default"] = entity.ModelConfig{ExtraFields: map[string]any{"model_id": modelID}}
	if embeddingModelID := strings.TrimSpace(agent.EmbeddingModel); embeddingModelID != "" {
		bound["embedding"] = entity.ModelConfig{ExtraFields: map[string]any{"model_id": embeddingModelID}}
	} else {
		delete(bound, "embedding")
	}
	if imageModelID := strings.TrimSpace(agent.ImageModel); imageModelID != "" {
		bound["image"] = entity.ModelConfig{ExtraFields: map[string]any{"model_id": imageModelID}}
	} else {
		delete(bound, "image")
	}
	*target = bound
	return nil
}

func (s *RuntimeService) injectMemories(ctx context.Context, values map[string]any) error {
	if s.memorySvc == nil || values == nil {
		return nil
	}
	text, err := s.memorySvc.ContextText(ctx, authctx.UserID(ctx), agentIDFromContext(values), 20)
	if err != nil {
		return errtrace.Wrap(err, "RuntimeService")
	}
	if text != "" {
		values["long_term_memories"] = text
	}
	return nil
}

func (s *RuntimeService) storeMemories(ctx context.Context, values map[string]any, memories []entity.MemoryEntry) {
	if s.memorySvc == nil || len(memories) == 0 {
		return
	}
	entries := make([]memorysvc.CreateReq, 0, len(memories))
	for _, item := range memories {
		entries = append(entries, memorysvc.CreateReq{Name: item.Name, Description: item.Description, MemoryType: item.Type, Content: item.Content, Importance: item.Importance})
	}
	if err := s.memorySvc.StoreExtracted(ctx, authctx.UserID(ctx), agentIDFromContext(values), sessionIDFromContext(values), entries); err != nil {
		log.Warnf("store extracted memories failed: %v", err)
	}
}

func (s *RuntimeService) memoryAwareEmitter(ctx context.Context, values map[string]any, emit StreamFunc) StreamFunc {
	return func(event *entity.StreamEvent) error {
		if event != nil && event.Done != nil && len(event.Done.Memories) > 0 {
			s.storeMemories(ctx, values, event.Done.Memories)
		}
		return emit(event)
	}
}

func sessionIDFromContext(values map[string]any) string {
	for _, key := range []string{"session_id", "sessionId"} {
		if value, ok := values[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
