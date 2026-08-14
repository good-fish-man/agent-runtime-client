package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/learning"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	controlpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	log "github.com/good-fish-man/logx"
)

const (
	defaultLimit = 50
	maximumLimit = 200
)

var ErrRevisionConflict = errors.New("learning record revision conflict")

type Store struct{ data *data.Data }

func NewStore(value *data.Data) *Store { return &Store{data: value} }

var _ repository.Store = (*Store)(nil)

func (s *Store) CapabilityPolicies(ctx context.Context, capabilityIDs []string) (map[string]entity.CapabilityPolicy, error) {
	result := make(map[string]entity.CapabilityPolicy, len(capabilityIDs))
	if len(capabilityIDs) == 0 {
		return result, nil
	}
	values := make([]controlpo.CapabilityDefinition, 0, len(capabilityIDs))
	if err := s.data.DB(ctx).Where("capability_id IN ?", uniqueStrings(capabilityIDs)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.CapabilityPolicies")
	}
	for _, value := range values {
		result[value.CapabilityID] = entity.CapabilityPolicy{Risk: normalizeRisk(value.Risk), Enabled: value.Enabled}
	}
	return result, nil
}

func (s *Store) ApprovedSkills(ctx context.Context, ownerID string) (map[string]bool, error) {
	values := make([]po.Skill, 0)
	if err := s.data.DB(ctx).Where("status = ? AND deleted_at = 0 AND (owner_id = ? OR visibility = ?)", entity.LifecycleApproved, ownerID, entity.VisibilityPublic).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.ApprovedSkills")
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value.SkillID] = true
	}
	return result, nil
}

func (s *Store) CreateCandidate(ctx context.Context, candidate entity.Candidate, evidence []entity.CandidateEvidence, evaluation entity.CandidateEvaluation) error {
	definition, err := candidateDefinition(candidate)
	if err != nil {
		return err
	}
	value := po.Candidate{
		CandidateID: candidate.CandidateID, OwnerID: candidate.OwnerID, Kind: candidate.Kind, Status: candidate.Status,
		Definition: definition, Evidence: encodeJSON(candidate.Evidence), Evaluation: encodeJSON(candidate.Evaluation),
		ReviewNote: candidate.ReviewNote, Revision: candidate.Revision, TraceID: candidate.TraceID,
		CreatedAt: millis(candidate.CreatedAt), UpdatedAt: millis(candidate.UpdatedAt),
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&value).Error; err != nil {
			return err
		}
		for _, item := range evidence {
			row := po.CandidateEvidence{
				EvidenceID: item.EvidenceID, CandidateID: candidate.CandidateID, OwnerID: candidate.OwnerID,
				ExperienceID: item.ExperienceID, Relation: item.Relation, Outcome: item.Outcome,
				Summary: item.Summary, TraceID: item.TraceID, CreatedAt: millis(item.CreatedAt),
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		row := po.CandidateEvaluation{
			EvaluationID: evaluation.EvaluationID, CandidateID: candidate.CandidateID, OwnerID: candidate.OwnerID,
			RunID: evaluation.RunID, Summary: encodeJSON(evaluation.Summary), TraceID: evaluation.TraceID,
			CreatedAt: millis(evaluation.CreatedAt),
		}
		return tx.Create(&row).Error
	}), "LearningStore.CreateCandidate")
}

func (s *Store) FindCandidate(ctx context.Context, ownerID, candidateID string) (*entity.Candidate, error) {
	var value po.Candidate
	result := s.data.DB(ctx).Where("candidate_id = ? AND owner_id = ? AND deleted_at = 0", candidateID, ownerID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "LearningStore.FindCandidate")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodeCandidate(value)
}

func (s *Store) ListCandidates(ctx context.Context, ownerID string, filter entity.CandidateFilter) ([]entity.Candidate, int64, error) {
	filter.Limit = normalizeLimit(filter.Limit)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	db := s.data.DB(ctx).Model(&po.Candidate{}).Where("owner_id = ? AND deleted_at = 0", ownerID)
	if filter.Kind != "" {
		db = db.Where("kind = ?", filter.Kind)
	}
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, log.WrapError(err, "LearningStore.ListCandidates.count")
	}
	values := make([]po.Candidate, 0, filter.Limit)
	if err := db.Order("created_at DESC").Offset(filter.Offset).Limit(filter.Limit).Find(&values).Error; err != nil {
		return nil, 0, log.WrapError(err, "LearningStore.ListCandidates")
	}
	items := make([]entity.Candidate, 0, len(values))
	for _, value := range values {
		item, err := decodeCandidate(value)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, nil
}

