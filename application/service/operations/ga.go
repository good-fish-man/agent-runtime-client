package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

type GAConfig struct {
	DataStore      bool
	Conversation   bool
	Memory         bool
	Knowledge      bool
	Deployment     bool
	Orchestration  bool
	GoalSupervisor bool
	PluginRegistry bool
}

func (s *Service) WithGAConfig(cfg GAConfig) *Service {
	s.gaConfig = cfg
	return s
}

func (s *Service) Readiness(ctx context.Context, userID string) ga.ReadinessReport {
	snapshot := s.Snapshot(ctx, userID)
	checks := make([]ga.ReadinessCheck, 0, 16)
	if consts.Version != ga.ReleaseVersion {
		checks = append(checks, gaCheck("protocol.freeze", "compatibility", ga.StatusFail, "control-plane version does not match the frozen GA release"))
	} else {
		checks = append(checks, gaCheck("protocol.freeze", "compatibility", ga.StatusPass, "compiled GA protocol and control-plane versions match the frozen release"))
	}
	checks = append(checks, gaCheck("frontend.independent", "durability", ga.StatusPass, "device control and goal supervision run outside the frontend lifecycle"))

	runtimeReport, runtimeErr := s.runtimeReadiness(ctx)
	if runtimeErr != nil {
		checks = append(checks, gaCheck("runtime.readiness", "runtime", ga.StatusFail, runtimeErr.Error()))
	} else if runtimeReport.Status != ga.StatusPass {
		checks = append(checks, gaCheck("runtime.readiness", "runtime", runtimeReport.Status, "runtime reported unmet GA invariants"))
	} else {
		checks = append(checks, gaCheck("runtime.readiness", "runtime", ga.StatusPass, "runtime GA invariants are satisfied"))
	}

	databaseStatus := healthCheckStatus(snapshot.Health.Checks, "database")
	if !s.gaConfig.DataStore || databaseStatus != operationsv1.HealthHealthy {
		checks = append(checks, gaCheck("database.durable", "data", ga.StatusFail, "durable control-plane database is unavailable"))
	} else {
		checks = append(checks, gaCheck("database.durable", "data", ga.StatusPass, "durable control-plane database is healthy"))
	}

	if s.backup == nil {
		checks = append(checks, gaCheck("recovery.backup", "recovery", ga.StatusFail, "encrypted backup management is not configured"))
	} else if backups, err := s.backup.List(); err != nil {
		checks = append(checks, gaCheck("recovery.backup", "recovery", ga.StatusFail, "backup inventory failed: "+err.Error()))
	} else if len(backups) == 0 {
		checks = append(checks, gaCheck("recovery.backup", "recovery", ga.StatusExternalRequired, "create and verify a backup before declaring GA readiness"))
	} else {
		checks = append(checks, gaCheck("recovery.backup", "recovery", ga.StatusPass, fmt.Sprintf("%d encrypted backup(s) are retained", len(backups))))
	}

	if !s.gaConfig.Orchestration || !s.gaConfig.GoalSupervisor {
		checks = append(checks, gaCheck("goals.background", "durability", ga.StatusFail, "durable goal supervision is not active"))
	} else {
		checks = append(checks, gaCheck("goals.background", "durability", ga.StatusPass, "durable goals can continue without an open frontend"))
	}

	if snapshot.OnlineDevices > 0 {
		checks = append(checks, gaCheck("device.control", "device", ga.StatusPass, "an exclusively leased desktop device is online"))
	} else {
		checks = append(checks, gaCheck("device.control", "device", ga.StatusExternalRequired, "connect a desktop device to validate browser and desktop journeys"))
	}

	latestJourneys, journeyErr := s.LastGoldenJourneyResults(ctx, userID)
	e2eJourneys, e2eErr := s.lastGoldenJourneyResults(ctx, userID, ga.VerificationE2E)
	journeys := latestJourneys
	if len(e2eJourneys) > 0 {
		journeys = e2eJourneys
	}
	if journeyErr == nil {
		journeyErr = e2eErr
	}
	if journeyErr != nil {
		checks = append(checks, gaCheck("golden.evidence-store", "verification", ga.StatusFail, "read persisted golden journey evidence: "+journeyErr.Error()))
	} else if s.gaStore == nil && s.gaConfig.DataStore {
		checks = append(checks, gaCheck("golden.evidence-store", "verification", ga.StatusFail, "durable golden journey evidence storage is not configured"))
	} else {
		checks = append(checks, gaCheck("golden.evidence-store", "verification", ga.StatusPass, "golden journey evidence is owner-scoped and integrity checked"))
	}
	suiteStatus, suiteMessage := goldenSuiteStatus(e2eJourneys)
	checks = append(checks, gaCheck("golden.suite", "verification", suiteStatus, suiteMessage))
	traceCoverage := traceCoverageFromJourneys(e2eJourneys)
	if traceCoverage == nil {
		checks = append(checks, gaCheck("trace.provenance", "traceability", ga.StatusNotRun, "no passing E2E suite yet proves build-to-observation trace continuity"))
	} else {
		checks = append(checks, ga.ReadinessCheck{
			ID: "trace.provenance", Category: "traceability", Status: ga.StatusPass, Required: true,
			Message:  "a passing E2E suite preserves build, manifest, capability, action, and observation identities",
			Evidence: []ga.EvidenceRef{{Kind: "golden_run_id", Reference: journeys[0].RunID}},
		})
	}
	checks = append(checks, s.domainReadinessChecks()...)
	return ga.ReadinessReport{
		Schema: ga.Schema, ReleaseVersion: consts.Version, Component: consts.ServiceName,
		InstanceID: s.instanceID, Status: aggregateGAStatus(checks), Checks: checks,
		Journeys: journeys, TraceCoverage: traceCoverage, ObservedAt: time.Now().UTC(),
	}
}

