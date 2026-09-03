package experience

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	controlpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/experience"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStoreOwnerIsolationIdempotencyAndDeletion(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	first := testStoredExperience("experience-1", "task-1", "user-1", time.Now().UTC().Add(time.Hour))
	created, err := store.Create(ctx, first)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	created, err = store.Create(ctx, first)
	if err != nil || created {
		t.Fatalf("idempotent create: created=%v err=%v", created, err)
	}
	if value, err := store.Find(ctx, "user-2", first.Experience.ExperienceID); err != nil || value != nil {
		t.Fatalf("cross-user read leaked: value=%#v err=%v", value, err)
	}
	candidates, err := store.SearchCandidates(ctx, "user-2", entity.SearchRequest{}, 10)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("cross-user search leaked: candidates=%#v err=%v", candidates, err)
	}
	now := time.Now().UTC()
	fixture := po.EvaluationFixture{
		FixtureID: "fixture-1", OwnerID: "user-1", ExperienceID: first.Experience.ExperienceID, Name: "private fixture",
		RuntimeKind: "browser", Simulator: "browser.mock.v1", EnvironmentVersion: "v1", SnapshotHash: "hash",
		Protocol: entity.Schema, Content: `{"goal":"private retained content"}`, CreatedAt: millis(now), UpdatedAt: millis(now),
	}
	suite := po.EvaluationSuite{SuiteID: "suite-1", OwnerID: "user-1", Name: "suite", FixtureIDs: `["fixture-1"]`, CreatedAt: millis(now), UpdatedAt: millis(now)}
	run := po.EvaluationRun{RunID: "run-1", OwnerID: "user-1", SuiteID: suite.SuiteID, Status: entity.EvaluationCompleted, Seed: 42, Metrics: `{}`, StartedAt: millis(now), CreatedAt: millis(now), UpdatedAt: millis(now)}
	result := po.EvaluationResult{ResultID: "result-1", OwnerID: "user-1", RunID: run.RunID, FixtureID: fixture.FixtureID, Metrics: `{}`, Summary: "private result", EvidenceIDs: `[]`, CreatedAt: millis(now)}
	malformedSuite := po.EvaluationSuite{SuiteID: "suite-corrupt", OwnerID: "user-1", Name: "corrupt suite", FixtureIDs: `{`, CreatedAt: millis(now), UpdatedAt: millis(now)}
	malformedRun := po.EvaluationRun{RunID: "run-corrupt", OwnerID: "user-1", SuiteID: malformedSuite.SuiteID, Status: entity.EvaluationCompleted, Seed: 42, Metrics: `{}`, StartedAt: millis(now), CreatedAt: millis(now), UpdatedAt: millis(now)}
	for _, value := range []any{&fixture, &suite, &run, &result, &malformedSuite, &malformedRun} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeletePayload(ctx, "user-1", first.Experience.ExperienceID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Find(ctx, "user-1", first.Experience.ExperienceID); err != nil || value != nil {
		t.Fatalf("deleted payload remained readable: value=%#v err=%v", value, err)
	}
	tombstones, total, err := store.List(ctx, "user-1", entity.ListFilter{Status: entity.StatusDeleted, Limit: 10})
	if err != nil || total != 1 || len(tombstones) != 1 {
		t.Fatalf("deleted audit shell was not listed: total=%d items=%#v err=%v", total, tombstones, err)
	}
	if tombstones[0].Retention.PayloadMode != entity.PayloadDeleted || tombstones[0].GoalSummary != "" {
		t.Fatalf("deleted audit shell exposed payload: %#v", tombstones[0])
	}
	var payloads int64
	if err := db.Model(&po.ExperiencePayload{}).Where("experience_id = ?", first.Experience.ExperienceID).Count(&payloads).Error; err != nil || payloads != 0 {
		t.Fatalf("payload was not physically deleted: count=%d err=%v", payloads, err)
	}
	var refs int64
	if err := db.Model(&po.ExperienceEventRef{}).Where("experience_id = ?", first.Experience.ExperienceID).Count(&refs).Error; err != nil || refs != 1 {
		t.Fatalf("audit references were unexpectedly removed: count=%d err=%v", refs, err)
	}
	for name, model := range map[string]any{
		"fixture": &po.EvaluationFixture{}, "suite": &po.EvaluationSuite{}, "run": &po.EvaluationRun{}, "result": &po.EvaluationResult{},
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("derived %s data survived experience deletion: count=%d err=%v", name, count, err)
		}
	}
}

