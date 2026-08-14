package experience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/experience"
	log "github.com/good-fish-man/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultRetentionDays = 30
	defaultListLimit     = 50
	maximumListLimit     = 200
)

var ErrNotFound = errors.New("experience record not found")

type Store struct{ data *data.Data }

func NewStore(d *data.Data) *Store { return &Store{data: d} }

func (s *Store) GetPreference(ctx context.Context, ownerID string) (*entity.Preference, error) {
	var value po.ExperiencePreference
	result := s.data.DB(ctx).Where("owner_id = ?", ownerID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ExperienceStore.GetPreference")
	}
	preference := &entity.Preference{OwnerID: ownerID, LearningEnabled: true, RetentionDays: defaultRetentionDays, MaxSensitivity: entity.SensitivitySensitive}
	if result.RowsAffected == 0 {
		return preference, nil
	}
	preference.LearningEnabled = value.LearningEnabled
	preference.RetentionDays = value.RetentionDays
	preference.MaxSensitivity = value.MaxSensitivity
	preference.UpdatedAt = fromMillis(value.UpdatedAt)
	preference.Normalize()
	return preference, nil
}

func (s *Store) SavePreference(ctx context.Context, preference entity.Preference) (*entity.Preference, error) {
	preference.Normalize()
	if strings.TrimSpace(preference.OwnerID) == "" {
		return nil, fmt.Errorf("owner_id is required")
	}
	now := time.Now().UTC()
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var current po.ExperiencePreference
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_id = ?", preference.OwnerID).Limit(1).Find(&current)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return tx.Create(&po.ExperiencePreference{
				OwnerID: preference.OwnerID, LearningEnabled: preference.LearningEnabled,
				RetentionDays: preference.RetentionDays, MaxSensitivity: preference.MaxSensitivity,
				Revision: 1, CreatedAt: millis(now), UpdatedAt: millis(now),
			}).Error
		}
		return tx.Model(&po.ExperiencePreference{}).Where("owner_id = ?", preference.OwnerID).Updates(map[string]any{
			"learning_enabled": preference.LearningEnabled,
			"retention_days":   preference.RetentionDays,
			"max_sensitivity":  preference.MaxSensitivity,
			"revision":         current.Revision + 1,
			"updated_at":       millis(now),
		}).Error
	})
	if err != nil {
		return nil, log.WrapError(err, "ExperienceStore.SavePreference")
	}
	return s.GetPreference(ctx, preference.OwnerID)
}

func (s *Store) ListPendingTerminalTasks(ctx context.Context, limit int) ([]entity.PendingTask, error) {
	limit = normalizeLimit(limit)
	rows := make([]entity.PendingTask, 0, limit)
	err := s.data.DB(ctx).Table("os_task AS task").
		Select("task.task_id").
		Where("task.status IN ?", []string{"COMPLETED", "FAILED", "CANCELLED"}).
		Where("NOT EXISTS (?)", s.data.DB(ctx).Table("os_experience AS experience").Select("1").Where("experience.task_id = task.task_id")).
		Order("task.updated_at ASC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, log.WrapError(err, "ExperienceStore.ListPendingTerminalTasks")
	}
	return rows, nil
}

func (s *Store) Create(ctx context.Context, stored *entity.StoredExperience) (bool, error) {
	if stored == nil {
		return false, fmt.Errorf("stored experience is required")
	}
	if err := stored.Experience.Validate(); err != nil {
		return false, log.WrapError(err, "ExperienceStore.Create.validate")
	}
	experience := stored.Experience
	created := false
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&po.Experience{}).Where("task_id = ?", experience.TaskID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		value := experienceToPO(experience)
		if err := tx.Create(&value).Error; err != nil {
			return err
		}
		if experience.Status == entity.StatusReady {
			payload := po.ExperiencePayload{
				ExperienceID: experience.ExperienceID, OwnerID: experience.OwnerID,
				Content: stored.Payload, SearchText: stored.SearchText, Vector: encodeJSON(stored.Vector),
				CreatedAt: millis(experience.CreatedAt), UpdatedAt: millis(experience.UpdatedAt),
			}
			if err := tx.Create(&payload).Error; err != nil {
				return err
			}
		}
		for _, ref := range stored.EventRefs {
			value := po.ExperienceEventRef{
				ExperienceID: experience.ExperienceID, EventID: ref.EventID, OwnerID: experience.OwnerID,
				TaskID: experience.TaskID, EventType: ref.EventType, CreatedAt: millis(ref.CreatedAt),
			}
			if err := tx.Create(&value).Error; err != nil {
				return err
			}
		}
		for _, redaction := range stored.Redactions {
			value := po.ExperienceRedaction{
				RedactionID: redaction.RedactionID, ExperienceID: experience.ExperienceID, OwnerID: experience.OwnerID,
				Category: redaction.Category, FieldPath: redaction.FieldPath, Digest: redaction.Digest, CreatedAt: millis(redaction.CreatedAt),
			}
			if err := tx.Create(&value).Error; err != nil {
				return err
			}
		}
		if experience.Failure != nil {
			failure := po.FailureClassification{
				ExperienceID: experience.ExperienceID, OwnerID: experience.OwnerID, Class: experience.Failure.Class,
				Rule: experience.Failure.Rule, Summary: experience.Failure.Summary,
				Evidence: encodeJSON(experience.Failure.EvidenceIDs), Confidence: experience.Failure.Confidence,
				CreatedAt: millis(experience.CreatedAt), UpdatedAt: millis(experience.UpdatedAt),
			}
			if err := tx.Create(&failure).Error; err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return false, log.WrapError(err, "ExperienceStore.Create")
	}
	return created, nil
}

