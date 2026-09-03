package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	runtimesvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	orchestrationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/dbctx"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	protocol "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"
	log "github.com/good-fish-man/logx"
)

const (
	defaultSupervisorInterval    = 3 * time.Second
	defaultSupervisorConcurrency = 2
	defaultSupervisorGoalLimit   = 100
)

var resultURLPattern = regexp.MustCompile(`https?://[^\s<>\[\]()"']+`)

var errSpecialistBudgetExhausted = errors.New("specialist execution budget exhausted")

type streamExecutor interface {
	RunStream(context.Context, *runtimedto.RunReq, runtimesvc.StreamFunc) error
}

type deviceCatalog interface {
	Devices(context.Context, string) ([]controlsvc.Device, error)
}

type SupervisorConfig struct {
	ScanInterval      time.Duration
	MaxConcurrentRuns int
}

// Supervisor advances durable goals through the existing Runtime and Control
// Hub. It never executes generated code and never fabricates a device result:
// browser and desktop specialists complete only after a real Observation.
type Supervisor struct {
	goals   *Service
	runtime streamExecutor
	devices deviceCatalog
	config  SupervisorConfig

	mu      sync.Mutex
	active  map[string]struct{}
	cancels map[string]activeCancellation
	sem     chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

type activeCancellation struct {
	goalID string
	cancel context.CancelFunc
}

func NewSupervisor(goals *Service, runtime streamExecutor, devices deviceCatalog, config SupervisorConfig) *Supervisor {
	if config.ScanInterval <= 0 {
		config.ScanInterval = defaultSupervisorInterval
	}
	if config.MaxConcurrentRuns <= 0 {
		config.MaxConcurrentRuns = defaultSupervisorConcurrency
	}
	supervisor := &Supervisor{goals: goals, runtime: runtime, devices: devices, config: config, active: make(map[string]struct{}), cancels: make(map[string]activeCancellation), sem: make(chan struct{}, config.MaxConcurrentRuns)}
	if goals != nil {
		goals.SetGoalCanceller(supervisor.CancelGoal)
	}
	return supervisor
}

func (s *Supervisor) Start(parent context.Context) error {
	if s == nil || s.goals == nil || s.runtime == nil {
		return fmt.Errorf("orchestration supervisor is not configured")
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := s.goals.RecoverInterruptedGoals(parent); err != nil {
		return log.WrapError(err, "OrchestrationSupervisor.Start.recover")
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		cancel()
		return nil
	}
	s.cancel = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go s.loop(ctx)
	return nil
}

func (s *Supervisor) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *Supervisor) loop(ctx context.Context) {
	defer s.wg.Done()
	dbCtx := dbctx.SuppressQueryInfo(ctx)
	_ = s.RunOnce(dbCtx)
	ticker := time.NewTicker(s.config.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RunOnce(dbCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Errorw(ctx, "orchestration supervisor scan failed", "error_chain", log.FormatError(err))
			}
		}
	}
}

// RunOnce scans all runnable owners and dispatches every currently eligible
// specialist for which local worker capacity is available.
func (s *Supervisor) RunOnce(ctx context.Context) error {
	goals, err := s.goals.ListRunnableGoals(ctx, defaultSupervisorGoalLimit)
	if err != nil {
		return log.WrapError(err, "OrchestrationSupervisor.RunOnce.list")
	}
	for _, goal := range goals {
		if err := s.dispatchGoal(ctx, goal); err != nil && !errors.Is(err, context.Canceled) {
			log.Warnw(ctx, "orchestration goal dispatch deferred", "goal_id", goal.GoalID, "error_chain", log.FormatError(err))
		}
	}
	return nil
}

