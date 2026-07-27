package runtime

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"

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

func tsTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
