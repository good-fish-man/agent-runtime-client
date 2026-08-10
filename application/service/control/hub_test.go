package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

type fakeConnection struct {
	mu   sync.Mutex
	sent []any
	hub  *Hub
}

func (f *fakeConnection) Send(value any) error {
	f.mu.Lock()
	f.sent = append(f.sent, value)
	f.mu.Unlock()
	action := value.(entity.Action)
	go func() {
		_ = f.hub.Observe(context.Background(), entity.Observation{Protocol: entity.Protocol, Type: entity.TypeObservation, TaskID: action.TaskID, ActionID: action.ActionID, Sequence: action.Sequence, Status: entity.ObservationSucceeded, ObservedAt: time.Now().UTC()})
	}()
	return nil
}

func (f *fakeConnection) Close() error { return nil }

func TestHubDispatchCorrelatesObservation(t *testing.T) {
	hub := NewHub()
	connection := &fakeConnection{hub: hub}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Name: "test", Capabilities: []string{"browser.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	action := entity.Action{Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: "task-1", ActionID: "action-1", Sequence: 1, IdempotencyKey: "task-1:1", Deadline: time.Now().Add(time.Second), Capability: "browser.open", Policy: entity.Policy{Risk: entity.RiskLow, Decision: entity.Allow}}
	observation, err := hub.Dispatch(context.Background(), "device-1", action)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ActionID != action.ActionID || observation.Status != entity.ObservationSucceeded {
		t.Fatalf("unexpected observation: %+v", observation)
	}
}

func TestHubDispatchForwardsProgressWithoutCompletingAction(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 2)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Name: "test", Capabilities: []string{"browser.download"}}, connection); err != nil {
		t.Fatal(err)
	}
	action := entity.Action{
		Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, IdempotencyKey: "task-1:1", Deadline: time.Now().Add(time.Second),
		Capability: "browser.download", Policy: entity.Policy{Risk: entity.RiskMedium, Decision: entity.Allow},
	}
	progressed := make(chan entity.Progress, 1)
	result := make(chan *entity.Observation, 1)
	errors := make(chan error, 1)
	go func() {
		observation, err := hub.Dispatch(context.Background(), "device-1", action, func(progress entity.Progress) error {
			progressed <- progress
			return nil
		})
		if err != nil {
			errors <- err
			return
		}
		result <- observation
	}()
	if _, ok := (<-connection.messages).(entity.Action); !ok {
		t.Fatal("first message is not an action")
	}
	if err := hub.Progress(context.Background(), entity.Progress{
		Protocol: entity.Protocol, Type: entity.TypeProgress, TaskID: action.TaskID, ActionID: action.ActionID,
		Sequence: action.Sequence, Stage: "downloading", Progress: 42, SentAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case progress := <-progressed:
		if progress.Progress != 42 || progress.Stage != "downloading" {
			t.Fatalf("unexpected progress: %+v", progress)
		}
	case observation := <-result:
		t.Fatalf("progress completed action early: %+v", observation)
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for progress")
	}
	if err := hub.Observe(context.Background(), entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, TaskID: action.TaskID, ActionID: action.ActionID,
		Sequence: action.Sequence, Status: entity.ObservationSucceeded, ObservedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case observation := <-result:
		if observation.Status != entity.ObservationSucceeded {
			t.Fatalf("unexpected observation: %+v", observation)
		}
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final observation")
	}
}

func TestHubRejectsOfflineDevice(t *testing.T) {
	_, err := NewHub().Dispatch(context.Background(), "missing", entity.Action{Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: "task", ActionID: "action", Sequence: 1, IdempotencyKey: "task:1", Capability: "browser.open", Deadline: time.Now().Add(time.Second), Policy: entity.Policy{Risk: entity.RiskLow, Decision: entity.Allow}})
	if err != ErrDeviceOffline {
		t.Fatalf("error = %v", err)
	}
}

type cancelConnection struct {
	messages chan any
}

func (c *cancelConnection) Send(value any) error {
	c.messages <- value
	return nil
}

func (c *cancelConnection) Close() error { return nil }

