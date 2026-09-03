package learning

type Candidate struct {
	CandidateID string `gorm:"column:candidate_id;primaryKey;type:varchar(128)"`
	OwnerID     string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_candidate_owner_created,priority:1;index:idx_os_candidate_owner_status,priority:1"`
	Kind        string `gorm:"column:kind;type:varchar(24);not null;index"`
	Status      string `gorm:"column:status;type:varchar(32);not null;index:idx_os_candidate_owner_status,priority:2"`
	Definition  string `gorm:"column:definition;type:text;not null"`
	Evidence    string `gorm:"column:evidence;type:text;not null"`
	Evaluation  string `gorm:"column:evaluation;type:text;not null"`
	ReviewNote  string `gorm:"column:review_note;type:text"`
	ReviewedBy  string `gorm:"column:reviewed_by;type:varchar(128);index"`
	ReviewedAt  int64  `gorm:"column:reviewed_at;type:bigint;not null;default:0;index"`
	Revision    int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID     string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt   int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_candidate_owner_created,priority:2"`
	UpdatedAt   int64  `gorm:"column:updated_at;type:bigint;not null"`
	DeletedAt   int64  `gorm:"column:deleted_at;type:bigint;not null;default:0;index"`
}

func (*Candidate) TableName() string { return "os_learning_candidate" }

type CandidateEvidence struct {
	EvidenceID   string `gorm:"column:evidence_id;primaryKey;type:varchar(128)"`
	CandidateID  string `gorm:"column:candidate_id;type:varchar(128);not null;index:idx_os_candidate_evidence_candidate_created,priority:1"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	ExperienceID string `gorm:"column:experience_id;type:varchar(128);not null;index"`
	Relation     string `gorm:"column:relation;type:varchar(32);not null"`
	Outcome      string `gorm:"column:outcome;type:varchar(24);not null"`
	Summary      string `gorm:"column:summary;type:text"`
	TraceID      string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_candidate_evidence_candidate_created,priority:2"`
}

func (*CandidateEvidence) TableName() string { return "os_candidate_evidence" }

type CandidateEvaluation struct {
	EvaluationID string `gorm:"column:evaluation_id;primaryKey;type:varchar(128)"`
	CandidateID  string `gorm:"column:candidate_id;type:varchar(128);not null;index:idx_os_candidate_eval_candidate_created,priority:1"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	RunID        string `gorm:"column:run_id;type:varchar(128);not null;index"`
	Summary      string `gorm:"column:summary;type:text;not null"`
	TraceID      string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_candidate_eval_candidate_created,priority:2"`
}

func (*CandidateEvaluation) TableName() string { return "os_candidate_evaluation" }

type Skill struct {
	SkillID        string `gorm:"column:skill_id;primaryKey;type:varchar(160)"`
	OwnerID        string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_skill_owner_created,priority:1"`
	OrganizationID string `gorm:"column:organization_id;type:varchar(128);not null;default:'';index:idx_os_skill_org_visibility,priority:1"`
	LatestVersion  string `gorm:"column:latest_version;type:varchar(32);not null"`
	Status         string `gorm:"column:status;type:varchar(32);not null;index"`
	Visibility     string `gorm:"column:visibility;type:varchar(24);not null;index;index:idx_os_skill_org_visibility,priority:2"`
	Revision       int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID        string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt      int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_skill_owner_created,priority:2"`
	UpdatedAt      int64  `gorm:"column:updated_at;type:bigint;not null"`
	DeletedAt      int64  `gorm:"column:deleted_at;type:bigint;not null;default:0;index"`
}

func (*Skill) TableName() string { return "os_skill" }

type SkillVersion struct {
	VersionID   string `gorm:"column:version_id;primaryKey;type:varchar(128)"`
	SkillID     string `gorm:"column:skill_id;type:varchar(160);not null;uniqueIndex:idx_os_skill_version,priority:1;index"`
	Version     string `gorm:"column:version;type:varchar(32);not null;uniqueIndex:idx_os_skill_version,priority:2"`
	OwnerID     string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	CandidateID string `gorm:"column:candidate_id;type:varchar(128);not null;uniqueIndex"`
	Definition  string `gorm:"column:definition;type:text;not null"`
	Checksum    string `gorm:"column:checksum;type:varchar(64);not null"`
	TraceID     string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt   int64  `gorm:"column:created_at;type:bigint;not null;index"`
}

func (*SkillVersion) TableName() string { return "os_skill_version" }

type Strategy struct {
	StrategyID     string `gorm:"column:strategy_id;primaryKey;type:varchar(160)"`
	OwnerID        string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_strategy_owner_created,priority:1"`
	OrganizationID string `gorm:"column:organization_id;type:varchar(128);not null;default:'';index:idx_os_strategy_org_visibility,priority:1"`
	LatestVersion  string `gorm:"column:latest_version;type:varchar(32);not null"`
	Status         string `gorm:"column:status;type:varchar(32);not null;index"`
	Visibility     string `gorm:"column:visibility;type:varchar(24);not null;index;index:idx_os_strategy_org_visibility,priority:2"`
	Revision       int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID        string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt      int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_strategy_owner_created,priority:2"`
	UpdatedAt      int64  `gorm:"column:updated_at;type:bigint;not null"`
	DeletedAt      int64  `gorm:"column:deleted_at;type:bigint;not null;default:0;index"`
}

func (*Strategy) TableName() string { return "os_strategy" }

type StrategyVersion struct {
	VersionID   string `gorm:"column:version_id;primaryKey;type:varchar(128)"`
	StrategyID  string `gorm:"column:strategy_id;type:varchar(160);not null;uniqueIndex:idx_os_strategy_version,priority:1;index"`
	Version     string `gorm:"column:version;type:varchar(32);not null;uniqueIndex:idx_os_strategy_version,priority:2"`
	OwnerID     string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	CandidateID string `gorm:"column:candidate_id;type:varchar(128);not null;uniqueIndex"`
	Definition  string `gorm:"column:definition;type:text;not null"`
	Checksum    string `gorm:"column:checksum;type:varchar(64);not null"`
	TraceID     string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt   int64  `gorm:"column:created_at;type:bigint;not null;index"`
}

func (*StrategyVersion) TableName() string { return "os_strategy_version" }

type Demonstration struct {
	DemonstrationID string `gorm:"column:demonstration_id;primaryKey;type:varchar(128)"`
	OwnerID         string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_demo_owner_created,priority:1"`
	TaskID          string `gorm:"column:task_id;type:varchar(128);not null;index"`
	Status          string `gorm:"column:status;type:varchar(32);not null;index"`
	Title           string `gorm:"column:title;type:varchar(255);not null"`
	Content         string `gorm:"column:content;type:text;not null"`
	PauseCount      int    `gorm:"column:pause_count;not null;default:0"`
	ConfirmedBy     string `gorm:"column:confirmed_by;type:varchar(128)"`
	ConfirmedAt     int64  `gorm:"column:confirmed_at;type:bigint;not null;default:0;index"`
	Revision        int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID         string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt       int64  `gorm:"column:created_at;type:bigint;not null;index:idx_os_demo_owner_created,priority:2"`
	UpdatedAt       int64  `gorm:"column:updated_at;type:bigint;not null"`
	DeletedAt       int64  `gorm:"column:deleted_at;type:bigint;not null;default:0;index"`
}

func (*Demonstration) TableName() string { return "os_demonstration" }
