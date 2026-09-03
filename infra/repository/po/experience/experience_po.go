package experience

// Experience stores immutable audit metadata. User-visible content is kept in
// ExperiencePayload so retention and deletion can remove it completely.
type Experience struct {
	ExperienceID  string `gorm:"column:experience_id;primaryKey;type:varchar(128)"`
	OwnerID       string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_experience_owner_created,priority:1;index:idx_os_experience_owner_status,priority:1"`
	TaskID        string `gorm:"column:task_id;type:varchar(128);not null;uniqueIndex"`
	Schema        string `gorm:"column:schema;type:varchar(64);not null"`
	Status        string `gorm:"column:status;type:varchar(24);not null;index:idx_os_experience_owner_status,priority:2"`
	SkipReason    string `gorm:"column:skip_reason;type:varchar(255)"`
	Outcome       string `gorm:"column:outcome;type:varchar(24);index"`
	FailureClass  string `gorm:"column:failure_class;type:varchar(64);index"`
	Sensitivity   string `gorm:"column:sensitivity;type:varchar(24);not null;index"`
	RetentionDays int    `gorm:"column:retention_days;not null;default:30"`
	DeleteAt      int64  `gorm:"column:delete_at;type:bigint;not null;index"`
	Tombstoned    bool   `gorm:"column:tombstoned;not null;default:false;index"`
	TraceID       string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt     int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_experience_owner_created,priority:2"`
	UpdatedAt     int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*Experience) TableName() string { return "os_experience" }

type ExperiencePayload struct {
	ExperienceID string `gorm:"column:experience_id;primaryKey;type:varchar(128)"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Content      string `gorm:"column:content;type:text;not null"`
	SearchText   string `gorm:"column:search_text;type:text;not null"`
	Vector       string `gorm:"column:vector;type:text;not null"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt    int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*ExperiencePayload) TableName() string { return "os_experience_payload" }

type ExperienceEventRef struct {
	ExperienceID string `gorm:"column:experience_id;primaryKey;type:varchar(128)"`
	EventID      string `gorm:"column:event_id;primaryKey;type:varchar(128)"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	TaskID       string `gorm:"column:task_id;type:varchar(128);not null;index"`
	EventType    string `gorm:"column:event_type;type:varchar(128);not null"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null"`
}

func (*ExperienceEventRef) TableName() string { return "os_experience_event_ref" }

// ExperienceRedaction records only the category, path, and a one-way digest.
// The original secret is never persisted.
type ExperienceRedaction struct {
	RedactionID  string `gorm:"column:redaction_id;primaryKey;type:varchar(128)"`
	ExperienceID string `gorm:"column:experience_id;type:varchar(128);not null;index"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Category     string `gorm:"column:category;type:varchar(64);not null;index"`
	FieldPath    string `gorm:"column:field_path;type:varchar(512);not null"`
	Digest       string `gorm:"column:digest;type:varchar(64);not null"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null"`
}

func (*ExperienceRedaction) TableName() string { return "os_experience_redaction" }

type FailureClassification struct {
	ExperienceID string  `gorm:"column:experience_id;primaryKey;type:varchar(128)"`
	OwnerID      string  `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_failure_owner_class,priority:1"`
	Class        string  `gorm:"column:class;type:varchar(64);not null;index:idx_os_failure_owner_class,priority:2"`
	Rule         string  `gorm:"column:rule;type:varchar(128);not null"`
	Summary      string  `gorm:"column:summary;type:text"`
	Evidence     string  `gorm:"column:evidence;type:text;not null"`
	Confidence   float64 `gorm:"column:confidence;type:decimal(6,5);not null"`
	CreatedAt    int64   `gorm:"column:created_at;type:bigint;not null;index"`
	UpdatedAt    int64   `gorm:"column:updated_at;type:bigint;not null"`
}

func (*FailureClassification) TableName() string { return "os_failure_classification" }

type ExperiencePreference struct {
	OwnerID         string `gorm:"column:owner_id;primaryKey;type:varchar(128)"`
	LearningEnabled bool   `gorm:"column:learning_enabled;not null"`
	RetentionDays   int    `gorm:"column:retention_days;not null;default:30"`
	MaxSensitivity  string `gorm:"column:max_sensitivity;type:varchar(24);not null"`
	Revision        int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	CreatedAt       int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt       int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*ExperiencePreference) TableName() string { return "os_experience_preference" }

