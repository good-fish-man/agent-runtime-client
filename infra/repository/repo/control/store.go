package control

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	irepository "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	log "github.com/good-fish-man/logx"
)

type Store struct{ data *data.Data }

func NewStore(data *data.Data) *Store { return &Store{data: data} }

var _ irepository.Store = (*Store)(nil)

func (s *Store) UpsertDevice(ctx context.Context, device *entity.RegisteredDevice) error {
	capabilities, err := encodeJSON(device.Capabilities)
	if err != nil {
		return err
	}
	value := &po.Device{
		DeviceID: device.DeviceID, UserID: device.UserID, Name: device.Name, Platform: device.Platform, Architecture: device.Architecture,
		Capabilities: capabilities, Online: device.Online, ConnectedAt: millis(device.ConnectedAt), LastSeenAt: millis(device.LastSeenAt),
	}
	return log.WrapError(s.data.DB(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "device_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "platform", "architecture", "capabilities", "online", "connected_at", "last_seen_at", "updated_at"}),
	}).Create(value).Error, "ControlStore.UpsertDevice")
}

func (s *Store) BindDevice(ctx context.Context, deviceID, userID string) error {
	result := s.data.DB(ctx).Model(&po.Device{}).
		Where("device_id = ? AND (user_id = '' OR user_id = ?)", deviceID, userID).
		Update("user_id", userID)
	if result.Error != nil {
		return log.WrapError(result.Error, "ControlStore.BindDevice")
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("device is already bound to another user")
	}
	return nil
}

func (s *Store) SetDeviceOnline(ctx context.Context, deviceID string, online bool, seenAt time.Time) error {
	return log.WrapError(s.data.DB(ctx).Model(&po.Device{}).Where("device_id = ?", deviceID).
		Updates(map[string]any{"online": online, "last_seen_at": millis(seenAt)}).Error, "ControlStore.SetDeviceOnline")
}

func (s *Store) MarkAllDevicesOffline(ctx context.Context, seenAt time.Time) error {
	return log.WrapError(s.data.DB(ctx).Model(&po.Device{}).Where("online = ?", true).
		Updates(map[string]any{"online": false, "last_seen_at": millis(seenAt)}).Error, "ControlStore.MarkAllDevicesOffline")
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
	active, err := encodeJSON(task.ActiveSessions)
	if err != nil {
		return err
	}
	metadata, err := encodeJSON(task.Metadata)
	if err != nil {
		return err
	}
	value := &po.Task{
		TaskID: task.TaskID, ConversationID: task.ConversationID, UserID: task.UserID, DeviceID: task.DeviceID,
		Status: task.Status, Sequence: task.Sequence, ActiveSessions: active, Metadata: metadata,
		CreatedAt: millis(task.CreatedAt), UpdatedAt: millis(task.UpdatedAt),
	}
	db := s.data.DB(ctx)
	created := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}}, DoNothing: true}).Create(value)
	if created.Error != nil {
		return log.WrapError(created.Error, "ControlStore.SaveTask.create")
	}
	if created.RowsAffected > 0 {
		return nil
	}
	return log.WrapError(db.Transaction(func(tx *gorm.DB) error {
		newer := tx.Model(&po.Task{}).
			Where("task_id = ? AND updated_at <= ?", value.TaskID, value.UpdatedAt).
			Updates(map[string]any{
				"conversation_id": value.ConversationID,
				"user_id":         value.UserID,
				"device_id":       value.DeviceID,
				"sequence":        value.Sequence,
				"active_sessions": value.ActiveSessions,
				"metadata":        value.Metadata,
				"updated_at":      value.UpdatedAt,
			})
		if newer.Error != nil {
			return newer.Error
		}
		return tx.Model(&po.Task{}).
			Where("task_id = ?", value.TaskID).
			Update("status", gorm.Expr(
				"CASE WHEN status IN ? THEN status ELSE ? END",
				[]string{entity.StatusCompleted, entity.StatusFailed, entity.StatusCancelled},
				value.Status,
			)).Error
	}), "ControlStore.SaveTask.update")
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
	task := &entity.TaskSession{
		TaskID: taskPO.TaskID, ConversationID: taskPO.ConversationID, UserID: taskPO.UserID, DeviceID: taskPO.DeviceID,
		Status: taskPO.Status, Sequence: taskPO.Sequence, CreatedAt: fromMillis(taskPO.CreatedAt), UpdatedAt: fromMillis(taskPO.UpdatedAt),
		ActiveSessions: map[string]string{}, Metadata: map[string]interface{}{},
	}
	_ = json.Unmarshal([]byte(taskPO.ActiveSessions), &task.ActiveSessions)
	_ = json.Unmarshal([]byte(taskPO.Metadata), &task.Metadata)
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
	arguments, err := encodeJSON(action.Arguments)
	if err != nil {
		return err
	}
	value := &po.Action{
		ActionID: action.ActionID, TaskID: action.TaskID, DeviceID: deviceID, UserID: userID, SessionID: action.SessionID,
		Sequence: action.Sequence, IdempotencyKey: action.IdempotencyKey, Deadline: millis(action.Deadline), Capability: action.Capability,
		Arguments: arguments, Risk: action.Policy.Risk, Decision: action.Policy.Decision,
	}
	return log.WrapError(s.data.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(value).Error, "ControlStore.SaveAction")
}

