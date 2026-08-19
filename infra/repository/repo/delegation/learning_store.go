package delegation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

var _ repository.LearningStore = (*Store)(nil)

func (s *Store) GetLearningPreference(ctx context.Context, ownerID string) (entity.LearningPreference, error) {
	var row po.LearningPreference
	result := s.data.DB(ctx).Where("owner_id = ?", ownerID).Limit(1).Find(&row)
	if result.Error != nil {
		return entity.LearningPreference{}, log.WrapError(result.Error, "DelegationLearningStore.GetPreference")
	}
	if result.RowsAffected == 0 {
		return entity.LearningPreference{OwnerID: ownerID, Enabled: true}, nil
	}
	return learningPreferenceEntity(row), nil
}

func (s *Store) SetLearningPreference(ctx context.Context, value entity.LearningPreference, expectedRevision int64, event entity.Event) error {
	if strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.UpdatedBy) == "" || value.Revision != expectedRevision+1 || value.UpdatedAt.IsZero() {
		return fmt.Errorf("learning preference requires owner, updater, next revision, and timestamp")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var row po.LearningPreference
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_id = ?", value.OwnerID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedRevision != 0 {
				return ErrRevisionConflict
			}
			created := learningPreferenceRow(value)
			if err := tx.Create(&created).Error; err != nil {
				return err
			}
			return createEvent(tx, event)
		}
		if err != nil {
			return err
		}
		if row.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		updated := tx.Model(&po.LearningPreference{}).Where("owner_id = ? AND revision = ?", value.OwnerID, expectedRevision).Updates(map[string]any{
			"enabled": value.Enabled, "revision": value.Revision, "updated_by": value.UpdatedBy, "updated_at": millis(value.UpdatedAt),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.SetPreference")
}

func (s *Store) CreateLearningCandidate(ctx context.Context, value entity.LearningCandidate, event entity.Event) error {
	if strings.TrimSpace(value.CandidateID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.DefinitionHash) == "" || strings.TrimSpace(value.Content) == "" || value.CreatedAt.IsZero() {
		return fmt.Errorf("learning candidate requires immutable identity, owner, content, hash, and timestamp")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.LearningCandidate
		found := tx.Where("candidate_id = ?", value.CandidateID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.OwnerID == value.OwnerID && existing.DefinitionHash == value.DefinitionHash {
				return nil
			}
			return ErrIdempotencyConflict
		}
		row := learningCandidateRow(value)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.CreateCandidate")
}

func (s *Store) FindLearningCandidate(ctx context.Context, ownerID, candidateID string) (*entity.LearningCandidate, error) {
	var row po.LearningCandidate
	result := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationLearningStore.FindCandidate")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := learningCandidateEntity(row)
	return &value, nil
}

func (s *Store) ListLearningCandidates(ctx context.Context, ownerID string, limit int) ([]entity.LearningCandidate, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []po.LearningCandidate
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationLearningStore.ListCandidates")
	}
	result := make([]entity.LearningCandidate, 0, len(rows))
	for _, row := range rows {
		result = append(result, learningCandidateEntity(row))
	}
	return result, nil
}

func (s *Store) CreateLearningEvaluation(ctx context.Context, value entity.LearningEvaluation, event entity.Event) error {
	if strings.TrimSpace(value.EvaluationID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.CandidateID) == "" || strings.TrimSpace(value.ContentHash) == "" || strings.TrimSpace(value.Content) == "" || value.CreatedAt.IsZero() {
		return fmt.Errorf("learning evaluation requires immutable identity, owner, candidate, content, hash, and timestamp")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireLearningCandidate(tx, value.OwnerID, value.CandidateID); err != nil {
			return err
		}
		row := learningEvaluationRow(value)
		if err := tx.Create(&row).Error; err != nil {
			if isDuplicate(err) {
				return ErrIdempotencyConflict
			}
			return err
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.CreateEvaluation")
}

func (s *Store) FindLearningEvaluation(ctx context.Context, ownerID, evaluationID string) (*entity.LearningEvaluation, error) {
	var row po.LearningEvaluation
	result := s.data.DB(ctx).Where("owner_id = ? AND evaluation_id = ?", ownerID, evaluationID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationLearningStore.FindEvaluation")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := learningEvaluationEntity(row)
	return &value, nil
}

func (s *Store) ListLearningEvaluations(ctx context.Context, ownerID, candidateID string) ([]entity.LearningEvaluation, error) {
	var rows []po.LearningEvaluation
	if err := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationLearningStore.ListEvaluations")
	}
	result := make([]entity.LearningEvaluation, 0, len(rows))
	for _, row := range rows {
		result = append(result, learningEvaluationEntity(row))
	}
	return result, nil
}

func (s *Store) CreateLearningReview(ctx context.Context, value entity.LearningReview, event entity.Event) error {
	if strings.TrimSpace(value.ReviewID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.CandidateID) == "" || strings.TrimSpace(value.EvaluationID) == "" || strings.TrimSpace(value.ReviewerID) == "" || strings.TrimSpace(value.ContentHash) == "" || strings.TrimSpace(value.Content) == "" || value.CreatedAt.IsZero() {
		return fmt.Errorf("learning review requires immutable reviewer and candidate evaluation bindings")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var evaluation po.LearningEvaluation
		if err := tx.Where("owner_id = ? AND candidate_id = ? AND evaluation_id = ?", value.OwnerID, value.CandidateID, value.EvaluationID).Take(&evaluation).Error; err != nil {
			return err
		}
		row := learningReviewRow(value)
		if err := tx.Create(&row).Error; err != nil {
			if isDuplicate(err) {
				return ErrIdempotencyConflict
			}
			return err
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.CreateReview")
}

func (s *Store) FindApprovedLearningReview(ctx context.Context, ownerID, candidateID string) (*entity.LearningReview, error) {
	var row po.LearningReview
	result := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ? AND decision = ?", ownerID, candidateID, dso.LearningReviewApprove).Order("created_at DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationLearningStore.FindApprovedReview")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := learningReviewEntity(row)
	return &value, nil
}

func (s *Store) ListLearningReviews(ctx context.Context, ownerID, candidateID string) ([]entity.LearningReview, error) {
	var rows []po.LearningReview
	if err := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationLearningStore.ListReviews")
	}
	result := make([]entity.LearningReview, 0, len(rows))
	for _, row := range rows {
		result = append(result, learningReviewEntity(row))
	}
	return result, nil
}

func (s *Store) CreateLearningRollout(ctx context.Context, value entity.LearningRollout, event entity.Event) error {
	if strings.TrimSpace(value.RolloutID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.CandidateID) == "" || strings.TrimSpace(value.ContentHash) == "" || strings.TrimSpace(value.Content) == "" || value.Revision != 1 || value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() {
		return fmt.Errorf("learning rollout requires immutable governance content and initial revision")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireLearningCandidate(tx, value.OwnerID, value.CandidateID); err != nil {
			return err
		}
		row := learningRolloutRow(value)
		if err := tx.Create(&row).Error; err != nil {
			if isDuplicate(err) {
				return ErrIdempotencyConflict
			}
			return err
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.CreateRollout")
}

func (s *Store) FindLearningRollout(ctx context.Context, ownerID, rolloutID string) (*entity.LearningRollout, error) {
	var row po.LearningRollout
	result := s.data.DB(ctx).Where("owner_id = ? AND rollout_id = ?", ownerID, rolloutID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationLearningStore.FindRollout")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := learningRolloutEntity(row)
	return &value, nil
}

func (s *Store) FindEffectiveLearningRollout(ctx context.Context, ownerID, kind string) (*entity.LearningRollout, error) {
	var row po.LearningRollout
	result := s.data.DB(ctx).Where("owner_id = ? AND kind = ? AND status IN ?", ownerID, kind, []string{dso.LearningRolloutPromoted, dso.LearningRolloutCanary}).
		Order("CASE WHEN status = 'PROMOTED' THEN 0 ELSE 1 END, updated_at DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationLearningStore.FindEffectiveRollout")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := learningRolloutEntity(row)
	return &value, nil
}

func (s *Store) ListLearningRollouts(ctx context.Context, ownerID string, limit int) ([]entity.LearningRollout, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []po.LearningRollout
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("updated_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationLearningStore.ListRollouts")
	}
	result := make([]entity.LearningRollout, 0, len(rows))
	for _, row := range rows {
		result = append(result, learningRolloutEntity(row))
	}
	return result, nil
}

func (s *Store) TransitionLearningRollout(ctx context.Context, ownerID, rolloutID string, expectedRevision int64, status, contentHash, content string, at time.Time, event entity.Event) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&po.LearningRollout{}).Where("owner_id = ? AND rollout_id = ? AND revision = ?", ownerID, rolloutID, expectedRevision).Updates(map[string]any{
			"status": status, "revision": expectedRevision + 1, "content_hash": contentHash, "content": content, "updated_at": millis(at),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.TransitionRollout")
}

func (s *Store) CreateLearningBenchmark(ctx context.Context, value entity.LearningBenchmark, event entity.Event) error {
	if strings.TrimSpace(value.ReportID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.CandidateID) == "" || strings.TrimSpace(value.RolloutID) == "" || strings.TrimSpace(value.ContentHash) == "" || strings.TrimSpace(value.Content) == "" || value.CreatedAt.IsZero() {
		return fmt.Errorf("learning benchmark requires immutable identity, rollout, content, hash, and timestamp")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var rollout po.LearningRollout
		if err := tx.Where("owner_id = ? AND candidate_id = ? AND rollout_id = ?", value.OwnerID, value.CandidateID, value.RolloutID).Take(&rollout).Error; err != nil {
			return err
		}
		row := learningBenchmarkRow(value)
		if err := tx.Create(&row).Error; err != nil {
			if isDuplicate(err) {
				return ErrIdempotencyConflict
			}
			return err
		}
		return createEvent(tx, event)
	}), "DelegationLearningStore.CreateBenchmark")
}

func (s *Store) ListLearningBenchmarks(ctx context.Context, ownerID, rolloutID string) ([]entity.LearningBenchmark, error) {
	var rows []po.LearningBenchmark
	if err := s.data.DB(ctx).Where("owner_id = ? AND rollout_id = ?", ownerID, rolloutID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationLearningStore.ListBenchmarks")
	}
	result := make([]entity.LearningBenchmark, 0, len(rows))
	for _, row := range rows {
		result = append(result, learningBenchmarkEntity(row))
	}
	return result, nil
}

func requireLearningCandidate(tx *gorm.DB, ownerID, candidateID string) error {
	var count int64
	if err := tx.Model(&po.LearningCandidate{}).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func learningPreferenceRow(value entity.LearningPreference) po.LearningPreference {
	return po.LearningPreference{OwnerID: value.OwnerID, Enabled: value.Enabled, Revision: value.Revision, UpdatedBy: value.UpdatedBy, UpdatedAt: millis(value.UpdatedAt)}
}

func learningPreferenceEntity(value po.LearningPreference) entity.LearningPreference {
	return entity.LearningPreference{OwnerID: value.OwnerID, Enabled: value.Enabled, Revision: value.Revision, UpdatedBy: value.UpdatedBy, UpdatedAt: fromMillis(value.UpdatedAt)}
}

func learningCandidateRow(value entity.LearningCandidate) po.LearningCandidate {
	return po.LearningCandidate{CandidateID: value.CandidateID, OwnerID: value.OwnerID, Kind: value.Kind, DefinitionHash: value.DefinitionHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func learningCandidateEntity(value po.LearningCandidate) entity.LearningCandidate {
	return entity.LearningCandidate{CandidateID: value.CandidateID, OwnerID: value.OwnerID, Kind: value.Kind, DefinitionHash: value.DefinitionHash, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}

func learningEvaluationRow(value entity.LearningEvaluation) po.LearningEvaluation {
	return po.LearningEvaluation{EvaluationID: value.EvaluationID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, Stage: value.Stage, Passed: value.Passed, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func learningEvaluationEntity(value po.LearningEvaluation) entity.LearningEvaluation {
	return entity.LearningEvaluation{EvaluationID: value.EvaluationID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, Stage: value.Stage, Passed: value.Passed, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}

func learningReviewRow(value entity.LearningReview) po.LearningReview {
	return po.LearningReview{ReviewID: value.ReviewID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, EvaluationID: value.EvaluationID, Decision: value.Decision, ReviewerID: value.ReviewerID, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func learningReviewEntity(value po.LearningReview) entity.LearningReview {
	return entity.LearningReview{ReviewID: value.ReviewID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, EvaluationID: value.EvaluationID, Decision: value.Decision, ReviewerID: value.ReviewerID, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}

func learningRolloutRow(value entity.LearningRollout) po.LearningRollout {
	return po.LearningRollout{RolloutID: value.RolloutID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, Kind: value.Kind, Status: value.Status, RiskCeiling: value.RiskCeiling, Revision: value.Revision, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
}

func learningRolloutEntity(value po.LearningRollout) entity.LearningRollout {
	return entity.LearningRollout{RolloutID: value.RolloutID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, Kind: value.Kind, Status: value.Status, RiskCeiling: value.RiskCeiling, Revision: value.Revision, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt)}
}

func learningBenchmarkRow(value entity.LearningBenchmark) po.LearningBenchmark {
	return po.LearningBenchmark{ReportID: value.ReportID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, RolloutID: value.RolloutID, Passed: value.Passed, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func learningBenchmarkEntity(value po.LearningBenchmark) entity.LearningBenchmark {
	return entity.LearningBenchmark{ReportID: value.ReportID, OwnerID: value.OwnerID, CandidateID: value.CandidateID, RolloutID: value.RolloutID, Passed: value.Passed, ContentHash: value.ContentHash, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}
