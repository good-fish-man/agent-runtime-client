package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	irepository "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	log "github.com/good-fish-man/logx"
)

const deviceLeaseDuration = 45 * time.Second

var ErrDeviceLeaseOwned = errors.New("device lease is owned by another control-plane instance")

type Store struct{ data *data.Data }

func NewStore(data *data.Data) *Store { return &Store{data: data} }

var _ irepository.Store = (*Store)(nil)

func (s *Store) UpsertDevice(ctx context.Context, device *entity.RegisteredDevice) error {
	capabilities, err := encodeJSON(device.Capabilities)
	if err != nil {
		return err
	}
	instances, err := encodeJSON(device.CapabilityInstances)
	if err != nil {
		return err
	}
	seenAt := device.LastSeenAt
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}
	value := &po.Device{
		DeviceID: device.DeviceID, UserID: device.UserID, Protocol: entity.Protocol,
		Name: device.Name, Platform: device.Platform, Architecture: device.Architecture,
		Capabilities: capabilities, CapabilityInstances: instances, Online: device.Online, Revision: 1,
		LeaseOwner: device.LeaseOwner, FencingToken: device.FencingToken,
		ConnectedAt: millis(device.ConnectedAt), LastSeenAt: millis(seenAt), LeaseExpiresAt: millis(device.LeaseExpiresAt),
	}
	if value.LeaseExpiresAt == 0 {
		value.LeaseExpiresAt = millis(seenAt.Add(deviceLeaseDuration))
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"protocol": value.Protocol, "name": value.Name, "platform": value.Platform,
				"architecture": value.Architecture, "capabilities": value.Capabilities,
				"capability_instances": value.CapabilityInstances, "online": value.Online,
				"connected_at": value.ConnectedAt, "last_seen_at": value.LastSeenAt,
				"lease_owner": value.LeaseOwner, "fencing_token": value.FencingToken,
				"lease_expires_at": value.LeaseExpiresAt, "revision": revisionIncrement("os_device"),
			}),
		}).Create(value).Error; err != nil {
			return err
		}
		return syncCapabilityInventory(tx, device, seenAt)
	}), "ControlStore.UpsertDevice")
}

// AcquireDeviceLease atomically gives one control-plane instance exclusive
// ownership of a device. Every accepted connection receives a new fencing
// token so delayed heartbeats from an older socket cannot mutate the lease.
func (s *Store) AcquireDeviceLease(ctx context.Context, device *entity.RegisteredDevice, owner string, seenAt time.Time) (*entity.RegisteredDevice, error) {
	if device == nil || strings.TrimSpace(device.DeviceID) == "" || strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("device and lease owner are required")
	}
	capabilities, err := encodeJSON(device.Capabilities)
	if err != nil {
		return nil, err
	}
	instances, err := encodeJSON(device.CapabilityInstances)
	if err != nil {
		return nil, err
	}
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}
	expiresAt := seenAt.Add(deviceLeaseDuration)
	var acquired po.Device
	err = s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		seed := &po.Device{
			DeviceID: device.DeviceID, UserID: device.UserID, Protocol: entity.Protocol,
			Name: device.Name, Platform: device.Platform, Architecture: device.Architecture,
			Capabilities: capabilities, CapabilityInstances: instances, Online: false,
			ConnectedAt: millis(seenAt), LastSeenAt: millis(seenAt), LeaseExpiresAt: 0,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(seed).Error; err != nil {
			return err
		}
		result := tx.Model(&po.Device{}).
			Where("device_id = ? AND (lease_owner = '' OR lease_owner = ? OR lease_expires_at <= ?)", device.DeviceID, owner, millis(seenAt)).
			Updates(map[string]any{
				"protocol": entity.Protocol, "name": device.Name, "platform": device.Platform,
				"architecture": device.Architecture, "capabilities": capabilities,
				"capability_instances": instances, "online": true, "lease_owner": owner,
				"fencing_token": gorm.Expr("fencing_token + 1"), "connected_at": millis(seenAt),
				"last_seen_at": millis(seenAt), "lease_expires_at": millis(expiresAt),
				"revision": gorm.Expr("revision + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeviceLeaseOwned
		}
		if err := tx.Where("device_id = ?", device.DeviceID).Limit(1).Take(&acquired).Error; err != nil {
			return err
		}
		leased := *device
		leased.LeaseOwner = owner
		leased.FencingToken = acquired.FencingToken
		leased.LeaseExpiresAt = expiresAt
		leased.Online = true
		leased.ConnectedAt = seenAt
		leased.LastSeenAt = seenAt
		return syncCapabilityInventory(tx, &leased, seenAt)
	})
	if err != nil {
		return nil, log.WrapError(err, "ControlStore.AcquireDeviceLease")
	}
	return deviceToEntity(&acquired), nil
}

func (s *Store) RenewDeviceLease(ctx context.Context, deviceID, owner string, token uint64, seenAt time.Time) error {
	result := s.data.DB(ctx).Model(&po.Device{}).
		Where("device_id = ? AND lease_owner = ? AND fencing_token = ?", deviceID, owner, token).
		Updates(map[string]any{"online": true, "last_seen_at": millis(seenAt), "lease_expires_at": millis(seenAt.Add(deviceLeaseDuration)), "revision": gorm.Expr("revision + 1")})
	if result.Error != nil {
		return log.WrapError(result.Error, "ControlStore.RenewDeviceLease")
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("renew device %s lease: fencing token is stale", deviceID)
	}
	return nil
}

func (s *Store) ValidateDeviceLease(ctx context.Context, deviceID, owner string, token uint64, now time.Time) error {
	if strings.TrimSpace(deviceID) == "" || strings.TrimSpace(owner) == "" || token == 0 {
		return fmt.Errorf("device lease identity and fencing token are required")
	}
	var count int64
	err := s.data.DB(ctx).Model(&po.Device{}).
		Where("device_id = ? AND online = ? AND lease_owner = ? AND fencing_token = ? AND lease_expires_at > ?", deviceID, true, owner, token, millis(now)).
		Count(&count).Error
	if err != nil {
		return log.WrapError(err, "ControlStore.ValidateDeviceLease")
	}
	if count != 1 {
		return fmt.Errorf("validate device %s lease: fencing token is stale or expired", deviceID)
	}
	return nil
}

func (s *Store) ReleaseDeviceLease(ctx context.Context, deviceID, owner string, token uint64, seenAt time.Time) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&po.Device{}).
			Where("device_id = ? AND lease_owner = ? AND fencing_token = ?", deviceID, owner, token).
			Updates(map[string]any{"online": false, "lease_owner": "", "lease_expires_at": millis(seenAt), "last_seen_at": millis(seenAt), "revision": gorm.Expr("revision + 1")})
		if result.Error != nil || result.RowsAffected == 0 {
			if result.Error != nil {
				return result.Error
			}
			return fmt.Errorf("release device %s lease: fencing token is stale", deviceID)
		}
		return tx.Model(&po.CapabilityInstance{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"online": false, "revision": gorm.Expr("revision + 1"), "updated_at": millis(seenAt)}).Error
	}), "ControlStore.ReleaseDeviceLease")
}

func (s *Store) BindDevice(ctx context.Context, deviceID, userID string) error {
	deviceID = strings.TrimSpace(deviceID)
	userID = strings.TrimSpace(userID)
	if deviceID == "" || userID == "" {
		return fmt.Errorf("device_id and authenticated user are required")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var device po.Device
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("device_id = ?", deviceID).Limit(1).Find(&device)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return irepository.ErrDeviceNotFound
		}
		if !device.Online || device.LeaseExpiresAt <= millis(time.Now().UTC()) {
			return irepository.ErrDeviceOffline
		}
		if device.UserID != "" && device.UserID != userID {
			return irepository.ErrDeviceOwnerMismatch
		}
		if err := tx.Model(&po.Device{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"user_id": userID, "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&po.CapabilityInstance{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"owner_id": userID, "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
		return tx.Model(&po.DeviceCapability{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"owner_id": userID, "revision": gorm.Expr("revision + 1")}).Error
	}), "ControlStore.BindDevice")
}

// UnbindDevice releases an owned device only when no unfinished task can still
// dispatch work to it. Device and capability ownership are changed atomically.
func (s *Store) UnbindDevice(ctx context.Context, deviceID, userID string) error {
	deviceID = strings.TrimSpace(deviceID)
	userID = strings.TrimSpace(userID)
	if deviceID == "" || userID == "" {
		return fmt.Errorf("device_id and authenticated user are required")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var device po.Device
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("device_id = ?", deviceID).Limit(1).Find(&device)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return irepository.ErrDeviceNotFound
		}
		if device.UserID == "" {
			return nil
		}
		if device.UserID != userID {
			return irepository.ErrDeviceOwnerMismatch
		}

		var unfinished int64
		if err := tx.Model(&po.Task{}).
			Where("device_id = ? AND user_id = ? AND status NOT IN ?", deviceID, userID, []string{
				entity.TaskStatusCompleted, entity.TaskStatusFailed, entity.TaskStatusCancelled,
			}).Count(&unfinished).Error; err != nil {
			return err
		}
		if unfinished > 0 {
			return fmt.Errorf("%w: %d task(s) must be completed or cancelled before unbinding", irepository.ErrDeviceHasActiveTasks, unfinished)
		}

		now := time.Now().UTC()
		if err := tx.Model(&po.Device{}).
			Where("device_id = ? AND user_id = ?", deviceID, userID).
			Updates(map[string]any{
				"user_id": "", "online": false, "lease_owner": "", "lease_expires_at": millis(now),
				"fencing_token": gorm.Expr("fencing_token + 1"), "revision": gorm.Expr("revision + 1"), "updated_at": millis(now),
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&po.CapabilityInstance{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"owner_id": "", "online": false, "revision": gorm.Expr("revision + 1"), "updated_at": millis(now)}).Error; err != nil {
			return err
		}
		return tx.Model(&po.DeviceCapability{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"owner_id": "", "revision": gorm.Expr("revision + 1"), "updated_at": millis(now)}).Error
	}), "ControlStore.UnbindDevice")
}

func (s *Store) SetDeviceOnline(ctx context.Context, deviceID string, online bool, seenAt time.Time) error {
	values := map[string]any{
		"online": online, "last_seen_at": millis(seenAt), "revision": gorm.Expr("revision + 1"),
	}
	if online {
		values["lease_expires_at"] = millis(seenAt.Add(deviceLeaseDuration))
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&po.Device{}).Where("device_id = ?", deviceID).Updates(values).Error; err != nil {
			return err
		}
		return tx.Model(&po.CapabilityInstance{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"online": online, "revision": gorm.Expr("revision + 1"), "updated_at": millis(seenAt)}).Error
	}), "ControlStore.SetDeviceOnline")
}

