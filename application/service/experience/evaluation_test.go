package experience

import (
	"reflect"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
)

func TestReplayFixtureIsDeterministicAndOffline(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-1", OwnerID: "user-1", ExperienceID: "experience-1", Name: "open page",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "1", SnapshotHash: stringsOf('a', 64),
		Protocol: "athena.agent.v4", Input: map[string]any{"goal": "open page"}, Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
	}
	run := entity.EvaluationRun{RunID: "run-1", Seed: 42}
	left, err := replayFixture(run, fixture)
	if err != nil {
		t.Fatal(err)
	}
	right, err := replayFixture(run, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if left.Passed != right.Passed || !reflect.DeepEqual(left.Metrics, right.Metrics) || left.Summary != right.Summary {
		t.Fatalf("replay was not deterministic: left=%#v right=%#v", left, right)
	}
	if left.Metrics.CostMicros != 0 || left.Metrics.SafetyScore != 1 {
		t.Fatalf("offline safety/cost metrics are invalid: %#v", left.Metrics)
	}
}

func TestReplayFixtureRejectsProductionRuntime(t *testing.T) {
	fixture := entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: "fixture-1", OwnerID: "user-1", ExperienceID: "experience-1", Name: "unsafe",
		RuntimeKind: "browser", Simulator: "browser.production", EnvironmentVersion: "1", SnapshotHash: stringsOf('a', 64),
		Protocol: "athena.agent.v4", Input: map[string]any{}, Sensitivity: entity.SensitivityInternal, CreatedAt: time.Now().UTC(),
	}
	if _, err := replayFixture(entity.EvaluationRun{RunID: "run-1", Seed: 1}, fixture); err == nil {
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