func (s *Store) UpdateCandidate(ctx context.Context, candidate entity.Candidate, expectedRevision int64) error {
	definition, err := candidateDefinition(candidate)
	if err != nil {
		return err
	}
	result := s.data.DB(ctx).Model(&po.Candidate{}).
		Where("candidate_id = ? AND owner_id = ? AND revision = ? AND deleted_at = 0", candidate.CandidateID, candidate.OwnerID, expectedRevision).
		Updates(map[string]any{
			"kind": candidate.Kind, "status": candidate.Status, "definition": definition,
			"evidence": encodeJSON(candidate.Evidence), "evaluation": encodeJSON(candidate.Evaluation),
			"review_note": candidate.ReviewNote, "revision": candidate.Revision,
			"trace_id": candidate.TraceID, "updated_at": millis(candidate.UpdatedAt),
		})
	if result.Error != nil {
		return log.WrapError(result.Error, "LearningStore.UpdateCandidate")
	}
	if result.RowsAffected != 1 {
		return log.WrapError(ErrRevisionConflict, "LearningStore.UpdateCandidate")
	}
	return nil
}

func (s *Store) SaveCandidateEvaluation(ctx context.Context, candidate entity.Candidate, evaluation entity.CandidateEvaluation, expectedRevision int64) error {
	definition, err := candidateDefinition(candidate)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&po.Candidate{}).
			Where("candidate_id = ? AND owner_id = ? AND revision = ? AND deleted_at = 0", candidate.CandidateID, candidate.OwnerID, expectedRevision).
			Updates(map[string]any{
				"status": candidate.Status, "definition": definition,
				"evaluation": encodeJSON(candidate.Evaluation), "review_note": candidate.ReviewNote,
				"revision": candidate.Revision, "trace_id": candidate.TraceID,
				"updated_at": millis(candidate.UpdatedAt),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		row := po.CandidateEvaluation{
			EvaluationID: evaluation.EvaluationID, CandidateID: candidate.CandidateID, OwnerID: candidate.OwnerID,
			RunID: evaluation.RunID, Summary: encodeJSON(evaluation.Summary), TraceID: evaluation.TraceID,
			CreatedAt: millis(evaluation.CreatedAt),
		}
		return tx.Create(&row).Error
	}), "LearningStore.SaveCandidateEvaluation")
}

func (s *Store) ReviewCandidate(ctx context.Context, ownerID, candidateID, decision, note, reviewerID string, expectedRevision int64) (*entity.Candidate, error) {
	var reviewed *entity.Candidate
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var row po.Candidate
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("candidate_id = ? AND owner_id = ? AND deleted_at = 0", candidateID, ownerID).Limit(1).Find(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		candidate, err := decodeCandidate(row)
		if err != nil {
			return err
		}
		if candidate.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if candidate.Status != entity.LifecycleReviewRequired {
			return fmt.Errorf("candidate is not awaiting review")
		}
		switch decision {
		case "APPROVE":
			if !candidate.Evaluation.Passed {
				return fmt.Errorf("candidate evaluation did not meet the approval gate")
			}
			candidate.Status = entity.LifecycleApproved
			if candidate.Skill != nil {
				candidate.Skill.LifecycleState = entity.LifecycleApproved
			}
			if candidate.Strategy != nil {
				candidate.Strategy.LifecycleState = entity.LifecycleApproved
			}
		case "REJECT":
			candidate.Status = entity.LifecycleRejected
			if candidate.Skill != nil {
				candidate.Skill.LifecycleState = entity.LifecycleRejected
			}
			if candidate.Strategy != nil {
				candidate.Strategy.LifecycleState = entity.LifecycleRejected
			}
		default:
			return fmt.Errorf("review decision must be APPROVE or REJECT")
		}
		candidate.ReviewNote = strings.TrimSpace(note)
		candidate.Revision++
		candidate.UpdatedAt = time.Now().UTC()
		definition, err := candidateDefinition(*candidate)
		if err != nil {
			return err
		}
		if err := tx.Model(&po.Candidate{}).Where("candidate_id = ? AND owner_id = ? AND revision = ?", candidateID, ownerID, expectedRevision).Updates(map[string]any{
			"status": candidate.Status, "definition": definition, "review_note": candidate.ReviewNote,
			"revision": candidate.Revision, "updated_at": millis(candidate.UpdatedAt),
		}).Error; err != nil {
			return err
		}
		if decision == "APPROVE" {
			if err := materialize(tx, *candidate, reviewerID); err != nil {
				return err
			}
		}
		reviewed = candidate
		return nil
	})
	if err != nil {
		return nil, log.WrapError(err, "LearningStore.ReviewCandidate")
	}
	return reviewed, nil
}

