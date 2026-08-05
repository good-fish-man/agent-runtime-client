package control

import (
	"testing"
	"time"
)

func TestActionValidateRequiresFrozenEnvelope(t *testing.T) {
	action := Action{
		Protocol: Protocol, Type: TypeAction, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, IdempotencyKey: "task-1:1", Deadline: time.Now().Add(time.Second),
		Capability: "browser.open", Policy: Policy{Risk: RiskLow, Decision: Allow},
	}
	if err := action.Validate(); err != nil {
		t.Fatal(err)
	}
	action.Protocol = "legacy"
	if err := action.Validate(); err == nil {
		t.Fatal("legacy action protocol was accepted")
	}
}

func TestObservationValidateRejectsUnknownStatus(t *testing.T) {
	observation := Observation{
		Protocol: Protocol, Type: TypeObservation, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, Status: "MAYBE", ObservedAt: time.Now().UTC(),
	}
	if err := observation.Validate(); err == nil {
		t.Fatal("unknown observation status was accepted")
	}
}

func TestTaskStatusIsClosedEnum(t *testing.T) {
	if !ValidTaskStatus(StatusExecuting) || ValidTaskStatus("RUNNING_SOMEHOW") {
		t.Fatal("task status enum is not enforced")
	}
}
