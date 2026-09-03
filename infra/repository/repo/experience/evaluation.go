package experience

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/experience"
	log "github.com/good-fish-man/logx"
	"gorm.io/gorm"
)

func (s *Store) CreateFixture(ctx context.Context, fixture entity.EvaluationFixture) error {
	if err := fixture.Validate(); err != nil {
		return err
	}
	content, err := json.Marshal(fixture)
	if err != nil {
		return err
	}
	value := po.EvaluationFixture{
		FixtureID: fixture.FixtureID, OwnerID: fixture.OwnerID, ExperienceID: fixture.ExperienceID,
		Name: fixture.Name, RuntimeKind: fixture.RuntimeKind, Simulator: fixture.Simulator,
		EnvironmentVersion: fixture.EnvironmentVersion, SnapshotHash: fixture.SnapshotHash,
		Protocol: fixture.Protocol, Content: string(content), CreatedAt: millis(fixture.CreatedAt), UpdatedAt: millis(fixture.CreatedAt),
	}
	return log.WrapError(s.data.DB(ctx).Create(&value).Error, "ExperienceStore.CreateFixture")
}

func (s *Store) FindFixture(ctx context.Context, ownerID, fixtureID string) (*entity.EvaluationFixture, error) {
	var value po.EvaluationFixture
	result := s.data.DB(ctx).Where("fixture_id = ? AND owner_id = ?", fixtureID, ownerID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ExperienceStore.FindFixture")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodeFixture(value)
}

func (s *Store) ListFixtures(ctx context.Context, ownerID string, limit int) ([]entity.EvaluationFixture, error) {
	values := make([]po.EvaluationFixture, 0)
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.ListFixtures")
	}
	items := make([]entity.EvaluationFixture, 0, len(values))
	for _, value := range values {
		item, err := decodeFixture(value)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) CreateSuite(ctx context.Context, suite entity.EvaluationSuite) error {
	if suite.SuiteID == "" || suite.OwnerID == "" || suite.Name == "" || len(suite.FixtureIDs) == 0 {
		return fmt.Errorf("suite id, owner, name, and fixtures are required")
	}
	value := po.EvaluationSuite{
		SuiteID: suite.SuiteID, OwnerID: suite.OwnerID, Name: suite.Name, FixtureIDs: encodeJSON(suite.FixtureIDs),
		CreatedAt: millis(suite.CreatedAt), UpdatedAt: millis(suite.UpdatedAt),
	}
	return log.WrapError(s.data.DB(ctx).Create(&value).Error, "ExperienceStore.CreateSuite")
}

func (s *Store) FindSuite(ctx context.Context, ownerID, suiteID string) (*entity.EvaluationSuite, error) {
	var value po.EvaluationSuite
	result := s.data.DB(ctx).Where("suite_id = ? AND owner_id = ?", suiteID, ownerID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ExperienceStore.FindSuite")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	suite := decodeSuite(value)
	return &suite, nil
}

func (s *Store) ListSuites(ctx context.Context, ownerID string, limit int) ([]entity.EvaluationSuite, error) {
	values := make([]po.EvaluationSuite, 0)
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.ListSuites")
	}
	items := make([]entity.EvaluationSuite, 0, len(values))
	for _, value := range values {
		items = append(items, decodeSuite(value))
	}
	return items, nil
}

func (s *Store) CreateRun(ctx context.Context, run entity.EvaluationRun, results []entity.EvaluationResult) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		value := po.EvaluationRun{
			RunID: run.RunID, OwnerID: run.OwnerID, SuiteID: run.SuiteID, Status: run.Status, Seed: run.Seed,
			CandidateID: run.CandidateID, BaselineID: run.BaselineID, Metrics: encodeJSON(run.Metrics),
			BaselineMetrics: encodeJSON(run.BaselineMetrics), MetricDelta: encodeJSON(run.MetricDelta),
			Regression: run.Regression, RegressionCount: run.RegressionCount,
			StartedAt: millis(run.StartedAt), FinishedAt: millis(run.FinishedAt), Error: run.Error,
			CreatedAt: millis(run.StartedAt), UpdatedAt: millis(firstTime(run.FinishedAt, run.StartedAt)),
		}
		if err := tx.Create(&value).Error; err != nil {
			return err
		}
		for _, result := range results {
			item := po.EvaluationResult{
				ResultID: result.ResultID, OwnerID: run.OwnerID, RunID: run.RunID, FixtureID: result.FixtureID,
				Passed: result.Passed, Metrics: encodeJSON(result.Metrics), BaselineMetrics: encodeJSON(result.BaselineMetrics),
				MetricDelta: encodeJSON(result.MetricDelta), Regression: result.Regression, Summary: result.Summary,
				EvidenceIDs: encodeJSON(result.EvidenceIDs), CreatedAt: millis(result.CreatedAt),
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	}), "ExperienceStore.CreateRun")
}

