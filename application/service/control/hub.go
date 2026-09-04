package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	worldmodel "github.com/good-fish-man/agent-runtime-client/application/service/worldmodel"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	irepository "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
	"github.com/good-fish-man/agent-runtime-client/pkg/dbctx"
	"github.com/good-fish-man/athena-protocol/sdk/safety"
	log "github.com/good-fish-man/logx"
)

var (
	ErrDeviceOffline               = irepository.ErrDeviceOffline
	ErrDeviceNotFound              = irepository.ErrDeviceNotFound
	ErrDeviceBoundToAnotherUser    = irepository.ErrDeviceOwnerMismatch
	ErrDeviceHasActiveTasks        = irepository.ErrDeviceHasActiveTasks
	ErrDeviceCapabilityUnsupported = errors.New("device capability is unsupported")
)

const (
	completedObservationLimit = 4096
	defaultApprovalTTL        = 15 * time.Minute
	deviceLeaseTTL            = 45 * time.Second
)

var inlineSecretPattern = regexp.MustCompile(`(?i)((?:password|passwd|secret|access[_-]?token|refresh[_-]?token|api[_-]?key|authorization|cookie|credential)\s*[:=]\s*)[^\s,;}&]+`)

type Connection interface {
	Send(any) error
	Close() error
}

type Device struct {
	ID                  string                      `json:"id"`
	UserID              string                      `json:"user_id,omitempty"`
	Name                string                      `json:"name"`
	Platform            string                      `json:"platform"`
	Architecture        string                      `json:"architecture"`
	Capabilities        []string                    `json:"capabilities"`
	CapabilityInstances []entity.CapabilityInstance `json:"capability_instances,omitempty"`
	ConnectedAt         time.Time                   `json:"connected_at"`
	LastSeenAt          time.Time                   `json:"last_seen_at"`
	Online              bool                        `json:"online"`
	LeaseOwner          string                      `json:"lease_owner,omitempty"`
	FencingToken        uint64                      `json:"fencing_token,omitempty"`
	LeaseExpiresAt      time.Time                   `json:"lease_expires_at,omitempty"`
	conn                Connection
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
	mu           sync.RWMutex
	devices      map[string]*Device
	pending      map[string]*pendingAction
	completed    map[string]entity.Observation
	sessions     map[string]*entity.TaskSession
	active       map[string]map[string]string
	store        irepository.Store
	world        *worldmodel.Service
	eventsMu     sync.RWMutex
	subscribers  map[string]map[chan entity.EventEnvelope]struct{}
	workerMu     sync.Mutex
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup
	terminalMu   sync.RWMutex
	terminal     []func(context.Context, string)
	instanceID   string
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
	hub := &Hub{devices: make(map[string]*Device), pending: make(map[string]*pendingAction), completed: make(map[string]entity.Observation), sessions: make(map[string]*entity.TaskSession), active: make(map[string]map[string]string), store: store, subscribers: make(map[string]map[chan entity.EventEnvelope]struct{}), instanceID: controlPlaneInstanceID()}
	if store != nil {
		hub.world = worldmodel.NewService(store)
	}
	return hub
}

func (h *Hub) SetOntologyResolver(resolver worldmodel.OntologyResolver) {
	if h == nil || h.world == nil {
		return
	}
	h.world.SetOntologyResolver(resolver)
}

func controlPlaneInstanceID() string {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
}

type deviceLeaseStore interface {
	AcquireDeviceLease(context.Context, *entity.RegisteredDevice, string, time.Time) (*entity.RegisteredDevice, error)
	RenewDeviceLease(context.Context, string, string, uint64, time.Time) error
	ReleaseDeviceLease(context.Context, string, string, uint64, time.Time) error
	ValidateDeviceLease(context.Context, string, string, uint64, time.Time) error
}

type outboxStore interface {
	ClaimOutbox(context.Context, int) ([]irepository.OutboxMessage, error)
	MarkOutboxPublished(context.Context, string, time.Time) error
	MarkOutboxFailed(context.Context, string, string, time.Time) error
}

func (h *Hub) Start(parent context.Context) {
	store, ok := h.store.(outboxStore)
	if !ok {
		return
	}
	h.workerMu.Lock()
	if h.workerCancel != nil {
		h.workerMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	h.workerCancel = cancel
	h.workerWG.Add(1)
	h.workerMu.Unlock()
	go func() {
		defer h.workerWG.Done()
		h.runOutbox(ctx, store)
	}()
}

func (h *Hub) Stop() {
	h.workerMu.Lock()
	cancel := h.workerCancel
	h.workerCancel = nil
	h.workerMu.Unlock()
	if cancel != nil {
		cancel()
		h.workerWG.Wait()
	}
}

// OnTaskTerminal registers a lightweight durable-task notification. Listeners
// must not perform blocking work; the Experience Engine only enqueues the ID
// and relies on its database scan for crash recovery.
func (h *Hub) OnTaskTerminal(listener func(context.Context, string)) {
	if listener == nil {
		return
	}
	h.terminalMu.Lock()
	h.terminal = append(h.terminal, listener)
	h.terminalMu.Unlock()
}

func (h *Hub) Subscribe(taskID string) (<-chan entity.EventEnvelope, func()) {
	channel := make(chan entity.EventEnvelope, 256)
	h.eventsMu.Lock()
	if h.subscribers[taskID] == nil {
		h.subscribers[taskID] = make(map[chan entity.EventEnvelope]struct{})
	}
	h.subscribers[taskID][channel] = struct{}{}
	h.eventsMu.Unlock()
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			h.eventsMu.Lock()
			delete(h.subscribers[taskID], channel)
			if len(h.subscribers[taskID]) == 0 {
				delete(h.subscribers, taskID)
			}
			close(channel)
			h.eventsMu.Unlock()
		})
	}
}

