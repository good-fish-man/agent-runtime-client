package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/knowledge"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge"
	log "github.com/good-fish-man/logx"
)

var ErrRevisionConflict = errors.New("ontology candidate revision conflict")
var ErrOntologyVersionConflict = errors.New("ontology current version conflict")

type Store struct{ data *data.Data }

func NewStore(value *data.Data) *Store { return &Store{data: value} }

var _ repository.Store = (*Store)(nil)

func (s *Store) CreateEvidence(ctx context.Context, evidence []entity.Evidence) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range evidence {
			row, err := evidenceRow(item)
			if err != nil {
				return err
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	}), "KnowledgeStore.CreateEvidence")
}

func (s *Store) CreateKnowledge(ctx context.Context, claim entity.Claim, contradictions []entity.Contradiction, conflictingClaimIDs []string) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		claimRow, err := claimRow(claim)
		if err != nil {
			return err
		}
		if err := tx.Create(&claimRow).Error; err != nil {
			return err
		}
		for _, id := range conflictingClaimIDs {
			var row po.Claim
			result := tx.Where("owner_id = ? AND claim_id = ?", claim.OwnerID, id).Limit(1).Find(&row)
			if result.Error != nil || result.RowsAffected == 0 {
				continue
			}
			value, err := decode[entity.Claim](row.Content, "KnowledgeStore.CreateKnowledge.conflict.decode")
			if err != nil {
				return err
			}
			value.Status = "CONTRADICTED"
			value.ContradictedBy = appendUnique(value.ContradictedBy, claim.ClaimID)
			value.UpdatedAt = claim.UpdatedAt
			content, err := encode(*value)
			if err != nil {
				return err
			}
			if err := tx.Model(&po.Claim{}).Where("claim_id = ? AND owner_id = ?", id, claim.OwnerID).Updates(map[string]any{"status": value.Status, "content": content, "updated_at": millis(value.UpdatedAt)}).Error; err != nil {
				return err
			}
		}
		for _, item := range contradictions {
			content, err := encode(item)
			if err != nil {
				return err
			}
			row := po.Contradiction{ContradictionID: item.ContradictionID, OwnerID: item.OwnerID, Resolved: item.Resolved, Content: content, CreatedAt: millis(item.CreatedAt), UpdatedAt: millis(item.UpdatedAt)}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	}), "KnowledgeStore.CreateKnowledge")
}

func (s *Store) FindClaim(ctx context.Context, ownerID, claimID string) (*entity.Claim, error) {
	var row po.Claim
	result := s.data.DB(ctx).Where("owner_id = ? AND claim_id = ?", ownerID, claimID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "KnowledgeStore.FindClaim")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.Claim](row.Content, "KnowledgeStore.FindClaim.decode")
}

func (s *Store) FindClaims(ctx context.Context, ownerID string, claimIDs []string) ([]entity.Claim, error) {
	if len(claimIDs) == 0 {
		return []entity.Claim{}, nil
	}
	var rows []po.Claim
	if err := s.data.DB(ctx).Where("owner_id = ? AND claim_id IN ?", ownerID, claimIDs).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.FindClaims")
	}
	return decodeRows[entity.Claim](rows, func(row po.Claim) string { return row.Content }, "KnowledgeStore.FindClaims.decode")
}

