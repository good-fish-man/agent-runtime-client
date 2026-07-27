package model

import "strings"

// Model providers, types, and runtime modes are shared by persistence,
// installation, and runtime dispatch code.
const (
	ProviderOllama           = "ollama"
	ProviderOllamaDisplay    = "Ollama"
	ProviderDiffusers        = "diffusers"
	ProviderDiffusersDisplay = "Diffusers"

	ModelTypeLLM       = "llm"
	ModelTypeEmbedding = "embedding"
	ModelTypeImage     = "image"

	RuntimeModeAlwaysOn = "always_on"
	RuntimeModeOnDemand = "on_demand"
	RuntimeModeOff      = "off"

	ModelStatusActive = "active"

	OllamaAPIBase       = "http://127.0.0.1:11434"
	OllamaOpenAIBaseURL = "http://localhost:11434/v1"
)

// NormalizeRuntimeMode applies the safe default used for local models.
func NormalizeRuntimeMode(value string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return RuntimeModeOnDemand
	}
	return mode
}