func (s *Store) Find(ctx context.Context, ownerID, experienceID string) (*entity.Experience, error) {
	var audit po.Experience
	result := s.data.DB(ctx).Where("experience_id = ? AND owner_id = ?", experienceID, ownerID).Limit(1).Find(&audit)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ExperienceStore.Find.audit")
	}
	if result.RowsAffected == 0 || audit.Tombstoned {
		return nil, nil
	}
	return s.hydrate(ctx, &audit)
}

func (s *Store) List(ctx context.Context, ownerID string, filter entity.ListFilter) ([]entity.Experience, int64, error) {
	filter.Limit = normalizeLimit(filter.Limit)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	db := s.data.DB(ctx).Model(&po.Experience{}).Where("owner_id = ?", ownerID)
	if filter.Status == entity.StatusDeleted {
		db = db.Where("tombstoned = ?", true)
	} else {
		db = db.Where("tombstoned = ?", false)
	}
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	if filter.Outcome != "" {
		db = db.Where("outcome = ?", filter.Outcome)
	}
	if filter.FailureClass != "" {
		db = db.Where("failure_class = ?", filter.FailureClass)
	}
	if filter.Sensitivity != "" {
		db = db.Where("sensitivity = ?", filter.Sensitivity)
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		db = db.Where("experience_id IN (?)", s.data.DB(ctx).Table("os_experience_payload").Select("experience_id").Where("owner_id = ? AND LOWER(search_text) LIKE ?", ownerID, "%"+strings.ToLower(query)+"%"))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, log.WrapError(err, "ExperienceStore.List.count")
	}
	values := make([]po.Experience, 0, filter.Limit)
	if err := db.Order("created_at DESC").Offset(filter.Offset).Limit(filter.Limit).Find(&values).Error; err != nil {
		return nil, 0, log.WrapError(err, "ExperienceStore.List")
	}
	items := make([]entity.Experience, 0, len(values))
	for index := range values {
		if values[index].Tombstoned {
			items = append(items, experienceFromPO(values[index]))
			continue
		}
		item, err := s.hydrate(ctx, &values[index])
		if err != nil {
			return nil, 0, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, total, nil
}

func (s *Store) SearchCandidates(ctx context.Context, ownerID string, request entity.SearchRequest, limit int) ([]entity.SearchCandidate, error) {
	limit = normalizeLimit(limit)
	db := s.data.DB(ctx).Table("os_experience AS experience").
		Select("experience.*, payload.content, payload.search_text, payload.vector").
		Joins("JOIN os_experience_payload AS payload ON payload.experience_id = experience.experience_id").
		Where("experience.owner_id = ? AND experience.status = ? AND experience.tombstoned = ?", ownerID, entity.StatusReady, false)
	if request.FailureClass != "" {
		db = db.Where("experience.failure_class = ?", request.FailureClass)
	}
	if request.Outcome != "" {
		db = db.Where("experience.outcome = ?", request.Outcome)
	}
	if request.Budget.MaxSensitivity != "" {
		db = db.Where("experience.sensitivity IN ?", allowedSensitivities(request.Budget.MaxSensitivity))
	}
	type row struct {
		po.Experience
		Content    string
		SearchText string
		Vector     string
	}
	rows := make([]row, 0, limit)
	if err := db.Order("experience.created_at DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.SearchCandidates")
	}
	items := make([]entity.SearchCandidate, 0, len(rows))
	for _, value := range rows {
		var experience entity.Experience
		if err := json.Unmarshal([]byte(value.Content), &experience); err != nil {
			return nil, log.WrapError(err, "ExperienceStore.SearchCandidates.decode")
		}
		vector := make([]float64, 0)
		_ = json.Unmarshal([]byte(value.Vector), &vector)
		items = append(items, entity.SearchCandidate{Experience: experience, SearchText: value.SearchText, Vector: vector})
	}
	return items, nil
}

func (s *Store) DeletePayload(ctx context.Context, ownerID, experienceID string, deletedAt time.Time) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var audit po.Experience
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("experience_id = ? AND owner_id = ?", experienceID, ownerID).Limit(1).Find(&audit)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		if audit.Tombstoned {
			return nil
		}
		return deleteExperiencePayload(tx, audit, deletedAt)
	}), "ExperienceStore.DeletePayload")
}

