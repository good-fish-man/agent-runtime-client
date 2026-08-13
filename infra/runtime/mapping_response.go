package runtime

import (
	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	infrapkg "github.com/good-fish-man/agent-runtime-client/infra/pkg"
)

func fromRunResponse(r *runtimev1.RunResponse) *entity.Completion {
	if r == nil {
		return &entity.Completion{}
	}
	return &entity.Completion{
		Content:          r.Content,
		ToolCalls:        fromToolCalls(r.ToolCalls),
		A2AResults:       fromA2AResults(r.A2AResults),
		TokensUsed:       r.TokensUsed,
		FinishReason:     r.FinishReason,
		Metadata:         fromMetadata(r.Metadata),
		A2UIMessages:     infrapkg.StructsToMaps(r.A2UiMessages),
		PendingApprovals: fromPendingApprovals(r.PendingApprovals),
		CheckpointID:     r.CheckpointId,
		Memories:         fromMemories(r.Memories),
		TraceID:          r.TraceId,
	}
}

func fromAgentResponse(r *runtimev1.AgentResponse) *entity.AgentResult {
	if r == nil {
		return &entity.AgentResult{}
	}
	return &entity.AgentResult{
		Content:      r.Content,
		ToolCalls:    fromToolCalls(r.ToolCalls),
		TokensUsed:   r.TokensUsed,
		FinishReason: r.FinishReason,
		Metadata:     fromMetadata(r.Metadata),
		Error:        r.Error,
		TraceID:      r.TraceId,
	}
}

func fromResumeResponse(r *runtimev1.ResumeResponse) *entity.ResumeResult {
	if r == nil {
		return &entity.ResumeResult{}
	}
	return &entity.ResumeResult{
		Success:          r.Success,
		Error:            r.Error,
		FinishReason:     r.FinishReason,
		Content:          r.Content,
		ToolCalls:        fromToolCalls(r.ToolCalls),
		Metadata:         fromMetadata(r.Metadata),
		PendingApprovals: fromPendingApprovals(r.PendingApprovals),
		CheckpointID:     r.CheckpointId,
		TraceID:          r.TraceId,
	}
}

func fromStopResponse(r *runtimev1.StopResponse) *entity.StopResult {
	if r == nil {
		return &entity.StopResult{}
	}
	return &entity.StopResult{
		Stopped: r.Stopped,
		Message: r.Message,
		TraceID: r.TraceId,
	}
}

func fromHealthResponse(r *runtimev1.HealthCheckResponse) *entity.HealthStatus {
	if r == nil {
		return &entity.HealthStatus{Status: "UNKNOWN"}
	}
	return &entity.HealthStatus{
		Status:  infrapkg.ServingStatusFromProto(r.Status),
		Version: r.Version,
		TraceID: r.TraceId,
	}
}

// ---- shared parts ----

func fromToolCalls(list []*runtimev1.ToolCall) []entity.ToolCall {
	if len(list) == 0 {
		return nil
	}
	out := make([]entity.ToolCall, 0, len(list))
	for _, t := range list {
		if t == nil {
			continue
		}
		out = append(out, entity.ToolCall{
			Tool:   t.Tool,
			Input:  infrapkg.FromValue(t.Input),
			Output: infrapkg.FromValue(t.Output),
		})
	}
	return out
}

func fromA2AResults(list []*runtimev1.A2AResult) []entity.A2AResult {
	if len(list) == 0 {
		return nil
	}
	out := make([]entity.A2AResult, 0, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		out = append(out, entity.A2AResult{
			AgentName: a.AgentName,
			Status:    a.Status,
			Result:    infrapkg.FromValue(a.Result),
			Error:     a.Error,
		})
	}
	return out
}

func fromMetadata(m *runtimev1.ResponseMetadata) *entity.ResponseMetadata {
	if m == nil {
		return nil
	}
	return &entity.ResponseMetadata{
		Model:            m.Model,
		LatencyMs:        m.LatencyMs,
		TokensUsed:       m.TokensUsed,
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
		ToolCallsCount:   m.ToolCallsCount,
		A2ACallsCount:    m.A2ACallsCount,
		SkillCallsCount:  m.SkillCallsCount,
		Iterations:       m.Iterations,
		ToolCallsDetail:  fromToolCallDetails(m.ToolCallsDetail),
		ModelUsage:       fromModelUsage(m),
		Error:            m.Error,
	}
}

func fromModelUsage(metadata *runtimev1.ResponseMetadata) []entity.ModelUsageMetadata {
	if metadata == nil {
		return nil
	}
	message := metadata.ProtoReflect()
	if usage := modelUsageFromReflection(message); len(usage) > 0 {
		return usage
	}
	// A client compiled against an older Runtime proto keeps field 12 as
	// unknown bytes. Decode it so Runtime and Client can be released in order.
	return modelUsageFromUnknown(message.GetUnknown())
}

