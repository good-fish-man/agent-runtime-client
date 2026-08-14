package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/orchestration"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/orchestration"
	orchestrationv1 "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"
)

func TestGoalPlanCheckpointAndCrossDeviceResume(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	goal, err := service.CreateGoal(ctx, "owner-1", CreateGoalRequest{AgentID: "agent-1", Objective: "create a sourced five-day trip plan", SuccessCriteria: []entity.SuccessCriterion{{Description: "weather, transport and lodging are sourced", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := service.PlanGoal(ctx, "owner-1", goal.GoalID, PlanGoalRequest{ExpectedRevision: goal.Revision, Tasks: []PlanTaskRequest{
		{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "research weather and transport", RequiredCapabilities: []string{"internet.search"}},
		{TaskID: "browser", Depth: 1, Specialist: orchestrationv1.SpecialistBrowser, Objective: "verify official pages", RequiredCapabilities: []string{"browser.action"}},
		{TaskID: "synthesis", Depth: 2, Specialist: orchestrationv1.SpecialistSynthesis, Objective: "synthesize itinerary", DependsOn: []string{"research", "browser"}},
	}})
	if err != nil || len(state.Tasks) != 3 {
		t.Fatalf("plan=%+v error=%v", state, err)
	}
	decision, state, err := service.StartTask(ctx, "owner-1", goal.GoalID, "browser", StartTaskRequest{ExpectedRevision: state.Goal.Revision, Devices: []entity.DeviceCandidate{{DeviceID: "windows-offline", Online: false, Capabilities: []string{"browser.action"}}, {DeviceID: "mac", Online: true, Capabilities: []string{"browser.action"}}}})
	if err != nil || decision.DeviceID != "mac" {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	paused, err := service.Pause(ctx, "owner-1", goal.GoalID, "launcher disconnected", state.Goal.Revision)
	if err != nil || paused.Goal.Status != orchestrationv1.GoalPaused {
		t.Fatalf("paused=%+v error=%v", paused, err)
	}
	checkpoint, err := service.SaveCheckpoint(ctx, "owner-1", goal.GoalID, CheckpointRequest{ExpectedRevision: paused.Goal.Revision, Reason: "persist external effect", ConfirmedEffectKeys: []string{"action-open-tab"}, WorldSlicesByDevice: map[string]map[string]any{"mac": {"url": "https://example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := service.Resume(ctx, "owner-1", goal.GoalID, checkpoint.GoalRevision)
	if err != nil || len(resume.ConfirmedEffectKeys) != 1 || resume.WorldSlicesByDevice["mac"]["url"] == nil {
		t.Fatalf("resume=%+v error=%v", resume, err)
	}
}

func TestSpecialistResultsRequireProvenanceAndUnlockDependencies(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	goal, _ := service.CreateGoal(ctx, "owner-1", CreateGoalRequest{AgentID: "agent-1", Objective: "research", SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "criterion", Description: "has evidence", Required: true}}})
	state, _ := service.PlanGoal(ctx, "owner-1", goal.GoalID, PlanGoalRequest{Tasks: []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "research"}, {TaskID: "synthesis", Depth: 2, Specialist: orchestrationv1.SpecialistSynthesis, Objective: "synthesize", DependsOn: []string{"research"}}}})
	_, state, err := service.StartTask(ctx, "owner-1", goal.GoalID, "research", StartTaskRequest{ExpectedRevision: state.Goal.Revision, Devices: []entity.DeviceCandidate{{DeviceID: "server", Online: true}}})
	if err != nil {
		t.Fatal(err)
	}
	request := RecordResultRequest{ExpectedRevision: state.Goal.Revision, VerifiedCriteriaIDs: []string{"criterion"}, Result: entity.SpecialistResult{Status: orchestrationv1.TaskCompleted, Summary: "official source found", EvidenceRefs: []string{"evidence-1"}, Provenance: entity.Provenance{RunManifestID: "manifest-1", AgentBuildID: "build-1", ModelConfigVersion: "model-1", TraceID: "trace-1", ProducedAt: time.Now().UTC()}}}
	state, err = service.RecordResult(ctx, "owner-1", goal.GoalID, "research", request)
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks[1].Status != orchestrationv1.TaskReady {
		t.Fatalf("dependency was not unlocked: %+v", state.Tasks)
	}
}

func TestBudgetStopsSupervisorInsteadOfLooping(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	budget := defaultBudget
	budget.MaxTokens = 10
	goal, _ := service.CreateGoal(ctx, "owner-1", CreateGoalRequest{AgentID: "agent-1", Objective: "bounded", Budget: budget, SuccessCriteria: []entity.SuccessCriterion{{Description: "done", Required: true}}})
	state, _ := service.PlanGoal(ctx, "owner-1", goal.GoalID, PlanGoalRequest{Tasks: []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "research"}}})
	_, state, _ = service.StartTask(ctx, "owner-1", goal.GoalID, "research", StartTaskRequest{ExpectedRevision: state.Goal.Revision, Devices: []entity.DeviceCandidate{{DeviceID: "server", Online: true}}})
	state, err := service.RecordResult(ctx, "owner-1", goal.GoalID, "research", RecordResultRequest{ExpectedRevision: state.Goal.Revision, Result: entity.SpecialistResult{Status: orchestrationv1.TaskCompleted, Summary: "used budget", Usage: entity.BudgetUsage{Tokens: 11}, Provenance: entity.Provenance{RunManifestID: "manifest", AgentBuildID: "build", ModelConfigVersion: "model", TraceID: "trace", ProducedAt: time.Now().UTC()}}})
	if err != nil || state.Goal.Status != orchestrationv1.GoalWaitingUser {
		t.Fatalf("goal did not stop: state=%+v error=%v", state, err)
	}
}

func TestSpecialistReceivesOnlyDeclaredWorldSlice(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	goal, _ := service.CreateGoal(ctx, "owner-1", CreateGoalRequest{AgentID: "agent-1", Objective: "filtered context", SuccessCriteria: []entity.SuccessCriterion{{Description: "done", Required: true}}})
	state, _ := service.PlanGoal(ctx, "owner-1", goal.GoalID, PlanGoalRequest{Tasks: []PlanTaskRequest{{TaskID: "browser", Depth: 1, Specialist: orchestrationv1.SpecialistBrowser, Objective: "inspect page", WorldSliceRefs: []string{"url"}, RequiredCapabilities: []string{"browser.action"}}}})
	_, state, err := service.StartTask(ctx, "owner-1", goal.GoalID, "browser", StartTaskRequest{ExpectedRevision: state.Goal.Revision, Devices: []entity.DeviceCandidate{{DeviceID: "mac", Online: true, Capabilities: []string{"browser.action"}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveCheckpoint(ctx, "owner-1", goal.GoalID, CheckpointRequest{ExpectedRevision: state.Goal.Revision, Reason: "observed page", WorldSlicesByDevice: map[string]map[string]any{"mac": {"url": "https://example.com", "cookies": "must-not-share"}, "windows": {"url": "https://other.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	world, err := service.WorldSlice(ctx, "owner-1", goal.GoalID, "browser")
	if err != nil || world["mac"]["url"] == nil || world["mac"]["cookies"] != nil || world["windows"] != nil {
		t.Fatalf("world slice was not filtered: world=%+v error=%v", world, err)
	}
}

func TestCreatePlannedGoalIsAtomic(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	state, err := service.CreatePlannedGoal(ctx, "owner-1", CreatePlannedGoalRequest{CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "long-running research", SuccessCriteria: []entity.SuccessCriterion{{Description: "verified result", Required: true}}}, Tasks: []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "collect evidence"}}})
	if err != nil || state.Goal.Status != orchestrationv1.GoalPlanned || len(state.Tasks) != 1 || state.Checkpoint == nil {
		t.Fatalf("planned goal was not created atomically: state=%+v error=%v", state, err)
	}
}

func TestRequiredCriterionCannotBeVerifiedWithoutEvidence(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	state, err := service.CreatePlannedGoal(ctx, "owner-1", CreatePlannedGoalRequest{CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "prove it", SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "criterion", Description: "has evidence", Required: true}}}, Tasks: []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "research"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, state, err = service.StartTask(ctx, "owner-1", state.Goal.GoalID, "research", StartTaskRequest{ExpectedRevision: state.Goal.Revision})
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.RecordResult(ctx, "owner-1", state.Goal.GoalID, "research", RecordResultRequest{ExpectedRevision: state.Goal.Revision, VerifiedCriteriaIDs: []string{"criterion"}, Result: entity.SpecialistResult{Status: orchestrationv1.TaskCompleted, Summary: "model asserted completion", Provenance: entity.Provenance{RunManifestID: "manifest", AgentBuildID: "build", ModelConfigVersion: "model", TraceID: "trace", ProducedAt: time.Now().UTC()}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.Goal.SuccessCriteria[0].Verified || state.Goal.Status != orchestrationv1.GoalWaitingUser {
		t.Fatalf("unsupported model assertion completed the goal: %+v", state.Goal)
	}
}

func TestResultMustMatchExecutionAndEffectsNeedObservation(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	state, _ := service.CreatePlannedGoal(ctx, "owner-1", CreatePlannedGoalRequest{CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "open", SuccessCriteria: []entity.SuccessCriterion{{Description: "opened", Required: true}}}, Tasks: []PlanTaskRequest{{TaskID: "browser", Depth: 1, Specialist: orchestrationv1.SpecialistBrowser, Objective: "open"}}})
	_, state, _ = service.StartTask(ctx, "owner-1", state.Goal.GoalID, "browser", StartTaskRequest{ExpectedRevision: state.Goal.Revision})
	base := entity.SpecialistResult{ExecutionID: "wrong", Status: orchestrationv1.TaskCompleted, Summary: "opened", Provenance: entity.Provenance{RunManifestID: "manifest", AgentBuildID: "build", ModelConfigVersion: "model", TraceID: "trace", ProducedAt: time.Now().UTC()}}
	if _, err := service.RecordResult(ctx, "owner-1", state.Goal.GoalID, "browser", RecordResultRequest{ExpectedRevision: state.Goal.Revision, Result: base}); err == nil {
		t.Fatal("result from another execution was accepted")
	}
	base.ExecutionID = state.Tasks[0].ExecutionID
	if _, err := service.RecordResult(ctx, "owner-1", state.Goal.GoalID, "browser", RecordResultRequest{ExpectedRevision: state.Goal.Revision, ConfirmedEffectKeys: []string{"effect"}, Result: base}); err == nil {
		t.Fatal("external effect without an observation was accepted")
	}
}

func TestCorruptCheckpointCannotResume(t *testing.T) {
	service, db := newServiceWithDB(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "resume safely", SuccessCriteria: []entity.SuccessCriterion{{Description: "done", Required: true}}}, Tasks: []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&po.GoalCheckpoint{}).Where("checkpoint_id = ?", state.Checkpoint.CheckpointID).Update("content", `{"schema":"athena.orchestration.v1","checkpoint_id":"tampered"}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID); err == nil {
		t.Fatal("corrupt checkpoint was trusted")
	}
}

func TestCheckpointHistoryRejectsBrokenHashChain(t *testing.T) {
	service, db := newServiceWithDB(t)
	ctx := context.Background()
	state, err := service.CreatePlannedGoal(ctx, "owner-1", CreatePlannedGoalRequest{CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "resume from an attributable chain", SuccessCriteria: []entity.SuccessCriterion{{Description: "done", Required: true}}}, Tasks: []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	state, err = service.Pause(ctx, "owner-1", state.Goal.GoalID, "checkpoint two", state.Goal.Revision)
	if err != nil {
		t.Fatal(err)
	}
	latest := *state.Checkpoint
	latest.PreviousChecksum = fmt.Sprintf("%064x", 42)
	latest.Checksum, err = checkpointChecksum(latest)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(latest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&po.GoalCheckpoint{}).Where("checkpoint_id = ?", latest.CheckpointID).Update("content", string(content)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListCheckpoints(ctx, "owner-1", state.Goal.GoalID, 20); err == nil {
		t.Fatal("individually valid checkpoints with a broken predecessor link were accepted")
	}
}

func TestScheduledTriggerWaitsWhenRequiredEvidenceIsMissing(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	created, err := service.CreateScheduledGoal(ctx, "owner-1", CreateScheduledGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "scheduled research", SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "criterion", Description: "cited result", Required: true}}},
		ScheduleID:        "schedule-1", Slot: "202608151230", ScheduledAt: time.Now().UTC(), Retry: orchestrationv1.RetryPolicy{MaxAttempts: 1},
		Task: PlanTaskRequest{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "research"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, state, err := service.StartTask(ctx, "owner-1", created.State.Goal.GoalID, "research", StartTaskRequest{ExpectedRevision: created.State.Goal.Revision})
	if err != nil {
		t.Fatal(err)
	}
	result := entity.SpecialistResult{ExecutionID: state.Tasks[0].ExecutionID, Status: orchestrationv1.TaskCompleted, Summary: "unsupported assertion", Provenance: entity.Provenance{RunManifestID: "manifest", AgentBuildID: "build", ModelConfigVersion: "model", TraceID: "trace", ProducedAt: time.Now().UTC()}}
	state, err = service.RecordResult(ctx, "owner-1", state.Goal.GoalID, "research", RecordResultRequest{ExpectedRevision: state.Goal.Revision, VerifiedCriteriaIDs: []string{"criterion"}, Result: result})
	if err != nil {
		t.Fatal(err)
	}
	if state.Goal.Status != orchestrationv1.GoalWaitingUser {
		t.Fatalf("expected goal to wait for evidence, got %s", state.Goal.Status)
	}
	triggers, err := service.ListScheduleTriggers(ctx, "owner-1", "schedule-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 || triggers[0].Status != orchestrationv1.ScheduleTriggerWaitingUser {
		t.Fatalf("expected schedule trigger to mirror WAITING_USER, got %+v", triggers)
	}
}

func TestPendingApprovalCannotBeBypassedByPublicResume(t *testing.T) {
	service := newService(t)
	ctx := context.Background()
	created, err := service.CreateScheduledGoal(ctx, "owner-1", CreateScheduledGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "approved work", SuccessCriteria: []entity.SuccessCriterion{{Description: "done", Required: true}}},
		ScheduleID:        "schedule-approval", Slot: "202608151245", ScheduledAt: time.Now().UTC(), Retry: orchestrationv1.RetryPolicy{MaxAttempts: 1},
		RequireApproval: true, PendingApprovalID: "approval-1",
		Task: PlanTaskRequest{TaskID: "research", Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: "work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resume(ctx, "owner-1", created.State.Goal.GoalID, created.State.Goal.Revision); err == nil {
		t.Fatal("public resume bypassed a pending pre-execution approval")
	}
	if _, err := service.ResumeApproved(ctx, "owner-1", created.State.Goal.GoalID, "another-approval", created.State.Goal.Revision); err == nil {
		t.Fatal("an unrelated approval resumed the goal")
	}
	plan, err := service.ResumeApproved(ctx, "owner-1", created.State.Goal.GoalID, "approval-1", created.State.Goal.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Goal.Status != orchestrationv1.GoalRunning {
		t.Fatalf("approved goal did not resume: %+v", plan.Goal)
	}
	state, err := service.GetGoal(ctx, "owner-1", created.State.Goal.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Checkpoint.PendingApprovalIDs) != 0 {
		t.Fatalf("resolved approval remained in checkpoint: %+v", state.Checkpoint.PendingApprovalIDs)
	}
}

func newService(t *testing.T) *Service {
	service, _ := newServiceWithDB(t)
	return service
}

func newServiceWithDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:orchestration-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.Goal{}, &po.GoalTask{}, &po.SpecialistRun{}, &po.GoalCheckpoint{}, &po.ScheduleTrigger{}); err != nil {
		t.Fatal(err)
	}
	return NewService(repo.NewStore(data.New(db))), db
}
