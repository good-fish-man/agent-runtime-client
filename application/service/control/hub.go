package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	irepository "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
)

var (
	ErrDeviceOffline               = errors.New("device is offline")
	ErrDeviceBoundToAnotherUser    = errors.New("device is bound to another user")
	ErrDeviceCapabilityUnsupported = errors.New("device capability is unsupported")
)

const completedObservationLimit = 4096

type Connection interface {
	Send(any) error
	Close() error
}

type Device struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	Name         string    `json:"name"`
	Platform     string    `json:"platform"`
	Architecture string    `json:"architecture"`
	Capabilities []string  `json:"capabilities"`
	ConnectedAt  time.Time `json:"connected_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Online       bool      `json:"online"`
	conn         Connection
}

type DeviceDiagnostic struct {
	ID             string   `json:"id"`
	Bound          bool     `json:"bound"`
	CurrentUser    bool     `json:"current_user"`
	SupportsAction bool     `json:"supports_action"`
	Capabilities   []string `json:"capabilities"`
}

type DeviceDiagnostics struct {
	UserAuthenticated  bool               `json:"user_authenticated"`
	Capability         string             `json:"capability"`
	ConnectedDevices   int                `json:"connected_devices"`
	CurrentUserDevices int                `json:"current_user_devices"`
	MatchingDevices    int                `json:"matching_devices"`
	OtherUserDevices   int                `json:"other_user_devices"`
	UnsupportedDevices int                `json:"unsupported_devices"`
	Devices            []DeviceDiagnostic `json:"devices"`
}

type Hub struct {
	mu        sync.RWMutex
	devices   map[string]*Device
	pending   map[string]*pendingAction
	completed map[string]entity.Observation
	sessions  map[string]*entity.TaskSession
	active    map[string]map[string]string
	store     irepository.Store
}

type pendingAction struct {
	channel    chan entity.Observation
	deviceID   string
	action     entity.Action
	onProgress func(entity.Progress) error
}

func NewHub(stores ...irepository.Store) *Hub {
	var store irepository.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	return &Hub{devices: make(map[string]*Device), pending: make(map[string]*pendingAction), completed: make(map[string]entity.Observation), sessions: make(map[string]*entity.TaskSession), active: make(map[string]map[string]string), store: store}
}

func (h *Hub) Register(ctx context.Context, message entity.DeviceMessage, conn Connection) error {
	if message.DeviceID == "" || conn == nil {
		return fmt.Errorf("device_id and connection are required")
	}
	now := time.Now().UTC()
	userID := ""
	if h.store != nil {
		stored, err := h.store.FindDevice(ctx, message.DeviceID)
		if err != nil {
			return err
		}
		if stored != nil {
			userID = stored.UserID
		}
		if err := h.store.UpsertDevice(ctx, &entity.RegisteredDevice{
			DeviceID: message.DeviceID, UserID: userID, Name: message.Name, Platform: message.Platform, Architecture: message.Architecture,
			Capabilities: message.Capabilities, Online: true, ConnectedAt: now, LastSeenAt: now,
		}); err != nil {
			return err
		}
	}
	device := &Device{ID: message.DeviceID, UserID: userID, Name: message.Name, Platform: message.Platform, Architecture: message.Architecture, Capabilities: append([]string(nil), message.Capabilities...), ConnectedAt: now, LastSeenAt: now, Online: true, conn: conn}
	h.mu.Lock()
	old := h.devices[device.ID]
	h.devices[device.ID] = device
	h.mu.Unlock()
	if old != nil && old.conn != conn {
		_ = old.conn.Close()
	}
	return nil
}

func (h *Hub) Unregister(ctx context.Context, deviceID string, conn Connection) {
	removed := false
	h.mu.Lock()
	if current := h.devices[deviceID]; current != nil && current.conn == conn {
		delete(h.devices, deviceID)
		removed = true
	}
	h.mu.Unlock()
	if removed && h.store != nil {
		_ = h.store.SetDeviceOnline(ctx, deviceID, false, time.Now().UTC())
	}
}

func (h *Hub) Touch(ctx context.Context, deviceID string) {
	now := time.Now().UTC()
	h.mu.Lock()
	if device := h.devices[deviceID]; device != nil {
		device.LastSeenAt = now
	}
	h.mu.Unlock()
	if h.store != nil {
		_ = h.store.SetDeviceOnline(ctx, deviceID, true, now)
	}
}

func (h *Hub) Devices(ctx context.Context, userID string) ([]Device, error) {
	if h.store != nil {
		stored, err := h.store.ListDevices(ctx, userID)
		if err != nil {
			return nil, err
		}
		result := make([]Device, 0, len(stored))
		for _, device := range stored {
			result = append(result, Device{ID: device.DeviceID, UserID: device.UserID, Name: device.Name, Platform: device.Platform, Architecture: device.Architecture, Capabilities: device.Capabilities, ConnectedAt: device.ConnectedAt, LastSeenAt: device.LastSeenAt, Online: device.Online})
		}
		return result, nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]Device, 0, len(h.devices))
	for _, device := range h.devices {
		if userID != "" && device.UserID != "" && device.UserID != userID {
			continue
		}
		copy := *device
		copy.conn = nil
		copy.Capabilities = append([]string(nil), device.Capabilities...)
		result = append(result, copy)
	}
	return result, nil
}

func (h *Hub) Diagnostics(userID, capability string) DeviceDiagnostics {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := DeviceDiagnostics{UserAuthenticated: strings.TrimSpace(userID) != "", Capability: capability}
	for _, device := range h.devices {
		result.ConnectedDevices++
		supports := supportsCapability(device.Capabilities, capability)
		currentUser := device.UserID == "" || device.UserID == userID
		if device.UserID != "" && device.UserID != userID {
			result.OtherUserDevices++
		}
		if currentUser {
			result.CurrentUserDevices++
		}
		if !supports {
			result.UnsupportedDevices++
		}
		if currentUser && supports {
			result.MatchingDevices++
		}
		result.Devices = append(result.Devices, DeviceDiagnostic{
			ID: shortDeviceID(device.ID), Bound: device.UserID != "", CurrentUser: currentUser,
			SupportsAction: supports, Capabilities: append([]string(nil), device.Capabilities...),
		})
	}
	return result
}

func (h *Hub) HasAvailableCapability(userID string, capabilities ...string) bool {
	if len(capabilities) == 0 {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, device := range h.devices {
		if device.UserID != "" && device.UserID != userID {
			continue
		}
		for _, capability := range capabilities {
			if supportsCapability(device.Capabilities, capability) {
				return true
			}
		}
	}
	return false
}

func (h *Hub) ResolveDevice(ctx context.Context, userID, requested, capability string) (string, error) {
	h.mu.RLock()
	if requested != "" {
		device := h.devices[requested]
		if device == nil {
			h.mu.RUnlock()
			return "", ErrDeviceOffline
		}
		if device.UserID != "" && device.UserID != userID {
			h.mu.RUnlock()
			return "", fmt.Errorf("%w: device %s belongs to another user", ErrDeviceBoundToAnotherUser, requested)
		}
		if !supportsCapability(device.Capabilities, capability) {
			h.mu.RUnlock()
			return "", fmt.Errorf("%w: device %s does not support capability %s", ErrDeviceCapabilityUnsupported, requested, capability)
		}
		h.mu.RUnlock()
		if err := h.bindDevice(ctx, requested, userID); err != nil {
			return "", err
		}
		return requested, nil
	}
	selected := ""
	connectedCount := 0
	ownedByOtherCount := 0
	unsupportedCount := 0
	for id, device := range h.devices {
		connectedCount++
		if device.UserID != "" && device.UserID != userID {
			ownedByOtherCount++
			continue
		}
		if !supportsCapability(device.Capabilities, capability) {
			unsupportedCount++
			continue
		}
		if selected != "" {
			h.mu.RUnlock()
			return "", fmt.Errorf("multiple devices support %s; device_id is required", capability)
		}
		selected = id
	}
	h.mu.RUnlock()
	if selected == "" {
		if connectedCount > 0 && ownedByOtherCount == connectedCount {
			return "", fmt.Errorf("%w: connected device is bound to another user", ErrDeviceBoundToAnotherUser)
		}
		if connectedCount > 0 && unsupportedCount+ownedByOtherCount >= connectedCount {
			return "", fmt.Errorf("%w: no connected device supports capability %s", ErrDeviceCapabilityUnsupported, capability)
		}
		return "", ErrDeviceOffline
	}
	if err := h.bindDevice(ctx, selected, userID); err != nil {
		return "", err
	}
	return selected, nil
}

func shortDeviceID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:6] + "..." + value[len(value)-6:]
}

func (h *Hub) bindDevice(ctx context.Context, deviceID, userID string) error {
	if userID == "" {
		return fmt.Errorf("authenticated user is required to bind a device")
	}
	h.mu.RLock()
	device := h.devices[deviceID]
	if device == nil {
		h.mu.RUnlock()
		return ErrDeviceOffline
	}
	if device.UserID != "" && device.UserID != userID {
		h.mu.RUnlock()
		return fmt.Errorf("device %s belongs to another user", deviceID)
	}
	h.mu.RUnlock()
	if h.store != nil {
		if err := h.store.BindDevice(ctx, deviceID, userID); err != nil {
			return err
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	device = h.devices[deviceID]
	if device == nil {
		return ErrDeviceOffline
	}
	if device.UserID != "" && device.UserID != userID {
		return fmt.Errorf("device %s belongs to another user", deviceID)
	}
	device.UserID = userID
	return nil
}

func (h *Hub) BindDevice(ctx context.Context, deviceID, userID string) error {
	return h.bindDevice(ctx, deviceID, userID)
}

func (h *Hub) BeginTask(ctx context.Context, taskID, userID, conversationID, deviceID string) error {
	now := time.Now().UTC()
	h.mu.Lock()
	if current := h.sessions[taskID]; current != nil {
		if current.UserID != "" && current.UserID != userID {
			h.mu.Unlock()
			return fmt.Errorf("task %s belongs to another user", taskID)
		}
		if current.UserID == "" {
			current.UserID = userID
		}
		current.DeviceID, current.ConversationID, current.UpdatedAt = deviceID, conversationID, now
		copy := cloneTask(current)
		h.mu.Unlock()
		return h.saveTask(ctx, &copy)
	}
	current := &entity.TaskSession{
		TaskID: taskID, UserID: userID, ConversationID: conversationID, DeviceID: deviceID, Status: entity.StatusWaitingAction,
		ActiveSessions: make(map[string]string), CreatedAt: now, UpdatedAt: now,
	}
	h.sessions[taskID] = current
	copy := cloneTask(current)
	h.mu.Unlock()
	return h.saveTask(ctx, &copy)
}

func (h *Hub) ActiveSessions(ctx context.Context, userID, conversationID string) map[string]string {
	h.mu.RLock()
	values := h.active[userID+":"+conversationID]
	result := make(map[string]string, len(values))
	for family, sessionID := range values {
		result[family] = sessionID
	}
	h.mu.RUnlock()
	if len(result) == 0 && h.store != nil {
		if tasks, err := h.store.ListTasks(ctx, userID, conversationID, 20); err == nil {
			for _, task := range tasks {
				for family, sessionID := range task.ActiveSessions {
					if result[family] == "" {
						result[family] = sessionID
					}
				}
			}
		}
	}
	return result
}

func (h *Hub) Task(ctx context.Context, taskID string) (entity.TaskSession, bool, error) {
	h.mu.RLock()
	current := h.sessions[taskID]
	if current != nil {
		copy := cloneTask(current)
		h.mu.RUnlock()
		return copy, true, nil
	}
	h.mu.RUnlock()
	if h.store == nil {
		return entity.TaskSession{}, false, nil
	}
	stored, err := h.store.FindTask(ctx, taskID)
	if err != nil {
		return entity.TaskSession{}, false, err
	}
	if stored == nil {
		return entity.TaskSession{}, false, nil
	}
	return *stored, true, nil
}

func (h *Hub) Tasks(ctx context.Context, userID, conversationID string, limit int) ([]entity.TaskSession, error) {
	if h.store != nil {
		return h.store.ListTasks(ctx, userID, conversationID, limit)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]entity.TaskSession, 0)
	for _, task := range h.sessions {
		if task.UserID == userID && (conversationID == "" || task.ConversationID == conversationID) {
			result = append(result, cloneTask(task))
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (h *Hub) Observe(ctx context.Context, observation entity.Observation) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	if h.store != nil {
		if err := h.store.SaveObservation(ctx, observation); err != nil {
			return err
		}
	}
	h.mu.RLock()
	pending := h.pending[observation.ActionID]
	h.mu.RUnlock()
	if pending == nil {
		if _, ok, _ := h.Task(ctx, observation.TaskID); !ok {
			return nil
		}
		h.mu.Lock()
		if h.sessions[observation.TaskID] == nil && h.store != nil {
			if stored, _ := h.store.FindTask(ctx, observation.TaskID); stored != nil {
				h.sessions[observation.TaskID] = stored
			}
		}
		h.mu.Unlock()
		h.recordObservation(observation.TaskID, observation)
		return nil
	}
	select {
	case pending.channel <- observation:
		return nil
	default:
		return fmt.Errorf("observation for action %s is already queued", observation.ActionID)
	}
}

func (h *Hub) Progress(ctx context.Context, progress entity.Progress) error {
	if err := progress.Validate(); err != nil {
		return err
	}
	h.mu.RLock()
	pending := h.pending[progress.ActionID]
	h.mu.RUnlock()
	if pending == nil {
		if _, ok, _ := h.Task(ctx, progress.TaskID); !ok {
			return nil
		}
		h.recordProgress(progress.TaskID, progress)
		return nil
	}
	if pending.action.TaskID != progress.TaskID || pending.action.Sequence != progress.Sequence {
		return fmt.Errorf("progress correlation mismatch")
	}
	h.recordProgress(progress.TaskID, progress)
	if pending.onProgress != nil {
		return pending.onProgress(progress)
	}
	return nil
}

func (h *Hub) SetTaskStatus(ctx context.Context, taskID, status string) error {
	if !entity.ValidTaskStatus(status) {
		return fmt.Errorf("unsupported task status %q", status)
	}
	h.mu.Lock()
	current := h.sessions[taskID]
	if current == nil {
		h.mu.Unlock()
		if h.store == nil {
			return nil
		}
		stored, err := h.store.FindTask(ctx, taskID)
		if err != nil || stored == nil {
			return err
		}
		current = stored
		h.mu.Lock()
		h.sessions[taskID] = current
	}
	current.Status = status
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	return h.saveTask(ctx, &copy)
}

func (h *Hub) Dispatch(ctx context.Context, deviceID string, action entity.Action, progressHandlers ...func(entity.Progress) error) (*entity.Observation, error) {
	action.Normalize()
	if err := action.Validate(); err != nil {
		return nil, err
	}
	if !action.Deadline.After(time.Now()) {
		return nil, fmt.Errorf("action deadline has expired")
	}
	h.mu.RLock()
	completed, alreadyCompleted := h.completed[action.IdempotencyKey]
	h.mu.RUnlock()
	if alreadyCompleted {
		return &completed, nil
	}
	if h.store != nil {
		stored, err := h.store.FindObservationByIdempotency(ctx, action.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			return stored, nil
		}
	}
	h.mu.RLock()
	device := h.devices[deviceID]
	h.mu.RUnlock()
	if device == nil {
		return nil, ErrDeviceOffline
	}
	if !supportsCapability(device.Capabilities, action.Capability) {
		return nil, fmt.Errorf("device %s does not support capability %s", deviceID, action.Capability)
	}
	channel := make(chan entity.Observation, 1)
	var onProgress func(entity.Progress) error
	if len(progressHandlers) > 0 {
		onProgress = progressHandlers[0]
	}
	h.mu.Lock()
	if _, exists := h.pending[action.ActionID]; exists {
		h.mu.Unlock()
		return nil, fmt.Errorf("action %s is already pending", action.ActionID)
	}
	h.pending[action.ActionID] = &pendingAction{channel: channel, deviceID: deviceID, action: action, onProgress: onProgress}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, action.ActionID)
		h.mu.Unlock()
	}()
	userID := h.taskUserID(ctx, action.TaskID)
	if device.UserID != "" && device.UserID != userID {
		return nil, fmt.Errorf("device %s belongs to another user", deviceID)
	}
	if h.store != nil {
		if err := h.store.SaveAction(ctx, deviceID, userID, action); err != nil {
			return nil, err
		}
	}
	h.recordAction(ctx, deviceID, action)
	if err := device.conn.Send(action); err != nil {
		return nil, fmt.Errorf("send action: %w", err)
	}
	timer := time.NewTimer(time.Until(action.Deadline))
	defer timer.Stop()
	select {
	case observation := <-channel:
		if observation.TaskID != action.TaskID || observation.Sequence != action.Sequence {
			return nil, fmt.Errorf("observation correlation mismatch")
		}
		h.mu.Lock()
		if len(h.completed) >= completedObservationLimit {
			h.completed = make(map[string]entity.Observation)
		}
		h.completed[action.IdempotencyKey] = observation
		h.recordObservationLocked(action.TaskID, observation)
		copy := cloneTask(h.sessions[action.TaskID])
		h.mu.Unlock()
		_ = h.saveTask(context.WithoutCancel(ctx), &copy)
		return &observation, nil
	case <-ctx.Done():
		_ = device.conn.Send(entity.NewCancel(action, "request canceled"))
		h.persistTerminalObservation(context.WithoutCancel(ctx), action, entity.ObservationCancelled, ctx.Err().Error())
		return nil, ctx.Err()
	case <-timer.C:
		_ = device.conn.Send(entity.NewCancel(action, "action deadline exceeded"))
		h.persistTerminalObservation(context.WithoutCancel(ctx), action, entity.ObservationExpired, "action deadline exceeded")
		return nil, fmt.Errorf("action timed out")
	}
}

func (h *Hub) persistTerminalObservation(ctx context.Context, action entity.Action, status, message string) {
	observation := entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, TaskID: action.TaskID, ActionID: action.ActionID,
		SessionID: action.SessionID, Sequence: action.Sequence, Status: status, ObservedAt: time.Now().UTC(), Error: message,
	}
	if h.store != nil {
		_ = h.store.SaveObservation(ctx, observation)
	}
	h.mu.Lock()
	h.completed[action.IdempotencyKey] = observation
	h.mu.Unlock()
	h.recordObservation(action.TaskID, observation)
}

func (h *Hub) CancelByConversation(ctx context.Context, userID, conversationID, reason string) error {
	if userID == "" || conversationID == "" {
		return nil
	}
	tasks, err := h.Tasks(ctx, userID, conversationID, 100)
	if err != nil {
		return err
	}
	taskIDs := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task.Status == entity.StatusCompleted || task.Status == entity.StatusFailed || task.Status == entity.StatusCancelled {
			continue
		}
		taskIDs[task.TaskID] = struct{}{}
		_ = h.SetTaskStatus(ctx, task.TaskID, entity.StatusCancelled)
	}
	h.mu.RLock()
	pending := make([]pendingAction, 0)
	for _, current := range h.pending {
		if _, ok := taskIDs[current.action.TaskID]; ok {
			pending = append(pending, *current)
		}
	}
	h.mu.RUnlock()
	for _, current := range pending {
		h.mu.RLock()
		device := h.devices[current.deviceID]
		h.mu.RUnlock()
		if device != nil {
			_ = device.conn.Send(entity.NewCancel(current.action, reason))
		}
	}
	return nil
}

func (h *Hub) recordAction(ctx context.Context, deviceID string, action entity.Action) {
	h.mu.Lock()
	current := h.sessions[action.TaskID]
	if current == nil {
		now := time.Now().UTC()
		current = &entity.TaskSession{TaskID: action.TaskID, DeviceID: deviceID, ActiveSessions: make(map[string]string), CreatedAt: now}
		h.sessions[action.TaskID] = current
	}
	current.DeviceID = deviceID
	current.Status = entity.StatusExecuting
	current.Sequence = action.Sequence
	current.Actions = append(current.Actions, action)
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	_ = h.saveTask(context.WithoutCancel(ctx), &copy)
}

func (h *Hub) recordObservation(taskID string, observation entity.Observation) {
	h.mu.Lock()
	h.recordObservationLocked(taskID, observation)
	current := h.sessions[taskID]
	var copy entity.TaskSession
	if current != nil {
		copy = cloneTask(current)
	}
	h.mu.Unlock()
	if copy.TaskID != "" {
		_ = h.saveTask(context.Background(), &copy)
	}
}

func (h *Hub) recordProgress(taskID string, progress entity.Progress) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.sessions[taskID]
	if current == nil {
		return
	}
	if current.Metadata == nil {
		current.Metadata = make(map[string]interface{})
	}
	current.Metadata["latest_progress"] = progress
	current.Status = entity.StatusExecuting
	current.UpdatedAt = time.Now().UTC()
}

func (h *Hub) recordObservationLocked(taskID string, observation entity.Observation) {
	current := h.sessions[taskID]
	if current == nil {
		return
	}
	current.Observations = append(current.Observations, observation)
	current.Status = entity.StatusEvaluating
	if observation.Status == entity.ObservationFailed || observation.Status == entity.ObservationExpired || observation.Status == entity.ObservationBlocked {
		current.Status = entity.StatusFailed
	} else if observation.Status == entity.ObservationWaitingApproval {
		current.Status = entity.StatusWaitingApproval
	} else if observation.Status == entity.ObservationWaitingUser {
		current.Status = entity.StatusWaitingUser
	} else if observation.Status == entity.ObservationCancelled {
		current.Status = entity.StatusCancelled
	}
	if observation.SessionID != "" {
		family := sessionFamily(observation.SessionID)
		key := current.UserID + ":" + current.ConversationID
		if h.active[key] == nil {
			h.active[key] = make(map[string]string)
		}
		closed := len(current.Actions) > 0 && strings.HasSuffix(current.Actions[len(current.Actions)-1].Capability, ".close") && observation.Status == entity.ObservationSucceeded
		if closed {
			delete(current.ActiveSessions, family)
			delete(h.active[key], family)
		} else {
			current.ActiveSessions[family] = observation.SessionID
			h.active[key][family] = observation.SessionID
		}
	}
	current.UpdatedAt = time.Now().UTC()
}

func sessionFamily(sessionID string) string {
	for index := 0; index < len(sessionID); index++ {
		if sessionID[index] == '-' {
			return sessionID[:index]
		}
	}
	return "default"
}

func supportsCapability(capabilities []string, capability string) bool {
	for _, candidate := range capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func cloneTask(current *entity.TaskSession) entity.TaskSession {
	if current == nil {
		return entity.TaskSession{}
	}
	copy := *current
	copy.Actions = append([]entity.Action(nil), current.Actions...)
	copy.Observations = append([]entity.Observation(nil), current.Observations...)
	copy.ActiveSessions = make(map[string]string, len(current.ActiveSessions))
	for key, value := range current.ActiveSessions {
		copy.ActiveSessions[key] = value
	}
	return copy
}

func (h *Hub) saveTask(ctx context.Context, task *entity.TaskSession) error {
	if h.store == nil || task == nil || task.TaskID == "" {
		return nil
	}
	return h.store.SaveTask(ctx, task)
}

func (h *Hub) taskUserID(ctx context.Context, taskID string) string {
	h.mu.RLock()
	if task := h.sessions[taskID]; task != nil {
		userID := task.UserID
		h.mu.RUnlock()
		return userID
	}
	h.mu.RUnlock()
	if h.store != nil {
		if task, _ := h.store.FindTask(ctx, taskID); task != nil {
			return task.UserID
		}
	}
	return ""
}