func (s *Supervisor) dispatchGoal(ctx context.Context, goal orchestrationentity.PersistentGoal) error {
	state, err := s.goals.GetGoal(ctx, goal.OwnerID, goal.GoalID)
	if err != nil {
		return err
	}
	if requiresExplicitResume(state.Tasks) {
		return nil
	}
	devices, err := s.deviceCandidates(ctx, goal.OwnerID)
	if err != nil {
		return err
	}
	for _, task := range state.Tasks {
		if task.Status != protocol.TaskReady && task.Status != protocol.TaskWaitingDevice {
			continue
		}
		if !task.NextAttemptAt.IsZero() && time.Now().UTC().Before(task.NextAttemptAt) {
			continue
		}
		if !s.reserve(task.TaskID) {
			continue
		}
		decision, next, startErr := s.goals.StartTask(ctx, goal.OwnerID, goal.GoalID, task.TaskID, StartTaskRequest{ExpectedRevision: state.Goal.Revision, Devices: devices})
		if startErr != nil {
			s.release(task.TaskID)
			state, _ = s.goals.GetGoal(ctx, goal.OwnerID, goal.GoalID)
			continue
		}
		state = next
		if decision == nil || decision.Status != protocol.TaskRunning {
			s.release(task.TaskID)
			continue
		}
		running, ok := findTask(next.Tasks, task.TaskID)
		if !ok {
			s.release(task.TaskID)
			continue
		}
		s.wg.Add(1)
		go func(goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, revision int64) {
			defer s.wg.Done()
			defer s.release(task.TaskID)
			s.execute(ctx, goal, task, revision)
		}(next.Goal, running, next.Goal.Revision)
	}
	return nil
}

func (s *Supervisor) reserve(taskID string) bool {
	s.mu.Lock()
	if _, exists := s.active[taskID]; exists {
		s.mu.Unlock()
		return false
	}
	select {
	case s.sem <- struct{}{}:
		s.active[taskID] = struct{}{}
		s.mu.Unlock()
		return true
	default:
		s.mu.Unlock()
		return false
	}
}

func (s *Supervisor) release(taskID string) {
	s.mu.Lock()
	delete(s.cancels, taskID)
	if _, exists := s.active[taskID]; exists {
		delete(s.active, taskID)
		<-s.sem
	}
	s.mu.Unlock()
}

// CancelGoal interrupts every in-flight specialist after the Service has
// durably persisted the PAUSED state. Late results are rejected because their
// task execution is no longer RUNNING.
func (s *Supervisor) CancelGoal(goalID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0)
	for _, active := range s.cancels {
		if active.goalID == goalID && active.cancel != nil {
			cancels = append(cancels, active.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Supervisor) execute(parent context.Context, goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, revision int64) {
	parent, stop := context.WithCancel(parent)
	s.mu.Lock()
	s.cancels[task.TaskID] = activeCancellation{goalID: goal.GoalID, cancel: stop}
	s.mu.Unlock()
	defer stop()

	// Pause can race with the goroutine launch before its cancellation handle is
	// registered. Re-reading the durable execution identity closes that window:
	// either Pause cancels this registered context, or this check observes that
	// the task is no longer RUNNING and avoids invoking Runtime altogether.
	active, err := s.executionIsActive(parent, goal.OwnerID, goal.GoalID, task)
	if err != nil {
		s.recordFailure(parent, goal, task, revision, err)
		return
	}
	if !active {
		return
	}

	traceID := fmt.Sprintf("goal-%s-%s-%d", goal.GoalID, task.TaskID, task.Attempt)
	ctx := authctx.WithUserID(log.WithReqID(parent, traceID), goal.OwnerID)
	budget := remainingExecutionBudget(goal, task)
	if !goal.Deadline.IsZero() {
		remaining := time.Until(goal.Deadline).Milliseconds()
		if remaining > 0 && remaining < budget.MaxDurationMS {
			budget.MaxDurationMS = remaining
		}
	}
	if budget.MaxDurationMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(budget.MaxDurationMS)*time.Millisecond)
		defer cancel()
	}
	world, err := s.goals.WorldSlice(ctx, goal.OwnerID, goal.GoalID, task.TaskID)
	if err != nil {
		s.recordFailure(ctx, goal, task, revision, err)
		return
	}
	values := map[string]any{
		"user_id": goal.OwnerID, "agent_id": goal.AgentID, "session_id": goal.ConversationID,
		"goal_id": goal.GoalID, "task_id": task.TaskID, "task_key": task.TaskKey, "specialist": task.Specialist,
		"persistent_goal_execution": true, "world_slice": world,
		"execution_id": task.ExecutionID, "idempotency_scope": task.IdempotencyScope,
		"confirmed_effect_keys": confirmedEffects(s.goals, ctx, goal.OwnerID, goal.GoalID),
		"goal_budget":           goal.Budget, "task_budget": budget,
	}
	request := &runtimedto.RunReq{
		Prompt:       specialistPrompt(goal, task, world),
		Context:      values,
		Capabilities: capabilityConfigs(task.RequiredCapabilities),
		Options: &runtimeentity.RunOptions{
			Stream: true, TimeoutMs: boundedInt32(budget.MaxDurationMS), MaxIterations: 12,
			MaxToolCalls: boundedInt32(int64(budget.MaxSearchQueries + budget.MaxPages + budget.MaxActions)), MaxTotalTokens: boundedInt32(budget.MaxTokens),
		},
		RequestID: task.ExecutionID,
		DeviceID:  task.DeviceID,
	}
	capture := newSupervisorCapture(budget)
	started := time.Now()
	err = s.runtime.RunStream(ctx, request, capture.emit)
	if parent.Err() != nil {
		return
	}
	result, requestData := capture.result(goal, task, values, traceID, time.Since(started), err)
	if recordErr := s.recordWithRetry(context.WithoutCancel(ctx), goal.OwnerID, goal.GoalID, task.TaskID, revision, result, requestData); recordErr != nil {
		log.Errorw(ctx, "orchestration specialist result persistence failed", "goal_id", goal.GoalID, "task_id", task.TaskID, "error_chain", log.FormatError(recordErr))
	}
}

