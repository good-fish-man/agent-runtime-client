package delegation

import "time"

const (
	VerificationSatisfied   = "satisfied"
	VerificationUnsatisfied = "unsatisfied"
	VerificationUnknown     = "unknown"
	VerificationConflicting = "conflicting"
)

// ImmutableRecord stores a validated, content-addressed DSO artifact without
// coupling the repository layer to the draft wire representation.
type ImmutableRecord struct {
	ID          string
	OwnerID     string
	RunID       string
	ContentHash string
	Content     string
	CreatedAt   time.Time
}

type InvocationBundle struct {
	ContextSlice   ImmutableRecord
	CapabilityView ImmutableRecord
	ActorBinding   ImmutableRecord
	Manifest       ImmutableRecord
}

type DecisionTurn struct {
	TurnID       string
	AttemptID    string
	OwnerID      string
	Sequence     int64
	DecisionType string
	Content      string
	CreatedAt    time.Time
}

type ModelInvocation struct {
	InvocationID     string
	TurnID           string
	OwnerID          string
	Provider         string
	ModelRef         string
	PromptTokens     int64
	CompletionTokens int64
	LatencyMS        int64
	Status           string
	Content          string
	StartedAt        time.Time
	EndedAt          time.Time
}

type CandidateResult struct {
	ResultID  string
	OwnerID   string
	RunID     string
	AttemptID string
	Status    string
	Content   string
	CreatedAt time.Time
}

type VerificationResult struct {
	VerificationID string
	OwnerID        string
	OutcomeID      string
	RunID          string
	AttemptID      string
	EffectClauseID string
	Status         string
	ExpectedValue  string
	ObservedValue  string
	EvidenceRefs   string
	Confidence     float64
	Content        string
	VerifiedAt     time.Time
}
