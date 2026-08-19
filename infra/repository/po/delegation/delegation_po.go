package delegation

type Proposal struct {
	ProposalID string `gorm:"column:proposal_id;primaryKey;size:64"`
	OwnerID    string `gorm:"column:owner_id;size:64;not null;index:idx_dso_proposal_owner_status,priority:1"`
	GoalID     string `gorm:"column:goal_id;size:64;not null;index"`
	TaskStepID string `gorm:"column:task_step_id;size:64;not null;index"`
	InputHash  string `gorm:"column:input_hash;size:64;not null"`
	Status     string `gorm:"column:status;size:32;not null;index:idx_dso_proposal_owner_status,priority:2"`
	Revision   int64  `gorm:"column:revision;not null"`
	Content    string `gorm:"column:content;type:text;not null"`
	CreatedAt  int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt  int64  `gorm:"column:updated_at;not null"`
}

func (Proposal) TableName() string { return "os_delegation_proposal" }

type Decision struct {
	DecisionID        string `gorm:"column:decision_id;primaryKey;size:64"`
	OwnerID           string `gorm:"column:owner_id;size:64;not null;index"`
	ProposalID        string `gorm:"column:proposal_id;size:64;not null;uniqueIndex"`
	ProposalInputHash string `gorm:"column:proposal_input_hash;size:64;not null"`
	Decision          string `gorm:"column:decision;size:24;not null;index"`
	PolicyVersion     string `gorm:"column:policy_version;size:64;not null"`
	Content           string `gorm:"column:content;type:text;not null"`
	CreatedAt         int64  `gorm:"column:created_at;not null;index"`
}

func (Decision) TableName() string { return "os_delegation_decision" }

