package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/orchestration"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/orchestration"
	log "github.com/good-fish-man/logx"
)

var ErrRevisionConflict = errors.New("goal revision conflict")

type Store struct{ data *data.Data }

func NewStore(value *data.Data) *Store { return &Store{data: value} }

var _ repository.Store = (*Store)(nil)

func (s *Store) CreateGoal(ctx context.Context, goal entity.PersistentGoal, checkpoint entity.GoalCheckpoint) error {
	goalRow, err := goalRow(goal)
	if err != nil {
		return err
	}
	checkpointRow, err := checkpointRow(checkpoint)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&goalRow).Error; err != nil {
			return err
		}
		return tx.Create(&checkpointRow).Error
	}), "OrchestrationStore.CreateGoal")
}

func (s *Store) CreatePlannedGoal(ctx context.Context, goal entity.PersistentGoal, tasks []entity.SpecialistTask, checkpoint entity.GoalCheckpoint) error {
	goalValue, err := goalRow(goal)
	if err != nil {
		return err
	}
	checkpointValue, err := checkpointRow(checkpoint)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&goalValue).Error; err != nil {
			return err
		}
		for _, task := range tasks {
			row, err := taskRow(goal.OwnerID, task)
			if err != nil {
				return err
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return tx.Create(&checkpointValue).Error
	}), "OrchestrationStore.CreatePlannedGoal")
}

func (s *Store) FindGoal(ctx context.Context, ownerID, goalID string) (*entity.PersistentGoal, error) {
	var row po.Goal
	result := s.data.DB(ctx).Where("owner_id = ? AND goal_id = ?", ownerID, goalID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "OrchestrationStore.FindGoal")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.PersistentGoal](row.Content, "OrchestrationStore.FindGoal.decode")
}

func (s *Store) ListGoals(ctx context.Context, ownerID string, filter entity.GoalFilter) ([]entity.PersistentGoal, error) {
	db := s.data.DB(ctx).Where("owner_id = ?", ownerID)
	if len(filter.Statuses) > 0 {
		db = db.Where("status IN ?", filter.Statuses)
	}
	var rows []po.Goal
	if err := db.Order("updated_at DESC").Limit(normalizeLimit(filter.Limit, 200)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "OrchestrationStore.ListGoals")
	}
	return decodeRows[entity.PersistentGoal](rows, func(row po.Goal) string { return row.Content }, "OrchestrationStore.ListGoals.decode")
}

func (s *Store) ListRunnableGoals(ctx context.Context, limit int) ([]entity.PersistentGoal, error) {
	var rows []po.Goal
	statuses := []string{"PLANNED", "RUNNING", "WAITING_USER"}
	if err := s.data.DB(ctx).Where("status IN ?", statuses).Order("updated_at ASC").Limit(normalizeLimit(limit, 500)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "OrchestrationStore.ListRunnableGoals")
	}
	return decodeRows[entity.PersistentGoal](rows, func(row po.Goal) string { return row.Content }, "OrchestrationStore.ListRunnableGoals.decode")
}

func (s *Store) ListTasks(ctx context.Context, ownerID, goalID string) ([]entity.SpecialistTask, error) {
	var rows []po.GoalTask
	if err := s.data.DB(ctx).Where("owner_id = ? AND goal_id = ?", ownerID, goalID).Order("depth ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "OrchestrationStore.ListTasks")
	}
	return decodeRows[entity.SpecialistTask](rows, func(row po.GoalTask) string { return row.Content }, "OrchestrationStore.ListTasks.decode")
}

func (s *Store) ListResults(ctx context.Context, ownerID, goalID string, limit int) ([]entity.SpecialistResult, error) {
	var rows []po.SpecialistRun
	if err := s.data.DB(ctx).Where("owner_id = ? AND goal_id = ?", ownerID, goalID).Order("created_at DESC").Limit(normalizeLimit(limit, 500)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "OrchestrationStore.ListResults")
	}
	return decodeRows[entity.SpecialistResult](rows, func(row po.SpecialistRun) string { return row.Content }, "OrchestrationStore.ListResults.decode")
}

