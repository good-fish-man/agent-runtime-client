package delegation

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	log "github.com/good-fish-man/logx"
)

var _ repository.AdHocStore = (*Store)(nil)

func (s *Store) CreateAdHocAdmission(ctx context.Context, value entity.AdHocAdmissionBundle) error {
	if strings.TrimSpace(value.Overlay.OverlayID) == "" || strings.TrimSpace(value.Overlay.OwnerID) == "" || strings.TrimSpace(value.Overlay.ContentHash) == "" || strings.TrimSpace(value.Overlay.Content) == "" || value.Overlay.CreatedAt.IsZero() || value.Overlay.ExpiresAt.IsZero() || value.Admission.OverlayID != value.Overlay.OverlayID || value.Admission.OwnerID != value.Overlay.OwnerID || strings.TrimSpace(value.Admission.Content) == "" {
		return fmt.Errorf("ad-hoc admission bundle is incomplete or crosses an owner boundary")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.AdHocOverlay
		found := tx.Where("overlay_id = ?", value.Overlay.OverlayID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.OwnerID == value.Overlay.OwnerID && existing.ContentHash == value.Overlay.ContentHash {
				return nil
			}
			return ErrIdempotencyConflict
		}
		overlay := adHocOverlayRow(value.Overlay)
		if err := tx.Create(&overlay).Error; err != nil {
			return err
		}
		admission := overlayAdmissionRow(value.Admission)
		if err := tx.Create(&admission).Error; err != nil {
			return err
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.CreateAdHocAdmission")
}

func (s *Store) FindAdHocOverlay(ctx context.Context, ownerID, overlayID string) (*entity.AdHocOverlay, *entity.OverlayAdmission, error) {
	var overlay po.AdHocOverlay
	result := s.data.DB(ctx).Where("owner_id = ? AND overlay_id = ?", ownerID, overlayID).Limit(1).Find(&overlay)
	if result.Error != nil {
		return nil, nil, log.WrapError(result.Error, "DelegationStore.FindAdHocOverlay.overlay")
	}
	if result.RowsAffected == 0 {
		return nil, nil, nil
	}
	var admission po.OverlayAdmission
	if err := s.data.DB(ctx).Where("owner_id = ? AND overlay_id = ?", ownerID, overlayID).Take(&admission).Error; err != nil {
		return nil, nil, log.WrapError(err, "DelegationStore.FindAdHocOverlay.admission")
	}
	overlayValue, admissionValue := adHocOverlayEntity(overlay), overlayAdmissionEntity(admission)
	return &overlayValue, &admissionValue, nil
}

func (s *Store) RecordAdHocOutcome(ctx context.Context, value entity.AdHocRunOutcome) error {
	if strings.TrimSpace(value.OutcomeID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.OverlayID) == "" || strings.TrimSpace(value.RunID) == "" || strings.TrimSpace(value.Status) == "" || value.CreatedAt.IsZero() {
		return fmt.Errorf("ad-hoc run outcome requires identity, owner, overlay, run, status, and timestamp")
	}
	var overlay po.AdHocOverlay
	if err := s.data.DB(ctx).Where("owner_id = ? AND overlay_id = ?", value.OwnerID, value.OverlayID).Take(&overlay).Error; err != nil {
		return log.WrapError(err, "DelegationStore.RecordAdHocOutcome.overlay")
	}
	var existing po.AdHocRunOutcome
	found := s.data.DB(ctx).Where("owner_id = ? AND run_id = ?", value.OwnerID, value.RunID).Limit(1).Find(&existing)
	if found.Error != nil {
		return log.WrapError(found.Error, "DelegationStore.RecordAdHocOutcome.find")
	}
	if found.RowsAffected > 0 {
		if existing.OverlayID == value.OverlayID && existing.Status == value.Status && existing.EvidenceRefs == value.EvidenceRefs {
			return nil
		}
		return ErrIdempotencyConflict
	}
	row := adHocRunOutcomeRow(value)
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "DelegationStore.RecordAdHocOutcome")
}

func (s *Store) ListSuccessfulAdHocOutcomes(ctx context.Context, ownerID, overlayID string) ([]entity.AdHocRunOutcome, error) {
	var rows []po.AdHocRunOutcome
	if err := s.data.DB(ctx).Where("owner_id = ? AND overlay_id = ? AND status = ?", ownerID, overlayID, entity.AdHocOutcomeSuccess).Order("run_id ASC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "DelegationStore.ListSuccessfulAdHocOutcomes")
	}
	result := make([]entity.AdHocRunOutcome, 0, len(rows))
	for _, row := range rows {
		result = append(result, adHocRunOutcomeEntity(row))
	}
	return result, nil
}

func (s *Store) CreateProfileCandidate(ctx context.Context, value entity.ProfileCandidate, event entity.Event) error {
	if strings.TrimSpace(value.CandidateID) == "" || strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.OverlayID) == "" || strings.TrimSpace(value.ContentHash) == "" || strings.TrimSpace(value.Content) == "" || value.Status != entity.ProfileReviewNeeded || value.CreatedAt.IsZero() {
		return fmt.Errorf("profile candidate requires immutable review-required content")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var overlay po.AdHocOverlay
		if err := tx.Where("owner_id = ? AND overlay_id = ?", value.OwnerID, value.OverlayID).Take(&overlay).Error; err != nil {
			return err
		}
		var existing po.ProfileCandidate
		found := tx.Where("candidate_id = ?", value.CandidateID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.OwnerID == value.OwnerID && existing.ContentHash == value.ContentHash {
				return nil
			}
			return ErrIdempotencyConflict
		}
		row := profileCandidateRow(value)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return createEvent(tx, event)
	}), "DelegationStore.CreateProfileCandidate")
}

