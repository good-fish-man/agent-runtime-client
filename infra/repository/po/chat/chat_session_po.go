package chat

import (
	"time"

	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

type ChatSession struct {
	Ulid      string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
	UpdatedAt int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint;comment:修改时间;" json:"updated_at"`
	DeletedAt int64  `gorm:"column:deleted_at;type:bigint;comment:删除时间;" json:"deleted_at"`
	UserId    string `gorm:"column:user_id;type:varchar(128);comment:用户ID;" json:"user_id"`
	AgentId   string `gorm:"column:agent_id;type:varchar(128);comment:智能体ID;" json:"agent_id"`
	Title     string `gorm:"column:title;type:varchar(256);comment:会话标题;" json:"title"`
	Status    string `gorm:"column:status;type:varchar(32);default:active;comment:状态:active/archived;" json:"status"`
	Channel   string `gorm:"column:channel;type:varchar(32);comment:渠道;" json:"channel"`
}

func (po *ChatSession) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (po *ChatSession) TableName() string { return "chat_session" }
func (po *ChatSession) IsActive() bool    { return po.Status == "active" }
func (po *ChatSession) SoftDelete()       { po.DeletedAt = time.Now().UnixMilli() }
