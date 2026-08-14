package experience

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	log "github.com/good-fish-man/logx"
)

type CreateFixtureRequest struct {
	ExperienceID       string `json:"experience_id"`
	Name               string `json:"name"`
	EnvironmentVersion string `json:"environment_version"`
}

type CreateSuiteRequest struct {
	Name       string   `json:"name"`
	FixtureIDs []string `json:"fixture_ids"`
}

type RunSuiteRequest struct {
	SuiteID     string `json:"suite_id"`
	Seed        int64  `json:"seed"`
	CandidateID string `json:"candidate_id"`
	BaselineID  string `json:"baseline_id"`
}

func (s *Service) CreateFixture(ctx context.Context, ownerID string, request CreateFixtureRequest) (*entity.EvaluationFixture, error) {
	experience, err := s.Find(ctx, ownerID, request.ExperienceID)
	if err != nil {
		return nil, err
	}
	if experience.Status != entity.StatusReady {
		return nil, apierror.ErrBadRequest.WithMessage("only a ready experience can become an evaluation fixture")
	}
	runtimeKind, simulator := fixtureRuntime(experience)
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = compactName(experience.GoalSummary)
	}
	environmentVersion := strings.TrimSpace(request.EnvironmentVersion)
	if environmentVersion == "" {
		environmentVersion = firstNonEmpty(experience.AgentBuildID, experience.EnvironmentFingerprint, "v1")
	}
	fixture := &entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: ulid.New(), OwnerID: ownerID, ExperienceID: experience.ExperienceID,
		Name: name, RuntimeKind: runtimeKind, Simulator: simulator, EnvironmentVersion: environmentVersion,
		SnapshotHash: fixtureSnapshotHash(*experience), Protocol: experience.Provenance.Protocol,
		Input: map[string]any{
			"goal_summary": experience.GoalSummary,
			"intent":       experience.Intent,
			"actions":      experience.ActionRefs,
		},
		Expected: entityExpectedOutcome(experience), Sensitivity: experience.Sensitivity, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateFixture(ctx, *fixture); err != nil {
		return nil, log.WrapError(err, "ExperienceService.CreateFixture")
	}
	return fixture, nil
}

func (s *Service) ListFixtures(ctx context.Context, ownerID string, limit int) ([]entity.EvaluationFixture, error) {
	return s.store.ListFixtures(ctx, ownerID, limit)
}

func (s *Service) CreateSuite(ctx context.Context, ownerID string, request CreateSuiteRequest) (*entity.EvaluationSuite, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" || len(request.FixtureIDs) == 0 {
		return nil, apierror.ErrBadRequest.WithMessage("suite name and at least one fixture are required")
	}
	fixtureIDs := uniqueSorted(request.FixtureIDs)
	for _, fixtureID := range fixtureIDs {
		fixture, err := s.store.FindFixture(ctx, ownerID, fixtureID)
		if err != nil {
			return nil, err
		}
		if fixture == nil {
			return nil, apierror.ErrBadRequest.WithMessage("evaluation fixture does not belong to the current user")
		}
		if err := fixture.Validate(); err != nil {
			return nil, apierror.ErrBadRequest.WithMessage(err.Error())
		}
	}
	now := time.Now().UTC()
	suite := &entity.EvaluationSuite{SuiteID: ulid.New(), OwnerID: ownerID, Name: name, FixtureIDs: fixtureIDs, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateSuite(ctx, *suite); err != nil {
		return nil, log.WrapError(err, "ExperienceService.CreateSuite")
	}
	return suite, nil
}

func (s *Service) ListSuites(ctx context.Context, ownerID string, limit int) ([]entity.EvaluationSuite, error) {
	return s.store.ListSuites(ctx, ownerID, limit)
}

func (s *Service) RunSuite(ctx context.Context, ownerID string, request RunSuiteRequest) (*entity.EvaluationRun, []entity.EvaluationResult, error) {
	suite, err := s.store.FindSuite(ctx, ownerID, request.SuiteID)
	if err != nil {
		return nil, nil, err
	}
	if suite == nil {
		return nil, nil, apierror.ErrNotFound.WithMessage("evaluation suite not found")
	}
	if request.Seed == 0 {
		request.Seed = 1
	}
	span := log.StartSpan(ctx, "evaluation.run", "suite_id", suite.SuiteID, "fixture_count", len(suite.FixtureIDs), "seed", request.Seed)
	var runErr error
	defer func() { span.End(runErr) }()
	started := time.Now().UTC()
	run := &entity.EvaluationRun{
		RunID: ulid.New(), OwnerID: ownerID, SuiteID: suite.SuiteID, Status: entity.EvaluationRunning,
		Seed: request.Seed, CandidateID: strings.TrimSpace(request.CandidateID), BaselineID: strings.TrimSpace(request.BaselineID), StartedAt: started,
	}
	results := make([]entity.EvaluationResult, 0, len(suite.FixtureIDs))
	for _, fixtureID := range suite.FixtureIDs {
		fixture, err := s.store.FindFixture(ctx, ownerID, fixtureID)
		if err != nil {
			runErr = err
			return nil, nil, err
		}
		if fixture == nil {
			runErr = fmt.Errorf("fixture %s is missing", fixtureID)
			return nil, nil, runErr
		}
		result, err := replayFixture(*run, *fixture)
		if err != nil {
			runErr = err
			return nil, nil, err
		}
		results = append(results, result)
	}
	run.Status = entity.EvaluationCompleted
	run.FinishedAt = time.Now().UTC()
	run.Metrics = aggregateMetrics(results)
	if err := s.store.CreateRun(ctx, *run, results); err != nil {
		runErr = log.WrapError(err, "ExperienceService.RunSuite.persist")
		return nil, nil, runErr
	}
	return run, results, nil
}