func (s *Store) SaveObservation(ctx context.Context, observation entity.Observation) error {
	state, err := encodeJSON(observation.State)
	if err != nil {
		return err
	}
	var action po.Action
	result := s.data.DB(ctx).Where("action_id = ?", observation.ActionID).Limit(1).Find(&action)
	if result.Error != nil {
		return log.WrapError(result.Error, "ControlStore.SaveObservation.action")
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("action %s is not registered", observation.ActionID)
	}
	value := &po.Observation{
		ActionID: observation.ActionID, TaskID: observation.TaskID, IdempotencyKey: action.IdempotencyKey,
		SessionID: observation.SessionID, Sequence: observation.Sequence, Status: observation.Status,
		ObservedAt: millis(observation.ObservedAt), State: state, Error: observation.Error,
	}
	return log.WrapError(s.data.DB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "action_id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "observed_at", "state", "error"}),
	}).Create(value).Error, "ControlStore.SaveObservation")
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

func deviceToEntity(value *po.Device) *entity.RegisteredDevice {
	device := &entity.RegisteredDevice{
		DeviceID: value.DeviceID, UserID: value.UserID, Name: value.Name, Platform: value.Platform,
		Architecture: value.Architecture, Online: value.Online, ConnectedAt: fromMillis(value.ConnectedAt), LastSeenAt: fromMillis(value.LastSeenAt),
	}
	_ = json.Unmarshal([]byte(value.Capabilities), &device.Capabilities)
	return device
}

func actionToEntity(value *po.Action) entity.Action {
	action := entity.Action{
		Protocol: entity.Protocol, Type: entity.TypeAction, TaskID: value.TaskID, ActionID: value.ActionID,
		SessionID: value.SessionID, Sequence: value.Sequence, IdempotencyKey: value.IdempotencyKey,
		Deadline: fromMillis(value.Deadline), Capability: value.Capability, Policy: entity.Policy{Risk: value.Risk, Decision: value.Decision},
	}
	_ = json.Unmarshal([]byte(value.Arguments), &action.Arguments)
	return action
}

func observationToEntity(value *po.Observation) entity.Observation {
	observation := entity.Observation{
		Protocol: entity.Protocol, Type: entity.TypeObservation, TaskID: value.TaskID, ActionID: value.ActionID,
		SessionID: value.SessionID, Sequence: value.Sequence, Status: value.Status, ObservedAt: fromMillis(value.ObservedAt), Error: value.Error,
	}
	_ = json.Unmarshal([]byte(value.State), &observation.State)
	return observation
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode control data: %w", err)
	}
	return string(data), nil
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
