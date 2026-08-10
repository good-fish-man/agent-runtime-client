package runtime

import (
	"testing"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestStreamCapturePreservesResearchEvidence(t *testing.T) {
	capture := newStreamCapture()
	capture.capture(&entity.StreamEvent{
		Type: entity.StreamTypeProgress,
		Progress: &controlentity.Progress{
			Capability: "research.execute",
			State: map[string]any{
				"queries": 2, "sources": 1, "confidence": 0.82,
				"query_texts": []string{"official protocol"},
				"valuable_pages": []any{map[string]any{
					"title": "Official protocol", "url": "https://example.com/spec",
				}},
			},
		},
	})

	if capture.research == nil {
		t.Fatal("research evidence was not captured")
	}
	pages, ok := capture.research["pages"].([]any)
	if !ok || len(pages) != 1 {
		t.Fatalf("unexpected research pages: %#v", capture.research["pages"])
	}
	if capture.research["queryCount"] != 2 || capture.research["sourceCount"] != 1 {
		t.Fatalf("research summary was not preserved: %#v", capture.research)
	}
}

func TestResearchSnapshotIgnoresProgressWithoutEvidence(t *testing.T) {
	progress := &controlentity.Progress{
		Capability: "research.execute",
		State:      map[string]any{"valuable_pages": []any{}},
	}
	if snapshot := researchSnapshotFromProgress(progress); snapshot != nil {
		t.Fatalf("expected nil snapshot, got %#v", snapshot)
	}
}

func TestRecordingModelIdentity(t *testing.T) {
	models := map[string]entity.ModelConfig{
		"default": {
			Name: "gpt-5-mini",
			ExtraFields: map[string]any{
				"model_id": "model-123",
			},
		},
	}

	if got := recordingModelID(models); got != "model-123" {
		t.Fatalf("recordingModelID() = %q, want model-123", got)
	}
	if got := recordingModelName(nil, models); got != "gpt-5-mini" {
		t.Fatalf("recordingModelName() fallback = %q, want gpt-5-mini", got)
	}
	metadata := &entity.ResponseMetadata{Model: "provider-model-name"}
	if got := recordingModelName(metadata, models); got != "provider-model-name" {
		t.Fatalf("recordingModelName() = %q, want provider-model-name", got)
	}
}
