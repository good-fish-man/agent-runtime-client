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

// CandidateReplaySpec is an internal declarative replay input. It is produced
// only after the learning service validates a Skill or Strategy and is never
// accepted from the public evaluation API.
type CandidateReplaySpec struct {
	ArtifactID                   string   `json:"artifact_id"`
	ArtifactChecksum             string   `json:"artifact_checksum"`
	Kind                         string   `json:"kind"`
	ActionPattern                string   `json:"action_pattern"`
	RecoveryConditions           []string `json:"recovery_conditions,omitempty"`
	VerificationEvidenceRequired bool     `json:"verification_evidence_required"`
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
	expected := entityExpectedOutcome(experience)
	fixture := &entity.EvaluationFixture{
		Schema: entity.Schema, FixtureID: ulid.New(), OwnerID: ownerID, ExperienceID: experience.ExperienceID,
		Name: name, RuntimeKind: runtimeKind, Simulator: simulator, EnvironmentVersion: environmentVersion,
		SnapshotHash: fixtureSnapshotHash(*experience), Protocol: experience.Provenance.Protocol,
		Input: map[string]any{
			"goal_summary": experience.GoalSummary,
			"intent":       experience.Intent,
			"actions":      experience.ActionRefs,
			"failure_class": func() string {
				if experience.Failure == nil {
					return ""
				}
				return experience.Failure.Class
			}(),
			"baseline_observation": map[string]any{
				"task_status": expected.TaskStatus, "observation_status": expected.ObservationStatus,
				"predicates": expected.Predicates,
			},
		},
		Expected: expected, Sensitivity: experience.Sensitivity, CreatedAt: time.Now().UTC(),
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
	return s.runSuite(ctx, ownerID, request, nil)
}

// RunCandidateSuite evaluates structural coverage against frozen offline
// fixtures. It cannot invoke Launcher, browser, network, credentials, or code.
func (s *Service) RunCandidateSuite(ctx context.Context, ownerID string, request RunSuiteRequest, candidate CandidateReplaySpec) (*entity.EvaluationRun, []entity.EvaluationResult, error) {
	if err := candidate.validate(); err != nil {
		return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	request.CandidateID = candidate.ArtifactID
	return s.runSuite(ctx, ownerID, request, &candidate)
}

func (s *Service) runSuite(ctx context.Context, ownerID string, request RunSuiteRequest, candidate *CandidateReplaySpec) (*entity.EvaluationRun, []entity.EvaluationResult, error) {
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
	if strings.TrimSpace(request.CandidateID) == "" {
		request.CandidateID = "current-runtime"
	}
	if strings.TrimSpace(request.BaselineID) == "" {
		request.BaselineID = "fixture-recorded-observation"
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
		result, err := replayFixture(*run, *fixture, candidate)
		if err != nil {
			runErr = err
			return nil, nil, err
		}
		results = append(results, result)
	}
	run.Status = entity.EvaluationCompleted
	run.FinishedAt = time.Now().UTC()
	run.Metrics = aggregateMetrics(results)
	run.BaselineMetrics = aggregateBaselineMetrics(results)
	run.MetricDelta = evaluationMetricDelta(run.Metrics, run.BaselineMetrics)
	for _, result := range results {
		if result.Regression {
			run.RegressionCount++
		}
	}
	run.Regression = run.RegressionCount > 0
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

func replayFixture(run entity.EvaluationRun, fixture entity.EvaluationFixture, candidate *CandidateReplaySpec) (entity.EvaluationResult, error) {
	if err := fixture.Validate(); err != nil {
		return entity.EvaluationResult{}, err
	}
	// The v1 runner accepts only offline simulators. No Launcher, browser profile,
	// network endpoint, credential store, or production account is reachable.
	if !strings.Contains(fixture.Simulator, ".mock.") && !strings.HasSuffix(fixture.Simulator, ".simulation") {
		return entity.EvaluationResult{}, fmt.Errorf("unsafe evaluation simulator %q", fixture.Simulator)
	}
	if candidate == nil && namedCandidateRequiresSimulation(run.CandidateID) && !hasReplayObservation(fixture, "candidate_observation") {
		return entity.EvaluationResult{}, fmt.Errorf("candidate %q has no offline simulator observation for fixture %s", run.CandidateID, fixture.FixtureID)
	}
	actualTaskStatus, actualObservationStatus, actualPredicates := fixtureReplayObservation(fixture, "candidate_observation", "replay_observation")
	passed := replayObservationMatches(fixture.Expected, actualTaskStatus, actualObservationStatus, actualPredicates)
	summary := "Offline simulator matched the frozen expected outcome."
	evidenceIDs := []string{"snapshot:" + fixture.SnapshotHash}
	if candidate != nil {
		passed, summary = replayDeclarativeCandidate(*candidate, fixture)
		evidenceIDs = append(evidenceIDs, "candidate:"+candidate.ArtifactChecksum)
	}
	if boolValue(fixture.Input, "force_failure") {
		passed = false
	}

	baselineTaskStatus, _, baselinePredicates := fixtureReplayObservation(fixture, "baseline_observation")
	baselinePassed := baselineTaskStatus == "COMPLETED" && predicateBool(baselinePredicates, "verification_passed")
	baselineReplay := candidate == nil && !hasAnyReplayObservation(fixture, "candidate_observation", "replay_observation")
	if baselineReplay {
		// A public baseline run has no independent candidate output. Reusing the
		// frozen observation must not manufacture an improvement on a failed task.
		passed = baselinePassed
		summary = "Offline simulator replayed the frozen baseline observation."
	}
	unsafeEffect := boolValue(fixture.Input, "unsafe_effect")
	latency := deterministicLatency(run.Seed, fixture.FixtureID, fixture.SnapshotHash)
	metrics := replayMetrics(passed, unsafeEffect, latency)
	passed = metrics.Correctness == 1 && metrics.SafetyScore == 1

	if boolValue(fixture.Input, "baseline_force_failure") {
		baselinePassed = false
	}
	baseline := replayMetrics(baselinePassed, boolValue(fixture.Input, "baseline_unsafe_effect"), latency)
	delta := evaluationMetricDelta(metrics, baseline)
	regression := evaluationMetricsRegressed(metrics, baseline)
	if !passed && !baselineReplay {
		summary = "Offline simulator did not match the frozen expected outcome."
	}
	if candidate != nil && !passed {
		summary = "Declarative candidate did not cover the frozen semantic pattern, verification, or failure recovery condition."
	}
	if unsafeEffect {
		summary = "Offline simulator observed a forbidden or unsafe effect."
	}
	return entity.EvaluationResult{
		ResultID: ulid.New(), RunID: run.RunID, FixtureID: fixture.FixtureID, Passed: passed,
		Metrics: metrics, BaselineMetrics: baseline, MetricDelta: delta, Regression: regression,
		Summary: summary, EvidenceIDs: evidenceIDs, CreatedAt: time.Now().UTC(),
	}, nil
}

func (candidate CandidateReplaySpec) validate() error {
	if strings.TrimSpace(candidate.ArtifactID) == "" || len(strings.TrimSpace(candidate.ArtifactChecksum)) != 64 {
		return fmt.Errorf("candidate replay requires an artifact id and SHA-256 checksum")
	}
	if candidate.Kind != "SKILL" && candidate.Kind != "STRATEGY" {
		return fmt.Errorf("candidate replay kind must be SKILL or STRATEGY")
	}
	if strings.TrimSpace(candidate.ActionPattern) == "" {
		return fmt.Errorf("candidate replay requires a semantic action pattern")
	}
	if !candidate.VerificationEvidenceRequired {
		return fmt.Errorf("candidate replay must require verification evidence")
	}
	return nil
}

func replayDeclarativeCandidate(candidate CandidateReplaySpec, fixture entity.EvaluationFixture) (bool, string) {
	if fixtureActionPattern(fixture) != candidate.ActionPattern {
		return false, "Declarative candidate action pattern did not match the frozen fixture."
	}
	failureClass := strings.TrimSpace(stringValue(fixture.Input["failure_class"]))
	if failureClass != "" && !containsReplayCondition(candidate.RecoveryConditions, failureClass) {
		return false, "Declarative candidate did not declare bounded recovery for the frozen failure condition."
	}
	return true, "Declarative candidate covered the frozen semantic pattern, evidence requirement, and observed failure conditions."
}

func fixtureActionPattern(fixture entity.EvaluationFixture) string {
	encoded, err := json.Marshal(fixture.Input["actions"])
	if err != nil {
		return ""
	}
	var actions []entity.ActionRef
	if err := json.Unmarshal(encoded, &actions); err != nil {
		return ""
	}
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		operation := strings.TrimSpace(action.Operation)
		if operation == "" {
			operation = "invoke"
		}
		parts = append(parts, strings.TrimSpace(action.Capability)+"."+operation)
	}
	return strings.Join(parts, "|")
}

func containsReplayCondition(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "*" || value == target {
			return true
		}
	}
	return false
}

func predicateBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func namedCandidateRequiresSimulation(candidateID string) bool {
	candidateID = strings.TrimSpace(candidateID)
	return candidateID != "" && candidateID != "current-runtime"
}

func hasReplayObservation(fixture entity.EvaluationFixture, key string) bool {
	_, ok := fixture.Input[key].(map[string]any)
	return ok
}

func hasAnyReplayObservation(fixture entity.EvaluationFixture, keys ...string) bool {
	for _, key := range keys {
		if hasReplayObservation(fixture, key) {
			return true
		}
	}
	return false
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

func aggregateBaselineMetrics(results []entity.EvaluationResult) entity.EvaluationMetrics {
	if len(results) == 0 {
		return entity.EvaluationMetrics{}
	}
	baselineResults := make([]entity.EvaluationResult, 0, len(results))
	for _, result := range results {
		baselineResults = append(baselineResults, entity.EvaluationResult{Metrics: result.BaselineMetrics})
	}
	return aggregateMetrics(baselineResults)
}

func evaluationMetricDelta(candidate, baseline entity.EvaluationMetrics) entity.EvaluationMetrics {
	return entity.EvaluationMetrics{
		Correctness: candidate.Correctness - baseline.Correctness,
		SuccessRate: candidate.SuccessRate - baseline.SuccessRate,
		SafetyScore: candidate.SafetyScore - baseline.SafetyScore,
		LatencyMS:   candidate.LatencyMS - baseline.LatencyMS,
		CostMicros:  candidate.CostMicros - baseline.CostMicros,
	}
}

func evaluationMetricsRegressed(candidate, baseline entity.EvaluationMetrics) bool {
	return candidate.Correctness < baseline.Correctness || candidate.SuccessRate < baseline.SuccessRate ||
		candidate.SafetyScore < baseline.SafetyScore || candidate.LatencyMS > baseline.LatencyMS || candidate.CostMicros > baseline.CostMicros
}

func fixtureReplayObservation(fixture entity.EvaluationFixture, keys ...string) (string, string, map[string]any) {
	taskStatus := fixture.Expected.TaskStatus
	observationStatus := fixture.Expected.ObservationStatus
	predicates := cloneAnyMap(fixture.Expected.Predicates)
	var value map[string]any
	for _, key := range keys {
		if candidate, ok := fixture.Input[key].(map[string]any); ok {
			value = candidate
			break
		}
	}
	if value == nil {
		return taskStatus, observationStatus, predicates
	}
	if candidate := strings.TrimSpace(stringValue(value["task_status"])); candidate != "" {
		taskStatus = candidate
	}
	if candidate := strings.TrimSpace(stringValue(value["observation_status"])); candidate != "" {
		observationStatus = candidate
	}
	if candidate, ok := value["predicates"].(map[string]any); ok {
		predicates = cloneAnyMap(candidate)
	}
	return taskStatus, observationStatus, predicates
}

func replayMetrics(matchedExpected, unsafeEffect bool, latency int64) entity.EvaluationMetrics {
	metrics := entity.EvaluationMetrics{LatencyMS: latency, SafetyScore: 1}
	if matchedExpected {
		metrics.Correctness = 1
		metrics.SuccessRate = 1
	}
	if unsafeEffect {
		metrics.SafetyScore = 0
		metrics.Correctness = 0
		metrics.SuccessRate = 0
	}
	return metrics
}

func replayObservationMatches(expected entity.ExpectedOutcome, taskStatus, observationStatus string, predicates map[string]any) bool {
	if expected.TaskStatus != "" && expected.TaskStatus != taskStatus {
		return false
	}
	if expected.ObservationStatus != "" && expected.ObservationStatus != observationStatus {
		return false
	}
	for key, expectedValue := range expected.Predicates {
		actualValue, exists := predicates[key]
		if !exists || canonicalJSON(actualValue) != canonicalJSON(expectedValue) {
			return false
		}
	}
	return true
}

func cloneAnyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func canonicalJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
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
		"actions": experience.ActionRefs, "outcome": experience.Outcome, "expected": entityExpectedOutcome(&experience),
		"protocol": experience.Provenance.Protocol,
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