func (s *Store) MarkAllDevicesOffline(ctx context.Context, seenAt time.Time) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		expired := tx.Model(&po.Device{}).Select("device_id").Where("online = ? AND lease_expires_at < ?", true, millis(seenAt))
		if err := tx.Model(&po.CapabilityInstance{}).Where("online = ? AND device_id IN (?)", true, expired).
			Updates(map[string]any{"online": false, "revision": gorm.Expr("revision + 1"), "updated_at": millis(seenAt)}).Error; err != nil {
			return err
		}
		return tx.Model(&po.Device{}).Where("online = ? AND lease_expires_at < ?", true, millis(seenAt)).
			Updates(map[string]any{"online": false, "lease_owner": "", "lease_expires_at": millis(seenAt), "last_seen_at": millis(seenAt), "revision": gorm.Expr("revision + 1")}).Error
	}), "ControlStore.MarkAllDevicesOffline")
}

func syncCapabilityInventory(tx *gorm.DB, device *entity.RegisteredDevice, seenAt time.Time) error {
	if err := tx.Model(&po.CapabilityInstance{}).Where("device_id = ?", device.DeviceID).
		Updates(map[string]any{"online": false, "updated_at": millis(seenAt)}).Error; err != nil {
		return err
	}
	for _, instance := range device.CapabilityInstances {
		operations, err := encodeJSON(instance.Operations)
		if err != nil {
			return err
		}
		modalities, err := encodeJSON(instance.Modalities)
		if err != nil {
			return err
		}
		metadata, err := encodeJSON(instance.Metadata)
		if err != nil {
			return err
		}
		worldContract, err := encodeJSON(instance.World)
		if err != nil {
			return err
		}
		definition := po.CapabilityDefinition{
			CapabilityID: instance.Capability, OwnerID: "system", Version: instance.Version,
			Operations: operations, Modalities: modalities, InputSchema: "{}", OutputSchema: "{}",
			Risk: minimumCapabilityRisk(instance.Capability, instance.Operations), WorldContract: worldContract, Enabled: true, Revision: 1, CreatedAt: millis(seenAt), UpdatedAt: millis(seenAt),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "capability_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"version", "operations", "modalities", "risk", "world_contract", "enabled", "updated_at"}),
		}).Create(&definition).Error; err != nil {
			return err
		}
		value := po.CapabilityInstance{
			InstanceID: instance.InstanceID, CapabilityID: instance.Capability, DeviceID: device.DeviceID,
			OwnerID: device.UserID, Version: instance.Version, Operations: operations, Modalities: modalities,
			Metadata: metadata, WorldContract: worldContract, Online: device.Online, Revision: 1, CreatedAt: millis(seenAt), UpdatedAt: millis(seenAt),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"capability_id": value.CapabilityID, "device_id": value.DeviceID, "owner_id": value.OwnerID,
				"version": value.Version, "operations": value.Operations, "modalities": value.Modalities,
				"metadata": value.Metadata, "world_contract": value.WorldContract, "online": value.Online, "revision": revisionIncrement("os_capability_instance"), "updated_at": value.UpdatedAt,
			}),
		}).Create(&value).Error; err != nil {
			return err
		}
		link := po.DeviceCapability{
			DeviceID: device.DeviceID, InstanceID: instance.InstanceID, CapabilityID: instance.Capability,
			OwnerID: device.UserID, Revision: 1, CreatedAt: millis(seenAt), UpdatedAt: millis(seenAt),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}, {Name: "instance_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"capability_id": link.CapabilityID, "owner_id": link.OwnerID,
				"revision": revisionIncrement("os_device_capability"), "updated_at": link.UpdatedAt,
			}),
		}).Create(&link).Error; err != nil {
			return err
		}
	}
	return nil
}

func revisionIncrement(table string) clause.Expr {
	return gorm.Expr(table + ".revision + 1")
}

func (s *Store) FindDevice(ctx context.Context, deviceID string) (*entity.RegisteredDevice, error) {
	var value po.Device
	result := s.data.DB(ctx).Where("device_id = ?", deviceID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ControlStore.FindDevice")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return deviceToEntity(&value), nil
}

func (s *Store) ListDevices(ctx context.Context, userID string) ([]entity.RegisteredDevice, error) {
	values := make([]po.Device, 0)
	db := s.data.DB(ctx)
	if userID != "" {
		db = db.Where("user_id = ? OR user_id = ''", userID)
	}
	if err := db.Order("online DESC, last_seen_at DESC").Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.ListDevices")
	}
	result := make([]entity.RegisteredDevice, 0, len(values))
	for index := range values {
		result = append(result, *deviceToEntity(&values[index]))
	}
	return result, nil
}

func (s *Store) SaveTask(ctx context.Context, task *entity.TaskSession) error {
	value, steps, err := taskToPO(task)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.Task
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", value.TaskID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		created := found.RowsAffected == 0
		if created {
			if err := tx.Create(value).Error; err != nil {
				return err
			}
		} else if entity.TerminalTaskStatus(existing.Status) {
			// A terminal task is immutable. Ignoring the complete late write also
			// prevents an event payload from claiming a status the row did not store.
			return nil
		} else if value.Revision == existing.Revision+1 {
			updated := tx.Model(&po.Task{}).Where("task_id = ? AND revision = ?", value.TaskID, existing.Revision).Updates(map[string]any{
				"parent_task_id": value.ParentTaskID, "conversation_id": value.ConversationID,
				"trace_id": value.TraceID, "user_id": value.UserID, "device_id": value.DeviceID,
				"goal": value.Goal, "status": value.Status, "sequence": value.Sequence,
				"revision": value.Revision, "current_step_id": value.CurrentStepID,
				"active_sessions": value.ActiveSessions, "metadata": value.Metadata,
				"result": value.Result, "error_detail": value.ErrorDetail, "updated_at": value.UpdatedAt,
			})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				return fmt.Errorf("task %s revision conflict: expected %d", value.TaskID, existing.Revision)
			}
		} else if value.Revision == existing.Revision {
			if !sameTaskSnapshot(value, &existing) {
				return fmt.Errorf("task %s revision %d conflicts with different durable state", value.TaskID, value.Revision)
			}
		} else {
			return fmt.Errorf("task revision conflict: got %d, want %d", value.Revision, existing.Revision+1)
		}
		for index := range steps {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "step_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"status", "title", "capability", "operation", "target", "input",
					"expected_observation", "retry_policy", "attempt", "updated_at",
				}),
			}).Create(&steps[index]).Error; err != nil {
				return err
			}
		}
		if created || value.Revision > existing.Revision {
			eventType := entity.EventTaskStatusChanged
			if created {
				eventType = entity.EventTaskCreated
			}
			return appendEvent(tx, entity.EventEnvelope{
				Type: eventType, Aggregate: "task", AggregateID: value.TaskID, TaskID: value.TaskID,
				TraceID: value.TraceID, Revision: value.Revision,
				Payload: map[string]any{"status": value.Status, "device_id": value.DeviceID, "current_step_id": value.CurrentStepID},
			})
		}
		return nil
	}), "ControlStore.SaveTask")
}

// PauseInterruptedTasks converts in-flight server work into an explicit,
// resumable state before devices reconnect. Waiting-user and waiting-approval
// tasks are already durable wait states and are intentionally preserved.
func (s *Store) PauseInterruptedTasks(ctx context.Context, reason string) error {
	statuses := []string{
		entity.TaskStatusCreated, entity.TaskStatusPlanning, entity.TaskStatusRunning,
		entity.TaskStatusWaitingObservation, entity.TaskStatusVerifying,
		entity.TaskStatusRetryWait, entity.TaskStatusCancelling,
	}
	var values []po.Task
	if err := s.data.DB(ctx).Where("status IN ?", statuses).Order("updated_at ASC").Find(&values).Error; err != nil {
		return log.WrapError(err, "ControlStore.PauseInterruptedTasks.list")
	}
	for _, value := range values {
		task, err := s.FindTask(ctx, value.TaskID)
		if err != nil {
			return log.WrapError(err, "ControlStore.PauseInterruptedTasks.load")
		}
		if task == nil || entity.TerminalTaskStatus(task.Status) || task.Status == entity.TaskStatusPaused {
			continue
		}
		if task.Metadata == nil {
			task.Metadata = make(map[string]interface{})
		}
		task.Metadata["pause_reason"] = reason
		task.Metadata["paused_at"] = time.Now().UTC()
		task.Status = entity.TaskStatusPaused
		for index := range task.Steps {
			if task.Steps[index].StepID == task.CurrentStepID &&
				!terminalStepStatus(task.Steps[index].Status) {
				task.Steps[index].Status = entity.StepStatusPaused
				task.Steps[index].UpdatedAt = time.Now().UTC()
			}
		}
		task.Revision++
		task.UpdatedAt = time.Now().UTC()
		if err := s.SaveTask(ctx, task); err != nil {
			return log.WrapError(err, "ControlStore.PauseInterruptedTasks.save")
		}
	}
	return nil
}

func terminalStepStatus(status string) bool {
	return status == entity.StepStatusCompleted || status == entity.StepStatusFailed || status == entity.StepStatusCancelled
}

func (s *Store) FindTask(ctx context.Context, taskID string) (*entity.TaskSession, error) {
	var taskPO po.Task
	result := s.data.DB(ctx).Where("task_id = ?", taskID).Limit(1).Find(&taskPO)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ControlStore.FindTask")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	task := taskToEntity(&taskPO)
	var steps []po.Step
	if err := s.data.DB(ctx).Where("task_id = ?", taskID).Order("ordinal ASC").Find(&steps).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.FindTask.steps")
	}
	for index := range steps {
		task.Steps = append(task.Steps, stepToEntity(&steps[index]))
	}
	var actions []po.Action
	if err := s.data.DB(ctx).Where("task_id = ?", taskID).Order("sequence ASC").Find(&actions).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.FindTask.actions")
	}
	for index := range actions {
		task.Actions = append(task.Actions, actionToEntity(&actions[index]))
	}
	var observations []po.Observation
	if err := s.data.DB(ctx).Where("task_id = ?", taskID).Order("sequence ASC").Find(&observations).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.FindTask.observations")
	}
	for index := range observations {
		task.Observations = append(task.Observations, observationToEntity(&observations[index]))
	}
	return task, nil
}