func TestHubSendsCancelWhenRequestIsCancelled(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 2)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Capabilities: []string{"browser.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := hub.Dispatch(ctx, "device-1", entity.Action{
			Protocol: entity.Protocol, Type: entity.TypeAction,
			TaskID: "task", ActionID: "action", IdempotencyKey: "task:1",
			Sequence: 1, Capability: "browser.open", Deadline: time.Now().Add(time.Second), Policy: entity.Policy{Risk: entity.RiskLow, Decision: entity.Allow},
		})
		result <- err
	}()
	if _, ok := (<-connection.messages).(entity.Action); !ok {
		t.Fatal("first message is not an action")
	}
	cancel()
	if _, ok := (<-connection.messages).(entity.Cancel); !ok {
		t.Fatal("second message is not a cancel")
	}
	if err := <-result; err != context.Canceled {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestHubPersistsConversationDeviceSession(t *testing.T) {
	hub := NewHub()
	if err := hub.BeginTask(context.Background(), "task-1", "user-1", "chat-1", "device-1"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.sessions["task-1"].Actions = append(hub.sessions["task-1"].Actions, entity.Action{Capability: "browser.open"})
	hub.recordObservationLocked("task-1", entity.Observation{SessionID: "athena-0123456789abcdef", Status: "SUCCEEDED"})
	hub.mu.Unlock()
	if got := hub.ActiveSessions(context.Background(), "user-1", "chat-1")["athena"]; got != "athena-0123456789abcdef" {
		t.Fatalf("active browser session = %q", got)
	}
}

func TestHubTerminalStatusIgnoresLateStatusProgressAndObservation(t *testing.T) {
	hub := NewHub()
	ctx := context.Background()
	if err := hub.BeginTask(ctx, "task-1", "user-1", "chat-1", "device-1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.SetTaskStatus(ctx, "task-1", entity.StatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := hub.SetTaskStatus(ctx, "task-1", entity.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	hub.recordProgress("task-1", entity.Progress{Progress: 100, Stage: "late"})
	hub.mu.Lock()
	hub.recordObservationLocked("task-1", entity.Observation{Status: entity.ObservationSucceeded})
	hub.mu.Unlock()
	task, ok, err := hub.Task(ctx, "task-1")
	if err != nil || !ok {
		t.Fatalf("task lookup = %t, %v", ok, err)
	}
	if task.Status != entity.StatusFailed {
		t.Fatalf("task status = %q, want %q", task.Status, entity.StatusFailed)
	}
	if len(task.Observations) != 1 {
		t.Fatalf("late observation was not retained: %+v", task.Observations)
	}
}

func TestHubActionFailureCanBeRecoveredByControlLoop(t *testing.T) {
	hub := NewHub()
	ctx := context.Background()
	if err := hub.BeginTask(ctx, "task-1", "user-1", "chat-1", "device-1"); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.recordObservationLocked("task-1", entity.Observation{Status: entity.ObservationFailed, Error: "retryable action failure"})
	hub.mu.Unlock()
	task, ok, err := hub.Task(ctx, "task-1")
	if err != nil || !ok {
		t.Fatalf("task lookup = %t, %v", ok, err)
	}
	if task.Status != entity.StatusEvaluating {
		t.Fatalf("action failure prematurely terminated task with %q", task.Status)
	}
	if err := hub.SetTaskStatus(ctx, "task-1", entity.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	task, _, _ = hub.Task(ctx, "task-1")
	if task.Status != entity.StatusCompleted {
		t.Fatalf("recovered task status = %q, want %q", task.Status, entity.StatusCompleted)
	}
}

func TestHubBindsDeviceToFirstAuthenticatedUser(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 1)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Capabilities: []string{"app.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	deviceID, err := hub.ResolveDevice(context.Background(), "user-1", "", "app.open")
	if err != nil || deviceID != "device-1" {
		t.Fatalf("resolve device = %q, %v", deviceID, err)
	}
	if _, err := hub.ResolveDevice(context.Background(), "user-2", "device-1", "app.open"); err == nil {
		t.Fatal("a device bound to another user was accepted")
	}
	devices, err := hub.Devices(context.Background(), "user-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("other user's devices leaked: %+v", devices)
	}
}

func TestHubResolveReportsDeviceBoundToAnotherUser(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 1)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Capabilities: []string{"app.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ResolveDevice(context.Background(), "user-1", "", "app.open"); err != nil {
		t.Fatal(err)
	}
	_, err := hub.ResolveDevice(context.Background(), "user-2", "", "app.open")
	if !errors.Is(err, ErrDeviceBoundToAnotherUser) {
		t.Fatalf("error = %v, want ErrDeviceBoundToAnotherUser", err)
	}
}

func TestHubResolveReportsUnsupportedCapability(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 1)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Capabilities: []string{"app.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	_, err := hub.ResolveDevice(context.Background(), "user-1", "", "browser.open")
	if !errors.Is(err, ErrDeviceCapabilityUnsupported) {
		t.Fatalf("error = %v, want ErrDeviceCapabilityUnsupported", err)
	}
}

func TestHubDiagnosticsDescribeConnectedDevices(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 1)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1234567890abcdef", Capabilities: []string{"app.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ResolveDevice(context.Background(), "user-1", "", "app.open"); err != nil {
		t.Fatal(err)
	}
	diagnostics := hub.Diagnostics("user-2", "browser.open")
	if diagnostics.ConnectedDevices != 1 || diagnostics.OtherUserDevices != 1 || diagnostics.MatchingDevices != 0 || diagnostics.UnsupportedDevices != 1 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	if len(diagnostics.Devices) != 1 || diagnostics.Devices[0].ID == "device-1234567890abcdef" {
		t.Fatalf("device id should be shortened in diagnostics: %+v", diagnostics.Devices)
	}
}

func TestHubCancelsPendingActionByConversation(t *testing.T) {
	hub := NewHub()
	connection := &cancelConnection{messages: make(chan any, 3)}
	if err := hub.Register(context.Background(), entity.DeviceMessage{DeviceID: "device-1", Capabilities: []string{"browser.open"}}, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ResolveDevice(context.Background(), "user-1", "device-1", "browser.open"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BeginTask(context.Background(), "task-1", "user-1", "conversation-1", "device-1"); err != nil {
		t.Fatal(err)
	}
	action := entity.Action{
		Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, IdempotencyKey: "task-1:1", Deadline: time.Now().Add(time.Second),
		Capability: "browser.open", Policy: entity.Policy{Risk: entity.RiskLow, Decision: entity.Allow},
	}
	result := make(chan error, 1)
	go func() {
		_, err := hub.Dispatch(context.Background(), "device-1", action)
		result <- err
	}()
	if _, ok := (<-connection.messages).(entity.Action); !ok {
		t.Fatal("first message is not an action")
	}
	if err := hub.CancelByConversation(context.Background(), "user-1", "conversation-1", "user requested stop"); err != nil {
		t.Fatal(err)
	}
	if _, ok := (<-connection.messages).(entity.Cancel); !ok {
		t.Fatal("device did not receive CANCEL")
	}
	if err := hub.Observe(context.Background(), entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, TaskID: action.TaskID, ActionID: action.ActionID,
		Sequence: action.Sequence, Status: entity.ObservationCancelled, ObservedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