type EvaluationFixture struct {
	FixtureID          string `gorm:"column:fixture_id;primaryKey;type:varchar(128)"`
	OwnerID            string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_eval_fixture_owner_created,priority:1"`
	ExperienceID       string `gorm:"column:experience_id;type:varchar(128);not null;index"`
	Name               string `gorm:"column:name;type:varchar(255);not null"`
	RuntimeKind        string `gorm:"column:runtime_kind;type:varchar(64);not null"`
	Simulator          string `gorm:"column:simulator;type:varchar(128);not null"`
	EnvironmentVersion string `gorm:"column:environment_version;type:varchar(128);not null"`
	SnapshotHash       string `gorm:"column:snapshot_hash;type:varchar(64);not null"`
	Protocol           string `gorm:"column:protocol;type:varchar(64);not null"`
	Content            string `gorm:"column:content;type:text;not null"`
	CreatedAt          int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_eval_fixture_owner_created,priority:2"`
	UpdatedAt          int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*EvaluationFixture) TableName() string { return "os_evaluation_fixture" }

type EvaluationSuite struct {
	SuiteID    string `gorm:"column:suite_id;primaryKey;type:varchar(128)"`
	OwnerID    string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_eval_suite_owner_created,priority:1"`
	Name       string `gorm:"column:name;type:varchar(255);not null"`
	FixtureIDs string `gorm:"column:fixture_ids;type:text;not null"`
	CreatedAt  int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_eval_suite_owner_created,priority:2"`
	UpdatedAt  int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*EvaluationSuite) TableName() string { return "os_evaluation_suite" }

type EvaluationRun struct {
	RunID           string `gorm:"column:run_id;primaryKey;type:varchar(128)"`
	OwnerID         string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_eval_run_owner_created,priority:1"`
	SuiteID         string `gorm:"column:suite_id;type:varchar(128);not null;index"`
	Status          string `gorm:"column:status;type:varchar(24);not null;index"`
	Seed            int64  `gorm:"column:seed;type:bigint;not null"`
	CandidateID     string `gorm:"column:candidate_id;type:varchar(128)"`
	BaselineID      string `gorm:"column:baseline_id;type:varchar(128)"`
	Metrics         string `gorm:"column:metrics;type:text;not null"`
	BaselineMetrics string `gorm:"column:baseline_metrics;type:text;not null;default:'{}'"`
	MetricDelta     string `gorm:"column:metric_delta;type:text;not null;default:'{}'"`
	Regression      bool   `gorm:"column:regression;not null;default:false;index"`
	RegressionCount int    `gorm:"column:regression_count;not null;default:0"`
	StartedAt       int64  `gorm:"column:started_at;type:bigint;not null"`
	FinishedAt      int64  `gorm:"column:finished_at;type:bigint"`
	Error           string `gorm:"column:error;type:text"`
	CreatedAt       int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_eval_run_owner_created,priority:2"`
	UpdatedAt       int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*EvaluationRun) TableName() string { return "os_evaluation_run" }

type EvaluationResult struct {
	ResultID        string `gorm:"column:result_id;primaryKey;type:varchar(128)"`
	OwnerID         string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	RunID           string `gorm:"column:run_id;type:varchar(128);not null;index:idx_os_eval_result_run_fixture,priority:1"`
	FixtureID       string `gorm:"column:fixture_id;type:varchar(128);not null;index:idx_os_eval_result_run_fixture,priority:2"`
	Passed          bool   `gorm:"column:passed;not null;index"`
	Metrics         string `gorm:"column:metrics;type:text;not null"`
	BaselineMetrics string `gorm:"column:baseline_metrics;type:text;not null;default:'{}'"`
	MetricDelta     string `gorm:"column:metric_delta;type:text;not null;default:'{}'"`
	Regression      bool   `gorm:"column:regression;not null;default:false;index"`
	Summary         string `gorm:"column:summary;type:text;not null"`
	EvidenceIDs     string `gorm:"column:evidence_ids;type:text;not null"`
	CreatedAt       int64  `gorm:"column:created_at;type:bigint;not null"`
}

func (*EvaluationResult) TableName() string { return "os_evaluation_result" }
