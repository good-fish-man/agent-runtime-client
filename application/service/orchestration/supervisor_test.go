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
	orchestrationrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/orchestration"
	"github.com/good-fish-man/agent-runtime-client/pkg/dbctx"
	protocol "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"
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
	if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeToolResult, ToolResult: &runtimeentity.ToolResultEvent{ID: "fetch-1", Tool: "internet.fetch", Success: true, Output: map[string]any{"url": "https://example.com/source"}}}); err != nil {
		return err
	}
	return emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeDone, Done: &runtimeentity.DoneEvent{
		Content: "verified output https://example.com/source\n{\"verified_criteria_ids\":[\"criterion-1\"]}", FinishReason: "stop", TotalTokens: 42, FinishedAt: time.Now().UTC(),
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

type pollingGoalStore struct {
	orchestrationrepo.Store
	contexts chan context.Context
}

func (s *pollingGoalStore) ListRunnableGoals(ctx context.Context, _ int) ([]orchestrationentity.PersistentGoal, error) {
	s.contexts <- ctx
	return nil, nil
}

func TestSupervisorLoopSuppressesRoutineQueryInfo(t *testing.T) {
	store := &pollingGoalStore{contexts: make(chan context.Context, 1)}
	supervisor := NewSupervisor(NewService(store), &fakeSupervisorRuntime{}, nil, SupervisorConfig{ScanInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	supervisor.wg.Add(1)
	go supervisor.loop(ctx)
	defer func() {
		cancel()
		supervisor.wg.Wait()
	}()

	select {
	case queryCtx := <-store.contexts:
		if !dbctx.QueryInfoSuppressed(queryCtx) {
			t.Fatal("supervisor polling did not suppress routine query info logs")
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not perform its initial goal scan")
	}
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

func TestSupervisorRejectsResultWithoutRuntimeDeploymentProvenance(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "attributable research", SuccessCriteria: []orchestrationentity.SuccessCriterion{{CriterionID: "criterion-1", Description: "verified", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "find evidence"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(service, &unprovenancedSupervisorRuntime{}, nil, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	current := waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalWaitingUser)
	if len(current.Results) != 1 || current.Results[0].Status != protocol.TaskFailed {
		t.Fatalf("unattributable result was not rejected: %+v", current.Results)
	}
	provenance := current.Results[0].Provenance
	if provenance.ProducerType != protocol.ProvenanceControlPlane || provenance.RunManifestID != "" || current.Goal.SuccessCriteria[0].Verified {
		t.Fatalf("unattributable output claimed runtime success: result=%+v goal=%+v", current.Results[0], current.Goal)
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
	if _, err = service.SaveCheckpoint(context.Background(), "owner-1", state.Goal.GoalID, CheckpointRequest{ExpectedRevision: state.Goal.Revision, Reason: "effect observed", ConfirmedEffectKeys: []string{"effect-1"}, ObservationRefs: []string{"observation-1"}}); err != nil {
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

func TestSupervisorStartIsIdempotentAndDoesNotRecoverItsOwnRunningTask(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "do not reset", SuccessCriteria: []orchestrationentity.SuccessCriterion{{Description: "done", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, state, err = service.StartTask(context.Background(), "owner-1", state.Goal.GoalID, "research", StartTaskRequest{ExpectedRevision: state.Goal.Revision})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &blockingSupervisorRuntime{started: make(chan struct{}), release: make(chan struct{})}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{ScanInterval: time.Hour, MaxConcurrentRuns: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := supervisor.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not dispatch recovered task")
	}
	if err := supervisor.Start(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Tasks[0].Status != protocol.TaskRunning || current.Tasks[0].ExecutionID == "" {
		t.Fatalf("duplicate Start reset a running task: %+v", current.Tasks[0])
	}
	close(runtime.release)
	supervisor.Stop()
}

func TestPauseCancelsInFlightSpecialistWithoutPersistingLateResult(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "pause safely", SuccessCriteria: []orchestrationentity.SuccessCriterion{{Description: "done", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "wait until paused"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &cancellableSupervisorRuntime{started: make(chan struct{}), cancelled: make(chan struct{})}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("specialist did not start")
	}
	running, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.Pause(context.Background(), "owner-1", state.Goal.GoalID, "user requested pause", running.Goal.Revision)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.cancelled:
	case <-time.After(time.Second):
		t.Fatal("pause did not cancel the in-flight Runtime call")
	}
	supervisor.Stop()

	current, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Goal.Status != protocol.GoalPaused || current.Goal.Status != protocol.GoalPaused {
		t.Fatalf("late runtime completion overwrote PAUSED state: pause=%s current=%s", paused.Goal.Status, current.Goal.Status)
	}
	if current.Tasks[0].Status != protocol.TaskReady || len(current.Results) != 0 {
		t.Fatalf("paused execution persisted a late result: task=%+v results=%+v", current.Tasks[0], current.Results)
	}
}

func TestSupervisorGlobalWorkerLimitIsHard(t *testing.T) {
	service := newService(t)
	for index := 0; index < 2; index++ {
		if _, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
			CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "global worker limit", SuccessCriteria: []orchestrationentity.SuccessCriterion{{Description: "done", Required: true}}},
			Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "block"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	runtime := &concurrencySupervisorRuntime{started: make(chan struct{}, 2), release: make(chan struct{})}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not dispatch any specialist")
	}
	time.Sleep(50 * time.Millisecond)
	if calls, maximum := runtime.snapshot(); calls != 1 || maximum != 1 {
		t.Fatalf("global worker budget was exceeded: calls=%d max_active=%d", calls, maximum)
	}
	close(runtime.release)
	supervisor.Stop()
}

type blockingSupervisorRuntime struct {
	started chan struct{}
	release chan struct{}
}

type unprovenancedSupervisorRuntime struct{}

func (*unprovenancedSupervisorRuntime) RunStream(_ context.Context, _ *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeToolResult, ToolResult: &runtimeentity.ToolResultEvent{ID: "fetch-1", Tool: "internet.fetch", Success: true, Output: map[string]any{"url": "https://example.com/source"}}}); err != nil {
		return err
	}
	return emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeDone, Done: &runtimeentity.DoneEvent{
		Content: "unsupported success\n{\"verified_criteria_ids\":[\"criterion-1\"]}", FinishReason: "stop", TotalTokens: 12, FinishedAt: time.Now().UTC(),
	}})
}

type cancellableSupervisorRuntime struct {
	started   chan struct{}
	cancelled chan struct{}
}

type concurrencySupervisorRuntime struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
	started   chan struct{}
	release   chan struct{}
}

func (r *concurrencySupervisorRuntime) RunStream(_ context.Context, _ *runtimedto.RunReq, _ runtimesvc.StreamFunc) error {
	r.mu.Lock()
	r.calls++
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()
	r.started <- struct{}{}
	<-r.release
	r.mu.Lock()
	r.active--
	r.mu.Unlock()
	return nil
}

func (r *concurrencySupervisorRuntime) snapshot() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.maxActive
}

func (b *cancellableSupervisorRuntime) RunStream(ctx context.Context, _ *runtimedto.RunReq, _ runtimesvc.StreamFunc) error {
	close(b.started)
	<-ctx.Done()
	close(b.cancelled)
	return ctx.Err()
}

func (b *blockingSupervisorRuntime) RunStream(ctx context.Context, _ *runtimedto.RunReq, _ runtimesvc.StreamFunc) error {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		return nil
	}
}