func modelUsageFromReflection(message protoreflect.Message) []entity.ModelUsageMetadata {
	field := message.Descriptor().Fields().ByNumber(12)
	if field == nil || !field.IsList() || field.Kind() != protoreflect.MessageKind {
		return nil
	}
	list := message.Get(field).List()
	result := make([]entity.ModelUsageMetadata, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		result = append(result, modelUsageFromMessage(list.Get(i).Message()))
	}
	return result
}

func modelUsageFromMessage(message protoreflect.Message) entity.ModelUsageMetadata {
	return entity.ModelUsageMetadata{
		ModelID:          reflectedString(message, 1),
		Provider:         reflectedString(message, 2),
		Model:            reflectedString(message, 3),
		PromptTokens:     int32(reflectedInt(message, 4)),
		CompletionTokens: int32(reflectedInt(message, 5)),
		TotalTokens:      int32(reflectedInt(message, 6)),
		RequestCount:     int32(reflectedInt(message, 7)),
	}
}

func reflectedString(message protoreflect.Message, number protoreflect.FieldNumber) string {
	field := message.Descriptor().Fields().ByNumber(number)
	if field == nil {
		return ""
	}
	return message.Get(field).String()
}

func reflectedInt(message protoreflect.Message, number protoreflect.FieldNumber) int64 {
	field := message.Descriptor().Fields().ByNumber(number)
	if field == nil {
		return 0
	}
	return message.Get(field).Int()
}

func modelUsageFromUnknown(data []byte) []entity.ModelUsageMetadata {
	result := make([]entity.ModelUsageMetadata, 0)
	for len(data) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(data)
		if tagLength < 0 {
			return result
		}
		data = data[tagLength:]
		if number == 12 && wireType == protowire.BytesType {
			payload, length := protowire.ConsumeBytes(data)
			if length < 0 {
				return result
			}
			result = append(result, decodeModelUsage(payload))
			data = data[length:]
			continue
		}
		length := protowire.ConsumeFieldValue(number, wireType, data)
		if length < 0 {
			return result
		}
		data = data[length:]
	}
	return result
}

func decodeModelUsage(data []byte) entity.ModelUsageMetadata {
	var result entity.ModelUsageMetadata
	for len(data) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(data)
		if tagLength < 0 {
			return result
		}
		data = data[tagLength:]
		switch {
		case number >= 1 && number <= 3 && wireType == protowire.BytesType:
			value, length := protowire.ConsumeString(data)
			if length < 0 {
				return result
			}
			switch number {
			case 1:
				result.ModelID = value
			case 2:
				result.Provider = value
			case 3:
				result.Model = value
			}
			data = data[length:]
		case number >= 4 && number <= 7 && wireType == protowire.VarintType:
			value, length := protowire.ConsumeVarint(data)
			if length < 0 {
				return result
			}
			switch number {
			case 4:
				result.PromptTokens = int32(value)
			case 5:
				result.CompletionTokens = int32(value)
			case 6:
				result.TotalTokens = int32(value)
			case 7:
				result.RequestCount = int32(value)
			}
			data = data[length:]
		default:
			length := protowire.ConsumeFieldValue(number, wireType, data)
			if length < 0 {
				return result
			}
			data = data[length:]
		}
	}
	return result
}

func fromToolCallDetails(list []*runtimev1.ToolCallMetadata) []entity.ToolCallMetadata {
	if len(list) == 0 {
		return nil
	}
	out := make([]entity.ToolCallMetadata, 0, len(list))
	for _, t := range list {
		if t == nil {
			continue
		}
		out = append(out, entity.ToolCallMetadata{
			Tool:      t.Tool,
			Input:     infrapkg.FromValue(t.Input),
			Output:    infrapkg.FromValue(t.Output),
			LatencyMs: t.LatencyMs,
			Success:   t.Success,
			Error:     t.Error,
		})
	}
	return out
}

func fromPendingApprovals(list []*runtimev1.PendingApproval) []entity.PendingApproval {
	if len(list) == 0 {
		return nil
	}
	out := make([]entity.PendingApproval, 0, len(list))
	for _, p := range list {
		if p == nil {
			continue
		}
		out = append(out, entity.PendingApproval{
			InterruptID:   p.InterruptId,
			ToolName:      p.ToolName,
			ToolType:      p.ToolType,
			ArgumentsJSON: p.ArgumentsJson,
			RiskLevel:     infrapkg.RiskLevelFromProto(p.RiskLevel),
			Description:   p.Description,
		})
	}
	return out
}

func fromMemories(list []*runtimev1.MemoryEntry) []entity.MemoryEntry {
	if len(list) == 0 {
		return nil
	}
	out := make([]entity.MemoryEntry, 0, len(list))
	for _, m := range list {
		if m == nil {
			continue
		}
		out = append(out, entity.MemoryEntry{
			Name:        m.Name,
			Description: m.Description,
			Type:        infrapkg.MemoryTypeFromProto(m.Type),
			Content:     m.Content,
			Importance:  m.Importance,
		})
	}
	return out
}