func (s *Store) ListTasks(ctx context.Context, userID, conversationID string, limit int) ([]entity.TaskSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	values := make([]po.Task, 0)
	db := s.data.DB(ctx).Where("user_id = ?", userID)
	if conversationID != "" {
		db = db.Where("conversation_id = ?", conversationID)
	}
	if err := db.Order("updated_at DESC").Limit(limit).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.ListTasks")
	}
	result := make([]entity.TaskSession, 0, len(values))
	for _, value := range values {
		task, err := s.FindTask(ctx, value.TaskID)
		if err != nil {
			return nil, err
		}
		if task != nil {
			result = append(result, *task)
		}
	}
	return result, nil
}

func (s *Store) SaveAction(ctx context.Context, deviceID, userID string, action entity.Action) error {
	value, err := actionToPO(deviceID, userID, action)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var duplicate po.Action
		foundDuplicate := tx.Where("action_id = ? OR idempotency_key = ?", action.ActionID, action.IdempotencyKey).Limit(1).Find(&duplicate)
		if foundDuplicate.Error != nil {
			return foundDuplicate.Error
		}
		if foundDuplicate.RowsAffected > 0 {
			traceCompatible := value.TraceID == "" || duplicate.TraceID == "" || value.TraceID == duplicate.TraceID
			if duplicate.ActionID == action.ActionID && duplicate.IdempotencyKey == action.IdempotencyKey &&
				duplicate.TaskID == action.TaskID && duplicate.StepID == action.StepID && traceCompatible && sameDurableAction(value, &duplicate) {
				return nil
			}
			return fmt.Errorf("action identity or immutable payload conflicts with an existing durable action")
		}
		var task po.Task
		foundTask := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", action.TaskID).Limit(1).Find(&task)
		if foundTask.Error != nil {
			return foundTask.Error
		}
		if foundTask.RowsAffected == 0 {
			return fmt.Errorf("task %s is not registered", action.TaskID)
		}
		if task.UserID != userID || (task.DeviceID != "" && task.DeviceID != deviceID) {
			return fmt.Errorf("action owner or device does not match task %s", action.TaskID)
		}
		if action.TraceID != "" && task.TraceID != "" && action.TraceID != task.TraceID {
			return fmt.Errorf("action trace_id does not match task %s", action.TaskID)
		}
		if value.TraceID == "" {
			value.TraceID = task.TraceID
		}
		if action.Revision != task.Revision {
			return fmt.Errorf("stale action revision: got %d, current task revision is %d", action.Revision, task.Revision)
		}
		if action.Sequence != task.Sequence+1 {
			return fmt.Errorf("action sequence is out of order: got %d, want %d", action.Sequence, task.Sequence+1)
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(value)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			return nil
		}
		return appendEvent(tx, entity.EventEnvelope{
			Type: entity.EventActionRequested, Aggregate: "action", AggregateID: action.ActionID,
			TaskID: action.TaskID, StepID: action.StepID, ActionID: action.ActionID, TraceID: value.TraceID, Revision: action.Revision,
			Payload: map[string]any{"device_id": deviceID, "capability": action.Capability, "capability_instance_id": action.CapabilityInstanceID, "operation": action.Operation},
		})
	}), "ControlStore.SaveAction")
}

func sameTaskSnapshot(left, right *po.Task) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.TaskID == right.TaskID && left.ParentTaskID == right.ParentTaskID &&
		left.ConversationID == right.ConversationID && left.TraceID == right.TraceID &&
		left.UserID == right.UserID && left.DeviceID == right.DeviceID && left.Goal == right.Goal &&
		left.Status == right.Status && left.Sequence == right.Sequence && left.Revision == right.Revision &&
		left.CurrentStepID == right.CurrentStepID && left.ActiveSessions == right.ActiveSessions &&
		left.Metadata == right.Metadata && left.Result == right.Result && left.ErrorDetail == right.ErrorDetail
}

func sameDurableAction(left, right *po.Action) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.ActionID == right.ActionID && left.TaskID == right.TaskID && left.StepID == right.StepID &&
		left.AgentBuildID == right.AgentBuildID && left.RunManifestID == right.RunManifestID &&
		left.DecisionID == right.DecisionID && left.DeviceID == right.DeviceID &&
		left.CapabilityInstanceID == right.CapabilityInstanceID && left.UserID == right.UserID &&
		left.SessionID == right.SessionID && left.Protocol == right.Protocol && left.Sequence == right.Sequence &&
		left.Revision == right.Revision && left.IdempotencyKey == right.IdempotencyKey &&
		left.IssuedAt == right.IssuedAt && left.Deadline == right.Deadline &&
		left.Capability == right.Capability && left.Operation == right.Operation && left.Target == right.Target &&
		left.Arguments == right.Arguments && left.Risk == right.Risk && left.Decision == right.Decision &&
		left.Policy == right.Policy && left.ExpectedObservation == right.ExpectedObservation
}

func (s *Store) CreateApproval(ctx context.Context, approval entity.Approval) error {
	if err := approval.Validate(); err != nil {
		return err
	}
	value, err := approvalToPO(approval)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var action po.Action
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("action_id = ?", approval.ActionID).Limit(1).Find(&action)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 || action.TaskID != approval.TaskID || action.StepID != approval.StepID || action.UserID != approval.OwnerID {
			return fmt.Errorf("approval does not match durable action %s", approval.ActionID)
		}
		var existing po.Approval
		duplicate := tx.Where("approval_id = ? OR action_id = ?", approval.ApprovalID, approval.ActionID).Limit(1).Find(&existing)
		if duplicate.Error != nil {
			return duplicate.Error
		}
		if duplicate.RowsAffected > 0 {
			if existing.ApprovalID == approval.ApprovalID && existing.ActionID == approval.ActionID && existing.OwnerID == approval.OwnerID {
				return nil
			}
			return fmt.Errorf("approval identity conflicts with an existing approval")
		}
		if err := tx.Create(value).Error; err != nil {
			return err
		}
		return appendEvent(tx, entity.EventEnvelope{
			Type: entity.EventApprovalRequested, Aggregate: "approval", AggregateID: approval.ApprovalID,
			TaskID: approval.TaskID, StepID: approval.StepID, ActionID: approval.ActionID,
			TraceID: approval.TraceID, Revision: approval.Revision,
			Payload: map[string]any{"approval_id": approval.ApprovalID, "risk": approval.Risk, "status": approval.Status, "expires_at": approval.ExpiresAt},
		})
	}), "ControlStore.CreateApproval")
}

func (s *Store) FindApproval(ctx context.Context, approvalID string) (*entity.Approval, error) {
	var value po.Approval
	result := s.data.DB(ctx).Where("approval_id = ?", approvalID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ControlStore.FindApproval")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	approval, err := approvalToEntity(&value)
	if err != nil {
		return nil, log.WrapError(err, "ControlStore.FindApproval.decode")
	}
	return approval, nil
}

func (s *Store) ListApprovals(ctx context.Context, ownerID, status string, limit int) ([]entity.Approval, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	values := make([]po.Approval, 0, limit)
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", status)
	}
	if err := db.Order("created_at DESC").Limit(limit).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.ListApprovals")
	}
	result := make([]entity.Approval, 0, len(values))
	for index := range values {
		approval, err := approvalToEntity(&values[index])
		if err != nil {
			return nil, log.WrapError(err, "ControlStore.ListApprovals.decode")
		}
		result = append(result, *approval)
	}
	return result, nil
}

func (s *Store) DecideApproval(ctx context.Context, approvalID, ownerID, status, decidedBy, reason string, decidedAt time.Time) (*entity.Approval, *entity.Action, error) {
	if status != entity.ApprovalApproved && status != entity.ApprovalRejected {
		return nil, nil, fmt.Errorf("approval decision must be APPROVED or REJECTED")
	}
	var decidedApproval entity.Approval
	var decidedAction entity.Action
	var expired bool
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var approval po.Approval
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("approval_id = ? AND owner_id = ?", approvalID, ownerID).Limit(1).Find(&approval)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			return fmt.Errorf("approval %s was not found", approvalID)
		}
		if approval.Status != entity.ApprovalPending {
			return fmt.Errorf("approval %s is already %s", approvalID, approval.Status)
		}
		if decidedAt.IsZero() {
			decidedAt = time.Now().UTC()
		}
		if approval.ExpiresAt > 0 && !decidedAt.Before(fromMillis(approval.ExpiresAt)) {
			status = entity.ApprovalExpired
			expired = true
		}
		updated := tx.Model(&po.Approval{}).Where("approval_id = ? AND revision = ? AND status = ?", approvalID, approval.Revision, entity.ApprovalPending).
			Updates(map[string]any{
				"status": status, "revision": approval.Revision + 1, "decided_by": decidedBy,
				"reason": reason, "decided_at": millis(decidedAt), "updated_at": millis(decidedAt),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return fmt.Errorf("approval %s revision conflict", approvalID)
		}
		var action po.Action
		foundAction := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("action_id = ?", approval.ActionID).Limit(1).Find(&action)
		if foundAction.Error != nil {
			return foundAction.Error
		}
		if foundAction.RowsAffected == 0 {
			return fmt.Errorf("action %s was not found", approval.ActionID)
		}
		policy := entity.Policy{}
		if err := decodeRequiredJSON(action.Policy, &policy, "action policy"); err != nil {
			return err
		}
		policy.ApprovalID = approvalID
		policy.Decision = entity.Block
		if status == entity.ApprovalApproved {
			policy.Decision = entity.Allow
		}
		encodedPolicy, err := encodeJSON(policy)
		if err != nil {
			return err
		}
		actionUpdates := map[string]any{"decision": policy.Decision, "policy": encodedPolicy}
		if status == entity.ApprovalApproved {
			duration := time.Duration(action.Deadline-action.IssuedAt) * time.Millisecond
			if duration <= 0 || duration > 10*time.Minute {
				duration = 2 * time.Minute
			}
			action.IssuedAt = millis(decidedAt)
			action.Deadline = millis(decidedAt.Add(duration))
			actionUpdates["issued_at"] = action.IssuedAt
			actionUpdates["deadline"] = action.Deadline
		}
		if err := tx.Model(&po.Action{}).Where("action_id = ?", action.ActionID).Updates(actionUpdates).Error; err != nil {
			return err
		}
		approval.Status, approval.Revision, approval.DecidedBy = status, approval.Revision+1, decidedBy
		approval.Reason, approval.DecidedAt, approval.UpdatedAt = reason, millis(decidedAt), millis(decidedAt)
		decodedApproval, err := approvalToEntity(&approval)
		if err != nil {
			return err
		}
		decidedApproval = *decodedApproval
		action.Decision, action.Policy = policy.Decision, encodedPolicy
		decidedAction = actionToEntity(&action)
		return appendEvent(tx, entity.EventEnvelope{
			Type: entity.EventApprovalDecided, Aggregate: "approval", AggregateID: approvalID,
			TaskID: approval.TaskID, StepID: approval.StepID, ActionID: approval.ActionID,
			TraceID: approval.TraceID, Revision: approval.Revision,
			Payload: map[string]any{"approval_id": approvalID, "status": status, "decided_by": decidedBy, "reason": reason},
		})
	})
	if err != nil {
		return nil, nil, log.WrapError(err, "ControlStore.DecideApproval")
	}
	if expired {
		return &decidedApproval, &decidedAction, fmt.Errorf("approval %s has expired", approvalID)
	}
	return &decidedApproval, &decidedAction, nil
}