func (s *Supervisor) executionIsActive(ctx context.Context, ownerID, goalID string, expected orchestrationentity.SpecialistTask) (bool, error) {
	state, err := s.goals.GetGoal(ctx, ownerID, goalID)
	if err != nil {
		return false, log.WrapError(err, "OrchestrationSupervisor.executionIsActive.load")
	}
	current, ok := findTask(state.Tasks, expected.TaskID)
	return ok && current.Status == protocol.TaskRunning && current.ExecutionID == expected.ExecutionID, nil
}

func (s *Supervisor) recordFailure(ctx context.Context, goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, revision int64, runErr error) {
	result := orchestrationentity.SpecialistResult{ExecutionID: task.ExecutionID, Status: protocol.TaskFailed, Summary: runErr.Error(), Output: map[string]any{"error": runErr.Error()}, Provenance: controlPlaneProvenance(task.DeviceID, fmt.Sprintf("goal-%s-%s-%d", goal.GoalID, task.TaskID, task.Attempt))}
	_ = s.recordWithRetry(context.WithoutCancel(ctx), goal.OwnerID, goal.GoalID, task.TaskID, revision, result, RecordResultRequest{})
}

func (s *Supervisor) recordWithRetry(ctx context.Context, ownerID, goalID, taskID string, revision int64, result orchestrationentity.SpecialistResult, data RecordResultRequest) error {
	for attempt := 0; attempt < 4; attempt++ {
		data.ExpectedRevision, data.Result = revision, result
		if _, err := s.goals.RecordResult(ctx, ownerID, goalID, taskID, data); err == nil {
			return nil
		} else if attempt == 3 {
			return err
		}
		state, err := s.goals.GetGoal(ctx, ownerID, goalID)
		if err != nil {
			return err
		}
		current, ok := findTask(state.Tasks, taskID)
		if !ok || current.Status != protocol.TaskRunning {
			return nil
		}
		revision = state.Goal.Revision
	}
	return nil
}

