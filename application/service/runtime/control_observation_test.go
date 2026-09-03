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
		AgentBuildID: "build-1", RunManifestID: "manifest-1",
		SessionID: "athena-existing", Sequence: 1, IdempotencyKey: "task-1:action-1",
		Deadline: time.Now().UTC().Add(time.Minute), Capability: "browser.open",
		Arguments: map[string]any{"target": "YouTube"}, Policy: controlentity.Policy{Risk: controlentity.RiskMedium, Decision: controlentity.Allow},
	}
	observation := &controlentity.Observation{
		Protocol: controlentity.Protocol, Type: controlentity.TypeObservation, TaskID: "task-1", ActionID: "action-1",
		AgentBuildID: "build-1", RunManifestID: "manifest-1",
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
	if !ok || latest["status"] != controlentity.ObservationSucceeded || latest["run_manifest_id"] != "manifest-1" {
		t.Fatalf("latest observation = %#v", values["latest_action_observation"])
	}
	actionCtx, ok := values["latest_action"].(map[string]any)
	if !ok || actionCtx["capability"] != "browser.open" || actionCtx["agent_build_id"] != "build-1" {
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
		TraceID: "trace-control-1", AgentBuildID: "build-1", RunManifestID: "manifest-1",
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
	if observation.TraceID != action.TraceID {
		t.Fatalf("observation trace_id = %q, want %q", observation.TraceID, action.TraceID)
	}
	if observation.AgentBuildID != action.AgentBuildID || observation.RunManifestID != action.RunManifestID {
		t.Fatalf("observation deployment provenance = %+v", observation)
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

func TestAttachControlDeploymentProvenanceRequiresCompletePair(t *testing.T) {
	now := time.Now().UTC()
	action := &controlentity.Action{
		Protocol: controlentity.Protocol, Type: controlentity.TypeAction,
		TaskID: "task-1", StepID: "step-1", ActionID: "action-1",
		Sequence: 1, Revision: 1, IdempotencyKey: "task-1:step-1:action-1",
		IssuedAt: now, Deadline: now.Add(time.Minute), Capability: "browser.open",
		Policy: controlentity.Policy{Risk: controlentity.RiskReadOnly, Decision: controlentity.Allow},
	}
	if err := attachControlDeploymentProvenance(map[string]any{
		"agent_build_id": "build-1", "run_manifest_id": "manifest-1",
	}, action); err != nil {
		t.Fatal(err)
	}
	if action.AgentBuildID != "build-1" || action.RunManifestID != "manifest-1" {
		t.Fatalf("deployment provenance = %+v", action)
	}
	if err := attachControlDeploymentProvenance(map[string]any{"agent_build_id": "build-2"}, action); err == nil {
		t.Fatal("partial deployment provenance was accepted")
	}
}

func TestPersistentGoalActionIdempotencySurvivesGeneratedActionIDs(t *testing.T) {
	values := map[string]any{"idempotency_scope": "goal-1:browser-task"}
	first := controlentity.Action{ActionID: "model-action-a", StepID: "step-a", Capability: "browser.click", Operation: "click", Target: map[string]any{"semantic_id": "video-2"}, Arguments: map[string]any{"button": "left"}}
	second := first
	second.ActionID, second.StepID = "model-action-after-restart", "different-step"
	firstKey := controlActionIdempotencyKey(values, "execution-a", 2, first)
	secondKey := controlActionIdempotencyKey(values, "execution-b", 2, second)
	if firstKey != secondKey {
		t.Fatalf("same durable effect received different keys: %q != %q", firstKey, secondKey)
	}
	if third := controlActionIdempotencyKey(values, "execution-c", 3, second); third == firstKey {
		t.Fatal("a distinct action occurrence reused an idempotency key")
	}
}

func TestBrowserActionSiteScopeStoresOnlyNormalizedHostname(t *testing.T) {
	action := &controlentity.Action{
		Capability: "browser.task",
		Arguments: map[string]any{
			"target": "https://WWW.Example.COM:8443/private/path?token=secret#fragment",
			"query":  "private search terms",
		},
	}
	if got := browserActionSiteScope(action); got != "example.com" {
		t.Fatalf("site scope = %q, want example.com", got)
	}
}

func TestBrowserActionSiteScopeRejectsUnsafeOrUnrelatedValues(t *testing.T) {
	tests := []controlentity.Action{
		{Capability: "browser.task", Arguments: map[string]any{"target": "YouTube"}},
		{Capability: "browser.open", Arguments: map[string]any{"url": "https://user:secret@example.com/private"}},
		{Capability: "app.open", Arguments: map[string]any{"url": "https://example.com"}},
	}
	for _, action := range tests {
		if got := browserActionSiteScope(&action); got != "" {
			t.Fatalf("site scope for %#v = %q, want empty", action.Arguments, got)
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
