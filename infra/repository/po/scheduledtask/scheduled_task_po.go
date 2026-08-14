package scheduledtask

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

const (
	StatusActive = "active"
	StatusPaused = "paused"
)

type ScheduledTask struct {
	Ulid               string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	CreatedAt          int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
	UpdatedAt          int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint" json:"updated_at"`
	DeletedAt          int64  `gorm:"column:deleted_at;type:bigint;default:0;index" json:"-"`
	UserID             string `gorm:"column:user_id;type:varchar(128);not null;index:idx_scheduled_task_owner" json:"-"`
	AgentID            string `gorm:"column:agent_id;type:varchar(128);not null;index:idx_scheduled_task_due" json:"agent_id"`
	SessionID          string `gorm:"column:session_id;type:varchar(128)" json:"session_id"`
	Name               string `gorm:"column:name;type:varchar(160);not null" json:"name"`
	TaskType           string `gorm:"column:task_type;type:varchar(32);not null" json:"task_type"`
	CronExpr           string `gorm:"column:cron_expr;type:varchar(100);not null" json:"cron"`
	Timezone           string `gorm:"column:timezone;type:varchar(80);not null;default:Local" json:"timezone"`
	Prompt             string `gorm:"column:prompt;type:text;not null" json:"prompt"`
	CriteriaJSON       string `gorm:"column:criteria_json;type:text" json:"criteria_json"`
	ActionMode         string `gorm:"column:action_mode;type:varchar(40);not null;default:confirm_before_commit" json:"action_mode"`
	MisfirePolicy      string `gorm:"column:misfire_policy;type:varchar(20);not null;default:FIRE_ONCE" json:"misfire_policy"`
	RetryMax           int    `gorm:"column:retry_max;not null;default:3" json:"retry_max"`
	RetryBackoffMS     int64  `gorm:"column:retry_backoff_ms;not null;default:1000" json:"retry_backoff_ms"`
	MaxConcurrency     int    `gorm:"column:max_concurrency;not null;default:1" json:"max_concurrency"`
	RiskLevel          string `gorm:"column:risk_level;type:varchar(8);not null;default:R1" json:"risk_level"`
	ApprovalMode       string `gorm:"column:approval_mode;type:varchar(24);not null;default:NONE" json:"approval_mode"`
	PreauthorizationID string `gorm:"column:preauthorization_id;type:varchar(128)" json:"preauthorization_id,omitempty"`
	Notify             bool   `gorm:"column:notify;not null;default:true" json:"notify"`
	Status             string `gorm:"column:status;type:varchar(20);not null;default:active;index:idx_scheduled_task_due" json:"status"`
	LastSlot           string `gorm:"column:last_slot;type:varchar(20);index" json:"-"`
	LastRunAt          int64  `gorm:"column:last_run_at;type:bigint" json:"last_run_at"`
	LastStatus         string `gorm:"column:last_status;type:varchar(20)" json:"last_status"`
	LastResult         string `gorm:"column:last_result;type:text" json:"last_result"`
	LastError          string `gorm:"column:last_error;type:text" json:"last_error"`
	ExecutionCount     int64  `gorm:"column:execution_count;type:bigint;not null;default:0" json:"execution_count"`
}

func (p *ScheduledTask) BeforeCreate(tx *gorm.DB) error {
	if p.Ulid == "" {
		p.Ulid = ulid.New()
	}
	return nil
}

func (*ScheduledTask) TableName() string { return "scheduled_task" }