func (s *Store) DeleteAllPayloads(ctx context.Context, ownerID string, deletedAt time.Time) (int64, error) {
	var deleted int64
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		values := make([]po.Experience, 0)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_id = ? AND tombstoned = ?", ownerID, false).Find(&values).Error; err != nil {
			return err
		}
		for _, value := range values {
			if err := deleteExperiencePayload(tx, value, deletedAt); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	return deleted, log.WrapError(err, "ExperienceStore.DeleteAllPayloads")
}

func deleteExperiencePayload(tx *gorm.DB, audit po.Experience, deletedAt time.Time) error {
	experienceID, ownerID := audit.ExperienceID, audit.OwnerID
	if err := tx.Where("experience_id = ? AND owner_id = ?", experienceID, ownerID).Delete(&po.ExperiencePayload{}).Error; err != nil {
		return err
	}
	if err := tx.Where("experience_id = ? AND owner_id = ?", experienceID, ownerID).Delete(&po.ExperienceRedaction{}).Error; err != nil {
		return err
	}
	if err := deleteDerivedEvaluationData(tx, ownerID, experienceID); err != nil {
		return err
	}
	if err := tx.Model(&po.FailureClassification{}).Where("experience_id = ? AND owner_id = ?", experienceID, ownerID).
		Updates(map[string]any{"summary": "", "evidence": "[]", "updated_at": millis(deletedAt)}).Error; err != nil {
		return err
	}
	return tx.Model(&po.Experience{}).Where("experience_id = ? AND owner_id = ?", experienceID, ownerID).Updates(map[string]any{
		"status": entity.StatusDeleted, "skip_reason": "", "outcome": "", "tombstoned": true, "updated_at": millis(deletedAt),
	}).Error
}

