package learning

import (
	"time"

	learningv1 "github.com/good-fish-man/athena-protocol/protocol/learning/v1"
)

const (
	Schema                       = learningv1.Schema
	CandidateSkill               = learningv1.CandidateSkill
	CandidateStrategy            = learningv1.CandidateStrategy
	LifecycleDraft               = learningv1.LifecycleDraft
	LifecycleValidating          = learningv1.LifecycleValidating
	LifecycleEvaluating          = learningv1.LifecycleEvaluating
	LifecycleReviewRequired      = learningv1.LifecycleReviewRequired
	LifecycleApproved            = learningv1.LifecycleApproved
	LifecycleRejected            = learningv1.LifecycleRejected
	LifecycleDeprecated          = learningv1.LifecycleDeprecated
	LifecycleRetired             = learningv1.LifecycleRetired
	VisibilityPrivate            = learningv1.VisibilityPrivate
	VisibilityTeam               = learningv1.VisibilityTeam
	VisibilityPublic             = learningv1.VisibilityPublic
	RiskLow                      = learningv1.RiskLow
	RiskMedium                   = learningv1.RiskMedium
	RiskHigh                     = learningv1.RiskHigh
	RiskCritical                 = learningv1.RiskCritical
	DemonstrationRecording       = learningv1.DemonstrationRecording
	DemonstrationPausedSensitive = learningv1.DemonstrationPausedSensitive
	DemonstrationPreview         = learningv1.DemonstrationPreview
	DemonstrationConfirmed       = learningv1.DemonstrationConfirmed
	DemonstrationDiscarded       = learningv1.DemonstrationDiscarded
)

type Candidate = learningv1.Candidate
type SkillDefinition = learningv1.SkillDefinition
type StrategyDefinition = learningv1.StrategyDefinition
type EvidenceSummary = learningv1.EvidenceSummary
type EvaluationSummary = learningv1.EvaluationSummary
type ConfidenceInterval = learningv1.ConfidenceInterval
type CapabilityPolicy = learningv1.CapabilityPolicy
type ValidationPolicy = learningv1.ValidationPolicy
type Demonstration = learningv1.Demonstration
type DemonstrationStep = learningv1.DemonstrationStep
type Predicate = learningv1.Predicate
type TaskStep = learningv1.TaskStep
type TaskGraphTemplate = learningv1.TaskGraphTemplate
type VerificationRule = learningv1.VerificationRule
type EvaluationSuiteRef = learningv1.EvaluationSuiteRef

type CandidateEvidence struct {
	EvidenceID   string    `json:"evidence_id"`
	CandidateID  string    `json:"candidate_id"`
	OwnerID      string    `json:"owner_id"`
	ExperienceID string    `json:"experience_id"`
	Relation     string    `json:"relation"`
	Outcome      string    `json:"outcome"`
	Summary      string    `json:"summary"`
	TraceID      string    `json:"trace_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type CandidateEvaluation struct {
	EvaluationID string            `json:"evaluation_id"`
	CandidateID  string            `json:"candidate_id"`
	OwnerID      string            `json:"owner_id"`
	RunID        string            `json:"run_id"`
	Summary      EvaluationSummary `json:"summary"`
	TraceID      string            `json:"trace_id,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

type Skill struct {
	SkillID       string          `json:"skill_id"`
	OwnerID       string          `json:"owner_id"`
	LatestVersion string          `json:"latest_version"`
	Status        string          `json:"status"`
	Visibility    string          `json:"visibility"`
	Definition    SkillDefinition `json:"definition"`
	Revision      int64           `json:"revision"`
	TraceID       string          `json:"trace_id,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Strategy struct {
	StrategyID    string             `json:"strategy_id"`
	OwnerID       string             `json:"owner_id"`
	LatestVersion string             `json:"latest_version"`
	Status        string             `json:"status"`
	Visibility    string             `json:"visibility"`
	Definition    StrategyDefinition `json:"definition"`
	Revision      int64              `json:"revision"`
	TraceID       string             `json:"trace_id,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type CandidateFilter struct {
	Kind   string
	Status string
	Limit  int
	Offset int
}

type SemanticAction struct {
	Capability string
	Operation  string
	Arguments  map[string]any
}
