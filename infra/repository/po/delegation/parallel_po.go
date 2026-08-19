package delegation

type ParallelPlan struct {
	PlanID         string `gorm:"column:plan_id;primaryKey;size:64"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index:idx_dso_parallel_plan_owner_status,priority:1"`
	GoalID         string `gorm:"column:goal_id;size:64;not null;index"`
	TaskStepID     string `gorm:"column:task_step_id;size:64;not null;index"`
	Status         string `gorm:"column:status;size:32;not null;index:idx_dso_parallel_plan_owner_status,priority:2"`
	DefinitionHash string `gorm:"column:definition_hash;size:64;not null;index"`
	Content        string `gorm:"column:content;type:text;not null"`
	Revision       int64  `gorm:"column:revision;not null"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt      int64  `gorm:"column:updated_at;not null;index"`
}

func (ParallelPlan) TableName() string { return "os_dso_parallel_plan" }

type ParallelNode struct {
	PlanID     string `gorm:"column:plan_id;size:64;not null;uniqueIndex:idx_dso_parallel_node,priority:1;index"`
	NodeID     string `gorm:"column:node_id;size:64;not null;uniqueIndex:idx_dso_parallel_node,priority:2"`
	OwnerID    string `gorm:"column:owner_id;size:64;not null;index"`
	Role       string `gorm:"column:role;size:96;not null;index"`
	Status     string `gorm:"column:status;size:32;not null;index"`
	Attempt    int    `gorm:"column:attempt;not null"`
	ResultID   string `gorm:"column:result_id;size:64;index"`
	ErrorChain string `gorm:"column:error_chain;type:text"`
	Content    string `gorm:"column:content;type:text;not null"`
	Revision   int64  `gorm:"column:revision;not null"`
	CreatedAt  int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt  int64  `gorm:"column:updated_at;not null;index"`
}

func (ParallelNode) TableName() string { return "os_dso_parallel_node" }

type ParallelAggregate struct {
	AggregateID string `gorm:"column:aggregate_id;primaryKey;size:64"`
	PlanID      string `gorm:"column:plan_id;size:64;not null;uniqueIndex"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	Status      string `gorm:"column:status;size:32;not null;index"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
}

func (ParallelAggregate) TableName() string { return "os_dso_parallel_aggregate" }
