package runtime

import (
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

func TestResourceSnapshotTracksLatestMatchingBrowserTab(t *testing.T) {
	task := controlentity.TaskSession{
		TaskID: "task-1", DeviceID: "device-1", Revision: 7,
		Observations: []controlentity.Observation{
			{ObservationID: "observation-1", SessionID: "session-1", ObservedAt: time.Now(), State: map[string]any{"resource_ref": "browser://session/session-1/tab/tab-a", "resource_version": "v1", "tab_id": "tab-a"}},
			{ObservationID: "observation-2", SessionID: "session-2", ObservedAt: time.Now(), State: map[string]any{"resource_ref": "browser://session/session-2/tab/tab-b", "resource_version": "v2", "tab_id": "tab-b"}},
			{ObservationID: "observation-3", SessionID: "session-1", ObservedAt: time.Now(), State: map[string]any{"resource_ref": "browser://session/session-1/tab/tab-c", "resource_version": "v3", "tab_id": "tab-c"}},
		},
	}
	action := controlentity.Action{Capability: "browser.action", SessionID: "session-1"}
	snapshot := resourceSnapshotFromTask(task, action)
	if snapshot.ResourceVersion != "v3" || snapshot.TabID != "tab-c" || snapshot.ObservationID != "observation-3" {
		t.Fatalf("latest matching tab was not selected: %#v", snapshot)
	}
}

func TestResourceSnapshotCreatesStableBootstrapIdentity(t *testing.T) {
	task := controlentity.TaskSession{TaskID: "task-1", DeviceID: "device-1", Revision: 4}
	action := controlentity.Action{Capability: "browser.open", SessionID: "session-1"}
	first := resourceSnapshotFromTask(task, action)
	second := resourceSnapshotFromTask(task, action)
	if first != second {
		t.Fatalf("bootstrap identity is not deterministic: %#v != %#v", first, second)
	}
	if first.ResourceRef != "browser://device/device-1/session/session-1/tab/pending" || first.ResourceVersion != "unobserved@4" {
		t.Fatalf("unexpected bootstrap identity: %#v", first)
	}
}
