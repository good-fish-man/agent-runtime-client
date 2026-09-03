package experience

import (
	"reflect"
	"strings"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
)

func TestReplayFixtureIsDeterministicAndOffline(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-1", OwnerID: "user-1", ExperienceID: "experience-1", Name: "open page",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('a', 64),
		Protocol: "athena.agent.v4", Input: map[string]any{"goal": "open page"},
		Expected:    entity.ExpectedOutcome{TaskStatus: "COMPLETED", ObservationStatus: "SUCCEEDED", Predicates: map[string]any{"verification_passed": true}},
		Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
	}
	run := entity.EvaluationRun{RunID: "run-1", Seed: 42}
	left, err := replayFixture(run, fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	right, err := replayFixture(run, fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if left.Passed != right.Passed || !reflect.DeepEqual(left.Metrics, right.Metrics) || left.Summary != right.Summary {
		t.Fatalf("replay was not deterministic: left=%#v right=%#v", left, right)
	}
	if left.Metrics.CostMicros != 0 || left.Metrics.SafetyScore != 1 {
		t.Fatalf("offline safety/cost metrics are invalid: %#v", left.Metrics)
	}
	if left.Regression || !reflect.DeepEqual(left.Metrics, left.BaselineMetrics) || left.MetricDelta != (entity.EvaluationMetrics{}) {
		t.Fatalf("matching replay was reported as a regression: %#v", left)
	}
}

func TestReplayFixtureDetectsOutcomeAndSafetyRegression(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-regression", OwnerID: "user-1", ExperienceID: "experience-1", Name: "play video",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('b', 64),
		Protocol: "athena.agent.v4", Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
		Expected: entity.ExpectedOutcome{TaskStatus: "COMPLETED", ObservationStatus: "SUCCEEDED", Predicates: map[string]any{"verification_passed": true}},
		Input: map[string]any{
			"unsafe_effect": true,
			"replay_observation": map[string]any{
				"task_status": "FAILED", "observation_status": "FAILED",
				"predicates": map[string]any{"verification_passed": false},
			},
		},
	}
	result, err := replayFixture(entity.EvaluationRun{RunID: "run-regression", Seed: 42}, fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || !result.Regression || result.Metrics.Correctness != 0 || result.Metrics.SafetyScore != 0 {
		t.Fatalf("offline regression was not detected: %#v", result)
	}
	if result.MetricDelta.Correctness != -1 || result.MetricDelta.SafetyScore != -1 {
		t.Fatalf("regression delta is not comparable to baseline: %#v", result.MetricDelta)
	}
}

func TestReplayFixtureComparesCandidateToRecordedBaseline(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-comparison", OwnerID: "user-1", ExperienceID: "experience-1", Name: "compare candidate",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('c', 64),
		Protocol: "athena.agent.v4", Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
		Expected: entity.ExpectedOutcome{TaskStatus: "COMPLETED", ObservationStatus: "SUCCEEDED", Predicates: map[string]any{"verification_passed": true}},
		Input: map[string]any{
			"baseline_observation": map[string]any{
				"task_status": "FAILED", "observation_status": "FAILED",
				"predicates": map[string]any{"verification_passed": false},
			},
			"candidate_observation": map[string]any{
				"task_status": "COMPLETED", "observation_status": "SUCCEEDED",
				"predicates": map[string]any{"verification_passed": true},
			},
		},
	}
	result, err := replayFixture(entity.EvaluationRun{RunID: "run-improved", Seed: 42}, fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Regression || result.BaselineMetrics.Correctness != 0 || result.Metrics.Correctness != 1 || result.MetricDelta.Correctness != 1 {
		t.Fatalf("candidate improvement was not compared to the recorded baseline: %#v", result)
	}
}

func TestReplayFixtureDoesNotManufactureImprovementFromFailedBaseline(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-failed-baseline", OwnerID: "user-1", ExperienceID: "experience-1", Name: "failed browser task",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('f', 64),
		Protocol: "athena.agent.v4", Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
		Expected: entity.ExpectedOutcome{TaskStatus: "FAILED", ObservationStatus: "FAILED", Predicates: map[string]any{"verification_passed": false}},
		Input: map[string]any{
			"baseline_observation": map[string]any{
				"task_status": "FAILED", "observation_status": "FAILED",
				"predicates": map[string]any{"verification_passed": false},
			},
		},
	}
	result, err := replayFixture(entity.EvaluationRun{RunID: "run-failed-baseline", Seed: 42, CandidateID: "current-runtime"}, fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.Regression || !reflect.DeepEqual(result.Metrics, result.BaselineMetrics) || result.MetricDelta != (entity.EvaluationMetrics{}) {
		t.Fatalf("failed baseline was reported as a candidate improvement: %#v", result)
	}
	if result.Summary != "Offline simulator replayed the frozen baseline observation." {
		t.Fatalf("failed baseline summary = %q", result.Summary)
	}
}

func TestReplayFixtureRejectsNamedCandidateWithoutSimulatorOutput(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-no-candidate", OwnerID: "user-1", ExperienceID: "experience-1", Name: "missing candidate output",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('d', 64),
		Protocol: "athena.agent.v4", Expected: entity.ExpectedOutcome{TaskStatus: "COMPLETED"},
		Input:       map[string]any{"baseline_observation": map[string]any{"task_status": "COMPLETED"}},
		Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
	}
	_, err := replayFixture(entity.EvaluationRun{RunID: "run-unproven", Seed: 42, CandidateID: "candidate-unproven"}, fixture, nil)
	if err == nil || !strings.Contains(err.Error(), "no offline simulator observation") {
		t.Fatalf("unproven candidate was allowed to reuse the baseline: %v", err)
	}
}