func TestStoreRetentionAndDisabledPreference(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	preference, err := store.SavePreference(ctx, entity.Preference{OwnerID: "user-1", LearningEnabled: false, RetentionDays: 14, MaxSensitivity: entity.SensitivityInternal})
	if err != nil {
		t.Fatal(err)
	}
	if preference.LearningEnabled || preference.RetentionDays != 14 {
		t.Fatalf("false preference was not persisted: %#v", preference)
	}
	skipped := testStoredExperience("experience-skipped", "task-skipped", "user-1", time.Now().UTC().Add(time.Hour))
	skipped.Experience.Status = entity.StatusSkipped
	skipped.Experience.SkipReason = "learning_disabled_by_user"
	skipped.Experience.GoalSummary = ""
	skipped.Experience.Outcome = ""
	skipped.Payload = ""
	skipped.SearchText = ""
	if _, err := store.Create(ctx, skipped); err != nil {
		t.Fatal(err)
	}
	value, err := store.Find(ctx, "user-1", skipped.Experience.ExperienceID)
	if err != nil || value == nil || value.Retention.PayloadMode != entity.PayloadNone {
		t.Fatalf("skipped experience payload mode = %#v, err=%v", value, err)
	}
	expired := testStoredExperience("experience-expired", "task-expired", "user-1", time.Now().UTC().Add(-time.Minute))
	if _, err := store.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	purged, err := store.PurgeExpired(ctx, time.Now().UTC(), 10)
	if err != nil || purged != 1 {
		t.Fatalf("purge: count=%d err=%v", purged, err)
	}
	if value, _ := store.Find(ctx, "user-1", expired.Experience.ExperienceID); value != nil {
		t.Fatalf("expired experience remained readable: %#v", value)
	}
}

