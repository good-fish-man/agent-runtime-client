package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

const (
	actionPolicyVersion = "dso-action-policy/v1"
	actionLeaseTTL      = 30 * time.Second
)

var ErrActionPolicyDenied = errors.New("governed action denied by policy")

type ActionDispatcher interface {
	Dispatch(context.Context, controlentity.Action) (*controlentity.Observation, error)
}

type ActionDispatcherFunc func(context.Context, controlentity.Action) (*controlentity.Observation, error)

func (f ActionDispatcherFunc) Dispatch(ctx context.Context, action controlentity.Action) (*controlentity.Observation, error) {
	return f(ctx, action)
}

type GovernedActionInput struct {
	OwnerID              string
	GoalID               string
	TaskStepID           string
	OutcomeID            string
	SubagentRunID        string
	SubagentAttemptID    string
	DecisionTurnID       string
	CapabilitySnapshotID string
	BudgetRef            string
	EnvironmentRef       string
	ActorDeviceID        string
	Action               controlentity.Action
	ReadResource         func(context.Context) (delegationentity.ResourceSnapshot, error)
}

type GovernedActionService struct {
	store      delegationrepo.ActionStore
	instanceID string
	now        func() time.Time
}

func NewGovernedActionService(store delegationrepo.ActionStore, instanceID string) *GovernedActionService {
	if strings.TrimSpace(instanceID) == "" {
		instanceID = "action-orchestrator-" + ulid.New()
	}
	return &GovernedActionService{store: store, instanceID: instanceID, now: func() time.Time { return time.Now().UTC() }}
}