func (s *Supervisor) deviceCandidates(ctx context.Context, ownerID string) ([]orchestrationentity.DeviceCandidate, error) {
	if s.devices == nil {
		return nil, nil
	}
	devices, err := s.devices.Devices(ctx, ownerID)
	if err != nil {
		return nil, log.WrapError(err, "OrchestrationSupervisor.deviceCandidates")
	}
	result := make([]orchestrationentity.DeviceCandidate, 0, len(devices))
	for _, device := range devices {
		result = append(result, orchestrationentity.DeviceCandidate{DeviceID: device.ID, Online: device.Online, Capabilities: append([]string(nil), device.Capabilities...)})
	}
	return result, nil
}

type supervisorCapture struct {
	content                strings.Builder
	done                   *runtimeentity.DoneEvent
	actions                map[string]string
	confirmedEffects       []string
	pendingApprovals       []string
	evidence               []string
	observations           []string
	world                  map[string]map[string]any
	searchQueries          int
	pages                  int
	actionCount            int
	successfulObservations int
	lastObservation        *controlentity.Observation
	budget                 orchestrationentity.TaskBudget
}

func newSupervisorCapture(budget orchestrationentity.TaskBudget) *supervisorCapture {
	return &supervisorCapture{actions: make(map[string]string), world: make(map[string]map[string]any), budget: budget}
}

func (c *supervisorCapture) emit(event *runtimeentity.StreamEvent) error {
	if event == nil {
		return nil
	}
	switch event.Type {
	case runtimeentity.StreamTypeDelta:
		if event.Delta != nil && !event.Delta.Reasoning {
			c.content.WriteString(event.Delta.Text)
		}
	case runtimeentity.StreamTypeToolCall:
		if event.ToolCall != nil {
			name := strings.ToLower(event.ToolCall.Tool)
			if strings.Contains(name, "search") {
				c.searchQueries++
				if c.searchQueries > c.budget.MaxSearchQueries {
					return fmt.Errorf("%w: search query limit %d", errSpecialistBudgetExhausted, c.budget.MaxSearchQueries)
				}
			}
			if strings.Contains(name, "fetch") || strings.Contains(name, "read") {
				c.pages++
				if c.pages > c.budget.MaxPages {
					return fmt.Errorf("%w: page limit %d", errSpecialistBudgetExhausted, c.budget.MaxPages)
				}
			}
		}
	case runtimeentity.StreamTypeToolResult:
		if event.ToolResult != nil && event.ToolResult.Success {
			c.evidence = append(c.evidence, evidenceRefsFromValue(event.ToolResult.Output)...)
		}
	case runtimeentity.StreamTypeInterrupted:
		if event.Interrupted != nil {
			for _, approval := range event.Interrupted.PendingApprovals {
				c.pendingApprovals = append(c.pendingApprovals, approval.InterruptID)
			}
		}
	case runtimeentity.StreamTypeAction:
		if event.Action != nil {
			c.actionCount++
			if c.actionCount > c.budget.MaxActions {
				return fmt.Errorf("%w: action limit %d", errSpecialistBudgetExhausted, c.budget.MaxActions)
			}
			c.actions[event.Action.ActionID] = event.Action.IdempotencyKey
		}
	case runtimeentity.StreamTypeObservation:
		if event.Observation != nil {
			c.lastObservation = event.Observation
			if event.Observation.ObservationID != "" {
				c.observations = append(c.observations, event.Observation.ObservationID)
			}
			if event.Observation.Status == controlentity.ObservationSucceeded {
				if event.Observation.ObservationID != "" {
					c.successfulObservations++
				}
				if key := c.actions[event.Observation.ActionID]; key != "" {
					c.confirmedEffects = append(c.confirmedEffects, key)
				}
				if event.Observation.DeviceID != "" && len(event.Observation.State) > 0 {
					c.world[event.Observation.DeviceID] = safeWorldState(event.Observation.State)
				}
			}
			for _, evidence := range event.Observation.Evidence {
				if evidence.URI != "" {
					c.evidence = append(c.evidence, evidence.URI)
				} else if evidence.EvidenceID != "" {
					c.evidence = append(c.evidence, evidence.EvidenceID)
				}
			}
		}
	case runtimeentity.StreamTypeDone:
		if event.Done != nil {
			copy := *event.Done
			c.done = &copy
			if strings.TrimSpace(copy.Content) != "" {
				c.content.Reset()
				c.content.WriteString(copy.Content)
			}
		}
	}
	return nil
}