// deleteDerivedEvaluationData prevents a user-deleted Experience from
// surviving in immutable fixture snapshots. Runs for affected suites are also
// removed because their aggregate metrics are no longer reproducible.
func deleteDerivedEvaluationData(tx *gorm.DB, ownerID, experienceID string) error {
	fixtures := make([]po.EvaluationFixture, 0)
	if err := tx.Select("fixture_id").Where("owner_id = ? AND experience_id = ?", ownerID, experienceID).Find(&fixtures).Error; err != nil {
		return err
	}
	if len(fixtures) == 0 {
		return nil
	}
	fixtureIDs := make([]string, 0, len(fixtures))
	removed := make(map[string]struct{}, len(fixtures))
	for _, fixture := range fixtures {
		fixtureIDs = append(fixtureIDs, fixture.FixtureID)
		removed[fixture.FixtureID] = struct{}{}
	}
	suites := make([]po.EvaluationSuite, 0)
	if err := tx.Where("owner_id = ?", ownerID).Find(&suites).Error; err != nil {
		return err
	}
	for _, suite := range suites {
		fixtureList := make([]string, 0)
		decodeErr := json.Unmarshal([]byte(suite.FixtureIDs), &fixtureList)
		remaining := make([]string, 0, len(fixtureList))
		// A malformed suite cannot prove that it excludes the deleted fixture.
		// Remove it conservatively so corrupted derived data never blocks a
		// user-requested privacy deletion.
		affected := decodeErr != nil
		for _, fixtureID := range fixtureList {
			if _, exists := removed[fixtureID]; exists {
				affected = true
				continue
			}
			remaining = append(remaining, fixtureID)
		}
		if !affected {
			continue
		}
		runs := make([]po.EvaluationRun, 0)
		if err := tx.Select("run_id").Where("owner_id = ? AND suite_id = ?", ownerID, suite.SuiteID).Find(&runs).Error; err != nil {
			return err
		}
		runIDs := make([]string, 0, len(runs))
		for _, run := range runs {
			runIDs = append(runIDs, run.RunID)
		}
		if len(runIDs) > 0 {
			if err := tx.Where("owner_id = ? AND run_id IN ?", ownerID, runIDs).Delete(&po.EvaluationResult{}).Error; err != nil {
				return err
			}
			if err := tx.Where("owner_id = ? AND run_id IN ?", ownerID, runIDs).Delete(&po.EvaluationRun{}).Error; err != nil {
				return err
			}
		}
		if len(remaining) == 0 {
			if err := tx.Where("owner_id = ? AND suite_id = ?", ownerID, suite.SuiteID).Delete(&po.EvaluationSuite{}).Error; err != nil {
				return err
			}
			continue
		}
		if err := tx.Model(&po.EvaluationSuite{}).Where("owner_id = ? AND suite_id = ?", ownerID, suite.SuiteID).
			Updates(map[string]any{"fixture_ids": encodeJSON(remaining), "updated_at": millis(time.Now().UTC())}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("owner_id = ? AND fixture_id IN ?", ownerID, fixtureIDs).Delete(&po.EvaluationResult{}).Error; err != nil {
		return err
	}
	return tx.Where("owner_id = ? AND fixture_id IN ?", ownerID, fixtureIDs).Delete(&po.EvaluationFixture{}).Error
}

func (s *Store) PurgeExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	limit = normalizeLimit(limit)
	values := make([]po.Experience, 0, limit)
	if err := s.data.DB(ctx).Where("tombstoned = ? AND delete_at > 0 AND delete_at <= ?", false, millis(now)).Order("delete_at ASC").Limit(limit).Find(&values).Error; err != nil {
		return 0, log.WrapError(err, "ExperienceStore.PurgeExpired.list")
	}
	var purged int64
	for _, value := range values {
		if err := s.DeletePayload(ctx, value.OwnerID, value.ExperienceID, now); err != nil {
			return purged, log.WrapError(err, "ExperienceStore.PurgeExpired.delete")
		}
		purged++
	}
	return purged, nil
}

func (s *Store) Stats(ctx context.Context, ownerID string) (*entity.Stats, error) {
	db := s.data.DB(ctx).Model(&po.Experience{})
	if ownerID != "" {
		db = db.Where("owner_id = ?", ownerID)
	}
	stats := &entity.Stats{FailureClasses: make(map[string]int64)}
	if err := db.Count(&stats.Total).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.Stats.total")
	}
	for status, destination := range map[string]*int64{entity.StatusReady: &stats.Ready, entity.StatusSkipped: &stats.Skipped, entity.StatusDeleted: &stats.Deleted} {
		query := s.data.DB(ctx).Model(&po.Experience{}).Where("status = ?", status)
		if ownerID != "" {
			query = query.Where("owner_id = ?", ownerID)
		}
		if err := query.Count(destination).Error; err != nil {
			return nil, log.WrapError(err, "ExperienceStore.Stats.status")
		}
	}
	redactionQuery := s.data.DB(ctx).Model(&po.ExperienceRedaction{})
	if ownerID != "" {
		redactionQuery = redactionQuery.Where("owner_id = ?", ownerID)
	}
	if err := redactionQuery.Count(&stats.Redactions).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.Stats.redactions")
	}
	type failureRow struct {
		Class string
		Count int64
	}
	failures := make([]failureRow, 0)
	failureQuery := s.data.DB(ctx).Model(&po.FailureClassification{}).Select("class, COUNT(*) AS count")
	if ownerID != "" {
		failureQuery = failureQuery.Where("owner_id = ?", ownerID)
	}
	if err := failureQuery.Group("class").Scan(&failures).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.Stats.failures")
	}
	for _, row := range failures {
		stats.FailureClasses[row.Class] = row.Count
	}
	runQuery := s.data.DB(ctx).Model(&po.EvaluationRun{})
	if ownerID != "" {
		runQuery = runQuery.Where("owner_id = ?", ownerID)
	}
	if err := runQuery.Count(&stats.EvaluationRuns).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.Stats.runs")
	}
	type resultCount struct{ Total, Passed int64 }
	var counts resultCount
	resultQuery := s.data.DB(ctx).Model(&po.EvaluationResult{}).Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN passed THEN 1 ELSE 0 END), 0) AS passed")
	if ownerID != "" {
		resultQuery = resultQuery.Where("owner_id = ?", ownerID)
	}
	if err := resultQuery.Scan(&counts).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.Stats.results")
	}
	if counts.Total > 0 {
		stats.EvaluationPassRate = float64(counts.Passed) / float64(counts.Total)
	}
	return stats, nil
}