func (s *Store) ListClaims(ctx context.Context, ownerID string, filter entity.ClaimFilter) ([]entity.Claim, error) {
	db := s.data.DB(ctx).Model(&po.Claim{}).Where("owner_id = ?", ownerID)
	if filter.Subject != "" {
		db = db.Where("subject = ?", filter.Subject)
	}
	if filter.Predicate != "" {
		db = db.Where("predicate = ?", filter.Predicate)
	}
	if len(filter.Scopes) > 0 {
		db = db.Where("scope IN ?", filter.Scopes)
	}
	if len(filter.Sensitivities) > 0 {
		db = db.Where("sensitivity IN ?", filter.Sensitivities)
	}
	if len(filter.Statuses) > 0 {
		db = db.Where("status IN ?", filter.Statuses)
	}
	var rows []po.Claim
	if err := db.Order("updated_at DESC").Limit(normalizeLimit(filter.Limit, 500)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.ListClaims")
	}
	items := make([]entity.Claim, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.Claim](row.Content, "KnowledgeStore.ListClaims.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) SearchClaims(ctx context.Context, userID string, organizationIDs []string, filter entity.ClaimFilter) ([]entity.Claim, error) {
	parts := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if containsString(filter.Scopes, "USER") {
		parts = append(parts, "(scope = ? AND owner_id = ?)")
		args = append(args, "USER", userID)
	}
	if containsString(filter.Scopes, "PUBLIC") {
		parts = append(parts, "scope = ?")
		args = append(args, "PUBLIC")
	}
	if containsString(filter.Scopes, "ORGANIZATION") && len(organizationIDs) > 0 {
		parts = append(parts, "(scope = ? AND organization_id IN ?)")
		args = append(args, "ORGANIZATION", organizationIDs)
	}
	if len(parts) == 0 {
		return []entity.Claim{}, nil
	}
	db := s.data.DB(ctx).Model(&po.Claim{}).Where("("+strings.Join(parts, " OR ")+")", args...)
	if filter.Subject != "" {
		db = db.Where("subject = ?", filter.Subject)
	}
	if filter.Predicate != "" {
		db = db.Where("predicate = ?", filter.Predicate)
	}
	if len(filter.Sensitivities) > 0 {
		db = db.Where("sensitivity IN ?", filter.Sensitivities)
	}
	if len(filter.Statuses) > 0 {
		db = db.Where("status IN ?", filter.Statuses)
	}
	var rows []po.Claim
	if err := db.Order("updated_at DESC").Limit(normalizeLimit(filter.Limit, 500)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.SearchClaims")
	}
	return decodeRows[entity.Claim](rows, func(row po.Claim) string { return row.Content }, "KnowledgeStore.SearchClaims.decode")
}

func (s *Store) FindEvidence(ctx context.Context, ownerID string, evidenceIDs []string) ([]entity.Evidence, error) {
	if len(evidenceIDs) == 0 {
		return []entity.Evidence{}, nil
	}
	var rows []po.Evidence
	if err := s.data.DB(ctx).Where("owner_id = ? AND evidence_id IN ?", ownerID, evidenceIDs).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.FindEvidence")
	}
	items := make([]entity.Evidence, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.Evidence](row.Content, "KnowledgeStore.FindEvidence.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) ListEvidence(ctx context.Context, ownerID string, filter entity.EvidenceFilter) ([]entity.Evidence, error) {
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if len(filter.SourceTypes) > 0 {
		db = db.Where("source_type IN ?", filter.SourceTypes)
	}
	if len(filter.Scopes) > 0 {
		db = db.Where("scope IN ?", filter.Scopes)
	}
	var rows []po.Evidence
	if err := db.Order("created_at DESC").Limit(normalizeLimit(filter.Limit, 500)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.ListEvidence")
	}
	items := make([]entity.Evidence, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.Evidence](row.Content, "KnowledgeStore.ListEvidence.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) ListContradictions(ctx context.Context, ownerID string, unresolvedOnly bool, limit int) ([]entity.Contradiction, error) {
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if unresolvedOnly {
		db = db.Where("resolved = ?", false)
	}
	var rows []po.Contradiction
	if err := db.Order("created_at DESC").Limit(normalizeLimit(limit, 200)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.ListContradictions")
	}
	items := make([]entity.Contradiction, 0, len(rows))
	for _, row := range rows {
		item, err := decode[entity.Contradiction](row.Content, "KnowledgeStore.ListContradictions.decode")
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Store) FindContradiction(ctx context.Context, ownerID, contradictionID string) (*entity.Contradiction, error) {
	var row po.Contradiction
	result := s.data.DB(ctx).Where("owner_id = ? AND contradiction_id = ?", ownerID, contradictionID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "KnowledgeStore.FindContradiction")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.Contradiction](row.Content, "KnowledgeStore.FindContradiction.decode")
}

func (s *Store) ResolveContradiction(ctx context.Context, value entity.Contradiction, claimStatuses map[string]string) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&po.Contradiction{}).Where("contradiction_id = ? AND owner_id = ? AND resolved = ?", value.ContradictionID, value.OwnerID, false).Updates(map[string]any{"resolved": true, "content": content, "updated_at": millis(value.UpdatedAt)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		for claimID, status := range claimStatuses {
			var row po.Claim
			found := tx.Where("claim_id = ? AND owner_id = ?", claimID, value.OwnerID).Limit(1).Find(&row)
			if found.Error != nil || found.RowsAffected != 1 {
				return fmt.Errorf("resolve contradiction claim %q not found", claimID)
			}
			claim, decodeErr := decode[entity.Claim](row.Content, "KnowledgeStore.ResolveContradiction.claim.decode")
			if decodeErr != nil {
				return decodeErr
			}
			claim.Status = status
			claim.UpdatedAt = value.UpdatedAt
			claimContent, encodeErr := encode(*claim)
			if encodeErr != nil {
				return encodeErr
			}
			if updateErr := tx.Model(&po.Claim{}).Where("claim_id = ? AND owner_id = ?", claimID, value.OwnerID).Updates(map[string]any{"status": status, "content": claimContent, "updated_at": millis(claim.UpdatedAt)}).Error; updateErr != nil {
				return updateErr
			}
		}
		return nil
	}), "KnowledgeStore.ResolveContradiction")
}

