package scheduledtask

import (
	"context"
	"testing"

	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/scheduledtask"
)

func TestScheduledExecutionCreatesStandardControlTask(t *testing.T) {
	hub := controlsvc.NewHub()
	service := (&Service{}).WithControlPlane(hub, nil)
	task := po.ScheduledTask{Ulid: "schedule-1", UserID: "owner-1", AgentID: "agent-1", SessionID: "conversation-1", Prompt: "check availability", Timezone: "Asia/Tokyo", CronExpr: "* * * * *"}
	if err := service.beginControlTask(context.Background(), task, "scheduled-task-1"); err != nil {
		t.Fatal(err)
	}
	created, ok, err := hub.Task(context.Background(), "scheduled-task-1")
	if err != nil || !ok || created.Goal != task.Prompt || created.Metadata["trigger_type"] != "SCHEDULE" {
		t.Fatalf("scheduled task bypassed standard control protocol: task=%+v ok=%v error=%v", created, ok, err)
	}
}
