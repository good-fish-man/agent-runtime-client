package orchestration

import (
	"context"
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

func newService(t *testing.T) *Service {
	t.Helper()
	dsn := fmt.Sprintf("file:orchestration-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.Goal{}, &po.GoalTask{}, &po.SpecialistRun{}, &po.GoalCheckpoint{}, &po.ScheduleTrigger{}); err != nil {
		t.Fatal(err)
	}
	return NewService(repo.NewStore(data.New(db)))
}
