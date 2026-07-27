package chat

// ChatMessage is a chat message.
type ChatMessage struct {
	Ulid         string `json:"ulid"`
	SessionId    string `json:"session_id"`
	CreatedAt    int64  `json:"created_at"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	LatencyMs    int    `json:"latency_ms"`
	Trace        string `json:"trace"`
	Status       string `json:"status"`
	ErrorMsg     string `json:"error_msg"`
	Metadata     string `json:"metadata"`
}

func (m *ChatMessage) IsPendingApproval() bool  { return m.Status == "pending_approval" }
func (m *ChatMessage) IsSuccess() bool          { return m.Status == "success" }
func (m *ChatMessage) IsFailed() bool           { return m.Status == "failed" }
func (m *ChatMessage) IsUserMessage() bool      { return m.Role == "user" }
func (m *ChatMessage) IsAssistantMessage() bool { return m.Role == "assistant" }
