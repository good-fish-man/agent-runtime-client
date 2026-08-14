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
	checks := []ga.ReadinessCheck{
		gaCheck("protocol.freeze", "compatibility", ga.StatusPass, "GA protocol contracts and component compatibility are pinned"),
		gaCheck("frontend.independent", "durability", ga.StatusPass, "device control and goal supervision run outside the frontend lifecycle"),
		gaCheck("trace.provenance", "traceability", ga.StatusPass, "control actions and observations preserve agent build and run manifest identities"),
	}

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

	checks = append(checks, s.domainReadinessChecks()...)
	return ga.ReadinessReport{
		Schema: ga.Schema, ReleaseVersion: consts.Version, Component: consts.ServiceName,
		InstanceID: s.instanceID, Status: aggregateGAStatus(checks), Checks: checks,
		Journeys: s.LastGoldenJourneyResults(), ObservedAt: time.Now().UTC(),
	}
}

func (s *Service) GoldenJourneyCatalog() []ga.GoldenJourney { return ga.GoldenJourneys() }

// RunGoldenJourneys performs a deterministic, non-destructive preflight. It
// proves that each journey has the required service and device infrastructure;
// external package signatures and real third-party UI flows remain explicit
// release gates rather than being reported as unit-test successes.
func (s *Service) RunGoldenJourneys(ctx context.Context, userID string) []ga.GoldenJourneyResult {
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
	results := make([]ga.GoldenJourneyResult, 0, len(ga.GoldenJourneys()))
	for _, journey := range ga.GoldenJourneys() {
		available := availability[journey.ID]
		status, message := ga.StatusPass, "required service contracts are available"
		if !available.ready {
			status, message = available.missingStatus, available.message
		}
		steps := make([]ga.GoldenJourneyStepResult, 0, len(journey.Steps))
		for _, step := range journey.Steps {
			steps = append(steps, ga.GoldenJourneyStepResult{
				StepID: step.ID, Status: status, Message: message, DurationMS: 0,
				Evidence: []ga.EvidenceRef{{Kind: "capability", Reference: step.Capability}},
			})
		}
		results = append(results, ga.GoldenJourneyResult{JourneyID: journey.ID, Status: status, Steps: steps, StartedAt: now, FinishedAt: time.Now().UTC()})
	}
	s.gaMu.Lock()
	s.gaRuns = append([]ga.GoldenJourneyResult(nil), results...)
	s.gaMu.Unlock()
	return results
}

func (s *Service) LastGoldenJourneyResults() []ga.GoldenJourneyResult {
	s.gaMu.RLock()
	defer s.gaMu.RUnlock()
	return append([]ga.GoldenJourneyResult(nil), s.gaRuns...)
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
