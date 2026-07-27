package runtime

import (
	"testing"

	agententity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestBindStoredAgentModelsUsesPersistedDefault(t *testing.T) {
	configured := map[string]entity.ModelConfig{
		"rewrite": {ExtraFields: map[string]any{"model_id": "rewrite-model"}},
	}
	target := map[string]entity.ModelConfig{
		"default": {ExtraFields: map[string]any{"model_id": "request-model"}},
	}

	err := bindStoredAgentModels(&agententity.SysAgent{Model: "agent-model"}, configured, &target)
	if err != nil {
		t.Fatalf("bindStoredAgentModels() error = %v", err)
	}
	if got := modelID(target["default"]); got != "agent-model" {
		t.Fatalf("default model = %q, want agent-model", got)
	}
	if got := modelID(target["rewrite"]); got != "rewrite-model" {
		t.Fatalf("rewrite model = %q, want rewrite-model", got)
	}
}

func TestBindStoredAgentModelsAllowsUserDefaultFallback(t *testing.T) {
	target := map[string]entity.ModelConfig{"default": {ExtraFields: map[string]any{"model_id": "request-model"}}}
	if err := bindStoredAgentModels(&agententity.SysAgent{}, nil, &target); err != nil {
		t.Fatalf("bindStoredAgentModels() error = %v", err)
	}
	if _, exists := target["default"]; exists {
		t.Fatal("unbound Agent kept a default model instead of allowing user fallback")
	}
}

func TestHasConfiguredDefaultModel(t *testing.T) {
	if hasConfiguredDefaultModel(map[string]entity.ModelConfig{"embedding": {Name: "embed"}}) {
		t.Fatal("embedding-only config was accepted as a default LLM")
	}
	if !hasConfiguredDefaultModel(map[string]entity.ModelConfig{"default": {ExtraFields: map[string]any{"model_id": "model-1"}}}) {
		t.Fatal("model ID was not recognized as a configured default")
	}
	if !hasConfiguredDefaultModel(map[string]entity.ModelConfig{"default": {Name: "gpt-test"}}) {
		t.Fatal("inline model config was not recognized as a configured default")
	}
}

func TestBindStoredAgentModelsUsesEffectiveSystemAgentBinding(t *testing.T) {
	target := map[string]entity.ModelConfig{}

	err := bindStoredAgentModels(&agententity.SysAgent{IsSystem: true, Model: "user-model", EmbeddingModel: "embedding-model", ImageModel: "image-model"}, nil, &target)
	if err != nil {
		t.Fatalf("bindStoredAgentModels() error = %v", err)
	}
	if got := modelID(target["default"]); got != "user-model" {
		t.Fatalf("default model = %q, want user-model", got)
	}
	if got := modelID(target["embedding"]); got != "embedding-model" {
		t.Fatalf("embedding model = %q, want embedding-model", got)
	}
	if got := modelID(target["image"]); got != "image-model" {
		t.Fatalf("image model = %q, want image-model", got)
	}
}

func TestModelTypeForRole(t *testing.T) {
	if got := modelTypeForRole("embedding"); got != "embedding" {
		t.Fatalf("embedding role type = %q", got)
	}
	if got := modelTypeForRole("image"); got != "image" {
		t.Fatalf("image role type = %q", got)
	}
	for _, role := range []string{"default", "rewrite", "skill", "summarize"} {
		if got := modelTypeForRole(role); got != "llm" {
			t.Fatalf("%s role type = %q, want llm", role, got)
		}
	}
}

func TestValidateRuntimeModelType(t *testing.T) {
	if err := validateRuntimeModelType("embedding", "embedding"); err != nil {
		t.Fatalf("embedding validation error = %v", err)
	}
	if err := validateRuntimeModelType("llm", "embedding"); err == nil {
		t.Fatal("LLM accepted for embedding role")
	}
	if err := validateRuntimeModelType("embedding", "llm"); err == nil {
		t.Fatal("embedding accepted for LLM role")
	}
}