type DelegatedOutcome struct {
	OutcomeID      string `gorm:"column:outcome_id;primaryKey;size:64"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index"`
	TaskStepID     string `gorm:"column:task_step_id;size:64;not null;index"`
	DefinitionHash string `gorm:"column:definition_hash;size:64;not null;index"`
	Content        string `gorm:"column:content;type:text;not null"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
}

func (DelegatedOutcome) TableName() string { return "os_delegated_outcome" }

type SubagentSpec struct {
	SpecID         string `gorm:"column:spec_id;primaryKey;size:64"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index"`
	TaskStepID     string `gorm:"column:task_step_id;size:64;not null;index"`
	DefinitionHash string `gorm:"column:definition_hash;size:64;not null;index"`
	Content        string `gorm:"column:content;type:text;not null"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
}

func (SubagentSpec) TableName() string { return "os_subagent_spec" }

type InvocationManifest struct {
	ManifestID  string `gorm:"column:manifest_id;primaryKey;size:64"`
	OwnerID     string `gorm:"column:owner_id;size:64;not null;index"`
	RunID       string `gorm:"column:run_id;size:64;not null;index"`
	ContentHash string `gorm:"column:content_hash;size:64;not null;uniqueIndex"`
	Content     string `gorm:"column:content;type:text;not null"`
	CreatedAt   int64  `gorm:"column:created_at;not null;index"`
}

func (InvocationManifest) TableName() string { return "os_invocation_manifest" }

type Run struct {
	RunID              string `gorm:"column:run_id;primaryKey;size:64"`
	OwnerID            string `gorm:"column:owner_id;size:64;not null;index:idx_dso_run_owner_status,priority:1"`
	GoalID             string `gorm:"column:goal_id;size:64;not null;index"`
	TaskStepID         string `gorm:"column:task_step_id;size:64;not null;index"`
	SubagentSpecID     string `gorm:"column:subagent_spec_id;size:64;not null"`
	DelegatedOutcomeID string `gorm:"column:delegated_outcome_id;size:64;not null"`
	ActorBindingID     string `gorm:"column:actor_binding_id;size:64;not null"`
	Status             string `gorm:"column:status;size:32;not null;index:idx_dso_run_owner_status,priority:2"`
	ActiveAttemptID    string `gorm:"column:active_attempt_id;size:64;index"`
	Revision           int64  `gorm:"column:revision;not null"`
	Deadline           int64  `gorm:"column:deadline;not null;index"`
	CancelRequestedAt  int64  `gorm:"column:cancel_requested_at"`
	TerminalReason     string `gorm:"column:terminal_reason;type:text"`
	CreatedAt          int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt          int64  `gorm:"column:updated_at;not null;index"`
}

func (Run) TableName() string { return "os_subagent_run" }

type Attempt struct {
	AttemptID            string `gorm:"column:attempt_id;primaryKey;size:64"`
	RunID                string `gorm:"column:run_id;size:64;not null;uniqueIndex:idx_dso_attempt_no,priority:1;index"`
	OwnerID              string `gorm:"column:owner_id;size:64;not null;index"`
	AttemptNo            int    `gorm:"column:attempt_no;not null;uniqueIndex:idx_dso_attempt_no,priority:2"`
	InvocationManifestID string `gorm:"column:invocation_manifest_id;size:64;not null;index"`
	IdempotencyKey       string `gorm:"column:idempotency_key;size:192;not null;uniqueIndex"`
	OwnerInstanceID      string `gorm:"column:owner_instance_id;size:128;not null;index"`
	LeaseExpiresAt       int64  `gorm:"column:lease_expires_at;not null;index"`
	HeartbeatAt          int64  `gorm:"column:heartbeat_at;not null;index"`
	Status               string `gorm:"column:status;size:32;not null;index"`
	BudgetReservationID  string `gorm:"column:budget_reservation_id;size:64;not null;index"`
	ResultID             string `gorm:"column:result_id;size:64"`
	ErrorRef             string `gorm:"column:error_ref;type:text"`
	Revision             int64  `gorm:"column:revision;not null"`
	StartedAt            int64  `gorm:"column:started_at;not null"`
	EndedAt              int64  `gorm:"column:ended_at"`
}

func (Attempt) TableName() string { return "os_subagent_attempt" }

type DecisionTurn struct {
	TurnID       string `gorm:"column:turn_id;primaryKey;size:64"`
	AttemptID    string `gorm:"column:attempt_id;size:64;not null;uniqueIndex:idx_dso_turn_sequence,priority:1"`
	OwnerID      string `gorm:"column:owner_id;size:64;not null;index"`
	Sequence     int64  `gorm:"column:sequence;not null;uniqueIndex:idx_dso_turn_sequence,priority:2"`
	DecisionType string `gorm:"column:decision_type;size:32;not null;index"`
	Content      string `gorm:"column:content;type:text;not null"`
	CreatedAt    int64  `gorm:"column:created_at;not null;index"`
}

func (DecisionTurn) TableName() string { return "os_decision_turn" }

type ModelInvocation struct {
	InvocationID     string `gorm:"column:invocation_id;primaryKey;size:64"`
	TurnID           string `gorm:"column:turn_id;size:64;not null;index"`
	OwnerID          string `gorm:"column:owner_id;size:64;not null;index"`
	Provider         string `gorm:"column:provider;size:64;not null;index"`
	ModelRef         string `gorm:"column:model_ref;size:128;not null;index"`
	PromptTokens     int64  `gorm:"column:prompt_tokens;not null"`
	CompletionTokens int64  `gorm:"column:completion_tokens;not null"`
	LatencyMS        int64  `gorm:"column:latency_ms;not null"`
	Status           string `gorm:"column:status;size:32;not null;index"`
	Content          string `gorm:"column:content;type:text;not null"`
	StartedAt        int64  `gorm:"column:started_at;not null;index"`
	EndedAt          int64  `gorm:"column:ended_at"`
}

func (ModelInvocation) TableName() string { return "os_model_invocation" }

type BudgetAccount struct {
	BudgetRef string `gorm:"column:budget_ref;primaryKey;size:64"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index"`
	Revision  int64  `gorm:"column:revision;not null"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;not null"`
}

