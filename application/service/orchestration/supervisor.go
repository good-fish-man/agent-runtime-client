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
	protocol "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"
	log "github.com/good-fish-man/logx"
)

const (
	defaultSupervisorInterval    = 3 * time.Second
	defaultSupervisorConcurrency = 2
	defaultSupervisorGoalLimit   = 100
)

var resultURLPattern = regexp.MustCompile(`https?://[^\s<>\[\]()"']+`)

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

	mu     sync.Mutex
	active map[string]struct{}
	sem    chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewSupervisor(goals *Service, runtime streamExecutor, devices deviceCatalog, config SupervisorConfig) *Supervisor {
	if config.ScanInterval <= 0 {
		config.ScanInterval = defaultSupervisorInterval
	}
	if config.MaxConcurrentRuns <= 0 {
		config.MaxConcurrentRuns = defaultSupervisorConcurrency
	}
	return &Supervisor{goals: goals, runtime: runtime, devices: devices, config: config, active: make(map[string]struct{}), sem: make(chan struct{}, config.MaxConcurrentRuns)}
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
	_ = s.RunOnce(ctx)
	ticker := time.NewTicker(s.config.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.ErrorwCtx(ctx, "orchestration supervisor scan failed", "error_chain", log.FormatError(err))
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
			log.WarnwCtx(ctx, "orchestration goal dispatch deferred", "goal_id", goal.GoalID, "error_chain", log.FormatError(err))
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
	if _, exists := s.active[taskID]; exists {
		delete(s.active, taskID)
		<-s.sem
	}
	s.mu.Unlock()
}

func (s *Supervisor) execute(parent context.Context, goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, revision int64) {
	traceID := fmt.Sprintf("goal-%s-%s-%d", goal.GoalID, task.TaskID, task.Attempt)
	ctx := authctx.WithUserID(log.WithReqID(parent, traceID), goal.OwnerID)
	if task.Budget.MaxDurationMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(task.Budget.MaxDurationMS)*time.Millisecond)
		defer cancel()
	}
	world, err := s.goals.WorldSlice(ctx, goal.OwnerID, goal.GoalID, task.TaskID)
	if err != nil {
		s.recordFailure(ctx, goal, task, revision, err)
		return
	}
	values := map[string]any{
		"user_id": goal.OwnerID, "agent_id": goal.AgentID, "session_id": goal.ConversationID,
		"goal_id": goal.GoalID, "task_id": task.TaskID, "specialist": task.Specialist,
		"persistent_goal_execution": true, "world_slice": world,
		"execution_id": task.ExecutionID, "idempotency_scope": task.IdempotencyScope,
		"confirmed_effect_keys": confirmedEffects(s.goals, ctx, goal.OwnerID, goal.GoalID),
		"goal_budget":           goal.Budget, "task_budget": task.Budget,
	}
	request := &runtimedto.RunReq{
		Prompt:       specialistPrompt(goal, task, world),
		Context:      values,
		Capabilities: capabilityConfigs(task.RequiredCapabilities),
		Options: &runtimeentity.RunOptions{
			Stream: true, TimeoutMs: boundedInt32(task.Budget.MaxDurationMS), MaxIterations: 12,
			MaxToolCalls: boundedInt32(int64(task.Budget.MaxActions)), MaxTotalTokens: boundedInt32(task.Budget.MaxTokens),
		},
		RequestID: task.ExecutionID,
		DeviceID:  task.DeviceID,
	}
	capture := newSupervisorCapture()
	started := time.Now()
	err = s.runtime.RunStream(ctx, request, capture.emit)
	if parent.Err() != nil {
		return
	}
	result, requestData := capture.result(goal, task, values, traceID, time.Since(started), err)
	if recordErr := s.recordWithRetry(context.WithoutCancel(ctx), goal.OwnerID, goal.GoalID, task.TaskID, revision, result, requestData); recordErr != nil {
		log.ErrorwCtx(ctx, "orchestration specialist result persistence failed", "goal_id", goal.GoalID, "task_id", task.TaskID, "error_chain", log.FormatError(recordErr))
	}
}

