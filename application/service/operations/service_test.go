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
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type deviceSource []controlsvc.Device

func (d deviceSource) Devices(context.Context, string) ([]controlsvc.Device, error) { return d, nil }

type delegationDiagnosticsFunc func(context.Context, time.Time, time.Time) (dso.OperationalSLOSnapshot, error)

func (f delegationDiagnosticsFunc) Diagnostics(ctx context.Context, startedAt, endedAt time.Time) (dso.OperationalSLOSnapshot, error) {
	return f(ctx, startedAt, endedAt)
}

func TestSnapshotAggregatesRuntimeDatabaseAndDeviceHealth(t *testing.T) {
	now := time.Now().UTC()
	runtimeHealth := operationsv1.HealthSnapshot{
		Schema: operationsv1.Schema, Component: "agent-runtime", Version: "0.9.0", InstanceID: "runtime-a",
		Status: operationsv1.HealthHealthy, ObservedAt: now,
	}
	runtimeSLO := operationsv1.SLOSnapshot{
		Schema: operationsv1.Schema, Component: "agent-runtime", WindowStart: now.Add(-time.Minute), WindowEnd: now,
		Requests: 10, Availability: 1, P95LatencyMS: 25,
	}
	payload, err := json.Marshal(map[string]any{"health": runtimeHealth, "slo": runtimeSLO})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/metrics" {
			t.Fatalf("runtime path = %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
	})}
	service := New("http://runtime.local", func(context.Context) error { return nil }, deviceSource{{ID: "device-1", Online: true, LeaseExpiresAt: now.Add(time.Minute)}}).WithHTTPClient(client)
	snapshot := service.Snapshot(context.Background(), "user-1")
	if snapshot.Health.Status != operationsv1.HealthHealthy || snapshot.OnlineDevices != 1 || snapshot.SLO == nil || snapshot.SLO.Requests != 10 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := snapshot.Health.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotFailsClosedWhenRuntimeIsUnavailable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})}
	snapshot := New("http://runtime.local", nil, deviceSource{}).WithHTTPClient(client).Snapshot(context.Background(), "user-1")
	if snapshot.Health.Status != operationsv1.HealthUnhealthy || snapshot.RuntimeHealth != nil || snapshot.SLO != nil {
		t.Fatalf("runtime outage did not fail closed: %+v", snapshot)
	}
}

func TestSnapshotDegradesWhenDelegationSafetySLOIsViolated(t *testing.T) {
	now := time.Now().UTC()
	runtimeHealth := operationsv1.HealthSnapshot{Schema: operationsv1.Schema, Component: "agent-runtime", Version: "0.9.0", InstanceID: "runtime-a", Status: operationsv1.HealthHealthy, ObservedAt: now}
	runtimeSLO := operationsv1.SLOSnapshot{Schema: operationsv1.Schema, Component: "agent-runtime", WindowStart: now.Add(-time.Minute), WindowEnd: now, Requests: 1, Availability: 1}
	payload, err := json.Marshal(map[string]any{"health": runtimeHealth, "slo": runtimeSLO})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
	})}
	diagnostics := delegationDiagnosticsFunc(func(_ context.Context, startedAt, endedAt time.Time) (dso.OperationalSLOSnapshot, error) {
		return dso.OperationalSLOSnapshot{Schema: dso.Schema, WindowStartedAt: startedAt, WindowEndedAt: endedAt, TotalRuns: 10, TerminalRuns: 10, DuplicateConfirmedSideEffects: 1, Availability: 1, GeneratedAt: endedAt}, nil
	})
	service := New("http://runtime.local", nil, deviceSource{{ID: "device-1", Online: true, LeaseExpiresAt: now.Add(time.Minute)}}).WithHTTPClient(client).WithDelegationDiagnostics(diagnostics)
	snapshot := service.Snapshot(context.Background(), "owner-1")
	if snapshot.DelegationSLO == nil || snapshot.Health.Status != operationsv1.HealthDegraded {
		t.Fatalf("unsafe delegation SLO did not degrade health: %+v", snapshot)
	}
}
