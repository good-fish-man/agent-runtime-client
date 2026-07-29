package agent

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// SysAgent is the gorm persistence object mapped to table sys_agent.
type SysAgent struct {
	Ulid           string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
	UpdatedAt      int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint;comment:修改时间;" json:"updated_at"`
	DeletedAt      int64  `gorm:"column:deleted_at;type:bigint;comment:删除时间;" json:"deleted_at"`
	CreatedBy      string `gorm:"column:created_by;type:varchar(128);comment:创建者;" json:"created_by"`
	UpdatedBy      string `gorm:"column:updated_by;type:varchar(128);comment:修改者;" json:"updated_by"`
	Name           string `gorm:"column:name;type:varchar(100);comment:Agent名称;" json:"name"`
	Description    string `gorm:"column:description;type:text;comment:描述;" json:"description"`
	Icon           string `gorm:"column:icon;type:varchar(50);comment:图标名称;" json:"icon"`
	Model          string `gorm:"column:model;type:varchar(100);comment:默认模型;" json:"model"`
	EmbeddingModel string `gorm:"column:embedding_model;type:varchar(128);comment:Embedding模型;" json:"embedding_model"`
	ImageModel     string `gorm:"column:image_model;type:varchar(128);comment:图片生成模型;" json:"image_model"`
	VideoModel     string `gorm:"column:video_model;type:varchar(128);comment:视频生成模型;" json:"video_model"`
	Config         string `gorm:"column:config;type:text;comment:完整JSON配置;" json:"config"`
	ConfigJson     string `gorm:"column:config_json;type:text;comment:可运行JSON配置;" json:"config_json"`
	IsSystem       bool   `gorm:"column:is_system;type:boolean;default:false;comment:是否系统内置;" json:"is_system"`
	Enabled        bool   `gorm:"column:enabled;type:boolean;default:true;comment:是否启用;" json:"enabled"`
	Channels       string `gorm:"column:channels;type:varchar(500);comment:渠道;" json:"channels"`
	IsPeriodic     bool   `gorm:"column:is_periodic;type:boolean;default:false;comment:是否周期任务;" json:"is_periodic"`
	CronRule       string `gorm:"column:cron_rule;type:varchar(100);comment:Cron规则;" json:"cron_rule"`
}

// BeforeCreate assigns a ULID primary key when absent.
func (po *SysAgent) BeforeCreate(tx *gorm.DB) (err error) {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return
}

// TableName maps to the shared sys_agent table.
func (po *SysAgent) TableName() string { return "sys_agent" }

// SysAgentUserModel stores per-user model choices for a system agent.
type SysAgentUserModel struct {
	Ulid           string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
	UpdatedAt      int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint" json:"updated_at"`
	UserID         string `gorm:"column:user_id;type:varchar(128);not null;uniqueIndex:uk_agent_user_model" json:"user_id"`
	AgentID        string `gorm:"column:agent_id;type:varchar(128);not null;uniqueIndex:uk_agent_user_model" json:"agent_id"`
	Model          string `gorm:"column:model;type:varchar(128);not null" json:"model"`
	EmbeddingModel string `gorm:"column:embedding_model;type:varchar(128)" json:"embedding_model"`
	ImageModel     string `gorm:"column:image_model;type:varchar(128)" json:"image_model"`
	VideoModel     string `gorm:"column:video_model;type:varchar(128)" json:"video_model"`
}

func (po *SysAgentUserModel) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (*SysAgentUserModel) TableName() string { return "sys_agent_user_model" }
