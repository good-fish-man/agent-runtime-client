package chat

import (
	"time"

	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

type ChatApproval struct {
	Ulid       string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	MessageId  string `gorm:"column:message_id;type:varchar(128);comment:消息ID;index:idx_message;" json:"message_id"`
	AgentId    string `gorm:"column:agent_id;type:varchar(128);comment:智能体ID;" json:"agent_id"`
	ToolName   string `gorm:"column:tool_name;type:varchar(128);comment:工具名称;" json:"tool_name"`
	RiskLevel  string `gorm:"column:risk_level;type:varchar(32);comment:风险等级:low/medium/high;" json:"risk_level"`
	Parameters string `gorm:"column:parameters;type:json;comment:工具参数;" json:"parameters"`
	Status     string `gorm:"column:status;type:varchar(32);default:pending;comment:状态:pending/approved/rejected;" json:"status"`
	ApprovedBy string `gorm:"column:approved_by;type:varchar(128);comment:审批人;" json:"approved_by"`
	ApprovedAt int64  `gorm:"column:approved_at;type:bigint;comment:审批时间;" json:"approved_at"`
	Reason     string `gorm:"column:reason;type:text;comment:审批理由/拒绝原因;" json:"reason"`
	CreatedAt  int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
}

func (po *ChatApproval) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (po *ChatApproval) TableName() string { return "chat_approval" }
func (po *ChatApproval) IsPending() bool   { return po.Status == "pending" }
func (po *ChatApproval) Approve(approvedBy string) {
	po.Status = "approved"
	po.ApprovedBy = approvedBy
	po.ApprovedAt = time.Now().UnixMilli()
}
func (po *ChatApproval) Reject(approvedBy, reason string) {
	po.Status = "rejected"
	po.ApprovedBy = approvedBy
	po.ApprovedAt = time.Now().UnixMilli()
	po.Reason = reason
}