func (s *Store) ModelUsage(ctx context.Context, ownerID, sessionID string, startedAt, endedAt time.Time) ([]entity.ModelUsage, error) {
	if ownerID == "" || sessionID == "" {
		return []entity.ModelUsage{}, nil
	}
	type row struct {
		ModelID          string
		Model            string
		Calls            int64
		PromptTokens     int64
		CompletionTokens int64
		CostAmount       float64
	}
	rows := make([]row, 0)
	db := s.data.DB(ctx).Model(&chatpo.ChatTokenStats{}).
		Select("model_id, model, COALESCE(SUM(request_count), 0) AS calls, COALESCE(SUM(input_tokens), 0) AS prompt_tokens, COALESCE(SUM(output_tokens), 0) AS completion_tokens, COALESCE(SUM(cost_amount), 0) AS cost_amount").
		Where("user_id = ? AND session_id = ?", ownerID, sessionID)
	if !startedAt.IsZero() {
		db = db.Where("created_at >= ?", millis(startedAt))
	}
	if !endedAt.IsZero() {
		db = db.Where("created_at <= ?", millis(endedAt.Add(5*time.Second)))
	}
	if err := db.Group("model_id, model").Scan(&rows).Error; err != nil {
		return nil, log.WrapError(err, "ExperienceStore.ModelUsage")
	}
	usage := make([]entity.ModelUsage, 0, len(rows))
	for _, row := range rows {
		usage = append(usage, entity.ModelUsage{
			ModelID: row.ModelID, Model: row.Model, Calls: row.Calls, PromptTokens: row.PromptTokens,
			CompletionTokens: row.CompletionTokens, CostMicros: int64(row.CostAmount * 1_000_000),
		})
	}
	return usage, nil
}

func (s *Store) hydrate(ctx context.Context, audit *po.Experience) (*entity.Experience, error) {
	if audit == nil || audit.Tombstoned {
		return nil, nil
	}
	if audit.Status == entity.StatusSkipped {
		value := experienceFromPO(*audit)
		return &value, nil
	}
	var payload po.ExperiencePayload
	result := s.data.DB(ctx).Where("experience_id = ? AND owner_id = ?", audit.ExperienceID, audit.OwnerID).Limit(1).Find(&payload)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ExperienceStore.hydrate.payload")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	var value entity.Experience
	if err := json.Unmarshal([]byte(payload.Content), &value); err != nil {
		return nil, log.WrapError(err, "ExperienceStore.hydrate.decode")
	}
	return &value, nil
}

func experienceToPO(value entity.Experience) po.Experience {
	failureClass := ""
	if value.Failure != nil {
		failureClass = value.Failure.Class
	}
	return po.Experience{
		ExperienceID: value.ExperienceID, OwnerID: value.OwnerID, TaskID: value.TaskID, Schema: value.Schema,
		Status: value.Status, SkipReason: value.SkipReason, Outcome: value.Outcome, FailureClass: failureClass,
		Sensitivity: value.Sensitivity, RetentionDays: value.Retention.Days, DeleteAt: millis(value.Retention.DeleteAt),
		TraceID: value.Provenance.TraceID, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt),
	}
}

func experienceFromPO(value po.Experience) entity.Experience {
	payloadMode := entity.PayloadNone
	deletedAt := time.Time{}
	if value.Tombstoned || value.Status == entity.StatusDeleted {
		payloadMode = entity.PayloadDeleted
		deletedAt = fromMillis(value.UpdatedAt)
	}
	return entity.Experience{
		Schema: value.Schema, ExperienceID: value.ExperienceID, OwnerID: value.OwnerID, TaskID: value.TaskID,
		Status: value.Status, SkipReason: value.SkipReason, Outcome: value.Outcome, Sensitivity: value.Sensitivity,
		Retention:  entity.RetentionPolicy{Days: value.RetentionDays, PayloadMode: payloadMode, DeleteAt: fromMillis(value.DeleteAt)},
		Provenance: entity.Provenance{TraceID: value.TraceID, Protocol: "athena.agent.v4", GeneratedBy: "experience-engine", GeneratedAt: fromMillis(value.CreatedAt)},
		CreatedAt:  fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt), DeletedAt: deletedAt,
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maximumListLimit {
		return maximumListLimit
	}
	return limit
}

func allowedSensitivities(max string) []string {
	switch max {
	case entity.SensitivityInternal:
		return []string{entity.SensitivityInternal}
	case entity.SensitivityRestricted:
		return []string{entity.SensitivityInternal, entity.SensitivitySensitive, entity.SensitivityRestricted}
	default:
		return []string{entity.SensitivityInternal, entity.SensitivitySensitive}
	}
}

func encodeJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

func millis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
