package learning

import (
	"time"

	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
)

const (
	Schema                       = learningv2.Schema
	CandidateSkill               = learningv2.CandidateSkill
	CandidateStrategy            = learningv2.CandidateStrategy
	LifecycleDraft               = learningv2.LifecycleDraft
	LifecycleValidating          = learningv2.LifecycleValidating
	LifecycleEvaluating          = learningv2.LifecycleEvaluating
	LifecycleReviewRequired      = learningv2.LifecycleReviewRequired
	LifecycleApproved            = learningv2.LifecycleApproved
	LifecycleRejected            = learningv2.LifecycleRejected
	LifecycleDeprecated          = learningv2.LifecycleDeprecated
	LifecycleRetired             = learningv2.LifecycleRetired
	VisibilityPrivate            = learningv2.VisibilityPrivate
	VisibilityTeam               = learningv2.VisibilityTeam
	VisibilityPublic             = learningv2.VisibilityPublic
	RiskLow                      = learningv2.RiskLow
	RiskMedium                   = learningv2.RiskMedium
	RiskHigh                     = learningv2.RiskHigh
	RiskCritical                 = learningv2.RiskCritical
	DemonstrationRecording       = learningv2.DemonstrationRecording
	DemonstrationPausedSensitive = learningv2.DemonstrationPausedSensitive
	DemonstrationPreview         = learningv2.DemonstrationPreview
	DemonstrationConfirmed       = learningv2.DemonstrationConfirmed
	DemonstrationDiscarded       = learningv2.DemonstrationDiscarded
	MinimumCandidateSamples      = learningv2.MinimumCandidateSamples
	MinimumStrategyScore         = learningv2.MinimumStrategyScore
)

type Candidate = learningv2.Candidate
type SkillDefinition = learningv2.SkillDefinition
type StrategyDefinition = learningv2.StrategyDefinition
type EvidenceSummary = learningv2.EvidenceSummary
type EvidenceContext = learningv2.EvidenceContext
type EvaluationSummary = learningv2.EvaluationSummary
type ConfidenceInterval = learningv2.ConfidenceInterval
type CapabilityPolicy = learningv2.CapabilityPolicy
type ValidationPolicy = learningv2.ValidationPolicy
type Demonstration = learningv2.Demonstration
type DemonstrationStep = learningv2.DemonstrationStep
type Predicate = learningv2.Predicate
type TaskStep = learningv2.TaskStep
type TaskGraphTemplate = learningv2.TaskGraphTemplate
type RecoveryPath = learningv2.RecoveryPath
type VerificationRule = learningv2.VerificationRule
type EvaluationSuiteRef = learningv2.EvaluationSuiteRef

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
	SkillID        string          `json:"skill_id"`
	OwnerID        string          `json:"owner_id"`
	OrganizationID string          `json:"organization_id,omitempty"`
	LatestVersion  string          `json:"latest_version"`
	Status         string          `json:"status"`
	Visibility     string          `json:"visibility"`
	Definition     SkillDefinition `json:"definition"`
	Revision       int64           `json:"revision"`
	TraceID        string          `json:"trace_id,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Strategy struct {
	StrategyID     string             `json:"strategy_id"`
	OwnerID        string             `json:"owner_id"`
	OrganizationID string             `json:"organization_id,omitempty"`
	LatestVersion  string             `json:"latest_version"`
	Status         string             `json:"status"`
	Visibility     string             `json:"visibility"`
	Definition     StrategyDefinition `json:"definition"`
	Revision       int64              `json:"revision"`
	TraceID        string             `json:"trace_id,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
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