func (s *GovernedActionService) Execute(ctx context.Context, input GovernedActionInput, dispatcher ActionDispatcher) (*controlentity.Observation, error) {
	if s == nil || s.store == nil || dispatcher == nil {
		return nil, fmt.Errorf("governed action service is not configured")
	}
	if strings.TrimSpace(input.OwnerID) == "" || strings.TrimSpace(input.TaskStepID) == "" {
		return nil, fmt.Errorf("governed action requires owner and task step")
	}
	if input.ReadResource == nil {
		return nil, fmt.Errorf("governed action requires a resource snapshot reader")
	}
	initial, err := input.ReadResource(ctx)
	if err != nil {
		return nil, log.WrapError(err, "GovernedAction.readInitialResource")
	}
	chain, typed, err := s.buildChain(ctx, input, initial)
	if err != nil {
		return nil, log.WrapError(err, "GovernedAction.buildChain")
	}
	if err := s.store.CreateActionChain(ctx, chain); err != nil {
		return nil, log.WrapError(err, "GovernedAction.persistChain")
	}

	if err := ctx.Err(); err != nil {
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, err, dso.ActionCancelled, dso.PlanRunCancelled, initial.ResourceVersion, false)
		return nil, err
	}
	if typed.Policy.Decision == dso.ActionPolicyDeny {
		if err := s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, ErrActionPolicyDenied, dso.ActionPolicyDenied, dso.PlanRunFailed, initial.ResourceVersion, true); err != nil {
			return nil, err
		}
		return nil, ErrActionPolicyDenied
	}
	if typed.Policy.Decision == dso.ActionPolicyRequireConfirmation {
		observation, dispatchErr := dispatcher.Dispatch(ctx, input.Action)
		attemptStatus, planStatus, terminal := actionOutcomeStatus(input.Action, observation, dispatchErr)
		attemptStatus, planStatus, terminal = dso.ActionWaitingApproval, dso.PlanRunWaiting, false
		if err := s.recordOutcome(context.WithoutCancel(ctx), chain, typed, observation, dispatchErr, attemptStatus, planStatus, resourceVersion(observation, initial.ResourceVersion), terminal); err != nil {
			return observation, err
		}
		return observation, dispatchErr
	}

	critical, err := input.ReadResource(ctx)
	if err != nil {
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, err, dso.ActionFailed, dso.PlanRunFailed, initial.ResourceVersion, true)
		return nil, log.WrapError(err, "GovernedAction.criticalRecheck")
	}
	if initial.ResourceRef != critical.ResourceRef || initial.ResourceVersion != critical.ResourceVersion {
		staleErr := fmt.Errorf("critical resource recheck failed: planned %s@%s, current %s@%s: %w", initial.ResourceRef, initial.ResourceVersion, critical.ResourceRef, critical.ResourceVersion, delegationrepo.ErrResourceStale)
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, staleErr, dso.ActionFailed, dso.PlanRunFailed, critical.ResourceVersion, true)
		return nil, staleErr
	}
	if expected := actionExpectedResourceVersion(input.Action); expected != "" && expected != critical.ResourceVersion {
		staleErr := fmt.Errorf("action expected resource version %s but current version is %s: %w", expected, critical.ResourceVersion, delegationrepo.ErrResourceStale)
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, staleErr, dso.ActionFailed, dso.PlanRunFailed, critical.ResourceVersion, true)
		return nil, staleErr
	}
	if err := ctx.Err(); err != nil {
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, err, dso.ActionCancelled, dso.PlanRunCancelled, critical.ResourceVersion, true)
		return nil, err
	}

	lease := delegationentity.ResourceLease{
		LeaseID: "resource-lease-" + stableActionSuffix(typed.Attempt.ActionAttemptID), OwnerID: input.OwnerID,
		RunID: firstNonEmpty(input.SubagentRunID, typed.PlanRun.PlanRunID), ResourceRef: critical.ResourceRef,
		ResourceVersion: critical.ResourceVersion, Mode: actionLeaseMode(input.Action),
		ActionAttemptID: typed.Attempt.ActionAttemptID, OwnerInstanceID: s.instanceID,
		Status: delegationentity.LeaseActive, AcquiredAt: s.now(), HeartbeatAt: s.now(),
		ExpiresAt: s.now().Add(actionLeaseTTL), Revision: 1,
	}
	leaseEvent := s.event(ctx, input.OwnerID, "action_attempt", typed.Attempt.ActionAttemptID, "ActionResourceLeased", 2, typed.Policy.PolicyDecisionID, lease)
	if err := s.store.AcquireActionLease(ctx, lease, initial.ResourceVersion, critical.ResourceVersion, s.now(), leaseEvent); err != nil {
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, err, dso.ActionFailed, dso.PlanRunFailed, critical.ResourceVersion, true)
		return nil, log.WrapError(err, "GovernedAction.acquireLease")
	}
	typed.Attempt.ResourceLeaseRef = lease.LeaseID
	chain.Attempt.ResourceLeaseID = lease.LeaseID

	if err := ctx.Err(); err != nil {
		_ = s.recordOutcome(context.WithoutCancel(ctx), chain, typed, nil, err, dso.ActionCancelled, dso.PlanRunCancelled, critical.ResourceVersion, true)
		return nil, err
	}
	observation, dispatchErr := dispatcher.Dispatch(ctx, input.Action)
	attemptStatus, planStatus, terminal := actionOutcomeStatus(input.Action, observation, dispatchErr)
	if err := s.recordOutcome(context.WithoutCancel(ctx), chain, typed, observation, dispatchErr, attemptStatus, planStatus, resourceVersion(observation, critical.ResourceVersion), terminal); err != nil {
		return observation, err
	}
	return observation, dispatchErr
}

type typedActionChain struct {
	Proposal dso.ActionProposal
	Plan     dso.PlanCandidate
	Context  dso.ExecutionContext
	Policy   dso.PolicyDecision
	PlanRun  dso.PlanRun
	Attempt  dso.GovernedActionAttempt
}