func (h *Hub) runOutbox(ctx context.Context, store outboxStore) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	dbCtx := dbctx.SuppressQueryInfo(ctx)
	for {
		if err := h.drainOutbox(dbCtx, store); err != nil && ctx.Err() == nil {
			log.Warnw(ctx, "control outbox dispatch failed", "error_chain", log.FormatError(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Hub) drainOutbox(ctx context.Context, store outboxStore) error {
	messages, err := store.ClaimOutbox(ctx, 100)
	if err != nil {
		return err
	}
	for _, message := range messages {
		var event entity.EventEnvelope
		if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
			retryAt := time.Now().UTC().Add(outboxBackoff(message.Attempts))
			_ = store.MarkOutboxFailed(context.WithoutCancel(ctx), message.OutboxID, err.Error(), retryAt)
			continue
		}
		h.publishEvent(event)
		if err := store.MarkOutboxPublished(ctx, message.OutboxID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) publishEvent(event entity.EventEnvelope) {
	h.eventsMu.RLock()
	defer h.eventsMu.RUnlock()
	for channel := range h.subscribers[event.TaskID] {
		select {
		case channel <- event:
		default:
		}
	}
}

func outboxBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func (h *Hub) Register(ctx context.Context, message entity.DeviceMessage, conn Connection) error {
	if message.DeviceID == "" || conn == nil {
		return fmt.Errorf("device_id and connection are required")
	}
	now := time.Now().UTC()
	instances := normalizeCapabilityInstances(message.DeviceID, message.Capabilities, message.CapabilityInstances)
	userID := ""
	if h.store != nil {
		stored, err := h.store.FindDevice(ctx, message.DeviceID)
		if err != nil {
			return err
		}
		if stored != nil {
			userID = stored.UserID
		}
		candidate := &entity.RegisteredDevice{
			DeviceID: message.DeviceID, UserID: userID, Name: message.Name, Platform: message.Platform, Architecture: message.Architecture,
			Capabilities: message.Capabilities, CapabilityInstances: instances,
			Online: true, ConnectedAt: now, LastSeenAt: now,
		}
		if leaseStore, ok := h.store.(deviceLeaseStore); ok {
			leased, err := leaseStore.AcquireDeviceLease(ctx, candidate, h.instanceID, now)
			if err != nil {
				return err
			}
			candidate = leased
		} else if err := h.store.UpsertDevice(ctx, candidate); err != nil {
			return err
		}
		userID = candidate.UserID
		device := &Device{ID: message.DeviceID, UserID: userID, Name: message.Name, Platform: message.Platform, Architecture: message.Architecture, Capabilities: append([]string(nil), message.Capabilities...), CapabilityInstances: append([]entity.CapabilityInstance(nil), instances...), ConnectedAt: now, LastSeenAt: now, Online: true, LeaseOwner: candidate.LeaseOwner, FencingToken: candidate.FencingToken, LeaseExpiresAt: candidate.LeaseExpiresAt, conn: conn}
		h.installDevice(ctx, device, conn)
		return nil
	}
	device := &Device{ID: message.DeviceID, UserID: userID, Name: message.Name, Platform: message.Platform, Architecture: message.Architecture, Capabilities: append([]string(nil), message.Capabilities...), CapabilityInstances: append([]entity.CapabilityInstance(nil), instances...), ConnectedAt: now, LastSeenAt: now, Online: true, conn: conn}
	h.installDevice(ctx, device, conn)
	return nil
}

func (h *Hub) installDevice(ctx context.Context, device *Device, conn Connection) {
	h.mu.Lock()
	old := h.devices[device.ID]
	h.devices[device.ID] = device
	h.mu.Unlock()
	if old != nil && old.conn != conn {
		_ = old.conn.Close()
	}
	go h.recoverDeviceActions(context.WithoutCancel(ctx), device)
}

func (h *Hub) recoverDeviceActions(ctx context.Context, device *Device) {
	store, ok := h.store.(interface {
		ListPendingActions(context.Context, string, int) ([]entity.Action, error)
	})
	if !ok || device == nil {
		return
	}
	actions, err := store.ListPendingActions(ctx, device.ID, 100)
	if err != nil {
		log.Warnw(ctx, "load recoverable device actions failed", "device_id", device.ID, "error_chain", log.FormatError(err))
		return
	}
	for _, action := range actions {
		if !action.Deadline.After(time.Now()) {
			h.persistTerminalObservation(ctx, action, entity.ObservationExpired, "action expired while the Control Plane was offline")
			continue
		}
		action.DeviceID = device.ID
		action.LeaseOwner = device.LeaseOwner
		action.FencingToken = device.FencingToken
		if err := h.ensureDeviceLease(ctx, device); err != nil {
			log.Warnw(ctx, "reject recoverable action from stale control plane", "device_id", device.ID, "action_id", action.ActionID, "error_chain", log.FormatError(err))
			return
		}
		if err := device.conn.Send(action); err != nil {
			log.Warnw(ctx, "redispatch recoverable device action failed", "device_id", device.ID, "task_id", action.TaskID, "action_id", action.ActionID, "error_chain", log.FormatError(err))
			return
		}
	}
}

func (h *Hub) DeviceLease(deviceID string, conn Connection) (entity.DeviceMessage, error) {
	h.mu.RLock()
	device := h.devices[deviceID]
	h.mu.RUnlock()
	if device == nil || device.conn != conn || !device.Online {
		return entity.DeviceMessage{}, ErrDeviceOffline
	}
	return entity.DeviceMessage{
		Protocol: entity.Protocol, Type: entity.TypeWelcome, DeviceID: device.ID,
		LeaseOwner: device.LeaseOwner, FencingToken: device.FencingToken,
		LeaseExpiresAt: device.LeaseExpiresAt, SentAt: time.Now().UTC(),
	}, nil
}

func (h *Hub) validateDeviceConnection(ctx context.Context, deviceID string, conn Connection, owner string, token uint64) (*Device, error) {
	h.mu.RLock()
	device := h.devices[deviceID]
	h.mu.RUnlock()
	if device == nil || device.conn != conn || !device.Online {
		return nil, ErrDeviceOffline
	}
	if device.FencingToken > 0 && (owner != device.LeaseOwner || token != device.FencingToken) {
		return nil, fmt.Errorf("device message fencing token is stale")
	}
	if err := h.ensureDeviceLease(ctx, device); err != nil {
		return nil, err
	}
	return device, nil
}

func (h *Hub) ensureDeviceLease(ctx context.Context, device *Device) error {
	if device == nil {
		return ErrDeviceOffline
	}
	if leaseStore, ok := h.store.(deviceLeaseStore); ok {
		if err := leaseStore.ValidateDeviceLease(ctx, device.ID, device.LeaseOwner, device.FencingToken, time.Now().UTC()); err != nil {
			return fmt.Errorf("device lease validation failed: %w", err)
		}
	}
	return nil
}

func (h *Hub) ObserveFromDevice(ctx context.Context, deviceID string, conn Connection, observation entity.Observation) error {
	if observation.DeviceID == "" {
		observation.DeviceID = deviceID
	}
	if observation.DeviceID != deviceID {
		return fmt.Errorf("observation device_id does not match the authenticated connection")
	}
	if _, err := h.validateDeviceConnection(ctx, deviceID, conn, observation.LeaseOwner, observation.FencingToken); err != nil {
		return err
	}
	return h.Observe(ctx, observation)
}

func (h *Hub) ProgressFromDevice(ctx context.Context, deviceID string, conn Connection, progress entity.Progress) error {
	if progress.DeviceID == "" {
		progress.DeviceID = deviceID
	}
	if progress.DeviceID != deviceID {
		return fmt.Errorf("progress device_id does not match the authenticated connection")
	}
	if _, err := h.validateDeviceConnection(ctx, deviceID, conn, progress.LeaseOwner, progress.FencingToken); err != nil {
		return err
	}
	return h.Progress(ctx, progress)
}

func (h *Hub) Unregister(ctx context.Context, deviceID string, conn Connection) {
	removed := false
	leaseOwner := ""
	var fencingToken uint64
	h.mu.Lock()
	if current := h.devices[deviceID]; current != nil && current.conn == conn {
		leaseOwner, fencingToken = current.LeaseOwner, current.FencingToken
		delete(h.devices, deviceID)
		removed = true
	}
	h.mu.Unlock()
	if removed && h.store != nil {
		if leaseStore, ok := h.store.(deviceLeaseStore); ok {
			_ = leaseStore.ReleaseDeviceLease(ctx, deviceID, leaseOwner, fencingToken, time.Now().UTC())
		} else {
			_ = h.store.SetDeviceOnline(ctx, deviceID, false, time.Now().UTC())
		}
	}
}

func (h *Hub) Touch(ctx context.Context, deviceID string) {
	now := time.Now().UTC()
	var owner string
	var token uint64
	var conn Connection
	h.mu.Lock()
	if device := h.devices[deviceID]; device != nil {
		device.LastSeenAt = now
		owner, token, conn = device.LeaseOwner, device.FencingToken, device.conn
		device.LeaseExpiresAt = now.Add(deviceLeaseTTL)
	}
	h.mu.Unlock()
	if h.store != nil {
		if leaseStore, ok := h.store.(deviceLeaseStore); ok && token > 0 {
			if err := leaseStore.RenewDeviceLease(ctx, deviceID, owner, token, now); err != nil {
				h.mu.Lock()
				if current := h.devices[deviceID]; current != nil && current.FencingToken == token {
					delete(h.devices, deviceID)
				}
				h.mu.Unlock()
				if conn != nil {
					_ = conn.Close()
				}
				log.Warnw(ctx, "device lease renewal rejected", "device_id", shortDeviceID(deviceID), "fencing_token", token, "error_chain", log.FormatError(err))
			}
		} else {
			_ = h.store.SetDeviceOnline(ctx, deviceID, true, now)
		}
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
			result = append(result, Device{ID: device.DeviceID, UserID: device.UserID, Name: device.Name, Platform: device.Platform, Architecture: device.Architecture, Capabilities: device.Capabilities, CapabilityInstances: device.CapabilityInstances, ConnectedAt: device.ConnectedAt, LastSeenAt: device.LastSeenAt, Online: device.Online, LeaseOwner: device.LeaseOwner, FencingToken: device.FencingToken, LeaseExpiresAt: device.LeaseExpiresAt})
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
		copy.CapabilityInstances = append([]entity.CapabilityInstance(nil), device.CapabilityInstances...)
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
	deviceID, _, err := h.ResolveCapability(ctx, userID, requested, capability, "")
	return deviceID, err
}

// ResolveCapability routes by owner, device and capability instance. An empty
// instance selects the sole matching instance and rejects ambiguous devices.
func (h *Hub) ResolveCapability(ctx context.Context, userID, requested, capability, requestedInstance string) (string, string, error) {
	h.mu.RLock()
	if requested != "" {
		device := h.devices[requested]
		if device == nil {
			h.mu.RUnlock()
			return "", "", ErrDeviceOffline
		}
		if device.UserID != "" && device.UserID != userID {
			h.mu.RUnlock()
			return "", "", fmt.Errorf("%w: device %s belongs to another user", ErrDeviceBoundToAnotherUser, requested)
		}
		instanceID, ok := resolveCapabilityInstance(device, capability, requestedInstance)
		if !ok {
			h.mu.RUnlock()
			return "", "", fmt.Errorf("%w: device %s does not support capability %s with instance %q", ErrDeviceCapabilityUnsupported, requested, capability, requestedInstance)
		}
		h.mu.RUnlock()
		if err := h.bindDevice(ctx, requested, userID); err != nil {
			return "", "", err
		}
		return requested, instanceID, nil
	}
	selected := ""
	selectedInstance := ""
	connectedCount := 0
	ownedByOtherCount := 0
	unsupportedCount := 0
	for id, device := range h.devices {
		connectedCount++
		if device.UserID != "" && device.UserID != userID {
			ownedByOtherCount++
			continue
		}
		instanceID, ok := resolveCapabilityInstance(device, capability, requestedInstance)
		if !ok {
			unsupportedCount++
			continue
		}
		if selected != "" {
			h.mu.RUnlock()
			return "", "", fmt.Errorf("multiple devices support %s; device_id is required", capability)
		}
		selected = id
		selectedInstance = instanceID
	}
	h.mu.RUnlock()
	if selected == "" {
		if connectedCount > 0 && ownedByOtherCount == connectedCount {
			return "", "", fmt.Errorf("%w: connected device is bound to another user", ErrDeviceBoundToAnotherUser)
		}
		if connectedCount > 0 && unsupportedCount+ownedByOtherCount >= connectedCount {
			return "", "", fmt.Errorf("%w: no connected device supports capability %s", ErrDeviceCapabilityUnsupported, capability)
		}
		return "", "", ErrDeviceOffline
	}
	if err := h.bindDevice(ctx, selected, userID); err != nil {
		return "", "", err
	}
	return selected, selectedInstance, nil
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

// UnbindDevice removes an account binding and invalidates the current logical
// connection. The launcher reconnects with a fresh lease and no account owner.
func (h *Hub) UnbindDevice(ctx context.Context, deviceID, userID string) error {
	deviceID = strings.TrimSpace(deviceID)
	userID = strings.TrimSpace(userID)
	if deviceID == "" {
		return ErrDeviceNotFound
	}
	if userID == "" {
		return fmt.Errorf("authenticated user is required to unbind a device")
	}

	h.mu.Lock()
	device := h.devices[deviceID]
	if device != nil && device.UserID != "" && device.UserID != userID {
		h.mu.Unlock()
		return ErrDeviceBoundToAnotherUser
	}
	for _, pending := range h.pending {
		if pending.deviceID == deviceID {
			h.mu.Unlock()
			return ErrDeviceHasActiveTasks
		}
	}
	for _, task := range h.sessions {
		if task.DeviceID == deviceID && task.UserID == userID && !entity.TerminalTaskStatus(task.Status) {
			h.mu.Unlock()
			return ErrDeviceHasActiveTasks
		}
	}
	if h.store != nil {
		if err := h.store.UnbindDevice(ctx, deviceID, userID); err != nil {
			h.mu.Unlock()
			return err
		}
	} else {
		if device == nil {
			h.mu.Unlock()
			return ErrDeviceNotFound
		}
		if device.UserID == "" {
			h.mu.Unlock()
			return nil
		}
	}
	var connection Connection
	if device != nil && h.store != nil {
		delete(h.devices, deviceID)
		connection = device.conn
	} else if device != nil {
		device.UserID = ""
	}
	h.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
	log.Infow(ctx, "device unbound", "device_id", shortDeviceID(deviceID), "user_id", userID)
	return nil
}

func (h *Hub) BeginTask(ctx context.Context, taskID, userID, conversationID, deviceID string) error {
	now := time.Now().UTC()
	h.mu.Lock()
	if current := h.sessions[taskID]; current != nil {
		if current.UserID != "" && current.UserID != userID {
			h.mu.Unlock()
			return fmt.Errorf("task %s belongs to another user", taskID)
		}
		changed := false
		if current.UserID == "" {
			current.UserID = userID
			changed = true
		}
		if current.DeviceID != deviceID || current.ConversationID != conversationID {
			current.DeviceID, current.ConversationID = deviceID, conversationID
			changed = true
		}
		if changed {
			current.Revision++
			current.UpdatedAt = now
		}
		copy := cloneTask(current)
		h.mu.Unlock()
		return h.saveTask(ctx, &copy)
	}
	current := &entity.TaskSession{
		TaskID: taskID, UserID: userID, ConversationID: conversationID, DeviceID: deviceID,
		Status: entity.TaskStatusCreated, Revision: 1,
		ActiveSessions: make(map[string]string), CreatedAt: now, UpdatedAt: now,
	}
	h.sessions[taskID] = current
	copy := cloneTask(current)
	h.mu.Unlock()
	if err := h.saveTask(ctx, &copy); err != nil {
		return err
	}
	return nil
}

// DescribeTask stores the sanitized user goal and structured intent used to
// explain the task later. Raw credentials are removed before durable storage.
func (h *Hub) DescribeTask(ctx context.Context, taskID, goal string, metadata map[string]any) error {
	h.mu.Lock()
	current := h.sessions[taskID]
	if current == nil {
		h.mu.Unlock()
		return fmt.Errorf("task %s is not loaded", taskID)
	}
	current.Goal = redactSensitiveString(goal)
	if current.Metadata == nil {
		current.Metadata = make(map[string]interface{})
	}
	for key, value := range metadata {
		current.Metadata[key] = redactSensitiveValue(value)
	}
	current.Revision++
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	return h.saveTask(ctx, &copy)
}

func (h *Hub) notifyTaskTerminal(ctx context.Context, taskID string) {
	h.terminalMu.RLock()
	listeners := append([]func(context.Context, string){}, h.terminal...)
	h.terminalMu.RUnlock()
	for _, listener := range listeners {
		listener(ctx, taskID)
	}
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

func (h *Hub) Events(ctx context.Context, taskID string, afterSequence int64, limit int) ([]entity.EventEnvelope, error) {
	if h.store == nil {
		return []entity.EventEnvelope{}, nil
	}
	return h.store.ListEvents(ctx, taskID, afterSequence, limit)
}

func (h *Hub) WorldState(ctx context.Context, taskID string) (state *entity.WorldState, err error) {
	span := log.StartSpan(ctx, "world.query", "task_id", taskID)
	defer func() {
		revision := int64(0)
		if state != nil {
			revision = state.Revision
		}
		span.End(err, "found", state != nil, "revision", revision)
	}()
	if h.store == nil {
		return nil, nil
	}
	return h.store.FindWorldState(ctx, taskID)
}

func (h *Hub) WorldSnapshot(ctx context.Context, ownerID, taskID string) (*worldmodel.Snapshot, error) {
	if h == nil || h.world == nil {
		return nil, nil
	}
	return h.world.Snapshot(ctx, ownerID, taskID)
}

func (h *Hub) OntologyContext(ctx context.Context, ownerID string) (*worldmodel.OntologyContext, error) {
	if h == nil || h.world == nil {
		return nil, nil
	}
	return h.world.OntologyContext(ctx, ownerID)
}

func (h *Hub) WorldModel() *worldmodel.Service {
	if h == nil {
		return nil
	}
	return h.world
}

func (h *Hub) Approvals(ctx context.Context, ownerID, status string, limit int) ([]entity.Approval, error) {
	if h.store == nil {
		return []entity.Approval{}, nil
	}
	return h.store.ListApprovals(ctx, ownerID, status, limit)
}

func (h *Hub) DecideApproval(ctx context.Context, approvalID, ownerID string, approved bool, reason string) (*entity.Approval, *entity.Observation, error) {
	if h.store == nil {
		return nil, nil, fmt.Errorf("durable approval store is unavailable")
	}
	status := entity.ApprovalRejected
	if approved {
		status = entity.ApprovalApproved
	}
	approval, action, err := h.store.DecideApproval(ctx, approvalID, ownerID, status, ownerID, strings.TrimSpace(reason), time.Now().UTC())
	if err != nil {
		return approval, nil, err
	}
	if action == nil {
		return approval, nil, fmt.Errorf("approval %s has no durable action", approvalID)
	}
	if err := h.ensureTaskLoaded(ctx, action.TaskID); err != nil {
		return approval, nil, err
	}
	if !approved {
		observation := policyObservation(*action, entity.ObservationBlocked, "action was rejected by the user")
		if err := h.persistImmediateObservation(context.WithoutCancel(ctx), *action, observation); err != nil {
			return approval, nil, err
		}
		if err := h.PauseTask(context.WithoutCancel(ctx), action.TaskID, "user rejected the pending action"); err != nil {
			return approval, &observation, err
		}
		return approval, &observation, nil
	}
	if err := h.markApprovedActionDispatched(context.WithoutCancel(ctx), *action); err != nil {
		return approval, nil, err
	}
	h.mu.RLock()
	device := h.devices[action.DeviceID]
	h.mu.RUnlock()
	if device == nil {
		_ = h.PauseTask(context.WithoutCancel(ctx), action.TaskID, "approved action is waiting for the device to reconnect")
		return approval, nil, ErrDeviceOffline
	}
	observation, err := h.dispatchPersistedAction(ctx, device, *action, nil)
	if err != nil {
		_ = h.PauseTask(context.WithoutCancel(ctx), action.TaskID, "approved action could not be dispatched; device recovery will retry it")
		return approval, observation, err
	}
	if pauseErr := h.PauseTask(context.WithoutCancel(ctx), action.TaskID, "approved action finished after the original decision stream ended; resume is required for goal verification"); pauseErr != nil {
		return approval, observation, pauseErr
	}
	return approval, observation, nil
}

func (h *Hub) ensureTaskLoaded(ctx context.Context, taskID string) error {
	h.mu.RLock()
	loaded := h.sessions[taskID] != nil
	h.mu.RUnlock()
	if loaded {
		return nil
	}
	if h.store == nil {
		return fmt.Errorf("task %s is not loaded", taskID)
	}
	task, err := h.store.FindTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task %s was not found", taskID)
	}
	h.mu.Lock()
	if h.sessions[taskID] == nil {
		h.sessions[taskID] = task
	}
	h.mu.Unlock()
	return nil
}

func (h *Hub) markApprovedActionDispatched(ctx context.Context, action entity.Action) error {
	if err := h.SetTaskStatus(ctx, action.TaskID, entity.TaskStatusRunning); err != nil {
		return err
	}
	h.mu.Lock()
	current := h.sessions[action.TaskID]
	if current == nil {
		h.mu.Unlock()
		return fmt.Errorf("task %s is not loaded", action.TaskID)
	}
	if entity.CanTransitionTaskStatus(current.Status, entity.TaskStatusWaitingObservation) {
		current.Status = entity.TaskStatusWaitingObservation
	}
	for index := range current.Steps {
		if current.Steps[index].StepID == action.StepID {
			current.Steps[index].Status = entity.StepStatusWaitingObservation
			current.Steps[index].UpdatedAt = time.Now().UTC()
			break
		}
	}
	current.Revision++
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	if err := h.saveTask(ctx, &copy); err != nil {
		return err
	}
	return nil
}

func (h *Hub) Observe(ctx context.Context, observation entity.Observation) (err error) {
	if strings.TrimSpace(observation.TraceID) != "" {
		ctx = log.WithReqID(ctx, observation.TraceID)
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	safeObservation := sanitizeObservationForPersistence(observation)
	worldSpan := log.StartSpan(ctx, "world.apply",
		"task_id", observation.TaskID,
		"action_id", observation.ActionID,
		"observation_id", observation.ObservationID,
		"has_world_patch", observation.WorldPatch != nil,
	)
	if h.store != nil {
		if err := h.commitObservation(ctx, safeObservation); err != nil {
			worldSpan.End(err, "durable", true)
			return err
		}
	}
	worldSpan.End(nil, "durable", h.store != nil, "evidence_count", len(safeObservation.Evidence))
	verifySpan := log.StartSpan(ctx, "task.verify",
		"task_id", observation.TaskID,
		"action_id", observation.ActionID,
		"observation_status", observation.Status,
	)
	defer func() {
		verifySpan.End(err)
	}()
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
		if err := h.recordObservation(ctx, observation.TaskID, safeObservation); err != nil {
			return err
		}
		if observation.Status != entity.ObservationWaitingApproval &&
			observation.Status != entity.ObservationWaitingUser &&
			observation.Status != entity.ObservationCancelled {
			return h.PauseTask(ctx, observation.TaskID, "device observation recovered after the decision loop stopped; resume is required")
		}
		return nil
	}
	if pending.action.TaskID != observation.TaskID || pending.action.StepID != observation.StepID ||
		pending.action.Sequence != observation.Sequence || pending.action.Revision != observation.Revision {
		return fmt.Errorf("observation correlation mismatch")
	}
	select {
	case pending.channel <- observation:
		return nil
	default:
		return fmt.Errorf("observation for action %s is already queued", observation.ActionID)
	}
}

func (h *Hub) Progress(ctx context.Context, progress entity.Progress) error {
	if strings.TrimSpace(progress.TraceID) != "" {
		ctx = log.WithReqID(ctx, progress.TraceID)
	}
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
		return h.recordProgress(ctx, progress.TaskID, progress)
	}
	if pending.action.TaskID != progress.TaskID || pending.action.StepID != progress.StepID ||
		pending.action.Sequence != progress.Sequence || pending.action.Revision != progress.Revision {
		return fmt.Errorf("progress correlation mismatch")
	}
	if err := h.recordProgress(ctx, progress.TaskID, progress); err != nil {
		return err
	}
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
	if !entity.CanTransitionTaskStatus(current.Status, status) {
		h.mu.Unlock()
		return nil
	}
	if current.Status == status {
		h.mu.Unlock()
		return nil
	}
	current.Status = status
	for index := range current.Steps {
		if current.Steps[index].StepID != current.CurrentStepID {
			continue
		}
		switch status {
		case entity.StatusCompleted:
			current.Steps[index].Status = entity.StepStatusCompleted
		case entity.StatusFailed:
			current.Steps[index].Status = entity.StepStatusFailed
		case entity.StatusCancelled:
			current.Steps[index].Status = entity.StepStatusCancelled
		case entity.TaskStatusPaused:
			current.Steps[index].Status = entity.StepStatusPaused
		}
		current.Steps[index].UpdatedAt = time.Now().UTC()
		break
	}
	current.Revision++
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	if err := h.saveTask(ctx, &copy); err != nil {
		return err
	}
	if entity.TerminalTaskStatus(status) {
		h.notifyTaskTerminal(context.WithoutCancel(ctx), taskID)
	}
	return nil
}

// PauseTask records why execution cannot continue without pretending that an
// Action outcome completed the user's Goal.
func (h *Hub) PauseTask(ctx context.Context, taskID, reason string) error {
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
	if entity.TerminalTaskStatus(current.Status) || current.Status == entity.TaskStatusPaused {
		h.mu.Unlock()
		return nil
	}
	if !entity.CanTransitionTaskStatus(current.Status, entity.TaskStatusPaused) {
		h.mu.Unlock()
		return fmt.Errorf("task %s cannot pause from %s", taskID, current.Status)
	}
	if current.Metadata == nil {
		current.Metadata = make(map[string]interface{})
	}
	current.Metadata["pause_reason"] = reason
	current.Metadata["paused_at"] = time.Now().UTC()
	current.Status = entity.TaskStatusPaused
	for index := range current.Steps {
		if current.Steps[index].StepID == current.CurrentStepID &&
			current.Steps[index].Status != entity.StepStatusCompleted &&
			current.Steps[index].Status != entity.StepStatusFailed &&
			current.Steps[index].Status != entity.StepStatusCancelled {
			current.Steps[index].Status = entity.StepStatusPaused
			current.Steps[index].UpdatedAt = time.Now().UTC()
		}
	}
	current.Revision++
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	return h.saveTask(context.WithoutCancel(ctx), &copy)
}

func (h *Hub) Dispatch(ctx context.Context, deviceID string, action entity.Action, progressHandlers ...func(entity.Progress) error) (observation *entity.Observation, err error) {
	action.Normalize()
	action.DeviceID = deviceID
	if strings.TrimSpace(action.TraceID) != "" {
		ctx = log.WithReqID(ctx, action.TraceID)
	}
	policySpan := log.StartSpan(ctx, "action.policy",
		"task_id", action.TaskID,
		"action_id", action.ActionID,
		"capability", action.Capability,
		"risk", action.Policy.Risk,
		"decision", action.Policy.Decision,
	)
	if validateErr := action.Validate(); validateErr != nil {
		policySpan.End(validateErr)
		return nil, validateErr
	}
	policySpan.End(nil, "constraint_count", len(action.Policy.Constraints))
	dispatchSpan := log.StartSpan(ctx, "action.dispatch",
		"task_id", action.TaskID,
		"action_id", action.ActionID,
		"device_id", deviceID,
		"capability", action.Capability,
	)
	defer func() {
		status := ""
		if observation != nil {
			status = observation.Status
		}
		dispatchSpan.End(err, "observation_status", status)
	}()
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
	taskRevision, taskSequence := int64(0), int64(0)
	if task := h.sessions[action.TaskID]; task != nil {
		taskRevision, taskSequence = task.Revision, task.Sequence
	}
	h.mu.RUnlock()
	if device == nil {
		return nil, ErrDeviceOffline
	}
	action.LeaseOwner = device.LeaseOwner
	action.FencingToken = device.FencingToken
	if err := action.Validate(); err != nil {
		return nil, err
	}
	if err := h.ensureDeviceLease(ctx, device); err != nil {
		return nil, err
	}
	if taskRevision > 0 {
		if action.Revision != taskRevision {
			return nil, fmt.Errorf("stale action revision: got %d, current task revision is %d", action.Revision, taskRevision)
		}
		if action.Sequence != taskSequence+1 {
			return nil, fmt.Errorf("action sequence is out of order: got %d, want %d", action.Sequence, taskSequence+1)
		}
	}
	instanceID, supported := resolveCapabilityInstance(device, action.Capability, action.CapabilityInstanceID)
	if !supported {
		return nil, fmt.Errorf("device %s does not support capability %s", deviceID, action.Capability)
	}
	action.CapabilityInstanceID = instanceID
	userID := h.taskUserID(ctx, action.TaskID)
	if device.UserID != "" && device.UserID != userID {
		return nil, fmt.Errorf("device %s belongs to another user", deviceID)
	}
	if action.Policy.Decision == entity.AskUser && action.Policy.ApprovalID == "" {
		action.Policy.ApprovalID = entity.NewID("approval")
	}
	if action.Policy.Decision == entity.AskUser && h.store == nil {
		return nil, fmt.Errorf("durable approval store is required for ASK_USER actions")
	}
	if h.store != nil {
		if err := h.store.SaveAction(ctx, deviceID, userID, action); err != nil {
			return nil, err
		}
		if action.Policy.Decision == entity.AskUser {
			now := time.Now().UTC()
			approval := entity.Approval{
				ApprovalID: action.Policy.ApprovalID, TaskID: action.TaskID, StepID: action.StepID,
				ActionID: action.ActionID, OwnerID: userID, Risk: action.Policy.Risk,
				Status: entity.ApprovalPending, Summary: approvalSummary(action), Scope: approvalScope(action),
				Revision: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(approvalTTL(action.Policy)),
			}
			if task, ok, _ := h.Task(ctx, action.TaskID); ok {
				approval.TraceID = task.TraceID
			}
			if err := h.store.CreateApproval(ctx, approval); err != nil {
				return nil, err
			}
		}
	}
	if err := h.recordAction(ctx, deviceID, action); err != nil {
		return nil, err
	}
	if action.Policy.Decision == entity.AskUser {
		return waitingApprovalObservation(action, time.Now().UTC().Add(approvalTTL(action.Policy))), nil
	}
	if action.Policy.Decision == entity.Block {
		observation := policyObservation(action, entity.ObservationBlocked, "action is blocked by policy")
		if err := h.persistImmediateObservation(context.WithoutCancel(ctx), action, observation); err != nil {
			return nil, err
		}
		return &observation, nil
	}
	var onProgress func(entity.Progress) error
	if len(progressHandlers) > 0 {
		onProgress = progressHandlers[0]
	}
	return h.dispatchPersistedAction(ctx, device, action, onProgress)
}

func (h *Hub) dispatchPersistedAction(ctx context.Context, device *Device, action entity.Action, onProgress func(entity.Progress) error) (*entity.Observation, error) {
	channel := make(chan entity.Observation, 1)
	h.mu.Lock()
	if _, exists := h.pending[action.ActionID]; exists {
		h.mu.Unlock()
		return nil, fmt.Errorf("action %s is already pending", action.ActionID)
	}
	h.pending[action.ActionID] = &pendingAction{channel: channel, deviceID: device.ID, action: action, onProgress: onProgress}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, action.ActionID)
		h.mu.Unlock()
	}()
	if err := h.ensureDeviceLease(ctx, device); err != nil {
		_ = h.SetTaskStatus(context.WithoutCancel(ctx), action.TaskID, entity.TaskStatusPaused)
		return nil, err
	}
	if err := device.conn.Send(action); err != nil {
		_ = h.SetTaskStatus(context.WithoutCancel(ctx), action.TaskID, entity.TaskStatusPaused)
		return nil, fmt.Errorf("%w: send action to %s: %v", ErrDeviceOffline, device.ID, err)
	}
	timer := time.NewTimer(time.Until(action.Deadline))
	defer timer.Stop()
	select {
	case observation := <-channel:
		if observation.TaskID != action.TaskID || observation.StepID != action.StepID ||
			observation.Sequence != action.Sequence || observation.Revision != action.Revision {
			return nil, fmt.Errorf("observation correlation mismatch")
		}
		h.mu.Lock()
		if len(h.completed) >= completedObservationLimit {
			h.completed = make(map[string]entity.Observation)
		}
		safeObservation := sanitizeObservationForPersistence(observation)
		h.completed[action.IdempotencyKey] = safeObservation
		h.recordObservationLocked(action.TaskID, safeObservation)
		copy := cloneTask(h.sessions[action.TaskID])
		h.mu.Unlock()
		if err := h.saveTask(context.WithoutCancel(ctx), &copy); err != nil {
			return nil, err
		}
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

func approvalTTL(policy entity.Policy) time.Duration {
	if raw, ok := policy.Constraints["approval_timeout_ms"]; ok {
		var milliseconds int64
		switch value := raw.(type) {
		case int:
			milliseconds = int64(value)
		case int64:
			milliseconds = value
		case float64:
			milliseconds = int64(value)
		}
		if milliseconds >= int64(time.Minute/time.Millisecond) && milliseconds <= int64((24*time.Hour)/time.Millisecond) {
			return time.Duration(milliseconds) * time.Millisecond
		}
	}
	return defaultApprovalTTL
}

func approvalSummary(action entity.Action) string {
	if reason := strings.TrimSpace(action.Policy.Reason); reason != "" {
		return reason
	}
	operation := strings.TrimSpace(action.Operation)
	if operation == "" {
		operation = action.Capability
	}
	return fmt.Sprintf("Allow %s on device %s", operation, shortDeviceID(action.DeviceID))
}

func approvalScope(action entity.Action) map[string]any {
	encodedArguments, _ := json.Marshal(action.Arguments)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(encodedArguments))
	return map[string]any{
		"device_id": action.DeviceID, "capability_instance_id": action.CapabilityInstanceID,
		"capability": action.Capability, "operation": action.Operation,
		"target": safeApprovalTarget(action.Target), "arguments_digest": digest,
		"allowed_attempts": approvalAllowedAttempts(action.Policy), "idempotency_key": action.IdempotencyKey,
	}
}

func approvalAllowedAttempts(policy entity.Policy) int {
	if raw, ok := policy.Constraints["allowed_attempts"]; ok {
		switch value := raw.(type) {
		case int:
			if value > 0 && value <= 10 {
				return value
			}
		case float64:
			if value >= 1 && value <= 10 {
				return int(value)
			}
		}
	}
	return 1
}

func safeApprovalTarget(target map[string]any) map[string]any {
	redacted, _ := redactSensitiveValue(target).(map[string]any)
	return redacted
}

func sanitizeObservationForPersistence(observation entity.Observation) entity.Observation {
	safe := observation.WithoutAttachmentData()
	if state, ok := redactSensitiveValue(safe.State).(map[string]any); ok {
		safe.State = state
	}
	if safe.WorldPatch != nil {
		patch := *safe.WorldPatch
		patch.Mutations = append([]entity.WorldMutation(nil), patch.Mutations...)
		for index := range patch.Mutations {
			patch.Mutations[index].Value = redactSensitiveValue(patch.Mutations[index].Value)
		}
		safe.WorldPatch = &patch
	}
	safe.Evidence = append([]entity.EvidenceRef(nil), safe.Evidence...)
	for index := range safe.Evidence {
		safe.Evidence[index].URI = redactSensitiveString(safe.Evidence[index].URI)
		safe.Evidence[index].Summary = redactSensitiveString(safe.Evidence[index].Summary)
		if metadata, ok := redactSensitiveValue(safe.Evidence[index].Metadata).(map[string]any); ok {
			safe.Evidence[index].Metadata = metadata
		}
	}
	safe.Summary = redactSensitiveString(safe.Summary)
	safe.Error = redactSensitiveString(safe.Error)
	safe.ErrorDetail = redactErrorDetail(safe.ErrorDetail)
	return normalizeObservationContent(safe)
}

func normalizeObservationContent(observation entity.Observation) entity.Observation {
	encoded, err := json.Marshal(observation)
	if err != nil {
		return observation
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return observation
	}
	normalized, report := safety.NormalizeValue(generic, 32*1024)
	encoded, err = json.Marshal(normalized)
	if err != nil || json.Unmarshal(encoded, &observation) != nil {
		return observation
	}
	if observation.State == nil {
		observation.State = make(map[string]any)
	}
	observation.State["_athena_content_security"] = map[string]any{
		"schema": safety.EnvelopeSchema, "trust": safety.TrustExternal,
		"policy": safety.PolicyDataOnly, "risk": report.Risk,
		"indicators": report.Indicators, "sha256": report.SHA256,
		"truncated": report.Truncated,
	}
	return observation
}

func redactErrorDetail(detail *entity.ErrorDetail) *entity.ErrorDetail {
	if detail == nil {
		return nil
	}
	copy := *detail
	copy.Message = redactSensitiveString(copy.Message)
	if values, ok := redactSensitiveValue(copy.Details).(map[string]any); ok {
		copy.Details = values
	}
	copy.Cause = redactErrorDetail(copy.Cause)
	return &copy
}

func redactSensitiveValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			if sensitiveFieldName(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = redactSensitiveValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = redactSensitiveValue(typed[index])
		}
		return result
	case []string:
		result := make([]string, len(typed))
		for index := range typed {
			result[index] = redactSensitiveString(typed[index])
		}
		return result
	case string:
		return redactSensitiveString(typed)
	default:
		return value
	}
}

func sensitiveFieldName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(normalized, "password") || strings.Contains(normalized, "passwd") ||
		strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "api_key") || strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "cookie") || strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "authorization")
}

