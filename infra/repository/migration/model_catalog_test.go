package migration

import "testing"

func TestDefaultModelCatalogIncludesCurrentModels(t *testing.T) {
	want := map[string]struct {
		provider      string
		contextWindow string
	}{
		"gpt-5.6":             {provider: "OpenAI", contextWindow: "1.05m"},
		"gpt-5.6-sol":         {provider: "OpenAI", contextWindow: "1.05m"},
		"claude-sonnet-5":     {provider: "Anthropic", contextWindow: "1m"},
		"gemini-3.6-flash":    {provider: "Google", contextWindow: "1m"},
		"deepseek-v4-pro":     {provider: "DeepSeek", contextWindow: "1m"},
		"qwen3.8-max-preview": {provider: "Alibaba Model Studio", contextWindow: "983k"},
		"grok-4.5":            {provider: "xAI", contextWindow: "500k"},
		"mistral-medium-3-5":  {provider: "Mistral", contextWindow: "256k"},
		"MiniMax-M2.7":        {provider: "MiniMax", contextWindow: "204.8k"},
		"gpt-image-2":         {provider: "OpenAI", contextWindow: ""},
	}

	seen := make(map[string]bool)
	for _, item := range defaultModelCatalog() {
		if seen[item.ModelVersion] {
			t.Fatalf("duplicate model version %q", item.ModelVersion)
		}
		seen[item.ModelVersion] = true

		expected, ok := want[item.ModelVersion]
		if !ok {
			continue
		}
		if item.Provider != expected.provider || item.ContextWindow != expected.contextWindow {
			t.Errorf("model %q = provider %q, context %q; want provider %q, context %q", item.ModelVersion, item.Provider, item.ContextWindow, expected.provider, expected.contextWindow)
		}
		delete(want, item.ModelVersion)
	}

	for model := range want {
		t.Errorf("default catalog is missing %q", model)
	}
}

func TestDefaultModelCatalogUsesGeminiOpenAIEndpoint(t *testing.T) {
	const want = "https://generativelanguage.googleapis.com/v1beta/openai"
	for _, item := range defaultModelCatalog() {
		if item.Provider == "Google" && item.DefaultBaseUrl != want {
			t.Errorf("Google model %q base URL = %q, want %q", item.ModelVersion, item.DefaultBaseUrl, want)
		}
	}
}
