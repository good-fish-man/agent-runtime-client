package experience

import (
	"testing"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
)

func TestFailureClassifierUsesRulePrecedence(t *testing.T) {
	task := &controlentity.TaskSession{
		Status:      controlentity.StatusFailed,
		ErrorDetail: &controlentity.ErrorDetail{Message: "model stream failed after device is offline"},
	}
	classification := classifyFailure(task)
	if classification == nil || classification.Class != entity.FailureDeviceOffline || classification.Rule != "device_offline" {
		t.Fatalf("classification = %#v", classification)
	}
}

func TestFailureClassifierFallsBackToRuntime(t *testing.T) {
	classification := classifyFailure(&controlentity.TaskSession{Status: controlentity.StatusFailed})
	if classification == nil || classification.Class != entity.FailureRuntime {
		t.Fatalf("classification = %#v", classification)
	}
}
