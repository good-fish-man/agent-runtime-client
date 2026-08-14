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

type memoryGAEvidenceStore struct {
	byOwner map[string][][]ga.GoldenJourneyResult
}

func newMemoryGAEvidenceStore() *memoryGAEvidenceStore {
	return &memoryGAEvidenceStore{byOwner: make(map[string][][]ga.GoldenJourneyResult)}
}

func (s *memoryGAEvidenceStore) SaveGoldenJourneyResults(_ context.Context, ownerID string, results []ga.GoldenJourneyResult) error {
	s.byOwner[ownerID] = append(s.byOwner[ownerID], append([]ga.GoldenJourneyResult(nil), results...))
	return nil
}

func (s *memoryGAEvidenceStore) LastGoldenJourneyResults(_ context.Context, ownerID, verificationLevel string) ([]ga.GoldenJourneyResult, error) {
	runs := s.byOwner[ownerID]
	for index := len(runs) - 1; index >= 0; index-- {
		if verificationLevel == "" || (len(runs[index]) > 0 && runs[index][0].VerificationLevel == verificationLevel) {
			return append([]ga.GoldenJourneyResult(nil), runs[index]...), nil
		}
	}
	return nil, nil
}

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
	evidenceStore := newMemoryGAEvidenceStore()
	service := New("http://runtime.local", func(context.Context) error { return nil }, deviceSource{{ID: "device-1", Online: true, LeaseExpiresAt: now.Add(time.Minute)}}).
		WithHTTPClient(client).
		WithBackupManager(backup).
		WithGAEvidenceStore(evidenceStore).
		WithGAConfig(GAConfig{DataStore: true, Conversation: true, Memory: true, Knowledge: true, Deployment: true, Orchestration: true, GoalSupervisor: true, PluginRegistry: true})
	report := service.Readiness(context.Background(), "user-1")
	if report.Status != ga.StatusExternalRequired {
		t.Fatalf("status = %s, want EXTERNAL_REQUIRED: %+v", report.Status, report.Checks)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	results, err := service.RunGoldenJourneys(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	lastResults, err := service.LastGoldenJourneyResults(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 10 || len(lastResults) != 10 {
		t.Fatalf("golden results = %d", len(results))
	}
	for _, result := range results {
		journey, _ := ga.GoldenJourneyByID(result.JourneyID)
		if err := result.ValidateAgainst(journey); err != nil {
			t.Fatalf("journey %s: %v", result.JourneyID, err)
		}
		if result.VerificationLevel != ga.VerificationPreflight || result.Status == ga.StatusPass {
			t.Fatalf("preflight journey %s reported level=%s status=%s", result.JourneyID, result.VerificationLevel, result.Status)
		}
		if (result.JourneyID == "install-mode" || result.JourneyID == "safe-upgrade") && result.Status != ga.StatusExternalRequired {
			t.Fatalf("journey %s must remain an external release gate, got %s", result.JourneyID, result.Status)
		}
	}
}

func TestRecordGoldenJourneyResultsRequiresCompleteE2EEvidence(t *testing.T) {
	store := newMemoryGAEvidenceStore()
	service := New("", nil, nil).WithGAEvidenceStore(store)
	results := completeE2EResults("e2e-run-1")
	if err := service.RecordGoldenJourneyResults(context.Background(), "owner-1", results); err != nil {
		t.Fatal(err)
	}
	latest := store.byOwner["owner-1"][len(store.byOwner["owner-1"])-1]
	if len(latest) != len(ga.GoldenJourneys()) {
		t.Fatal("complete E2E suite was not persisted")
	}
	if coverage := traceCoverageFromJourneys(latest); coverage == nil {
		t.Fatal("complete E2E suite did not produce build-to-observation trace coverage")
	}
	results[0].Steps[0].Evidence = results[0].Steps[0].Evidence[1:]
	if err := service.RecordGoldenJourneyResults(context.Background(), "owner-1", results); err == nil {
		t.Fatal("expected missing E2E evidence to be rejected")
	}
}

func TestPreflightDoesNotHideLastE2ESuite(t *testing.T) {
	store := newMemoryGAEvidenceStore()
	service := New("", nil, nil).WithGAEvidenceStore(store)
	e2e := completeE2EResults("e2e-run-1")
	if err := service.RecordGoldenJourneyResults(context.Background(), "owner-1", e2e); err != nil {
		t.Fatal(err)
	}
	preflight := preflightResults("preflight-run-2")
	if err := service.saveGoldenJourneyResults(context.Background(), "owner-1", preflight); err != nil {
		t.Fatal(err)
	}
	latest, err := service.LastGoldenJourneyResults(context.Background(), "owner-1")
	if err != nil || len(latest) == 0 || latest[0].VerificationLevel != ga.VerificationPreflight {
		t.Fatalf("latest run = %+v, error = %v", latest, err)
	}
	verified, err := service.lastGoldenJourneyResults(context.Background(), "owner-1", ga.VerificationE2E)
	if err != nil || len(verified) == 0 || verified[0].RunID != "e2e-run-1" {
		t.Fatalf("last E2E run = %+v, error = %v", verified, err)
	}
	if status, _ := goldenSuiteStatus(verified); status != ga.StatusPass {
		t.Fatalf("golden suite status = %s, want PASS", status)
	}
}

func completeE2EResults(runID string) []ga.GoldenJourneyResult {
	now := time.Now().UTC()
	results := make([]ga.GoldenJourneyResult, 0, len(ga.GoldenJourneys()))
	for _, journey := range ga.GoldenJourneys() {
		steps := make([]ga.GoldenJourneyStepResult, 0, len(journey.Steps))
		for _, step := range journey.Steps {
			evidence := make([]ga.EvidenceRef, 0, len(step.ExpectedEvidence))
			for _, kind := range step.ExpectedEvidence {
				evidence = append(evidence, ga.EvidenceRef{Kind: kind, Reference: kind + "-value"})
			}
			steps = append(steps, ga.GoldenJourneyStepResult{StepID: step.ID, Status: ga.StatusPass, Message: "verified", Evidence: evidence, DurationMS: 1})
		}
		results = append(results, ga.GoldenJourneyResult{
			RunID: runID, JourneyID: journey.ID, VerificationLevel: ga.VerificationE2E,
			Status: ga.StatusPass, Steps: steps, StartedAt: now, FinishedAt: now.Add(time.Millisecond),
		})
	}
	return results
}

func preflightResults(runID string) []ga.GoldenJourneyResult {
	now := time.Now().UTC()
	results := make([]ga.GoldenJourneyResult, 0, len(ga.GoldenJourneys()))
	for _, journey := range ga.GoldenJourneys() {
		steps := make([]ga.GoldenJourneyStepResult, 0, len(journey.Steps))
		for _, step := range journey.Steps {
			steps = append(steps, ga.GoldenJourneyStepResult{StepID: step.ID, Status: ga.StatusNotRun, Message: "preflight only"})
		}
		results = append(results, ga.GoldenJourneyResult{
			RunID: runID, JourneyID: journey.ID, VerificationLevel: ga.VerificationPreflight,
			Status: ga.StatusNotRun, Steps: steps, StartedAt: now, FinishedAt: now,
		})
	}
	return results
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
