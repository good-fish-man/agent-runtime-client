package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/deployment"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	runtimeartifact "github.com/good-fish-man/athena-protocol/draft/runtimeartifact"
	deploymentv1 "github.com/good-fish-man/athena-protocol/protocol/deployment/v1"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
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

func (s *Store) ResolveApprovedArtifacts(ctx context.Context, ownerID, organizationID string, skills, strategies map[string]string) (entity.ArtifactApprovals, error) {
	result := entity.ArtifactApprovals{References: make([]entity.ArtifactApprovalReference, 0, len(skills)+len(strategies))}
	for id, version := range skills {
		reference, err := s.resolveSkillApproval(ctx, ownerID, organizationID, id, version)
		if err != nil {
			return result, err
		}
		result.References = append(result.References, *reference)
	}
	for id, version := range strategies {
		reference, err := s.resolveStrategyApproval(ctx, ownerID, organizationID, id, version)
		if err != nil {
			return result, err
		}
		result.References = append(result.References, *reference)
	}
	return result, nil
}

// LoadApprovedRuntimeArtifacts resolves the exact immutable definitions pinned
// by a build. Every reference is rechecked against its evaluation and human
// review at run time; a mutable latest version is never consulted.
func (s *Store) LoadApprovedRuntimeArtifacts(ctx context.Context, ownerID, organizationID string, build entity.AgentBuild) ([]runtimeartifact.SkillArtifact, []runtimeartifact.StrategyArtifact, error) {
	approvalIndex := make(map[string]entity.ArtifactApprovalReference, len(build.ArtifactApprovals))
	for _, approval := range build.ArtifactApprovals {
		approvalIndex[approval.Kind+":"+approval.ArtifactID] = approval
	}
	skillIDs := sortedArtifactIDs(build.SkillVersions)
	strategyIDs := sortedArtifactIDs(build.StrategyVersions)
	skills := make([]runtimeartifact.SkillArtifact, 0, len(skillIDs))
	strategies := make([]runtimeartifact.StrategyArtifact, 0, len(strategyIDs))
	for _, id := range skillIDs {
		version := build.SkillVersions[id]
		approved, err := s.resolveSkillApproval(ctx, ownerID, organizationID, id, version)
		if err != nil {
			return nil, nil, err
		}
		pinned, ok := approvalIndex[runtimeartifact.KindSkill+":"+id]
		if !ok || !sameArtifactApproval(pinned, *approved) {
			return nil, nil, fmt.Errorf("build %s does not pin the current approval for skill %s@%s", build.BuildID, id, version)
		}
		var immutable learningpo.SkillVersion
		result := s.data.DB(ctx).Where("version_id = ? AND skill_id = ? AND version = ?", pinned.VersionID, id, version).Limit(1).Find(&immutable)
		if result.Error != nil {
			return nil, nil, log.WrapError(result.Error, "DeploymentStore.LoadApprovedRuntimeArtifacts.skill")
		}
		if result.RowsAffected == 0 || rawArtifactChecksum(immutable.Definition) != pinned.Checksum {
			return nil, nil, fmt.Errorf("immutable skill %s@%s is missing or corrupted", id, version)
		}
		var definition learningv2.SkillDefinition
		if err := json.Unmarshal([]byte(immutable.Definition), &definition); err != nil {
			return nil, nil, log.WrapError(err, "DeploymentStore.LoadApprovedRuntimeArtifacts.skillDefinition")
		}
		skills = append(skills, runtimeartifact.SkillArtifact{Reference: runtimeReference(pinned), Definition: definition})
	}
	for _, id := range strategyIDs {
		version := build.StrategyVersions[id]
		approved, err := s.resolveStrategyApproval(ctx, ownerID, organizationID, id, version)
		if err != nil {
			return nil, nil, err
		}
		pinned, ok := approvalIndex[runtimeartifact.KindStrategy+":"+id]
		if !ok || !sameArtifactApproval(pinned, *approved) {
			return nil, nil, fmt.Errorf("build %s does not pin the current approval for strategy %s@%s", build.BuildID, id, version)
		}
		var immutable learningpo.StrategyVersion
		result := s.data.DB(ctx).Where("version_id = ? AND strategy_id = ? AND version = ?", pinned.VersionID, id, version).Limit(1).Find(&immutable)
		if result.Error != nil {
			return nil, nil, log.WrapError(result.Error, "DeploymentStore.LoadApprovedRuntimeArtifacts.strategy")
		}
		if result.RowsAffected == 0 || rawArtifactChecksum(immutable.Definition) != pinned.Checksum {
			return nil, nil, fmt.Errorf("immutable strategy %s@%s is missing or corrupted", id, version)
		}
		var definition learningv2.StrategyDefinition
		if err := json.Unmarshal([]byte(immutable.Definition), &definition); err != nil {
			return nil, nil, log.WrapError(err, "DeploymentStore.LoadApprovedRuntimeArtifacts.strategyDefinition")
		}
		strategies = append(strategies, runtimeartifact.StrategyArtifact{Reference: runtimeReference(pinned), Definition: definition})
	}
	return skills, strategies, nil
}

