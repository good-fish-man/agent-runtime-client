package runtime

import (
	"strings"
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

func TestPrepareSemanticActionCreatesGovernedRunAndBindsActor(t *testing.T) {
	action := testSemanticAction(t)
	if err := prepareSemanticAction(action, "device-requested"); err != nil {
		t.Fatal(err)
	}
	trace := mustActionTrace(t, action)
	if trace.Policy == nil || trace.Policy.Decision != semantics.PolicyAllow {
		t.Fatalf("policy decision was not recorded: %#v", trace.Policy)
	}
	if trace.Policy.WorldReadSetHash == "" || trace.Policy.InputHash == "" {
		t.Fatalf("policy decision is not bound to its inputs: %#v", trace.Policy)
	}
	if !trace.Policy.ValidFor(trace.Policy.WorldReadSetHash, action.Deadline.Add(-time.Second)) {
		t.Fatal("fresh policy decision was unexpectedly invalid")
	}
	if trace.Run == nil || trace.Run.Status != semantics.RunRunning || len(trace.Attempts) != 1 {
		t.Fatalf("execution run was not created: run=%#v attempts=%#v", trace.Run, trace.Attempts)
	}
	if trace.Attempts[0].ActionID != action.ActionID || trace.Attempts[0].Status != semantics.AttemptRunning {
		t.Fatalf("action attempt was not linked to the action: %#v", trace.Attempts[0])
	}
	if err := bindSemanticActor(action, "device-resolved", "browser-instance-1"); err != nil {
		t.Fatal(err)
	}
	trace = mustActionTrace(t, action)
	bindings := trace.Run.Execution.ActorBindings
	if bindings["requested_device"] != "device-requested" || bindings["device"] != "device-resolved" {
		t.Fatalf("device grounding was not retained: %#v", bindings)
	}
	if bindings["capability_instance"] != "browser-instance-1" {
		t.Fatalf("capability actor was not bound: %#v", bindings)
	}
}

func TestEnsureSemanticObservationDoesNotTreatTransportSuccessAsGoalSuccess(t *testing.T) {
	action := testSemanticAction(t)
	if err := prepareSemanticAction(action, "device-1"); err != nil {
		t.Fatal(err)
	}
	observation := testSemanticObservation(action, controlentity.ObservationSucceeded)
	if err := ensureSemanticObservation(action, observation); err != nil {
		t.Fatal(err)
	}
	trace := mustObservationTrace(t, observation)
	if trace.VerificationSummary == nil || trace.VerificationSummary.Status != semantics.OutcomeUnknown {
		t.Fatalf("transport success was promoted to goal success: %#v", trace.VerificationSummary)
	}
	if trace.Run == nil || trace.Run.Status != semantics.RunPaused {
		t.Fatalf("unknown outcome did not pause the run: %#v", trace.Run)
	}
	if len(trace.Attempts) != 1 || trace.Attempts[0].Status != semantics.AttemptSucceeded {
		t.Fatalf("transport attempt status was not preserved independently: %#v", trace.Attempts)
	}
	values := map[string]any{"latest_action_observation": map[string]any{"state": observation.State}}
	status, reason, ok := semanticTaskTerminal(values)
	if !ok || status != controlentity.TaskStatusPaused || !strings.Contains(reason, "unknown") {
		t.Fatalf("unknown verification mapped to %q (%q), semantic=%v", status, reason, ok)
	}
}

func TestEnsureSemanticObservationMapsDeviceFailureToFailedOutcome(t *testing.T) {
	action := testSemanticAction(t)
	if err := prepareSemanticAction(action, "device-1"); err != nil {
		t.Fatal(err)
	}
	observation := testSemanticObservation(action, controlentity.ObservationFailed)
	observation.Error = "browser action failed"
	if err := ensureSemanticObservation(action, observation); err != nil {
		t.Fatal(err)
	}
	trace := mustObservationTrace(t, observation)
	if trace.VerificationSummary == nil || trace.VerificationSummary.Status != semantics.OutcomeFailed {
		t.Fatalf("device failure did not fail the desired effect: %#v", trace.VerificationSummary)
	}
	if trace.Run == nil || trace.Run.Status != semantics.RunFailed {
		t.Fatalf("failed outcome did not fail the run: %#v", trace.Run)
	}
	if trace.Attempts[0].Status != semantics.AttemptFailed || trace.Attempts[0].Error == "" {
		t.Fatalf("failed attempt did not retain its error: %#v", trace.Attempts[0])
	}
}

func TestEnsureSemanticObservationMapsCancellationToCancelledRun(t *testing.T) {
	action := testSemanticAction(t)
	if err := prepareSemanticAction(action, "device-1"); err != nil {
		t.Fatal(err)
	}
	observation := testSemanticObservation(action, controlentity.ObservationCancelled)
	observation.Error = "user requested stop"
	if err := ensureSemanticObservation(action, observation); err != nil {
		t.Fatal(err)
	}
	trace := mustObservationTrace(t, observation)
	if trace.VerificationSummary == nil || trace.VerificationSummary.Status != semantics.OutcomeUnknown {
		t.Fatalf("cancellation invented an effect failure: %#v", trace.VerificationSummary)
	}
	if trace.Run == nil || trace.Run.Status != semantics.RunCancelled {
		t.Fatalf("cancelled observation did not cancel the run: %#v", trace.Run)
	}
	if len(trace.Attempts) != 1 || trace.Attempts[0].Status != semantics.AttemptCancelled {
		t.Fatalf("cancelled attempt was not preserved: %#v", trace.Attempts)
	}
	values := map[string]any{"latest_action_observation": map[string]any{"state": observation.State}}
	status, reason, ok := semanticTaskTerminal(values)
	if !ok || status != controlentity.TaskStatusCancelled || !strings.Contains(reason, "user requested stop") {
		t.Fatalf("cancelled semantic run mapped to %q (%q), semantic=%v", status, reason, ok)
	}
}

func TestEnsureSemanticObservationPreservesDeviceAttemptFailureForRecoverableOutcome(t *testing.T) {
	action := testSemanticAction(t)
	if err := prepareSemanticAction(action, "device-1"); err != nil {
		t.Fatal(err)
	}
	trace := mustActionTrace(t, action)
	now := time.Now().UTC()
	result := semantics.VerificationResult{
		VerificationID: "verification-reobserve", OutcomeRef: trace.Outcome.OutcomeID,
		PlanRunRef: trace.Run.PlanRunID, EffectClauseID: "effect-playback",
		Status: semantics.VerificationUnknown, Confidence: 0, Reason: "snapshot drift requires re-observation", VerifiedAt: now,
	}
	summary := semantics.Aggregate(trace.Outcome.OutcomeID, trace.Run.PlanRunID, []semantics.VerificationResult{result}, now)
	trace.VerificationResults = []semantics.VerificationResult{result}
	trace.VerificationSummary = &summary
	trace.Run.Status = semantics.RunPaused
	trace.Run.TerminalReason = "snapshot drift"
	trace.Run.FinishedAt = now
	trace.Attempts[0].Status = semantics.AttemptFailed
	trace.Attempts[0].Error = "browser page changed from snapshot-a to snapshot-b"
	trace.Attempts[0].FinishedAt = now
	encoded, err := semantics.ToMap(trace)
	if err != nil {
		t.Fatal(err)
	}
	observation := testSemanticObservation(action, controlentity.ObservationSucceeded)
	observation.State[semantics.StateKey] = encoded
	if err := ensureSemanticObservation(action, observation); err != nil {
		t.Fatal(err)
	}
	observed := mustObservationTrace(t, observation)
	if observed.Run.Status != semantics.RunPaused || observed.VerificationSummary.Status != semantics.OutcomeUnknown {
		t.Fatalf("recoverable device result was overwritten: run=%#v summary=%#v", observed.Run, observed.VerificationSummary)
	}
	if observed.Attempts[0].Status != semantics.AttemptFailed || !strings.Contains(observed.Attempts[0].Error, "page changed") {
		t.Fatalf("device attempt result was overwritten by transport success: %#v", observed.Attempts[0])
	}
	if observed.Attempts[0].ObservationRef != observation.ObservationID {
		t.Fatalf("observation was not linked to the preserved attempt: %#v", observed.Attempts[0])
	}
}

func TestSemanticTaskTerminalUsesEffectSummary(t *testing.T) {
	for _, test := range []struct {
		name    string
		outcome string
		want    string
	}{
		{name: "succeeded", outcome: semantics.OutcomeSucceeded, want: controlentity.StatusCompleted},
		{name: "failed", outcome: semantics.OutcomeFailed, want: controlentity.StatusFailed},
		{name: "conflicting", outcome: semantics.OutcomeConflicting, want: controlentity.StatusFailed},
		{name: "unknown", outcome: semantics.OutcomeUnknown, want: controlentity.TaskStatusPaused},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := testSemanticTrace(t)
			now := time.Now().UTC()
			verificationStatus := semantics.VerificationUnknown
			switch test.outcome {
			case semantics.OutcomeSucceeded:
				verificationStatus = semantics.VerificationSatisfied
			case semantics.OutcomeFailed:
				verificationStatus = semantics.VerificationUnsatisfied
			case semantics.OutcomeConflicting:
				verificationStatus = semantics.VerificationConflicting
			}
			result := semantics.VerificationResult{
				VerificationID: "verification-terminal", OutcomeRef: trace.Outcome.OutcomeID, PlanRunRef: "run-terminal",
				EffectClauseID: "effect-playback", Status: verificationStatus, Confidence: 0.9, VerifiedAt: now,
			}
			summary := semantics.Aggregate(trace.Outcome.OutcomeID, "run-terminal", []semantics.VerificationResult{result}, now)
			trace.Run = &semantics.PlanRun{
				PlanRunID: "run-terminal", PlanRef: trace.Plan.PlanCandidateID, Status: semantics.TerminalRunStatus(&summary),
				StartedAt: now.Add(-time.Second), FinishedAt: now,
			}
			trace.VerificationResults = []semantics.VerificationResult{result}
			trace.VerificationSummary = &summary
			value, err := semantics.ToMap(trace)
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]any{"latest_action_observation": map[string]any{"state": map[string]any{semantics.StateKey: value}}}
			status, _, ok := semanticTaskTerminal(values)
			if !ok || status != test.want {
				t.Fatalf("status = %q, semantic=%v; want %q", status, ok, test.want)
			}
		})
	}
}