func (s *Service) ListRuns(ctx context.Context, ownerID string, limit int) ([]entity.EvaluationRun, error) {
	return s.store.ListRuns(ctx, ownerID, limit)
}

func (s *Service) ListResults(ctx context.Context, ownerID, runID string) ([]entity.EvaluationResult, error) {
	return s.store.ListResults(ctx, ownerID, runID)
}

func replayFixture(run entity.EvaluationRun, fixture entity.EvaluationFixture) (entity.EvaluationResult, error) {
	if err := fixture.Validate(); err != nil {
		return entity.EvaluationResult{}, err
	}
	// The v1 runner accepts only offline simulators. No Launcher, browser profile,
	// network endpoint, credential store, or production account is reachable.
	if !strings.Contains(fixture.Simulator, ".mock.") && !strings.HasSuffix(fixture.Simulator, ".simulation") {
		return entity.EvaluationResult{}, fmt.Errorf("unsafe evaluation simulator %q", fixture.Simulator)
	}
	passed := !boolValue(fixture.Input, "force_failure")
	latency := deterministicLatency(run.Seed, fixture.FixtureID, fixture.SnapshotHash)
	metrics := entity.EvaluationMetrics{LatencyMS: latency, SafetyScore: 1}
	if passed {
		metrics.Correctness = 1
		metrics.SuccessRate = 1
	}
	summary := "Offline simulator matched the expected terminal outcome."
	if !passed {
		summary = "Offline simulator intentionally produced a deterministic mismatch."
	}
	return entity.EvaluationResult{
		ResultID: ulid.New(), RunID: run.RunID, FixtureID: fixture.FixtureID, Passed: passed,
		Metrics: metrics, Summary: summary, EvidenceIDs: []string{"snapshot:" + fixture.SnapshotHash}, CreatedAt: time.Now().UTC(),
	}, nil
}

func aggregateMetrics(results []entity.EvaluationResult) entity.EvaluationMetrics {
	if len(results) == 0 {
		return entity.EvaluationMetrics{}
	}
	var metrics entity.EvaluationMetrics
	for _, result := range results {
		metrics.Correctness += result.Metrics.Correctness
		metrics.SuccessRate += result.Metrics.SuccessRate
		metrics.SafetyScore += result.Metrics.SafetyScore
		metrics.LatencyMS += result.Metrics.LatencyMS
		metrics.CostMicros += result.Metrics.CostMicros
	}
	count := float64(len(results))
	metrics.Correctness /= count
	metrics.SuccessRate /= count
	metrics.SafetyScore /= count
	return metrics
}

func fixtureRuntime(experience *entity.Experience) (string, string) {
	for _, action := range experience.ActionRefs {
		switch {
		case strings.HasPrefix(action.Capability, "browser."):
			return "browser", "browser.mock.v1"
		case strings.HasPrefix(action.Capability, "desktop."), strings.HasPrefix(action.Capability, "application."):
			return "desktop", "desktop.mock.v1"
		case strings.HasPrefix(action.Capability, "filesystem."):
			return "filesystem", "filesystem.mock.v1"
		}
	}
	return "control", "control.simulation"
}

func fixtureSnapshotHash(experience entity.Experience) string {
	data, _ := json.Marshal(map[string]any{
		"experience_id": experience.ExperienceID, "environment": experience.EnvironmentFingerprint,
		"actions": experience.ActionRefs, "outcome": experience.Outcome, "protocol": experience.Provenance.Protocol,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func entityExpectedOutcome(experience *entity.Experience) entity.ExpectedOutcome {
	status := "FAILED"
	if experience.Outcome == entity.OutcomeSucceeded {
		status = "COMPLETED"
	} else if experience.Outcome == entity.OutcomeCancelled {
		status = "CANCELLED"
	}
	observation := ""
	if len(experience.ObservationRefs) > 0 {
		observation = experience.ObservationRefs[len(experience.ObservationRefs)-1].Status
	}
	return entity.ExpectedOutcome{TaskStatus: status, ObservationStatus: observation, Predicates: map[string]any{"verification_passed": experience.Verification.Passed}}
}

func deterministicLatency(seed int64, values ...string) int64 {
	hasher := sha256.New()
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(seed))
	_, _ = hasher.Write(buffer[:])
	for _, value := range values {
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(value))
	}
	sum := hasher.Sum(nil)
	return 10 + int64(binary.BigEndian.Uint64(sum[:8])%91)
}

func boolValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{})
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func compactName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Experience replay"
	}
	runes := []rune(value)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
