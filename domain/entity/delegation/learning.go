package delegation

import "time"

type LearningPreference struct {
	OwnerID   string
	Enabled   bool
	Revision  int64
	UpdatedBy string
	UpdatedAt time.Time
}

type LearningCandidate struct {
	CandidateID    string
	OwnerID        string
	Kind           string
	DefinitionHash string
	Content        string
	CreatedAt      time.Time
}

type LearningEvaluation struct {
	EvaluationID string
	OwnerID      string
	CandidateID  string
	Stage        string
	Passed       bool
	ContentHash  string
	Content      string
	CreatedAt    time.Time
}

type LearningReview struct {
	ReviewID     string
	OwnerID      string
	CandidateID  string
	EvaluationID string
	Decision     string
	ReviewerID   string
	ContentHash  string
	Content      string
	CreatedAt    time.Time
}

type LearningRollout struct {
	RolloutID   string
	OwnerID     string
	CandidateID string
	Kind        string
	Status      string
	RiskCeiling string
	Revision    int64
	ContentHash string
	Content     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type LearningBenchmark struct {
	ReportID    string
	OwnerID     string
	CandidateID string
	RolloutID   string
	Passed      bool
	ContentHash string
	Content     string
	CreatedAt   time.Time
}

type LearningSnapshot struct {
	Preference  LearningPreference
	Candidates  []LearningCandidate
	Evaluations []LearningEvaluation
	Reviews     []LearningReview
	Rollouts    []LearningRollout
	Benchmarks  []LearningBenchmark
}
