package control

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

func TestObservationAttachmentValidationAndSanitization(t *testing.T) {
	data := []byte("bounded-image")
	sum := sha256.Sum256(data)
	observation := Observation{
		Protocol: Protocol, Type: TypeObservation, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, Status: ObservationSucceeded, ObservedAt: time.Now().UTC(),
		Attachments: []Attachment{{
			ID: "artifact-1", Kind: "image", MIMEType: "image/png", Size: int64(len(data)),
			SHA256: hex.EncodeToString(sum[:]), Encoding: AttachmentEncodingBase64,
			Data: base64.StdEncoding.EncodeToString(data),
		}},
	}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	safe := observation.WithoutAttachmentData()
	if safe.Attachments[0].Data != "" || observation.Attachments[0].Data == "" {
		t.Fatalf("sanitization mutated source or retained payload: source=%#v safe=%#v", observation, safe)
	}
}

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

func TestTaskStatusKeepsFirstTerminalOutcome(t *testing.T) {
	if !CanTransitionTaskStatus(StatusExecuting, StatusFailed) {
		t.Fatal("active task could not enter a terminal state")
	}
	if CanTransitionTaskStatus(StatusFailed, StatusCompleted) {
		t.Fatal("failed task was allowed to become completed")
	}
	if CanTransitionTaskStatus(StatusCancelled, StatusExecuting) {
		t.Fatal("cancelled task was allowed to resume from late progress")
	}
	if !CanTransitionTaskStatus(StatusCompleted, StatusCompleted) {
		t.Fatal("idempotent terminal status write was rejected")
	}
}