func materialize(tx *gorm.DB, candidate entity.Candidate, reviewerID string) error {
	now := candidate.UpdatedAt
	if candidate.Skill != nil {
		definition := encodeJSON(candidate.Skill)
		version := po.SkillVersion{
			VersionID: ulid.New(), SkillID: candidate.Skill.ID, Version: candidate.Skill.Version,
			OwnerID: candidate.OwnerID, CandidateID: candidate.CandidateID, Definition: definition,
			Checksum: checksum(definition), TraceID: candidate.TraceID, CreatedAt: millis(now),
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		value := po.Skill{
			SkillID: candidate.Skill.ID, OwnerID: candidate.OwnerID, LatestVersion: candidate.Skill.Version,
			Status: entity.LifecycleApproved, Visibility: candidate.Skill.Visibility, Revision: 1,
			TraceID: candidate.TraceID, CreatedAt: millis(now), UpdatedAt: millis(now),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "skill_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"latest_version": value.LatestVersion, "status": value.Status, "visibility": value.Visibility,
				"revision": gorm.Expr("revision + 1"), "trace_id": value.TraceID, "updated_at": value.UpdatedAt,
			}),
		}).Create(&value).Error
	}
	if candidate.Strategy != nil {
		definition := encodeJSON(candidate.Strategy)
		version := po.StrategyVersion{
			VersionID: ulid.New(), StrategyID: candidate.Strategy.ID, Version: candidate.Strategy.Version,
			OwnerID: candidate.OwnerID, CandidateID: candidate.CandidateID, Definition: definition,
			Checksum: checksum(definition), TraceID: candidate.TraceID, CreatedAt: millis(now),
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		value := po.Strategy{
			StrategyID: candidate.Strategy.ID, OwnerID: candidate.OwnerID, LatestVersion: candidate.Strategy.Version,
			Status: entity.LifecycleApproved, Visibility: candidate.Strategy.Visibility, Revision: 1,
			TraceID: candidate.TraceID, CreatedAt: millis(now), UpdatedAt: millis(now),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "strategy_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"latest_version": value.LatestVersion, "status": value.Status, "visibility": value.Visibility,
				"revision": gorm.Expr("revision + 1"), "trace_id": value.TraceID, "updated_at": value.UpdatedAt,
			}),
		}).Create(&value).Error
	}
	return fmt.Errorf("candidate has no declarative artifact")
}

func (s *Store) ListEvidence(ctx context.Context, ownerID, candidateID string) ([]entity.CandidateEvidence, error) {
	values := make([]po.CandidateEvidence, 0)
	if err := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Order("created_at ASC").Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.ListEvidence")
	}
	items := make([]entity.CandidateEvidence, 0, len(values))
	for _, value := range values {
		items = append(items, entity.CandidateEvidence{
			EvidenceID: value.EvidenceID, CandidateID: value.CandidateID, OwnerID: value.OwnerID,
			ExperienceID: value.ExperienceID, Relation: value.Relation, Outcome: value.Outcome,
			Summary: value.Summary, TraceID: value.TraceID, CreatedAt: fromMillis(value.CreatedAt),
		})
	}
	return items, nil
}

func (s *Store) ListEvaluations(ctx context.Context, ownerID, candidateID string) ([]entity.CandidateEvaluation, error) {
	values := make([]po.CandidateEvaluation, 0)
	if err := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Order("created_at ASC").Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.ListEvaluations")
	}
	items := make([]entity.CandidateEvaluation, 0, len(values))
	for _, value := range values {
		var summary entity.EvaluationSummary
		if err := json.Unmarshal([]byte(value.Summary), &summary); err != nil {
			return nil, log.WrapError(err, "LearningStore.ListEvaluations.decode")
		}
		items = append(items, entity.CandidateEvaluation{
			EvaluationID: value.EvaluationID, CandidateID: value.CandidateID, OwnerID: value.OwnerID,
			RunID: value.RunID, Summary: summary, TraceID: value.TraceID, CreatedAt: fromMillis(value.CreatedAt),
		})
	}
	return items, nil
}

