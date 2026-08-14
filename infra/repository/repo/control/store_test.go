package control

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeviceLeaseIsExclusiveAndFenced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.TempDir()+"/control.db?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.Device{}, &po.CapabilityDefinition{}, &po.CapabilityInstance{}, &po.DeviceCapability{}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(data.New(db))
	now := time.Now().UTC().Truncate(time.Millisecond)
	device := &entity.RegisteredDevice{DeviceID: "device-1", Name: "Desktop", Capabilities: []string{"browser.open"}}
	first, err := store.AcquireDeviceLease(context.Background(), device, "client-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if first.FencingToken != 1 || first.LeaseOwner != "client-a" {
		t.Fatalf("first lease = %+v", first)
	}
	if _, err := store.AcquireDeviceLease(context.Background(), device, "client-b", now.Add(time.Second)); !errors.Is(err, ErrDeviceLeaseOwned) {
		t.Fatalf("second owner error = %v, want ErrDeviceLeaseOwned", err)
	}
	if err := db.Model(&po.Device{}).Where("device_id = ?", device.DeviceID).Update("lease_expires_at", now.Add(-time.Second).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	second, err := store.AcquireDeviceLease(context.Background(), device, "client-b", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.FencingToken != 2 || second.LeaseOwner != "client-b" {
		t.Fatalf("takeover lease = %+v", second)
	}
	if err := store.RenewDeviceLease(context.Background(), device.DeviceID, "client-a", first.FencingToken, now.Add(3*time.Second)); err == nil {
		t.Fatal("stale lease renewed after takeover")
	}
	if err := store.ReleaseDeviceLease(context.Background(), device.DeviceID, "client-a", first.FencingToken, now.Add(3*time.Second)); err == nil {
		t.Fatal("stale lease released the current owner")
	}
}

func TestApplyWorldPatchSupportsSetMergeRemoveAndEscapedPointers(t *testing.T) {
	state := map[string]any{
		"browser": map[string]any{"title": "Before", "stale": true},
	}
	patch := entity.WorldPatch{Mutations: []entity.WorldMutation{
		{Operation: "set", Path: "/browser/title", Value: "After"},
		{Operation: "merge", Path: "/browser", Value: map[string]any{"url": "https://example.com"}},
		{Operation: "remove", Path: "/browser/stale"},
		{Operation: "set", Path: "/entities/a~1b/name~0value", Value: "escaped"},
	}}
	if err := applyWorldPatch(state, patch); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"browser":  map[string]any{"title": "After", "url": "https://example.com"},
		"entities": map[string]any{"a/b": map[string]any{"name~value": "escaped"}},
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("world state = %#v, want %#v", state, want)
	}
}

func TestApplyWorldPatchRejectsPathThroughScalar(t *testing.T) {
	state := map[string]any{"browser": "not-an-object"}
	err := applyWorldPatch(state, entity.WorldPatch{Mutations: []entity.WorldMutation{{
		Operation: "set", Path: "/browser/title", Value: "unsafe",
	}}})
	if err == nil {
		t.Fatal("patch silently replaced a scalar with an object")
	}
	if got := state["browser"]; got != "not-an-object" {
		t.Fatalf("failed patch changed state to %#v", got)
	}
}

func TestApplyWorldPatchRejectsMergeIntoScalar(t *testing.T) {
	state := map[string]any{"browser": "not-an-object"}
	err := applyWorldPatch(state, entity.WorldPatch{Mutations: []entity.WorldMutation{{
		Operation: "merge", Path: "/browser", Value: map[string]any{"title": "unsafe"},
	}}})
	if err == nil {
		t.Fatal("merge silently replaced a scalar with an object")
	}
}

func TestSameDurableActionRejectsArgumentMutation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	action := entity.Action{
		Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: "task-1", StepID: "step-1", ActionID: "action-1",
		Sequence: 1, Revision: 1, IdempotencyKey: "task-1:step-1:action-1", IssuedAt: now,
		Deadline: now.Add(time.Minute), Capability: "browser.type", Operation: "type",
		Arguments: map[string]any{"text": "first"}, Policy: entity.Policy{Risk: entity.RiskReversible, Decision: entity.Allow},
	}
	stored, err := actionToPO("device-1", "user-1", action)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := actionToPO("device-1", "user-1", action)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDurableAction(stored, retry) {
		t.Fatal("identical action retry was rejected")
	}
	action.Arguments["text"] = "materially different"
	mutated, err := actionToPO("device-1", "user-1", action)
	if err != nil {
		t.Fatal(err)
	}
	if sameDurableAction(stored, mutated) {
		t.Fatal("same idempotency identity accepted changed arguments")
	}
}

func TestObservationPersistenceKeepsAttachmentMetadataWithoutPayload(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	observation := entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation,
		ObservationID: "observation-1", TaskID: "task-1", StepID: "step-1", ActionID: "action-1",
		AgentBuildID: "build-1", RunManifestID: "manifest-1",
		Sequence: 1, Revision: 2, Status: entity.ObservationSucceeded, ObservedAt: now,
		Attachments: []entity.Attachment{{
			ID: "capture-1", Kind: "image", MIMEType: "image/png", Size: 4,
			SHA256: "digest", Encoding: entity.AttachmentEncodingBase64, Data: "c2FmZQ==", Purpose: "viewport",
		}},
	}
	safe := observation.WithoutAttachmentData()
	stored, err := observationToPO(safe, "task-1:action-1", "user-1", "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	restored := observationToEntity(stored)
	if len(restored.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(restored.Attachments))
	}
	if restored.Attachments[0].Data != "" {
		t.Fatal("transient attachment payload was persisted")
	}
	if restored.Attachments[0].SHA256 != "digest" || restored.Attachments[0].Purpose != "viewport" {
		t.Fatalf("attachment metadata was not preserved: %#v", restored.Attachments[0])
	}
	if stored.OwnerID != "user-1" || stored.TraceID != "trace-1" {
		t.Fatalf("owner/trace correlation = %q/%q", stored.OwnerID, stored.TraceID)
	}
	if restored.AgentBuildID != "build-1" || restored.RunManifestID != "manifest-1" {
		t.Fatalf("deployment provenance was not preserved: %+v", restored)
	}
}

func TestStableRecordIDIsDeterministicAndScoped(t *testing.T) {
	first := stableRecordID("artifact", "observation-1", "capture-1")
	if first != stableRecordID("artifact", "observation-1", "capture-1") {
		t.Fatal("stable record id changed for identical input")
	}
	if first == stableRecordID("artifact", "observation-2", "capture-1") {
		t.Fatal("stable record id collided across observations")
	}
}