func traceCoverageFromJourneys(results []ga.GoldenJourneyResult) *ga.TraceCoverage {
	if len(results) != len(ga.GoldenJourneys()) {
		return nil
	}
	values := map[string]string{}
	for _, result := range results {
		if result.VerificationLevel != ga.VerificationE2E || result.Status != ga.StatusPass {
			return nil
		}
		for _, step := range result.Steps {
			for _, evidence := range step.Evidence {
				if values[evidence.Kind] == "" {
					values[evidence.Kind] = evidence.Reference
				}
			}
		}
	}
	coverage := &ga.TraceCoverage{
		AgentBuildID: values["agent_build_id"], RunManifestID: values["run_manifest_id"],
		CapabilityID: values["capability_id"], ActionID: values["action_id"], ObservationID: values["observation_id"],
	}
	if coverage.Validate() != nil {
		return nil
	}
	return coverage
}

func goldenSuiteStatus(results []ga.GoldenJourneyResult) (string, string) {
	if len(results) == 0 {
		return ga.StatusNotRun, "no complete E2E Golden Journey suite has been recorded"
	}
	status := ga.StatusPass
	for _, result := range results {
		if result.VerificationLevel != ga.VerificationE2E {
			return ga.StatusFail, "stored Golden Journey evidence is not an E2E suite"
		}
		switch result.Status {
		case ga.StatusFail:
			return ga.StatusFail, "one or more required E2E Golden Journeys failed"
		case ga.StatusBlocked:
			status = ga.StatusBlocked
		case ga.StatusExternalRequired:
			if status == ga.StatusPass {
				status = ga.StatusExternalRequired
			}
		case ga.StatusNotRun:
			if status == ga.StatusPass {
				status = ga.StatusNotRun
			}
		}
	}
	if status == ga.StatusPass {
		return status, "all ten required E2E Golden Journeys passed with catalog evidence"
	}
	return status, "the latest E2E Golden Journey suite is incomplete"
}

func (s *Service) GoldenJourneyCatalog() []ga.GoldenJourney { return ga.GoldenJourneys() }

