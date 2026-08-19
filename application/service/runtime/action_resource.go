package runtime

import (
	"context"
	"fmt"
	"strings"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

func (s *RuntimeService) controlResourceSnapshot(ctx context.Context, taskID string, action controlentity.Action) (delegationentity.ResourceSnapshot, error) {
	if s == nil || s.controlHub == nil {
		return delegationentity.ResourceSnapshot{}, fmt.Errorf("control hub is unavailable")
	}
	task, ok, err := s.controlHub.Task(ctx, taskID)
	if err != nil {
		return delegationentity.ResourceSnapshot{}, err
	}
	if !ok {
		return delegationentity.ResourceSnapshot{}, fmt.Errorf("control task %s was not found", taskID)
	}
	return resourceSnapshotFromTask(task, action), nil
}

func resourceSnapshotFromTask(task controlentity.TaskSession, action controlentity.Action) delegationentity.ResourceSnapshot {
	explicitRef := actionMapString(action.Target, "resource_ref")
	if explicitRef == "" {
		explicitRef = actionMapString(action.Arguments, "resource_ref")
	}
	explicitVersion := actionMapString(action.Target, "resource_version")
	if explicitVersion == "" {
		explicitVersion = actionMapString(action.Arguments, "resource_version")
	}
	wantedSession := firstActionString(action.SessionID, actionMapString(action.Arguments, "session_id"), actionMapString(action.Target, "session_id"))
	for index := len(task.Observations) - 1; index >= 0; index-- {
		observation := task.Observations[index]
		sessionID := firstActionString(observation.SessionID, actionMapString(observation.State, "session_id"))
		if wantedSession != "" && sessionID != "" && sessionID != wantedSession {
			continue
		}
		resourceRef := firstActionString(actionMapString(observation.State, "resource_ref"), explicitRef)
		resourceVersion := firstActionString(actionMapString(observation.State, "resource_version"), explicitVersion)
		if resourceRef == "" || resourceVersion == "" {
			continue
		}
		return delegationentity.ResourceSnapshot{
			ResourceRef: resourceRef, ResourceVersion: resourceVersion,
			SessionID: sessionID, TabID: actionMapString(observation.State, "tab_id"),
			ObservationID: observation.ObservationID, TaskRevision: task.Revision,
		}
	}
	deviceID := firstActionString(action.DeviceID, task.DeviceID, "unbound")
	sessionID := firstActionString(wantedSession, task.ActiveSessions["browser"], "pending")
	tabID := firstActionString(actionMapString(action.Arguments, "tab_id"), actionMapString(action.Target, "tab_id"), "pending")
	resourceRef := explicitRef
	if resourceRef == "" {
		if strings.HasPrefix(strings.ToLower(action.Capability), "browser.") {
			resourceRef = fmt.Sprintf("browser://device/%s/session/%s/tab/%s", deviceID, sessionID, tabID)
		} else {
			resourceRef = fmt.Sprintf("capability://device/%s/%s", deviceID, action.Capability)
		}
	}
	resourceVersion := firstActionString(explicitVersion, fmt.Sprintf("unobserved@%d", task.Revision))
	return delegationentity.ResourceSnapshot{ResourceRef: resourceRef, ResourceVersion: resourceVersion, SessionID: sessionID, TabID: tabID, TaskRevision: task.Revision}
}

func actionMapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstActionString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
