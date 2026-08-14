package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/operations"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
	log "github.com/good-fish-man/logx"
)

type Store struct{ data *data.Data }

func NewStore(value *data.Data) *Store { return &Store{data: value} }

func (s *Store) SaveGoldenJourneyResults(ctx context.Context, ownerID string, results []ga.GoldenJourneyResult) error {
	if s == nil || s.data == nil || strings.TrimSpace(ownerID) == "" || len(results) == 0 {
		return fmt.Errorf("golden journey owner, store, and results are required")
	}
	if err := ga.ValidateGoldenJourneySuite(results, results[0].VerificationLevel); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	rows := make([]po.GoldenJourneyResult, 0, len(results))
	for _, result := range results {
		content, err := json.Marshal(result)
		if err != nil {
			return log.WrapError(err, "GAEvidenceStore.Save.encode")
		}
		digest := sha256.Sum256(content)
		rows = append(rows, po.GoldenJourneyResult{
			OwnerID: ownerID, RunID: result.RunID, JourneyID: result.JourneyID,
			Verification: result.VerificationLevel, Status: result.Status, Content: string(content),
			ContentSHA256: hex.EncodeToString(digest[:]), TraceID: traceEvidence(result),
			Revision: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(&rows).Error
	}), "GAEvidenceStore.Save")
}

func (s *Store) LastGoldenJourneyResults(ctx context.Context, ownerID, verificationLevel string) ([]ga.GoldenJourneyResult, error) {
	if s == nil || s.data == nil || strings.TrimSpace(ownerID) == "" {
		return nil, nil
	}
	var latest po.GoldenJourneyResult
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if strings.TrimSpace(verificationLevel) != "" {
		db = db.Where("verification_level = ?", verificationLevel)
	}
	query := db.Order("created_at DESC, run_id DESC, journey_id DESC").Limit(1).Find(&latest)
	if query.Error != nil {
		return nil, log.WrapError(query.Error, "GAEvidenceStore.Last.find-run")
	}
	if query.RowsAffected == 0 {
		return nil, nil
	}
	var rows []po.GoldenJourneyResult
	if err := s.data.DB(ctx).Where("owner_id = ? AND run_id = ?", ownerID, latest.RunID).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "GAEvidenceStore.Last.list")
	}
	byJourney := make(map[string]ga.GoldenJourneyResult, len(rows))
	for _, row := range rows {
		content := []byte(row.Content)
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != row.ContentSHA256 {
			return nil, fmt.Errorf("golden journey evidence %s/%s failed integrity verification", row.RunID, row.JourneyID)
		}
		var result ga.GoldenJourneyResult
		if err := json.Unmarshal(content, &result); err != nil {
			return nil, log.WrapError(err, "GAEvidenceStore.Last.decode")
		}
		journey, ok := ga.GoldenJourneyByID(result.JourneyID)
		if !ok || result.RunID != row.RunID || result.JourneyID != row.JourneyID || result.VerificationLevel != row.Verification || result.Status != row.Status {
			return nil, fmt.Errorf("golden journey evidence metadata does not match its signed content")
		}
		if err := result.ValidateAgainst(journey); err != nil {
			return nil, log.WrapError(err, "GAEvidenceStore.Last.validate")
		}
		byJourney[result.JourneyID] = result
	}
	ordered := make([]ga.GoldenJourneyResult, 0, len(byJourney))
	for _, journey := range ga.GoldenJourneys() {
		if result, ok := byJourney[journey.ID]; ok {
			ordered = append(ordered, result)
		}
	}
	if len(ordered) != len(rows) {
		return nil, fmt.Errorf("golden journey run %s contains duplicate or unknown records", latest.RunID)
	}
	if err := ga.ValidateGoldenJourneySuite(ordered, latest.Verification); err != nil {
		return nil, log.WrapError(err, "GAEvidenceStore.Last.validate-suite")
	}
	return ordered, nil
}

func traceEvidence(result ga.GoldenJourneyResult) string {
	for _, step := range result.Steps {
		for _, evidence := range step.Evidence {
			if evidence.Kind == "trace_id" {
				return evidence.Reference
			}
		}
	}
	return ""
}