func (BudgetAccount) TableName() string { return "os_budget_account" }

type BudgetReservation struct {
	ReservationID string `gorm:"column:reservation_id;primaryKey;size:64"`
	OwnerID       string `gorm:"column:owner_id;size:64;not null;index"`
	BudgetRef     string `gorm:"column:budget_ref;size:64;not null;index"`
	RunID         string `gorm:"column:run_id;size:64;not null;index"`
	Status        string `gorm:"column:status;size:32;not null;index"`
	Revision      int64  `gorm:"column:revision;not null"`
	Content       string `gorm:"column:content;type:text;not null"`
	ExpiresAt     int64  `gorm:"column:expires_at;not null;index"`
	CreatedAt     int64  `gorm:"column:created_at;not null"`
	UpdatedAt     int64  `gorm:"column:updated_at;not null"`
}

func (BudgetReservation) TableName() string { return "os_budget_reservation" }

type ResourceLease struct {
	LeaseID         string `gorm:"column:lease_id;primaryKey;size:64"`
	OwnerID         string `gorm:"column:owner_id;size:64;not null;index"`
	RunID           string `gorm:"column:run_id;size:64;not null;index"`
	ResourceRef     string `gorm:"column:resource_ref;size:192;not null;index:idx_dso_resource_lease,priority:1"`
	ResourceVersion string `gorm:"column:resource_version;size:128;not null"`
	Mode            string `gorm:"column:mode;size:24;not null;index:idx_dso_resource_lease,priority:2"`
	ActionAttemptID string `gorm:"column:action_attempt_id;size:64;not null;index"`
	OwnerInstanceID string `gorm:"column:owner_instance_id;size:128;not null"`
	Status          string `gorm:"column:status;size:24;not null;index:idx_dso_resource_lease,priority:3"`
	AcquiredAt      int64  `gorm:"column:acquired_at"`
	ExpiresAt       int64  `gorm:"column:expires_at;not null;index"`
	HeartbeatAt     int64  `gorm:"column:heartbeat_at"`
	Revision        int64  `gorm:"column:revision;not null"`
}

func (ResourceLease) TableName() string { return "os_resource_lease" }

type CandidateResult struct {
	ResultID  string `gorm:"column:result_id;primaryKey;size:64"`
	OwnerID   string `gorm:"column:owner_id;size:64;not null;index"`
	RunID     string `gorm:"column:run_id;size:64;not null;index"`
	AttemptID string `gorm:"column:attempt_id;size:64;not null;index"`
	Status    string `gorm:"column:status;size:32;not null;index"`
	Content   string `gorm:"column:content;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;not null;index"`
}

func (CandidateResult) TableName() string { return "os_candidate_result" }

type Event struct {
	EventID        string `gorm:"column:event_id;primaryKey;size:64"`
	OwnerID        string `gorm:"column:owner_id;size:64;not null;index"`
	AggregateType  string `gorm:"column:aggregate_type;size:32;not null;uniqueIndex:idx_dso_event_sequence,priority:1"`
	AggregateID    string `gorm:"column:aggregate_id;size:64;not null;uniqueIndex:idx_dso_event_sequence,priority:2;index"`
	Sequence       int64  `gorm:"column:sequence;not null;uniqueIndex:idx_dso_event_sequence,priority:3"`
	Type           string `gorm:"column:type;size:64;not null;index"`
	IdempotencyKey string `gorm:"column:idempotency_key;size:192;not null;uniqueIndex"`
	TraceID        string `gorm:"column:trace_id;size:128;not null;index"`
	CausationID    string `gorm:"column:causation_id;size:64;not null"`
	Payload        string `gorm:"column:payload;type:text;not null"`
	Published      bool   `gorm:"column:published;not null;index"`
	CreatedAt      int64  `gorm:"column:created_at;not null;index"`
	PublishedAt    int64  `gorm:"column:published_at"`
}

func (Event) TableName() string { return "os_dso_event" }
