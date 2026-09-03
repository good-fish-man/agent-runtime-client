package experience

import (
	"time"

	experiencev1 "github.com/good-fish-man/athena-protocol/protocol/experience/v1"
)

const (
	Schema                     = experiencev1.Schema
	StatusReady                = experiencev1.StatusReady
	StatusSkipped              = experiencev1.StatusSkipped
	StatusDeleted              = experiencev1.StatusDeleted
	OutcomeSucceeded           = experiencev1.OutcomeSucceeded
	OutcomeFailed              = experiencev1.OutcomeFailed
	OutcomeCancelled           = experiencev1.OutcomeCancelled
	SensitivityInternal        = experiencev1.SensitivityInternal
	SensitivitySensitive       = experiencev1.SensitivitySensitive
	SensitivityRestricted      = experiencev1.SensitivityRestricted
	PayloadSeparate            = experiencev1.PayloadSeparate
	PayloadNone                = experiencev1.PayloadNone
	PayloadDeleted             = experiencev1.PayloadDeleted
	FailureIntent              = experiencev1.FailureIntent
	FailureRouting             = experiencev1.FailureRouting
	FailurePlanning            = experiencev1.FailurePlanning
	FailureModel               = experiencev1.FailureModel
	FailureCapabilitySelection = experiencev1.FailureCapabilitySelection
	FailureArgument            = experiencev1.FailureArgument
	FailurePolicy              = experiencev1.FailurePolicy
	FailureDeviceOffline       = experiencev1.FailureDeviceOffline
	FailureRuntime             = experiencev1.FailureRuntime
	FailurePerception          = experiencev1.FailurePerception
	FailureVerification        = experiencev1.FailureVerification
	FailureEnvironmentDrift    = experiencev1.FailureEnvironmentDrift
	FailureUserInterruption    = experiencev1.FailureUserInterruption
	EvaluationPending          = experiencev1.EvaluationPending
	EvaluationRunning          = experiencev1.EvaluationRunning
	EvaluationCompleted        = experiencev1.EvaluationCompleted
	EvaluationFailed           = experiencev1.EvaluationFailed
)

type Experience = experiencev1.Experience
type Preference = experiencev1.Preference
type SearchRequest = experiencev1.SearchRequest
type SearchBudget = experiencev1.SearchBudget
type SearchHit = experiencev1.SearchHit
type FailureClassification = experiencev1.FailureClassification
type RetentionPolicy = experiencev1.RetentionPolicy
type Provenance = experiencev1.Provenance
type ActionRef = experiencev1.ActionRef
type ObservationRef = experiencev1.ObservationRef
type Verification = experiencev1.Verification
type CostSummary = experiencev1.CostSummary
type ModelUsage = experiencev1.ModelUsage
type CapabilityUsage = experiencev1.CapabilityUsage
type HumanIntervention = experiencev1.HumanIntervention
type EvaluationFixture = experiencev1.EvaluationFixture
type EvaluationSuite = experiencev1.EvaluationSuite
type EvaluationRun = experiencev1.EvaluationRun
type EvaluationResult = experiencev1.EvaluationResult
type EvaluationMetrics = experiencev1.EvaluationMetrics
type ExpectedOutcome = experiencev1.ExpectedOutcome

type Redaction struct {
	RedactionID  string    `json:"redaction_id"`
	ExperienceID string    `json:"experience_id"`
	OwnerID      string    `json:"owner_id"`
	Category     string    `json:"category"`
	FieldPath    string    `json:"field_path"`
	Digest       string    `json:"digest"`
	CreatedAt    time.Time `json:"created_at"`
}

type EventRef struct {
	ExperienceID string    `json:"experience_id"`
	EventID      string    `json:"event_id"`
	OwnerID      string    `json:"owner_id"`
	TaskID       string    `json:"task_id"`
	EventType    string    `json:"event_type"`
	CreatedAt    time.Time `json:"created_at"`
}

type StoredExperience struct {
	Experience Experience
	Payload    string
	SearchText string
	Vector     []float64
	EventRefs  []EventRef
	Redactions []Redaction
}

type ListFilter struct {
	Status       string
	Outcome      string
	FailureClass string
	Sensitivity  string
	Query        string
	Limit        int
	Offset       int
}

type SearchCandidate struct {
	Experience Experience
	SearchText string
	Vector     []float64
}

type Stats struct {
	Total              int64            `json:"total"`
	TerminalTasks      int64            `json:"terminal_tasks"`
	CoveredTasks       int64            `json:"covered_tasks"`
	PendingTasks       int64            `json:"pending_tasks"`
	CoverageRate       float64          `json:"coverage_rate"`
	Ready              int64            `json:"ready"`
	Skipped            int64            `json:"skipped"`
	Deleted            int64            `json:"deleted"`
	Redactions         int64            `json:"redactions"`
	EvaluationRuns     int64            `json:"evaluation_runs"`
	EvaluationPassRate float64          `json:"evaluation_pass_rate"`
	FailureClasses     map[string]int64 `json:"failure_classes"`
}

type PendingTask struct {
	TaskID string
}
