// Package migration owns startup database initialization for repository POs.
package migration

import (
	"context"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm/clause"

	modelentity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	agentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/agent"
	channelpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/channel"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	jobpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/job"
	kbpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge_base"
	memorypo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/memory"
	modelpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/model"
	skillpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/skill"
	userpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
)

const (
	defaultAdminUsername = "athena"
	defaultAdminPassword = "athena"
	defaultAdminNickname = "Athena Administrator"
	defaultAdminRoleID   = "admin"
	systemActor          = "system"
	activeUserState      = 1
	adminAccessLevel     = 1
)

// InitTables creates or updates every table used by agent-runtime-client.
func InitTables(ctx context.Context, d *data.Data) error {
	db := d.DB(ctx)
	if err := db.AutoMigrate(
		&userpo.SysUser{},
		&userpo.SysUserSession{},
		&userpo.SysLog{},
		&modelpo.SysModelKey{},
		&modelpo.SysModel{},
		&modelpo.ModelCatalog{},
		&modelpo.ModelTrainingJob{},
		&kbpo.SysKnowledgeBase{},
		&skillpo.SysSkill{},
		&agentpo.SysAgent{},
		&agentpo.SysAgentUserModel{},
		&channelpo.SysChannel{},
		&chatpo.ChatSession{},
		&chatpo.ChatMessage{},
		&chatpo.ChatApproval{},
		&chatpo.ChatTokenStats{},
		&memorypo.AgentMemory{},
		&jobpo.JobExecutionPO{},
	); err != nil {
		return err
	}

	// AutoMigrate never removes columns. Model credentials now live exclusively
	// in sys_model_key, so remove the obsolete per-model credential column.
	if db.Migrator().HasColumn(&modelpo.SysModel{}, "api_key") {
		if err := db.Migrator().DropColumn(&modelpo.SysModel{}, "api_key"); err != nil {
			return err
		}
	}
	if err := seedAdministrator(ctx, d); err != nil {
		return err
	}
	if err := seedModelCatalog(ctx, d); err != nil {
		return err
	}
	return seedPublicAgents(ctx, d)
}

func seedAdministrator(ctx context.Context, d *data.Data) error {
	db := d.DB(ctx)
	var existing userpo.SysUser
	if err := db.Where("member_code = ? AND deleted_at = 0", defaultAdminUsername).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	if existing.Ulid != "" {
		updates := map[string]any{}
		if existing.AdminLevel == 0 {
			updates["admin_level"] = adminAccessLevel
		}
		if existing.State != activeUserState {
			updates["state"] = activeUserState
		}
		if len(updates) == 0 {
			return nil
		}
		updates["updated_by"] = systemActor
		return db.Model(&userpo.SysUser{}).Where("ulid = ?", existing.Ulid).Updates(updates).Error
	}

	password, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.Create(&userpo.SysUser{
		MemberCode: defaultAdminUsername,
		NickName:   defaultAdminNickname,
		Password:   string(password),
		State:      activeUserState,
		AdminLevel: adminAccessLevel,
		CreatedBy:  systemActor,
		UpdatedBy:  systemActor,
		RoleId:     defaultAdminRoleID,
	}).Error
}

func seedPublicAgents(ctx context.Context, d *data.Data) error {
	db := d.DB(ctx)
	agents := []*agentpo.SysAgent{
		{Name: "通用助手", Description: "适合问答、总结和日常任务的公共 Agent", Icon: "Bot", IsSystem: true, Enabled: true, CreatedBy: "system", ConfigJson: `{"system_prompt":"你是一个可靠、简洁的通用助手。请先理解目标，再给出可执行的答案。"}`},
		{Name: "代码助手", Description: "用于阅读、解释和优化代码的公共 Agent", Icon: "Code", IsSystem: true, Enabled: true, CreatedBy: "system", ConfigJson: `{"system_prompt":"你是资深软件工程师。修改前先理解项目上下文，优先给出安全、可验证的实现。"}`},
		{Name: "数据分析助手", Description: "用于数据解读、指标分析和报告整理的公共 Agent", Icon: "ChartNoAxesCombined", IsSystem: true, Enabled: true, CreatedBy: "system", ConfigJson: `{"system_prompt":"你是数据分析助手。明确数据口径，展示关键结论，并说明不确定性。"}`},
	}
	for _, item := range agents {
		var count int64
		if err := db.Model(&agentpo.SysAgent{}).Where("name = ? AND is_system = ? AND deleted_at = 0", item.Name, true).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := db.Create(item).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func seedModelCatalog(ctx context.Context, d *data.Data) error {
	db := d.DB(ctx)
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "model_version"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"provider", "model_type", "model_family", "display_name", "default_base_url",
			"context_window", "is_free", "installable", "runtime", "download_size",
			"min_memory_gb", "capabilities", "description", "sort", "updated_at",
		}),
	}).Create(defaultModelCatalog()).Error
}