func (s *Store) CreateSnapshot(ctx context.Context, value entity.Snapshot) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	row := po.Snapshot{SnapshotID: value.SnapshotID, OwnerID: value.OwnerID, Checksum: value.Checksum, Content: content, CreatedAt: millis(value.CreatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "KnowledgeStore.CreateSnapshot")
}

func (s *Store) BindSnapshotToRun(ctx context.Context, ownerID, snapshotID, runManifestID string, boundAtMillis int64) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var row po.Snapshot
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("snapshot_id = ? AND owner_id = ?", snapshotID, ownerID).Limit(1).Find(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		value, decodeErr := decode[entity.Snapshot](row.Content, "KnowledgeStore.BindSnapshotToRun.decode")
		if decodeErr != nil {
			return decodeErr
		}
		if value.RunManifestID != "" && value.RunManifestID != runManifestID {
			return fmt.Errorf("knowledge snapshot is already bound to another run manifest")
		}
		boundAt := time.UnixMilli(boundAtMillis).UTC()
		value.RunManifestID = runManifestID
		value.BoundAt = &boundAt
		content, encodeErr := encode(*value)
		if encodeErr != nil {
			return encodeErr
		}
		return tx.Model(&po.Snapshot{}).Where("snapshot_id = ? AND owner_id = ?", snapshotID, ownerID).Updates(map[string]any{"run_manifest_id": runManifestID, "content": content}).Error
	}), "KnowledgeStore.BindSnapshotToRun")
}

func (s *Store) ListSnapshots(ctx context.Context, ownerID string, limit int) ([]entity.Snapshot, error) {
	var rows []po.Snapshot
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(normalizeLimit(limit, 100)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.ListSnapshots")
	}
	return decodeRows[entity.Snapshot](rows, func(row po.Snapshot) string { return row.Content }, "KnowledgeStore.ListSnapshots.decode")
}

func (s *Store) CreateOntologyPack(ctx context.Context, value entity.OntologyPack) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Create(&po.OntologyPack{PackID: value.PackID, OwnerID: value.OwnerID, Name: value.Name, Domain: value.Domain, Current: value.Current, Content: content, CreatedAt: millis(value.CreatedAt)}).Error, "KnowledgeStore.CreateOntologyPack")
}

func (s *Store) CreateOntologyPackWithVersion(ctx context.Context, pack entity.OntologyPack, version entity.OntologyVersion) error {
	packContent, err := encode(pack)
	if err != nil {
		return err
	}
	versionContent, err := encode(version)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		packRow := po.OntologyPack{PackID: pack.PackID, OwnerID: pack.OwnerID, Name: pack.Name, Domain: pack.Domain, Current: pack.Current, Content: packContent, CreatedAt: millis(pack.CreatedAt)}
		if err := tx.Create(&packRow).Error; err != nil {
			return err
		}
		versionRow := po.OntologyVersion{VersionID: version.VersionID, PackID: version.PackID, OwnerID: version.OwnerID, Version: version.Version, Status: version.Status, Checksum: version.Checksum, Content: versionContent, CreatedAt: millis(version.CreatedAt)}
		return tx.Create(&versionRow).Error
	}), "KnowledgeStore.CreateOntologyPackWithVersion")
}

func (s *Store) FindOntologyPack(ctx context.Context, ownerID, packID string) (*entity.OntologyPack, error) {
	var row po.OntologyPack
	result := s.data.DB(ctx).Where("owner_id = ? AND pack_id = ?", ownerID, packID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "KnowledgeStore.FindOntologyPack")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.OntologyPack](row.Content, "KnowledgeStore.FindOntologyPack.decode")
}

func (s *Store) FindOntologyVersion(ctx context.Context, ownerID, packID, version string) (*entity.OntologyVersion, error) {
	var row po.OntologyVersion
	result := s.data.DB(ctx).Where("owner_id = ? AND pack_id = ? AND version = ?", ownerID, packID, version).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "KnowledgeStore.FindOntologyVersion")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.OntologyVersion](row.Content, "KnowledgeStore.FindOntologyVersion.decode")
}

