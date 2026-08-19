package delegation

import "time"

type ReplayRecord struct {
	ReplayID           string
	OwnerID            string
	SourceRunID        string
	SourceManifestID   string
	SourceManifestHash string
	Mode               string
	Status             string
	RequestedBy        string
	LiveApprovalRef    string
	RequestContent     string
	ResultContent      string
	ResultHash         string
	ErrorRef           string
	CreatedAt          time.Time
	StartedAt          time.Time
	EndedAt            time.Time
}

type ReplaySource struct {
	Run              Run
	Manifest         ImmutableRecord
	ContextSlice     ImmutableRecord
	CapabilityView   ImmutableRecord
	ActorBinding     ImmutableRecord
	SubagentSpec     Definition
	DelegatedOutcome Definition
	ObservationRefs  []string
	VerificationRefs []string
}

type SchedulerLease struct {
	LeaseKey        string
	OwnerInstanceID string
	FencingToken    int64
	Status          string
	AcquiredAt      time.Time
	HeartbeatAt     time.Time
	ExpiresAt       time.Time
	Revision        int64
}

type SLOCounters struct {
	TotalRuns                     int64
	TerminalRuns                  int64
	FailedRuns                    int64
	RecoveredAttempts             int64
	FencedLateResults             int64
	DuplicateConfirmedSideEffects int64
	CancelPropagationMS           []int64
}

type OwnerDelegationExport struct {
	Schema     string            `json:"schema"`
	OwnerID    string            `json:"owner_id"`
	ExportedAt time.Time         `json:"exported_at"`
	Runs       []Run             `json:"runs"`
	Replays    []ReplayRecord    `json:"replays"`
	Events     []Event           `json:"events"`
	Manifests  []ImmutableRecord `json:"manifests"`
}

type DeletionSummary struct {
	OwnerID     string
	Cutoff      time.Time
	DeletedRows int64
	TombstoneID string
	CompletedAt time.Time
}

type RetentionTombstone struct {
	TombstoneID string
	OwnerID     string
	Cutoff      time.Time
	DeletedRows int64
	RequestedBy string
	CreatedAt   time.Time
}