func sortedArtifactIDs(values map[string]string) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sameArtifactApproval(left, right entity.ArtifactApprovalReference) bool {
	return left.Verified && right.Verified && left.Kind == right.Kind && left.ArtifactID == right.ArtifactID && left.Version == right.Version &&
		left.VersionID == right.VersionID && left.CandidateID == right.CandidateID && left.EvaluationRunID == right.EvaluationRunID &&
		left.ReviewedBy == right.ReviewedBy && left.ReviewedAt.Equal(right.ReviewedAt) && left.Checksum == right.Checksum
}

func runtimeReference(value entity.ArtifactApprovalReference) runtimeartifact.Reference {
	return runtimeartifact.Reference{Kind: value.Kind, ArtifactID: value.ArtifactID, Version: value.Version, VersionID: value.VersionID, CandidateID: value.CandidateID, Checksum: value.Checksum}
}

func rawArtifactChecksum(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}

func (s *Store) resolveSkillApproval(ctx context.Context, ownerID, organizationID, id, version string) (*entity.ArtifactApprovalReference, error) {
	var artifact learningpo.Skill
	result := approvedArtifactScope(s.data.DB(ctx), ownerID, organizationID).
		Where("skill_id = ? AND status = ? AND deleted_at = 0", id, learningv2.LifecycleApproved).
		Limit(1).Find(&artifact)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.resolveSkillApproval.artifact")
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("approved skill %s@%s is unavailable", id, version)
	}
	var immutable learningpo.SkillVersion
	result = s.data.DB(ctx).Where("skill_id = ? AND version = ?", id, version).Limit(1).Find(&immutable)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.resolveSkillApproval.version")
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("approved skill %s@%s has no immutable version", id, version)
	}
	if immutable.OwnerID != artifact.OwnerID {
		return nil, fmt.Errorf("approved skill %s@%s crosses owner boundaries", id, version)
	}
	return s.resolveCandidateApproval(ctx, "SKILL", id, version, immutable.VersionID, immutable.CandidateID, immutable.OwnerID, immutable.Checksum)
}

func (s *Store) resolveStrategyApproval(ctx context.Context, ownerID, organizationID, id, version string) (*entity.ArtifactApprovalReference, error) {
	var artifact learningpo.Strategy
	result := approvedArtifactScope(s.data.DB(ctx), ownerID, organizationID).
		Where("strategy_id = ? AND status = ? AND deleted_at = 0", id, learningv2.LifecycleApproved).
		Limit(1).Find(&artifact)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.resolveStrategyApproval.artifact")
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("approved strategy %s@%s is unavailable", id, version)
	}
	var immutable learningpo.StrategyVersion
	result = s.data.DB(ctx).Where("strategy_id = ? AND version = ?", id, version).Limit(1).Find(&immutable)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.resolveStrategyApproval.version")
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("approved strategy %s@%s has no immutable version", id, version)
	}
	if immutable.OwnerID != artifact.OwnerID {
		return nil, fmt.Errorf("approved strategy %s@%s crosses owner boundaries", id, version)
	}
	return s.resolveCandidateApproval(ctx, "STRATEGY", id, version, immutable.VersionID, immutable.CandidateID, immutable.OwnerID, immutable.Checksum)
}

func approvedArtifactScope(db *gorm.DB, ownerID, organizationID string) *gorm.DB {
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return db.Where("(owner_id = ? OR visibility = ?)", ownerID, learningv2.VisibilityPublic)
	}
	return db.Where(
		"(owner_id = ? OR visibility = ? OR (visibility = ? AND organization_id = ?))",
		ownerID, learningv2.VisibilityPublic, learningv2.VisibilityTeam, organizationID,
	)
}

func (s *Store) resolveCandidateApproval(ctx context.Context, kind, id, version, versionID, candidateID, expectedOwnerID, checksum string) (*entity.ArtifactApprovalReference, error) {
	var candidate learningpo.Candidate
	result := s.data.DB(ctx).Where("candidate_id = ? AND status = ? AND deleted_at = 0", candidateID, learningv2.LifecycleApproved).Limit(1).Find(&candidate)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.resolveCandidateApproval")
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%s %s@%s is not backed by an approved candidate", strings.ToLower(kind), id, version)
	}
	if candidate.OwnerID != expectedOwnerID || candidate.Kind != kind {
		return nil, fmt.Errorf("%s %s@%s has mismatched candidate ownership or kind", strings.ToLower(kind), id, version)
	}
	var evaluation learningv2.EvaluationSummary
	if err := json.Unmarshal([]byte(candidate.Evaluation), &evaluation); err != nil {
		return nil, log.WrapError(err, "DeploymentStore.resolveCandidateApproval.evaluation")
	}
	if !evaluation.Passed || evaluation.RunID == "" || candidate.ReviewedBy == "" || candidate.ReviewedAt == 0 {
		return nil, fmt.Errorf("%s %s@%s is missing evaluation or human review provenance", strings.ToLower(kind), id, version)
	}
	return &entity.ArtifactApprovalReference{
		Kind: kind, ArtifactID: id, Version: version, VersionID: versionID, CandidateID: candidateID,
		EvaluationRunID: evaluation.RunID, ReviewedBy: candidate.ReviewedBy, ReviewedAt: fromMillis(candidate.ReviewedAt),
		Checksum: checksum, Verified: true,
	}, nil
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