func (c *supervisorCapture) result(goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, values map[string]any, traceID string, duration time.Duration, runErr error) (orchestrationentity.SpecialistResult, RecordResultRequest) {
	content := strings.TrimSpace(c.content.String())
	status := protocol.TaskCompleted
	finishReason := "stop"
	usage := orchestrationentity.BudgetUsage{DurationMS: duration.Milliseconds(), SearchQueries: c.searchQueries, Pages: c.pages, Actions: c.actionCount}
	if c.done != nil {
		finishReason = c.done.FinishReason
		usage.Tokens = int64(c.done.TotalTokens)
		if usage.Tokens == 0 && c.done.Metadata != nil {
			usage.Tokens = int64(c.done.Metadata.TokensUsed)
		}
	}
	if finishReason == "waiting_user" || finishReason == "waiting_approval" {
		status = protocol.TaskWaitingUser
	}
	if runErr != nil {
		status = protocol.TaskFailed
		if errors.Is(runErr, errSpecialistBudgetExhausted) || errors.Is(runErr, context.DeadlineExceeded) {
			status = protocol.TaskWaitingUser
		} else if isDeviceUnavailable(runErr, c.lastObservation) {
			status = protocol.TaskWaitingDevice
		}
		if content == "" {
			content = runErr.Error()
		}
	}
	if content == "" {
		content = nonEmptyResultSummary(status)
	}
	criteria := verifiedCriteriaFromContent(content, goal.SuccessCriteria)
	if task.RequiresDevice && status == protocol.TaskCompleted && c.successfulObservations == 0 {
		status = protocol.TaskFailed
		content = "device specialist result rejected because no successful device observation was received"
		criteria = nil
		c.confirmedEffects = nil
		c.world = nil
	}
	provenance := orchestrationentity.Provenance{
		ProducerType: protocol.ProvenanceRuntimeRun, Producer: "agent-runtime", ProducerVersion: consts.Version,
		RunManifestID: stringValue(values["run_manifest_id"]), AgentBuildID: stringValue(values["agent_build_id"]),
		ModelConfigVersion: stringValue(values["model_config_version"]), DeviceID: task.DeviceID, TraceID: traceID, ProducedAt: time.Now().UTC(),
	}
	if provenance.RunManifestID == "" || provenance.AgentBuildID == "" || provenance.ModelConfigVersion == "" {
		status = protocol.TaskFailed
		content = "runtime result rejected because deployment provenance was not published"
		c.evidence, c.observations, c.confirmedEffects, c.world = nil, nil, nil, nil
		provenance = controlPlaneProvenance(task.DeviceID, traceID)
	}
	result := orchestrationentity.SpecialistResult{
		ExecutionID: task.ExecutionID, Status: status, Summary: content, EvidenceRefs: uniqueStrings(c.evidence), ObservationRefs: uniqueStrings(c.observations), ConfirmedEffectKeys: uniqueStrings(c.confirmedEffects), Usage: usage,
		Output:     map[string]any{"content": content, "finish_reason": finishReason},
		Provenance: provenance,
	}
	return result, RecordResultRequest{VerifiedCriteriaIDs: criteria, ConfirmedEffectKeys: uniqueStrings(c.confirmedEffects), PendingApprovalIDs: uniqueStrings(c.pendingApprovals), WorldSlicesByDevice: c.world}
}

