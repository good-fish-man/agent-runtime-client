package runtime

import (
	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	infrapkg "github.com/good-fish-man/agent-runtime-client/infra/pkg"
)

// ---- top-level requests ----

func toRunRequest(in *entity.RunInput) *runtimev1.RunRequest {
	if in == nil {
		return &runtimev1.RunRequest{}
	}
	return &runtimev1.RunRequest{
		Prompt:         in.Prompt,
		Models:         toModels(in.Models),
		Messages:       toMessages(in.Messages),
		Context:        infrapkg.ToStruct(in.Context),
		KnowledgeBases: toKnowledgeBases(in.KnowledgeBases),
		Skills:         toSkills(in.Skills),
		Mcps:           toMCPs(in.MCPs),
		Clis:           toCLIs(in.CLIs),
		A2A:            toA2A(in.A2A),
		Tools:          toTools(in.Tools),
		InternalAgents: toInternalAgents(in.InternalAgents),
		SubAgents:      toSubAgents(in.SubAgents),
		Options:        toRunOptions(in.Options),
		Sandbox:        toSandbox(in.Sandbox),
		Files:          toFiles(in.Files),
		RequestId:      in.RequestID,
		TraceId:        in.TraceID,
	}
}

func toAgentRequest(in *entity.AgentInput) *runtimev1.AgentRequest {
	if in == nil {
		return &runtimev1.AgentRequest{}
	}
	return &runtimev1.AgentRequest{
		Task:      in.Task,
		Context:   infrapkg.ToStruct(in.Context),
		Models:    toModels(in.Models),
		Stream:    in.Stream,
		RequestId: in.RequestID,
		TraceId:   in.TraceID,
	}
}

func toResumeRequest(in *entity.ResumeInput) *runtimev1.ResumeRequest {
	if in == nil {
		return &runtimev1.ResumeRequest{}
	}
	approvals := make([]*runtimev1.ResumeApproval, 0, len(in.Approvals))
	for i := range in.Approvals {
		a := in.Approvals[i]
		approvals = append(approvals, &runtimev1.ResumeApproval{
			InterruptId:      a.InterruptID,
			Approved:         a.Approved,
			DisapproveReason: a.DisapproveReason,
		})
	}
	return &runtimev1.ResumeRequest{
		CheckpointId: in.CheckpointID,
		Approvals:    approvals,
		RequestId:    in.RequestID,
		TraceId:      in.TraceID,
	}
}

func toStopRequest(in *entity.StopInput) *runtimev1.StopRequest {
	if in == nil {
		return &runtimev1.StopRequest{}
	}
	return &runtimev1.StopRequest{
		CheckpointId: in.CheckpointID,
		SessionId:    in.SessionID,
		TraceId:      in.TraceID,
	}
}

func toHealthRequest(in *entity.HealthInput) *runtimev1.HealthCheckRequest {
	if in == nil {
		return &runtimev1.HealthCheckRequest{}
	}
	return &runtimev1.HealthCheckRequest{
		Service: in.Service,
		TraceId: in.TraceID,
	}
}

// ---- collections ----

func toModels(models map[string]entity.ModelConfig) map[string]*runtimev1.ModelConfig {
	if len(models) == 0 {
		return nil
	}
	out := make(map[string]*runtimev1.ModelConfig, len(models))
	for k, v := range models {
		mc := v
		out[k] = toModelConfig(&mc)
	}
	return out
}

func toModelConfig(m *entity.ModelConfig) *runtimev1.ModelConfig {
	if m == nil {
		return nil
	}
	return &runtimev1.ModelConfig{
		Provider:    m.Provider,
		Name:        m.Name,
		ApiKey:      m.APIKey,
		ApiBase:     m.APIBase,
		Temperature: m.Temperature,
		MaxTokens:   m.MaxTokens,
		TopP:        m.TopP,
		ExtraFields: infrapkg.ToStruct(m.ExtraFields),
	}
}

func toMessages(msgs []entity.ChatMessage) []*runtimev1.ChatMessage {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*runtimev1.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, &runtimev1.ChatMessage{Role: m.Role, Content: m.Content})
	}
	return out
}

