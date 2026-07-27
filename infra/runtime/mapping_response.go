package runtime

import (
	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"

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
		Error:            m.Error,
	}
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