// RunGoldenJourneys performs a deterministic, non-destructive preflight. It
// proves that each journey has the required service and device infrastructure;
// external package signatures and real third-party UI flows remain explicit
// release gates rather than being reported as unit-test successes.
func (s *Service) RunGoldenJourneys(ctx context.Context, userID string) ([]ga.GoldenJourneyResult, error) {
	snapshot := s.Snapshot(ctx, userID)
	runtimeReady := false
	if report, err := s.runtimeReadiness(ctx); err == nil && report.Status == ga.StatusPass {
		runtimeReady = true
	}
	availability := map[string]journeyAvailability{
		"install-mode":         {false, ga.StatusExternalRequired, "signed installer and Local/Remote selection require a packaged release test"},
		"identity-model-agent": {s.gaConfig.DataStore, ga.StatusFail, "identity, model, and agent persistence require the control-plane database"},
		"observable-chat":      {s.gaConfig.Conversation && runtimeReady, ga.StatusFail, "conversation execution and runtime telemetry must be available"},
		"browser-handoff":      {snapshot.OnlineDevices > 0, ga.StatusExternalRequired, "connect a desktop device for a real browser handoff"},
		"desktop-control":      {snapshot.OnlineDevices > 0, ga.StatusExternalRequired, "connect a desktop device for file and app controls"},
		"evidence-research":    {s.gaConfig.Knowledge && runtimeReady, ga.StatusFail, "knowledge evidence storage and runtime research must be available"},
		"durable-goal":         {s.gaConfig.Orchestration && s.gaConfig.GoalSupervisor, ga.StatusFail, "durable goal supervisor must be active"},
		"memory-control":       {s.gaConfig.Memory && s.gaConfig.DataStore, ga.StatusFail, "memory controls require durable storage"},
		"governed-improvement": {s.gaConfig.Deployment && s.gaConfig.PluginRegistry, ga.StatusFail, "deployment governance and plugin registry must be active"},
		"safe-upgrade":         {false, ga.StatusExternalRequired, "create and verify a backup, then run the signed package upgrade matrix"},
	}

	now := time.Now().UTC()
	runID := "preflight-" + ulid.New()
	results := make([]ga.GoldenJourneyResult, 0, len(ga.GoldenJourneys()))
	for _, journey := range ga.GoldenJourneys() {
		available := availability[journey.ID]
		status, message := ga.StatusNotRun, "infrastructure is ready; the E2E journey has not been executed"
		if !available.ready {
			status, message = available.missingStatus, available.message
		}
		steps := make([]ga.GoldenJourneyStepResult, 0, len(journey.Steps))
		for _, step := range journey.Steps {
			steps = append(steps, ga.GoldenJourneyStepResult{
				StepID: step.ID, Status: status, Message: message, DurationMS: 0,
				Evidence: []ga.EvidenceRef{{Kind: "preflight_capability", Reference: step.Capability}},
			})
		}
		results = append(results, ga.GoldenJourneyResult{
			RunID: runID, JourneyID: journey.ID, VerificationLevel: ga.VerificationPreflight,
			Status: status, Steps: steps, StartedAt: now, FinishedAt: time.Now().UTC(),
		})
	}
	if err := validateGoldenJourneySuite(results, ga.VerificationPreflight); err != nil {
		return nil, err
	}
	if err := s.saveGoldenJourneyResults(ctx, userID, results); err != nil {
		return nil, err
	}
	return results, nil
}

// RecordGoldenJourneyResults accepts evidence from an independent E2E runner.
// The runner cannot redefine the catalog or omit a step from a passing result.
func (s *Service) RecordGoldenJourneyResults(ctx context.Context, userID string, results []ga.GoldenJourneyResult) error {
	if s.gaStore == nil {
		return fmt.Errorf("durable golden journey evidence storage is not configured")
	}
	if err := validateGoldenJourneySuite(results, ga.VerificationE2E); err != nil {
		return err
	}
	return s.gaStore.SaveGoldenJourneyResults(ctx, userID, results)
}

func validateGoldenJourneySuite(results []ga.GoldenJourneyResult, level string) error {
	return ga.ValidateGoldenJourneySuite(results, level)
}