func (s *Supervisor) recordFailure(ctx context.Context, goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, revision int64, runErr error) {
	result := orchestrationentity.SpecialistResult{ExecutionID: task.ExecutionID, Status: protocol.TaskFailed, Summary: runErr.Error(), Output: map[string]any{"error": runErr.Error()}, Provenance: orchestrationentity.Provenance{RunManifestID: "not-started", AgentBuildID: "not-started", ModelConfigVersion: "not-started", DeviceID: task.DeviceID, TraceID: fmt.Sprintf("goal-%s-%s-%d", goal.GoalID, task.TaskID, task.Attempt), ProducedAt: time.Now().UTC()}}
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
	content          strings.Builder
	done             *runtimeentity.DoneEvent
	actions          map[string]string
	confirmedEffects []string
	pendingApprovals []string
	evidence         []string
	observations     []string
	world            map[string]map[string]any
	searchQueries    int
	pages            int
	actionCount      int
	lastObservation  *controlentity.Observation
}

func newSupervisorCapture() *supervisorCapture {
	return &supervisorCapture{actions: make(map[string]string), world: make(map[string]map[string]any)}
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
			}
			if strings.Contains(name, "fetch") || strings.Contains(name, "read") {
				c.pages++
			}
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
			c.actions[event.Action.ActionID] = event.Action.IdempotencyKey
		}
	case runtimeentity.StreamTypeObservation:
		if event.Observation != nil {
			c.lastObservation = event.Observation
			if event.Observation.ObservationID != "" {
				c.observations = append(c.observations, event.Observation.ObservationID)
			}
			if event.Observation.Status == controlentity.ObservationSucceeded {
				if key := c.actions[event.Observation.ActionID]; key != "" {
					c.confirmedEffects = append(c.confirmedEffects, key)
				}
				if event.Observation.DeviceID != "" && len(event.Observation.State) > 0 {
					c.world[event.Observation.DeviceID] = cloneAnyMap(event.Observation.State)
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
		if isDeviceUnavailable(runErr, c.lastObservation) {
			status = protocol.TaskWaitingDevice
		}
		if content == "" {
			content = runErr.Error()
		}
	}
	if content == "" {
		content = nonEmptyResultSummary(status)
	}
	c.evidence = append(c.evidence, resultURLPattern.FindAllString(content, -1)...)
	criteria := verifiedCriteriaFromContent(content, goal.SuccessCriteria)
	result := orchestrationentity.SpecialistResult{
		ExecutionID: task.ExecutionID, Status: status, Summary: content, EvidenceRefs: uniqueStrings(c.evidence), ObservationRefs: uniqueStrings(c.observations), ConfirmedEffectKeys: uniqueStrings(c.confirmedEffects), Usage: usage,
		Output: map[string]any{"content": content, "finish_reason": finishReason},
		Provenance: orchestrationentity.Provenance{
			RunManifestID: stringValue(values["run_manifest_id"]), AgentBuildID: stringValue(values["agent_build_id"]),
			ModelConfigVersion: stringValue(values["model_config_version"]), DeviceID: task.DeviceID, TraceID: traceID, ProducedAt: time.Now().UTC(),
		},
	}
	if result.Provenance.RunManifestID == "" {
		result.Provenance.RunManifestID = "unavailable"
	}
	if result.Provenance.AgentBuildID == "" {
		result.Provenance.AgentBuildID = "unavailable"
	}
	if result.Provenance.ModelConfigVersion == "" {
		result.Provenance.ModelConfigVersion = "unavailable"
	}
	return result, RecordResultRequest{VerifiedCriteriaIDs: criteria, ConfirmedEffectKeys: uniqueStrings(c.confirmedEffects), PendingApprovalIDs: uniqueStrings(c.pendingApprovals), WorldSlicesByDevice: c.world}
}

func specialistPrompt(goal orchestrationentity.PersistentGoal, task orchestrationentity.SpecialistTask, world map[string]map[string]any) string {
	criteria, _ := json.Marshal(goal.SuccessCriteria)
	constraints, _ := json.Marshal(goal.Constraints)
	worldJSON, _ := json.Marshal(world)
	return fmt.Sprintf(`You are the bounded %s specialist for a durable Athena goal.
Goal: %s
Task: %s
Constraints: %s
Success criteria: %s
Declared world slice: %s

Use only registered capabilities and the supplied world slice. Do not create another persistent goal, hidden sub-agent, executable code, or unbounded loop. Never repeat an idempotency key listed in confirmed_effect_keys. Browser and desktop effects count only after a real device observation. Return a concise, verifiable result. If a criterion is proven, include a final JSON object with key "verified_criteria_ids" and its exact criterion IDs.`, task.Specialist, goal.Objective, task.Objective, constraints, criteria, worldJSON)
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
		if task.TaskID == id {
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

func cloneAnyMap(values map[string]any) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
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
