package delegation

type LearningPreference struct {
	OwnerID   string `gorm:"column:owner_id;primaryKey;size:64"`
	Enabled   bool   `gorm:"column:enabled;not null"`
	Revision  int64  `gorm:"column:revision;not null"`
	UpdatedBy string `gorm:"column:updated_by;size:64;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;not null;index"`
}

func (LearningPreference) TableName() string { return "os_dso_learning_preference" }

type LearningCandidate struct {
	CandidateID    string `gorm:"column:candidate_id;primaryKey;size:64"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index:idx_dso_learning_candidate_owner_kind,priority:1"`
	Kind           string `gorm:"column:kind;size:32;not null;index:idx_dso_learning_candidate_owner_kind,priority:2"`
	DefinitionHash string `gorm:"column:definition_hash;size:64;not null;uniqueIndex"`
	Content        string `gorm:"column:content;type:text;not null"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
}

func (LearningCandidate) TableName() string { return "os_dso_learning_candidate" }

type LearningEvaluation struct {
	EvaluationID string `gorm:"column:evaluation_id;primaryKey;size:64"`
	OwnerID      string `gorm:"column:owner_id;size:64;not null;index:idx_dso_learning_evaluation_candidate,priority:1"`
	CandidateID  string `gorm:"column:candidate_id;size:64;not null;index:idx_dso_learning_evaluation_candidate,priority:2"`
	Stage        string `gorm:"column:stage;size:24;not null;index"`
	Passed       bool   `gorm:"column:passed;not null"`
	ContentHash  string `gorm:"column:content_hash;size:64;not null;uniqueIndex"`
	Content      string `gorm:"column:content;type:text;not null"`
	CreatedAt    int64  `gorm:"column:created_at;not null;index"`
}

func (LearningEvaluation) TableName() string { return "os_dso_learning_evaluation" }

type LearningReview struct {
	ReviewID     string `gorm:"column:review_id;primaryKey;size:64"`
	OwnerID      string `gorm:"column:owner_id;size:64;not null;index:idx_dso_learning_review_candidate,priority:1"`
	CandidateID  string `gorm:"column:candidate_id;size:64;not null;index:idx_dso_learning_review_candidate,priority:2"`
	EvaluationID string `gorm:"column:evaluation_id;size:64;not null;index"`
	Decision     string `gorm:"column:decision;size:24;not null;index"`
	ReviewerID   string `gorm:"column:reviewer_id;size:64;not null"`
	ContentHash  string `gorm:"column:content_hash;size:64;not null;uniqueIndex"`
	Content      string `gorm:"column:content;type:text;not null"`
	CreatedAt    int64  `gorm:"column:created_at;not null;index"`
}

func (LearningReview) TableName() string { return "os_dso_learning_review" }

type LearningRollout struct {
	RolloutID   string `gorm:"column:rollout_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index:idx_dso_learning_rollout_effective,priority:1"`
	CandidateID string `gorm:"column:candidate_id;size:64;not null;index"`
	Kind        string `gorm:"column:kind;size:32;not null;index:idx_dso_learning_rollout_effective,priority:2"`
	Status      string `gorm:"column:status;size:24;not null;index:idx_dso_learning_rollout_effective,priority:3"`
	RiskCeiling string `gorm:"column:risk_ceiling;size:16;not null"`
	Revision    int64  `gorm:"column:revision;not null"`
	ContentHash string `gorm:"column:content_hash;size:64;not null"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null"`
	UpdatedAt   int64  `gorm:"column:updated_at;not null;index"`
}

func (LearningRollout) TableName() string { return "os_dso_learning_rollout" }

type LearningBenchmark struct {
	ReportID    string `gorm:"column:report_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index:idx_dso_learning_benchmark_rollout,priority:1"`
	CandidateID string `gorm:"column:candidate_id;size:64;not null;index"`
	RolloutID   string `gorm:"column:rollout_id;size:64;not null;index:idx_dso_learning_benchmark_rollout,priority:2"`
	Passed      bool   `gorm:"column:passed;not null"`
	ContentHash string `gorm:"column:content_hash;size:64;not null;uniqueIndex"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
}

func (LearningBenchmark) TableName() string { return "os_dso_learning_benchmark" }