func (s *Store) SaveObservation(ctx context.Context, observation entity.Observation) error {
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var action po.Action
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("action_id = ?", observation.ActionID).Limit(1).Find(&action)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("action %s is not registered", observation.ActionID)
		}
		if observation.TaskID != action.TaskID || observation.StepID != action.StepID ||
			observation.DeviceID != action.DeviceID || observation.Sequence != action.Sequence ||
			observation.Revision != action.Revision {
			return fmt.Errorf("observation correlation does not match durable action %s", observation.ActionID)
		}
		var task po.Task
		taskResult := tx.Where("task_id = ?", action.TaskID).Limit(1).Find(&task)
		if taskResult.Error != nil {
			return taskResult.Error
		}
		if taskResult.RowsAffected == 0 {
			return fmt.Errorf("task %s is not registered", action.TaskID)
		}
		if task.UserID != action.UserID {
			return fmt.Errorf("action %s owner does not match task owner", observation.ActionID)
		}
		traceID := action.TraceID
		if traceID == "" {
			traceID = task.TraceID
		}
		if observation.TraceID != "" && traceID != "" && observation.TraceID != traceID {
			return fmt.Errorf("observation trace_id does not match durable action %s", observation.ActionID)
		}
		if observation.TraceID == "" {
			observation.TraceID = traceID
		}
		if observation.AgentBuildID != action.AgentBuildID || observation.RunManifestID != action.RunManifestID {
			return fmt.Errorf("observation deployment provenance does not match durable action %s", observation.ActionID)
		}
		value, err := observationToPO(observation, action.IdempotencyKey, task.UserID, traceID)
		if err != nil {
			return err
		}
		var existing po.Observation
		found := tx.Where("action_id = ?", observation.ActionID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 && terminalObservationStatus(existing.Status) {
			if existing.Status == value.Status && existing.ObservationID == value.ObservationID {
				return nil
			}
			return fmt.Errorf("action %s already has terminal observation %s", observation.ActionID, existing.ObservationID)
		}
		changed := found.RowsAffected == 0 || existing.Status != value.Status || existing.Revision != value.Revision
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "action_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"observation_id", "owner_id", "trace_id", "device_id", "session_id", "revision", "status", "started_at",
				"finished_at", "observed_at", "summary", "state", "evidence", "attachments", "world_patch", "error", "error_detail", "updated_at",
			}),
		}).Create(value).Error; err != nil {
			return err
		}
		if err := projectObservationEvidence(tx, observation, task.UserID, traceID); err != nil {
			return err
		}
		worldRevision, err := projectStructuredWorld(tx, observation, task.UserID, traceID)
		if err != nil {
			return err
		}
		if changed {
			if err := appendEvent(tx, entity.EventEnvelope{
				Type: entity.EventObservationReceived, Aggregate: "observation", AggregateID: observation.ObservationID,
				TaskID: observation.TaskID, StepID: observation.StepID, ActionID: observation.ActionID, TraceID: traceID, Revision: observation.Revision,
				Payload: map[string]any{"status": observation.Status, "summary": observation.Summary, "world_revision": worldRevision},
			}); err != nil {
				return err
			}
		}
		return nil
	}), "ControlStore.SaveObservation")
}

func (s *Store) SaveProgress(ctx context.Context, progress entity.Progress) error {
	if err := progress.Validate(); err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var action po.Action
		result := tx.Where("action_id = ?", progress.ActionID).Limit(1).Find(&action)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("action %s is not registered", progress.ActionID)
		}
		if progress.TaskID != action.TaskID || progress.StepID != action.StepID ||
			progress.Sequence != action.Sequence || progress.Revision != action.Revision {
			return fmt.Errorf("progress correlation does not match durable action %s", progress.ActionID)
		}
		return appendEvent(tx, entity.EventEnvelope{
			Type: entity.EventActionProgressed, Aggregate: "action", AggregateID: progress.ActionID,
			TaskID: progress.TaskID, StepID: progress.StepID, ActionID: progress.ActionID, Revision: progress.Revision,
			Payload: map[string]any{"stage": progress.Stage, "message": progress.Message, "progress": progress.Progress, "bytes": progress.Bytes, "total": progress.Total},
		})
	}), "ControlStore.SaveProgress")
}

func (s *Store) FindObservationByIdempotency(ctx context.Context, key string) (*entity.Observation, error) {
	var value po.Observation
	result := s.data.DB(ctx).Where("idempotency_key = ?", key).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ControlStore.FindObservationByIdempotency")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	observation := observationToEntity(&value)
	return &observation, nil
}

func (s *Store) ListPendingActions(ctx context.Context, deviceID string, limit int) ([]entity.Action, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	values := make([]po.Action, 0, limit)
	err := s.data.DB(ctx).Table("os_action AS action").Select("action.*").
		Joins("JOIN os_task AS task ON task.task_id = action.task_id").
		Joins("LEFT JOIN os_observation AS observation ON observation.action_id = action.action_id").
		Where("action.device_id = ? AND observation.action_id IS NULL", deviceID).
		Where("action.decision = ?", entity.Allow).
		Where("task.status NOT IN ?", []string{entity.StatusCompleted, entity.StatusFailed, entity.StatusCancelled}).
		Order("action.created_at ASC").Limit(limit).Scan(&values).Error
	if err != nil {
		return nil, log.WrapError(err, "ControlStore.ListPendingActions")
	}
	result := make([]entity.Action, 0, len(values))
	for index := range values {
		result = append(result, actionToEntity(&values[index]))
	}
	return result, nil
}

func (s *Store) ListEvents(ctx context.Context, taskID string, afterSequence int64, limit int) ([]entity.EventEnvelope, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	values := make([]po.Event, 0)
	if err := s.data.DB(ctx).Where("task_id = ? AND sequence > ?", taskID, afterSequence).
		Order("sequence ASC").Limit(limit).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.ListEvents")
	}
	result := make([]entity.EventEnvelope, 0, len(values))
	for index := range values {
		result = append(result, eventToEntity(&values[index]))
	}
	return result, nil
}

func (s *Store) FindWorldState(ctx context.Context, taskID string) (*entity.WorldState, error) {
	var value po.WorldState
	result := s.data.DB(ctx).Where("task_id = ?", taskID).Limit(1).Find(&value)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ControlStore.FindWorldState")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	world := &entity.WorldState{TaskID: value.TaskID, Revision: value.Revision, State: map[string]any{}, UpdatedAt: fromMillis(value.UpdatedAt)}
	decodeJSON(value.State, &world.State)
	return world, nil
}

func (s *Store) QueryWorld(ctx context.Context, ownerID string, query entity.WorldQuery) ([]entity.WorldEntity, []entity.WorldRelation, []entity.WorldFact, error) {
	query.Normalize()
	if err := query.Validate(); err != nil {
		return nil, nil, nil, err
	}
	now := millis(query.AsOf)
	scope := ""
	if query.TaskID != "" {
		scope = "task:" + query.TaskID
	}

	entityDB := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	relationDB := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	factDB := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if scope != "" {
		entityDB = entityDB.Where("scope = ?", scope)
		relationDB = relationDB.Where("scope = ?", scope)
		factDB = factDB.Where("scope = ?", scope)
	} else if len(query.Scopes) > 0 {
		entityDB = entityDB.Where("scope IN ?", query.Scopes)
		relationDB = relationDB.Where("scope IN ?", query.Scopes)
		factDB = factDB.Where("scope IN ?", query.Scopes)
	}
	if !query.IncludeExpired {
		entityDB = entityDB.Where("expires_at > ?", now)
		relationDB = relationDB.Where("expires_at > ?", now)
		factDB = factDB.Where("expires_at > ?", now)
	}
	if len(query.EntityTypes) > 0 {
		entityDB = entityDB.Where("kind IN ?", query.EntityTypes)
	}
	if len(query.Predicates) > 0 {
		relationDB = relationDB.Where("kind IN ?", query.Predicates)
		factDB = factDB.Where("predicate IN ?", query.Predicates)
	}
	if text := strings.TrimSpace(query.Text); text != "" {
		like := "%" + text + "%"
		entityDB = entityDB.Where("entity_id LIKE ? OR properties LIKE ?", like, like)
		relationDB = relationDB.Where("relation_id LIKE ? OR properties LIKE ?", like, like)
		factDB = factDB.Where("fact_id LIKE ? OR value LIKE ? OR properties LIKE ?", like, like, like)
	}
	var entityRows []po.WorldEntity
	var relationRows []po.WorldRelation
	var factRows []po.WorldFact
	if err := entityDB.Order("updated_at DESC").Limit(query.Limit).Find(&entityRows).Error; err != nil {
		return nil, nil, nil, log.WrapError(err, "ControlStore.QueryWorld.entities")
	}
	if err := relationDB.Order("updated_at DESC").Limit(query.Limit).Find(&relationRows).Error; err != nil {
		return nil, nil, nil, log.WrapError(err, "ControlStore.QueryWorld.relations")
	}
	if err := factDB.Order("updated_at DESC").Limit(query.Limit).Find(&factRows).Error; err != nil {
		return nil, nil, nil, log.WrapError(err, "ControlStore.QueryWorld.facts")
	}
	entities := make([]entity.WorldEntity, 0, len(entityRows))
	for _, row := range entityRows {
		value := entity.WorldEntity{EntityID: row.EntityID, Scope: row.Scope, Type: row.Kind, Confidence: row.Confidence, ObservedAt: fromMillis(row.ObservedAt), ExpiresAt: fromMillis(row.ExpiresAt), Revision: row.Revision}
		decodeJSON(row.Properties, &value.Properties)
		decodeJSON(row.Evidence, &value.Evidence)
		value.CanonicalName, _ = value.Properties["canonical_name"].(string)
		value.Aliases = stringSlice(value.Properties["aliases"])
		entities = append(entities, value)
	}
	relations := make([]entity.WorldRelation, 0, len(relationRows))
	for _, row := range relationRows {
		value := entity.WorldRelation{RelationID: row.RelationID, Scope: row.Scope, SourceID: row.FromID, TargetID: row.ToID, Predicate: row.Kind, Confidence: row.Confidence, ObservedAt: fromMillis(row.ObservedAt), ExpiresAt: fromMillis(row.ExpiresAt), Revision: row.Revision}
		decodeJSON(row.Properties, &value.Properties)
		decodeJSON(row.Evidence, &value.Evidence)
		relations = append(relations, value)
	}
	facts := make([]entity.WorldFact, 0, len(factRows))
	for _, row := range factRows {
		value := entity.WorldFact{FactID: row.FactID, Scope: row.Scope, SubjectID: row.SubjectID, SubjectType: row.SubjectType, Predicate: row.Predicate, ValueType: row.ValueType, Confidence: row.Confidence, ObservedAt: fromMillis(row.ObservedAt), ExpiresAt: fromMillis(row.ExpiresAt), Revision: row.Revision}
		decodeJSON(row.Value, &value.Value)
		decodeJSON(row.Properties, &value.Properties)
		decodeJSON(row.Evidence, &value.Evidence)
		facts = append(facts, value)
	}
	return entities, relations, facts, nil
}