func TestReplayFixtureExecutesDeclarativeCandidateAgainstFrozenEvidence(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-declarative", OwnerID: "user-1", ExperienceID: "experience-1", Name: "recover navigation",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('e', 64),
		Protocol: "athena.agent.v4", Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
		Expected: entity.ExpectedOutcome{TaskStatus: "FAILED", ObservationStatus: "FAILED", Predicates: map[string]any{"verification_passed": false}},
		Input: map[string]any{
			"actions":       []entity.ActionRef{{Capability: "browser.navigate", Operation: "navigate"}},
			"failure_class": "VERIFICATION_FAILURE",
			"baseline_observation": map[string]any{
				"task_status": "FAILED", "observation_status": "FAILED",
				"predicates": map[string]any{"verification_passed": false},
			},
		},
	}
	candidate := CandidateReplaySpec{
		ArtifactID: "candidate-1", ArtifactChecksum: stringsOf('f', 64), Kind: "SKILL",
		ActionPattern: "browser.navigate.navigate", RecoveryConditions: []string{"VERIFICATION_FAILURE"},
		VerificationEvidenceRequired: true,
	}
	result, err := replayFixture(entity.EvaluationRun{RunID: "run-declarative", Seed: 42, CandidateID: candidate.ArtifactID}, fixture, &candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.BaselineMetrics.SuccessRate != 0 || result.MetricDelta.SuccessRate != 1 {
		t.Fatalf("declarative candidate did not improve the failed baseline: %#v", result)
	}
	candidate.RecoveryConditions = nil
	result, err = replayFixture(entity.EvaluationRun{RunID: "run-no-recovery", Seed: 42, CandidateID: candidate.ArtifactID}, fixture, &candidate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatalf("candidate without bounded recovery passed: %#v", result)
	}
}

func TestReplayFixtureRejectsProductionRuntime(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-1", OwnerID: "user-1", ExperienceID: "experience-1", Name: "unsafe",
		RuntimeKind: "browser", Simulator: "browser.production", EnvironmentVersion: "1", SnapshotHash: stringsOf('a', 64),
		Protocol: "athena.agent.v4", Input: map[string]any{}, Expected: entity.ExpectedOutcome{TaskStatus: "COMPLETED"},
		Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
	}
	if _, err := replayFixture(entity.EvaluationRun{RunID: "run-1", Seed: 1}, fixture, nil); err == nil {
		t.Fatal("production simulator was accepted")
	}
}

func stringsOf(value byte, count int) string {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return string(result)
}
