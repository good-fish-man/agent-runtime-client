package delegation

type ActionProposal struct {
	ActionProposalID  string `gorm:"column:action_proposal_id;primaryKey;size:64"`
	OwnerID           string `gorm:"column:owner_id;size:64;not null;index"`
	GoalID            string `gorm:"column:goal_id;size:64;not null;index"`
	TaskStepID        string `gorm:"column:task_step_id;size:64;not null;index"`
	DecisionTurnID    string `gorm:"column:decision_turn_id;size:64;not null;index"`
	SubagentRunID     string `gorm:"column:subagent_run_id;size:64;index"`
	SubagentAttemptID string `gorm:"column:subagent_attempt_id;size:64;index"`
	Capability        string `gorm:"column:capability;size:128;not null;index"`
	Operation         string `gorm:"column:operation;size:64;not null"`
	ResourceRef       string `gorm:"column:resource_ref;size:192;not null;index"`
	ResourceVersion   string `gorm:"column:resource_version;size:128;not null"`
	InputHash         string `gorm:"column:input_hash;size:64;not null;index"`
	Content           string `gorm:"column:content;type:text;not null"`
	CreatedAt         int64  `gorm:"column:created_at;not null;index"`
}

func (ActionProposal) TableName() string { return "os_dso_action_proposal" }

type PlanCandidate struct {
	PlanCandidateID  string `gorm:"column:plan_candidate_id;primaryKey;size:64"`
	OwnerID          string `gorm:"column:owner_id;size:64;not null;index"`
	TaskStepID       string `gorm:"column:task_step_id;size:64;not null;index"`
	ActionProposalID string `gorm:"column:action_proposal_id;size:64;not null;uniqueIndex"`
	ResourceRef      string `gorm:"column:resource_ref;size:192;not null;index"`
	ResourceVersion  string `gorm:"column:resource_version;size:128;not null"`
	DefinitionHash   string `gorm:"column:definition_hash;size:64;not null;index"`
	Content          string `gorm:"column:content;type:text;not null"`
	CreatedAt        int64  `gorm:"column:created_at;not null;index"`
}

func (PlanCandidate) TableName() string { return "os_dso_plan_candidate" }

type ExecutionContext struct {
	ExecutionContextID string `gorm:"column:execution_context_id;primaryKey;size:64"`
	OwnerID            string `gorm:"column:owner_id;size:64;not null;index"`
	TaskStepID         string `gorm:"column:task_step_id;size:64;not null;index"`
	ContentHash        string `gorm:"column:content_hash;size:64;not null;index"`
	Content            string `gorm:"column:content;type:text;not null"`
	CreatedAt          int64  `gorm:"column:created_at;not null;index"`
}

func (ExecutionContext) TableName() string { return "os_dso_execution_context" }

type ActionPolicyDecision struct {
	PolicyDecisionID string `gorm:"column:policy_decision_id;primaryKey;size:64"`
	OwnerID          string `gorm:"column:owner_id;size:64;not null;index"`
	PlanCandidateID  string `gorm:"column:plan_candidate_id;size:64;not null;uniqueIndex"`
	ActionProposalID string `gorm:"column:action_proposal_id;size:64;not null;index"`
	WorldReadSetHash string `gorm:"column:world_read_set_hash;size:64;not null"`
	InputHash        string `gorm:"column:input_hash;size:64;not null"`
	PolicyVersion    string `gorm:"column:policy_version;size:64;not null"`
	Decision         string `gorm:"column:decision;size:32;not null;index"`
	Content          string `gorm:"column:content;type:text;not null"`
	DecidedAt        int64  `gorm:"column:decided_at;not null;index"`
	ExpiresAt        int64  `gorm:"column:expires_at;not null;index"`
}

func (ActionPolicyDecision) TableName() string { return "os_dso_action_policy_decision" }

type ActionPlanRun struct {
	PlanRunID          string `gorm:"column:plan_run_id;primaryKey;size:64"`
	OwnerID            string `gorm:"column:owner_id;size:64;not null;index"`
	PlanCandidateID    string `gorm:"column:plan_candidate_id;size:64;not null;index"`
	ExecutionContextID string `gorm:"column:execution_context_id;size:64;not null;index"`
	SubagentRunID      string `gorm:"column:subagent_run_id;size:64;index"`
	SubagentAttemptID  string `gorm:"column:subagent_attempt_id;size:64;index"`
	Status             string `gorm:"column:status;size:32;not null;index"`
	Revision           int64  `gorm:"column:revision;not null"`
	Content            string `gorm:"column:content;type:text;not null"`
	StartedAt          int64  `gorm:"column:started_at;not null;index"`
	EndedAt            int64  `gorm:"column:ended_at"`
}

func (ActionPlanRun) TableName() string { return "os_dso_action_plan_run" }

type GovernedActionAttempt struct {
	ActionAttemptID       string `gorm:"column:action_attempt_id;primaryKey;size:64"`
	OwnerID               string `gorm:"column:owner_id;size:64;not null;index"`
	PlanRunID             string `gorm:"column:plan_run_id;size:64;not null;index"`
	PlanCandidateID       string `gorm:"column:plan_candidate_id;size:64;not null;index"`
	PolicyDecisionID      string `gorm:"column:policy_decision_id;size:64;not null;index"`
	ActionProposalID      string `gorm:"column:action_proposal_id;size:64;not null;uniqueIndex"`
	ResourceLeaseID       string `gorm:"column:resource_lease_id;size:64;index"`
	ObservationID         string `gorm:"column:observation_id;size:64;index"`
	ResourceVersionBefore string `gorm:"column:resource_version_before;size:128;not null"`
	ResourceVersionAfter  string `gorm:"column:resource_version_after;size:128"`
	Status                string `gorm:"column:status;size:32;not null;index"`
	Revision              int64  `gorm:"column:revision;not null"`
	ErrorChain            string `gorm:"column:error_chain;type:text"`
	Content               string `gorm:"column:content;type:text;not null"`
	CreatedAt             int64  `gorm:"column:created_at;not null;index"`
	UpdatedAt             int64  `gorm:"column:updated_at;not null;index"`
	EndedAt               int64  `gorm:"column:ended_at"`
}

func (GovernedActionAttempt) TableName() string { return "os_dso_governed_action_attempt" }

type ActionVerification struct {
	VerificationID  string  `gorm:"column:verification_id;primaryKey;size:64"`
	OwnerID         string  `gorm:"column:owner_id;size:64;not null;index"`
	OutcomeID       string  `gorm:"column:outcome_id;size:64;not null;index"`
	PlanRunID       string  `gorm:"column:plan_run_id;size:64;not null;index"`
	ActionAttemptID string  `gorm:"column:action_attempt_id;size:64;not null;index"`
	EffectClauseID  string  `gorm:"column:effect_clause_id;size:128;not null;index"`
	Status          string  `gorm:"column:status;size:32;not null;index"`
	Confidence      float64 `gorm:"column:confidence;not null"`
	EvidenceRefs    string  `gorm:"column:evidence_refs;type:text;not null"`
	Content         string  `gorm:"column:content;type:text;not null"`
	VerifiedAt      int64   `gorm:"column:verified_at;not null;index"`
}

func (ActionVerification) TableName() string { return "os_dso_action_verification" }