func specialistPrompt(goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, world map[string]map[string]any) string {
	criteria, _ := json.Marshal(goal.SuccessCriteria)
	constraints, _ := json.Marshal(goal.Constraints)
	worldJSON, _ := json.Marshal(world)
	inputs, _ := json.Marshal(goal.Inputs)
	return fmt.Sprintf(`You are the bounded %s specialist for a durable Athena goal.
Goal: %s
Task: %s
Constraints: %s
Success criteria: %s
Declared world slice: %s
User clarifications: %s

Use only registered capabilities and the supplied world slice. Do not create another persistent goal, hidden sub-agent, executable code, or unbounded loop. Never repeat an idempotency key listed in confirmed_effect_keys. Browser and desktop effects count only after a real device observation. If essential information is missing, ask one focused question and stop with finish_reason waiting_user. Return a concise, verifiable result. If a criterion is proven, include a final JSON object with key "verified_criteria_ids" and its exact criterion IDs.`, task.Specialist, goal.Objective, task.Objective, constraints, criteria, worldJSON, inputs)
}

func capabilityConfigs(ids []string) []runtimeentity.CapabilityConfig {
	result := make([]runtimeentity.CapabilityConfig, 0, len(ids))
	for _, id := range uniqueStrings(ids) {
		result = append(result, runtimeentity.CapabilityConfig{ID: id})
	}
	return result
}

func confirmedEffects(service *Service, ctx context.Context, ownerID, goalID string) []string {
	checkpoints, err := service.ListCheckpoints(ctx, ownerID, goalID, 1)
	if err != nil || len(checkpoints) == 0 {
		return nil
	}
	return append([]string(nil), checkpoints[0].ConfirmedEffectKeys...)
}

func verifiedCriteriaFromContent(content string, criteria []orchestrationentity.SuccessCriterion) []string {
	allowed := make(map[string]struct{}, len(criteria))
	for _, criterion := range criteria {
		allowed[criterion.CriterionID] = struct{}{}
	}
	var payload struct {
		Verified []string `json:"verified_criteria_ids"`
	}
	start, end := strings.LastIndex(content, "{"), strings.LastIndex(content, "}")
	if start >= 0 && end > start && json.Unmarshal([]byte(content[start:end+1]), &payload) == nil {
		result := make([]string, 0, len(payload.Verified))
		for _, id := range payload.Verified {
			if _, ok := allowed[id]; ok {
				result = append(result, id)
			}
		}
		return uniqueStrings(result)
	}
	return nil
}

func requiresExplicitResume(tasks []orchestrationentity.SpecialistTask) bool {
	for _, task := range tasks {
		if task.Status == protocol.TaskWaitingUser || task.Status == protocol.TaskFailed {
			return true
		}
	}
	return false
}

func findTask(tasks []orchestrationentity.SpecialistTask, id string) (orchestrationentity.SpecialistTask, bool) {
	for _, task := range tasks {
		if task.TaskID == id || task.TaskKey == id {
			return task, true
		}
	}
	return orchestrationentity.SpecialistTask{}, false
}

func isDeviceUnavailable(err error, observation *controlentity.Observation) bool {
	value := strings.ToLower(err.Error())
	if strings.Contains(value, "device is offline") || strings.Contains(value, "desktop is not connected") || strings.Contains(value, "connection") {
		return true
	}
	return observation != nil && observation.Status == controlentity.ObservationFailed && strings.Contains(strings.ToLower(observation.Error), "not connected")
}

func boundedInt32(value int64) int32 {
	if value <= 0 {
		return 1
	}
	if value > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}

