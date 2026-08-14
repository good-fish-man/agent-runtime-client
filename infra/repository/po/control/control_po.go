package control

type Device struct {
	DeviceID            string `gorm:"column:device_id;primaryKey;type:varchar(128)"`
	UserID              string `gorm:"column:user_id;type:varchar(128);index"`
	Protocol            string `gorm:"column:protocol;type:varchar(32);not null"`
	Name                string `gorm:"column:name;type:varchar(255)"`
	Platform            string `gorm:"column:platform;type:varchar(64)"`
	Architecture        string `gorm:"column:architecture;type:varchar(64)"`
	Capabilities        string `gorm:"column:capabilities;type:text"`
	CapabilityInstances string `gorm:"column:capability_instances;type:text"`
	Online              bool   `gorm:"column:online;not null;default:false;index"`
	LeaseOwner          string `gorm:"column:lease_owner;type:varchar(160);index"`
	FencingToken        uint64 `gorm:"column:fencing_token;type:bigint;not null;default:0"`
	Revision            int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	ConnectedAt         int64  `gorm:"column:connected_at;type:bigint;default:0"`
	LastSeenAt          int64  `gorm:"column:last_seen_at;type:bigint;default:0;index"`
	LeaseExpiresAt      int64  `gorm:"column:lease_expires_at;type:bigint;default:0;index"`
	CreatedAt           int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
	UpdatedAt           int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint"`
}

func (*Device) TableName() string { return "os_device" }

type CapabilityDefinition struct {
	CapabilityID string `gorm:"column:capability_id;primaryKey;type:varchar(160)"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;default:system;index"`
	Version      string `gorm:"column:version;type:varchar(64);not null"`
	Description  string `gorm:"column:description;type:text"`
	Operations   string `gorm:"column:operations;type:text;not null"`
	Modalities   string `gorm:"column:modalities;type:text;not null"`
	InputSchema  string `gorm:"column:input_schema;type:text;not null"`
	OutputSchema string `gorm:"column:output_schema;type:text;not null"`
	Risk         string `gorm:"column:risk;type:varchar(16);not null"`
	Enabled      bool   `gorm:"column:enabled;not null;default:true;index"`
	Revision     int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID      string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt    int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*CapabilityDefinition) TableName() string { return "os_capability_definition" }