func toRunOptions(o *entity.RunOptions) *runtimev1.RunOptions {
	if o == nil {
		return nil
	}
	return &runtimev1.RunOptions{
		Temperature:    o.Temperature,
		MaxTokens:      o.MaxTokens,
		Stream:         o.Stream,
		TopP:           o.TopP,
		Stop:           o.Stop,
		TimeoutMs:      o.TimeoutMs,
		MaxIterations:  o.MaxIterations,
		MaxToolCalls:   o.MaxToolCalls,
		MaxA2ACalls:    o.MaxA2ACalls,
		MaxTotalTokens: o.MaxTotalTokens,
		Retry:          toRetry(o.Retry),
		ResponseSchema: toResponseSchema(o.ResponseSchema),
		Routing:        toRouting(o.Routing),
		ApprovalPolicy: toApprovalPolicy(o.ApprovalPolicy),
		CheckpointId:   o.CheckpointID,
	}
}

func toRetry(r *entity.RetryConfig) *runtimev1.RetryConfig {
	if r == nil {
		return nil
	}
	return &runtimev1.RetryConfig{
		MaxAttempts:              r.MaxAttempts,
		InitialDelayMs:           r.InitialDelayMs,
		MaxDelayMs:               r.MaxDelayMs,
		BackoffMultiplier:        r.BackoffMultiplier,
		RetryableErrors:          r.RetryableErrors,
		CircuitBreakerThreshold:  r.CircuitBreakerThreshold,
		CircuitBreakerDurationMs: r.CircuitBreakerDurationMs,
	}
}

func toRouting(r *entity.RoutingConfig) *runtimev1.RoutingConfig {
	if r == nil {
		return nil
	}
	return &runtimev1.RoutingConfig{
		DefaultModel:    infrapkg.ModelRoleToProto(r.DefaultModel),
		RewritePrompt:   r.RewritePrompt,
		SummarizePrompt: r.SummarizePrompt,
	}
}

func toResponseSchema(r *entity.ResponseSchemaConfig) *runtimev1.ResponseSchemaConfig {
	if r == nil {
		return nil
	}
	return &runtimev1.ResponseSchemaConfig{
		Type:     r.Type,
		Version:  r.Version,
		Strict:   r.Strict,
		Schema:   infrapkg.ToStruct(r.Schema),
		Fallback: r.Fallback,
	}
}

func toApprovalPolicy(a *entity.ApprovalPolicy) *runtimev1.ApprovalPolicy {
	if a == nil {
		return nil
	}
	return &runtimev1.ApprovalPolicy{
		Enabled:       a.Enabled,
		RiskThreshold: infrapkg.RiskLevelToProto(a.RiskThreshold),
		AutoApprove:   a.AutoApprove,
	}
}

func toSkills(skills []entity.Skill) []*runtimev1.Skill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]*runtimev1.Skill, 0, len(skills))
	for i := range skills {
		out = append(out, toSkill(&skills[i]))
	}
	return out
}

func toSkill(s *entity.Skill) *runtimev1.Skill {
	if s == nil {
		return nil
	}
	return &runtimev1.Skill{
		Id:             s.ID,
		Name:           s.Name,
		Description:    s.Description,
		Instruction:    s.Instruction,
		Scope:          s.Scope,
		Trigger:        s.Trigger,
		EntryScript:    s.EntryScript,
		FilePath:       s.FilePath,
		Inputs:         s.Inputs,
		Outputs:        s.Outputs,
		RiskLevel:      infrapkg.RiskLevelToProto(s.RiskLevel),
		OutputPatterns: s.OutputPatterns,
	}
}

func toMCPs(mcps []entity.MCPConfig) []*runtimev1.MCPConfig {
	if len(mcps) == 0 {
		return nil
	}
	out := make([]*runtimev1.MCPConfig, 0, len(mcps))
	for _, m := range mcps {
		out = append(out, &runtimev1.MCPConfig{
			Name:      m.Name,
			Transport: m.Transport,
			Command:   m.Command,
			Args:      m.Args,
			Env:       m.Env,
			Endpoint:  m.Endpoint,
			Headers:   m.Headers,
			RiskLevel: infrapkg.RiskLevelToProto(m.RiskLevel),
		})
	}
	return out
}

func toCLIs(clis []entity.CLIConfig) []*runtimev1.CLIConfig {
	if len(clis) == 0 {
		return nil
	}
	out := make([]*runtimev1.CLIConfig, 0, len(clis))
	for _, c := range clis {
		out = append(out, &runtimev1.CLIConfig{
			Name:      c.Name,
			Command:   c.Command,
			ConfigDir: c.ConfigDir,
			SkillsDir: c.SkillsDir,
			RiskLevel: infrapkg.RiskLevelToProto(c.RiskLevel),
			AuthType:  c.AuthType,
		})
	}
	return out
}