func remainingExecutionBudget(goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask) orchestrationentity.TaskBudget {
	remainingInt64 := func(taskLimit, taskUsed, goalLimit, goalUsed int64) int64 {
		left := taskLimit - taskUsed
		if goalLeft := goalLimit - goalUsed; goalLeft < left {
			left = goalLeft
		}
		if left < 1 {
			return 1
		}
		return left
	}
	remainingInt := func(taskLimit, taskUsed, goalLimit, goalUsed int) int {
		left := taskLimit - taskUsed
		if goalLeft := goalLimit - goalUsed; goalLeft < left {
			left = goalLeft
		}
		if left < 1 {
			return 1
		}
		return left
	}
	return orchestrationentity.TaskBudget{
		MaxTokens:        remainingInt64(task.Budget.MaxTokens, task.Usage.Tokens, goal.Budget.MaxTokens, goal.Usage.Tokens),
		MaxDurationMS:    remainingInt64(task.Budget.MaxDurationMS, task.Usage.DurationMS, goal.Budget.MaxDurationMS, goal.Usage.DurationMS),
		MaxSearchQueries: remainingInt(task.Budget.MaxSearchQueries, task.Usage.SearchQueries, goal.Budget.MaxSearchQueries, goal.Usage.SearchQueries),
		MaxPages:         remainingInt(task.Budget.MaxPages, task.Usage.Pages, goal.Budget.MaxPages, goal.Usage.Pages),
		MaxActions:       remainingInt(task.Budget.MaxActions, task.Usage.Actions, goal.Budget.MaxActions, goal.Usage.Actions),
	}
}

func controlPlaneProvenance(deviceID, traceID string) orchestrationentity.Provenance {
	return orchestrationentity.Provenance{
		ProducerType: protocol.ProvenanceControlPlane, Producer: "orchestration-supervisor", ProducerVersion: consts.Version,
		DeviceID: deviceID, TraceID: traceID, ProducedAt: time.Now().UTC(),
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimRight(value, ".,;"))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func evidenceRefsFromValue(value any) []string {
	refs := make([]string, 0)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case string:
			refs = append(refs, resultURLPattern.FindAllString(typed, -1)...)
		case []string:
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return uniqueStrings(refs)
}

const (
	maxWorldSliceDepth   = 6
	maxWorldSliceEntries = 64
	maxWorldStringRunes  = 4096
)

func safeWorldState(values map[string]any) map[string]any {
	value, ok := safeWorldValue(values, 0)
	if !ok {
		return map[string]any{}
	}
	result, _ := value.(map[string]any)
	return result
}

func safeWorldValue(value any, depth int) (any, bool) {
	if depth > maxWorldSliceDepth {
		return nil, false
	}
	switch typed := value.(type) {
	case nil, bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		return typed, true
	case string:
		runes := []rune(typed)
		if len(runes) > maxWorldStringRunes {
			typed = string(runes[:maxWorldStringRunes])
		}
		return typed, true
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if !sensitiveWorldKey(key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if len(keys) > maxWorldSliceEntries {
			keys = keys[:maxWorldSliceEntries]
		}
		result := make(map[string]any, len(keys))
		for _, key := range keys {
			if child, ok := safeWorldValue(typed[key], depth+1); ok {
				result[key] = child
			}
		}
		return result, true
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for key, item := range typed {
			converted[key] = item
		}
		return safeWorldValue(converted, depth)
	case []any:
		limit := len(typed)
		if limit > maxWorldSliceEntries {
			limit = maxWorldSliceEntries
		}
		result := make([]any, 0, limit)
		for _, item := range typed[:limit] {
			if child, ok := safeWorldValue(item, depth+1); ok {
				result = append(result, child)
			}
		}
		return result, true
	case []string:
		limit := len(typed)
		if limit > maxWorldSliceEntries {
			limit = maxWorldSliceEntries
		}
		result := make([]any, 0, limit)
		for _, item := range typed[:limit] {
			child, _ := safeWorldValue(item, depth+1)
			result = append(result, child)
		}
		return result, true
	default:
		body, err := json.Marshal(typed)
		if err != nil || len(body) > maxWorldStringRunes*maxWorldSliceEntries {
			return nil, false
		}
		var decoded any
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, false
		}
		return safeWorldValue(decoded, depth+1)
	}
}

func sensitiveWorldKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"password", "passwd", "secret", "token", "cookie", "credential", "authorization", "api_key", "apikey"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func nonEmptyResultSummary(status string) string {
	if status == protocol.TaskCompleted {
		return "specialist completed without visible content"
	}
	return "specialist paused without visible content"
}