func (s *Store) FindLatestExposurePreference(ctx context.Context, ownerID, agentID string) (bool, bool, error) {
	var row po.Exposure
	result := s.data.DB(ctx).Where("owner_id = ? AND agent_id = ?", ownerID, agentID).
		Order("updated_at DESC, created_at DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return false, false, log.WrapError(result.Error, "DeploymentStore.FindLatestExposurePreference")
	}
	if result.RowsAffected == 0 {
		return false, false, nil
	}
	return row.OptedOut, true, nil
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

func (s *Store) AppendCanarySample(ctx context.Context, sample entity.CanarySample, promotion entity.Promotion) (*entity.CanaryMetric, *entity.Promotion, error) {
	var metric *entity.CanaryMetric
	var updated *entity.Promotion
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var locked po.Promotion
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("promotion_id = ? AND owner_id = ?", promotion.PromotionID, promotion.OwnerID).Limit(1).Find(&locked)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || locked.Revision != promotion.Revision || locked.Status != entity.StatusCanary {
			return ErrRevisionConflict
		}
		row := po.CanarySample{
			SampleID: sample.SampleID, PromotionID: sample.PromotionID, OwnerID: sample.OwnerID,
			ManifestID: sample.ManifestID, ExposureID: sample.ExposureID, BuildID: sample.AgentBuildID,
			Content: encode(sample), CreatedAt: millis(sample.CreatedAt),
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var rows []po.CanarySample
		if err := tx.Where("owner_id = ? AND promotion_id = ?", promotion.OwnerID, promotion.PromotionID).Order("created_at ASC, sample_id ASC").Find(&rows).Error; err != nil {
			return err
		}
		samples := make([]entity.CanarySample, 0, len(rows))
		for _, stored := range rows {
			value, err := decode[entity.CanarySample](stored.Content, "DeploymentStore.AppendCanarySample.decode")
			if err != nil {
				return err
			}
			samples = append(samples, *value)
		}
		aggregated, err := deploymentv1.AggregateCanary(promotion.PromotionID, promotion.OwnerID, ulid.New(), samples)
		if err != nil {
			return err
		}
		stop, reason := promotion.Thresholds.Evaluate(aggregated)
		aggregated.StopTriggered, aggregated.StopReason = stop, reason
		metricRow := po.CanaryMetric{MetricID: aggregated.MetricID, PromotionID: aggregated.PromotionID, OwnerID: aggregated.OwnerID, Content: encode(aggregated), StopTriggered: aggregated.StopTriggered, CreatedAt: millis(aggregated.CreatedAt)}
		if err := tx.Create(&metricRow).Error; err != nil {
			return err
		}
		metric = &aggregated
		if !stop {
			return nil
		}
		promotion.Status = entity.StatusPaused
		promotion.Revision++
		promotion.UpdatedAt = sample.CreatedAt
		update := tx.Model(&po.Promotion{}).Where("promotion_id = ? AND owner_id = ? AND revision = ?", promotion.PromotionID, promotion.OwnerID, locked.Revision).Updates(map[string]any{
			"status": promotion.Status, "revision": promotion.Revision, "updated_at": millis(promotion.UpdatedAt),
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		copy := promotion
		updated = &copy
		return nil
	})
	if err != nil {
		return nil, nil, log.WrapError(err, "DeploymentStore.AppendCanarySample")
	}
	return metric, updated, nil
}

func (s *Store) ListCanarySamples(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.CanarySample, error) {
	var rows []po.CanarySample
	if err := s.data.DB(ctx).Where("owner_id = ? AND promotion_id = ?", ownerID, promotionID).Order("created_at DESC").Limit(normalizeLimit(limit)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DeploymentStore.ListCanarySamples")
	}
	items := make([]entity.CanarySample, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.CanarySample](row.Content, "DeploymentStore.ListCanarySamples.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
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

func (s *Store) FindRunManifest(ctx context.Context, ownerID, manifestID string) (*entity.RunManifest, error) {
	var row po.RunManifest
	result := s.data.DB(ctx).Where("owner_id = ? AND manifest_id = ?", ownerID, manifestID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DeploymentStore.FindRunManifest")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.RunManifest](row.Content, "DeploymentStore.FindRunManifest.decode")
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
