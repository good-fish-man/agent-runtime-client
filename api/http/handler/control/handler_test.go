package control

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"

	service "github.com/good-fish-man/agent-runtime-client/application/service/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

func protocolTestAction(taskID, actionID, capability string) entity.Action {
	now := time.Now().UTC()
	stepID := "step-" + actionID
	return entity.Action{
		Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: taskID, StepID: stepID, ActionID: actionID,
		Sequence: 1, Revision: 1, IdempotencyKey: taskID + ":" + stepID + ":" + actionID,
		IssuedAt: now, Deadline: now.Add(5 * time.Second), Capability: capability,
		Policy: entity.Policy{Risk: entity.RiskLow, Decision: entity.Allow},
	}
}

func TestDeviceWebSocketActionObservationRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := service.NewHub()
	engine := gin.New()
	NewHandler(hub, "device-secret").Register(engine, nil)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local sockets are unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(engine)
	server.Listener = listener
	server.Start()
	defer server.Close()

	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/control/device", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	config.Header = http.Header{"Authorization": []string{"Bearer device-secret"}}
	socket, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := websocket.JSON.Send(socket, entity.DeviceMessage{
		Protocol: entity.Protocol, Type: entity.TypeHello, DeviceID: "device-1",
		Capabilities: []string{"browser.open"},
	}); err != nil {
		t.Fatal(err)
	}
	var welcome map[string]any
	if err := websocket.JSON.Receive(socket, &welcome); err != nil {
		t.Fatal(err)
	}

	action := protocolTestAction("task-1", "action-1", "browser.open")
	observationChannel := make(chan *entity.Observation, 1)
	errorChannel := make(chan error, 1)
	go func() {
		observation, dispatchErr := hub.Dispatch(context.Background(), "device-1", action)
		if dispatchErr != nil {
			errorChannel <- dispatchErr
			return
		}
		observationChannel <- observation
	}()

	var received entity.Action
	if err := websocket.JSON.Receive(socket, &received); err != nil {
		t.Fatal(err)
	}
	if received.Protocol != entity.Protocol || received.Type != entity.TypeAction || received.ActionID != action.ActionID {
		t.Fatalf("unexpected action: %#v", received)
	}
	if err := websocket.JSON.Send(socket, entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, ObservationID: entity.NewID("observation"),
		TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID,
		Sequence: action.Sequence, Revision: action.Revision, Status: "SUCCEEDED",
		ObservedAt: time.Now().UTC(), State: map[string]any{"url": "https://youtube.com"},
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errorChannel:
		t.Fatal(err)
	case observation := <-observationChannel:
		if observation.Status != "SUCCEEDED" || observation.State["url"] != "https://youtube.com" {
			t.Fatalf("unexpected observation: %#v", observation)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for observation")
	}
}

func TestDeviceWebSocketAcceptsProgressBeforeObservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := service.NewHub()
	engine := gin.New()
	NewHandler(hub, "device-secret").Register(engine, nil)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local sockets are unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(engine)
	server.Listener = listener
	server.Start()
	defer server.Close()

	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/control/device", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	config.Header = http.Header{"Authorization": []string{"Bearer device-secret"}}
	socket, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := websocket.JSON.Send(socket, entity.DeviceMessage{
		Protocol: entity.Protocol, Type: entity.TypeHello, DeviceID: "device-1",
		Capabilities: []string{"browser.download"},
	}); err != nil {
		t.Fatal(err)
	}
	var welcome map[string]any
	if err := websocket.JSON.Receive(socket, &welcome); err != nil {
		t.Fatal(err)
	}

	action := protocolTestAction("task-1", "action-1", "browser.download")
	action.Policy.Risk = entity.RiskMedium
	progressChannel := make(chan entity.Progress, 1)
	observationChannel := make(chan *entity.Observation, 1)
	errorChannel := make(chan error, 1)
	go func() {
		observation, dispatchErr := hub.Dispatch(context.Background(), "device-1", action, func(progress entity.Progress) error {
			progressChannel <- progress
			return nil
		})
		if dispatchErr != nil {
			errorChannel <- dispatchErr
			return
		}
		observationChannel <- observation
	}()

	var received entity.Action
	if err := websocket.JSON.Receive(socket, &received); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(socket, entity.Progress{
		Protocol: entity.Protocol, Type: entity.TypeProgress, TaskID: action.TaskID, StepID: action.StepID,
		ActionID: action.ActionID, Sequence: action.Sequence, Revision: action.Revision, Stage: "downloading", Progress: 55,
		SentAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case progress := <-progressChannel:
		if progress.Progress != 55 {
			t.Fatalf("unexpected progress: %#v", progress)
		}
	case observation := <-observationChannel:
		t.Fatalf("progress completed dispatch early: %#v", observation)
	case err := <-errorChannel:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for progress")
	}
	if err := websocket.JSON.Send(socket, entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, ObservationID: entity.NewID("observation"),
		TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID,
		Sequence: action.Sequence, Revision: action.Revision, Status: "SUCCEEDED",
		ObservedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorChannel:
		t.Fatal(err)
	case observation := <-observationChannel:
		if observation.Status != "SUCCEEDED" {
			t.Fatalf("unexpected observation: %#v", observation)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for observation")
	}
}

func TestDeviceWebSocketRequiresToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewHandler(service.NewHub(), "device-secret").Register(engine, nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/control/device", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestDeviceWebSocketAllowsLoopbackWhenTokenUnset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := service.NewHub()
	engine := gin.New()
	NewHandler(hub, "").Register(engine, nil)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local sockets are unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(engine)
	server.Listener = listener
	server.Start()
	defer server.Close()

	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/control/device", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := websocket.JSON.Send(socket, entity.DeviceMessage{
		Protocol: entity.Protocol, Type: entity.TypeHello, DeviceID: "device-local",
		Capabilities: []string{"browser.open"},
	}); err != nil {
		t.Fatal(err)
	}
	var welcome map[string]any
	if err := websocket.JSON.Receive(socket, &welcome); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceWebSocketRejectsRemoteWhenTokenUnset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewHandler(service.NewHub(), "").Register(engine, nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/control/device", nil)
	request.RemoteAddr = "203.0.113.10:34567"
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), "device token required") {
		t.Fatalf("body = %q, want token required error", response.Body.String())
	}
}

func TestDeviceWebSocketRegistersPublicPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewHandler(service.NewHub(), "device-secret").Register(engine, nil, "/api/agent-runtime-client/v1")
	request := httptest.NewRequest(http.MethodGet, "/api/agent-runtime-client/v1/control/device", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want route to exist and reject without token", response.Code)
	}
}
