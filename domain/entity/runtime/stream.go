package runtime

import (
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

// Stream event type discriminators (match SSE event names).
const (
	StreamTypeMeta        = "meta"
	StreamTypeDelta       = "delta"
	StreamTypeToolCall    = "tool_call"
	StreamTypeToolResult  = "tool_result"
	StreamTypeInterrupted = "interrupted"
	StreamTypeError       = "error"
	StreamTypeDone        = "done"
	StreamTypeAction      = "action"
	StreamTypeProgress    = "progress"
	StreamTypeObservation = "observation"
)

// MetaEvent announces stream start / heartbeat.
type MetaEvent struct {
	StartedAt      time.Time `json:"started_at"`
	StreamProtocol string    `json:"stream_protocol"`
	CheckpointID   string    `json:"checkpoint_id,omitempty"`
	HeartbeatAt    time.Time `json:"heartbeat_at,omitempty"`
}

// DeltaEvent carries an incremental content chunk.
type DeltaEvent struct {
	Text      string `json:"text"`
	Role      string `json:"role"`
	Reasoning bool   `json:"reasoning"`
}

// ToolCallEvent announces a tool invocation.
type ToolCallEvent struct {
	ID       string         `json:"id"`
	Tool     string         `json:"tool"`
	ToolType string         `json:"tool_type"`
	Input    map[string]any `json:"input,omitempty"`
}

// ToolResultEvent carries a tool result.
type ToolResultEvent struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Output    map[string]any `json:"output,omitempty"`
	Success   bool           `json:"success"`
	Error     string         `json:"error,omitempty"`
	LatencyMs int64          `json:"latency_ms"`
}

// InterruptedEvent signals the run paused awaiting approvals.
type InterruptedEvent struct {
	CheckpointID     string            `json:"checkpoint_id"`
	PendingApprovals []PendingApproval `json:"pending_approvals,omitempty"`
}

// ErrorEvent carries a stream-level error.
type ErrorEvent struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// DoneEvent is the terminal stream event with the final result.
type DoneEvent struct {
	Content          string            `json:"content"`
	FinishReason     string            `json:"finish_reason"`
	FinishedAt       time.Time         `json:"finished_at"`
	PromptTokens     int32             `json:"prompt_tokens"`
	CompletionTokens int32             `json:"completion_tokens"`
	TotalTokens      int32             `json:"total_tokens"`
	Metadata         *ResponseMetadata `json:"metadata,omitempty"`
	Memories         []MemoryEntry     `json:"memories,omitempty"`
	CheckpointID     string            `json:"checkpoint_id,omitempty"`
}

// StreamEvent is a single event from a streaming run. Exactly one payload field
// is set, indicated by Type.
type StreamEvent struct {
	Seq         int64                      `json:"seq"`
	EmittedAt   time.Time                  `json:"emitted_at"`
	TraceID     string                     `json:"trace_id,omitempty"`
	Type        string                     `json:"type"`
	Meta        *MetaEvent                 `json:"meta,omitempty"`
	Delta       *DeltaEvent                `json:"delta,omitempty"`
	ToolCall    *ToolCallEvent             `json:"tool_call,omitempty"`
	ToolResult  *ToolResultEvent           `json:"tool_result,omitempty"`
	Interrupted *InterruptedEvent          `json:"interrupted,omitempty"`
	Error       *ErrorEvent                `json:"error,omitempty"`
	Done        *DoneEvent                 `json:"done,omitempty"`
	Action      *controlentity.Action      `json:"action,omitempty"`
	Progress    *controlentity.Progress    `json:"progress,omitempty"`
	Observation *controlentity.Observation `json:"observation,omitempty"`
}

// Payload returns the active payload for this event, or nil if none is set.
func (e *StreamEvent) Payload() any {
	switch e.Type {
	case StreamTypeMeta:
		return e.Meta
	case StreamTypeDelta:
		return e.Delta
	case StreamTypeToolCall:
		return e.ToolCall
	case StreamTypeToolResult:
		return e.ToolResult
	case StreamTypeInterrupted:
		return e.Interrupted
	case StreamTypeError:
		return e.Error
	case StreamTypeDone:
		return e.Done
	case StreamTypeAction:
		return e.Action
	case StreamTypeProgress:
		return e.Progress
	case StreamTypeObservation:
		return e.Observation
	default:
		return nil
	}
}