func (s *Store) RecordWorldConflict(ctx context.Context, conflict entity.WorldConflict) error {
	candidates, _ := encodeJSON(conflict.CandidateIDs)
	details, _ := encodeJSON(conflict.Details)
	return s.data.DB(ctx).Create(&po.WorldConflict{ConflictID: conflict.ConflictID, OwnerID: conflict.OwnerID, TaskID: conflict.TaskID, ObservationID: conflict.ObservationID, Kind: conflict.Kind, Subject: conflict.Subject, CandidateIDs: candidates, Details: details, Status: conflict.Status, Resolution: conflict.Resolution, Revision: conflict.Revision, CreatedAt: millis(conflict.CreatedAt), UpdatedAt: millis(conflict.UpdatedAt)}).Error
}

func (s *Store) ListWorldConflicts(ctx context.Context, ownerID, status string, limit int) ([]entity.WorldConflict, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var rows []po.WorldConflict
	if err := db.Order("updated_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.ListWorldConflicts")
	}
	result := make([]entity.WorldConflict, 0, len(rows))
	for _, row := range rows {
		value := entity.WorldConflict{ConflictID: row.ConflictID, OwnerID: row.OwnerID, TaskID: row.TaskID, ObservationID: row.ObservationID, Kind: row.Kind, Subject: row.Subject, Status: row.Status, Resolution: row.Resolution, Revision: row.Revision, CreatedAt: fromMillis(row.CreatedAt), UpdatedAt: fromMillis(row.UpdatedAt)}
		decodeJSON(row.CandidateIDs, &value.CandidateIDs)
		decodeJSON(row.Details, &value.Details)
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) ResolveWorldConflict(ctx context.Context, ownerID, conflictID, resolution string, expectedRevision int64) (*entity.WorldConflict, error) {
	now := time.Now().UTC()
	updated := s.data.DB(ctx).Model(&po.WorldConflict{}).Where("owner_id = ? AND conflict_id = ? AND status = 'OPEN' AND revision = ?", ownerID, conflictID, expectedRevision).Updates(map[string]any{"status": "RESOLVED", "resolution": strings.TrimSpace(resolution), "revision": expectedRevision + 1, "updated_at": millis(now)})
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected == 0 {
		return nil, fmt.Errorf("world conflict not found or revision changed")
	}
	items, err := s.ListWorldConflicts(ctx, ownerID, "RESOLVED", 500)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ConflictID == conflictID {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("world conflict not found")
}

func (s *Store) CreateWorldProvider(ctx context.Context, value entity.WorldProvider) error {
	content, err := encodeJSON(value)
	if err != nil {
		return err
	}
	row := po.WorldProvider{ProviderID: value.ProviderID, OwnerID: value.OwnerID, Name: value.Name, Kind: value.Kind, Enabled: value.Enabled, Revision: value.Revision, Config: content, HealthStatus: value.HealthStatus, HealthMessage: value.HealthMessage, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "ControlStore.CreateWorldProvider")
}

func (s *Store) FindWorldProvider(ctx context.Context, ownerID, providerID string) (*entity.WorldProvider, error) {
	var row po.WorldProvider
	result := s.data.DB(ctx).Where("owner_id = ? AND provider_id = ?", ownerID, providerID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "ControlStore.FindWorldProvider")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return worldProviderToEntity(row), nil
}

func (s *Store) ListWorldProviders(ctx context.Context, ownerID string) ([]entity.WorldProvider, error) {
	var rows []po.WorldProvider
	if err := s.data.DB(ctx).Where("owner_id = ?", ownerID).Order("name ASC, provider_id ASC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "ControlStore.ListWorldProviders")
	}
	result := make([]entity.WorldProvider, 0, len(rows))
	for _, row := range rows {
		result = append(result, *worldProviderToEntity(row))
	}
	return result, nil
}

func (s *Store) UpdateWorldProvider(ctx context.Context, value entity.WorldProvider, expectedRevision int64) error {
	content, err := encodeJSON(value)
	if err != nil {
		return err
	}
	updated := s.data.DB(ctx).Model(&po.WorldProvider{}).
		Where("owner_id = ? AND provider_id = ? AND revision = ?", value.OwnerID, value.ProviderID, expectedRevision).
		Updates(map[string]any{"name": value.Name, "kind": value.Kind, "enabled": value.Enabled, "revision": value.Revision, "config": content, "health_status": value.HealthStatus, "health_message": value.HealthMessage, "last_checked_at": millisPtr(value.LastCheckedAt), "updated_at": millis(value.UpdatedAt)})
	if updated.Error != nil {
		return log.WrapError(updated.Error, "ControlStore.UpdateWorldProvider")
	}
	if updated.RowsAffected == 0 {
		return fmt.Errorf("world provider not found or revision changed")
	}
	return nil
}

func (s *Store) DeleteWorldProvider(ctx context.Context, ownerID, providerID string) error {
	deleted := s.data.DB(ctx).Where("owner_id = ? AND provider_id = ?", ownerID, providerID).Delete(&po.WorldProvider{})
	if deleted.Error != nil {
		return log.WrapError(deleted.Error, "ControlStore.DeleteWorldProvider")
	}
	if deleted.RowsAffected == 0 {
		return fmt.Errorf("world provider not found")
	}
	return nil
}

func (s *Store) RecordWorldProviderHealth(ctx context.Context, ownerID, providerID, status, message string, checkedAt time.Time) error {
	updated := s.data.DB(ctx).Model(&po.WorldProvider{}).Where("owner_id = ? AND provider_id = ?", ownerID, providerID).
		Updates(map[string]any{"health_status": status, "health_message": message, "last_checked_at": millis(checkedAt), "updated_at": millis(checkedAt)})
	if updated.Error != nil {
		return log.WrapError(updated.Error, "ControlStore.RecordWorldProviderHealth")
	}
	if updated.RowsAffected == 0 {
		return fmt.Errorf("world provider not found")
	}
	return nil
}

func worldProviderToEntity(row po.WorldProvider) *entity.WorldProvider {
	value := &entity.WorldProvider{}
	decodeJSON(row.Config, value)
	value.ProviderID, value.OwnerID, value.Name, value.Kind = row.ProviderID, row.OwnerID, row.Name, row.Kind
	value.Enabled, value.Revision = row.Enabled, row.Revision
	value.HealthStatus, value.HealthMessage = row.HealthStatus, row.HealthMessage
	value.CreatedAt, value.UpdatedAt = fromMillis(row.CreatedAt), fromMillis(row.UpdatedAt)
	if row.LastCheckedAt > 0 {
		checked := fromMillis(row.LastCheckedAt)
		value.LastCheckedAt = &checked
	}
	return value
}

func millisPtr(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return millis(*value)
}

func (s *Store) ClaimOutbox(ctx context.Context, limit int) ([]irepository.OutboxMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	messages := make([]irepository.OutboxMessage, 0, limit)
	err := s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		stale := now.Add(-30 * time.Second)
		values := make([]po.Outbox, 0, limit)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(status = ? AND available_at <= ?) OR (status = ? AND updated_at <= ?)",
				"PENDING", millis(now), "PROCESSING", millis(stale)).
			Order("available_at ASC, created_at ASC").Limit(limit).Find(&values).Error; err != nil {
			return err
		}
		for index := range values {
			updated := tx.Model(&po.Outbox{}).Where("outbox_id = ?", values[index].OutboxID).
				Updates(map[string]any{"status": "PROCESSING", "attempts": gorm.Expr("attempts + 1")})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				continue
			}
			messages = append(messages, irepository.OutboxMessage{
				OutboxID: values[index].OutboxID, EventID: values[index].EventID,
				Topic: values[index].Topic, Payload: values[index].Payload, Attempts: values[index].Attempts + 1,
			})
		}
		return nil
	})
	if err != nil {
		return nil, log.WrapError(err, "ControlStore.ClaimOutbox")
	}
	return messages, nil
}

func (s *Store) MarkOutboxPublished(ctx context.Context, outboxID string, publishedAt time.Time) error {
	result := s.data.DB(ctx).Model(&po.Outbox{}).Where("outbox_id = ? AND status = ?", outboxID, "PROCESSING").
		Updates(map[string]any{"status": "PUBLISHED", "published_at": millis(publishedAt), "last_error": ""})
	if result.Error != nil {
		return log.WrapError(result.Error, "ControlStore.MarkOutboxPublished")
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("outbox message %s is not processing", outboxID)
	}
	return nil
}

