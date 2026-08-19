package delegation

import "time"

const (
	ProposalSubmitted = "SUBMITTED"
	ProposalAccepted  = "ACCEPTED"
	ProposalRejected  = "REJECTED"

	DecisionLocal    = "LOCAL"
	DecisionDelegate = "DELEGATE"

	RunCreated            = "CREATED"
	RunAdmitted           = "ADMITTED"
	RunQueued             = "QUEUED"
	RunRunning            = "RUNNING"
	RunWaitingObservation = "WAITING_OBSERVATION"
	RunWaitingUser        = "WAITING_USER"
	RunWaitingDevice      = "WAITING_DEVICE"
	RunWaitingRetry       = "WAITING_RETRY"
	RunCompleted          = "COMPLETED"
	RunFailed             = "FAILED"
	RunCancelled          = "CANCELLED"
	RunExpired            = "EXPIRED"

	AttemptReserved           = "RESERVED"
	AttemptStarting           = "STARTING"
	AttemptRunning            = "RUNNING"
	AttemptWaitingAction      = "WAITING_ACTION"
	AttemptWaitingObservation = "WAITING_OBSERVATION"
	AttemptCancelRequested    = "CANCEL_REQUESTED"
	AttemptCompleted          = "COMPLETED"
	AttemptFailed             = "FAILED"
	AttemptTimedOut           = "TIMED_OUT"
	AttemptAbandoned          = "ABANDONED"

	BudgetRequested = "REQUESTED"
	BudgetReserved  = "RESERVED"
	BudgetCommitted = "COMMITTED"
	BudgetReleased  = "RELEASED"
	BudgetExpired   = "EXPIRED"

	LeaseRequested = "REQUESTED"
	LeaseActive    = "ACTIVE"
	LeaseReleased  = "RELEASED"
	LeaseExpired   = "EXPIRED"
	LeaseRevoked   = "REVOKED"
)

type BudgetAmount struct {
	Tokens      int64 `json:"tokens,omitempty"`
	MoneyMicros int64 `json:"money_micros,omitempty"`
	Actions     int64 `json:"actions,omitempty"`
	Queries     int64 `json:"queries,omitempty"`
	Pages       int64 `json:"pages,omitempty"`
	ComputeMS   int64 `json:"compute_ms,omitempty"`
	WallClockMS int64 `json:"wall_clock_ms,omitempty"`
}

func (b BudgetAmount) Add(other BudgetAmount) BudgetAmount {
	return BudgetAmount{
		Tokens: b.Tokens + other.Tokens, MoneyMicros: b.MoneyMicros + other.MoneyMicros,
		Actions: b.Actions + other.Actions, Queries: b.Queries + other.Queries,
		Pages: b.Pages + other.Pages, ComputeMS: b.ComputeMS + other.ComputeMS,
		WallClockMS: b.WallClockMS + other.WallClockMS,
	}
}

func (b BudgetAmount) Sub(other BudgetAmount) BudgetAmount {
	return BudgetAmount{
		Tokens: b.Tokens - other.Tokens, MoneyMicros: b.MoneyMicros - other.MoneyMicros,
		Actions: b.Actions - other.Actions, Queries: b.Queries - other.Queries,
		Pages: b.Pages - other.Pages, ComputeMS: b.ComputeMS - other.ComputeMS,
		WallClockMS: b.WallClockMS - other.WallClockMS,
	}
}

func (b BudgetAmount) NonNegative() bool {
	return b.Tokens >= 0 && b.MoneyMicros >= 0 && b.Actions >= 0 && b.Queries >= 0 && b.Pages >= 0 && b.ComputeMS >= 0 && b.WallClockMS >= 0
}

func (b BudgetAmount) FitsWithin(limit BudgetAmount) bool {
	return b.Tokens <= limit.Tokens && b.MoneyMicros <= limit.MoneyMicros && b.Actions <= limit.Actions && b.Queries <= limit.Queries && b.Pages <= limit.Pages && b.ComputeMS <= limit.ComputeMS && b.WallClockMS <= limit.WallClockMS
}

type Proposal struct {
	ProposalID string
	OwnerID    string
	GoalID     string
	TaskStepID string
	InputHash  string
	Status     string
	Revision   int64
	Content    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Decision struct {
	DecisionID        string
	OwnerID           string
	ProposalID        string
	ProposalInputHash string
	Decision          string
	PolicyVersion     string
	Content           string
	CreatedAt         time.Time
}

type Definition struct {
	ID             string
	OwnerID        string
	TaskStepID     string
	DefinitionHash string
	Content        string
	CreatedAt      time.Time
}

type Run struct {
	RunID              string
	OwnerID            string
	GoalID             string
	TaskStepID         string
	SubagentSpecID     string
	DelegatedOutcomeID string
	ActorBindingID     string
	Status             string
	ActiveAttemptID    string
	Revision           int64
	Deadline           time.Time
	CancelRequestedAt  time.Time
	TerminalReason     string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Attempt struct {
	AttemptID            string
	RunID                string
	OwnerID              string
	AttemptNo            int
	InvocationManifestID string
	IdempotencyKey       string
	OwnerInstanceID      string
	LeaseExpiresAt       time.Time
	HeartbeatAt          time.Time
	Status               string
	BudgetReservationID  string
	ResultID             string
	ErrorRef             string
	Revision             int64
	StartedAt            time.Time
	EndedAt              time.Time
}

type BudgetAccount struct {
	BudgetRef string
	OwnerID   string
	Total     BudgetAmount
	Consumed  BudgetAmount
	Reserved  BudgetAmount
	Revision  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type BudgetReservation struct {
	ReservationID string
	OwnerID       string
	BudgetRef     string
	RunID         string
	Requested     BudgetAmount
	Reserved      BudgetAmount
	Committed     BudgetAmount
	Status        string
	Revision      int64
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ResourceLease struct {
	LeaseID         string
	OwnerID         string
	RunID           string
	ResourceRef     string
	ResourceVersion string
	Mode            string
	ActionAttemptID string
	OwnerInstanceID string
	Status          string
	AcquiredAt      time.Time
	ExpiresAt       time.Time
	HeartbeatAt     time.Time
	Revision        int64
}

type Event struct {
	EventID        string
	OwnerID        string
	AggregateType  string
	AggregateID    string
	Sequence       int64
	Type           string
	IdempotencyKey string
	TraceID        string
	CausationID    string
	Payload        string
	Published      bool
	CreatedAt      time.Time
	PublishedAt    time.Time
}

type AcceptedDelegation struct {
	Proposal         Proposal
	Decision         Decision
	DelegatedOutcome Definition
	SubagentSpec     Definition
	Run              Run
	Event            Event
}

func IsAttemptTerminal(status string) bool {
	return status == AttemptCompleted || status == AttemptFailed || status == AttemptTimedOut || status == AttemptAbandoned
}

func IsRunTerminal(status string) bool {
	return status == RunCompleted || status == RunFailed || status == RunCancelled || status == RunExpired
}