func (s *Store) ListOntologyPacks(ctx context.Context, ownerID string) ([]entity.OntologyPack, error) {
	var rows []po.OntologyPack
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.ListOntologyPacks")
	}
	return decodeRows[entity.OntologyPack](rows, func(row po.OntologyPack) string { return row.Content }, "KnowledgeStore.ListOntologyPacks.decode")
}

func (s *Store) CreateOntologyCandidate(ctx context.Context, value entity.OntologyCandidate) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	row := po.OntologyCandidate{CandidateID: value.CandidateID, OwnerID: value.OwnerID, PackID: value.PackID, Status: value.Status, Revision: value.Revision, Content: content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "KnowledgeStore.CreateOntologyCandidate")
}

func (s *Store) ListOntologyCandidates(ctx context.Context, ownerID string, limit int) ([]entity.OntologyCandidate, error) {
	var rows []po.OntologyCandidate
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(normalizeLimit(limit, 200)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "KnowledgeStore.ListOntologyCandidates")
	}
	return decodeRows[entity.OntologyCandidate](rows, func(row po.OntologyCandidate) string { return row.Content }, "KnowledgeStore.ListOntologyCandidates.decode")
}

func (s *Store) FindOntologyCandidate(ctx context.Context, ownerID, candidateID string) (*entity.OntologyCandidate, error) {
	var row po.OntologyCandidate
	result := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "KnowledgeStore.FindOntologyCandidate")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.OntologyCandidate](row.Content, "KnowledgeStore.FindOntologyCandidate.decode")
}

func (s *Store) ReviewOntologyCandidate(ctx context.Context, value entity.OntologyCandidate, expectedRevision int64, version *entity.OntologyVersion) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&po.OntologyCandidate{}).Where("candidate_id = ? AND owner_id = ? AND revision = ?", value.CandidateID, value.OwnerID, expectedRevision).Updates(map[string]any{"status": value.Status, "revision": value.Revision, "content": content, "updated_at": millis(value.UpdatedAt)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		if version == nil {
			return nil
		}
		versionContent, err := encode(*version)
		if err != nil {
			return err
		}
		row := po.OntologyVersion{VersionID: version.VersionID, PackID: version.PackID, OwnerID: version.OwnerID, Version: version.Version, Status: version.Status, Checksum: version.Checksum, Content: versionContent, CreatedAt: millis(version.CreatedAt)}
		return tx.Create(&row).Error
	}), "KnowledgeStore.ReviewOntologyCandidate")
}

func (s *Store) CreateOntologyMigration(ctx context.Context, value entity.OntologyMigration) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	row := po.OntologyMigration{MigrationID: value.MigrationID, OwnerID: value.OwnerID, PackID: value.PackID, Status: value.Status, Revision: value.Revision, Content: content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "KnowledgeStore.CreateOntologyMigration")
}

func (s *Store) FindOntologyMigration(ctx context.Context, ownerID, migrationID string) (*entity.OntologyMigration, error) {
	var row po.OntologyMigration
	result := s.data.DB(ctx).Where("owner_id = ? AND migration_id = ?", ownerID, migrationID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "KnowledgeStore.FindOntologyMigration")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.OntologyMigration](row.Content, "KnowledgeStore.FindOntologyMigration.decode")
}

