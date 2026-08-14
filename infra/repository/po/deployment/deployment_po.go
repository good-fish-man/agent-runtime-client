package deployment

type AgentBuild struct {
	BuildID   string `gorm:"column:build_id;primaryKey;size:64"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index:idx_agent_build_owner_agent,priority:1"`
	AgentID   string `gorm:"column:agent_id;size:64;not null;index:idx_agent_build_owner_agent,priority:2"`
	Version   string `gorm:"column:version;size:64;not null"`
	RiskLevel string `gorm:"column:risk_level;size:8;not null"`
	Checksum  string `gorm:"column:checksum;size:64;not null;uniqueIndex:idx_agent_build_checksum,priority:3"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedBy string `gorm:"column:created_by;size:64;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null;index"`
}

func (AgentBuild) TableName() string { return "os_agent_build" }

type RunManifest struct {
	ManifestID string `gorm:"column:manifest_id;primaryKey;size:64"`
	OwnerID    string `gorm:"column:owner_id;size:64;not null;index:idx_run_manifest_owner_agent,priority:1"`
	AgentID    string `gorm:"column:agent_id;size:64;not null;index:idx_run_manifest_owner_agent,priority:2"`
	TaskID     string `gorm:"column:task_id;size:96;not null;index"`
	BuildID    string `gorm:"column:build_id;size:64;not null;index"`
	ExposureID string `gorm:"column:exposure_id;size:64"`
	Content    string `gorm:"column:content;type:text;not null"`
	CreatedAt  int64  `gorm:"column:created_at;not null;index"`
}

func (RunManifest) TableName() string { return "os_run_manifest" }

type Promotion struct {
	PromotionID     string `gorm:"column:promotion_id;primaryKey;size:64"`
	OwnerID         string `gorm:"column:owner_id;size:64;not null;index:idx_promotion_owner_agent,priority:1"`
	AgentID         string `gorm:"column:agent_id;size:64;not null;index:idx_promotion_owner_agent,priority:2"`
	BuildID         string `gorm:"column:build_id;size:64;not null;index"`
	PreviousBuildID string `gorm:"column:previous_build_id;size:64"`
	Status          string `gorm:"column:status;size:32;not null;index"`
	RiskLevel       string `gorm:"column:risk_level;size:8;not null"`
	CanaryPercent   int    `gorm:"column:canary_percent;not null"`
	Thresholds      string `gorm:"column:thresholds;type:text;not null"`
	Verified        bool   `gorm:"column:verified;not null"`
	Recoverable     bool   `gorm:"column:recoverable;not null"`
	ApprovedBy      string `gorm:"column:approved_by;size:64"`
	Revision        int64  `gorm:"column:revision;not null"`
	CreatedAt       int64  `gorm:"column:created_at;not null"`
	UpdatedAt       int64  `gorm:"column:updated_at;not null;index"`
}

func (Promotion) TableName() string { return "os_promotion" }

type Exposure struct {
	ExposureID  string `gorm:"column:exposure_id;primaryKey;size:64"`
	PromotionID string `gorm:"column:promotion_id;size:64;not null;uniqueIndex:idx_exposure_assignment,priority:1"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;uniqueIndex:idx_exposure_assignment,priority:2"`
	AgentID     string `gorm:"column:agent_id;size:64;not null;uniqueIndex:idx_exposure_assignment,priority:3"`
	Bucket      int    `gorm:"column:bucket;not null"`
	Variant     string `gorm:"column:variant;size:16;not null"`
	OptedOut    bool   `gorm:"column:opted_out;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null"`
	UpdatedAt   int64  `gorm:"column:updated_at;not null"`
}

func (Exposure) TableName() string { return "os_exposure" }

type ShadowResult struct {
	ShadowID    string `gorm:"column:shadow_id;primaryKey;size:64"`
	PromotionID string `gorm:"column:promotion_id;size:64;not null;index"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	TaskID      string `gorm:"column:task_id;size:96;not null"`
	Content     string `gorm:"column:content;type:text;not null"`
	Passed      bool   `gorm:"column:passed;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
}

func (ShadowResult) TableName() string { return "os_shadow_result" }

type CanaryMetric struct {
	MetricID      string `gorm:"column:metric_id;primaryKey;size:64"`
	PromotionID   string `gorm:"column:promotion_id;size:64;not null;index"`
	OwnerID       string `gorm:"column:owner_id;size:64;not null;index"`
	Content       string `gorm:"column:content;type:text;not null"`
	StopTriggered bool   `gorm:"column:stop_triggered;not null"`
	CreatedAt     int64  `gorm:"column:created_at;not null;index"`
}

func (CanaryMetric) TableName() string { return "os_canary_metric" }

type CanarySample struct {
	SampleID    string `gorm:"column:sample_id;primaryKey;size:64"`
	PromotionID string `gorm:"column:promotion_id;size:64;not null;index:idx_canary_sample_promotion_created,priority:1"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	ManifestID  string `gorm:"column:manifest_id;size:64;not null;uniqueIndex"`
	ExposureID  string `gorm:"column:exposure_id;size:64;not null;index"`
	BuildID     string `gorm:"column:build_id;size:64;not null;index"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index:idx_canary_sample_promotion_created,priority:2"`
}

func (CanarySample) TableName() string { return "os_canary_sample" }

type Rollback struct {
	RollbackID  string `gorm:"column:rollback_id;primaryKey;size:64"`
	PromotionID string `gorm:"column:promotion_id;size:64;not null;index"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	AgentID     string `gorm:"column:agent_id;size:64;not null"`
	FromBuildID string `gorm:"column:from_build_id;size:64;not null"`
	ToBuildID   string `gorm:"column:to_build_id;size:64;not null"`
	Reason      string `gorm:"column:reason;type:text;not null"`
	RequestedBy string `gorm:"column:requested_by;size:64;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
}

func (Rollback) TableName() string { return "os_rollback" }

type Compensation struct {
	CompensationID string `gorm:"column:compensation_id;primaryKey;size:64"`
	RollbackID     string `gorm:"column:rollback_id;size:64;not null;index"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index"`
	ActionID       string `gorm:"column:action_id;size:96;not null"`
	Status         string `gorm:"column:status;size:24;not null"`
	Instructions   string `gorm:"column:instructions;type:text;not null"`
	CreatedAt      int64  `gorm:"column:created_at;not null"`
}

func (Compensation) TableName() string { return "os_compensation" }