func redactSensitiveString(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		query := parsed.Query()
		changed := false
		for key := range query {
			if sensitiveFieldName(key) {
				query.Set(key, "[REDACTED]")
				changed = true
			}
		}
		if parsed.User != nil {
			if _, hasPassword := parsed.User.Password(); hasPassword {
				parsed.User = url.User(parsed.User.Username())
				changed = true
			}
		}
		if changed {
			parsed.RawQuery = query.Encode()
			value = parsed.String()
		}
	}
	return inlineSecretPattern.ReplaceAllString(value, "${1}[REDACTED]")
}

func waitingApprovalObservation(action entity.Action, expiresAt time.Time) *entity.Observation {
	now := time.Now().UTC()
	return &entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, ObservationID: entity.NewID("observation"),
		TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID, TraceID: action.TraceID,
		AgentBuildID: action.AgentBuildID, RunManifestID: action.RunManifestID, DeviceID: action.DeviceID,
		SessionID: action.SessionID, Sequence: action.Sequence, Revision: action.Revision,
		Status: entity.ObservationWaitingApproval, ObservedAt: now,
		Summary: "Waiting for explicit user approval before dispatching the action.",
		State:   map[string]any{"approval_id": action.Policy.ApprovalID, "risk": action.Policy.Risk, "expires_at": expiresAt},
	}
}