func (s *GovernedActionService) buildChain(ctx context.Context, input GovernedActionInput, resource delegationentity.ResourceSnapshot) (delegationentity.ActionChain, typedActionChain, error) {
	issuedAt := input.Action.IssuedAt.UTC()
	if issuedAt.IsZero() {
		issuedAt = s.now()
	}
	goalID := firstNonEmpty(input.GoalID, "goal://task/"+input.TaskStepID)
	outcomeID := firstNonEmpty(input.OutcomeID, "outcome://task/"+input.TaskStepID)
	decisionTurnID := firstNonEmpty(input.DecisionTurnID, input.Action.DecisionID, "decision://action/"+input.Action.ActionID)
	proposalInput := map[string]any{
		"owner_id": input.OwnerID, "goal_id": goalID, "task_step_id": input.TaskStepID,
		"action_id": input.Action.ActionID, "capability": input.Action.Capability, "operation": input.Action.Operation,
		"target": input.Action.Target, "arguments": input.Action.Arguments,
		"resource_ref": resource.ResourceRef, "resource_version": resource.ResourceVersion,
	}
	inputHash, err := dso.Hash(proposalInput)
	if err != nil {
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	suffix := stableActionSuffix(inputHash)
	proposal := dso.ActionProposal{
		ActionProposalID: "action-proposal-" + suffix, DecisionTurnRef: decisionTurnID,
		Capability: input.Action.Capability, Operation: input.Action.Operation,
		Arguments: cloneActionArguments(input.Action), ExpectedEffectIDs: actionExpectedEffects(input.Action), CreatedAt: issuedAt,
	}
	if err := proposal.Validate(); err != nil {
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	plan := dso.PlanCandidate{
		Schema: dso.Schema, PlanCandidateID: "plan-" + suffix, OwnerID: input.OwnerID,
		GoalRef: goalID, TaskStepRef: input.TaskStepID, OutcomeRef: outcomeID,
		ActionProposalRef: proposal.ActionProposalID,
		WorldReadSet:      []dso.WorldRead{{ResourceRef: resource.ResourceRef, ResourceVersion: resource.ResourceVersion}},
		ResourceRef:       resource.ResourceRef, ResourceVersion: resource.ResourceVersion,
		Steps:     []dso.PlanStep{{StepID: "plan-step-" + suffix, Ordinal: 1, ActionProposalRef: proposal.ActionProposalID, ExpectedEffectID: firstExpectedEffect(proposal.ExpectedEffectIDs)}},
		CreatedAt: issuedAt,
	}
	plan.DefinitionHash, err = dso.PlanCandidateDefinitionHash(plan)
	if err != nil || plan.Validate() != nil {
		if err == nil {
			err = plan.Validate()
		}
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	worldHash, err := dso.Hash(plan.WorldReadSet)
	if err != nil {
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	decision := actionPolicyDecision(input.Action)
	policy := dso.PolicyDecision{
		Schema: dso.Schema, PolicyDecisionID: "action-policy-" + suffix,
		PlanCandidateRef: plan.PlanCandidateID, ActionProposalRef: proposal.ActionProposalID,
		WorldReadSetHash: worldHash, PolicyVersion: actionPolicyVersion, InputHash: plan.DefinitionHash,
		Decision: decision, Reasons: []string{"bound_to_control_action_policy", "critical_resource_recheck_required"},
		DecidedAt: issuedAt, ExpiresAt: minActionPolicyExpiry(issuedAt.Add(time.Minute), input.Action.Deadline),
	}
	if err := policy.Validate(plan); err != nil {
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	executionContext := dso.ExecutionContext{
		ExecutionContextID:    "execution-context-" + suffix,
		WorldSnapshotRef:      fmt.Sprintf("world://task/%s@%d", input.TaskStepID, resource.TaskRevision),
		CapabilitySnapshotRef: firstNonEmpty(input.CapabilitySnapshotID, "capability://"+input.Action.Capability),
		PolicyVersion:         actionPolicyVersion, BudgetRef: input.BudgetRef,
		ActorBindings:  map[string]string{input.Action.Capability: firstNonEmpty(input.ActorDeviceID, input.Action.DeviceID)},
		EnvironmentRef: firstNonEmpty(input.EnvironmentRef, "desktop://"+firstNonEmpty(input.ActorDeviceID, input.Action.DeviceID)), CreatedAt: issuedAt,
	}
	executionContext.ContentHash, err = dso.ExecutionContextContentHash(executionContext)
	if err != nil || executionContext.Validate() != nil {
		if err == nil {
			err = executionContext.Validate()
		}
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	planRun := dso.PlanRun{
		PlanRunID: "plan-run-" + suffix, OwnerID: input.OwnerID, PlanCandidateRef: plan.PlanCandidateID,
		ExecutionContextRef: executionContext.ExecutionContextID, SubagentRunRef: input.SubagentRunID,
		SubagentAttemptRef: input.SubagentAttemptID, Status: dso.PlanRunRunning, Revision: 1, StartedAt: issuedAt,
	}
	if err := planRun.Validate(); err != nil {
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	actionStatus := dso.ActionPolicyAllowed
	if decision == dso.ActionPolicyDeny {
		actionStatus = dso.ActionReserved
	} else if decision == dso.ActionPolicyRequireConfirmation {
		actionStatus = dso.ActionWaitingApproval
	}
	attempt := dso.GovernedActionAttempt{
		ActionAttemptID: "action-attempt-" + suffix, PlanCandidateRef: plan.PlanCandidateID,
		PolicyDecisionRef: policy.PolicyDecisionID, PlanRunRef: planRun.PlanRunID,
		ActionProposalRef: proposal.ActionProposalID, ResourceVersionBefore: resource.ResourceVersion,
		Status: actionStatus, Revision: 1, CreatedAt: issuedAt, UpdatedAt: issuedAt,
	}
	if err := attempt.Validate(); err != nil {
		return delegationentity.ActionChain{}, typedActionChain{}, err
	}
	proposalJSON, _ := json.Marshal(proposal)
	planJSON, _ := json.Marshal(plan)
	contextJSON, _ := json.Marshal(executionContext)
	policyJSON, _ := json.Marshal(policy)
	planRunJSON, _ := json.Marshal(planRun)
	attemptJSON, _ := json.Marshal(attempt)
	chain := delegationentity.ActionChain{
		Proposal:         delegationentity.ActionProposalRecord{ActionProposalID: proposal.ActionProposalID, OwnerID: input.OwnerID, GoalID: goalID, TaskStepID: input.TaskStepID, DecisionTurnID: decisionTurnID, SubagentRunID: input.SubagentRunID, SubagentAttemptID: input.SubagentAttemptID, Capability: input.Action.Capability, Operation: input.Action.Operation, ResourceRef: resource.ResourceRef, ResourceVersion: resource.ResourceVersion, InputHash: inputHash, Content: string(proposalJSON), CreatedAt: issuedAt},
		Plan:             delegationentity.PlanCandidateRecord{PlanCandidateID: plan.PlanCandidateID, OwnerID: input.OwnerID, TaskStepID: input.TaskStepID, ActionProposalID: proposal.ActionProposalID, ResourceRef: resource.ResourceRef, ResourceVersion: resource.ResourceVersion, DefinitionHash: plan.DefinitionHash, Content: string(planJSON), CreatedAt: issuedAt},
		ExecutionContext: delegationentity.ExecutionContextRecord{ExecutionContextID: executionContext.ExecutionContextID, OwnerID: input.OwnerID, TaskStepID: input.TaskStepID, ContentHash: executionContext.ContentHash, Content: string(contextJSON), CreatedAt: issuedAt},
		Policy:           delegationentity.ActionPolicyDecisionRecord{PolicyDecisionID: policy.PolicyDecisionID, OwnerID: input.OwnerID, PlanCandidateID: plan.PlanCandidateID, ActionProposalID: proposal.ActionProposalID, WorldReadSetHash: worldHash, InputHash: policy.InputHash, PolicyVersion: policy.PolicyVersion, Decision: policy.Decision, Content: string(policyJSON), DecidedAt: policy.DecidedAt, ExpiresAt: policy.ExpiresAt},
		PlanRun:          delegationentity.ActionPlanRunRecord{PlanRunID: planRun.PlanRunID, OwnerID: input.OwnerID, PlanCandidateID: plan.PlanCandidateID, ExecutionContextID: executionContext.ExecutionContextID, SubagentRunID: input.SubagentRunID, SubagentAttemptID: input.SubagentAttemptID, Status: planRun.Status, Revision: 1, Content: string(planRunJSON), StartedAt: issuedAt},
		Attempt:          delegationentity.GovernedActionAttemptRecord{ActionAttemptID: attempt.ActionAttemptID, OwnerID: input.OwnerID, PlanRunID: planRun.PlanRunID, PlanCandidateID: plan.PlanCandidateID, PolicyDecisionID: policy.PolicyDecisionID, ActionProposalID: proposal.ActionProposalID, ResourceVersionBefore: resource.ResourceVersion, Status: attempt.Status, Revision: 1, Content: string(attemptJSON), CreatedAt: issuedAt, UpdatedAt: issuedAt},
	}
	chain.Event = s.event(ctx, input.OwnerID, "action_attempt", attempt.ActionAttemptID, "GovernedActionCreated", 1, proposal.ActionProposalID, map[string]any{"plan_candidate_id": plan.PlanCandidateID, "policy_decision_id": policy.PolicyDecisionID})
	return chain, typedActionChain{Proposal: proposal, Plan: plan, Context: executionContext, Policy: policy, PlanRun: planRun, Attempt: attempt}, nil
}

func (s *GovernedActionService) recordOutcome(ctx context.Context, chain delegationentity.ActionChain, typed typedActionChain, observation *controlentity.Observation, runErr error, attemptStatus, planStatus, versionAfter string, terminal bool) error {
	recordedAt := s.now()
	endedAt := time.Time{}
	if terminal {
		endedAt = recordedAt
	}
	typed.Attempt.Status = attemptStatus
	typed.Attempt.ObservationRef = observationID(observation)
	typed.Attempt.ResourceVersionAfter = versionAfter
	typed.Attempt.ErrorChain = errorChain(runErr)
	typed.Attempt.Revision++
	typed.Attempt.UpdatedAt = recordedAt
	typed.Attempt.EndedAt = endedAt
	typed.PlanRun.Status = planStatus
	typed.PlanRun.Revision++
	typed.PlanRun.EndedAt = endedAt
	attemptJSON, _ := json.Marshal(typed.Attempt)
	planJSON, _ := json.Marshal(typed.PlanRun)
	verification := buildActionVerification(chain, typed, observation, runErr, recordedAt)
	verificationJSON, _ := json.Marshal(verification)
	evidenceJSON, _ := json.Marshal(verification.EvidenceRefs)
	completion := delegationentity.ActionCompletion{
		OwnerID: chain.Attempt.OwnerID, PlanRunID: chain.PlanRun.PlanRunID, ActionAttemptID: chain.Attempt.ActionAttemptID,
		ResourceLeaseID: chain.Attempt.ResourceLeaseID, AttemptStatus: attemptStatus, PlanStatus: planStatus,
		ObservationID: observationID(observation), ResourceVersionAfter: versionAfter, ErrorChain: strings.Join(errorChain(runErr), "\n"),
		AttemptContent: string(attemptJSON), PlanContent: string(planJSON), RecordedAt: recordedAt, EndedAt: endedAt,
		Verification: delegationentity.ActionVerificationRecord{VerificationID: verification.VerificationID, OwnerID: chain.Attempt.OwnerID, OutcomeID: verification.OutcomeRef, PlanRunID: verification.PlanRunRef, ActionAttemptID: verification.ActionAttemptRef, EffectClauseID: verification.EffectClauseID, Status: verification.Status, Confidence: verification.Confidence, EvidenceRefs: string(evidenceJSON), Content: string(verificationJSON), VerifiedAt: verification.VerifiedAt},
	}
	completion.Event = s.event(ctx, chain.Attempt.OwnerID, "action_attempt", chain.Attempt.ActionAttemptID, "GovernedAction"+attemptStatus, 3, completion.ObservationID, map[string]any{"status": attemptStatus, "plan_status": planStatus, "observation_id": completion.ObservationID})
	return log.WrapError(s.store.CompleteActionChain(ctx, completion), "GovernedAction.recordOutcome")
}

func buildActionVerification(chain delegationentity.ActionChain, typed typedActionChain, observation *controlentity.Observation, runErr error, at time.Time) dso.VerificationResult {
	status, confidence := dso.VerificationUnknown, 0.25
	if observation != nil {
		switch observation.Status {
		case controlentity.ObservationSucceeded:
			status, confidence = dso.VerificationSatisfied, 1
		case controlentity.ObservationFailed, controlentity.ObservationBlocked, controlentity.ObservationCancelled, controlentity.ObservationExpired:
			status, confidence = dso.VerificationUnsatisfied, 1
		}
	}
	if runErr != nil && observation == nil {
		status, confidence = dso.VerificationUnknown, 0
	}
	refs := []string{}
	if id := observationID(observation); id != "" {
		refs = append(refs, id)
	}
	return dso.VerificationResult{
		VerificationID: "action-verification-" + stableActionSuffix(chain.Attempt.ActionAttemptID),
		OutcomeRef:     typed.Plan.OutcomeRef, PlanRunRef: typed.PlanRun.PlanRunID,
		ActionAttemptRef: typed.Attempt.ActionAttemptID, EffectClauseID: firstExpectedEffect(typed.Proposal.ExpectedEffectIDs),
		Status: status, ExpectedValue: true, ObservedValue: observationState(observation),
		EvidenceRefs: refs, Confidence: confidence, VerifiedAt: at,
	}
}

func actionOutcomeStatus(action controlentity.Action, observation *controlentity.Observation, err error) (string, string, bool) {
	if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && observation == nil {
		return dso.ActionCancelled, dso.PlanRunCancelled, true
	}
	if observation == nil {
		return dso.ActionUnknownOutcome, dso.PlanRunUnknown, true
	}
	switch observation.Status {
	case controlentity.ObservationSucceeded:
		return dso.ActionSucceeded, dso.PlanRunCompleted, true
	case controlentity.ObservationWaitingApproval:
		return dso.ActionWaitingApproval, dso.PlanRunWaiting, false
	case controlentity.ObservationWaitingUser:
		return dso.ActionWaitingApproval, dso.PlanRunWaiting, false
	case controlentity.ObservationCancelled:
		return dso.ActionCancelled, dso.PlanRunCancelled, true
	case controlentity.ObservationFailed, controlentity.ObservationBlocked, controlentity.ObservationExpired:
		return dso.ActionFailed, dso.PlanRunFailed, true
	default:
		if err != nil {
			return dso.ActionUnknownOutcome, dso.PlanRunUnknown, true
		}
		return dso.ActionUnknownOutcome, dso.PlanRunUnknown, true
	}
}

func actionPolicyDecision(action controlentity.Action) string {
	switch action.Policy.Decision {
	case controlentity.Block:
		return dso.ActionPolicyDeny
	case controlentity.AskUser:
		return dso.ActionPolicyRequireConfirmation
	default:
		return dso.ActionPolicyAllow
	}
}

func actionLeaseMode(action controlentity.Action) string {
	operation := strings.ToLower(strings.TrimSpace(action.Operation))
	switch operation {
	case "observe", "extract", "screenshot", "wait", "read", "status":
		return delegationentity.LeaseSharedRead
	default:
		return delegationentity.LeaseExclusiveWrite
	}
}

func cloneActionArguments(action controlentity.Action) map[string]any {
	result := make(map[string]any, len(action.Arguments)+2)
	for key, value := range action.Arguments {
		result[key] = value
	}
	if len(action.Target) > 0 {
		result["target"] = action.Target
	}
	result["control_action_id"] = action.ActionID
	return result
}

func actionExpectedEffects(action controlentity.Action) []string {
	if action.ExpectedObservation.Kind != "" {
		return []string{action.ExpectedObservation.Kind}
	}
	return []string{action.Capability + "." + firstNonEmpty(action.Operation, "completed")}
}

func firstExpectedEffect(values []string) string {
	if len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		return strings.TrimSpace(values[0])
	}
	return "action.effect_observed"
}

func actionExpectedResourceVersion(action controlentity.Action) string {
	for _, values := range []map[string]any{action.Arguments, action.Target} {
		if value, _ := values["expected_resource_version"].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func resourceVersion(observation *controlentity.Observation, fallback string) string {
	if observation != nil {
		if value, _ := observation.State["resource_version"].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func observationID(observation *controlentity.Observation) string {
	if observation == nil {
		return ""
	}
	return strings.TrimSpace(observation.ObservationID)
}

func observationState(observation *controlentity.Observation) any {
	if observation == nil {
		return nil
	}
	return map[string]any{"status": observation.Status, "state": observation.State, "error": observation.Error}
}

func errorChain(err error) []string {
	if err == nil {
		return nil
	}
	return strings.Split(log.FormatError(err), "\n")
}

func stableActionSuffix(value string) string {
	digest, _ := dso.Hash(strings.TrimSpace(value))
	if len(digest) > 26 {
		return digest[:26]
	}
	return digest
}

func minActionPolicyExpiry(preferred, deadline time.Time) time.Time {
	if !deadline.IsZero() && deadline.Before(preferred) {
		return deadline
	}
	return preferred
}

func (s *GovernedActionService) event(ctx context.Context, ownerID, aggregateType, aggregateID, eventType string, sequence int64, causationID string, payload any) delegationentity.Event {
	encoded, _ := json.Marshal(payload)
	traceID := log.ReqID(ctx)
	if traceID == "" {
		traceID = "dso-" + ulid.New()
	}
	return delegationentity.Event{
		EventID: "event-" + ulid.New(), OwnerID: ownerID, AggregateType: aggregateType, AggregateID: aggregateID,
		Sequence: sequence, Type: eventType, IdempotencyKey: fmt.Sprintf("%s:%s:%d", aggregateID, eventType, sequence),
		TraceID: traceID, CausationID: causationID, Payload: string(encoded), CreatedAt: s.now(),
	}
}
