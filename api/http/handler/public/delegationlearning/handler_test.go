package delegationlearning

import (
	"encoding/json"
	"strings"
	"testing"

	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

func TestDecodeSnapshotEncodesEmptyCollectionsAsArrays(t *testing.T) {
	value, err := decodeSnapshot(delegationentity.LearningSnapshot{})
	if err != nil {
		t.Fatalf("decodeSnapshot() error = %v", err)
	}
	if value.Candidates == nil || value.Evaluations == nil || value.Reviews == nil || value.Rollouts == nil || value.Benchmarks == nil {
		t.Fatalf("decodeSnapshot() returned nil collections: %+v", value)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, field := range []string{"candidates", "evaluations", "reviews", "rollouts", "benchmarks"} {
		if !strings.Contains(string(payload), `"`+field+`":[]`) {
			t.Fatalf("%s must be encoded as an empty array: %s", field, payload)
		}
	}
}
