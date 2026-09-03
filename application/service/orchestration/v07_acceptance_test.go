package orchestration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	runtimesvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/orchestration"
	protocol "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"
)

func TestV07TravelGoalAsksOneGapThenProducesSourcedFiveDayPlan(t *testing.T) {
	service := newService(t)
	runtime := &travelRuntime{}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{MaxConcurrentRuns: 2})
	state, err := service.CreatePlannedGoal(context.Background(), "traveler", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{
			AgentID: "travel-agent", Objective: "Create a five-day Hokkaido itinerary for next month",
			Constraints:     []string{"use current official sources", "do not book or purchase"},
			SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "sourced-plan", Description: "weather, transport, lodging, and interests are reflected in a sourced five-day plan", Required: true}},
		},
		Tasks: []PlanTaskRequest{
			{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "research weather, transport, lodging, and identify missing traveler preferences", RequiredCapabilities: []string{"internet.search", "internet.fetch"}},
			{TaskID: "synthesis", Depth: 2, Specialist: protocol.SpecialistSynthesis, Objective: "produce the sourced five-day itinerary", DependsOn: []string{"research"}, WorldSliceRefs: []string{"result.research.summary", "result.research.evidence_refs"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "traveler", state.Goal.GoalID, protocol.GoalWaitingUser)
	if len(state.Results) != 1 || !strings.Contains(state.Results[0].Summary, "driving") {
		t.Fatalf("research did not ask the focused travel gap: %+v", state.Results)
	}
	answer := "No driving. Moderate budget. I prefer nature, hot springs, and quiet local food."
	if _, err := service.ResumeWithInput(context.Background(), "traveler", state.Goal.GoalID, answer, state.Goal.Revision); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForTaskStatus(t, service, "traveler", state.Goal.GoalID, "synthesis", protocol.TaskReady)
	if len(state.Goal.Inputs) != 1 || state.Goal.Inputs[0].Content != answer {
		t.Fatalf("clarification was not durably retained: %+v", state.Goal.Inputs)
	}
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "traveler", state.Goal.GoalID, protocol.GoalCompleted)
	if !state.Goal.SuccessCriteria[0].Verified || len(state.Results) != 3 {
		t.Fatalf("travel plan was not verified after clarification: %+v", state)
	}
	if !runtime.synthesisReceivedFilteredWorld() {
		t.Fatal("synthesis did not receive the declared research world slice")
	}
	for _, result := range state.Results {
		if result.Provenance.ProducerType != protocol.ProvenanceRuntimeRun || result.Provenance.RunManifestID == "" {
			t.Fatalf("specialist result lacks runtime provenance: %+v", result.Provenance)
		}
	}
}