func policyObservation(action entity.Action, status, message string) entity.Observation {
	now := time.Now().UTC()
	return entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, ObservationID: entity.NewID("observation"),
		TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID, TraceID: action.TraceID,
		AgentBuildID: action.AgentBuildID, RunManifestID: action.RunManifestID, DeviceID: action.DeviceID,
		SessionID: action.SessionID, Sequence: action.Sequence, Revision: action.Revision,
		Status: status, FinishedAt: now, ObservedAt: now, Summary: message, Error: message,
		ErrorDetail: &entity.ErrorDetail{Code: "ACTION_" + status, Message: message, Operation: "action.policy"},
	}
}

func (h *Hub) persistImmediateObservation(ctx context.Context, action entity.Action, observation entity.Observation) error {
	if h.store != nil {
		if err := h.commitObservation(ctx, observation); err != nil {
			return err
		}
	}
	h.mu.Lock()
	if len(h.completed) >= completedObservationLimit {
		h.completed = make(map[string]entity.Observation)
	}
	h.completed[action.IdempotencyKey] = observation
	h.recordObservationLocked(action.TaskID, observation)
	copy := cloneTask(h.sessions[action.TaskID])
	h.mu.Unlock()
	if copy.TaskID != "" {
		return h.saveTask(ctx, &copy)
	}
	return nil
}

