package runtime

import (
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	infrapkg "github.com/good-fish-man/agent-runtime-client/infra/pkg"
)

// fromStreamEvent converts a proto StreamEvent (oneof payload) to the domain
// StreamEvent, setting Type to the active payload.
func fromStreamEvent(ev *runtimev1.StreamEvent) *entity.StreamEvent {
	if ev == nil {
		return &entity.StreamEvent{}
	}
	out := &entity.StreamEvent{
		Seq:       ev.Seq,
		EmittedAt: tsTime(ev.EmittedAt),
		TraceID:   ev.TraceId,
	}
	switch {
	case ev.GetMeta() != nil:
		m := ev.GetMeta()
		out.Type = entity.StreamTypeMeta
		out.Meta = &entity.MetaEvent{
			StartedAt:      tsTime(m.StartedAt),
			StreamProtocol: m.StreamProtocol,
			CheckpointID:   m.CheckpointId,
			HeartbeatAt:    tsTime(m.HeartbeatAt),
		}
	case ev.GetDelta() != nil:
		d := ev.GetDelta()
		out.Type = entity.StreamTypeDelta
		out.Delta = &entity.DeltaEvent{Text: d.Text, Role: d.Role, Reasoning: d.Reasoning}
	case ev.GetToolCall() != nil:
		t := ev.GetToolCall()
		out.Type = entity.StreamTypeToolCall
		out.ToolCall = &entity.ToolCallEvent{
			ID:       t.Id,
			Tool:     t.Tool,
			ToolType: t.ToolType,
			Input:    infrapkg.FromStruct(t.Input),
		}
	case ev.GetToolResult() != nil:
		t := ev.GetToolResult()
		if t.Tool == "research.progress" {
			data := infrapkg.FromStruct(t.Output)
			out.Type = entity.StreamTypeProgress
			out.Progress = researchProgressFromMap(data, ev.TraceId, out.EmittedAt)
			break
		}
		out.Type = entity.StreamTypeToolResult
		out.ToolResult = &entity.ToolResultEvent{
			ID:        t.Id,
			Tool:      t.Tool,
			Output:    infrapkg.FromStruct(t.Output),
			Success:   t.Success,
			Error:     t.Error,
			LatencyMs: t.LatencyMs,
		}
	case ev.GetInterrupted() != nil:
		i := ev.GetInterrupted()
		out.Type = entity.StreamTypeInterrupted
		out.Interrupted = &entity.InterruptedEvent{
			CheckpointID:     i.CheckpointId,
			PendingApprovals: fromPendingApprovals(i.PendingApprovals),
		}
	case ev.GetError() != nil:
		e := ev.GetError()
		out.Type = entity.StreamTypeError
		out.Error = &entity.ErrorEvent{Code: e.Code, Message: e.Message, Retryable: e.Retryable}
	case ev.GetDone() != nil:
		d := ev.GetDone()
		out.Type = entity.StreamTypeDone
		out.Done = &entity.DoneEvent{
			Content:          d.Content,
			FinishReason:     d.FinishReason,
			FinishedAt:       tsTime(d.FinishedAt),
			PromptTokens:     d.PromptTokens,
			CompletionTokens: d.CompletionTokens,
			TotalTokens:      d.TotalTokens,
			Metadata:         fromMetadata(d.Metadata),
			Memories:         fromMemories(d.Memories),
			CheckpointID:     d.CheckpointId,
		}
	}
	return out
}

func researchProgressFromMap(data map[string]any, traceID string, emittedAt time.Time) *controlentity.Progress {
	if data == nil {
		data = make(map[string]any)
	}
	state := map[string]any{
		"round": intValue(data["round"]), "queries": intValue(data["queries"]),
		"sources": intValue(data["sources"]), "confidence": floatValue(data["confidence"]),
		"query_texts":    queryTextsValue(data["query_texts"]),
		"valuable_pages": valuablePagesValue(data["valuable_pages"]),
		"completed":      boolValue(data["completed"]),
	}
	return &controlentity.Progress{
		Protocol: stringValue(data["protocol"]), Type: controlentity.TypeProgress,
		TaskID:     defaultString(stringValue(data["task_id"]), traceID),
		ActionID:   defaultString(stringValue(data["action_id"]), "research-"+traceID),
		Capability: defaultString(stringValue(data["capability"]), "research.execute"),
		Stage:      stringValue(data["stage"]), Message: stringValue(data["message"]),
		Progress: intValue(data["progress"]), State: state, SentAt: emittedAt,
	}
}

func queryTextsValue(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, minInt(len(raw), 10))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		text := truncateText(strings.TrimSpace(stringValue(item)), 240)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		result = append(result, text)
		if len(result) >= 10 {
			break
		}
	}
	return result
}

func valuablePagesValue(value any) []any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, minInt(len(raw), 8))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		page, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rawURL := strings.TrimSpace(stringValue(page["url"]))
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		title := truncateText(strings.TrimSpace(stringValue(page["title"])), 240)
		if title == "" {
			title = parsed.Hostname()
		}
		result = append(result, map[string]any{
			"id": stringValue(page["id"]), "rank": intValue(page["rank"]),
			"title": title, "url": rawURL, "domain": parsed.Hostname(),
			"provider":      truncateText(stringValue(page["provider"]), 80),
			"kind":          truncateText(stringValue(page["kind"]), 40),
			"snippet":       truncateText(stringValue(page["snippet"]), 280),
			"value_signals": boundedStrings(page["value_signals"], 6, 40),
			"authority":     floatValue(page["authority"]), "relevance": floatValue(page["relevance"]),
			"freshness": floatValue(page["freshness"]), "evidence_score": floatValue(page["evidence_score"]),
			"fetched": boolValue(page["fetched"]), "published_at": truncateText(stringValue(page["published_at"]), 40),
		})
		if len(result) >= 8 {
			break
		}
	}
	return result
}

func boundedStrings(value any, limit, maxRunes int) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, minInt(len(raw), limit))
	for _, item := range raw {
		text := truncateText(strings.TrimSpace(stringValue(item)), maxRunes)
		if text != "" {
			result = append(result, text)
		}
		if len(result) >= limit {
			break
		}
	}
	return result
}

func truncateText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func stringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	default:
		return 0
	}
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func tsTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