type CapabilityInstance struct {
	InstanceID   string `gorm:"column:instance_id;primaryKey;type:varchar(160)"`
	CapabilityID string `gorm:"column:capability_id;type:varchar(160);not null;index"`
	DeviceID     string `gorm:"column:device_id;type:varchar(128);not null;index"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Version      string `gorm:"column:version;type:varchar(64)"`
	Operations   string `gorm:"column:operations;type:text;not null"`
	Modalities   string `gorm:"column:modalities;type:text;not null"`
	Metadata     string `gorm:"column:metadata;type:text;not null"`
	Online       bool   `gorm:"column:online;not null;default:false;index"`
	Revision     int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID      string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt    int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*CapabilityInstance) TableName() string { return "os_capability_instance" }

type DeviceCapability struct {
	DeviceID     string `gorm:"column:device_id;primaryKey;type:varchar(128)"`
	InstanceID   string `gorm:"column:instance_id;primaryKey;type:varchar(160)"`
	CapabilityID string `gorm:"column:capability_id;type:varchar(160);not null;index"`
	OwnerID      string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Revision     int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID      string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt    int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt    int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*DeviceCapability) TableName() string { return "os_device_capability" }

type Task struct {
	TaskID         string `gorm:"column:task_id;primaryKey;type:varchar(128)"`
	ParentTaskID   string `gorm:"column:parent_task_id;type:varchar(128);index"`
	ConversationID string `gorm:"column:conversation_id;type:varchar(128);index"`
	TraceID        string `gorm:"column:trace_id;type:varchar(128);index"`
	UserID         string `gorm:"column:user_id;type:varchar(128);not null;index:idx_os_task_owner_status"`
	DeviceID       string `gorm:"column:device_id;type:varchar(128);index"`
	Goal           string `gorm:"column:goal;type:text"`
	Status         string `gorm:"column:status;type:varchar(32);not null;index:idx_os_task_owner_status"`
	Sequence       int64  `gorm:"column:sequence;type:bigint;not null;default:0"`
	Revision       int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	CurrentStepID  string `gorm:"column:current_step_id;type:varchar(128);index"`
	ActiveSessions string `gorm:"column:active_sessions;type:text"`
	Metadata       string `gorm:"column:metadata;type:text"`
	Result         string `gorm:"column:result;type:text"`
	ErrorDetail    string `gorm:"column:error_detail;type:text"`
	CreatedAt      int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt      int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*Task) TableName() string { return "os_task" }

type Step struct {
	StepID              string `gorm:"column:step_id;primaryKey;type:varchar(128)"`
	TaskID              string `gorm:"column:task_id;type:varchar(128);not null;uniqueIndex:idx_os_task_step_ordinal,priority:1;index"`
	Ordinal             int    `gorm:"column:ordinal;not null;uniqueIndex:idx_os_task_step_ordinal,priority:2"`
	Status              string `gorm:"column:status;type:varchar(32);not null;index"`
	Title               string `gorm:"column:title;type:varchar(512)"`
	Capability          string `gorm:"column:capability;type:varchar(160);index"`
	Operation           string `gorm:"column:operation;type:varchar(128)"`
	Target              string `gorm:"column:target;type:text"`
	Input               string `gorm:"column:input;type:text"`
	ExpectedObservation string `gorm:"column:expected_observation;type:text"`
	RetryPolicy         string `gorm:"column:retry_policy;type:text"`
	Attempt             int    `gorm:"column:attempt;not null;default:0"`
	CreatedAt           int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt           int64  `gorm:"column:updated_at;type:bigint;not null"`
}

func (*Step) TableName() string { return "os_task_step" }

type Action struct {
	ActionID             string `gorm:"column:action_id;primaryKey;type:varchar(128)"`
	TaskID               string `gorm:"column:task_id;type:varchar(128);not null;index:idx_os_action_task_sequence,priority:1"`
	StepID               string `gorm:"column:step_id;type:varchar(128);not null;index"`
	TraceID              string `gorm:"column:trace_id;type:varchar(128);index"`
	AgentBuildID         string `gorm:"column:agent_build_id;type:varchar(128);index"`
	RunManifestID        string `gorm:"column:run_manifest_id;type:varchar(128);index"`
	DecisionID           string `gorm:"column:decision_id;type:varchar(128);index"`
	DeviceID             string `gorm:"column:device_id;type:varchar(128);not null;index"`
	CapabilityInstanceID string `gorm:"column:capability_instance_id;type:varchar(160);not null;index"`
	UserID               string `gorm:"column:user_id;type:varchar(128);not null;index"`
	SessionID            string `gorm:"column:session_id;type:varchar(128)"`
	Protocol             string `gorm:"column:protocol;type:varchar(32);not null"`
	Sequence             int64  `gorm:"column:sequence;type:bigint;not null;index:idx_os_action_task_sequence,priority:2"`
	Revision             int64  `gorm:"column:revision;type:bigint;not null"`
	IdempotencyKey       string `gorm:"column:idempotency_key;type:varchar(255);not null;uniqueIndex"`
	IssuedAt             int64  `gorm:"column:issued_at;type:bigint;not null"`
	Deadline             int64  `gorm:"column:deadline;type:bigint;not null"`
	Capability           string `gorm:"column:capability;type:varchar(160);not null;index"`
	Operation            string `gorm:"column:operation;type:varchar(128)"`
	Target               string `gorm:"column:target;type:text"`
	Arguments            string `gorm:"column:arguments;type:text"`
	Risk                 string `gorm:"column:risk;type:varchar(16);not null"`
	Decision             string `gorm:"column:decision;type:varchar(24);not null"`
	Policy               string `gorm:"column:policy;type:text"`
	ExpectedObservation  string `gorm:"column:expected_observation;type:text"`
	CreatedAt            int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
}

func (*Action) TableName() string { return "os_action" }

type Approval struct {
	ApprovalID string `gorm:"column:approval_id;primaryKey;type:varchar(128)"`
	TaskID     string `gorm:"column:task_id;type:varchar(128);not null;index"`
	StepID     string `gorm:"column:step_id;type:varchar(128);not null;index"`
	ActionID   string `gorm:"column:action_id;type:varchar(128);not null;uniqueIndex"`
	OwnerID    string `gorm:"column:owner_id;type:varchar(128);not null;index:idx_os_approval_owner_status,priority:1"`
	Risk       string `gorm:"column:risk;type:varchar(16);not null"`
	Status     string `gorm:"column:status;type:varchar(24);not null;index:idx_os_approval_owner_status,priority:2"`
	Summary    string `gorm:"column:summary;type:text"`
	Scope      string `gorm:"column:scope;type:text;not null"`
	Revision   int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID    string `gorm:"column:trace_id;type:varchar(128);index"`
	DecidedBy  string `gorm:"column:decided_by;type:varchar(128);index"`
	Reason     string `gorm:"column:reason;type:text"`
	CreatedAt  int64  `gorm:"column:created_at;type:bigint;not null;index"`
	UpdatedAt  int64  `gorm:"column:updated_at;type:bigint;not null;index"`
	ExpiresAt  int64  `gorm:"column:expires_at;type:bigint;not null;index"`
	DecidedAt  int64  `gorm:"column:decided_at;type:bigint"`
}

func (*Approval) TableName() string { return "os_approval" }

type Artifact struct {
	ArtifactID string `gorm:"column:artifact_id;primaryKey;type:varchar(128)"`
	TaskID     string `gorm:"column:task_id;type:varchar(128);not null;index"`
	StepID     string `gorm:"column:step_id;type:varchar(128);index"`
	OwnerID    string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Kind       string `gorm:"column:kind;type:varchar(64);not null;index"`
	URI        string `gorm:"column:uri;type:text;not null"`
	MIMEType   string `gorm:"column:mime_type;type:varchar(128)"`
	Size       int64  `gorm:"column:size;type:bigint;not null;default:0"`
	SHA256     string `gorm:"column:sha256;type:varchar(64);index"`
	Metadata   string `gorm:"column:metadata;type:text;not null"`
	Revision   int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID    string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt  int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt  int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*Artifact) TableName() string { return "os_artifact" }

type Observation struct {
	ObservationID  string `gorm:"column:observation_id;primaryKey;type:varchar(128)"`
	ActionID       string `gorm:"column:action_id;type:varchar(128);not null;uniqueIndex"`
	TaskID         string `gorm:"column:task_id;type:varchar(128);not null;index:idx_os_observation_task_sequence,priority:1"`
	StepID         string `gorm:"column:step_id;type:varchar(128);not null;index"`
	OwnerID        string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	TraceID        string `gorm:"column:trace_id;type:varchar(128);index"`
	AgentBuildID   string `gorm:"column:agent_build_id;type:varchar(128);index"`
	RunManifestID  string `gorm:"column:run_manifest_id;type:varchar(128);index"`
	DeviceID       string `gorm:"column:device_id;type:varchar(128);index"`
	IdempotencyKey string `gorm:"column:idempotency_key;type:varchar(255);not null;uniqueIndex"`
	SessionID      string `gorm:"column:session_id;type:varchar(128)"`
	Protocol       string `gorm:"column:protocol;type:varchar(32);not null"`
	Sequence       int64  `gorm:"column:sequence;type:bigint;not null;index:idx_os_observation_task_sequence,priority:2"`
	Revision       int64  `gorm:"column:revision;type:bigint;not null"`
	Status         string `gorm:"column:status;type:varchar(32);not null;index"`
	StartedAt      int64  `gorm:"column:started_at;type:bigint"`
	FinishedAt     int64  `gorm:"column:finished_at;type:bigint"`
	ObservedAt     int64  `gorm:"column:observed_at;type:bigint;not null"`
	Summary        string `gorm:"column:summary;type:text"`
	State          string `gorm:"column:state;type:text"`
	Evidence       string `gorm:"column:evidence;type:text"`
	Attachments    string `gorm:"column:attachments;type:text"`
	WorldPatch     string `gorm:"column:world_patch;type:text"`
	Error          string `gorm:"column:error;type:text"`
	ErrorDetail    string `gorm:"column:error_detail;type:text"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
	UpdatedAt      int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint"`
}

