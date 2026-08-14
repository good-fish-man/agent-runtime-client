package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/deployment"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	log "github.com/good-fish-man/logx"
)

const (
	defaultLimit = 50
	maximumLimit = 200
)

var ErrRevisionConflict = errors.New("deployment record revision conflict")

type Store struct{ data *data.Data }

func NewStore(value *data.Data) *Store { return &Store{data: value} }

var _ repository.Store = (*Store)(nil)

func (s *Store) ApprovedArtifactVersions(ctx context.Context, ownerID string) (entity.ArtifactVersions, error) {
	result := entity.ArtifactVersions{Skills: map[string]string{}, Strategies: map[string]string{}}
	var skills []learningpo.Skill
	if err := s.data.DB(ctx).Where("status = ? AND deleted_at = 0 AND (owner_id = ? OR visibility = ?)", "APPROVED_FOR_USE", ownerID, "PUBLIC").Find(&skills).Error; err != nil {
		return result, log.WrapError(err, "DeploymentStore.ApprovedArtifactVersions.skills")
	}
	for _, skill := range skills {
		result.Skills[skill.SkillID] = skill.LatestVersion
	}
	var strategies []learningpo.Strategy
	if err := s.data.DB(ctx).Where("status = ? AND deleted_at = 0 AND (owner_id = ? OR visibility = ?)", "APPROVED_FOR_USE", ownerID, "PUBLIC").Find(&strategies).Error; err != nil {
		return result, log.WrapError(err, "DeploymentStore.ApprovedArtifactVersions.strategies")
	}
	for _, strategy := range strategies {
		result.Strategies[strategy.StrategyID] = strategy.LatestVersion
	}
	return result, nil
}

