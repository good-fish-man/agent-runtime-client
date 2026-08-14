package operations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

func TestReadinessAggregatesRuntimeRecoveryAndDurableServices(t *testing.T) {
	now := time.Now().UTC()
	runtimeHealth := operationsv1.HealthSnapshot{Schema: operationsv1.Schema, Component: "agent-runtime", Version: "1.0.0", InstanceID: "runtime-1", Status: operationsv1.HealthHealthy, ObservedAt: now}
	runtimeSLO := operationsv1.SLOSnapshot{Schema: operationsv1.Schema, Component: "agent-runtime", WindowStart: now.Add(-time.Minute), WindowEnd: now, Availability: 1}
	runtimeReadiness := ga.ReadinessReport{
		Schema: ga.Schema, ReleaseVersion: "1.0.0", Component: "agent-runtime", InstanceID: "runtime-1", Status: ga.StatusPass,
		Checks: []ga.ReadinessCheck{{ID: "protocol", Category: "compatibility", Status: ga.StatusPass, Required: true, Message: "stable"}}, ObservedAt: now,
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload any
		switch request.URL.Path {
		case "/metrics":
			payload = map[string]any{"health": runtimeHealth, "slo": runtimeSLO}
		case "/readiness":
			payload = runtimeReadiness
		default:
			t.Fatalf("unexpected runtime path %q", request.URL.Path)
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}
	backup := newBackupManagerForTest(t.TempDir(), t.TempDir()+"/key", nil)
	service := New("http://runtime.local", func(context.Context) error { return nil }, deviceSource{{ID: "device-1", Online: true, LeaseExpiresAt: now.Add(time.Minute)}}).
		WithHTTPClient(client).
		WithBackupManager(backup).
		WithGAConfig(GAConfig{DataStore: true, Conversation: true, Memory: true, Knowledge: true, Deployment: true, Orchestration: true, GoalSupervisor: true, PluginRegistry: true})
	report := service.Readiness(context.Background(), "user-1")
	if report.Status != ga.StatusExternalRequired {
		t.Fatalf("status = %s, want EXTERNAL_REQUIRED: %+v", report.Status, report.Checks)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	results := service.RunGoldenJourneys(context.Background(), "user-1")
	if len(results) != 10 || len(service.LastGoldenJourneyResults()) != 10 {
		t.Fatalf("golden results = %d", len(results))
	}
	for _, result := range results {
		if err := result.Validate(); err != nil {
			t.Fatalf("journey %s: %v", result.JourneyID, err)
		}
		if (result.JourneyID == "install-mode" || result.JourneyID == "safe-upgrade") && result.Status != ga.StatusExternalRequired {
			t.Fatalf("journey %s must remain an external release gate, got %s", result.JourneyID, result.Status)
		}
	}
}

func TestReadinessFailsClosedWhenRuntimeAndDataStoreAreUnavailable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})}
	service := New("http://runtime.local", nil, deviceSource([]controlsvc.Device{})).WithHTTPClient(client)
	report := service.Readiness(context.Background(), "user-1")
	if report.Status != ga.StatusFail {
		t.Fatalf("status = %s, want FAIL", report.Status)
	}
}
