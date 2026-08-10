package runtime

import (
	"testing"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestFromStreamEventPromotesResearchProgress(t *testing.T) {
	payload, err := structpb.NewStruct(map[string]any{
		"protocol": "athena.research.v3", "action_id": "research-trace-1",
		"capability": "research.execute", "stage": "ranking", "message": "Ranking evidence",
		"progress": 72, "round": 2, "queries": 4, "sources": 5, "confidence": 0.81,
		"query_texts": []any{"official protocol specification", "protocol implementation"},
		"valuable_pages": []any{map[string]any{
			"id": "source-1", "rank": 1, "title": "Official protocol", "url": "https://example.com/spec",
			"domain": "untrusted.example", "provider": "official", "kind": "official",
			"snippet": "Relevant evidence", "value_signals": []any{"opened", "authoritative"},
			"authority": 0.92, "relevance": 0.88, "freshness": 0.4, "evidence_score": 0.86, "fetched": true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := fromStreamEvent(&runtimev1.StreamEvent{
		TraceId: "trace-1",
		Payload: &runtimev1.StreamEvent_ToolResult{ToolResult: &runtimev1.ToolResultEvent{
			Id: "research-trace-1", Tool: "research.progress", Output: payload, Success: true,
		}},
	})
	if event.Type != entity.StreamTypeProgress || event.Progress == nil {
		t.Fatalf("research progress was not promoted: %+v", event)
	}
	if event.Progress.ActionID != "research-trace-1" || event.Progress.Capability != "research.execute" || event.Progress.Progress != 72 {
		t.Fatalf("unexpected progress mapping: %+v", event.Progress)
	}
	if event.Progress.State["sources"] != 5 || event.Progress.State["confidence"] != 0.81 {
		t.Fatalf("progress state was not preserved: %+v", event.Progress.State)
	}
	queries, ok := event.Progress.State["query_texts"].([]string)
	if !ok || len(queries) != 2 || queries[0] != "official protocol specification" {
		t.Fatalf("research query details were not preserved: %#v", event.Progress.State["query_texts"])
	}
	pages, ok := event.Progress.State["valuable_pages"].([]any)
	if !ok || len(pages) != 1 {
		t.Fatalf("valuable pages were not preserved: %#v", event.Progress.State["valuable_pages"])
	}
	page, ok := pages[0].(map[string]any)
	if !ok || page["domain"] != "example.com" || page["fetched"] != true {
		t.Fatalf("valuable page was not sanitized: %#v", pages[0])
	}
}