func (s *Store) FindProfileCandidate(ctx context.Context, ownerID, candidateID string) (*entity.ProfileCandidate, error) {
	var row po.ProfileCandidate
	result := s.data.DB(ctx).Where("owner_id = ? AND candidate_id = ?", ownerID, candidateID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "DelegationStore.FindProfileCandidate")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	value := profileCandidateEntity(row)
	return &value, nil
}

func adHocOverlayRow(value entity.AdHocOverlay) po.AdHocOverlay {
	return po.AdHocOverlay{OverlayID: value.OverlayID, OwnerID: value.OwnerID, BaseProfileRef: value.BaseProfileRef, ContentHash: value.ContentHash, Status: value.Status, Content: value.Content, ExpiresAt: millis(value.ExpiresAt), CreatedAt: millis(value.CreatedAt)}
}

func adHocOverlayEntity(value po.AdHocOverlay) entity.AdHocOverlay {
	return entity.AdHocOverlay{OverlayID: value.OverlayID, OwnerID: value.OwnerID, BaseProfileRef: value.BaseProfileRef, ContentHash: value.ContentHash, Status: value.Status, Content: value.Content, ExpiresAt: fromMillis(value.ExpiresAt), CreatedAt: fromMillis(value.CreatedAt)}
}

func overlayAdmissionRow(value entity.OverlayAdmission) po.OverlayAdmission {
	return po.OverlayAdmission{DecisionID: value.DecisionID, OverlayID: value.OverlayID, OwnerID: value.OwnerID, Decision: value.Decision, PolicyVersion: value.PolicyVersion, InputHash: value.InputHash, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func overlayAdmissionEntity(value po.OverlayAdmission) entity.OverlayAdmission {
	return entity.OverlayAdmission{DecisionID: value.DecisionID, OverlayID: value.OverlayID, OwnerID: value.OwnerID, Decision: value.Decision, PolicyVersion: value.PolicyVersion, InputHash: value.InputHash, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}

func adHocRunOutcomeRow(value entity.AdHocRunOutcome) po.AdHocRunOutcome {
	return po.AdHocRunOutcome{OutcomeID: value.OutcomeID, OverlayID: value.OverlayID, OwnerID: value.OwnerID, RunID: value.RunID, Status: value.Status, EvidenceRefs: value.EvidenceRefs, CreatedAt: millis(value.CreatedAt)}
}

func adHocRunOutcomeEntity(value po.AdHocRunOutcome) entity.AdHocRunOutcome {
	return entity.AdHocRunOutcome{OutcomeID: value.OutcomeID, OverlayID: value.OverlayID, OwnerID: value.OwnerID, RunID: value.RunID, Status: value.Status, EvidenceRefs: value.EvidenceRefs, CreatedAt: fromMillis(value.CreatedAt)}
}

func profileCandidateRow(value entity.ProfileCandidate) po.ProfileCandidate {
	return po.ProfileCandidate{CandidateID: value.CandidateID, OwnerID: value.OwnerID, OverlayID: value.OverlayID, BaseProfileRef: value.BaseProfileRef, ContentHash: value.ContentHash, Status: value.Status, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func profileCandidateEntity(value po.ProfileCandidate) entity.ProfileCandidate {
	return entity.ProfileCandidate{CandidateID: value.CandidateID, OwnerID: value.OwnerID, OverlayID: value.OverlayID, BaseProfileRef: value.BaseProfileRef, ContentHash: value.ContentHash, Status: value.Status, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}
