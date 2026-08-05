// Package runtime (domain/entity) holds the transport-agnostic typed model for a
// "runtime invocation" bounded context. These value-objects mirror the semantics
// of the agent-runtime proto while carrying json tags so they can be reused by the
// application DTO envelopes. Nothing here imports transport (gRPC/HTTP) packages.
package runtime

// ChatMessage is a single conversation turn.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ModelConfig describes an LLM endpoint plus sampling defaults.
type ModelConfig struct {
	Provider    string         `json:"provider"`
	Name        string         `json:"name"`
	APIKey      string         `json:"api_key"`
	APIBase     string         `json:"api_base"`
	Temperature float64        `json:"temperature"`
	MaxTokens   int32          `json:"max_tokens"`
	TopP        float64        `json:"top_p"`
	ExtraFields map[string]any `json:"extra_fields,omitempty"`
}

// RetryConfig controls retry/circuit-breaker behavior.
type RetryConfig struct {
	MaxAttempts              int32    `json:"max_attempts"`
	InitialDelayMs           int32    `json:"initial_delay_ms"`
	MaxDelayMs               int32    `json:"max_delay_ms"`
	BackoffMultiplier        float64  `json:"backoff_multiplier"`
	RetryableErrors          []string `json:"retryable_errors,omitempty"`
	CircuitBreakerThreshold  int32    `json:"circuit_breaker_threshold"`
	CircuitBreakerDurationMs int32    `json:"circuit_breaker_duration_ms"`
}

// RoutingConfig selects models/prompts for auxiliary roles. DefaultModel is a
// ModelRole name ("default"|"rewrite"|"skill"|"summarize").
type RoutingConfig struct {
	DefaultModel    string `json:"default_model"`
	RewritePrompt   string `json:"rewrite_prompt"`
	SummarizePrompt string `json:"summarize_prompt"`
}

// ResponseSchemaConfig requests structured output.
type ResponseSchemaConfig struct {
	Type     string         `json:"type"`
	Version  string         `json:"version"`
	Strict   bool           `json:"strict"`
	Schema   map[string]any `json:"schema,omitempty"`
	Fallback string         `json:"fallback"`
}

// ApprovalPolicy gates risky tool calls. RiskThreshold is a RiskLevel name.
type ApprovalPolicy struct {
	Enabled       bool     `json:"enabled"`
	RiskThreshold string   `json:"risk_threshold"`
	AutoApprove   []string `json:"auto_approve,omitempty"`
}

// RunOptions holds per-run sampling and control knobs.
type RunOptions struct {
	Temperature    float64               `json:"temperature"`
	MaxTokens      int32                 `json:"max_tokens"`
	Stream         bool                  `json:"stream"`
	TopP           float64               `json:"top_p"`
	Stop           []string              `json:"stop,omitempty"`
	TimeoutMs      int32                 `json:"timeout_ms"`
	MaxIterations  int32                 `json:"max_iterations"`
	MaxToolCalls   int32                 `json:"max_tool_calls"`
	MaxA2ACalls    int32                 `json:"max_a2a_calls"`
	MaxTotalTokens int32                 `json:"max_total_tokens"`
	Retry          *RetryConfig          `json:"retry,omitempty"`
	ResponseSchema *ResponseSchemaConfig `json:"response_schema,omitempty"`
	Routing        *RoutingConfig        `json:"routing,omitempty"`
	ApprovalPolicy *ApprovalPolicy       `json:"approval_policy,omitempty"`
	CheckpointID   string                `json:"checkpoint_id"`
}

// Skill is a runnable capability. RiskLevel is a RiskLevel name.
type Skill struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Instruction    string   `json:"instruction"`
	Scope          string   `json:"scope"`
	Trigger        string   `json:"trigger"`
	EntryScript    string   `json:"entry_script"`
	FilePath       string   `json:"file_path"`
	Inputs         []string `json:"inputs,omitempty"`
	Outputs        []string `json:"outputs,omitempty"`
	RiskLevel      string   `json:"risk_level"`
	OutputPatterns []string `json:"output_patterns,omitempty"`
}

// MCPConfig configures an MCP server.
type MCPConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Endpoint  string            `json:"endpoint"`
	Headers   map[string]string `json:"headers,omitempty"`
	RiskLevel string            `json:"risk_level"`
}

// CLIConfig configures a CLI-backed tool.
type CLIConfig struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	ConfigDir string `json:"config_dir"`
	SkillsDir string `json:"skills_dir"`
	RiskLevel string `json:"risk_level"`
	AuthType  string `json:"auth_type"`
}

// CapabilityConfig selects a provider-independent Runtime ability.
type CapabilityConfig struct {
	ID     string         `json:"id"`
	Config map[string]any `json:"config,omitempty"`
}

// A2AAgentConfig configures an agent-to-agent endpoint.
type A2AAgentConfig struct {
	Name      string            `json:"name"`
	Endpoint  string            `json:"endpoint"`
	Headers   map[string]string `json:"headers,omitempty"`
	RiskLevel string            `json:"risk_level"`
}

// InternalAgentConfig configures a lightweight internal agent.
type InternalAgentConfig struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Prompt string       `json:"prompt"`
	Model  *ModelConfig `json:"model,omitempty"`
}

// SubAgentConfig configures a delegated sub-agent.
type SubAgentConfig struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Prompt        string             `json:"prompt"`
	Model         *ModelConfig       `json:"model,omitempty"`
	Capabilities  []CapabilityConfig `json:"capabilities,omitempty"`
	Skills        []Skill            `json:"skills,omitempty"`
	MaxIterations int32              `json:"max_iterations"`
	TimeoutMs     int32              `json:"timeout_ms"`
	Extra         map[string]any     `json:"extra,omitempty"`
}

// KnowledgeBaseConfig configures a retrievable knowledge base.
type KnowledgeBaseConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RetrievalURL string `json:"retrieval_url"`
	Token        string `json:"token"`
	TopK         int32  `json:"top_k"`
}

// FileConfig describes an uploaded file made available to the run.
type FileConfig struct {
	Name        string `json:"name"`
	VirtualPath string `json:"virtual_path"`
	Size        int64  `json:"size"`
	Type        string `json:"type"`
}

// SandboxLimits caps sandbox resources.
type SandboxLimits struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// VolumeMount binds a host path into the sandbox.
type VolumeMount struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only"`
}

// SandboxConfig configures sandboxed tool execution.
type SandboxConfig struct {
	Enabled   bool              `json:"enabled"`
	Mode      string            `json:"mode"`
	Image     string            `json:"image"`
	Workdir   string            `json:"workdir"`
	Network   string            `json:"network"`
	TimeoutMs int32             `json:"timeout_ms"`
	Env       map[string]string `json:"env,omitempty"`
	Limits    *SandboxLimits    `json:"limits,omitempty"`
	Volumes   []VolumeMount     `json:"volumes,omitempty"`
}