func (s *Service) saveGoldenJourneyResults(ctx context.Context, userID string, results []ga.GoldenJourneyResult) error {
	if s.gaStore != nil {
		return s.gaStore.SaveGoldenJourneyResults(ctx, userID, results)
	}
	s.gaMu.Lock()
	s.gaRuns[userID] = append(s.gaRuns[userID], append([]ga.GoldenJourneyResult(nil), results...))
	s.gaMu.Unlock()
	return nil
}

func (s *Service) LastGoldenJourneyResults(ctx context.Context, userID string) ([]ga.GoldenJourneyResult, error) {
	return s.lastGoldenJourneyResults(ctx, userID, "")
}

func (s *Service) lastGoldenJourneyResults(ctx context.Context, userID, verificationLevel string) ([]ga.GoldenJourneyResult, error) {
	if s.gaStore != nil {
		return s.gaStore.LastGoldenJourneyResults(ctx, userID, verificationLevel)
	}
	s.gaMu.RLock()
	defer s.gaMu.RUnlock()
	runs := s.gaRuns[userID]
	for index := len(runs) - 1; index >= 0; index-- {
		if verificationLevel == "" || (len(runs[index]) > 0 && runs[index][0].VerificationLevel == verificationLevel) {
			return append([]ga.GoldenJourneyResult(nil), runs[index]...), nil
		}
	}
	return nil, nil
}

type journeyAvailability struct {
	ready         bool
	missingStatus string
	message       string
}

func (s *Service) runtimeReadiness(ctx context.Context) (*ga.ReadinessReport, error) {
	if s.runtimeURL == "" {
		return nil, fmt.Errorf("runtime endpoint is not configured")
	}
	endpoint, err := url.JoinPath(s.runtimeURL, "readiness")
	if err != nil {
		return nil, fmt.Errorf("build runtime readiness URL: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create runtime readiness request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("runtime readiness request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime readiness returned status %d", response.StatusCode)
	}
	var report ga.ReadinessReport
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&report); err != nil {
		return nil, fmt.Errorf("decode runtime readiness: %w", err)
	}
	if err := report.Validate(); err != nil {
		return nil, fmt.Errorf("validate runtime readiness: %w", err)
	}
	return &report, nil
}

func (s *Service) domainReadinessChecks() []ga.ReadinessCheck {
	values := []struct {
		id      string
		enabled bool
	}{
		{"conversation", s.gaConfig.Conversation},
		{"memory", s.gaConfig.Memory},
		{"knowledge", s.gaConfig.Knowledge},
		{"deployment", s.gaConfig.Deployment},
		{"plugin-registry", s.gaConfig.PluginRegistry},
	}
	sort.Slice(values, func(i, j int) bool { return values[i].id < values[j].id })
	checks := make([]ga.ReadinessCheck, 0, len(values))
	for _, value := range values {
		status, message := ga.StatusPass, value.id+" service is available"
		if !value.enabled {
			status, message = ga.StatusFail, value.id+" service is unavailable"
		}
		checks = append(checks, gaCheck("domain."+value.id, "capability", status, message))
	}
	return checks
}

func healthCheckStatus(checks []operationsv1.HealthCheck, name string) string {
	for _, check := range checks {
		if check.Name == name {
			return check.Status
		}
	}
	return operationsv1.HealthUnhealthy
}

func gaCheck(id, category, status, message string) ga.ReadinessCheck {
	return ga.ReadinessCheck{ID: id, Category: category, Status: status, Required: true, Message: message}
}

func aggregateGAStatus(checks []ga.ReadinessCheck) string {
	result := ga.StatusPass
	for _, check := range checks {
		if !check.Required {
			continue
		}
		switch check.Status {
		case ga.StatusFail:
			return ga.StatusFail
		case ga.StatusBlocked:
			result = ga.StatusBlocked
		case ga.StatusExternalRequired:
			if result == ga.StatusPass {
				result = ga.StatusExternalRequired
			}
		case ga.StatusNotRun:
			if result == ga.StatusPass {
				result = ga.StatusNotRun
			}
		}
	}
	return result
}