func (s *Store) ListRuns(ctx context.Context, ownerID string, limit int) ([]entity.EvaluationRun, error) {
	values := make([]po.EvaluationRun, 0)
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.ListRuns")
	}
	items := make([]entity.EvaluationRun, 0, len(values))
	for _, value := range values {
		item, err := decodeRun(value)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) ListResults(ctx context.Context, ownerID, runID string) ([]entity.EvaluationResult, error) {
	values := make([]po.EvaluationResult, 0)
	if err := s.data.DB(ctx).Where("owner_id = ? AND run_id = ?", ownerID, runID).Order("created_at ASC").Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.ListResults")
	}
	items := make([]entity.EvaluationResult, 0, len(values))
	for _, value := range values {
		metrics, err := decodeEvaluationMetrics(value.Metrics, "ExperienceStore.ListResults.metrics")
		if err != nil {
			return nil, err
		}
		baselineMetrics, err := decodeEvaluationMetrics(value.BaselineMetrics, "ExperienceStore.ListResults.baseline_metrics")
		if err != nil {
			return nil, err
		}
		metricDelta, err := decodeEvaluationMetrics(value.MetricDelta, "ExperienceStore.ListResults.metric_delta")
		if err != nil {
			return nil, err
		}
		var evidenceIDs []string
		if err := json.Unmarshal([]byte(value.EvidenceIDs), &evidenceIDs); err != nil {
			return nil, log.WrapError(err, "ExperienceStore.ListResults.evidence_ids")
		}
		items = append(items, entity.EvaluationResult{
			ResultID: value.ResultID, RunID: value.RunID, FixtureID: value.FixtureID, Passed: value.Passed,
			Metrics: metrics, BaselineMetrics: baselineMetrics, MetricDelta: metricDelta, Regression: value.Regression,
			Summary: value.Summary, EvidenceIDs: evidenceIDs, CreatedAt: fromMillis(value.CreatedAt),
		})
	}
	return items, nil
}

func decodeFixture(value po.EvaluationFixture) (*entity.EvaluationFixture, error) {
	var fixture entity.EvaluationFixture
	if err := json.Unmarshal([]byte(value.Content), &fixture); err != nil {
		return nil, log.WrapError(err, "ExperienceStore.decodeFixture")
	}
	return &fixture, nil
}

func decodeSuite(value po.EvaluationSuite) entity.EvaluationSuite {
	var fixtureIDs []string
	_ = json.Unmarshal([]byte(value.FixtureIDs), &fixtureIDs)
	return entity.EvaluationSuite{
		SuiteID: value.SuiteID, OwnerID: value.OwnerID, Name: value.Name, FixtureIDs: fixtureIDs,
		CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
	}
}

func decodeRun(value po.EvaluationRun) (entity.EvaluationRun, error) {
	metrics, err := decodeEvaluationMetrics(value.Metrics, "ExperienceStore.decodeRun.metrics")
	if err != nil {
		return entity.EvaluationRun{}, err
	}
	baselineMetrics, err := decodeEvaluationMetrics(value.BaselineMetrics, "ExperienceStore.decodeRun.baseline_metrics")
	if err != nil {
		return entity.EvaluationRun{}, err
	}
	metricDelta, err := decodeEvaluationMetrics(value.MetricDelta, "ExperienceStore.decodeRun.metric_delta")
	if err != nil {
		return entity.EvaluationRun{}, err
	}
	return entity.EvaluationRun{
		RunID: value.RunID, OwnerID: value.OwnerID, SuiteID: value.SuiteID, Status: value.Status,
		Seed: value.Seed, CandidateID: value.CandidateID, BaselineID: value.BaselineID, Metrics: metrics,
		BaselineMetrics: baselineMetrics, MetricDelta: metricDelta, Regression: value.Regression, RegressionCount: value.RegressionCount,
		StartedAt: fromMillis(value.StartedAt), FinishedAt: fromMillis(value.FinishedAt), Error: value.Error,
	}, nil
}

func decodeEvaluationMetrics(value, operation string) (entity.EvaluationMetrics, error) {
	var metrics entity.EvaluationMetrics
	if err := json.Unmarshal([]byte(value), &metrics); err != nil {
		return entity.EvaluationMetrics{}, log.WrapError(err, operation)
	}
	return metrics, nil
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