func (*Observation) TableName() string { return "os_observation" }

type Event struct {
	EventID     string `gorm:"column:event_id;primaryKey;type:varchar(128)"`
	Protocol    string `gorm:"column:protocol;type:varchar(32);not null"`
	Type        string `gorm:"column:type;type:varchar(128);not null;index"`
	Aggregate   string `gorm:"column:aggregate;type:varchar(32);not null;index:idx_os_task_event_aggregate_sequence,priority:1"`
	AggregateID string `gorm:"column:aggregate_id;type:varchar(128);not null;index:idx_os_task_event_aggregate_sequence,priority:2"`
	TaskID      string `gorm:"column:task_id;type:varchar(128);uniqueIndex:idx_os_task_event_task_sequence,priority:1"`
	StepID      string `gorm:"column:step_id;type:varchar(128);index"`
	ActionID    string `gorm:"column:action_id;type:varchar(128);index"`
	TraceID     string `gorm:"column:trace_id;type:varchar(128);index"`
	Sequence    int64  `gorm:"column:sequence;type:bigint;not null;uniqueIndex:idx_os_task_event_task_sequence,priority:2;index:idx_os_task_event_aggregate_sequence,priority:3"`
	Revision    int64  `gorm:"column:revision;type:bigint;not null"`
	OccurredAt  int64  `gorm:"column:occurred_at;type:bigint;not null;index"`
	Payload     string `gorm:"column:payload;type:text"`
}