func (s *Store) MarkOutboxFailed(ctx context.Context, outboxID, message string, retryAt time.Time) error {
	status := "PENDING"
	var value po.Outbox
	if err := s.data.DB(ctx).Where("outbox_id = ?", outboxID).Limit(1).Find(&value).Error; err != nil {
		return log.WrapError(err, "ControlStore.MarkOutboxFailed.find")
	}
	if value.Attempts >= 10 {
		status = "FAILED"
	}
	return log.WrapError(s.data.DB(ctx).Model(&po.Outbox{}).Where("outbox_id = ?", outboxID).
		Updates(map[string]any{"status": status, "available_at": millis(retryAt), "last_error": message}).Error,
		"ControlStore.MarkOutboxFailed")
}

func taskToPO(task *entity.TaskSession) (*po.Task, []po.Step, error) {
	if task == nil || task.TaskID == "" {
		return nil, nil, fmt.Errorf("task is required")
	}
	active, err := encodeJSON(task.ActiveSessions)
	if err != nil {
		return nil, nil, err
	}
	metadata, err := encodeJSON(task.Metadata)
	if err != nil {
		return nil, nil, err
	}
	result, err := encodeJSON(task.Result)
	if err != nil {
		return nil, nil, err
	}
	errorDetail, err := encodeJSON(task.ErrorDetail)
	if err != nil {
		return nil, nil, err
	}
	value := &po.Task{
		TaskID: task.TaskID, ParentTaskID: task.ParentTaskID, ConversationID: task.ConversationID,
		TraceID: task.TraceID, UserID: task.UserID, DeviceID: task.DeviceID, Goal: task.Goal,
		Status: task.Status, Sequence: task.Sequence, Revision: task.Revision, CurrentStepID: task.CurrentStepID,
		ActiveSessions: active, Metadata: metadata, Result: result, ErrorDetail: errorDetail,
		CreatedAt: millis(task.CreatedAt), UpdatedAt: millis(task.UpdatedAt),
	}
	if value.Revision <= 0 {
		value.Revision = 1
	}
	steps := make([]po.Step, 0, len(task.Steps))
	for index := range task.Steps {
		step, err := stepToPO(task.Steps[index])
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, step)
	}
	return value, steps, nil
}

func stepToPO(step entity.TaskStep) (po.Step, error) {
	target, err := encodeJSON(step.Target)
	if err != nil {
		return po.Step{}, err
	}
	input, err := encodeJSON(step.Input)
	if err != nil {
		return po.Step{}, err
	}
	expected, err := encodeJSON(step.ExpectedObservation)
	if err != nil {
		return po.Step{}, err
	}
	retry, err := encodeJSON(step.RetryPolicy)
	if err != nil {
		return po.Step{}, err
	}
	return po.Step{
		StepID: step.StepID, TaskID: step.TaskID, Ordinal: step.Ordinal, Status: step.Status,
		Title: step.Title, Capability: step.Capability, Operation: step.Operation,
		Target: target, Input: input, ExpectedObservation: expected, RetryPolicy: retry,
		Attempt: step.Attempt, CreatedAt: millis(step.CreatedAt), UpdatedAt: millis(step.UpdatedAt),
	}, nil
}

func actionToPO(deviceID, userID string, action entity.Action) (*po.Action, error) {
	target, err := encodeJSON(action.Target)
	if err != nil {
		return nil, err
	}
	arguments, err := encodeJSON(action.Arguments)
	if err != nil {
		return nil, err
	}
	policy, err := encodeJSON(action.Policy)
	if err != nil {
		return nil, err
	}
	expected, err := encodeJSON(action.ExpectedObservation)
	if err != nil {
		return nil, err
	}
	return &po.Action{
		ActionID: action.ActionID, TaskID: action.TaskID, StepID: action.StepID, TraceID: action.TraceID, DecisionID: action.DecisionID,
		AgentBuildID: action.AgentBuildID, RunManifestID: action.RunManifestID,
		DeviceID: deviceID, CapabilityInstanceID: action.CapabilityInstanceID,
		UserID: userID, SessionID: action.SessionID, Protocol: action.Protocol,
		Sequence: action.Sequence, Revision: action.Revision, IdempotencyKey: action.IdempotencyKey,
		IssuedAt: millis(action.IssuedAt), Deadline: millis(action.Deadline), Capability: action.Capability,
		Operation: action.Operation, Target: target, Arguments: arguments, Risk: action.Policy.Risk,
		Decision: action.Policy.Decision, Policy: policy, ExpectedObservation: expected,
	}, nil
}

func approvalToPO(approval entity.Approval) (*po.Approval, error) {
	scope, err := encodeJSON(approval.Scope)
	if err != nil {
		return nil, err
	}
	return &po.Approval{
		ApprovalID: approval.ApprovalID, TaskID: approval.TaskID, StepID: approval.StepID,
		ActionID: approval.ActionID, OwnerID: approval.OwnerID, Risk: approval.Risk,
		Status: approval.Status, Summary: approval.Summary, Scope: scope, Revision: approval.Revision,
		TraceID: approval.TraceID, DecidedBy: approval.DecidedBy, Reason: approval.Reason,
		CreatedAt: millis(approval.CreatedAt), UpdatedAt: millis(approval.UpdatedAt),
		ExpiresAt: millis(approval.ExpiresAt), DecidedAt: millis(approval.DecidedAt),
	}, nil
}

func approvalToEntity(value *po.Approval) (*entity.Approval, error) {
	approval := &entity.Approval{
		ApprovalID: value.ApprovalID, TaskID: value.TaskID, StepID: value.StepID,
		ActionID: value.ActionID, OwnerID: value.OwnerID, Risk: value.Risk, Status: value.Status,
		Summary: value.Summary, Scope: map[string]any{}, Revision: value.Revision,
		TraceID: value.TraceID, DecidedBy: value.DecidedBy, Reason: value.Reason,
		CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
		ExpiresAt: fromMillis(value.ExpiresAt), DecidedAt: fromMillis(value.DecidedAt),
	}
	if err := decodeRequiredJSON(value.Scope, &approval.Scope, "approval scope"); err != nil {
		return nil, err
	}
	return approval, nil
}

func observationToPO(observation entity.Observation, idempotencyKey, ownerID, traceID string) (*po.Observation, error) {
	state, err := encodeJSON(observation.State)
	if err != nil {
		return nil, err
	}
	evidence, err := encodeJSON(observation.Evidence)
	if err != nil {
		return nil, err
	}
	attachments, err := encodeJSON(observation.Attachments)
	if err != nil {
		return nil, err
	}
	worldPatch, err := encodeJSON(observation.WorldPatch)
	if err != nil {
		return nil, err
	}
	errorDetail, err := encodeJSON(observation.ErrorDetail)
	if err != nil {
		return nil, err
	}
	return &po.Observation{
		ObservationID: observation.ObservationID, ActionID: observation.ActionID, TaskID: observation.TaskID,
		StepID: observation.StepID, OwnerID: ownerID, TraceID: traceID,
		AgentBuildID: observation.AgentBuildID, RunManifestID: observation.RunManifestID,
		DeviceID: observation.DeviceID, IdempotencyKey: idempotencyKey,
		SessionID: observation.SessionID, Protocol: observation.Protocol, Sequence: observation.Sequence,
		Revision: observation.Revision, Status: observation.Status, StartedAt: millis(observation.StartedAt),
		FinishedAt: millis(observation.FinishedAt), ObservedAt: millis(observation.ObservedAt),
		Summary: observation.Summary, State: state, Evidence: evidence, Attachments: attachments, WorldPatch: worldPatch,
		Error: observation.Error, ErrorDetail: errorDetail,
	}, nil
}

func projectObservationEvidence(tx *gorm.DB, observation entity.Observation, ownerID, traceID string) error {
	now := time.Now().UTC()
	for index, evidence := range observation.Evidence {
		evidenceID := strings.TrimSpace(evidence.EvidenceID)
		if evidenceID == "" {
			evidenceID = stableRecordID("evidence", observation.ObservationID, fmt.Sprintf("%d", index), evidence.Kind, evidence.URI, evidence.SHA256)
		}
		metadata, err := encodeJSON(map[string]any{
			"mime_type": evidence.MIMEType,
			"summary":   evidence.Summary,
			"source":    evidence.Metadata,
		})
		if err != nil {
			return err
		}
		value := &po.WorldEvidenceRef{
			EvidenceID: evidenceID, OwnerID: ownerID, Scope: "task:" + observation.TaskID,
			TaskID: observation.TaskID, ActionID: observation.ActionID, Kind: defaultString(evidence.Kind, "evidence"),
			URI: evidence.URI, SHA256: evidence.SHA256, Metadata: metadata, Revision: observation.Revision,
			TraceID: traceID, CreatedAt: millis(now), UpdatedAt: millis(now),
		}
		if err := upsertWorldEvidenceRef(tx, value); err != nil {
			return err
		}
		if strings.TrimSpace(evidence.URI) != "" {
			artifact := &po.Artifact{
				ArtifactID: stableRecordID("artifact", observation.ObservationID, evidenceID),
				TaskID:     observation.TaskID, StepID: observation.StepID, OwnerID: ownerID,
				Kind: defaultString(evidence.Kind, "evidence"), URI: evidence.URI, MIMEType: evidence.MIMEType,
				SHA256: evidence.SHA256, Metadata: metadata, Revision: observation.Revision,
				TraceID: traceID, CreatedAt: millis(now), UpdatedAt: millis(now),
			}
			if err := upsertArtifact(tx, artifact); err != nil {
				return err
			}
		}
	}
	for _, attachment := range observation.Attachments {
		artifactID := stableRecordID("artifact", observation.ObservationID, attachment.ID)
		uri := fmt.Sprintf("athena://observation/%s/attachment/%s", observation.ObservationID, attachment.ID)
		metadata, err := encodeJSON(map[string]any{
			"purpose": attachment.Purpose, "detail": attachment.Detail, "encoding": attachment.Encoding,
			"retention": "metadata_only", "data_persisted": false,
		})
		if err != nil {
			return err
		}
		artifact := &po.Artifact{
			ArtifactID: artifactID, TaskID: observation.TaskID, StepID: observation.StepID, OwnerID: ownerID,
			Kind: attachment.Kind, URI: uri, MIMEType: attachment.MIMEType, Size: attachment.Size,
			SHA256: attachment.SHA256, Metadata: metadata, Revision: observation.Revision,
			TraceID: traceID, CreatedAt: millis(now), UpdatedAt: millis(now),
		}
		if err := upsertArtifact(tx, artifact); err != nil {
			return err
		}
		if err := upsertWorldEvidenceRef(tx, &po.WorldEvidenceRef{
			EvidenceID: artifactID, OwnerID: ownerID, Scope: "task:" + observation.TaskID,
			TaskID: observation.TaskID, ActionID: observation.ActionID, Kind: attachment.Kind,
			URI: uri, SHA256: attachment.SHA256, Metadata: metadata, Revision: observation.Revision,
			TraceID: traceID, CreatedAt: millis(now), UpdatedAt: millis(now),
		}); err != nil {
			return err
		}
	}
	return nil
}

