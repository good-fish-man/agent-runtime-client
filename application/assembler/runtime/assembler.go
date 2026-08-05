// Package runtime (application/assembler) converts request DTOs into domain
// inputs. Conversions are pass-through since nested configs share entity types.
package runtime

import (
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

// ToRunInput maps a RunReq to a domain RunInput.
func ToRunInput(r *dto.RunReq) *entity.RunInput {
	if r == nil {
		return &entity.RunInput{}
	}
	return &entity.RunInput{
		Prompt:         r.Prompt,
		Models:         r.Models,
		Messages:       r.Messages,
		Context:        r.Context,
		KnowledgeBases: r.KnowledgeBases,
		Skills:         r.Skills,
		MCPs:           r.MCPs,
		CLIs:           r.CLIs,
		A2A:            r.A2A,
		Capabilities:   r.Capabilities,
		InternalAgents: r.InternalAgents,
		SubAgents:      r.SubAgents,
		Options:        r.Options,
		Sandbox:        r.Sandbox,
		Files:          r.Files,
		RequestID:      r.RequestID,
	}
}

// ToAgentInput maps an AgentReq to a domain AgentInput.
func ToAgentInput(r *dto.AgentReq) *entity.AgentInput {
	if r == nil {
		return &entity.AgentInput{}
	}
	return &entity.AgentInput{
		Task:         r.Task,
		Context:      r.Context,
		Models:       r.Models,
		Capabilities: r.Capabilities,
		Stream:       r.Stream,
		RequestID:    r.RequestID,
	}
}

// ToResumeInput maps a ResumeReq to a domain ResumeInput.
func ToResumeInput(r *dto.ResumeReq) *entity.ResumeInput {
	if r == nil {
		return &entity.ResumeInput{}
	}
	return &entity.ResumeInput{
		CheckpointID: r.CheckpointID,
		Approvals:    r.Approvals,
		RequestID:    r.RequestID,
	}
}

// ToStopInput maps a StopReq to a domain StopInput.
func ToStopInput(r *dto.StopReq) *entity.StopInput {
	if r == nil {
		return &entity.StopInput{}
	}
	return &entity.StopInput{
		CheckpointID: r.CheckpointID,
		SessionID:    r.SessionID,
	}
}
