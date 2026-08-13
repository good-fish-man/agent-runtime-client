package chat

import "time"

// ChatTokenStats stores token consumption statistics.
type ChatTokenStats struct {
	Ulid         string  `json:"ulid"`
	SessionId    string  `json:"session_id"`
	AgentId      string  `json:"agent_id"`
	UserId       string  `json:"user_id"`
	Date         string  `json:"date"`
	ModelID      string  `json:"model_id"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	RequestCount int     `json:"request_count"`
	CostAmount   float64 `json:"cost_amount"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
}

func (s *ChatTokenStats) AddTokens(input, output int) {
	s.InputTokens += input
	s.OutputTokens += output
	s.TotalTokens += input + output
	s.RequestCount++
	s.UpdatedAt = time.Now().UnixMilli()
}

func (s *ChatTokenStats) GetKey() string {
	return s.AgentId + "_" + s.UserId + "_" + s.Date + "_" + s.ModelID + "_" + s.Model
}
