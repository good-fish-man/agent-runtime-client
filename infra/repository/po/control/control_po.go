package control

type Device struct {
	DeviceID     string `gorm:"column:device_id;primaryKey;type:varchar(128)"`
	UserID       string `gorm:"column:user_id;type:varchar(128);index"`
	Name         string `gorm:"column:name;type:varchar(255)"`
	Platform     string `gorm:"column:platform;type:varchar(64)"`
	Architecture string `gorm:"column:architecture;type:varchar(64)"`
	Capabilities string `gorm:"column:capabilities;type:text"`
	Online       bool   `gorm:"column:online;not null;default:false;index"`
	ConnectedAt  int64  `gorm:"column:connected_at;type:bigint;default:0"`
	LastSeenAt   int64  `gorm:"column:last_seen_at;type:bigint;default:0;index"`
	CreatedAt    int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
	UpdatedAt    int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint"`
}

func (*Device) TableName() string { return "agent_control_device" }

type Task struct {
	TaskID         string `gorm:"column:task_id;primaryKey;type:varchar(128)"`
	ConversationID string `gorm:"column:conversation_id;type:varchar(128);index"`
	UserID         string `gorm:"column:user_id;type:varchar(128);not null;index:idx_control_task_owner_status"`
	DeviceID       string `gorm:"column:device_id;type:varchar(128);index"`
	Status         string `gorm:"column:status;type:varchar(32);not null;index:idx_control_task_owner_status"`
	Sequence       int64  `gorm:"column:sequence;type:bigint;not null;default:0"`
	ActiveSessions string `gorm:"column:active_sessions;type:text"`
	Metadata       string `gorm:"column:metadata;type:text"`
	CreatedAt      int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt      int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*Task) TableName() string { return "agent_control_task" }

type Action struct {
	ActionID       string `gorm:"column:action_id;primaryKey;type:varchar(128)"`
	TaskID         string `gorm:"column:task_id;type:varchar(128);not null;index:idx_control_action_task_sequence,priority:1"`
	DeviceID       string `gorm:"column:device_id;type:varchar(128);not null;index"`
	UserID         string `gorm:"column:user_id;type:varchar(128);not null;index"`
	SessionID      string `gorm:"column:session_id;type:varchar(128)"`
	Sequence       int64  `gorm:"column:sequence;type:bigint;not null;index:idx_control_action_task_sequence,priority:2"`
	IdempotencyKey string `gorm:"column:idempotency_key;type:varchar(255);not null;uniqueIndex"`
	Deadline       int64  `gorm:"column:deadline;type:bigint;not null"`
	Capability     string `gorm:"column:capability;type:varchar(160);not null;index"`
	Arguments      string `gorm:"column:arguments;type:text"`
	Risk           string `gorm:"column:risk;type:varchar(16);not null"`
	Decision       string `gorm:"column:decision;type:varchar(24);not null"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
}

func (*Action) TableName() string { return "agent_control_action" }

type Observation struct {
	ActionID       string `gorm:"column:action_id;primaryKey;type:varchar(128)"`
	TaskID         string `gorm:"column:task_id;type:varchar(128);not null;index:idx_control_observation_task_sequence,priority:1"`
	IdempotencyKey string `gorm:"column:idempotency_key;type:varchar(255);not null;uniqueIndex"`
	SessionID      string `gorm:"column:session_id;type:varchar(128)"`
	Sequence       int64  `gorm:"column:sequence;type:bigint;not null;index:idx_control_observation_task_sequence,priority:2"`
	Status         string `gorm:"column:status;type:varchar(32);not null;index"`
	ObservedAt     int64  `gorm:"column:observed_at;type:bigint;not null"`
	State          string `gorm:"column:state;type:text"`
	Error          string `gorm:"column:error;type:text"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
}

func (*Observation) TableName() string { return "agent_control_observation" }
