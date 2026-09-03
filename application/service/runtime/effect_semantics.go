package runtime

import (
	"fmt"
	"strings"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

func prepareSemanticAction(action *controlentity.Action, requestedDevice string) error {
	if action == nil {
		return nil
	}
	trace, err := semantics.TraceFromArguments(action.Arguments)
	if err != nil || trace == nil {
		return err
	}
	now := time.Now().UTC()
	readSet := map[string]any{
		"target_spec": trace.Outcome.TargetSpec,
		"session_id":  action.SessionID,
		"device_id":   strings.TrimSpace(requestedDevice),
		"capability":  action.Capability,
	}
	worldHash := semantics.Hash(readSet)
	decision := semanticPolicyDecision(action.Policy.Decision)
	trace.Policy = &semantics.PolicyDecision{
		DecisionID: semantics.NewID("policy-decision"), PlanRef: trace.Plan.PlanCandidateID,
		WorldReadSetHash: worldHash, PolicyVersion: "control-policy/v4", Decision: decision,
		Reasons: []string{semanticPolicyReason(action)}, DecidedAt: now, ExpiresAt: action.Deadline,
	}
	trace.Policy.InputHash = semantics.Hash(map[string]any{
		"plan_definition_hash": trace.Plan.DefinitionHash,
		"world_read_set_hash":  worldHash,
		"risk":                 action.Policy.Risk,
		"decision":             decision,
	})
	action.DecisionID = trace.Policy.DecisionID
	trace.Run = &semantics.PlanRun{
		PlanRunID: semantics.NewID("plan-run"), PlanRef: trace.Plan.PlanCandidateID, Status: semantics.RunRunning,
		Execution: semantics.ExecutionContext{
			WorldSnapshotRef: trace.Outcome.TargetSpec.SourceSnapshotRef,
			PolicyVersion:    trace.Policy.PolicyVersion,
			WorldReadSetHash: worldHash,
			ActorBindings:    map[string]string{"requested_device": strings.TrimSpace(requestedDevice)},
		},
		StartedAt: now,
	}
	trace.Attempts = append(trace.Attempts, semantics.ActionAttempt{
		AttemptID: semantics.NewID("action-attempt"), PlanRunRef: trace.Run.PlanRunID,
		PlanStepRef: semanticExecutionStep(trace.Plan), ActionID: action.ActionID, Attempt: 1,
		Status: semantics.AttemptRunning, StartedAt: now,
	})
	if err := trace.Validate(); err != nil {
		return err
	}
	return semantics.PutTrace(action.Arguments, trace)
}

func bindSemanticActor(action *controlentity.Action, deviceID, capabilityInstanceID string) error {
	if action == nil {
		return nil
	}
	trace, err := semantics.TraceFromArguments(action.Arguments)
	if err != nil || trace == nil || trace.Run == nil {
		return err
	}
	if trace.Run.Execution.ActorBindings == nil {
		trace.Run.Execution.ActorBindings = make(map[string]string)
	}
	trace.Run.Execution.ActorBindings["device"] = strings.TrimSpace(deviceID)
	trace.Run.Execution.ActorBindings["capability_instance"] = strings.TrimSpace(capabilityInstanceID)
	trace.Run.Execution.CapabilitySnapshotRef = strings.TrimSpace(capabilityInstanceID)
	trace.Run.Execution.EnvironmentRef = "device:" + strings.TrimSpace(deviceID)
	if err := trace.Validate(); err != nil {
		return err
	}
	return semantics.PutTrace(action.Arguments, trace)
}

func ensureSemanticObservation(action *controlentity.Action, observation *controlentity.Observation) error {
	if action == nil || observation == nil {
		return nil
	}
	if observation.State == nil {
		observation.State = make(map[string]any)
	}
	trace, err := semantics.TraceFromMap(observation.State[semantics.StateKey])
	if err != nil {
		return err
	}
	deviceTrace := trace != nil
	if trace == nil {
		trace, err = semantics.TraceFromArguments(action.Arguments)
		if err != nil || trace == nil {
			return err
		}
	}
	now := observation.ObservedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for index := range trace.Attempts {
		attempt := &trace.Attempts[index]
		if attempt.ActionID != action.ActionID {
			continue
		}
		attempt.ObservationRef = observation.ObservationID
		if observationError := strings.TrimSpace(observation.Error); observationError != "" && attempt.Error == "" {
			attempt.Error = observationError
		}
		if deviceTrace && attempt.Status != "" && attempt.Status != semantics.AttemptRunning {
			continue
		}
		switch observation.Status {
		case controlentity.ObservationSucceeded:
			attempt.Status = semantics.AttemptSucceeded
			attempt.FinishedAt = now
		case controlentity.ObservationCancelled:
			attempt.Status = semantics.AttemptCancelled
			attempt.FinishedAt = now
		case controlentity.ObservationWaitingApproval, controlentity.ObservationWaitingUser:
			attempt.Status = semantics.AttemptPending
			attempt.FinishedAt = time.Time{}
		default:
			attempt.Status = semantics.AttemptFailed
			attempt.FinishedAt = now
		}
	}
	if trace.VerificationSummary == nil {
		trace.VerificationResults = fallbackVerificationResults(trace, observation, now)
		summary := semantics.Aggregate(trace.Outcome.OutcomeID, semanticPlanRunID(trace), trace.VerificationResults, now)
		trace.VerificationSummary = &summary
	}
	if trace.Run != nil {
		if observation.Status == controlentity.ObservationCancelled {
			trace.Run.Status = semantics.RunCancelled
			trace.Run.TerminalReason = semanticCancellationReason(observation)
		} else {
			trace.Run.Status = semantics.TerminalRunStatus(trace.VerificationSummary)
			trace.Run.TerminalReason = semanticTerminalReason(trace.VerificationSummary)
		}
		if trace.Run.Status != semantics.RunRunning && trace.Run.Status != semantics.RunPending {
			trace.Run.FinishedAt = now
		}
	}
	if err := trace.Validate(); err != nil {
		return err
	}
	value, err := semantics.ToMap(trace)
	if err != nil {
		return err
	}
	observation.State[semantics.StateKey] = value
	return nil
}

func fallbackVerificationResults(trace *semantics.SemanticTrace, observation *controlentity.Observation, now time.Time) []semantics.VerificationResult {
	clauses := semanticClauses(trace.Outcome)
	results := make([]semantics.VerificationResult, 0, len(clauses))
	cancelled := observation.Status == controlentity.ObservationCancelled
	executionFailed := observation.Status != controlentity.ObservationSucceeded &&
		observation.Status != controlentity.ObservationWaitingApproval && observation.Status != controlentity.ObservationWaitingUser && !cancelled
	for _, clause := range clauses {
		status, reason := semantics.VerificationUnknown, "device returned no effect-specific verification"
		if cancelled {
			reason = "execution was cancelled before the effect could be verified"
		} else if executionFailed {
			if clause.Kind == semantics.EffectDesired {
				status, reason = semantics.VerificationUnsatisfied, "device action did not succeed"
			} else {
				reason = "constraint state is unknown because device execution did not finish"
			}
		} else if observation.Status == controlentity.ObservationWaitingApproval || observation.Status == controlentity.ObservationWaitingUser {
			reason = "effect verification is waiting for approval or user input"
		}
		results = append(results, semantics.VerificationResult{
			VerificationID: semantics.NewID("verification"), OutcomeRef: trace.Outcome.OutcomeID,
			PlanRunRef: semanticPlanRunID(trace), EffectClauseID: clause.ClauseID,
			Status: status, ExpectedValue: clause.Expected, Confidence: 0, Reason: reason, VerifiedAt: now,
		})
	}
	return results
}

func semanticClauses(outcome semantics.OutcomeSpec) []semantics.EffectClause {
	result := append([]semantics.EffectClause(nil), outcome.DesiredEffects...)
	result = append(result, outcome.MustPreserve...)
	result = append(result, outcome.ForbiddenEffects...)
	return result
}

func semanticExecutionStep(plan semantics.PlanCandidate) string {
	for _, step := range plan.Steps {
		if len(step.ExpectedEffectIDs) > 0 && step.Operation != "verify" {
			return step.StepID
		}
	}
	if len(plan.Steps) > 0 {
		return plan.Steps[0].StepID
	}
	return ""
}

func semanticPlanRunID(trace *semantics.SemanticTrace) string {
	if trace != nil && trace.Run != nil {
		return trace.Run.PlanRunID
	}
	return ""
}

func semanticPolicyDecision(decision string) string {
	switch decision {
	case controlentity.Allow:
		return semantics.PolicyAllow
	case controlentity.AskUser:
		return semantics.PolicyRequireConfirmation
	default:
		return semantics.PolicyDeny
	}
}

func semanticPolicyReason(action *controlentity.Action) string {
	if action == nil {
		return "control policy evaluated"
	}
	if reason := strings.TrimSpace(action.Policy.Reason); reason != "" {
		return reason
	}
	return fmt.Sprintf("%s action evaluated at risk %s", action.Capability, action.Policy.Risk)
}

func semanticTerminalReason(summary *semantics.OutcomeVerificationSummary) string {
	if summary == nil {
		return "effect verification is unavailable"
	}
	return fmt.Sprintf("effect verification %s: %d satisfied, %d unsatisfied, %d unknown, %d conflicting",
		summary.Status, summary.Satisfied, summary.Unsatisfied, summary.Unknown, summary.Conflicting)
}

func semanticCancellationReason(observation *controlentity.Observation) string {
	if observation != nil {
		if reason := strings.TrimSpace(observation.Error); reason != "" {
			return "execution cancelled: " + reason
		}
	}
	return "execution cancelled before effect verification completed"
}

func semanticMetadata(action *controlentity.Action) map[string]any {
	if action == nil || action.Arguments == nil {
		return nil
	}
	value, _ := action.Arguments[semantics.MetadataKey].(map[string]any)
	return value
}

func semanticTaskTerminal(values map[string]any) (status, reason string, ok bool) {
	observation, exists := values["latest_action_observation"].(map[string]any)
	if !exists {
		return "", "", false
	}
	state, exists := observation["state"].(map[string]any)
	if !exists {
		return "", "", false
	}
	trace, err := semantics.TraceFromMap(state[semantics.StateKey])
	if err != nil || trace == nil || trace.VerificationSummary == nil {
		return "", "", false
	}
	if trace.Run != nil && trace.Run.Status == semantics.RunCancelled {
		return controlentity.TaskStatusCancelled, trace.Run.TerminalReason, true
	}
	summary := trace.VerificationSummary
	switch summary.Status {
	case semantics.OutcomeSucceeded:
		return controlentity.StatusCompleted, semanticTerminalReason(summary), true
	case semantics.OutcomeFailed, semantics.OutcomeConflicting:
		return controlentity.StatusFailed, semanticTerminalReason(summary), true
	default:
		return controlentity.TaskStatusPaused, semanticTerminalReason(summary), true
	}
}