func defaultModelCatalog() []*modelpo.ModelCatalog {
	return []*modelpo.ModelCatalog{
		{Provider: "OpenAI", ModelType: "llm", ModelFamily: "GPT-5", ModelVersion: "gpt-5", DisplayName: "GPT-5", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "400k", Enabled: true, Sort: 10},
		{Provider: "OpenAI", ModelType: "llm", ModelFamily: "GPT-5", ModelVersion: "gpt-5-mini", DisplayName: "GPT-5 mini", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "400k", Enabled: true, Sort: 20},
		{Provider: "OpenAI", ModelType: "llm", ModelFamily: "GPT-4o", ModelVersion: "gpt-4o", DisplayName: "GPT-4o", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "128k", Enabled: true, Sort: 30},
		{Provider: "OpenAI", ModelType: "embedding", ModelFamily: "text-embedding-3", ModelVersion: "text-embedding-3-large", DisplayName: "text-embedding-3-large", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "8191", Enabled: true, Sort: 40},
		{Provider: "OpenAI", ModelType: "embedding", ModelFamily: "text-embedding-3", ModelVersion: "text-embedding-3-small", DisplayName: "text-embedding-3-small", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "8191", Enabled: true, Sort: 50},
		{Provider: "Anthropic", ModelType: "llm", ModelFamily: "Claude Sonnet", ModelVersion: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", DefaultBaseUrl: "https://api.anthropic.com/v1", ContextWindow: "200k", Enabled: true, Sort: 60},
		{Provider: "Anthropic", ModelType: "llm", ModelFamily: "Claude Haiku", ModelVersion: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", DefaultBaseUrl: "https://api.anthropic.com/v1", ContextWindow: "200k", Enabled: true, Sort: 70},
		{Provider: "Google", ModelType: "llm", ModelFamily: "Gemini 3", ModelVersion: "gemini-3-pro", DisplayName: "Gemini 3 Pro", DefaultBaseUrl: "https://generativelanguage.googleapis.com/v1beta", ContextWindow: "1m", Enabled: true, Sort: 80},
		{Provider: "Google", ModelType: "llm", ModelFamily: "Gemini 3", ModelVersion: "gemini-3-flash", DisplayName: "Gemini 3 Flash", DefaultBaseUrl: "https://generativelanguage.googleapis.com/v1beta", ContextWindow: "1m", Enabled: true, Sort: 90},
		{Provider: "DeepSeek", ModelType: "llm", ModelFamily: "DeepSeek", ModelVersion: "deepseek-chat", DisplayName: "DeepSeek Chat", DefaultBaseUrl: "https://api.deepseek.com/v1", ContextWindow: "64k", Enabled: true, Sort: 100},
		{Provider: "DeepSeek", ModelType: "llm", ModelFamily: "DeepSeek", ModelVersion: "deepseek-reasoner", DisplayName: "DeepSeek Reasoner", DefaultBaseUrl: "https://api.deepseek.com/v1", ContextWindow: "64k", Enabled: true, Sort: 110},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeLLM, ModelFamily: "Qwen 3", ModelVersion: "qwen3:0.6b", DisplayName: "Qwen3 0.6B", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "40k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "523MB", MinMemoryGB: 2, Capabilities: "chat,tools,reasoning,multilingual", Description: "Small multilingual model for low-memory devices", Enabled: true, Sort: 120},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeLLM, ModelFamily: "Qwen 3", ModelVersion: "qwen3:1.7b", DisplayName: "Qwen3 1.7B", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "40k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "1.4GB", MinMemoryGB: 4, Capabilities: "chat,tools,reasoning,multilingual", Description: "Balanced compact model for everyday laptops", Enabled: true, Sort: 121},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeLLM, ModelFamily: "Qwen 3", ModelVersion: "qwen3:4b", DisplayName: "Qwen3 4B", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "256k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "2.5GB", MinMemoryGB: 8, Capabilities: "chat,tools,reasoning,multilingual", Description: "Recommended local model for capable laptops", Enabled: true, Sort: 122},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeLLM, ModelFamily: "Qwen 3", ModelVersion: "qwen3:8b", DisplayName: "Qwen3 8B", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "40k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "5.2GB", MinMemoryGB: 12, Capabilities: "chat,tools,reasoning,multilingual", Description: "Higher-quality local model for systems with more memory", Enabled: true, Sort: 123},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeLLM, ModelFamily: "Gemma 3", ModelVersion: "gemma3:1b", DisplayName: "Gemma 3 1B", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "32k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "815MB", MinMemoryGB: 3, Capabilities: "chat,multilingual", Description: "Lightweight open model for resource-limited devices", Enabled: true, Sort: 124},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeLLM, ModelFamily: "Gemma 3", ModelVersion: "gemma3:4b", DisplayName: "Gemma 3 4B", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "128k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "3.3GB", MinMemoryGB: 8, Capabilities: "chat,vision,multilingual", Description: "Multimodal local model with image understanding", Enabled: true, Sort: 125},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeEmbedding, ModelFamily: "Nomic Embed", ModelVersion: "nomic-embed-text:v1.5", DisplayName: "Nomic Embed Text v1.5", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "2k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "274MB", MinMemoryGB: 2, Capabilities: "embedding", Description: "Compact local text embedding model", Enabled: true, Sort: 130},
		{Provider: modelentity.ProviderOllamaDisplay, ModelType: modelentity.ModelTypeEmbedding, ModelFamily: "EmbeddingGemma", ModelVersion: "embeddinggemma:300m", DisplayName: "EmbeddingGemma 300M", DefaultBaseUrl: modelentity.OllamaOpenAIBaseURL, ContextWindow: "2k", IsFree: true, Installable: true, Runtime: modelentity.ProviderOllama, DownloadSize: "622MB", MinMemoryGB: 2, Capabilities: "embedding,multilingual", Description: "Multilingual on-device embedding model", Enabled: true, Sort: 131},
		{Provider: "OpenAI", ModelType: "image", ModelFamily: "GPT Image", ModelVersion: "gpt-image-1", DisplayName: "GPT Image 1", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "", Capabilities: "text-to-image,image-edit", Description: "OpenAI image generation and editing model", Enabled: true, Sort: 200},
		{Provider: "OpenAI", ModelType: "image", ModelFamily: "GPT Image", ModelVersion: "gpt-image-1-mini", DisplayName: "GPT Image 1 Mini", DefaultBaseUrl: "https://api.openai.com/v1", ContextWindow: "", Capabilities: "text-to-image,image-edit", Description: "Cost-efficient OpenAI image generation model", Enabled: true, Sort: 201},
		{Provider: "Stability AI", ModelType: "image", ModelFamily: "Stable Image", ModelVersion: "stable-image-core", DisplayName: "Stable Image Core", DefaultBaseUrl: "https://api.stability.ai/v2beta/stable-image/generate/core", ContextWindow: "", Capabilities: "text-to-image", Description: "Fast cloud image generation from Stability AI", Enabled: true, Sort: 210},
		{Provider: modelentity.ProviderDiffusersDisplay, ModelType: modelentity.ModelTypeImage, ModelFamily: "Stable Diffusion XL", ModelVersion: "stabilityai/stable-diffusion-xl-base-1.0", DisplayName: "Stable Diffusion XL 1.0", DefaultBaseUrl: "diffusers://stabilityai/stable-diffusion-xl-base-1.0", ContextWindow: "", IsFree: true, Installable: true, Runtime: modelentity.ProviderDiffusers, DownloadSize: "7GB", MinMemoryGB: 12, Capabilities: "text-to-image,image-to-image,local", Description: "Open-weight local image generation model", Enabled: true, Sort: 220},
		{Provider: modelentity.ProviderDiffusersDisplay, ModelType: modelentity.ModelTypeImage, ModelFamily: "FLUX.1", ModelVersion: "black-forest-labs/FLUX.1-schnell", DisplayName: "FLUX.1 Schnell", DefaultBaseUrl: "diffusers://black-forest-labs/FLUX.1-schnell", ContextWindow: "", IsFree: true, Installable: true, Runtime: modelentity.ProviderDiffusers, DownloadSize: "24GB", MinMemoryGB: 24, Capabilities: "text-to-image,local", Description: "High quality open-weight local image generation model", Enabled: true, Sort: 221},
	}
}