func (*Event) TableName() string { return "os_task_event" }

type Outbox struct {
	OutboxID    string `gorm:"column:outbox_id;primaryKey;type:varchar(128)"`
	EventID     string `gorm:"column:event_id;type:varchar(128);not null;uniqueIndex"`
	Topic       string `gorm:"column:topic;type:varchar(128);not null;index"`
	AggregateID string `gorm:"column:aggregate_id;type:varchar(128);not null;index"`
	Payload     string `gorm:"column:payload;type:text;not null"`
	Status      string `gorm:"column:status;type:varchar(24);not null;index"`
	Attempts    int    `gorm:"column:attempts;not null;default:0"`
	AvailableAt int64  `gorm:"column:available_at;type:bigint;not null;index"`
	PublishedAt int64  `gorm:"column:published_at;type:bigint"`
	LastError   string `gorm:"column:last_error;type:text"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
	UpdatedAt   int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint"`
}

func (*Outbox) TableName() string { return "os_outbox" }

type WorldState struct {
	TaskID    string `gorm:"column:task_id;primaryKey;type:varchar(128)"`
	Revision  int64  `gorm:"column:revision;type:bigint;not null;default:0"`
	State     string `gorm:"column:state;type:text;not null"`
	CreatedAt int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*WorldState) TableName() string { return "os_world_state" }

type WorldEntity struct {
	EntityID   string  `gorm:"column:entity_id;primaryKey;type:varchar(128)"`
	OwnerID    string  `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Scope      string  `gorm:"column:scope;type:varchar(128);not null;index"`
	Kind       string  `gorm:"column:kind;type:varchar(128);not null;index"`
	Properties string  `gorm:"column:properties;type:text;not null"`
	Confidence float64 `gorm:"column:confidence;type:decimal(6,5);not null;default:1"`
	ObservedAt int64   `gorm:"column:observed_at;type:bigint;not null;index"`
	ExpiresAt  int64   `gorm:"column:expires_at;type:bigint;index"`
	Revision   int64   `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID    string  `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt  int64   `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt  int64   `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*WorldEntity) TableName() string { return "os_world_entity" }

type WorldRelation struct {
	RelationID string  `gorm:"column:relation_id;primaryKey;type:varchar(128)"`
	OwnerID    string  `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Scope      string  `gorm:"column:scope;type:varchar(128);not null;index"`
	FromID     string  `gorm:"column:from_id;type:varchar(128);not null;index"`
	ToID       string  `gorm:"column:to_id;type:varchar(128);not null;index"`
	Kind       string  `gorm:"column:kind;type:varchar(128);not null;index"`
	Properties string  `gorm:"column:properties;type:text;not null"`
	Confidence float64 `gorm:"column:confidence;type:decimal(6,5);not null;default:1"`
	ObservedAt int64   `gorm:"column:observed_at;type:bigint;not null;index"`
	ExpiresAt  int64   `gorm:"column:expires_at;type:bigint;index"`
	Revision   int64   `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID    string  `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt  int64   `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt  int64   `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*WorldRelation) TableName() string { return "os_world_relation" }

type WorldEvidenceRef struct {
	EvidenceID string `gorm:"column:evidence_id;primaryKey;type:varchar(128)"`
	OwnerID    string `gorm:"column:owner_id;type:varchar(128);not null;index"`
	Scope      string `gorm:"column:scope;type:varchar(128);not null;index"`
	EntityID   string `gorm:"column:entity_id;type:varchar(128);index"`
	RelationID string `gorm:"column:relation_id;type:varchar(128);index"`
	TaskID     string `gorm:"column:task_id;type:varchar(128);index"`
	ActionID   string `gorm:"column:action_id;type:varchar(128);index"`
	Kind       string `gorm:"column:kind;type:varchar(64);not null"`
	URI        string `gorm:"column:uri;type:text"`
	SHA256     string `gorm:"column:sha256;type:varchar(64);index"`
	Metadata   string `gorm:"column:metadata;type:text;not null"`
	Revision   int64  `gorm:"column:revision;type:bigint;not null;default:1"`
	TraceID    string `gorm:"column:trace_id;type:varchar(128);index"`
	CreatedAt  int64  `gorm:"column:created_at;type:bigint;not null"`
	UpdatedAt  int64  `gorm:"column:updated_at;type:bigint;not null;index"`
}

func (*WorldEvidenceRef) TableName() string { return "os_world_evidence_ref" }
