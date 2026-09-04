package runtime

import protocol "github.com/good-fish-man/athena-protocol/protocol/v5"

// ---- Inputs ----

// RunInput is a domain request to execute a single run.
type RunInput struct {
	Prompt         string                 `json:"prompt"`
	Models         map[string]ModelConfig `json:"models,omitempty"`
	Messages       []ChatMessage          `json:"messages,omitempty"`
	Context        map[string]any         `json:"context,omitempty"`
	KnowledgeBases []KnowledgeBaseConfig  `json:"knowledge_bases,omitempty"`
	Skills         []Skill                `json:"skills,omitempty"`
	MCPs           []MCPConfig            `json:"mcps,omitempty"`
	CLIs           []CLIConfig            `json:"clis,omitempty"`
	A2A            []A2AAgentConfig       `json:"a2a,omitempty"`
	Capabilities   []CapabilityConfig     `json:"capabilities,omitempty"`
	InternalAgents []InternalAgentConfig  `json:"internal_agents,omitempty"`
	SubAgents      []SubAgentConfig       `json:"sub_agents,omitempty"`
	Options        *RunOptions            `json:"options,omitempty"`
	Sandbox        *SandboxConfig         `json:"sandbox,omitempty"`
	Files          []FileConfig           `json:"files,omitempty"`
	VisualInputs   []VisualInput          `json:"visual_inputs,omitempty"`
	RequestID      string                 `json:"request_id"`
	TraceID        string                 `json:"trace_id"`
}

// AgentInput is a domain request to run the built-in agent loop.
type AgentInput struct {
	Task         string                 `json:"task"`
	Context      map[string]any         `json:"context,omitempty"`
	Models       map[string]ModelConfig `json:"models,omitempty"`
	Capabilities []CapabilityConfig     `json:"capabilities,omitempty"`
	VisualInputs []VisualInput          `json:"visual_inputs,omitempty"`
	Stream       bool                   `json:"stream"`
	RequestID    string                 `json:"request_id"`
	TraceID      string                 `json:"trace_id"`
}

// VisualInput is trusted, bounded evidence decoded from an Athena device
// observation. It is sent as a native multimodal model part, never prompt text.
type VisualInput struct {
	ID       string `json:"id"`
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data"`
	SHA256   string `json:"sha256"`
	Purpose  string `json:"purpose,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type CapabilityDefinition struct {
	ID              string                    `json:"id"`
	Description     string                    `json:"description"`
	Input           map[string]string         `json:"input,omitempty"`
	Output          string                    `json:"output,omitempty"`
	ReadOnly        bool                      `json:"read_only"`
	Risk            string                    `json:"risk"`
	Status          string                    `json:"status"`
	Provider        string                    `json:"provider,omitempty"`
	Reason          string                    `json:"reason,omitempty"`
	Preconditions   []protocol.WorldCondition `json:"preconditions"`
	ExpectedEffects []protocol.WorldEffect    `json:"expected_effects"`
	Postconditions  []protocol.WorldCondition `json:"postconditions"`
}

type MediaGenerationInput struct {
	Model           ModelConfig `json:"model"`
	MediaType       string      `json:"media_type"`
	Operation       string      `json:"operation"`
	Prompt          string      `json:"prompt"`
	NegativePrompt  string      `json:"negative_prompt,omitempty"`
	SourceURL       string      `json:"source_url,omitempty"`
	Size            string      `json:"size,omitempty"`
	Quality         string      `json:"quality,omitempty"`
	DurationSeconds int         `json:"duration_seconds,omitempty"`
	RequestID       string      `json:"request_id"`
	TraceID         string      `json:"trace_id"`
}

// ResumeApproval is a single approval decision for a pending interrupt.
type ResumeApproval struct {
	InterruptID      string  `json:"interrupt_id"`
	Approved         bool    `json:"approved"`
	DisapproveReason *string `json:"disapprove_reason,omitempty"`
}

// ResumeInput resumes a checkpointed run with approval decisions.
type ResumeInput struct {
	CheckpointID string           `json:"checkpoint_id"`
	Approvals    []ResumeApproval `json:"approvals,omitempty"`
	RequestID    string           `json:"request_id"`
	TraceID      string           `json:"trace_id"`
}

// StopInput stops a run identified by checkpoint or session.
type StopInput struct {
	CheckpointID string `json:"checkpoint_id"`
	SessionID    string `json:"session_id"`
	TraceID      string `json:"trace_id"`
}

// HealthInput probes runtime health.
type HealthInput struct {
	Service string `json:"service"`
	TraceID string `json:"trace_id"`
}

// ---- Shared result parts ----

// ToolCall is a completed tool invocation.
type ToolCall struct {
	Tool   string `json:"tool"`
	Input  any    `json:"input,omitempty"`
	Output any    `json:"output,omitempty"`
}

// A2AResult is the outcome of an agent-to-agent call.
type A2AResult struct {
	AgentName string `json:"agent_name"`
	Status    string `json:"status"`
	Result    any    `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ToolCallMetadata is per-tool-call telemetry.
type ToolCallMetadata struct {
	Tool      string `json:"tool"`
	Input     any    `json:"input,omitempty"`
	Output    any    `json:"output,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// ModelUsageMetadata is the request-scoped token aggregate for one concrete LLM.
type ModelUsageMetadata struct {
	ModelID          string `json:"model_id"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	PromptTokens     int32  `json:"prompt_tokens"`
	CompletionTokens int32  `json:"completion_tokens"`
	TotalTokens      int32  `json:"total_tokens"`
	RequestCount     int32  `json:"request_count"`
}

// ResponseMetadata is aggregate telemetry for a response.
type ResponseMetadata struct {
	Model            string               `json:"model"`
	LatencyMs        int64                `json:"latency_ms"`
	TokensUsed       int32                `json:"tokens_used"`
	PromptTokens     int32                `json:"prompt_tokens"`
	CompletionTokens int32                `json:"completion_tokens"`
	ToolCallsCount   int32                `json:"tool_calls_count"`
	A2ACallsCount    int32                `json:"a2a_calls_count"`
	SkillCallsCount  int32                `json:"skill_calls_count"`
	Iterations       int32                `json:"iterations"`
	ToolCallsDetail  []ToolCallMetadata   `json:"tool_calls_detail,omitempty"`
	ModelUsage       []ModelUsageMetadata `json:"model_usage,omitempty"`
	Error            string               `json:"error,omitempty"`
}

// PendingApproval describes a tool call awaiting human approval.
type PendingApproval struct {
	InterruptID   string `json:"interrupt_id"`
	ToolName      string `json:"tool_name"`
	ToolType      string `json:"tool_type"`
	ArgumentsJSON string `json:"arguments_json"`
	RiskLevel     string `json:"risk_level"`
	Description   string `json:"description"`
}

// MemoryEntry is an extracted long-term memory. Type is a MemoryType name.
type MemoryEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Importance  int32  `json:"importance"`
}