func (s *Store) ReviewOntologyMigration(ctx context.Context, value entity.OntologyMigration, expectedStatus string) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	result := s.data.DB(ctx).Model(&po.OntologyMigration{}).Where("migration_id = ? AND owner_id = ? AND status = ? AND revision = ?", value.MigrationID, value.OwnerID, expectedStatus, value.Revision-1).Updates(map[string]any{"status": value.Status, "revision": value.Revision, "content": content, "updated_at": millis(value.UpdatedAt)})
	if result.Error != nil {
		return log.WrapError(result.Error, "KnowledgeStore.ReviewOntologyMigration")
	}
	if result.RowsAffected != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (s *Store) ApplyOntologyMigration(ctx context.Context, value entity.OntologyMigration) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var packRow po.OntologyPack
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("pack_id = ? AND owner_id = ?", value.PackID, value.OwnerID).Limit(1).Find(&packRow)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || packRow.Current != value.FromVersion {
			return ErrOntologyVersionConflict
		}
		pack, err := decode[entity.OntologyPack](packRow.Content, "KnowledgeStore.ApplyOntologyMigration.pack.decode")
		if err != nil {
			return err
		}
		pack.Current = value.ToVersion
		packContent, err := encode(*pack)
		if err != nil {
			return err
		}
		packUpdate := tx.Model(&po.OntologyPack{}).Where("pack_id = ? AND owner_id = ? AND current_version = ?", value.PackID, value.OwnerID, value.FromVersion).Updates(map[string]any{"current_version": value.ToVersion, "content": packContent})
		if packUpdate.Error != nil {
			return packUpdate.Error
		}
		if packUpdate.RowsAffected != 1 {
			return ErrOntologyVersionConflict
		}
		var versionRow po.OntologyVersion
		versionResult := tx.Where("pack_id = ? AND owner_id = ? AND version = ?", value.PackID, value.OwnerID, value.ToVersion).Limit(1).Find(&versionRow)
		if versionResult.Error != nil {
			return versionResult.Error
		}
		if versionResult.RowsAffected != 1 {
			return ErrOntologyVersionConflict
		}
		version, decodeErr := decode[entity.OntologyVersion](versionRow.Content, "KnowledgeStore.ApplyOntologyMigration.version.decode")
		if decodeErr != nil {
			return decodeErr
		}
		version.Status = "APPLIED"
		versionContent, encodeErr := encode(*version)
		if encodeErr != nil {
			return encodeErr
		}
		versionUpdate := tx.Model(&po.OntologyVersion{}).Where("version_id = ? AND owner_id = ? AND status = ?", version.VersionID, value.OwnerID, "APPROVED").Updates(map[string]any{"status": version.Status, "content": versionContent})
		if versionUpdate.Error != nil {
			return versionUpdate.Error
		}
		if versionUpdate.RowsAffected != 1 {
			return ErrOntologyVersionConflict
		}
		migrationResult := tx.Model(&po.OntologyMigration{}).Where("migration_id = ? AND owner_id = ? AND status = ? AND revision = ?", value.MigrationID, value.OwnerID, "APPROVED", value.Revision-1).Updates(map[string]any{"status": value.Status, "revision": value.Revision, "content": content, "updated_at": millis(value.UpdatedAt)})
		if migrationResult.Error != nil {
			return migrationResult.Error
		}
		if migrationResult.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return nil
	}), "KnowledgeStore.ApplyOntologyMigration")
}

func claimRow(value entity.Claim) (po.Claim, error) {
	content, err := encode(value)
	if err != nil {
		return po.Claim{}, err
	}
	validUntil := int64(0)
	if value.ValidUntil != nil {
		validUntil = millis(*value.ValidUntil)
	}
	return po.Claim{ClaimID: value.ClaimID, OwnerID: value.OwnerID, OrganizationID: value.OrganizationID, Subject: value.Subject, Predicate: value.Predicate, Scope: value.Scope, Sensitivity: value.Sensitivity, Status: value.Status, ValidUntil: validUntil, SearchText: value.Subject + " " + value.Predicate + " " + value.Value, Content: content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}, nil
}

func evidenceRow(value entity.Evidence) (po.Evidence, error) {
	content, err := encode(value)
	if err != nil {
		return po.Evidence{}, err
	}
	staleAt := int64(0)
	if value.StaleAt != nil {
		staleAt = millis(*value.StaleAt)
	}
	return po.Evidence{EvidenceID: value.EvidenceID, OwnerID: value.OwnerID, OrganizationID: value.OrganizationID, Scope: value.Scope, Sensitivity: value.Sensitivity, SourceType: value.SourceType, URI: value.URI, Accessible: value.Accessible, Authority: value.Authority, Freshness: value.Freshness, StaleAt: staleAt, Content: content, CreatedAt: millis(value.ObservedAt)}, nil
}

func encode(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", log.WrapError(err, "KnowledgeStore.encode")
	}
	return string(body), nil
}

func decode[T any](body, operation string) (*T, error) {
	var value T
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		return nil, log.WrapError(err, operation)
	}
	return &value, nil
}

func decodeRows[T any, R any](rows []R, content func(R) string, operation string) ([]T, error) {
	items := make([]T, 0, len(rows))
	for _, row := range rows {
		item, err := decode[T](content(row), operation)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func normalizeLimit(value, maximum int) int {
	if value < 1 {
		return 50
	}
	if value > maximum {
		return maximum
	}
	return value
}

func millis(value time.Time) int64 { return value.UTC().UnixMilli() }

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
