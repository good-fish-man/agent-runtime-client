package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
)

func TestApplyDeviceObservationStoresLatestHistoryAndActiveSession(t *testing.T) {
	values := map[string]any{}
	action := &controlentity.Action{
		Protocol: controlentity.Protocol, Type: controlentity.TypeAction, TaskID: "task-1", ActionID: "action-1",
		SessionID: "athena-existing", Sequence: 1, IdempotencyKey: "task-1:action-1",
		Deadline: time.Now().UTC().Add(time.Minute), Capability: "browser.open",
		Arguments: map[string]any{"target": "YouTube"}, Policy: controlentity.Policy{Risk: controlentity.RiskMedium, Decision: controlentity.Allow},
	}
	observation := &controlentity.Observation{
		Protocol: controlentity.Protocol, Type: controlentity.TypeObservation, TaskID: "task-1", ActionID: "action-1",
		SessionID: "athena-browser-1", Sequence: 1, Status: controlentity.ObservationSucceeded, ObservedAt: time.Now().UTC(),
		State: map[string]any{"url": "https://youtube.com", "title": "YouTube"},
	}

	applyDeviceObservation(values, "open YouTube", action, observation)

	if values["original_task"] != "open YouTube" {
		t.Fatalf("original task = %v", values["original_task"])
	}
	if values["active_browser_session"] != "athena-browser-1" || values["active_device_session"] != "athena-browser-1" {
		t.Fatalf("active sessions not set: %#v", values)
	}
	latest, ok := values["latest_action_observation"].(map[string]any)
	if !ok || latest["status"] != controlentity.ObservationSucceeded {
		t.Fatalf("latest observation = %#v", values["latest_action_observation"])
	}
	actionCtx, ok := values["latest_action"].(map[string]any)
	if !ok || actionCtx["capability"] != "browser.open" {
		t.Fatalf("latest action = %#v", values["latest_action"])
	}
	history, ok := values["action_observation_history"].([]any)
	if !ok || len(history) != 1 {
		t.Fatalf("history = %#v", values["action_observation_history"])
	}
}

func TestNextDeviceObservationPromptIncludesObservationAndRules(t *testing.T) {
	action := &controlentity.Action{
		Protocol: controlentity.Protocol, Type: controlentity.TypeAction, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, IdempotencyKey: "task-1:action-1", Deadline: time.Now().UTC().Add(time.Minute),
		Capability: "browser.observe", Policy: controlentity.Policy{Risk: controlentity.RiskLow, Decision: controlentity.Allow},
	}
	observation := &controlentity.Observation{
		Protocol: controlentity.Protocol, Type: controlentity.TypeObservation, TaskID: "task-1", ActionID: "action-1",
		SessionID: "athena-browser-1", Sequence: 1, Status: controlentity.ObservationSucceeded, ObservedAt: time.Now().UTC(),
		State: map[string]any{"title": "A video about AI Agents"},
	}

	prompt := nextDeviceObservationPrompt("open the first suitable video", action, observation)
	for _, want := range []string{
		"real Athena Desktop/Browser action has finished",
		"open the first suitable video",
		`"capability": "browser.observe"`,
		`"title": "A video about AI Agents"`,
		"Reuse session_id",
		"Do not repeat the same successful action",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestNextDeviceObservationPromptWarnsAboutBrowserChallenge(t *testing.T) {
	action := &controlentity.Action{
		Protocol: controlentity.Protocol, Type: controlentity.TypeAction, TaskID: "task-1", ActionID: "action-1",
		Sequence: 1, IdempotencyKey: "task-1:action-1", Deadline: time.Now().UTC().Add(time.Minute),
		Capability: "browser.open", Policy: controlentity.Policy{Risk: controlentity.RiskMedium, Decision: controlentity.Allow},
	}
	observation := &controlentity.Observation{
		Protocol: controlentity.Protocol, Type: controlentity.TypeObservation, TaskID: "task-1", ActionID: "action-1",
		SessionID: "athena-browser-1", Sequence: 1, Status: controlentity.ObservationWaitingUser, ObservedAt: time.Now().UTC(),
		Error: "Google blocked the automated browser with an unusual-traffic verification page.",
		State: map[string]any{
			"url":                "https://www.google.com/sorry/index",
			"title":              "Sorry",
			"challenge_detected": true,
			"challenge": map[string]any{
				"kind":                   "google_unusual_traffic",
				"message":                "Google blocked the automated browser with an unusual-traffic verification page.",
				"requires_user_takeover": true,
			},
		},
	}

	prompt := nextDeviceObservationPrompt("open YouTube", action, observation)
	for _, want := range []string{
		`"status": "WAITING_USER"`,
		`"challenge_detected": true`,
		"google_unusual_traffic",
		"not the requested destination content",
		"do not claim success",
		"complete verification",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestFailedControlObservationCarriesActionAndFriendlyOfflineMessage(t *testing.T) {
	action := &controlentity.Action{
		Protocol: controlentity.Protocol, Type: controlentity.TypeAction, TaskID: "task-1", ActionID: "action-1",
		SessionID: "athena-existing", Sequence: 2, Capability: "browser.open",
	}
	message := desktopOfflineMessage(action.Capability, "device-1")
	observation := failedControlObservation(action, message, map[string]any{"connected": false})

	if observation.Status != controlentity.ObservationFailed {
		t.Fatalf("status = %q", observation.Status)
	}
	if observation.TaskID != action.TaskID || observation.ActionID != action.ActionID || observation.Sequence != action.Sequence {
		t.Fatalf("observation did not preserve action identity: %+v", observation)
	}
	if observation.State["connected"] != false {
		t.Fatalf("state = %#v", observation.State)
	}
	for _, want := range []string{"Athena Desktop is not connected", "browser.open", "device-1", "control.device_token"} {
		if !strings.Contains(observation.Error, want) {
			t.Fatalf("offline message missing %q: %s", want, observation.Error)
		}
	}
}

func TestDesktopBoundToAnotherUserMessageDoesNotClaimOffline(t *testing.T) {
	message := desktopBoundToAnotherUserMessage("browser.open", "device-1")
	for _, want := range []string{"is connected", "bound to another user", "browser.open", "device-1"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message missing %q: %s", want, message)
		}
	}
	if strings.Contains(message, "not connected") {
		t.Fatalf("bound device message should not claim offline: %s", message)
	}
}

func TestHydrateControlContextFromConnectedDevice(t *testing.T) {
	hub := controlsvc.NewHub()
	connection := &testConnection{}
	if err := hub.Register(context.Background(), controlentity.DeviceMessage{
		DeviceID: "device-1", Capabilities: []string{"browser.hover", "app.open"},
	}, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ResolveDevice(context.Background(), "user-1", "", "browser.hover"); err != nil {
		t.Fatal(err)
	}
	service := &RuntimeService{controlHub: hub}
	values := map[string]any{}
	service.hydrateControlContext(authctx.WithUserID(context.Background(), "user-1"), values)
	if values["browser_controller"] != true || values["desktop_bridge"] != true {
		t.Fatalf("control context not hydrated: %#v", values)
	}
}

type testConnection struct{}

func (testConnection) Send(any) error { return nil }
func (testConnection) Close() error   { return nil }