func (s *Store) CreateBuild(ctx context.Context, build entity.AgentBuild) error {
	row := po.AgentBuild{BuildID: build.BuildID, OwnerID: build.OwnerID, AgentID: build.AgentID, Version: build.Version, RiskLevel: build.RiskLevel, Checksum: build.Checksum, Content: encode(build), CreatedBy: build.CreatedBy, CreatedAt: millis(build.CreatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DeploymentStore.CreateBuild")
}

func (s *Store) FindBuild(ctx context.Context, ownerID, buildID string) (*entity.AgentBuild, error) {
	var row po.AgentBuild
	result := s.data.DB(ctx).Where("owner_id = ? AND build_id = ?", ownerID, buildID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.FindBuild")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.AgentBuild](row.Content, "DeploymentStore.FindBuild.decode")
}

func (s *Store) FindBuildByChecksum(ctx context.Context, ownerID, agentID, checksum string) (*entity.AgentBuild, error) {
	var row po.AgentBuild
	result := s.data.DB(ctx).Where("owner_id = ? AND agent_id = ? AND checksum = ?", ownerID, agentID, checksum).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.FindBuildByChecksum")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.AgentBuild](row.Content, "DeploymentStore.FindBuildByChecksum.decode")
}

func (s *Store) ListBuilds(ctx context.Context, ownerID string, filter entity.BuildFilter) ([]entity.AgentBuild, int64, error) {
	db := s.data.DB(ctx).Model(&po.AgentBuild{}).Where("owner_id = ?", ownerID)
	if filter.AgentID != "" {
		db = db.Where("agent_id = ?", filter.AgentID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, log.WrapError(err, "DeploymentStore.ListBuilds.count")
	}
	var rows []po.AgentBuild
	if err := db.Order("created_at DESC").Offset(normalizeOffset(filter.Offset)).Limit(normalizeLimit(filter.Limit)).Find(&rows).Error; err != nil {
		return nil, 0, log.WrapError(err, "DeploymentStore.ListBuilds")
	}
	items := make([]entity.AgentBuild, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.AgentBuild](row.Content, "DeploymentStore.ListBuilds.decode")
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, nil
}

func (s *Store) CreatePromotion(ctx context.Context, promotion entity.Promotion) error {
	row := encodePromotion(promotion)
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DeploymentStore.CreatePromotion")
}

func (s *Store) FindPromotion(ctx context.Context, ownerID, promotionID string) (*entity.Promotion, error) {
	var row po.Promotion
	result := s.data.DB(ctx).Where("owner_id = ? AND promotion_id = ?", ownerID, promotionID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.FindPromotion")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodePromotion(row)
}

func (s *Store) FindActivePromotion(ctx context.Context, ownerID, agentID string) (*entity.Promotion, error) {
	return s.findPromotionByStatus(ctx, ownerID, agentID, entity.StatusActive)
}

func (s *Store) FindCanaryPromotion(ctx context.Context, ownerID, agentID string) (*entity.Promotion, error) {
	return s.findPromotionByStatus(ctx, ownerID, agentID, entity.StatusCanary)
}

func (s *Store) findPromotionByStatus(ctx context.Context, ownerID, agentID, status string) (*entity.Promotion, error) {
	var row po.Promotion
	result := s.data.DB(ctx).Where("owner_id = ? AND agent_id = ? AND status = ?", ownerID, agentID, status).Order("updated_at DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.findPromotionByStatus")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodePromotion(row)
}

func (s *Store) FindPromotionForBuild(ctx context.Context, ownerID, agentID, buildID string) (*entity.Promotion, error) {
	var row po.Promotion
	result := s.data.DB(ctx).Where("owner_id = ? AND agent_id = ? AND build_id = ?", ownerID, agentID, buildID).Order("updated_at DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.FindPromotionForBuild")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodePromotion(row)
}

func (s *Store) ListPromotions(ctx context.Context, ownerID string, filter entity.PromotionFilter) ([]entity.Promotion, int64, error) {
	db := s.data.DB(ctx).Model(&po.Promotion{}).Where("owner_id = ?", ownerID)
	if filter.AgentID != "" {
		db = db.Where("agent_id = ?", filter.AgentID)
	}
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, log.WrapError(err, "DeploymentStore.ListPromotions.count")
	}
	var rows []po.Promotion
	if err := db.Order("updated_at DESC").Offset(normalizeOffset(filter.Offset)).Limit(normalizeLimit(filter.Limit)).Find(&rows).Error; err != nil {
		return nil, 0, log.WrapError(err, "DeploymentStore.ListPromotions")
	}
	items := make([]entity.Promotion, 0, len(rows))
	for _, row := range rows {
		item, err := decodePromotion(row)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, nil
}

func (s *Store) UpdatePromotion(ctx context.Context, promotion entity.Promotion, expectedRevision int64) error {
	row := encodePromotion(promotion)
	result := s.data.DB(ctx).Model(&po.Promotion{}).Where("promotion_id = ? AND owner_id = ? AND revision = ?", promotion.PromotionID, promotion.OwnerID, expectedRevision).Updates(map[string]any{
		"status": row.Status, "canary_percent": row.CanaryPercent, "thresholds": row.Thresholds,
		"verified": row.Verified, "recoverable": row.Recoverable, "approved_by": row.ApprovedBy,
		"revision": row.Revision, "updated_at": row.UpdatedAt,
	})
	if result.Error != nil {
		return log.WrapError(result.Error, "DeploymentStore.UpdatePromotion")
	}
	if result.RowsAffected != 1 {
		return log.WrapError(ErrRevisionConflict, "DeploymentStore.UpdatePromotion")
	}
	return nil
}

func (s *Store) ActivatePromotion(ctx context.Context, promotion entity.Promotion, previous *entity.Promotion, expectedRevision int64) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&po.Promotion{}).Where("promotion_id = ? AND owner_id = ? AND revision = ?", promotion.PromotionID, promotion.OwnerID, expectedRevision).Updates(map[string]any{"status": entity.StatusActive, "approved_by": promotion.ApprovedBy, "revision": promotion.Revision, "updated_at": millis(promotion.UpdatedAt)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		if previous == nil || previous.PromotionID == promotion.PromotionID {
			return nil
		}
		previousResult := tx.Model(&po.Promotion{}).Where("promotion_id = ? AND owner_id = ? AND revision = ? AND status = ?", previous.PromotionID, previous.OwnerID, previous.Revision, entity.StatusActive).Updates(map[string]any{"status": entity.StatusPaused, "revision": previous.Revision + 1, "updated_at": millis(promotion.UpdatedAt)})
		if previousResult.Error != nil {
			return previousResult.Error
		}
		if previousResult.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return nil
	}), "DeploymentStore.ActivatePromotion")
}

func (s *Store) FindExposure(ctx context.Context, promotionID, ownerID, agentID string) (*entity.Exposure, error) {
	var row po.Exposure
	result := s.data.DB(ctx).Where("promotion_id = ? AND owner_id = ? AND agent_id = ?", promotionID, ownerID, agentID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.FindExposure")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decodeExposure(row), nil
}

func (s *Store) CreateExposure(ctx context.Context, exposure entity.Exposure) error {
	row := po.Exposure{ExposureID: exposure.ExposureID, PromotionID: exposure.PromotionID, OwnerID: exposure.OwnerID, AgentID: exposure.AgentID, Bucket: exposure.Bucket, Variant: exposure.Variant, OptedOut: exposure.OptedOut, CreatedAt: millis(exposure.CreatedAt), UpdatedAt: millis(exposure.CreatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DeploymentStore.CreateExposure")
}

func (s *Store) SetExposurePreference(ctx context.Context, ownerID, agentID string, optedOut bool, variant string) error {
	result := s.data.DB(ctx).Model(&po.Exposure{}).Where("owner_id = ? AND agent_id = ?", ownerID, agentID).Updates(map[string]any{
		"opted_out":  optedOut,
		"variant":    variant,
		"updated_at": time.Now().UTC().UnixMilli(),
	})
	if result.Error != nil {
		return log.WrapError(result.Error, "DeploymentStore.SetExposurePreference")
	}
	if result.RowsAffected == 0 {
		return log.WrapError(gorm.ErrRecordNotFound, "DeploymentStore.SetExposurePreference")
	}
	return nil
}

func (s *Store) CreateShadowResult(ctx context.Context, shadow entity.ShadowResult) error {
	row := po.ShadowResult{ShadowID: shadow.ShadowID, PromotionID: shadow.PromotionID, OwnerID: shadow.OwnerID, TaskID: shadow.TaskID, Content: encode(shadow), Passed: shadow.Passed, CreatedAt: millis(shadow.CreatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DeploymentStore.CreateShadowResult")
}

func (s *Store) ListShadowResults(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.ShadowResult, error) {
	var rows []po.ShadowResult
	if err := s.data.DB(ctx).Where("owner_id = ? AND promotion_id = ?", ownerID, promotionID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DeploymentStore.ListShadowResults")
	}
	items := make([]entity.ShadowResult, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.ShadowResult](row.Content, "DeploymentStore.ListShadowResults.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) SaveCanaryMetric(ctx context.Context, metric entity.CanaryMetric, promotion *entity.Promotion, expectedRevision int64) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		row := po.CanaryMetric{MetricID: metric.MetricID, PromotionID: metric.PromotionID, OwnerID: metric.OwnerID, Content: encode(metric), StopTriggered: metric.StopTriggered, CreatedAt: millis(metric.CreatedAt)}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if promotion == nil {
			return nil
		}
		result := tx.Model(&po.Promotion{}).Where("promotion_id = ? AND owner_id = ? AND revision = ?", promotion.PromotionID, promotion.OwnerID, expectedRevision).Updates(map[string]any{"status": promotion.Status, "revision": promotion.Revision, "updated_at": millis(promotion.UpdatedAt)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return nil
	}), "DeploymentStore.SaveCanaryMetric")
}

func (s *Store) ListCanaryMetrics(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.CanaryMetric, error) {
	var rows []po.CanaryMetric
	if err := s.data.DB(ctx).Where("owner_id = ? AND promotion_id = ?", ownerID, promotionID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DeploymentStore.ListCanaryMetrics")
	}
	items := make([]entity.CanaryMetric, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.CanaryMetric](row.Content, "DeploymentStore.ListCanaryMetrics.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) CreateRunManifest(ctx context.Context, manifest entity.RunManifest) error {
	row := po.RunManifest{ManifestID: manifest.ManifestID, OwnerID: manifest.OwnerID, AgentID: manifest.AgentID, TaskID: manifest.TaskID, BuildID: manifest.AgentBuildID, ExposureID: manifest.ExposureID, Content: encode(manifest), CreatedAt: millis(manifest.CreatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DeploymentStore.CreateRunManifest")
}

func (s *Store) ListRunManifests(ctx context.Context, ownerID, agentID string, limit int) ([]entity.RunManifest, error) {
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if agentID != "" {
		db = db.Where("agent_id = ?", agentID)
	}
	var rows []po.RunManifest
	if err := db.Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DeploymentStore.ListRunManifests")
	}
	items := make([]entity.RunManifest, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.RunManifest](row.Content, "DeploymentStore.ListRunManifests.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) RollbackPromotion(ctx context.Context, current entity.Promotion, previous *entity.Promotion, rollback entity.Rollback, compensations []entity.Compensation, expectedRevision int64) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var locked po.Promotion
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("promotion_id = ? AND owner_id = ?", current.PromotionID, current.OwnerID).Limit(1).Find(&locked)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || locked.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if err := tx.Model(&po.Promotion{}).Where("promotion_id = ?", current.PromotionID).Updates(map[string]any{"status": entity.StatusRolledBack, "revision": current.Revision, "updated_at": millis(current.UpdatedAt)}).Error; err != nil {
			return err
		}
		if previous != nil {
			previousResult := tx.Model(&po.Promotion{}).Where("promotion_id = ? AND owner_id = ? AND revision = ?", previous.PromotionID, previous.OwnerID, previous.Revision-1).Updates(map[string]any{"status": entity.StatusActive, "revision": previous.Revision, "updated_at": millis(previous.UpdatedAt)})
			if previousResult.Error != nil {
				return previousResult.Error
			}
			if previousResult.RowsAffected != 1 {
				return ErrRevisionConflict
			}
		}
		rollbackRow := po.Rollback{RollbackID: rollback.RollbackID, PromotionID: rollback.PromotionID, OwnerID: rollback.OwnerID, AgentID: rollback.AgentID, FromBuildID: rollback.FromBuildID, ToBuildID: rollback.ToBuildID, Reason: rollback.Reason, RequestedBy: rollback.RequestedBy, CreatedAt: millis(rollback.CreatedAt)}
		if err := tx.Create(&rollbackRow).Error; err != nil {
			return err
		}
		for _, compensation := range compensations {
			row := po.Compensation{CompensationID: compensation.CompensationID, RollbackID: compensation.RollbackID, OwnerID: compensation.OwnerID, ActionID: compensation.ActionID, Status: compensation.Status, Instructions: compensation.Instructions, CreatedAt: millis(compensation.CreatedAt)}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	}), "DeploymentStore.RollbackPromotion")
}

func (s *Store) ListRollbacks(ctx context.Context, ownerID, agentID string, limit int) ([]entity.Rollback, error) {
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if agentID != "" {
		db = db.Where("agent_id = ?", agentID)
	}
	var rows []po.Rollback
	if err := db.Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DeploymentStore.ListRollbacks")
	}
	items := make([]entity.Rollback, 0, len(rows))
	for _, row := range rows {
		items = append(items, entity.Rollback{RollbackID: row.RollbackID, PromotionID: row.PromotionID, OwnerID: row.OwnerID, AgentID: row.AgentID, FromBuildID: row.FromBuildID, ToBuildID: row.ToBuildID, Reason: row.Reason, RequestedBy: row.RequestedBy, CreatedAt: fromMillis(row.CreatedAt)})
	}
	return items, nil
}

func encodePromotion(value entity.Promotion) po.Promotion {
	return po.Promotion{PromotionID: value.PromotionID, OwnerID: value.OwnerID, AgentID: value.AgentID, BuildID: value.BuildID, PreviousBuildID: value.PreviousBuildID, Status: value.Status, RiskLevel: value.RiskLevel, CanaryPercent: value.CanaryPercent, Thresholds: encode(value.Thresholds), Verified: value.Verified, Recoverable: value.Recoverable, ApprovedBy: value.ApprovedBy, Revision: value.Revision, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
}

func decodePromotion(row po.Promotion) (*entity.Promotion, error) {
	thresholds, err := decode[entity.CanaryThresholds](row.Thresholds, "DeploymentStore.decodePromotion.thresholds")
	if err != nil {
		return nil, err
	}
	return &entity.Promotion{Schema: entity.Schema, PromotionID: row.PromotionID, OwnerID: row.OwnerID, AgentID: row.AgentID, BuildID: row.BuildID, PreviousBuildID: row.PreviousBuildID, Status: row.Status, RiskLevel: row.RiskLevel, CanaryPercent: row.CanaryPercent, Thresholds: *thresholds, Verified: row.Verified, Recoverable: row.Recoverable, ApprovedBy: row.ApprovedBy, Revision: row.Revision, CreatedAt: fromMillis(row.CreatedAt), UpdatedAt: fromMillis(row.UpdatedAt)}, nil
}

func decodeExposure(row po.Exposure) *entity.Exposure {
	return &entity.Exposure{ExposureID: row.ExposureID, PromotionID: row.PromotionID, OwnerID: row.OwnerID, AgentID: row.AgentID, Bucket: row.Bucket, Variant: row.Variant, OptedOut: row.OptedOut, CreatedAt: fromMillis(row.CreatedAt)}
}

func encode(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func decode[T any](value, operation string) (*T, error) {
	var result T
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, log.WrapError(err, operation)
	}
	return &result, nil
}

func millis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func normalizeLimit(value int) int {
	if value <= 0 {
		return defaultLimit
	}
	if value > maximumLimit {
		return maximumLimit
	}
	return value
}

func normalizeOffset(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func IsRevisionConflict(err error) bool { return errors.Is(err, ErrRevisionConflict) }

func missing(name string) error { return fmt.Errorf("%s not found", name) }
