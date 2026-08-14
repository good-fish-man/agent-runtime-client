package scheduledtask

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	orchestrationsvc "github.com/good-fish-man/agent-runtime-client/application/service/orchestration"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	orchestrationpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/orchestration"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/scheduledtask"
	orchestrationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/orchestration"
	protocol "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"
)

func TestScheduledExecutionCreatesStandardDurableGoal(t *testing.T) {
	dsn := fmt.Sprintf("file:scheduled-control-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&orchestrationpo.Goal{}, &orchestrationpo.GoalTask{}, &orchestrationpo.SpecialistRun{}, &orchestrationpo.GoalCheckpoint{}, &orchestrationpo.ScheduleTrigger{}, &po.ScheduledTask{}); err != nil {
		t.Fatal(err)
	}
	d := data.New(db)
	goals := orchestrationsvc.NewService(orchestrationrepo.NewStore(d))
	service := NewService(d, nil).WithControlPlane(nil, goals)
	task := po.ScheduledTask{Ulid: "schedule-1", UserID: "owner-1", AgentID: "agent-1", SessionID: "conversation-1", Prompt: "check availability", Timezone: "Asia/Tokyo", CronExpr: "* * * * *", RetryMax: 3, RetryBackoffMS: 1000, MaxConcurrency: 1, RiskLevel: "R1", ApprovalMode: protocol.ApprovalNone, Notify: true}
	service.execute(task, "202608151200")

	triggers, err := goals.ListScheduleTriggers(context.Background(), task.UserID, task.Ulid, 10)
	if err != nil || len(triggers) != 1 {
		t.Fatalf("triggers=%+v error=%v", triggers, err)
	}
	state, err := goals.GetGoal(context.Background(), task.UserID, triggers[0].GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Goal.Trigger.Type != protocol.TriggerSchedule || state.Goal.Trigger.ScheduleID != task.Ulid || len(state.Tasks) != 1 || state.Tasks[0].Specialist != protocol.SpecialistResearch {
		t.Fatalf("schedule bypassed the standard Goal/Task protocol: %+v", state)
	}
	if triggers[0].Status != protocol.ScheduleTriggerQueued || triggers[0].IdempotencyKey != "schedule-1:202608151200" {
		t.Fatalf("unexpected trigger: %+v", triggers[0])
	}

	service.execute(task, "202608151200")
	triggers, _ = goals.ListScheduleTriggers(context.Background(), task.UserID, task.Ulid, 10)
	if len(triggers) != 1 {
		t.Fatalf("same schedule slot created duplicate goals: %+v", triggers)
	}
}

func TestScheduledPreExecutionApprovalIsDurableAndCannotBeBypassed(t *testing.T) {
	dsn := fmt.Sprintf("file:scheduled-approval-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&orchestrationpo.Goal{}, &orchestrationpo.GoalTask{}, &orchestrationpo.SpecialistRun{}, &orchestrationpo.GoalCheckpoint{}, &orchestrationpo.ScheduleTrigger{}, &po.ScheduledTask{}, &chatpo.ChatApproval{}); err != nil {
		t.Fatal(err)
	}
	d := data.New(db)
	goals := orchestrationsvc.NewService(orchestrationrepo.NewStore(d))
	service := NewService(d, nil).WithControlPlane(nil, goals)
	task := po.ScheduledTask{Ulid: "schedule-r2", UserID: "owner-1", AgentID: "agent-1", Prompt: "check and prepare", Timezone: "Asia/Tokyo", CronExpr: "* * * * *", RetryMax: 1, MaxConcurrency: 1, RiskLevel: "R2", ApprovalMode: protocol.ApprovalBeforeRun}
	service.execute(task, "202608151300")

	var approval chatpo.ChatApproval
	if err := db.Where("user_id = ? AND status = ?", task.UserID, "pending").First(&approval).Error; err != nil {
		t.Fatal(err)
	}
	triggers, err := goals.ListScheduleTriggers(context.Background(), task.UserID, task.Ulid, 10)
	if err != nil || len(triggers) != 1 {
		t.Fatalf("triggers=%+v error=%v", triggers, err)
	}
	state, err := goals.GetGoal(context.Background(), task.UserID, triggers[0].GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Checkpoint.PendingApprovalIDs) != 1 || state.Checkpoint.PendingApprovalIDs[0] != approval.Ulid {
		t.Fatalf("approval was not checkpointed: approval=%s checkpoint=%+v", approval.Ulid, state.Checkpoint.PendingApprovalIDs)
	}
	if _, err := goals.Resume(context.Background(), task.UserID, state.Goal.GoalID, state.Goal.Revision); err == nil {
		t.Fatal("public resume bypassed scheduled approval")
	}
	if err := service.DecideApproval(context.Background(), task.UserID, approval.Ulid, ApprovalDecision{Approved: true, Reason: "approved"}); err != nil {
		t.Fatal(err)
	}
	state, err = goals.GetGoal(context.Background(), task.UserID, state.Goal.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Goal.Status != protocol.GoalRunning || len(state.Checkpoint.PendingApprovalIDs) != 0 {
		t.Fatalf("approved scheduled goal did not resume cleanly: %+v", state)
	}
}
