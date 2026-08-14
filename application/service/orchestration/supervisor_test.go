package orchestration

import (
	"context"
	"sync"
	"testing"
	"time"

	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	runtimesvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	orchestrationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	protocol "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"
)

type fakeSupervisorRuntime struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeSupervisorRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	request.Context["run_manifest_id"] = "manifest-1"
	request.Context["agent_build_id"] = "build-1"
	request.Context["model_config_version"] = "model-1"
	return emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeDone, Done: &runtimeentity.DoneEvent{
		Content: "verified output\n{\"verified_criteria_ids\":[\"criterion-1\"]}", FinishReason: "stop", TotalTokens: 42, FinishedAt: time.Now().UTC(),
	}})
}

func (f *fakeSupervisorRuntime) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeDeviceCatalog struct{ devices []controlsvc.Device }

func (f *fakeDeviceCatalog) Devices(context.Context, string) ([]controlsvc.Device, error) {
	return append([]controlsvc.Device(nil), f.devices...), nil
}

func TestSupervisorCompletesBoundedServerSpecialist(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "durable research", SuccessCriteria: []orchestrationentity.SuccessCriterion{{CriterionID: "criterion-1", Description: "verified", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "find evidence"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeSupervisorRuntime{}
	supervisor := NewSupervisor(service, runtime, &fakeDeviceCatalog{}, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if current.Goal.Status == protocol.GoalCompleted {
			if len(current.Results) != 1 || current.Results[0].Provenance.RunManifestID != "manifest-1" || current.Goal.Usage.Tokens != 42 {
				t.Fatalf("result was not fully persisted: %+v", current)
			}
			if runtime.count() != 1 {
				t.Fatalf("runtime calls = %d", runtime.count())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("supervisor did not complete the goal")
}

func TestSupervisorWaitsForCompatibleDevice(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "open a page", SuccessCriteria: []orchestrationentity.SuccessCriterion{{CriterionID: "criterion-1", Description: "opened", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "browser", Depth: 1, Specialist: protocol.SpecialistBrowser, Objective: "open", RequiredCapabilities: []string{"browser.open"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeSupervisorRuntime{}
	devices := &fakeDeviceCatalog{devices: []controlsvc.Device{{ID: "mac", Online: false, Capabilities: []string{"browser.open"}}}}
	supervisor := NewSupervisor(service, runtime, devices, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
	if err != nil || current.Tasks[0].Status != protocol.TaskWaitingDevice || runtime.count() != 0 {
		t.Fatalf("offline task was dispatched: state=%+v calls=%d err=%v", current, runtime.count(), err)
	}
}

func TestRecoverInterruptedGoalPreservesCheckpointAndRetriesTask(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "resume", SuccessCriteria: []orchestrationentity.SuccessCriterion{{Description: "done", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, state, err = service.StartTask(context.Background(), "owner-1", state.Goal.GoalID, "research", StartTaskRequest{ExpectedRevision: state.Goal.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveCheckpoint(context.Background(), "owner-1", state.Goal.GoalID, CheckpointRequest{ExpectedRevision: state.Goal.Revision, Reason: "effect observed", ConfirmedEffectKeys: []string{"effect-1"}}); err != nil {
		t.Fatal(err)
	}
	if err = service.RecoverInterruptedGoals(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
	if err != nil || current.Tasks[0].Status != protocol.TaskReady || len(current.Checkpoint.ConfirmedEffectKeys) != 1 {
		t.Fatalf("interrupted goal was not recovered safely: state=%+v err=%v", current, err)
	}
}
