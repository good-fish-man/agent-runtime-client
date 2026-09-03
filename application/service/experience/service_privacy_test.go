package experience

import (
	"context"
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	controlrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/experience"
)

func TestProcessTaskDoesNotReadTaskContentWhenLearningIsDisabled(t *testing.T) {
	now := time.Now().UTC()
	control := &disabledLearningControlStore{task: &controlentity.TaskSession{
		TaskID: "task-disabled", UserID: "user-1", Goal: "password=must-not-be-read",
		Status: controlentity.StatusCompleted, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
	}}
	store := &disabledLearningExperienceStore{preference: entity.Preference{
		OwnerID: "user-1", LearningEnabled: false, RetentionDays: 30, MaxSensitivity: entity.SensitivitySensitive,
	}}
	service := NewService(store, control)
	created, err := service.ProcessTask(context.Background(), control.task.TaskID)
	if err != nil || !created {
		t.Fatalf("process disabled task: created=%v err=%v", created, err)
	}
	if control.listEventsCalled || store.modelUsageCalled {
		t.Fatalf("disabled learning read private task content: events=%v model_usage=%v", control.listEventsCalled, store.modelUsageCalled)
	}
	if store.created == nil || store.created.Experience.Status != entity.StatusSkipped || store.created.Experience.SkipReason != "learning_disabled_by_user" {
		t.Fatalf("disabled task did not create an explicit skip record: %#v", store.created)
	}
	if store.created.Payload != "" || len(store.created.EventRefs) != 0 {
		t.Fatalf("disabled task retained payload or event references: %#v", store.created)
	}
}

type disabledLearningControlStore struct {
	controlrepo.Store
	task             *controlentity.TaskSession
	listEventsCalled bool
}

func (s *disabledLearningControlStore) FindTask(context.Context, string) (*controlentity.TaskSession, error) {
	return s.task, nil
}

func (s *disabledLearningControlStore) ListEvents(context.Context, string, int64, int) ([]controlentity.EventEnvelope, error) {
	s.listEventsCalled = true
	return nil, nil
}

type disabledLearningExperienceStore struct {
	repository.Store
	preference       entity.Preference
	created          *entity.StoredExperience
	modelUsageCalled bool
}

func (s *disabledLearningExperienceStore) GetPreference(context.Context, string) (*entity.Preference, error) {
	value := s.preference
	return &value, nil
}

func (s *disabledLearningExperienceStore) Create(_ context.Context, value *entity.StoredExperience) (bool, error) {
	s.created = value
	return true, nil
}

func (s *disabledLearningExperienceStore) ModelUsage(context.Context, string, string, time.Time, time.Time) ([]entity.ModelUsage, error) {
	s.modelUsageCalled = true
	return nil, nil
}