func (s *Store) ListSkills(ctx context.Context, ownerID string, limit int) ([]entity.Skill, error) {
	values := make([]po.Skill, 0)
	if err := s.data.DB(ctx).Where("deleted_at = 0 AND (owner_id = ? OR visibility = ?)", ownerID, entity.VisibilityPublic).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.ListSkills")
	}
	items := make([]entity.Skill, 0, len(values))
	for _, value := range values {
		var version po.SkillVersion
		if err := s.data.DB(ctx).Where("skill_id = ? AND version = ?", value.SkillID, value.LatestVersion).Limit(1).Take(&version).Error; err != nil {
			return nil, log.WrapError(err, "LearningStore.ListSkills.version")
		}
		var definition entity.SkillDefinition
		if err := json.Unmarshal([]byte(version.Definition), &definition); err != nil {
			return nil, err
		}
		items = append(items, entity.Skill{
			SkillID: value.SkillID, OwnerID: value.OwnerID, LatestVersion: value.LatestVersion,
			Status: value.Status, Visibility: value.Visibility, Definition: definition, Revision: value.Revision,
			TraceID: value.TraceID, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
		})
	}
	return items, nil
}

func (s *Store) ListStrategies(ctx context.Context, ownerID string, limit int) ([]entity.Strategy, error) {
	values := make([]po.Strategy, 0)
	if err := s.data.DB(ctx).Where("deleted_at = 0 AND (owner_id = ? OR visibility = ?)", ownerID, entity.VisibilityPublic).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.ListStrategies")
	}
	items := make([]entity.Strategy, 0, len(values))
	for _, value := range values {
		var version po.StrategyVersion
		if err := s.data.DB(ctx).Where("strategy_id = ? AND version = ?", value.StrategyID, value.LatestVersion).Limit(1).Take(&version).Error; err != nil {
			return nil, log.WrapError(err, "LearningStore.ListStrategies.version")
		}
		var definition entity.StrategyDefinition
		if err := json.Unmarshal([]byte(version.Definition), &definition); err != nil {
			return nil, err
		}
		items = append(items, entity.Strategy{
			StrategyID: value.StrategyID, OwnerID: value.OwnerID, LatestVersion: value.LatestVersion,
			Status: value.Status, Visibility: value.Visibility, Definition: definition, Revision: value.Revision,
			TraceID: value.TraceID, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
		})
	}
	return items, nil
}

func (s *Store) CreateDemonstration(ctx context.Context, demonstration entity.Demonstration) error {
	value := po.Demonstration{
		DemonstrationID: demonstration.DemonstrationID, OwnerID: demonstration.OwnerID, TaskID: demonstration.TaskID,
		Status: demonstration.Status, Title: demonstration.Title, Content: encodeJSON(demonstration.Steps),
		PauseCount: demonstration.PauseCount, ConfirmedBy: demonstration.ConfirmedBy, Revision: demonstration.Revision,
		TraceID: demonstration.TraceID, CreatedAt: millis(demonstration.CreatedAt), UpdatedAt: millis(demonstration.UpdatedAt),
	}
	return log.WrapError(s.data.DB(ctx).Create(&value).Error, "LearningStore.CreateDemonstration")
}

func (s *Store) FindDemonstration(ctx context.Context, ownerID, demonstrationID string) (*entity.Demonstration, error) {
	var value po.Demonstration
	result := s.data.DB(ctx).Where("demonstration_id = ? AND owner_id = ? AND deleted_at = 0", demonstrationID, ownerID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "LearningStore.FindDemonstration")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodeDemonstration(value)
}

func (s *Store) ListDemonstrations(ctx context.Context, ownerID string, limit int) ([]entity.Demonstration, error) {
	values := make([]po.Demonstration, 0)
	if err := s.data.DB(ctx).Where("owner_id = ? AND deleted_at = 0", ownerID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "LearningStore.ListDemonstrations")
	}
	items := make([]entity.Demonstration, 0, len(values))
	for _, value := range values {
		item, err := decodeDemonstration(value)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) SaveDemonstration(ctx context.Context, demonstration entity.Demonstration, expectedRevision int64) error {
	result := s.data.DB(ctx).Model(&po.Demonstration{}).
		Where("demonstration_id = ? AND owner_id = ? AND revision = ? AND deleted_at = 0", demonstration.DemonstrationID, demonstration.OwnerID, expectedRevision).
		Updates(map[string]any{
			"status": demonstration.Status, "title": demonstration.Title, "content": encodeJSON(demonstration.Steps),
			"pause_count": demonstration.PauseCount, "confirmed_by": demonstration.ConfirmedBy,
			"revision": demonstration.Revision, "trace_id": demonstration.TraceID, "updated_at": millis(demonstration.UpdatedAt),
		})
	if result.Error != nil {
		return log.WrapError(result.Error, "LearningStore.SaveDemonstration")
	}
	if result.RowsAffected == 0 {
		return ErrRevisionConflict
	}
	return nil
}

