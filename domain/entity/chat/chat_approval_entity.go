package chat

import "time"

// ChatApproval is a chat tool-call approval.
type ChatApproval struct {
	Ulid       string `json:"ulid"`
	MessageId  string `json:"message_id"`
	AgentId    string `json:"agent_id"`
	ToolName   string `json:"tool_name"`
	RiskLevel  string `json:"risk_level"`
	Parameters string `json:"parameters"`
	Status     string `json:"status"`
	ApprovedBy string `json:"approved_by"`
	ApprovedAt int64  `json:"approved_at"`
	Reason     string `json:"reason"`
	CreatedAt  int64  `json:"created_at"`
}

func (a *ChatApproval) IsPending() bool  { return a.Status == "pending" }
func (a *ChatApproval) IsApproved() bool { return a.Status == "approved" }
func (a *ChatApproval) IsRejected() bool { return a.Status == "rejected" }

func (a *ChatApproval) Approve(approvedBy string) {
	a.Status = "approved"
	a.ApprovedBy = approvedBy
	a.ApprovedAt = time.Now().UnixMilli()
}

func (a *ChatApproval) Reject(approvedBy, reason string) {
	a.Status = "rejected"
	a.ApprovedBy = approvedBy
	a.ApprovedAt = time.Now().UnixMilli()
	a.Reason = reason
}
