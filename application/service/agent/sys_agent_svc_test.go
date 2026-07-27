package agent

import (
	"encoding/json"
	"testing"
)

func TestPreserveSensitiveConfigMatchesSubAgentByID(t *testing.T) {
	existing := `{"systemPrompt":"secret","subAgents":[{"id":"reviewer","prompt":"review secret"},{"id":"coder","prompt":"code secret"}]}`
	incoming := `{"systemPrompt":"","subAgents":[{"id":"coder","prompt":""},{"id":"reviewer","prompt":"new review prompt"}]}`

	var got map[string]any
	if err := json.Unmarshal([]byte(preserveSensitiveConfig(existing, incoming)), &got); err != nil {
		t.Fatal(err)
	}
	if got["systemPrompt"] != "secret" {
		t.Fatalf("systemPrompt = %v, want preserved value", got["systemPrompt"])
	}
	items := got["subAgents"].([]any)
	if items[0].(map[string]any)["prompt"] != "code secret" {
		t.Fatalf("coder prompt was not preserved by id")
	}
	if items[1].(map[string]any)["prompt"] != "new review prompt" {
		t.Fatalf("explicit reviewer prompt was not retained")
	}
}

func TestModelBindingConfigIncludesEmbeddingModel(t *testing.T) {
	var got struct {
		Models         map[string]string `json:"models"`
		EmbeddingModel string            `json:"embeddingModel"`
		ImageModel     string            `json:"imageModel"`
	}
	if err := json.Unmarshal([]byte(modelBindingConfig("chat-model", "embedding-model", "image-model")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Models["default"] != "chat-model" || got.Models["summarize"] != "chat-model" {
		t.Fatalf("chat model roles = %#v", got.Models)
	}
	if got.EmbeddingModel != "embedding-model" {
		t.Fatalf("embeddingModel = %q, want embedding-model", got.EmbeddingModel)
	}
	if got.ImageModel != "image-model" {
		t.Fatalf("imageModel = %q, want image-model", got.ImageModel)
	}
}