func (h *Hub) persistTerminalObservation(ctx context.Context, action entity.Action, status, message string) {
	observation := entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, ObservationID: entity.NewID("observation"),
		TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID, TraceID: action.TraceID,
		AgentBuildID: action.AgentBuildID, RunManifestID: action.RunManifestID, DeviceID: action.DeviceID,
		SessionID: action.SessionID, Sequence: action.Sequence, Revision: action.Revision,
		Status: status, FinishedAt: time.Now().UTC(), ObservedAt: time.Now().UTC(), Error: message,
		ErrorDetail: &entity.ErrorDetail{Code: "ACTION_" + status, Message: message},
	}
	h.mu.Lock()
	h.completed[action.IdempotencyKey] = observation
	h.mu.Unlock()
	if h.store != nil {
		if err := h.commitObservation(ctx, observation); err != nil {
			log.Warnw(ctx, "persist terminal control observation failed", "task_id", action.TaskID, "action_id", action.ActionID, "error_chain", log.FormatError(err))
			return
		}
		h.mu.Lock()
		if h.sessions[action.TaskID] == nil {
			if stored, _ := h.store.FindTask(ctx, action.TaskID); stored != nil {
				h.sessions[action.TaskID] = stored
			}
		}
		h.mu.Unlock()
	}
	if err := h.recordObservation(ctx, action.TaskID, observation); err != nil {
		log.Warnw(ctx, "project terminal control observation failed", "task_id", action.TaskID, "action_id", action.ActionID, "error_chain", log.FormatError(err))
	}
	taskStatus := entity.StatusFailed
	if status == entity.ObservationCancelled {
		taskStatus = entity.StatusCancelled
	}
	_ = h.SetTaskStatus(ctx, action.TaskID, taskStatus)
}

