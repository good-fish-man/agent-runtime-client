package delegation

import "time"

const (
	ActionReserved        = "RESERVED"
	ActionPolicyAllowed   = "POLICY_ALLOWED"
	ActionPolicyDenied    = "POLICY_DENIED"
	ActionWaitingApproval = "WAITING_APPROVAL"
	ActionLeased          = "LEASED"
	ActionExecuting       = "EXECUTING"
	ActionSucceeded       = "SUCCEEDED"
	ActionFailed          = "FAILED"
	ActionCancelled       = "CANCELLED"
	ActionUnknownOutcome  = "UNKNOWN_OUTCOME"

	PlanRunCreated   = "CREATED"
	PlanRunRunning   = "RUNNING"
	PlanRunWaiting   = "WAITING"
	PlanRunCompleted = "COMPLETED"
	PlanRunFailed    = "FAILED"
	PlanRunCancelled = "CANCELLED"
	PlanRunUnknown   = "UNKNOWN_OUTCOME"
)

type ActionProposalRecord struct {
	ActionProposalID  string
	OwnerID           string
	GoalID            string
	TaskStepID        string
	DecisionTurnID    string
	SubagentRunID     string
	SubagentAttemptID string
	Capability        string
	Operation         string
	ResourceRef       string
	ResourceVersion   string
	InputHash         string
	Content           string
	CreatedAt         time.Time
}

type PlanCandidateRecord struct {
	PlanCandidateID  string
	OwnerID          string
	TaskStepID       string
	ActionProposalID string
	ResourceRef      string
	ResourceVersion  string
	DefinitionHash   string
	Content          string
	CreatedAt        time.Time
}

type ExecutionContextRecord struct {
	ExecutionContextID string
	OwnerID            string
	TaskStepID         string
	ContentHash        string
	Content            string
	CreatedAt          time.Time
}

type ActionPolicyDecisionRecord struct {
	PolicyDecisionID string
	OwnerID          string
	PlanCandidateID  string
	ActionProposalID string
	WorldReadSetHash string
	InputHash        string
	PolicyVersion    string
	Decision         string
	Content          string
	DecidedAt        time.Time
	ExpiresAt        time.Time
}

type ActionPlanRunRecord struct {
	PlanRunID          string
	OwnerID            string
	PlanCandidateID    string
	ExecutionContextID string
	SubagentRunID      string
	SubagentAttemptID  string
	Status             string
	Revision           int64
	Content            string
	StartedAt          time.Time
	EndedAt            time.Time
}

type GovernedActionAttemptRecord struct {
	ActionAttemptID       string
	OwnerID               string
	PlanRunID             string
	PlanCandidateID       string
	PolicyDecisionID      string
	ActionProposalID      string
	ResourceLeaseID       string
	ObservationID         string
	ResourceVersionBefore string
	ResourceVersionAfter  string
	Status                string
	Revision              int64
	ErrorChain            string
	Content               string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	EndedAt               time.Time
}

type ActionVerificationRecord struct {
	VerificationID  string
	OwnerID         string
	OutcomeID       string
	PlanRunID       string
	ActionAttemptID string
	EffectClauseID  string
	Status          string
	Confidence      float64
	EvidenceRefs    string
	Content         string
	VerifiedAt      time.Time
}

type ActionChain struct {
	Proposal         ActionProposalRecord
	Plan             PlanCandidateRecord
	ExecutionContext ExecutionContextRecord
	Policy           ActionPolicyDecisionRecord
	PlanRun          ActionPlanRunRecord
	Attempt          GovernedActionAttemptRecord
	Event            Event
}

type ActionCompletion struct {
	OwnerID                 string
	PlanRunID               string
	ActionAttemptID         string
	ResourceLeaseID         string
	ExpectedAttemptRevision int64
	ExpectedPlanRevision    int64
	AttemptStatus           string
	PlanStatus              string
	ObservationID           string
	ResourceVersionAfter    string
	ErrorChain              string
	AttemptContent          string
	PlanContent             string
	Verification            ActionVerificationRecord
	RecordedAt              time.Time
	EndedAt                 time.Time
	Event                   Event
}

type ResourceSnapshot struct {
	ResourceRef     string
	ResourceVersion string
	SessionID       string
	TabID           string
	ObservationID   string
	TaskRevision    int64
}