func (s *Store) LatestCheckpoint(ctx context.Context, ownerID, goalID string) (*entity.GoalCheckpoint, error) {
	var row po.GoalCheckpoint
	result := s.data.DB(ctx).Where("owner_id = ? AND goal_id = ?", ownerID, goalID).Order("sequence DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, log.WrapError(result.Error, "OrchestrationStore.LatestCheckpoint")
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return decode[entity.GoalCheckpoint](row.Content, "OrchestrationStore.LatestCheckpoint.decode")
}

func (s *Store) ListCheckpoints(ctx context.Context, ownerID, goalID string, limit int) ([]entity.GoalCheckpoint, error) {
	var rows []po.GoalCheckpoint
	if err := s.data.DB(ctx).Where("owner_id = ? AND goal_id = ?", ownerID, goalID).Order("sequence DESC").Limit(normalizeLimit(limit, 200)).Find(&rows).Error; err != nil {
		return nil, log.WrapError(err, "OrchestrationStore.ListCheckpoints")
	}
	return decodeRows[entity.GoalCheckpoint](rows, func(row po.GoalCheckpoint) string { return row.Content }, "OrchestrationStore.ListCheckpoints.decode")
}

func (s *Store) SaveState(ctx context.Context, goal entity.PersistentGoal, tasks []entity.SpecialistTask, result *entity.SpecialistResult, checkpoint entity.GoalCheckpoint, expectedRevision int64) error {
	goalContent, err := encode(goal)
	if err != nil {
		return err
	}
	checkpointValue, err := checkpointRow(checkpoint)
	if err != nil {
		return err
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&po.Goal{}).Where("goal_id = ? AND owner_id = ? AND revision = ?", goal.GoalID, goal.OwnerID, expectedRevision).Updates(map[string]any{"status": goal.Status, "revision": goal.Revision, "deadline": millis(goal.Deadline), "content": goalContent, "updated_at": millis(goal.UpdatedAt)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		for _, task := range tasks {
			row, err := taskRow(goal.OwnerID, task)
			if err != nil {
				return err
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}}, DoUpdates: clause.AssignmentColumns([]string{"specialist", "status", "depth", "device_id", "content", "updated_at"})}).Create(&row).Error; err != nil {
				return err
			}
		}
		if result != nil {
			content, err := encode(*result)
			if err != nil {
				return err
			}
			row := po.SpecialistRun{RunID: result.RunID, GoalID: result.GoalID, TaskID: result.TaskID, OwnerID: goal.OwnerID, Status: result.Status, Content: content, CreatedAt: millis(result.CreatedAt)}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return tx.Create(&checkpointValue).Error
	}), "OrchestrationStore.SaveState")
}

func (s *Store) CreateScheduleTrigger(ctx context.Context, ownerID string, value entity.ScheduleTrigger) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	row := po.ScheduleTrigger{TriggerID: value.TriggerID, ScheduleID: value.ScheduleID, TaskID: value.TaskID, OwnerID: ownerID, Status: value.Status, ScheduledAt: millis(value.ScheduledAt), Content: content}
	return log.WrapError(s.data.DB(ctx).Create(&row).Error, "OrchestrationStore.CreateScheduleTrigger")
}

func (s *Store) UpdateScheduleTrigger(ctx context.Context, ownerID string, value entity.ScheduleTrigger) error {
	content, err := encode(value)
	if err != nil {
		return err
	}
	result := s.data.DB(ctx).Model(&po.ScheduleTrigger{}).Where("trigger_id = ? AND owner_id = ?", value.TriggerID, ownerID).Updates(map[string]any{"status": value.Status, "content": content})
	if result.Error != nil {
		return log.WrapError(result.Error, "OrchestrationStore.UpdateScheduleTrigger")
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func goalRow(value entity.PersistentGoal) (po.Goal, error) {
	content, err := encode(value)
	if err != nil {
		return po.Goal{}, err
	}
	return po.Goal{GoalID: value.GoalID, OwnerID: value.OwnerID, AgentID: value.AgentID, Status: value.Status, Revision: value.Revision, Deadline: millis(value.Deadline), Content: content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}, nil
}

func taskRow(ownerID string, value entity.SpecialistTask) (po.GoalTask, error) {
	content, err := encode(value)
	if err != nil {
		return po.GoalTask{}, err
	}
	return po.GoalTask{TaskID: value.TaskID, GoalID: value.GoalID, OwnerID: ownerID, Specialist: value.Specialist, Status: value.Status, Depth: value.Depth, DeviceID: value.DeviceID, Content: content, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}, nil
}

func checkpointRow(value entity.GoalCheckpoint) (po.GoalCheckpoint, error) {
	content, err := encode(value)
	if err != nil {
		return po.GoalCheckpoint{}, err
	}
	return po.GoalCheckpoint{CheckpointID: value.CheckpointID, GoalID: value.GoalID, OwnerID: value.OwnerID, Sequence: value.Sequence, Status: value.Status, Checksum: value.Checksum, Content: content, CreatedAt: millis(value.CreatedAt)}, nil
}

func encode(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", log.WrapError(err, "OrchestrationStore.encode")
	}
	return string(body), nil
}

func decode[T any](body, operation string) (*T, error) {
	var value T
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		return nil, log.WrapError(err, operation)
	}
	return &value, nil
}

func decodeRows[T any, R any](rows []R, content func(R) string, operation string) ([]T, error) {
	items := make([]T, 0, len(rows))
	for _, row := range rows {
		item, err := decode[T](content(row), operation)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func normalizeLimit(value, maximum int) int {
	if value < 1 {
		return 50
	}
	if value > maximum {
		return maximum
	}
	return value
}

func millis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}