func upsertArtifact(tx *gorm.DB, value *po.Artifact) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "artifact_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"task_id", "step_id", "owner_id", "kind", "uri", "mime_type", "size", "sha256",
			"metadata", "revision", "trace_id", "updated_at",
		}),
	}).Create(value).Error
}

func upsertWorldEvidenceRef(tx *gorm.DB, value *po.WorldEvidenceRef) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "evidence_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"owner_id", "scope", "entity_id", "relation_id", "task_id", "action_id", "kind", "uri",
			"sha256", "metadata", "revision", "trace_id", "updated_at",
		}),
	}).Create(value).Error
}

func stableRecordID(prefix string, parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%s-%x", prefix, digest[:12])
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, text)
		}
	}
	return result
}

func appendEvent(tx *gorm.DB, event entity.EventEnvelope) error {
	if event.EventID == "" {
		event.EventID = entity.NewID("event")
	}
	event.Protocol = entity.Protocol
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.Revision <= 0 {
		event.Revision = 1
	}
	if event.TaskID == "" {
		return fmt.Errorf("task_id is required for durable control events")
	}
	var task po.Task
	locked := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", event.TaskID).Limit(1).Find(&task)
	if locked.Error != nil {
		return locked.Error
	}
	if locked.RowsAffected == 0 {
		return fmt.Errorf("task %s is not registered", event.TaskID)
	}
	var sequence int64
	if err := tx.Model(&po.Event{}).Where("task_id = ?", event.TaskID).
		Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
		return err
	}
	event.Sequence = sequence + 1
	payload, err := encodeJSON(event.Payload)
	if err != nil {
		return err
	}
	value := &po.Event{
		EventID: event.EventID, Protocol: event.Protocol, Type: event.Type, Aggregate: event.Aggregate,
		AggregateID: event.AggregateID, TaskID: event.TaskID, StepID: event.StepID,
		ActionID: event.ActionID, TraceID: event.TraceID, Sequence: event.Sequence,
		Revision: event.Revision, OccurredAt: millis(event.OccurredAt), Payload: payload,
	}
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(value)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 0 {
		return nil
	}
	outboxPayload, err := encodeJSON(event)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return tx.Create(&po.Outbox{
		OutboxID: entity.NewID("outbox"), EventID: event.EventID, Topic: "athena.task.events",
		AggregateID: event.AggregateID, Payload: outboxPayload, Status: "PENDING", AvailableAt: millis(now),
	}).Error
}

func projectStructuredWorld(tx *gorm.DB, observation entity.Observation, ownerID, traceID string) (int64, error) {
	now := time.Now().UTC()
	var stored po.WorldState
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", observation.TaskID).Limit(1).Find(&stored)
	if result.Error != nil {
		return 0, result.Error
	}
	state := map[string]any{}
	if result.RowsAffected > 0 {
		decodeJSON(stored.State, &state)
	}
	devices := nestedMap(state, "devices")
	deviceID := observation.DeviceID
	if deviceID == "" {
		deviceID = "unknown"
	}
	device := nestedMap(devices, deviceID)
	sessions := nestedMap(device, "sessions")
	sessionID := observation.SessionID
	if sessionID == "" {
		sessionID = "default"
	}
	sessions[sessionID] = map[string]any{
		"status": observation.Status, "summary": observation.Summary, "state": observation.State,
		"observed_at": observation.ObservedAt, "action_id": observation.ActionID, "step_id": observation.StepID,
	}
	device["last_seen_at"] = observation.ObservedAt
	changes := make([]entity.WorldChange, 0)
	if observation.WorldPatch != nil {
		if observation.WorldPatch.BaseRevision != 0 && observation.WorldPatch.BaseRevision != stored.Revision {
			return 0, fmt.Errorf("world revision conflict: got %d, current %d", observation.WorldPatch.BaseRevision, stored.Revision)
		}
		for _, mutation := range observation.WorldPatch.Mutations {
			before, _ := worldValueAt(state, mutation.Path)
			changes = append(changes, entity.WorldChange{Kind: worldMutationKind(mutation.Path), ObjectID: worldMutationObjectID(mutation.Path), Operation: mutation.Operation, Path: mutation.Path, Before: before})
		}
		if err := applyWorldPatch(state, *observation.WorldPatch); err != nil {
			return 0, err
		}
		for index, mutation := range observation.WorldPatch.Mutations {
			changes[index].After, _ = worldValueAt(state, mutation.Path)
		}
	}
	encoded, err := encodeJSON(state)
	if err != nil {
		return 0, err
	}
	revision := stored.Revision + 1
	value := &po.WorldState{TaskID: observation.TaskID, Revision: revision, State: encoded, CreatedAt: millis(now), UpdatedAt: millis(now)}
	if result.RowsAffected == 0 {
		if err := tx.Create(value).Error; err != nil {
			return 0, err
		}
	} else {
		updated := tx.Model(&po.WorldState{}).Where("task_id = ? AND revision = ?", observation.TaskID, stored.Revision).
			Updates(map[string]any{"revision": revision, "state": encoded, "updated_at": millis(now)})
		if updated.Error != nil {
			return 0, updated.Error
		}
		if updated.RowsAffected == 0 {
			return 0, fmt.Errorf("world revision conflict: expected %d", stored.Revision)
		}
	}
	if observation.WorldPatch != nil {
		if err := persistSemanticWorld(tx, ownerID, traceID, observation, state, revision); err != nil {
			return 0, err
		}
	}
	if err := appendEvent(tx, entity.EventEnvelope{
		Type: entity.EventWorldPatched, Aggregate: "world", AggregateID: observation.TaskID,
		TaskID: observation.TaskID, StepID: observation.StepID, ActionID: observation.ActionID,
		Revision: revision, Payload: map[string]any{"world_revision": revision, "device_id": deviceID, "session_id": sessionID, "ontology_pack": worldPatchPack(observation.WorldPatch), "ontology_version": worldPatchVersion(observation.WorldPatch), "changes": changes},
	}); err != nil {
		return 0, err
	}
	return revision, nil
}

func persistSemanticWorld(tx *gorm.DB, ownerID, traceID string, observation entity.Observation, state map[string]any, revision int64) error {
	patch := observation.WorldPatch
	if patch == nil {
		return nil
	}
	scope := "task:" + observation.TaskID
	observedAt := observation.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	expiresAt := observedAt.Add(time.Duration(patch.TTLSeconds) * time.Second)
	evidence, err := encodeJSON(patch.Evidence)
	if err != nil {
		return err
	}
	touched := map[string]map[string]struct{}{"entities": {}, "relations": {}, "facts": {}}
	for _, mutation := range patch.Mutations {
		parts := jsonPointerParts(mutation.Path)
		if len(parts) >= 2 {
			if values, ok := touched[parts[0]]; ok {
				values[parts[1]] = struct{}{}
			}
		}
	}
	for id := range touched["entities"] {
		value, ok := semanticObject(state, "entities", id)
		if !ok {
			if err := tx.Where("owner_id = ? AND scope = ? AND entity_id = ?", ownerID, scope, id).Delete(&po.WorldEntity{}).Error; err != nil {
				return err
			}
			continue
		}
		properties, err := encodeJSON(value)
		if err != nil {
			return err
		}
		row := po.WorldEntity{EntityID: id, OwnerID: ownerID, Scope: scope, Kind: mapString(value, "type"), Properties: properties, Evidence: evidence, Confidence: patch.Confidence, ObservedAt: millis(observedAt), ExpiresAt: millis(expiresAt), Revision: revision, TraceID: traceID, CreatedAt: millis(observedAt), UpdatedAt: millis(observedAt)}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entity_id"}}, DoUpdates: clause.AssignmentColumns([]string{"owner_id", "scope", "kind", "properties", "evidence", "confidence", "observed_at", "expires_at", "revision", "trace_id", "updated_at"})}).Create(&row).Error; err != nil {
			return err
		}
	}
	for id := range touched["relations"] {
		value, ok := semanticObject(state, "relations", id)
		if !ok {
			if err := tx.Where("owner_id = ? AND scope = ? AND relation_id = ?", ownerID, scope, id).Delete(&po.WorldRelation{}).Error; err != nil {
				return err
			}
			continue
		}
		properties, err := encodeJSON(value)
		if err != nil {
			return err
		}
		row := po.WorldRelation{RelationID: id, OwnerID: ownerID, Scope: scope, FromID: firstMapString(value, "source_id", "source"), ToID: firstMapString(value, "target_id", "target"), Kind: firstMapString(value, "predicate", "type"), Properties: properties, Evidence: evidence, Confidence: patch.Confidence, ObservedAt: millis(observedAt), ExpiresAt: millis(expiresAt), Revision: revision, TraceID: traceID, CreatedAt: millis(observedAt), UpdatedAt: millis(observedAt)}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "relation_id"}}, DoUpdates: clause.AssignmentColumns([]string{"owner_id", "scope", "from_id", "to_id", "kind", "properties", "evidence", "confidence", "observed_at", "expires_at", "revision", "trace_id", "updated_at"})}).Create(&row).Error; err != nil {
			return err
		}
	}
	for id := range touched["facts"] {
		value, ok := semanticObject(state, "facts", id)
		if !ok {
			if err := tx.Where("owner_id = ? AND scope = ? AND fact_id = ?", ownerID, scope, id).Delete(&po.WorldFact{}).Error; err != nil {
				return err
			}
			continue
		}
		properties, err := encodeJSON(value)
		if err != nil {
			return err
		}
		encodedValue, err := encodeJSON(value["value"])
		if err != nil {
			return err
		}
		row := po.WorldFact{FactID: id, OwnerID: ownerID, TaskID: observation.TaskID, Scope: scope, SubjectID: mapString(value, "subject_id"), SubjectType: mapString(value, "subject_type"), Predicate: mapString(value, "predicate"), Value: encodedValue, ValueType: mapString(value, "value_type"), Properties: properties, Evidence: evidence, Confidence: patch.Confidence, ObservedAt: millis(observedAt), ExpiresAt: millis(expiresAt), Revision: revision, TraceID: traceID, CreatedAt: millis(observedAt), UpdatedAt: millis(observedAt)}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "fact_id"}}, DoUpdates: clause.AssignmentColumns([]string{"owner_id", "task_id", "scope", "subject_id", "subject_type", "predicate", "value", "value_type", "properties", "evidence", "confidence", "observed_at", "expires_at", "revision", "trace_id", "updated_at"})}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func semanticObject(state map[string]any, root, id string) (map[string]any, bool) {
	values, ok := state[root].(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := values[id].(map[string]any)
	return value, ok
}
func worldPatchPack(patch *entity.WorldPatch) string {
	if patch == nil {
		return ""
	}
	return patch.OntologyPack
}
func worldPatchVersion(patch *entity.WorldPatch) string {
	if patch == nil {
		return ""
	}
	return patch.OntologyVersion
}
func mapString(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return strings.TrimSpace(result)
}
func firstMapString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result := mapString(value, key); result != "" {
			return result
		}
	}
	return ""
}
func worldMutationKind(path string) string {
	parts := jsonPointerParts(path)
	if len(parts) > 0 {
		return parts[0]
	}
	return "world"
}
func worldMutationObjectID(path string) string {
	parts := jsonPointerParts(path)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}