func TestListReadyOwnersUsesStableKeysetAndExcludesTombstones(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	deleteAt := time.Now().UTC().Add(time.Hour)
	for _, fixture := range []*entity.StoredExperience{
		testStoredExperience("experience-c", "task-c", "user-c", deleteAt),
		testStoredExperience("experience-a", "task-a", "user-a", deleteAt),
		testStoredExperience("experience-b", "task-b", "user-b", deleteAt),
	} {
		if created, err := store.Create(ctx, fixture); err != nil || !created {
			t.Fatalf("create %s: created=%v err=%v", fixture.Experience.ExperienceID, created, err)
		}
	}
	if err := store.DeletePayload(ctx, "user-b", "experience-b", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	first, err := store.ListReadyOwners(ctx, "", 1)
	if err != nil || len(first) != 1 || first[0] != "user-a" {
		t.Fatalf("first owner page = %#v, err=%v", first, err)
	}
	second, err := store.ListReadyOwners(ctx, first[0], 10)
	if err != nil || len(second) != 1 || second[0] != "user-c" {
		t.Fatalf("second owner page = %#v, err=%v", second, err)
	}
}

func TestStoreDeleteAllPayloadsIsOwnerScoped(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	deleteAt := time.Now().UTC().Add(time.Hour)
	for _, fixture := range []*entity.StoredExperience{
		testStoredExperience("experience-bulk-1", "task-bulk-1", "user-1", deleteAt),
		testStoredExperience("experience-bulk-2", "task-bulk-2", "user-1", deleteAt),
		testStoredExperience("experience-other", "task-other", "user-2", deleteAt),
	} {
		created, err := store.Create(ctx, fixture)
		if err != nil || !created {
			t.Fatalf("create %s: created=%v err=%v", fixture.Experience.ExperienceID, created, err)
		}
	}

	deleted, err := store.DeleteAllPayloads(ctx, "user-1", time.Now().UTC())
	if err != nil || deleted != 2 {
		t.Fatalf("delete all: deleted=%d err=%v", deleted, err)
	}
	for _, id := range []string{"experience-bulk-1", "experience-bulk-2"} {
		value, err := store.Find(ctx, "user-1", id)
		if err != nil || value != nil {
			t.Fatalf("deleted experience remained readable: id=%s value=%#v err=%v", id, value, err)
		}
	}
	other, err := store.Find(ctx, "user-2", "experience-other")
	if err != nil || other == nil || other.GoalSummary == "" {
		t.Fatalf("other owner's experience changed: value=%#v err=%v", other, err)
	}

	var ownerPayloads int64
	if err := db.Model(&po.ExperiencePayload{}).Where("owner_id = ?", "user-1").Count(&ownerPayloads).Error; err != nil || ownerPayloads != 0 {
		t.Fatalf("owner payloads survived deletion: count=%d err=%v", ownerPayloads, err)
	}
	var otherPayloads int64
	if err := db.Model(&po.ExperiencePayload{}).Where("owner_id = ?", "user-2").Count(&otherPayloads).Error; err != nil || otherPayloads != 1 {
		t.Fatalf("other owner payload was deleted: count=%d err=%v", otherPayloads, err)
	}
	_, total, err := store.List(ctx, "user-1", entity.ListFilter{Status: entity.StatusDeleted, Limit: 10})
	if err != nil || total != 2 {
		t.Fatalf("audit tombstones: total=%d err=%v", total, err)
	}
}

func TestStoreStatsReportsOwnerScopedTerminalCoverage(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	tasks := []controlpo.Task{
		{TaskID: "task-covered", UserID: "user-1", Status: "COMPLETED", Revision: 1, CreatedAt: millis(now), UpdatedAt: millis(now)},
		{TaskID: "task-pending", UserID: "user-1", Status: "FAILED", Revision: 1, CreatedAt: millis(now), UpdatedAt: millis(now)},
		{TaskID: "task-running", UserID: "user-1", Status: "RUNNING", Revision: 1, CreatedAt: millis(now), UpdatedAt: millis(now)},
		{TaskID: "task-other", UserID: "user-2", Status: "CANCELLED", Revision: 1, CreatedAt: millis(now), UpdatedAt: millis(now)},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []*entity.StoredExperience{
		testStoredExperience("experience-covered", "task-covered", "user-1", now.Add(time.Hour)),
		testStoredExperience("experience-other", "task-other", "user-2", now.Add(time.Hour)),
	} {
		if created, err := store.Create(ctx, fixture); err != nil || !created {
			t.Fatalf("create %s: created=%v err=%v", fixture.Experience.ExperienceID, created, err)
		}
	}
	stats, err := store.Stats(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.TerminalTasks != 2 || stats.CoveredTasks != 1 || stats.PendingTasks != 1 || stats.CoverageRate != 0.5 {
		t.Fatalf("unexpected owner coverage: %#v", stats)
	}
	other, err := store.Stats(ctx, "user-2")
	if err != nil {
		t.Fatal(err)
	}
	if other.TerminalTasks != 1 || other.CoveredTasks != 1 || other.PendingTasks != 0 || other.CoverageRate != 1 {
		t.Fatalf("cross-owner coverage leaked: %#v", other)
	}
}

func TestStoreEvaluationRoundTripPreservesBaselineAndRegression(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	run := entity.EvaluationRun{
		RunID: "run-roundtrip", OwnerID: "user-1", SuiteID: "suite-roundtrip", Status: entity.EvaluationCompleted,
		Seed: 42, CandidateID: "candidate-v3", BaselineID: "baseline-v2",
		Metrics:         entity.EvaluationMetrics{Correctness: 0.5, SuccessRate: 0.5, SafetyScore: 1, LatencyMS: 20},
		BaselineMetrics: entity.EvaluationMetrics{Correctness: 1, SuccessRate: 1, SafetyScore: 1, LatencyMS: 20},
		MetricDelta:     entity.EvaluationMetrics{Correctness: -0.5, SuccessRate: -0.5},
		Regression:      true, RegressionCount: 1, StartedAt: now, FinishedAt: now.Add(time.Second),
	}
	result := entity.EvaluationResult{
		ResultID: "result-roundtrip", RunID: run.RunID, FixtureID: "fixture-roundtrip", Passed: false,
		Metrics: run.Metrics, BaselineMetrics: run.BaselineMetrics, MetricDelta: run.MetricDelta,
		Regression: true, Summary: "candidate regressed", EvidenceIDs: []string{"snapshot:test"}, CreatedAt: now,
	}
	if err := store.CreateRun(ctx, run, []entity.EvaluationResult{result}); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns(ctx, "user-1", 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: items=%#v err=%v", runs, err)
	}
	if !runs[0].Regression || runs[0].RegressionCount != 1 || runs[0].MetricDelta.Correctness != -0.5 || runs[0].BaselineMetrics.Correctness != 1 {
		t.Fatalf("evaluation run lost comparison fields: %#v", runs[0])
	}
	results, err := store.ListResults(ctx, "user-1", run.RunID)
	if err != nil || len(results) != 1 {
		t.Fatalf("list results: items=%#v err=%v", results, err)
	}
	if !results[0].Regression || results[0].MetricDelta.SuccessRate != -0.5 || len(results[0].EvidenceIDs) != 1 {
		t.Fatalf("evaluation result lost comparison fields: %#v", results[0])
	}
	if leaked, err := store.ListRuns(ctx, "user-2", 10); err != nil || len(leaked) != 0 {
		t.Fatalf("cross-owner evaluation run leaked: items=%#v err=%v", leaked, err)
	}
}

func newTestStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&controlpo.Task{},
		&po.Experience{}, &po.ExperiencePayload{}, &po.ExperienceEventRef{}, &po.ExperienceRedaction{},
		&po.FailureClassification{}, &po.ExperiencePreference{}, &po.EvaluationFixture{}, &po.EvaluationSuite{},
		&po.EvaluationRun{}, &po.EvaluationResult{},
	); err != nil {
		t.Fatal(err)
	}
	return NewStore(data.New(db)), db
}

func testStoredExperience(experienceID, taskID, ownerID string, deleteAt time.Time) *entity.StoredExperience {
	now := time.Now().UTC()
	experience := entity.Experience{
		Schema: entity.Schema, ExperienceID: experienceID, OwnerID: ownerID, TaskID: taskID, Status: entity.StatusReady,
		GoalSummary: "open a local mock page", Outcome: entity.OutcomeSucceeded, Sensitivity: entity.SensitivityInternal,
		Retention:  entity.RetentionPolicy{Days: 30, PayloadMode: entity.PayloadSeparate, DeleteAt: deleteAt},
		Provenance: entity.Provenance{Protocol: "athena.agent.v4", GeneratedBy: "test", GeneratedAt: now},
		CreatedAt:  now, UpdatedAt: now,
	}
	payload, _ := json.Marshal(experience)
	return &entity.StoredExperience{
		Experience: experience, Payload: string(payload), SearchText: experience.GoalSummary, Vector: []float64{1, 0},
		EventRefs: []entity.EventRef{{ExperienceID: experienceID, EventID: "event-" + experienceID, OwnerID: ownerID, TaskID: taskID, EventType: "task.status_changed", CreatedAt: now}},
	}
}
