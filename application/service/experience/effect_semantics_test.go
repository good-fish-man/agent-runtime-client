package experience

import (
	"strings"
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

func TestBuildExperienceLearnsOnlyFromVerifiedEffectSuccess(t *testing.T) {
	task := semanticExperienceTask(t, semantics.OutcomeSucceeded)
	service := &Service{redactor: NewRedactor()}
	stored, err := service.build(task, nil, testLearningPreference(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Experience.Verification.Passed || stored.Experience.Outcome != entity.OutcomeSucceeded {
		t.Fatalf("verified success was not retained: %#v", stored.Experience)
	}
	if stored.Experience.Intent["effect_trace"] == nil {
		t.Fatalf("experience lost its effect provenance: %#v", stored.Experience.Intent)
	}
	if stored.Experience.TaskType != "browser_interaction" || stored.Experience.Domain != "video" || len(stored.Experience.SkillRefs) != 1 || stored.Experience.SkillRefs[0] != "media.playback" {
		t.Fatalf("experience lost its structured retrieval dimensions: %#v", stored.Experience)
	}
	for _, want := range []string{"browser.task.play", "Policy control-policy/v4", "world-hash"} {
		combined := stored.Experience.PlanSummary + " " + stored.Experience.DecisionSummary
		if !strings.Contains(combined, want) {
			t.Fatalf("experience summary missing %q: %s", want, combined)
		}
	}
}

func TestBuildExperienceUsesPersistedWorldChanges(t *testing.T) {
	task := semanticExperienceTask(t, semantics.OutcomeSucceeded)
	events := []controlentity.EventEnvelope{{EventID: "event-world", Type: controlentity.EventWorldPatched, Payload: map[string]any{"changes": []any{map[string]any{"kind": "facts", "object_id": "fact-1", "operation": "set", "path": "/facts/fact-1/value", "before": "old", "after": "new"}}}}}
	stored, err := (&Service{redactor: NewRedactor()}).build(task, events, testLearningPreference(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Experience.WorldChanges) != 1 || stored.Experience.WorldChanges[0].After != "new" {
		t.Fatalf("world changes = %#v", stored.Experience.WorldChanges)
	}
}

func TestBuildExperienceDoesNotLearnFromPlainCompletedStatus(t *testing.T) {
	now := time.Now().UTC()
	task := &controlentity.TaskSession{
		TaskID: "task-plain", UserID: "user-1", Goal: "play the second video",
		Status: controlentity.StatusCompleted, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
		Observations: []controlentity.Observation{{
			ObservationID: "observation-plain", ActionID: "action-plain",
			Status: controlentity.ObservationSucceeded, ObservedAt: now,
		}},
	}
	service := &Service{redactor: NewRedactor()}
	stored, err := service.build(task, nil, testLearningPreference(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Experience.Verification.Passed {
		t.Fatalf("transport completion became verified learning data: %#v", stored.Experience.Verification)
	}
	if !strings.Contains(stored.Experience.Verification.Summary, "without effect-specific evidence") {
		t.Fatalf("missing unverified completion explanation: %#v", stored.Experience.Verification)
	}
}

func TestCostSummaryIgnoresIncompleteObservationTimestamps(t *testing.T) {
	now := time.Now().UTC()
	task := &controlentity.TaskSession{
		Actions: []controlentity.Action{{ActionID: "action-incomplete-time", Capability: "browser.task", Operation: "task"}},
		Observations: []controlentity.Observation{{
			ActionID: "action-incomplete-time", Status: controlentity.ObservationSucceeded, FinishedAt: now,
		}},
	}
	cost := costSummary(task, nil)
	if len(cost.Capabilities) != 1 || cost.Capabilities[0].DurationMS != 0 {
		t.Fatalf("incomplete observation timestamps produced a duration: %#v", cost.Capabilities)
	}
}

func TestTaskBrowserSiteScopeUsesObservedURLWithoutPersistingPath(t *testing.T) {
	task := &controlentity.TaskSession{
		Actions: []controlentity.Action{{Capability: "browser.task", Arguments: map[string]any{"target": "YouTube"}}},
		Observations: []controlentity.Observation{{State: map[string]any{
			"browser_task": map[string]any{"url": "https://WWW.Example.Test/private?q=secret#fragment"},
		}}},
	}
	if got := taskBrowserSiteScope(task); got != "example.test" {
		t.Fatalf("site scope = %q, want example.test", got)
	}
}

func TestTaskBrowserSiteScopeRejectsCredentialURL(t *testing.T) {
	task := &controlentity.TaskSession{
		Actions: []controlentity.Action{{Capability: "browser.open", Arguments: map[string]any{"url": "https://user:secret@example.test/private"}}},
	}
	if got := taskBrowserSiteScope(task); got != "" {
		t.Fatalf("site scope = %q, want empty", got)
	}
}

func TestBuildExperienceRecordsFailedEffectDespiteCompletedEnvelope(t *testing.T) {
	task := semanticExperienceTask(t, semantics.OutcomeFailed)
	service := &Service{redactor: NewRedactor()}
	stored, err := service.build(task, nil, testLearningPreference(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Experience.Verification.Passed || stored.Experience.Outcome != entity.OutcomeFailed {
		t.Fatalf("failed effect was hidden by task completion: %#v", stored.Experience)
	}
}

func semanticExperienceTask(t *testing.T, outcomeStatus string) *controlentity.TaskSession {
	t.Helper()
	now := time.Now().UTC()
	outcome := semantics.OutcomeSpec{
		Schema: semantics.Schema, OutcomeID: "outcome-experience", Goal: "play the second video", CreatedAt: now,
		TargetSpec: semantics.TargetSpec{TargetSpecID: "target-experience", Selector: semantics.TargetSelector{Type: "ordinal", Kind: "video", Ordinal: 2}},
		DesiredEffects: []semantics.EffectClause{{
			ClauseID: "effect-playback", Kind: semantics.EffectDesired, Subject: "target.entity",
			Predicate: "media.playback_state", Operator: "equals", Expected: "playing", Required: true,
		}},
	}
	plan := semantics.PlanCandidate{
		PlanCandidateID: "plan-experience", OutcomeRef: outcome.OutcomeID, CreatedAt: now,
		Steps: []semantics.PlanStep{{StepID: "step-experience", Ordinal: 1, Capability: "browser.task", Operation: "play", ExpectedEffectIDs: []string{"effect-playback"}}},
	}
	plan.DefinitionHash = semantics.Hash(plan.Steps)
	verificationStatus := semantics.VerificationSatisfied
	observed := any("playing")
	if outcomeStatus != semantics.OutcomeSucceeded {
		verificationStatus = semantics.VerificationUnsatisfied
		observed = "paused"
	}
	result := semantics.VerificationResult{
		VerificationID: "verification-experience", OutcomeRef: outcome.OutcomeID, PlanRunRef: "run-experience",
		EffectClauseID: "effect-playback", Status: verificationStatus, ExpectedValue: "playing", ObservedValue: observed,
		EvidenceRefs: []string{"evidence-playback"}, Confidence: 0.98, VerifiedAt: now,
	}
	trace := &semantics.SemanticTrace{
		Schema: semantics.Schema, Outcome: outcome, Plan: plan,
		Policy: &semantics.PolicyDecision{
			DecisionID: "decision-experience", PlanRef: plan.PlanCandidateID, WorldReadSetHash: "world-hash",
			PolicyVersion: "control-policy/v4", InputHash: "input-hash", Decision: semantics.PolicyAllow,
			DecidedAt: now, ExpiresAt: now.Add(time.Minute),
		},
		Run:                 &semantics.PlanRun{PlanRunID: "run-experience", PlanRef: plan.PlanCandidateID, Status: semantics.TerminalRunStatus(&semantics.OutcomeVerificationSummary{Status: outcomeStatus}), StartedAt: now.Add(-time.Second), FinishedAt: now},
		VerificationResults: []semantics.VerificationResult{result},
		VerificationSummary: &semantics.OutcomeVerificationSummary{
			Schema: semantics.Schema, OutcomeRef: outcome.OutcomeID, PlanRunRef: "run-experience", Status: outcomeStatus,
			Satisfied: boolCount(outcomeStatus == semantics.OutcomeSucceeded), Unsatisfied: boolCount(outcomeStatus != semantics.OutcomeSucceeded),
			Total: 1, Results: []semantics.VerificationResult{result}, EvidenceRefs: []string{"evidence-playback"}, VerifiedAt: now,
		},
	}
	value, err := semantics.ToMap(trace)
	if err != nil {
		t.Fatal(err)
	}
	return &controlentity.TaskSession{
		TaskID: "task-experience", UserID: "user-1", DeviceID: "device-1", Goal: outcome.Goal,
		Status: controlentity.StatusCompleted, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
		Metadata: map[string]interface{}{
			"task_type": "browser_interaction", "domain": "video", "skill_refs": []any{"media.playback"},
		},
		Observations: []controlentity.Observation{{
			ObservationID: "observation-experience", ActionID: "action-experience", Status: controlentity.ObservationSucceeded,
			ObservedAt: now, State: map[string]any{semantics.StateKey: value},
			Evidence: []controlentity.EvidenceRef{{EvidenceID: "evidence-playback", Kind: "media_state"}},
		}},
	}
}

func testLearningPreference() entity.Preference {
	return entity.Preference{LearningEnabled: true, RetentionDays: 30, MaxSensitivity: entity.SensitivityRestricted}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