func worldValueAt(state map[string]any, path string) (any, bool) {
	parts := jsonPointerParts(path)
	var value any = state
	for _, part := range parts {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var cloned any
	if json.Unmarshal(body, &cloned) != nil {
		return nil, false
	}
	return cloned, true
}

func applyWorldPatch(state map[string]any, patch entity.WorldPatch) error {
	if err := patch.Validate(); err != nil {
		return err
	}
	for _, mutation := range patch.Mutations {
		parts := jsonPointerParts(mutation.Path)
		if len(parts) == 0 {
			return fmt.Errorf("world mutation cannot replace the root")
		}
		cursor := state
		for _, part := range parts[:len(parts)-1] {
			var err error
			cursor, err = worldChildMap(cursor, part, mutation.Path)
			if err != nil {
				return err
			}
		}
		leaf := parts[len(parts)-1]
		switch mutation.Operation {
		case "set":
			cursor[leaf] = mutation.Value
		case "remove":
			delete(cursor, leaf)
		case "merge":
			incoming, ok := mutation.Value.(map[string]any)
			if !ok {
				return fmt.Errorf("merge mutation at %s requires an object", mutation.Path)
			}
			target, err := worldChildMap(cursor, leaf, mutation.Path)
			if err != nil {
				return err
			}
			for key, value := range incoming {
				target[key] = value
			}
		}
	}
	return nil
}

func worldChildMap(parent map[string]any, key, path string) (map[string]any, error) {
	if existing, exists := parent[key]; exists {
		value, ok := existing.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("world mutation at %s crosses non-object field %q", path, key)
		}
		return value, nil
	}
	value := map[string]any{}
	parent[key] = value
	return value, nil
}

func jsonPointerParts(path string) []string {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	for index := range parts {
		parts[index] = strings.ReplaceAll(strings.ReplaceAll(parts[index], "~1", "/"), "~0", "~")
	}
	return parts
}

func nestedMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

func deviceToEntity(value *po.Device) *entity.RegisteredDevice {
	device := &entity.RegisteredDevice{
		DeviceID: value.DeviceID, UserID: value.UserID, Name: value.Name, Platform: value.Platform,
		Architecture: value.Architecture, Online: value.Online, LeaseOwner: value.LeaseOwner, FencingToken: value.FencingToken,
		LeaseExpiresAt: fromMillis(value.LeaseExpiresAt), ConnectedAt: fromMillis(value.ConnectedAt), LastSeenAt: fromMillis(value.LastSeenAt),
	}
	decodeJSON(value.Capabilities, &device.Capabilities)
	decodeJSON(value.CapabilityInstances, &device.CapabilityInstances)
	return device
}

func taskToEntity(value *po.Task) *entity.TaskSession {
	task := &entity.TaskSession{
		TaskID: value.TaskID, ParentTaskID: value.ParentTaskID, ConversationID: value.ConversationID,
		TraceID: value.TraceID, UserID: value.UserID, DeviceID: value.DeviceID, Goal: value.Goal,
		Status: value.Status, Sequence: value.Sequence, Revision: value.Revision, CurrentStepID: value.CurrentStepID,
		CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
		ActiveSessions: map[string]string{}, Metadata: map[string]interface{}{}, Result: map[string]any{},
	}
	decodeJSON(value.ActiveSessions, &task.ActiveSessions)
	decodeJSON(value.Metadata, &task.Metadata)
	decodeJSON(value.Result, &task.Result)
	decodeJSON(value.ErrorDetail, &task.ErrorDetail)
	return task
}

func stepToEntity(value *po.Step) entity.TaskStep {
	step := entity.TaskStep{
		StepID: value.StepID, TaskID: value.TaskID, Ordinal: value.Ordinal, Status: value.Status,
		Title: value.Title, Capability: value.Capability, Operation: value.Operation, Attempt: value.Attempt,
		CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt),
	}
	decodeJSON(value.Target, &step.Target)
	decodeJSON(value.Input, &step.Input)
	decodeJSON(value.ExpectedObservation, &step.ExpectedObservation)
	decodeJSON(value.RetryPolicy, &step.RetryPolicy)
	return step
}

func actionToEntity(value *po.Action) entity.Action {
	action := entity.Action{
		Protocol: value.Protocol, Type: entity.TypeAction, TaskID: value.TaskID, StepID: value.StepID,
		ActionID: value.ActionID, TraceID: value.TraceID, AgentBuildID: value.AgentBuildID, RunManifestID: value.RunManifestID,
		DecisionID: value.DecisionID, DeviceID: value.DeviceID,
		CapabilityInstanceID: value.CapabilityInstanceID,
		SessionID:            value.SessionID, Sequence: value.Sequence, Revision: value.Revision,
		IdempotencyKey: value.IdempotencyKey, IssuedAt: fromMillis(value.IssuedAt), Deadline: fromMillis(value.Deadline),
		Capability: value.Capability, Operation: value.Operation, Policy: entity.Policy{Risk: value.Risk, Decision: value.Decision},
	}
	decodeJSON(value.Target, &action.Target)
	decodeJSON(value.Arguments, &action.Arguments)
	decodeJSON(value.Policy, &action.Policy)
	decodeJSON(value.ExpectedObservation, &action.ExpectedObservation)
	return action
}

func observationToEntity(value *po.Observation) entity.Observation {
	observation := entity.Observation{
		Protocol: value.Protocol, Type: entity.TypeObservation, ObservationID: value.ObservationID,
		TaskID: value.TaskID, StepID: value.StepID, ActionID: value.ActionID, TraceID: value.TraceID,
		AgentBuildID: value.AgentBuildID, RunManifestID: value.RunManifestID, DeviceID: value.DeviceID,
		SessionID: value.SessionID, Sequence: value.Sequence, Revision: value.Revision,
		Status: value.Status, StartedAt: fromMillis(value.StartedAt), FinishedAt: fromMillis(value.FinishedAt),
		ObservedAt: fromMillis(value.ObservedAt), Summary: value.Summary, Error: value.Error,
	}
	decodeJSON(value.State, &observation.State)
	decodeJSON(value.Evidence, &observation.Evidence)
	decodeJSON(value.Attachments, &observation.Attachments)
	decodeJSON(value.WorldPatch, &observation.WorldPatch)
	decodeJSON(value.ErrorDetail, &observation.ErrorDetail)
	return observation
}

func eventToEntity(value *po.Event) entity.EventEnvelope {
	event := entity.EventEnvelope{
		EventID: value.EventID, Protocol: value.Protocol, Type: value.Type, Aggregate: value.Aggregate,
		AggregateID: value.AggregateID, TaskID: value.TaskID, StepID: value.StepID, ActionID: value.ActionID,
		TraceID: value.TraceID, Sequence: value.Sequence, Revision: value.Revision, OccurredAt: fromMillis(value.OccurredAt),
	}
	decodeJSON(value.Payload, &event.Payload)
	return event
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode control data: %w", err)
	}
	return string(data), nil
}

func decodeJSON(value string, target any) {
	if strings.TrimSpace(value) != "" && value != "null" {
		_ = json.Unmarshal([]byte(value), target)
	}
}

func decodeRequiredJSON(value string, target any, field string) error {
	if strings.TrimSpace(value) == "" || value == "null" {
		return fmt.Errorf("%s is empty", field)
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return fmt.Errorf("decode %s: %w", field, err)
	}
	return nil
}

func terminalObservationStatus(status string) bool {
	switch status {
	case entity.ObservationSucceeded, entity.ObservationFailed, entity.ObservationCancelled,
		entity.ObservationExpired, entity.ObservationBlocked:
		return true
	default:
		return false
	}
}

func minimumCapabilityRisk(capability string, operations []string) string {
	values := append([]string{capability}, operations...)
	risk := entity.RiskReadOnly
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch {
		case containsRiskTerm(value, "credential", "password", "secret", "token", "cookie", "auth", "terminal", "shell", "python", "javascript", "code.execute"):
			return entity.RiskSensitive
		case containsRiskTerm(value, "purchase", "payment", "checkout", "book", "reserve", "send", "submit", "post", "publish", "upload", "delete", "remove", "install", "uninstall", "write"):
			if controlRiskRank(entity.RiskExternalWrite) > controlRiskRank(risk) {
				risk = entity.RiskExternalWrite
			}
		case containsRiskTerm(value, "click", "type", "press", "download", "close", "activate", "open_application", "app.open"):
			if controlRiskRank(entity.RiskReversible) > controlRiskRank(risk) {
				risk = entity.RiskReversible
			}
		}
	}
	return risk
}

func controlRiskRank(value string) int {
	switch value {
	case entity.RiskReadOnly:
		return 0
	case entity.RiskReversible:
		return 1
	case entity.RiskExternalWrite:
		return 2
	case entity.RiskSensitive:
		return 3
	default:
		return -1
	}
}

func containsRiskTerm(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func millis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func fromMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
