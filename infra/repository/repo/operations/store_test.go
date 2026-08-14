package operations

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/operations"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
)

func TestStoreScopesRunsByOwnerAndRejectsReplacement(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	ownerOne := preflightSuite("run-owner-1")
	ownerTwo := preflightSuite("run-owner-2")
	if err := store.SaveGoldenJourneyResults(ctx, "owner-1", ownerOne); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGoldenJourneyResults(ctx, "owner-2", ownerTwo); err != nil {
		t.Fatal(err)
	}
	actual, err := store.LastGoldenJourneyResults(ctx, "owner-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 10 || actual[0].RunID != "run-owner-1" {
		t.Fatalf("owner-scoped results = %+v", actual)
	}
	if err := store.SaveGoldenJourneyResults(ctx, "owner-1", ownerOne); err == nil {
		t.Fatal("expected append-only run replacement to fail")
	}
}

func TestStoreDetectsEvidenceTampering(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	results := preflightSuite("run-tamper")
	if err := store.SaveGoldenJourneyResults(ctx, "owner-1", results); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&po.GoldenJourneyResult{}).
		Where("owner_id = ? AND run_id = ? AND journey_id = ?", "owner-1", "run-tamper", results[0].JourneyID).
		Update("content", "{}").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.LastGoldenJourneyResults(ctx, "owner-1", ""); err == nil {
		t.Fatal("expected modified evidence content to fail integrity verification")
	}
}

func TestStoreRejectsIncompletePersistedSuite(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	results := preflightSuite("run-incomplete")
	if err := store.SaveGoldenJourneyResults(ctx, "owner-1", results); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("owner_id = ? AND run_id = ? AND journey_id = ?", "owner-1", "run-incomplete", results[0].JourneyID).
		Delete(&po.GoldenJourneyResult{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.LastGoldenJourneyResults(ctx, "owner-1", ""); err == nil {
		t.Fatal("expected incomplete persisted evidence suite to fail validation")
	}
}

func newTestStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.GoldenJourneyResult{}); err != nil {
		t.Fatal(err)
	}
	return NewStore(data.New(db)), db
}

func preflightSuite(runID string) []ga.GoldenJourneyResult {
	now := time.Now().UTC()
	results := make([]ga.GoldenJourneyResult, 0, len(ga.GoldenJourneys()))
	for _, journey := range ga.GoldenJourneys() {
		steps := make([]ga.GoldenJourneyStepResult, 0, len(journey.Steps))
		for _, step := range journey.Steps {
			steps = append(steps, ga.GoldenJourneyStepResult{StepID: step.ID, Status: ga.StatusNotRun, Message: "preflight only", DurationMS: 0})
		}
		results = append(results, ga.GoldenJourneyResult{
			RunID: runID, JourneyID: journey.ID, VerificationLevel: ga.VerificationPreflight,
			Status: ga.StatusNotRun, Steps: steps, StartedAt: now, FinishedAt: now,
		})
	}
	return results
}