func (h *Hub) commitObservation(ctx context.Context, observation entity.Observation) error {
	if h.world != nil {
		return h.world.CommitObservation(ctx, observation)
	}
	if h.store == nil {
		return nil
	}
	return h.store.SaveObservation(ctx, observation)
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
		if err := h.SetTaskStatus(ctx, task.TaskID, entity.StatusCancelled); err != nil {
			return err
		}
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

func (h *Hub) CancelTask(ctx context.Context, taskID, reason string) error {
	task, ok, err := h.Task(ctx, taskID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("task %s was not found", taskID)
	}
	if entity.TerminalTaskStatus(task.Status) {
		return nil
	}
	if err := h.SetTaskStatus(ctx, taskID, entity.StatusCancelled); err != nil {
		return err
	}
	h.mu.RLock()
	pending := make([]pendingAction, 0)
	for _, current := range h.pending {
		if current.action.TaskID == taskID {
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

func (h *Hub) recordAction(ctx context.Context, deviceID string, action entity.Action) error {
	h.mu.Lock()
	current := h.sessions[action.TaskID]
	if current == nil {
		now := time.Now().UTC()
		current = &entity.TaskSession{TaskID: action.TaskID, DeviceID: deviceID, Status: entity.TaskStatusCreated, Revision: 1, ActiveSessions: make(map[string]string), CreatedAt: now}
		h.sessions[action.TaskID] = current
	}
	current.DeviceID = deviceID
	nextTaskStatus := entity.TaskStatusWaitingObservation
	nextStepStatus := entity.StepStatusWaitingObservation
	if action.Policy.Decision == entity.AskUser {
		nextTaskStatus = entity.TaskStatusWaitingApproval
		nextStepStatus = entity.StepStatusWaitingApproval
	}
	if entity.CanTransitionTaskStatus(current.Status, nextTaskStatus) {
		current.Status = nextTaskStatus
	}
	current.Sequence = action.Sequence
	current.Revision++
	current.CurrentStepID = action.StepID
	stepFound := false
	for index := range current.Steps {
		if current.Steps[index].StepID == action.StepID {
			current.Steps[index].Status = nextStepStatus
			current.Steps[index].UpdatedAt = time.Now().UTC()
			stepFound = true
			break
		}
	}
	if !stepFound {
		now := time.Now().UTC()
		current.Steps = append(current.Steps, entity.TaskStep{
			StepID: action.StepID, TaskID: action.TaskID, Ordinal: len(current.Steps) + 1,
			Status: nextStepStatus, Capability: action.Capability,
			Operation: action.Operation, Target: action.Target, Input: action.Arguments,
			ExpectedObservation: action.ExpectedObservation, CreatedAt: now, UpdatedAt: now,
		})
	}
	current.Actions = append(current.Actions, action)
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	return h.saveTask(context.WithoutCancel(ctx), &copy)
}

func (h *Hub) recordObservation(ctx context.Context, taskID string, observation entity.Observation) error {
	h.mu.Lock()
	h.recordObservationLocked(taskID, observation)
	current := h.sessions[taskID]
	var copy entity.TaskSession
	if current != nil {
		copy = cloneTask(current)
	}
	h.mu.Unlock()
	if copy.TaskID != "" {
		return h.saveTask(context.WithoutCancel(ctx), &copy)
	}
	return nil
}

func (h *Hub) recordProgress(ctx context.Context, taskID string, progress entity.Progress) error {
	h.mu.Lock()
	current := h.sessions[taskID]
	if current == nil {
		h.mu.Unlock()
		return nil
	}
	if current.Metadata == nil {
		current.Metadata = make(map[string]interface{})
	}
	current.Metadata["latest_progress"] = progress
	if entity.CanTransitionTaskStatus(current.Status, entity.StatusExecuting) {
		current.Status = entity.StatusExecuting
	}
	current.Revision++
	for index := range current.Steps {
		if current.Steps[index].StepID == progress.StepID {
			current.Steps[index].Status = entity.StepStatusRunning
			current.Steps[index].UpdatedAt = time.Now().UTC()
			break
		}
	}
	current.UpdatedAt = time.Now().UTC()
	copy := cloneTask(current)
	h.mu.Unlock()
	if store, ok := h.store.(interface {
		SaveProgress(context.Context, entity.Progress) error
	}); ok {
		if err := store.SaveProgress(ctx, progress); err != nil {
			return err
		}
	}
	return h.saveTask(context.WithoutCancel(ctx), &copy)
}

func (h *Hub) recordObservationLocked(taskID string, observation entity.Observation) {
	current := h.sessions[taskID]
	if current == nil {
		return
	}
	current.Observations = append(current.Observations, observation)
	if !entity.TerminalTaskStatus(current.Status) {
		current.Status = entity.StatusEvaluating
		if observation.Status == entity.ObservationWaitingApproval {
			current.Status = entity.StatusWaitingApproval
		} else if observation.Status == entity.ObservationWaitingUser {
			current.Status = entity.StatusWaitingUser
		} else if observation.Status == entity.ObservationCancelled {
			current.Status = entity.StatusCancelled
		}
	}
	current.Revision++
	for index := range current.Steps {
		if current.Steps[index].StepID != observation.StepID {
			continue
		}
		switch observation.Status {
		case entity.ObservationSucceeded:
			current.Steps[index].Status = entity.StepStatusVerifying
		case entity.ObservationWaitingApproval:
			current.Steps[index].Status = entity.StepStatusWaitingApproval
		case entity.ObservationWaitingUser:
			current.Steps[index].Status = entity.StepStatusWaitingUser
		case entity.ObservationCancelled:
			current.Steps[index].Status = entity.StepStatusCancelled
		default:
			current.Steps[index].Status = entity.StepStatusFailed
		}
		current.Steps[index].UpdatedAt = time.Now().UTC()
		break
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

func normalizeCapabilityInstances(deviceID string, capabilities []string, instances []entity.CapabilityInstance) []entity.CapabilityInstance {
	result := make([]entity.CapabilityInstance, 0, len(instances)+len(capabilities))
	seen := make(map[string]bool, len(instances)+len(capabilities))
	for _, instance := range instances {
		if strings.TrimSpace(instance.InstanceID) == "" || strings.TrimSpace(instance.Capability) == "" || seen[instance.InstanceID] {
			continue
		}
		seen[instance.InstanceID] = true
		result = append(result, instance)
	}
	for _, capability := range capabilities {
		found := false
		for _, instance := range result {
			if instance.Capability == capability {
				found = true
				break
			}
		}
		if found {
			continue
		}
		instanceID := deviceID + ":" + strings.ReplaceAll(capability, ".", "-")
		if !seen[instanceID] {
			seen[instanceID] = true
			result = append(result, entity.CapabilityInstance{InstanceID: instanceID, Capability: capability})
		}
	}
	return result
}

func resolveCapabilityInstance(device *Device, capability, requested string) (string, bool) {
	if device == nil {
		return "", false
	}
	matched := ""
	for _, instance := range device.CapabilityInstances {
		if instance.Capability != capability || (requested != "" && instance.InstanceID != requested) {
			continue
		}
		if requested == "" && matched != "" && matched != instance.InstanceID {
			return "", false
		}
		matched = instance.InstanceID
	}
	if matched != "" {
		return matched, true
	}
	if requested == "" && supportsCapability(device.Capabilities, capability) {
		return device.ID + ":" + strings.ReplaceAll(capability, ".", "-"), true
	}
	return "", false
}

func cloneTask(current *entity.TaskSession) entity.TaskSession {
	if current == nil {
		return entity.TaskSession{}
	}
	copy := *current
	copy.Actions = append([]entity.Action(nil), current.Actions...)
	copy.Observations = append([]entity.Observation(nil), current.Observations...)
	copy.Steps = append([]entity.TaskStep(nil), current.Steps...)
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
