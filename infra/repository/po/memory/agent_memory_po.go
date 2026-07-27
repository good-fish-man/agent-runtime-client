package memory

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// AgentMemory is a user-owned long-term memory extracted from conversations.
type AgentMemory struct {
	Ulid        string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	UserID      string `gorm:"column:user_id;type:varchar(128);not null;index:idx_memory_owner" json:"user_id"`
	AgentID     string `gorm:"column:agent_id;type:varchar(128);index:idx_memory_owner" json:"agent_id"`
	SessionID   string `gorm:"column:session_id;type:varchar(128);index" json:"session_id"`
	Name        string `gorm:"column:name;type:varchar(200)" json:"name"`
	Description string `gorm:"column:description;type:text" json:"description"`
	MemoryType  string `gorm:"column:memory_type;type:varchar(32);index" json:"memory_type"`
	Content     string `gorm:"column:content;type:text;not null" json:"content"`
	Importance  int32  `gorm:"column:importance;type:int;default:1" json:"importance"`
	Enabled     bool   `gorm:"column:enabled;type:boolean;default:true" json:"enabled"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
	UpdatedAt   int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint" json:"updated_at"`
	DeletedAt   int64  `gorm:"column:deleted_at;type:bigint;default:0;index" json:"deleted_at"`
}

func (po *AgentMemory) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (*AgentMemory) TableName() string { return "agent_memory" }
