package control

// LegacyV3* are read-only migration projections. v0.2 never writes these tables.
type LegacyV3Device struct {
	DeviceID     string `gorm:"column:device_id;primaryKey"`
	UserID       string `gorm:"column:user_id"`
	Name         string `gorm:"column:name"`
	Platform     string `gorm:"column:platform"`
	Architecture string `gorm:"column:architecture"`
	Capabilities string `gorm:"column:capabilities"`
	Online       bool   `gorm:"column:online"`
	ConnectedAt  int64  `gorm:"column:connected_at"`
	LastSeenAt   int64  `gorm:"column:last_seen_at"`
	CreatedAt    int64  `gorm:"column:created_at"`
	UpdatedAt    int64  `gorm:"column:updated_at"`
}

func (*LegacyV3Device) TableName() string { return "agent_control_device" }

type LegacyV3Task struct {
	TaskID         string `gorm:"column:task_id;primaryKey"`
	ConversationID string `gorm:"column:conversation_id"`
	UserID         string `gorm:"column:user_id"`
	DeviceID       string `gorm:"column:device_id"`
	Status         string `gorm:"column:status"`
	Sequence       int64  `gorm:"column:sequence"`
	ActiveSessions string `gorm:"column:active_sessions"`
	Metadata       string `gorm:"column:metadata"`
	CreatedAt      int64  `gorm:"column:created_at"`
	UpdatedAt      int64  `gorm:"column:updated_at"`
}

func (*LegacyV3Task) TableName() string { return "agent_control_task" }

type LegacyV3Action struct {
	ActionID       string `gorm:"column:action_id;primaryKey"`
	TaskID         string `gorm:"column:task_id"`
	DeviceID       string `gorm:"column:device_id"`
	UserID         string `gorm:"column:user_id"`
	SessionID      string `gorm:"column:session_id"`
	Sequence       int64  `gorm:"column:sequence"`
	IdempotencyKey string `gorm:"column:idempotency_key"`
	Deadline       int64  `gorm:"column:deadline"`
	Capability     string `gorm:"column:capability"`
	Arguments      string `gorm:"column:arguments"`
	Risk           string `gorm:"column:risk"`
	Decision       string `gorm:"column:decision"`
	CreatedAt      int64  `gorm:"column:created_at"`
}

func (*LegacyV3Action) TableName() string { return "agent_control_action" }

type LegacyV3Observation struct {
	ActionID       string `gorm:"column:action_id;primaryKey"`
	TaskID         string `gorm:"column:task_id"`
	IdempotencyKey string `gorm:"column:idempotency_key"`
	SessionID      string `gorm:"column:session_id"`
	Sequence       int64  `gorm:"column:sequence"`
	Status         string `gorm:"column:status"`
	ObservedAt     int64  `gorm:"column:observed_at"`
	State          string `gorm:"column:state"`
	Error          string `gorm:"column:error"`
	CreatedAt      int64  `gorm:"column:created_at"`
}

func (*LegacyV3Observation) TableName() string { return "agent_control_observation" }