func toTools(tools []entity.ToolConfig) []*runtimev1.ToolConfig {
	if len(tools) == 0 {
		return nil
	}
	out := make([]*runtimev1.ToolConfig, 0, len(tools))
	for i := range tools {
		out = append(out, toTool(&tools[i]))
	}
	return out
}

func toTool(t *entity.ToolConfig) *runtimev1.ToolConfig {
	if t == nil {
		return nil
	}
	return &runtimev1.ToolConfig{
		Type:        t.Type,
		Name:        t.Name,
		Description: t.Description,
		Endpoint:    t.Endpoint,
		Method:      t.Method,
		Headers:     t.Headers,
		RiskLevel:   infrapkg.RiskLevelToProto(t.RiskLevel),
	}
}

func toA2A(list []entity.A2AAgentConfig) []*runtimev1.A2AAgentConfig {
	if len(list) == 0 {
		return nil
	}
	out := make([]*runtimev1.A2AAgentConfig, 0, len(list))
	for _, a := range list {
		out = append(out, &runtimev1.A2AAgentConfig{
			Name:      a.Name,
			Endpoint:  a.Endpoint,
			Headers:   a.Headers,
			RiskLevel: infrapkg.RiskLevelToProto(a.RiskLevel),
		})
	}
	return out
}

func toInternalAgents(list []entity.InternalAgentConfig) []*runtimev1.InternalAgentConfig {
	if len(list) == 0 {
		return nil
	}
	out := make([]*runtimev1.InternalAgentConfig, 0, len(list))
	for i := range list {
		a := list[i]
		out = append(out, &runtimev1.InternalAgentConfig{
			Id:     a.ID,
			Name:   a.Name,
			Prompt: a.Prompt,
			Model:  toModelConfig(a.Model),
		})
	}
	return out
}

func toSubAgents(list []entity.SubAgentConfig) []*runtimev1.SubAgentConfig {
	if len(list) == 0 {
		return nil
	}
	out := make([]*runtimev1.SubAgentConfig, 0, len(list))
	for i := range list {
		a := list[i]
		out = append(out, &runtimev1.SubAgentConfig{
			Id:            a.ID,
			Name:          a.Name,
			Description:   a.Description,
			Prompt:        a.Prompt,
			Model:         toModelConfig(a.Model),
			Tools:         toTools(a.Tools),
			Skills:        toSkills(a.Skills),
			MaxIterations: a.MaxIterations,
			TimeoutMs:     a.TimeoutMs,
			Extra:         infrapkg.ToStruct(a.Extra),
		})
	}
	return out
}

func toKnowledgeBases(list []entity.KnowledgeBaseConfig) []*runtimev1.KnowledgeBaseConfig {
	if len(list) == 0 {
		return nil
	}
	out := make([]*runtimev1.KnowledgeBaseConfig, 0, len(list))
	for _, k := range list {
		out = append(out, &runtimev1.KnowledgeBaseConfig{
			Id:           k.ID,
			Name:         k.Name,
			RetrievalUrl: k.RetrievalURL,
			Token:        k.Token,
			TopK:         k.TopK,
		})
	}
	return out
}

func toFiles(list []entity.FileConfig) []*runtimev1.FileConfig {
	if len(list) == 0 {
		return nil
	}
	out := make([]*runtimev1.FileConfig, 0, len(list))
	for _, f := range list {
		out = append(out, &runtimev1.FileConfig{
			Name:        f.Name,
			VirtualPath: f.VirtualPath,
			Size:        f.Size,
			Type:        f.Type,
		})
	}
	return out
}

func toSandbox(s *entity.SandboxConfig) *runtimev1.SandboxConfig {
	if s == nil {
		return nil
	}
	var limits *runtimev1.SandboxLimits
	if s.Limits != nil {
		limits = &runtimev1.SandboxLimits{Cpu: s.Limits.CPU, Memory: s.Limits.Memory}
	}
	var volumes []*runtimev1.VolumeMount
	if len(s.Volumes) > 0 {
		volumes = make([]*runtimev1.VolumeMount, 0, len(s.Volumes))
		for _, v := range s.Volumes {
			volumes = append(volumes, &runtimev1.VolumeMount{
				HostPath:      v.HostPath,
				ContainerPath: v.ContainerPath,
				ReadOnly:      v.ReadOnly,
			})
		}
	}
	return &runtimev1.SandboxConfig{
		Enabled:   s.Enabled,
		Mode:      s.Mode,
		Image:     s.Image,
		Workdir:   s.Workdir,
		Network:   s.Network,
		TimeoutMs: s.TimeoutMs,
		Env:       s.Env,
		Limits:    limits,
		Volumes:   volumes,
	}
}
