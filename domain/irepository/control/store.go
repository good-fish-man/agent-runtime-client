package control

import (
	"context"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

type Store interface {
	UpsertDevice(context.Context, *entity.RegisteredDevice) error
	BindDevice(context.Context, string, string) error
	SetDeviceOnline(context.Context, string, bool, time.Time) error
	MarkAllDevicesOffline(context.Context, time.Time) error
	FindDevice(context.Context, string) (*entity.RegisteredDevice, error)
	ListDevices(context.Context, string) ([]entity.RegisteredDevice, error)
	SaveTask(context.Context, *entity.TaskSession) error
	FindTask(context.Context, string) (*entity.TaskSession, error)
	ListTasks(context.Context, string, string, int) ([]entity.TaskSession, error)
	SaveAction(context.Context, string, string, entity.Action) error
	CreateApproval(context.Context, entity.Approval) error
	FindApproval(context.Context, string) (*entity.Approval, error)
	ListApprovals(context.Context, string, string, int) ([]entity.Approval, error)
	DecideApproval(context.Context, string, string, string, string, string, time.Time) (*entity.Approval, *entity.Action, error)
	SaveObservation(context.Context, entity.Observation) error
	FindObservationByIdempotency(context.Context, string) (*entity.Observation, error)
	ListEvents(context.Context, string, int64, int) ([]entity.EventEnvelope, error)
	FindWorldState(context.Context, string) (*entity.WorldState, error)
}

type OutboxMessage struct {
	OutboxID string
	EventID  string
	Topic    string
	Payload  string
	Attempts int
}