func TestV07RestartRecoversCheckpointWithoutForgettingConfirmedEffect(t *testing.T) {
	service, db := newServiceWithDB(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "continue the open browser task", SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "continued", Description: "task continued without repeating the open action", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "browser", Depth: 1, Specialist: protocol.SpecialistBrowser, Objective: "continue on the existing page", RequiredCapabilities: []string{"browser.observe"}, WorldSliceRefs: []string{"url", "cookie"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, state, err = service.StartTask(context.Background(), "owner-1", state.Goal.GoalID, "browser", StartTaskRequest{ExpectedRevision: state.Goal.Revision, Devices: []entity.DeviceCandidate{{DeviceID: "mac", Online: true, Capabilities: []string{"browser.observe"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveCheckpoint(context.Background(), "owner-1", state.Goal.GoalID, CheckpointRequest{ExpectedRevision: state.Goal.Revision, Reason: "browser open observation confirmed", ConfirmedEffectKeys: []string{"open-youtube-once"}, ObservationRefs: []string{"observation-open-youtube"}, WorldSlicesByDevice: map[string]map[string]any{"mac": {"url": "https://youtube.com", "cookie": "must-never-cross-specialists"}}}); err != nil {
		t.Fatal(err)
	}

	restarted := NewService(repo.NewStore(data.New(db)))
	if err := restarted.RecoverInterruptedGoals(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime := &resumeRuntime{t: t}
	supervisor := NewSupervisor(restarted, runtime, &fakeDeviceCatalog{devices: []controlsvc.Device{{ID: "mac", Online: true, Capabilities: []string{"browser.observe"}}}}, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, restarted, "owner-1", state.Goal.GoalID, protocol.GoalCompleted)
	if runtime.calls != 1 || len(state.Checkpoint.ConfirmedEffectKeys) != 1 || state.Checkpoint.ConfirmedEffectKeys[0] != "open-youtube-once" {
		t.Fatalf("restart lost idempotency evidence: calls=%d checkpoint=%+v", runtime.calls, state.Checkpoint)
	}
}

func TestV07OfflinePreferredDeviceReroutesWithObservedDeviceScope(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "open the guide on an available device", SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "opened", Description: "guide opened on an observed device", Required: true}}},
		Tasks: []PlanTaskRequest{{
			TaskID: "browser", Depth: 1, Specialist: protocol.SpecialistBrowser, Objective: "open the guide",
			RequiredCapabilities: []string{"browser.open"}, PreferredDeviceID: "mac", WorldSliceRefs: []string{"url"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	devices := &fakeDeviceCatalog{}
	runtime := &deviceObservationRuntime{}
	supervisor := NewSupervisor(service, runtime, devices, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalWaitingDevice)
	if runtime.device() != "" {
		t.Fatal("offline device task reached Runtime")
	}

	devices.devices = []controlsvc.Device{{ID: "windows", Online: true, Capabilities: []string{"browser.open"}}}
	if _, err := service.Resume(context.Background(), "owner-1", state.Goal.GoalID, state.Goal.Revision); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalCompleted)
	if runtime.device() != "windows" || state.Tasks[0].DeviceID != "windows" {
		t.Fatalf("task was not rerouted to the available device: runtime=%q task=%+v", runtime.device(), state.Tasks[0])
	}
	if state.Checkpoint.WorldSlicesByDevice["windows"]["url"] != "https://example.com/guide" || state.Checkpoint.WorldSlicesByDevice["mac"] != nil {
		t.Fatalf("world state crossed device scope: %+v", state.Checkpoint.WorldSlicesByDevice)
	}
	if len(state.Checkpoint.ConfirmedEffectKeys) != 1 || len(state.Checkpoint.ObservationRefs) != 1 || state.Results[0].Provenance.DeviceID != "windows" {
		t.Fatalf("observed effect or device provenance was not checkpointed: %+v", state)
	}
}

func TestV07RuntimeReceivesEveryRemainingHardLimit(t *testing.T) {
	service := newService(t)
	budget := entity.GoalBudget{MaxConcurrentSpecialists: 1, MaxDepth: 1, MaxTokens: 321, MaxDurationMS: 5000, MaxSearchQueries: 2, MaxPages: 3, MaxActions: 4}
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "bounded execution", Budget: budget, SuccessCriteria: []entity.SuccessCriterion{{CriterionID: "bounded", Description: "bounded answer", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "work within every limit"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &limitCapturingRuntime{seen: make(chan runtimeentity.RunOptions, 1)}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case options := <-runtime.seen:
		if options.TimeoutMs != 5000 || options.MaxTotalTokens != 321 || options.MaxToolCalls != 9 {
			t.Fatalf("Runtime did not receive hard limits: %+v", options)
		}
	case <-time.After(time.Second):
		t.Fatal("Runtime did not receive the bounded request")
	}
	waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalCompleted)
}

func TestV07ActionBudgetStopsBeforeUnboundedContinuation(t *testing.T) {
	service := newService(t)
	budget := entity.GoalBudget{MaxConcurrentSpecialists: 1, MaxDepth: 1, MaxTokens: 1000, MaxDurationMS: 5000, MaxSearchQueries: 1, MaxPages: 1, MaxActions: 1}
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "bounded device actions", Budget: budget, SuccessCriteria: []entity.SuccessCriterion{{Description: "finished", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "browser", Depth: 1, Specialist: protocol.SpecialistBrowser, Objective: "do at most one action", RequiredCapabilities: []string{"browser.open"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(service, &actionOverBudgetRuntime{}, &fakeDeviceCatalog{devices: []controlsvc.Device{{ID: "mac", Online: true, Capabilities: []string{"browser.open"}}}}, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalWaitingUser)
	if len(state.Results) != 1 || state.Results[0].Status != protocol.TaskWaitingUser || state.Tasks[0].Usage.Actions != 2 {
		t.Fatalf("action budget did not stop in a bounded state: %+v", state)
	}
}

func TestV07SpecialistBudgetStopsDuringStream(t *testing.T) {
	service := newService(t)
	budget := entity.GoalBudget{MaxConcurrentSpecialists: 1, MaxDepth: 1, MaxTokens: 1000, MaxDurationMS: 60000, MaxSearchQueries: 2, MaxPages: 2, MaxActions: 1}
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "bounded search", Budget: budget, SuccessCriteria: []entity.SuccessCriterion{{Description: "answer", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "search within budget"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &overBudgetRuntime{}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalWaitingUser)
	if runtime.emitted != 3 || state.Tasks[0].Usage.SearchQueries != 3 || state.Results[0].Status != protocol.TaskWaitingUser {
		t.Fatalf("stream budget did not stop deterministically: emitted=%d state=%+v", runtime.emitted, state)
	}
}

func TestV07SpecialistTimeBudgetCancelsRuntime(t *testing.T) {
	service := newService(t)
	budget := entity.GoalBudget{MaxConcurrentSpecialists: 1, MaxDepth: 1, MaxTokens: 1000, MaxDurationMS: 50, MaxSearchQueries: 2, MaxPages: 2, MaxActions: 1}
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "bounded duration", Budget: budget, SuccessCriteria: []entity.SuccessCriterion{{Description: "answer", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "research", Depth: 1, Specialist: protocol.SpecialistResearch, Objective: "stop at deadline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &deadlineRuntime{cancelled: make(chan struct{})}
	supervisor := NewSupervisor(service, runtime, nil, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalWaitingUser)
	select {
	case <-runtime.cancelled:
	default:
		t.Fatal("Runtime context was not cancelled at the specialist deadline")
	}
	if len(state.Results) != 1 || state.Results[0].Status != protocol.TaskWaitingUser {
		t.Fatalf("time budget did not enter a bounded state: %+v", state)
	}
}

func TestV07OfflineDeviceUsesDedicatedWaitingStateWithoutRevisionChurn(t *testing.T) {
	service := newService(t)
	state, err := service.CreatePlannedGoal(context.Background(), "owner-1", CreatePlannedGoalRequest{
		CreateGoalRequest: CreateGoalRequest{AgentID: "agent-1", Objective: "open browser", SuccessCriteria: []entity.SuccessCriterion{{Description: "opened", Required: true}}},
		Tasks:             []PlanTaskRequest{{TaskID: "browser", Depth: 1, Specialist: protocol.SpecialistBrowser, Objective: "open", RequiredCapabilities: []string{"browser.open"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	supervisor := NewSupervisor(service, &fakeSupervisorRuntime{}, &fakeDeviceCatalog{}, SupervisorConfig{MaxConcurrentRuns: 1})
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state = waitForGoalStatus(t, service, "owner-1", state.Goal.GoalID, protocol.GoalWaitingDevice)
	revision := state.Goal.Revision
	if state.Tasks[0].Status != protocol.TaskWaitingDevice || state.Tasks[0].NextAttemptAt.IsZero() {
		t.Fatalf("offline task lacks durable retry state: %+v", state.Tasks[0])
	}
	if err := supervisor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, err := service.GetGoal(context.Background(), "owner-1", state.Goal.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Goal.Revision != revision {
		t.Fatalf("offline scan churned durable checkpoints: before=%d after=%d", revision, again.Goal.Revision)
	}
}

type travelRuntime struct {
	mu            sync.Mutex
	researchCalls int
	filteredWorld bool
}

type deviceObservationRuntime struct {
	mu       sync.Mutex
	deviceID string
}

func (r *deviceObservationRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	r.mu.Lock()
	r.deviceID = request.DeviceID
	r.mu.Unlock()
	attachTestManifest(request, "device-browser")
	if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeAction, Action: &controlentity.Action{ActionID: "open-guide", IdempotencyKey: "open-guide-once"}}); err != nil {
		return err
	}
	if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeObservation, Observation: &controlentity.Observation{ObservationID: "observation-guide", ActionID: "open-guide", DeviceID: request.DeviceID, Status: controlentity.ObservationSucceeded, State: map[string]any{"url": "https://example.com/guide"}}}); err != nil {
		return err
	}
	if err := emitSourceToolResult(emit, "https://example.com/guide"); err != nil {
		return err
	}
	return emitDone(emit, "The guide is open on the observed device.\n{\"verified_criteria_ids\":[\"opened\"]}", "stop", 50)
}

func (r *deviceObservationRuntime) device() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deviceID
}

type limitCapturingRuntime struct{ seen chan runtimeentity.RunOptions }

func (r *limitCapturingRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	attachTestManifest(request, "bounded-options")
	r.seen <- *request.Options
	if err := emitSourceToolResult(emit, "https://example.com/bounded"); err != nil {
		return err
	}
	return emitDone(emit, "Bounded result.\n{\"verified_criteria_ids\":[\"bounded\"]}", "stop", 100)
}

type actionOverBudgetRuntime struct{}

func (*actionOverBudgetRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	attachTestManifest(request, "action-budget")
	for index := 0; index < 2; index++ {
		if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeAction, Action: &controlentity.Action{ActionID: fmt.Sprintf("action-%d", index), IdempotencyKey: fmt.Sprintf("effect-%d", index)}}); err != nil {
			return err
		}
	}
	return nil
}

func (r *travelRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	taskKey, _ := request.Context["task_key"].(string)
	attachTestManifest(request, taskKey)
	switch taskKey {
	case "research":
		r.mu.Lock()
		r.researchCalls++
		attempt := r.researchCalls
		r.mu.Unlock()
		if err := emitSourceToolResult(emit, "https://www.jma.go.jp/", "https://www.jrhokkaido.co.jp/", "https://www.visit-hokkaido.jp/"); err != nil {
			return err
		}
		if attempt == 1 {
			return emitDone(emit, "I found current weather, rail, and lodging sources. Before planning: will you be driving, what is your budget, and which interests matter most?", "waiting_user", 120)
		}
		if !strings.Contains(request.Prompt, "No driving") || !strings.Contains(request.Prompt, "hot springs") {
			return fmt.Errorf("durable clarification was not supplied to resumed research")
		}
		return emitDone(emit, "Verified official weather, rail, lodging, nature, hot-spring, and food options for a non-driving moderate-budget traveler.", "stop", 260)
	case "synthesis":
		world, _ := request.Context["world_slice"].(map[string]map[string]any)
		server := world[protocol.WorldScopeServer]
		r.mu.Lock()
		r.filteredWorld = server["result.research.summary"] != nil && server["result.research.evidence_refs"] != nil && len(server) == 2
		r.mu.Unlock()
		if err := emitSourceToolResult(emit, "https://www.jma.go.jp/", "https://www.jrhokkaido.co.jp/", "https://www.visit-hokkaido.jp/"); err != nil {
			return err
		}
		return emitDone(emit, "Day 1 Sapporo; Day 2 Otaru; Day 3 Noboribetsu; Day 4 Lake Toya; Day 5 Sapporo food and departure. Every day uses rail or bus and cites the verified official sources.\n{\"verified_criteria_ids\":[\"sourced-plan\"]}", "stop", 320)
	default:
		return fmt.Errorf("unexpected task %q", taskKey)
	}
}

func (r *travelRuntime) synthesisReceivedFilteredWorld() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.filteredWorld
}

type resumeRuntime struct {
	t     *testing.T
	calls int
}

func (r *resumeRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	r.calls++
	effects, _ := request.Context["confirmed_effect_keys"].([]string)
	if len(effects) != 1 || effects[0] != "open-youtube-once" {
		r.t.Fatalf("resumed runtime did not receive confirmed effects: %+v", effects)
	}
	world, _ := request.Context["world_slice"].(map[string]map[string]any)
	if world["mac"]["url"] != "https://youtube.com" {
		r.t.Fatalf("resumed runtime lost the device-scoped page: %+v", world)
	}
	if _, leaked := world["mac"]["cookie"]; leaked {
		r.t.Fatal("credential-like world state crossed the specialist boundary")
	}
	attachTestManifest(request, "browser-resume")
	if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeObservation, Observation: &controlentity.Observation{ObservationID: "observation-resumed-page", DeviceID: request.DeviceID, Status: controlentity.ObservationSucceeded, State: map[string]any{"url": "https://youtube.com"}}}); err != nil {
		return err
	}
	if err := emitSourceToolResult(emit, "https://youtube.com/"); err != nil {
		return err
	}
	return emitDone(emit, "Continued from the existing observed page without reopening it.\n{\"verified_criteria_ids\":[\"continued\"]}", "stop", 80)
}

type overBudgetRuntime struct{ emitted int }

func (r *overBudgetRuntime) RunStream(_ context.Context, request *runtimedto.RunReq, emit runtimesvc.StreamFunc) error {
	attachTestManifest(request, "bounded")
	for index := 0; index < 5; index++ {
		r.emitted++
		if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeToolCall, ToolCall: &runtimeentity.ToolCallEvent{ID: fmt.Sprintf("search-%d", index), Tool: "internet.search"}}); err != nil {
			return err
		}
	}
	return nil
}

type deadlineRuntime struct{ cancelled chan struct{} }

func (r *deadlineRuntime) RunStream(ctx context.Context, request *runtimedto.RunReq, _ runtimesvc.StreamFunc) error {
	attachTestManifest(request, "deadline")
	<-ctx.Done()
	close(r.cancelled)
	return ctx.Err()
}

func attachTestManifest(request *runtimedto.RunReq, suffix string) {
	request.Context["run_manifest_id"] = "manifest-" + suffix
	request.Context["agent_build_id"] = "build-" + suffix
	request.Context["model_config_version"] = "model-" + suffix
}

func emitSourceToolResult(emit runtimesvc.StreamFunc, urls ...string) error {
	values := make([]any, 0, len(urls))
	for _, url := range urls {
		values = append(values, map[string]any{"url": url})
	}
	return emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeToolResult, ToolResult: &runtimeentity.ToolResultEvent{ID: "sources", Tool: "internet.fetch", Success: true, Output: map[string]any{"sources": values}}})
}

func emitDone(emit runtimesvc.StreamFunc, content, reason string, tokens int32) error {
	return emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeDone, Done: &runtimeentity.DoneEvent{Content: content, FinishReason: reason, TotalTokens: tokens, FinishedAt: time.Now().UTC()}})
}

func waitForGoalStatus(t *testing.T, service *Service, ownerID, goalID, status string) *GoalState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.GetGoal(context.Background(), ownerID, goalID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Goal.Status == status {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _ := service.GetGoal(context.Background(), ownerID, goalID)
	t.Fatalf("goal did not reach %s: %+v", status, state)
	return nil
}

func waitForTaskStatus(t *testing.T, service *Service, ownerID, goalID, taskID, status string) *GoalState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.GetGoal(context.Background(), ownerID, goalID)
		if err != nil {
			t.Fatal(err)
		}
		if task, ok := findTask(state.Tasks, taskID); ok && task.Status == status {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _ := service.GetGoal(context.Background(), ownerID, goalID)
	t.Fatalf("task %s did not reach %s: %+v", taskID, status, state)
	return nil
}
