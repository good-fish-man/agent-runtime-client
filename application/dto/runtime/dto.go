// Package runtime (application/dto) defines the HTTP request envelopes. Nested
// config value-objects are reused from domain/entity to keep a single strongly
// typed definition; the top-level Req types own binding/validation and API shape.
package runtime

import entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"

// RunReq is the POST /v1/run(/stream) body.
type RunReq struct {
	Prompt         string                        `json:"prompt"`
	Models         map[string]entity.ModelConfig `json:"models"`
	Messages       []entity.ChatMessage          `json:"messages"`
	Context        map[string]any                `json:"context"`
	KnowledgeBases []entity.KnowledgeBaseConfig  `json:"knowledge_bases"`
	Skills         []entity.Skill                `json:"skills"`
	MCPs           []entity.MCPConfig            `json:"mcps"`
	CLIs           []entity.CLIConfig            `json:"clis"`
	A2A            []entity.A2AAgentConfig       `json:"a2a"`
	Tools          []entity.ToolConfig           `json:"tools"`
	InternalAgents []entity.InternalAgentConfig  `json:"internal_agents"`
	SubAgents      []entity.SubAgentConfig       `json:"sub_agents"`
	Options        *entity.RunOptions            `json:"options"`
	Sandbox        *entity.SandboxConfig         `json:"sandbox"`
	Files          []entity.FileConfig           `json:"files"`
	RequestID      string                        `json:"request_id"`
}

// AgentReq is the POST /v1/agent(/stream) body.
type AgentReq struct {
	Task      string                        `json:"task"`
	Context   map[string]any                `json:"context"`
	Models    map[string]entity.ModelConfig `json:"models"`
	Stream    bool                          `json:"stream"`
	RequestID string                        `json:"request_id"`
}

// MediaGenerationReq is the authenticated direct-media request. Model secrets
// are resolved server-side from ModelID and are never accepted from browsers.
type MediaGenerationReq struct {
	ModelID         string `json:"modelId" validate:"required"`
	MediaType       string `json:"mediaType" validate:"required,oneof=image video"`
	Operation       string `json:"operation"`
	Prompt          string `json:"prompt" validate:"required"`
	NegativePrompt  string `json:"negativePrompt"`
	SourceURL       string `json:"sourceUrl"`
	Size            string `json:"size"`
	Quality         string `json:"quality"`
	DurationSeconds int    `json:"durationSeconds"`
}

// ResumeReq is the POST /v1/resume body.
type ResumeReq struct {
	CheckpointID string                  `json:"checkpoint_id"`
	Approvals    []entity.ResumeApproval `json:"approvals"`
	RequestID    string                  `json:"request_id"`
}

// StopReq is the POST /v1/stop body.
type StopReq struct {
	CheckpointID string `json:"checkpoint_id"`
	SessionID    string `json:"session_id"`
}
