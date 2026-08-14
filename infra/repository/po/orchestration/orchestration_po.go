package orchestration

type Goal struct {
	GoalID    string `gorm:"column:goal_id;primaryKey;size:64"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index:idx_goal_owner_status,priority:1"`
	AgentID   string `gorm:"column:agent_id;size:64;not null;index"`
	Status    string `gorm:"column:status;size:24;not null;index:idx_goal_owner_status,priority:2"`
	Revision  int64  `gorm:"column:revision;not null"`
	Deadline  int64  `gorm:"column:deadline;index"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt int64  `gorm:"column:updated_at;not null;index"`
}

func (Goal) TableName() string { return "os_goal" }

type GoalTask struct {
	TaskID     string `gorm:"column:task_id;primaryKey;size:64"`
	GoalID     string `gorm:"column:goal_id;size:64;not null;index:idx_goal_task_order,priority:1"`
	OwnerID    string `gorm:"column:owner_id;size:64;not null;index"`
	Specialist string `gorm:"column:specialist;size:24;not null;index"`
	Status     string `gorm:"column:status;size:24;not null;index:idx_goal_task_order,priority:2"`
	Depth      int    `gorm:"column:depth;not null"`
	DeviceID   string `gorm:"column:device_id;size:128;index"`
	Content    string `gorm:"column:content;type:text;not null"`
	CreatedAt  int64  `gorm:"column:created_at;not null"`
	UpdatedAt  int64  `gorm:"column:updated_at;not null"`
}

func (GoalTask) TableName() string { return "os_goal_task" }

type SpecialistRun struct {
	RunID     string `gorm:"column:run_id;primaryKey;size:64"`
	GoalID    string `gorm:"column:goal_id;size:64;not null;index"`
	TaskID    string `gorm:"column:task_id;size:64;not null;index"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index"`
	Status    string `gorm:"column:status;size:24;not null;index"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null;index"`
}

func (SpecialistRun) TableName() string { return "os_specialist_run" }

type GoalCheckpoint struct {
	CheckpointID string `gorm:"column:checkpoint_id;primaryKey;size:64"`
	GoalID       string `gorm:"column:goal_id;size:64;not null;uniqueIndex:idx_goal_checkpoint_sequence,priority:1"`
	OwnerID      string `gorm:"column:owner_id;size:64;not null;index"`
	Sequence     int64  `gorm:"column:sequence;not null;uniqueIndex:idx_goal_checkpoint_sequence,priority:2"`
	Status       string `gorm:"column:status;size:24;not null;index"`
	Checksum     string `gorm:"column:checksum;size:64;not null"`
	Content      string `gorm:"column:content;type:text;not null"`
	CreatedAt    int64  `gorm:"column:created_at;not null;index"`
}

func (GoalCheckpoint) TableName() string { return "os_goal_checkpoint" }

type ScheduleTrigger struct {
	TriggerID      string `gorm:"column:trigger_id;primaryKey;size:64"`
	ScheduleID     string `gorm:"column:schedule_id;size:64;not null;index"`
	GoalID         string `gorm:"column:goal_id;size:64;not null;index"`
	TaskID         string `gorm:"column:task_id;size:64;not null;index"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index"`
	IdempotencyKey string `gorm:"column:idempotency_key;size:192;not null;uniqueIndex"`
	Status         string `gorm:"column:status;size:24;not null;index"`
	ScheduledAt    int64  `gorm:"column:scheduled_at;not null;index"`
	UpdatedAt      int64  `gorm:"column:updated_at;not null;index"`
	Content        string `gorm:"column:content;type:text;not null"`
}

func (ScheduleTrigger) TableName() string { return "os_schedule_trigger" }