func (s *Store) TaskActions(ctx context.Context, ownerID, taskID string) ([]entity.SemanticAction, bool, error) {
	var task controlpo.Task
	result := s.data.DB(ctx).Where("task_id = ? AND user_id = ?", taskID, ownerID).Limit(1).Find(&task)
	if result.Error != nil {
		return nil, false, log.WrapError(result.Error, "LearningStore.TaskActions.task")
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	values := make([]controlpo.Action, 0)
	if err := s.data.DB(ctx).Where("task_id = ? AND user_id = ?", taskID, ownerID).Order("sequence ASC").Find(&values).Error; err != nil {
		return nil, true, log.WrapError(err, "LearningStore.TaskActions.actions")
	}
	items := make([]entity.SemanticAction, 0, len(values))
	for _, value := range values {
		arguments := make(map[string]any)
		if strings.TrimSpace(value.Arguments) != "" {
			_ = json.Unmarshal([]byte(value.Arguments), &arguments)
		}
		items = append(items, entity.SemanticAction{Capability: value.Capability, Operation: value.Operation, Arguments: arguments})
	}
	return items, true, nil
}

func decodeCandidate(value po.Candidate) (*entity.Candidate, error) {
	result := &entity.Candidate{
		Schema: entity.Schema, CandidateID: value.CandidateID, OwnerID: value.OwnerID,
		Kind: value.Kind, Status: value.Status, ReviewNote: value.ReviewNote, Revision: value.Revision,
		TraceID: value.TraceID, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
	}
	if err := json.Unmarshal([]byte(value.Evidence), &result.Evidence); err != nil {
		return nil, log.WrapError(err, "LearningStore.decodeCandidate.evidence")
	}
	if err := json.Unmarshal([]byte(value.Evaluation), &result.Evaluation); err != nil {
		return nil, log.WrapError(err, "LearningStore.decodeCandidate.evaluation")
	}
	switch value.Kind {
	case entity.CandidateSkill:
		result.Skill = &entity.SkillDefinition{}
		if err := json.Unmarshal([]byte(value.Definition), result.Skill); err != nil {
			return nil, log.WrapError(err, "LearningStore.decodeCandidate.skill")
		}
	case entity.CandidateStrategy:
		result.Strategy = &entity.StrategyDefinition{}
		if err := json.Unmarshal([]byte(value.Definition), result.Strategy); err != nil {
			return nil, log.WrapError(err, "LearningStore.decodeCandidate.strategy")
		}
	default:
		return nil, fmt.Errorf("unsupported candidate kind %q", value.Kind)
	}
	return result, nil
}

func decodeDemonstration(value po.Demonstration) (*entity.Demonstration, error) {
	steps := make([]entity.DemonstrationStep, 0)
	if err := json.Unmarshal([]byte(value.Content), &steps); err != nil {
		return nil, log.WrapError(err, "LearningStore.decodeDemonstration")
	}
	return &entity.Demonstration{
		Schema: entity.Schema, DemonstrationID: value.DemonstrationID, OwnerID: value.OwnerID,
		TaskID: value.TaskID, Status: value.Status, Title: value.Title, Steps: steps,
		PauseCount: value.PauseCount, ConfirmedBy: value.ConfirmedBy, Revision: value.Revision,
		TraceID: value.TraceID, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
	}, nil
}

func candidateDefinition(candidate entity.Candidate) (string, error) {
	if candidate.Kind == entity.CandidateSkill && candidate.Skill != nil && candidate.Strategy == nil {
		return encodeJSON(candidate.Skill), nil
	}
	if candidate.Kind == entity.CandidateStrategy && candidate.Strategy != nil && candidate.Skill == nil {
		return encodeJSON(candidate.Strategy), nil
	}
	return "", fmt.Errorf("candidate definition does not match kind %q", candidate.Kind)
}

func normalizeRisk(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "R0", "LOW", "READ_ONLY":
		return entity.RiskLow
	case "R1", "MEDIUM", "REVERSIBLE":
		return entity.RiskMedium
	case "R2", "HIGH", "EXTERNAL_WRITE":
		return entity.RiskHigh
	case "R3", "CRITICAL", "SENSITIVE":
		return entity.RiskCritical
	default:
		return ""
	}
}

func encodeJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func checksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeLimit(value int) int {
	if value < 1 {
		return defaultLimit
	}
	if value > maximumLimit {
		return maximumLimit
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func millis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func fromMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
