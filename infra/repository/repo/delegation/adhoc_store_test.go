package delegation

import (
	"context"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
)

func TestAdHocStoreIsOwnerScopedAndIdempotent(t *testing.T) {
	store, db := newTestStore(t)
	if err := db.AutoMigrate(&po.AdHocOverlay{}, &po.OverlayAdmission{}, &po.AdHocRunOutcome{}, &po.ProfileCandidate{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	bundle := entity.AdHocAdmissionBundle{
		Overlay: entity.AdHocOverlay{
			OverlayID: "overlay-1", OwnerID: "owner-a", BaseProfileRef: "specialist-profile://general-read/v1",
			ContentHash: "hash-a", Status: entity.AdHocOverlayAllowed, Content: `{"overlay_id":"overlay-1"}`,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		},
		Admission: entity.OverlayAdmission{
			DecisionID: "decision-1", OverlayID: "overlay-1", OwnerID: "owner-a", Decision: "ALLOW",
			PolicyVersion: "policy/v1", InputHash: "input-a", Content: `{"decision":"ALLOW"}`, CreatedAt: now,
		},
		Event: entity.Event{
			EventID: "event-adhoc-1", OwnerID: "owner-a", AggregateType: "adhoc_overlay", AggregateID: "overlay-1",
			Sequence: 1, Type: "AdHocOverlayAdmissionDecided", IdempotencyKey: "overlay-1:admission:1",
			TraceID: "trace-1", CausationID: "outcome-1", Payload: `{"decision":"ALLOW"}`, CreatedAt: now,
		},
	}
	if err := store.CreateAdHocAdmission(context.Background(), bundle); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAdHocAdmission(context.Background(), bundle); err != nil {
		t.Fatalf("exact replay failed: %v", err)
	}
	if found, admission, err := store.FindAdHocOverlay(context.Background(), "owner-b", "overlay-1"); err != nil || found != nil || admission != nil {
		t.Fatalf("cross-owner read leaked data: found=%+v admission=%+v err=%v", found, admission, err)
	}

	outcome := entity.AdHocRunOutcome{
		OutcomeID: "adhoc-outcome-1", OverlayID: "overlay-1", OwnerID: "owner-a", RunID: "run-1",
		Status: entity.AdHocOutcomeSuccess, EvidenceRefs: `["evidence-1"]`, CreatedAt: now,
	}
	if err := store.RecordAdHocOutcome(context.Background(), outcome); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAdHocOutcome(context.Background(), outcome); err != nil {
		t.Fatalf("outcome exact replay failed: %v", err)
	}
	mutated := outcome
	mutated.Status = entity.AdHocOutcomeFailed
	if err := store.RecordAdHocOutcome(context.Background(), mutated); err != ErrIdempotencyConflict {
		t.Fatalf("conflicting replay err=%v want=%v", err, ErrIdempotencyConflict)
	}
}