func testSemanticAction(t *testing.T) *controlentity.Action {
	t.Helper()
	trace := testSemanticTrace(t)
	arguments := map[string]any{"goal": trace.Outcome.Goal}
	if err := semantics.PutTrace(arguments, trace); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return &controlentity.Action{
		Protocol: controlentity.Protocol, Type: controlentity.TypeAction,
		TaskID: "task-1", StepID: "step-1", ActionID: "action-1",
		Sequence: 1, Revision: 1, IdempotencyKey: "task-1:step-1:action-1",
		IssuedAt: now, Deadline: now.Add(time.Minute), Capability: "browser.task", Operation: "play",
		SessionID: "browser-session-1", Arguments: arguments,
		Policy: controlentity.Policy{Risk: controlentity.RiskReversible, Decision: controlentity.Allow, Reason: "user requested playback"},
	}
}

func testSemanticTrace(t *testing.T) *semantics.SemanticTrace {
	t.Helper()
	now := time.Now().UTC()
	outcome := semantics.OutcomeSpec{
		Schema: semantics.Schema, OutcomeID: "outcome-1", Goal: "play the second video", CreatedAt: now,
		TargetSpec: semantics.TargetSpec{
			TargetSpecID: "target-1", SourceSnapshotRef: "snapshot-1",
			Selector: semantics.TargetSelector{Type: "ordinal", Value: "video", Ordinal: 2},
		},
		DesiredEffects: []semantics.EffectClause{{
			ClauseID: "effect-playback", Kind: semantics.EffectDesired, Subject: "target.entity",
			Predicate: "media.playback_state", Operator: "equals", Expected: "playing", Required: true,
		}},
	}
	plan := semantics.PlanCandidate{
		PlanCandidateID: "plan-1", OutcomeRef: outcome.OutcomeID, CreatedAt: now,
		Steps: []semantics.PlanStep{{StepID: "step-1", Ordinal: 1, Capability: "browser.task", Operation: "play", ExpectedEffectIDs: []string{"effect-playback"}}},
	}
	plan.DefinitionHash = semantics.Hash(plan.Steps)
	trace := &semantics.SemanticTrace{Schema: semantics.Schema, Outcome: outcome, Plan: plan}
	if err := trace.Validate(); err != nil {
		t.Fatal(err)
	}
	return trace
}

func testSemanticObservation(action *controlentity.Action, status string) *controlentity.Observation {
	return &controlentity.Observation{
		Protocol: controlentity.Protocol, Type: controlentity.TypeObservation,
		ObservationID: "observation-1", TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID,
		Sequence: action.Sequence, Revision: action.Revision, Status: status, ObservedAt: time.Now().UTC(),
		State: map[string]any{"url": "https://example.test/watch/2"},
	}
}

func mustActionTrace(t *testing.T, action *controlentity.Action) *semantics.SemanticTrace {
	t.Helper()
	trace, err := semantics.TraceFromArguments(action.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	if trace == nil {
		t.Fatal("action does not contain a semantic trace")
	}
	return trace
}

func mustObservationTrace(t *testing.T, observation *controlentity.Observation) *semantics.SemanticTrace {
	t.Helper()
	trace, err := semantics.TraceFromMap(observation.State[semantics.StateKey])
	if err != nil {
		t.Fatal(err)
	}
	if trace == nil {
		t.Fatal("observation does not contain a semantic trace")
	}
	return trace
}