// ---- Outputs ----

// Completion is the result of a non-streaming Run.
type Completion struct {
	Content          string            `json:"content"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	A2AResults       []A2AResult       `json:"a2a_results,omitempty"`
	TokensUsed       int32             `json:"tokens_used"`
	FinishReason     string            `json:"finish_reason"`
	Metadata         *ResponseMetadata `json:"metadata,omitempty"`
	A2UIMessages     []map[string]any  `json:"a2ui_messages,omitempty"`
	PendingApprovals []PendingApproval `json:"pending_approvals,omitempty"`
	CheckpointID     string            `json:"checkpoint_id,omitempty"`
	Memories         []MemoryEntry     `json:"memories,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
}

// AgentResult is the result of a non-streaming RunAgent.
type AgentResult struct {
	Content      string            `json:"content"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	TokensUsed   int32             `json:"tokens_used"`
	FinishReason string            `json:"finish_reason"`
	Metadata     *ResponseMetadata `json:"metadata,omitempty"`
	Error        string            `json:"error,omitempty"`
	TraceID      string            `json:"trace_id,omitempty"`
}

type MediaGenerationResult struct {
	MediaURL      string `json:"mediaUrl"`
	MediaType     string `json:"mediaType"`
	MimeType      string `json:"mimeType"`
	ProviderJobID string `json:"providerJobId,omitempty"`
	TraceID       string `json:"traceId,omitempty"`
}

const (
	MediaJobStatusQueued    = "queued"
	MediaJobStatusRunning   = "running"
	MediaJobStatusCompleted = "completed"
	MediaJobStatusFailed    = "failed"
)

// MediaGenerationJob is a persisted, user-owned image or video generation.
type MediaGenerationJob struct {
	Ulid            string `json:"ulid"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	UserID          string `json:"-"`
	ModelID         string `json:"modelId"`
	ModelName       string `json:"modelName"`
	MediaType       string `json:"mediaType"`
	Prompt          string `json:"prompt"`
	NegativePrompt  string `json:"negativePrompt,omitempty"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	Size            string `json:"size,omitempty"`
	Quality         string `json:"quality,omitempty"`
	DurationSeconds int    `json:"durationSeconds,omitempty"`
	Status          string `json:"status"`
	Progress        int    `json:"progress"`
	MediaURL        string `json:"mediaUrl,omitempty"`
	MimeType        string `json:"mimeType,omitempty"`
	ProviderJobID   string `json:"providerJobId,omitempty"`
	ErrorMessage    string `json:"errorMessage,omitempty"`
	TraceID         string `json:"traceId,omitempty"`
	StartedAt       int64  `json:"startedAt,omitempty"`
	FinishedAt      int64  `json:"finishedAt,omitempty"`
}

// ResumeResult is the result of a Resume.
type ResumeResult struct {
	Success          bool              `json:"success"`
	Error            string            `json:"error,omitempty"`
	FinishReason     string            `json:"finish_reason"`
	Content          string            `json:"content"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	Metadata         *ResponseMetadata `json:"metadata,omitempty"`
	PendingApprovals []PendingApproval `json:"pending_approvals,omitempty"`
	CheckpointID     string            `json:"checkpoint_id,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
}

// StopResult is the result of a Stop.
type StopResult struct {
	Stopped bool   `json:"stopped"`
	Message string `json:"message"`
	TraceID string `json:"trace_id,omitempty"`
}

// HealthStatus is the runtime's health. Status is a ServingStatus name.
type HealthStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	TraceID string `json:"trace_id,omitempty"`
}
