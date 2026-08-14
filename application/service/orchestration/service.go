package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/orchestration"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/orchestration"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	orchestrationv1 "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"
	log "github.com/good-fish-man/logx"
)

var defaultBudget = entity.GoalBudget{MaxConcurrentSpecialists: 3, MaxDepth: 3, MaxTokens: 120000, MaxDurationMS: int64((30 * time.Minute) / time.Millisecond), MaxSearchQueries: 20, MaxPages: 40, MaxActions: 30}

type Service struct{ store repository.Store }

func NewService(store repository.Store) *Service { return &Service{store: store} }

type CreateGoalRequest struct {
	AgentID         string                    `json:"agent_id"`
	ConversationID  string                    `json:"conversation_id"`
	Objective       string                    `json:"objective"`
	Constraints     []string                  `json:"constraints"`
	SuccessCriteria []entity.SuccessCriterion `json:"success_criteria"`
	Budget          entity.GoalBudget         `json:"budget"`
	Deadline        time.Time                 `json:"deadline"`
	ApprovalPolicy  entity.ApprovalPolicy     `json:"approval_policy"`
}

type PlanTaskRequest struct {
	TaskID               string            `json:"task_id"`
	ParentTaskID         string            `json:"parent_task_id"`
	Depth                int               `json:"depth"`
	Specialist           string            `json:"specialist"`
	Objective            string            `json:"objective"`
	DependsOn            []string          `json:"depends_on"`
	RequiredCapabilities []string          `json:"required_capabilities"`
	WorldSliceRefs       []string          `json:"world_slice_refs"`
	Budget               entity.TaskBudget `json:"budget"`
}

type PlanGoalRequest struct {
	ExpectedRevision int64             `json:"expected_revision"`
	Tasks            []PlanTaskRequest `json:"tasks"`
}

type CreatePlannedGoalRequest struct {
	CreateGoalRequest
	Tasks []PlanTaskRequest `json:"tasks"`
}

type StartTaskRequest struct {
	ExpectedRevision int64                    `json:"expected_revision"`
	Devices          []entity.DeviceCandidate `json:"devices"`
}

type RecordResultRequest struct {
	ExpectedRevision    int64                     `json:"expected_revision"`
	Result              entity.SpecialistResult   `json:"result"`
	VerifiedCriteriaIDs []string                  `json:"verified_criteria_ids"`
	ConfirmedEffectKeys []string                  `json:"confirmed_effect_keys"`
	PendingApprovalIDs  []string                  `json:"pending_approval_ids"`
	WorldSlicesByDevice map[string]map[string]any `json:"world_slices_by_device"`
}

type CheckpointRequest struct {
	ExpectedRevision    int64                     `json:"expected_revision"`
	Reason              string                    `json:"reason"`
	ConfirmedEffectKeys []string                  `json:"confirmed_effect_keys"`
	PendingApprovalIDs  []string                  `json:"pending_approval_ids"`
	WorldSlicesByDevice map[string]map[string]any `json:"world_slices_by_device"`
}

type GoalState struct {
	Goal       entity.PersistentGoal     `json:"goal"`
	Tasks      []entity.SpecialistTask   `json:"tasks"`
	Results    []entity.SpecialistResult `json:"results"`
	Checkpoint *entity.GoalCheckpoint    `json:"checkpoint,omitempty"`
}

type ResumePlan struct {
	Goal                entity.PersistentGoal     `json:"goal"`
	PendingTasks        []entity.SpecialistTask   `json:"pending_tasks"`
	ConfirmedEffectKeys []string                  `json:"confirmed_effect_keys"`
	WorldSlicesByDevice map[string]map[string]any `json:"world_slices_by_device"`
}

func (s *Service) CreateGoal(ctx context.Context, ownerID string, request CreateGoalRequest) (*entity.PersistentGoal, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("orchestration service is not configured")
	}
	now := time.Now().UTC()
	budget := request.Budget
	if budget.MaxConcurrentSpecialists == 0 {
		budget = defaultBudget
	}
	criteria := append([]entity.SuccessCriterion(nil), request.SuccessCriteria...)
	for index := range criteria {
		if criteria[index].CriterionID == "" {
			criteria[index].CriterionID = ulid.New()
		}
	}
	goal := entity.PersistentGoal{Schema: entity.Schema, GoalID: ulid.New(), OwnerID: ownerID, AgentID: strings.TrimSpace(request.AgentID), ConversationID: strings.TrimSpace(request.ConversationID), Objective: strings.TrimSpace(request.Objective), Constraints: compact(request.Constraints), SuccessCriteria: criteria, Budget: budget, Deadline: request.Deadline, ApprovalPolicy: request.ApprovalPolicy, Status: orchestrationv1.GoalDraft, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := goal.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	checkpoint, err := newCheckpoint(goal, nil, nil, "goal created", nil, nil, 1)
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID = checkpoint.CheckpointID
	checkpoint.GoalRevision = goal.Revision
	checkpoint.Checksum, err = checkpointChecksum(checkpoint)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateGoal(ctx, goal, checkpoint); err != nil {
		return nil, log.WrapError(err, "OrchestrationService.CreateGoal.persist")
	}
	return &goal, nil
}

func (s *Service) CreatePlannedGoal(ctx context.Context, ownerID string, request CreatePlannedGoalRequest) (*GoalState, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("orchestration service is not configured")
	}
	now := time.Now().UTC()
	budget := request.Budget
	if budget.MaxConcurrentSpecialists == 0 {
		budget = defaultBudget
	}
	criteria := append([]entity.SuccessCriterion(nil), request.SuccessCriteria...)
	for index := range criteria {
		if criteria[index].CriterionID == "" {
			criteria[index].CriterionID = ulid.New()
		}
	}
	goal := entity.PersistentGoal{Schema: entity.Schema, GoalID: ulid.New(), OwnerID: ownerID, AgentID: strings.TrimSpace(request.AgentID), ConversationID: strings.TrimSpace(request.ConversationID), Objective: strings.TrimSpace(request.Objective), Constraints: compact(request.Constraints), SuccessCriteria: criteria, Budget: budget, Deadline: request.Deadline, ApprovalPolicy: request.ApprovalPolicy, Status: orchestrationv1.GoalPlanned, Revision: 1, CreatedAt: now, UpdatedAt: now}
	tasks := make([]entity.SpecialistTask, 0, len(request.Tasks))
	for _, item := range request.Tasks {
		id := strings.TrimSpace(item.TaskID)
		if id == "" {
			id = ulid.New()
		}
		status := orchestrationv1.TaskPending
		if len(item.DependsOn) == 0 {
			status = orchestrationv1.TaskReady
		}
		tasks = append(tasks, entity.SpecialistTask{TaskID: id, GoalID: goal.GoalID, ParentTaskID: strings.TrimSpace(item.ParentTaskID), Depth: item.Depth, Specialist: item.Specialist, Objective: strings.TrimSpace(item.Objective), DependsOn: unique(item.DependsOn), RequiredCapabilities: unique(item.RequiredCapabilities), WorldSliceRefs: unique(item.WorldSliceRefs), Budget: normalizedTaskBudget(item.Budget, goal.Budget, len(request.Tasks)), Status: status, CreatedAt: now, UpdatedAt: now})
	}
	graph := entity.TaskGraph{Schema: entity.Schema, GoalID: goal.GoalID, PlanRevision: 1, Nodes: tasks, CreatedAt: now}
	if err := goal.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := graph.Validate(goal.Budget); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	checkpoint, err := newCheckpoint(goal, tasks, nil, "goal and finite task graph created", nil, nil, 1)
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID = checkpoint.CheckpointID
	if err := s.store.CreatePlannedGoal(ctx, goal, tasks, checkpoint); err != nil {
		return nil, log.WrapError(err, "OrchestrationService.CreatePlannedGoal.persist")
	}
	return s.GetGoal(ctx, ownerID, goal.GoalID)
}

func (s *Service) GetGoal(ctx context.Context, ownerID, goalID string) (*GoalState, error) {
	goal, err := s.store.FindGoal(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	if goal == nil {
		return nil, apierror.ErrNotFound.WithMessage("goal not found")
	}
	tasks, err := s.store.ListTasks(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	results, err := s.store.ListResults(ctx, ownerID, goalID, 200)
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.store.LatestCheckpoint(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	return &GoalState{Goal: *goal, Tasks: tasks, Results: results, Checkpoint: checkpoint}, nil
}

func (s *Service) ListGoals(ctx context.Context, ownerID string, statuses []string, limit int) ([]entity.PersistentGoal, error) {
	return s.store.ListGoals(ctx, ownerID, entity.GoalFilter{Statuses: statuses, Limit: limit})
}

func (s *Service) ListRunnableGoals(ctx context.Context, limit int) ([]entity.PersistentGoal, error) {
	return s.store.ListRunnableGoals(ctx, limit)
}

func (s *Service) PlanGoal(ctx context.Context, ownerID, goalID string, request PlanGoalRequest) (*GoalState, error) {
	goal, currentTasks, _, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	if len(currentTasks) != 0 {
		return nil, apierror.ErrBadRequest.WithMessage("goal already has a task graph")
	}
	if request.ExpectedRevision == 0 {
		request.ExpectedRevision = goal.Revision
	}
	now := time.Now().UTC()
	tasks := make([]entity.SpecialistTask, 0, len(request.Tasks))
	for _, item := range request.Tasks {
		id := strings.TrimSpace(item.TaskID)
		if id == "" {
			id = ulid.New()
		}
		status := orchestrationv1.TaskPending
		if len(item.DependsOn) == 0 {
			status = orchestrationv1.TaskReady
		}
		tasks = append(tasks, entity.SpecialistTask{TaskID: id, GoalID: goal.GoalID, ParentTaskID: strings.TrimSpace(item.ParentTaskID), Depth: item.Depth, Specialist: item.Specialist, Objective: strings.TrimSpace(item.Objective), DependsOn: unique(item.DependsOn), RequiredCapabilities: unique(item.RequiredCapabilities), WorldSliceRefs: unique(item.WorldSliceRefs), Budget: normalizedTaskBudget(item.Budget, goal.Budget, len(request.Tasks)), Status: status, CreatedAt: now, UpdatedAt: now})
	}
	graph := entity.TaskGraph{Schema: entity.Schema, GoalID: goal.GoalID, PlanRevision: goal.Revision + 1, Nodes: tasks, CreatedAt: now}
	if err := graph.Validate(goal.Budget); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	expected := request.ExpectedRevision
	goal.Status, goal.Revision, goal.UpdatedAt = orchestrationv1.GoalPlanned, expected+1, now
	checkpoint, err := s.nextCheckpoint(ctx, *goal, tasks, "finite task graph planned", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID = checkpoint.CheckpointID
	checkpoint.GoalRevision = goal.Revision
	checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
	if err := goal.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expected); err != nil {
		return nil, log.WrapError(err, "OrchestrationService.PlanGoal.persist")
	}
	return s.GetGoal(ctx, ownerID, goalID)
}

func (s *Service) StartTask(ctx context.Context, ownerID, goalID, taskID string, request StartTaskRequest) (*entity.RouteDecision, *GoalState, error) {
	goal, tasks, previous, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, nil, err
	}
	index := taskIndex(tasks, taskID)
	if index < 0 {
		return nil, nil, apierror.ErrNotFound.WithMessage("goal task not found")
	}
	if request.ExpectedRevision == 0 {
		request.ExpectedRevision = goal.Revision
	}
	if tasks[index].Status != orchestrationv1.TaskReady && tasks[index].Status != orchestrationv1.TaskWaitingDevice {
		return nil, nil, apierror.ErrBadRequest.WithMessage("task is not ready")
	}
	if !dependenciesComplete(tasks[index], tasks) {
		return nil, nil, apierror.ErrBadRequest.WithMessage("task dependencies are incomplete")
	}
	if runningCount(tasks) >= goal.Budget.MaxConcurrentSpecialists {
		return nil, nil, apierror.ErrBadRequest.WithMessage("specialist concurrency budget is exhausted")
	}
	if budgetExhausted(*goal) {
		expected := request.ExpectedRevision
		goal.Status, goal.Revision, goal.UpdatedAt = orchestrationv1.GoalWaitingUser, expected+1, time.Now().UTC()
		checkpoint, checkpointErr := s.nextCheckpoint(ctx, *goal, tasks, "goal budget or deadline exhausted before specialist dispatch", previous.ConfirmedEffectKeys, previous.PendingApprovalIDs, previous.WorldSlicesByDevice)
		if checkpointErr != nil {
			return nil, nil, checkpointErr
		}
		goal.LatestCheckpointID, checkpoint.GoalRevision = checkpoint.CheckpointID, goal.Revision
		checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
		if persistErr := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expected); persistErr != nil {
			return nil, nil, log.WrapError(persistErr, "OrchestrationService.StartTask.persistBudgetStop")
		}
		return nil, nil, apierror.ErrBadRequest.WithMessage("goal budget or deadline is exhausted")
	}
	decision := orchestrationv1.Route(tasks[index], request.Devices)
	now, expected := time.Now().UTC(), request.ExpectedRevision
	tasks[index].Status, tasks[index].DeviceID, tasks[index].UpdatedAt = decision.Status, decision.DeviceID, now
	if decision.Status == orchestrationv1.TaskRunning {
		tasks[index].Attempt++
		goal.Status = orchestrationv1.GoalRunning
	} else {
		goal.Status = orchestrationv1.GoalWaitingUser
	}
	goal.ActiveTaskIDs = runningTaskIDs(tasks)
	goal.Revision, goal.UpdatedAt = expected+1, now
	checkpoint, err := s.nextCheckpoint(ctx, *goal, tasks, decision.Reason, previous.ConfirmedEffectKeys, previous.PendingApprovalIDs, previous.WorldSlicesByDevice)
	if err != nil {
		return nil, nil, err
	}
	goal.LatestCheckpointID = checkpoint.CheckpointID
	checkpoint.GoalRevision = goal.Revision
	checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
	if err := goal.Validate(); err != nil {
		return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expected); err != nil {
		return nil, nil, log.WrapError(err, "OrchestrationService.StartTask.persist")
	}
	state, err := s.GetGoal(ctx, ownerID, goalID)
	return &decision, state, err
}

func (s *Service) RecordResult(ctx context.Context, ownerID, goalID, taskID string, request RecordResultRequest) (*GoalState, error) {
	goal, tasks, previous, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	index := taskIndex(tasks, taskID)
	if index < 0 {
		return nil, apierror.ErrNotFound.WithMessage("goal task not found")
	}
	if tasks[index].Status != orchestrationv1.TaskRunning {
		return nil, apierror.ErrBadRequest.WithMessage("specialist task is not running")
	}
	if request.ExpectedRevision == 0 {
		request.ExpectedRevision = goal.Revision
	}
	result := request.Result
	result.Schema, result.GoalID, result.TaskID, result.Specialist = entity.Schema, goalID, taskID, tasks[index].Specialist
	if result.RunID == "" {
		result.RunID = ulid.New()
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	if result.Status != orchestrationv1.TaskCompleted && result.Status != orchestrationv1.TaskFailed && result.Status != orchestrationv1.TaskWaitingUser && result.Status != orchestrationv1.TaskWaitingDevice {
		return nil, apierror.ErrBadRequest.WithMessage("specialist result must be completed, failed, waiting for user, or waiting for device")
	}
	if err := result.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	now, expected := time.Now().UTC(), request.ExpectedRevision
	tasks[index].Status, tasks[index].UpdatedAt = result.Status, now
	goal.Usage = addUsage(goal.Usage, result.Usage)
	verifyCriteria(goal.SuccessCriteria, request.VerifiedCriteriaIDs, result.EvidenceRefs)
	refreshReadyTasks(tasks)
	goal.ActiveTaskIDs = runningTaskIDs(tasks)
	switch {
	case result.Status == orchestrationv1.TaskFailed:
		goal.Status = orchestrationv1.GoalWaitingUser
	case result.Status == orchestrationv1.TaskWaitingUser || result.Status == orchestrationv1.TaskWaitingDevice:
		goal.Status = orchestrationv1.GoalWaitingUser
	case budgetExhausted(*goal):
		goal.Status = orchestrationv1.GoalWaitingUser
	case allTasksCompleted(tasks) && allRequiredCriteriaVerified(goal.SuccessCriteria):
		goal.Status = orchestrationv1.GoalCompleted
	case allTasksTerminal(tasks):
		goal.Status = orchestrationv1.GoalWaitingUser
	default:
		goal.Status = orchestrationv1.GoalRunning
	}
	goal.Revision, goal.UpdatedAt = expected+1, now
	effects := union(previous.ConfirmedEffectKeys, request.ConfirmedEffectKeys)
	world := mergeWorldSlices(previous.WorldSlicesByDevice, request.WorldSlicesByDevice)
	approvals := previous.PendingApprovalIDs
	if len(request.PendingApprovalIDs) > 0 {
		approvals = union(approvals, request.PendingApprovalIDs)
	}
	checkpoint, err := s.nextCheckpoint(ctx, *goal, tasks, "specialist result recorded", effects, approvals, world)
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID = checkpoint.CheckpointID
	checkpoint.GoalRevision = goal.Revision
	checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
	if err := goal.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.SaveState(ctx, *goal, tasks, &result, checkpoint, expected); err != nil {
		return nil, log.WrapError(err, "OrchestrationService.RecordResult.persist")
	}
	return s.GetGoal(ctx, ownerID, goalID)
}

func (s *Service) SaveCheckpoint(ctx context.Context, ownerID, goalID string, request CheckpointRequest) (*entity.GoalCheckpoint, error) {
	goal, tasks, previous, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	if request.ExpectedRevision == 0 {
		request.ExpectedRevision = goal.Revision
	}
	expected := request.ExpectedRevision
	goal.Revision, goal.UpdatedAt = expected+1, time.Now().UTC()
	checkpoint, err := s.nextCheckpoint(ctx, *goal, tasks, strings.TrimSpace(request.Reason), union(previous.ConfirmedEffectKeys, request.ConfirmedEffectKeys), union(previous.PendingApprovalIDs, request.PendingApprovalIDs), mergeWorldSlices(previous.WorldSlicesByDevice, request.WorldSlicesByDevice))
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID = checkpoint.CheckpointID
	checkpoint.GoalRevision = goal.Revision
	checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
	if err := checkpoint.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expected); err != nil {
		return nil, log.WrapError(err, "OrchestrationService.SaveCheckpoint.persist")
	}
	return &checkpoint, nil
}

func (s *Service) Pause(ctx context.Context, ownerID, goalID, reason string, expectedRevision int64) (*GoalState, error) {
	goal, tasks, previous, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	if expectedRevision == 0 {
		expectedRevision = goal.Revision
	}
	for index := range tasks {
		if tasks[index].Status == orchestrationv1.TaskRunning {
			tasks[index].Status, tasks[index].DeviceID = orchestrationv1.TaskReady, ""
		}
	}
	goal.Status, goal.ActiveTaskIDs, goal.Revision, goal.UpdatedAt = orchestrationv1.GoalPaused, nil, expectedRevision+1, time.Now().UTC()
	checkpoint, err := s.nextCheckpoint(ctx, *goal, tasks, nonEmpty(reason, "goal paused"), previous.ConfirmedEffectKeys, previous.PendingApprovalIDs, previous.WorldSlicesByDevice)
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID, checkpoint.GoalRevision = checkpoint.CheckpointID, goal.Revision
	checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
	if err := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expectedRevision); err != nil {
		return nil, err
	}
	return s.GetGoal(ctx, ownerID, goalID)
}

func (s *Service) Resume(ctx context.Context, ownerID, goalID string, expectedRevision int64) (*ResumePlan, error) {
	goal, tasks, previous, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	if goal.Status != orchestrationv1.GoalPaused && goal.Status != orchestrationv1.GoalWaitingUser && goal.Status != orchestrationv1.GoalFailed {
		return nil, apierror.ErrBadRequest.WithMessage("goal is not resumable")
	}
	if expectedRevision == 0 {
		expectedRevision = goal.Revision
	}
	if budgetExhausted(*goal) {
		return nil, apierror.ErrBadRequest.WithMessage("increase the goal budget or deadline before resuming")
	}
	for index := range tasks {
		if tasks[index].Status == orchestrationv1.TaskWaitingDevice || tasks[index].Status == orchestrationv1.TaskWaitingUser || tasks[index].Status == orchestrationv1.TaskFailed {
			tasks[index].Status, tasks[index].DeviceID = orchestrationv1.TaskReady, ""
		}
	}
	refreshReadyTasks(tasks)
	goal.Status, goal.Revision, goal.UpdatedAt = orchestrationv1.GoalRunning, expectedRevision+1, time.Now().UTC()
	checkpoint, err := s.nextCheckpoint(ctx, *goal, tasks, "goal resumed from durable checkpoint", previous.ConfirmedEffectKeys, nil, previous.WorldSlicesByDevice)
	if err != nil {
		return nil, err
	}
	goal.LatestCheckpointID, checkpoint.GoalRevision = checkpoint.CheckpointID, goal.Revision
	checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
	if err := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expectedRevision); err != nil {
		return nil, log.WrapError(err, "OrchestrationService.Resume.persist")
	}
	pending := make([]entity.SpecialistTask, 0)
	for _, task := range tasks {
		if task.Status != orchestrationv1.TaskCompleted && task.Status != orchestrationv1.TaskCancelled {
			pending = append(pending, task)
		}
	}
	return &ResumePlan{Goal: *goal, PendingTasks: pending, ConfirmedEffectKeys: append([]string(nil), checkpoint.ConfirmedEffectKeys...), WorldSlicesByDevice: cloneWorldSlices(checkpoint.WorldSlicesByDevice)}, nil
}

// RecoverInterruptedGoals converts orphan RUNNING tasks into retryable READY
// tasks. Confirmed effect keys remain in the checkpoint so a resumed model can
// never assume an external side effect needs to be repeated.
func (s *Service) RecoverInterruptedGoals(ctx context.Context) error {
	goals, err := s.store.ListRunnableGoals(ctx, 500)
	if err != nil {
		return err
	}
	for _, listed := range goals {
		goal, tasks, previous, loadErr := s.load(ctx, listed.OwnerID, listed.GoalID)
		if loadErr != nil {
			return loadErr
		}
		changed := false
		for index := range tasks {
			if tasks[index].Status == orchestrationv1.TaskRunning {
				tasks[index].Status, tasks[index].DeviceID, tasks[index].UpdatedAt = orchestrationv1.TaskReady, "", time.Now().UTC()
				changed = true
			}
		}
		if !changed {
			continue
		}
		expected := goal.Revision
		goal.ActiveTaskIDs = nil
		goal.Status, goal.Revision, goal.UpdatedAt = orchestrationv1.GoalRunning, expected+1, time.Now().UTC()
		checkpoint, checkpointErr := s.nextCheckpoint(ctx, *goal, tasks, "control plane restarted; interrupted specialists are ready to resume", previous.ConfirmedEffectKeys, previous.PendingApprovalIDs, previous.WorldSlicesByDevice)
		if checkpointErr != nil {
			return checkpointErr
		}
		goal.LatestCheckpointID, checkpoint.GoalRevision = checkpoint.CheckpointID, goal.Revision
		checkpoint.Checksum, _ = checkpointChecksum(checkpoint)
		if saveErr := s.store.SaveState(ctx, *goal, tasks, nil, checkpoint, expected); saveErr != nil {
			return log.WrapError(saveErr, "OrchestrationService.RecoverInterruptedGoals.persist")
		}
	}
	return nil
}

func (s *Service) ListCheckpoints(ctx context.Context, ownerID, goalID string, limit int) ([]entity.GoalCheckpoint, error) {
	if goal, err := s.store.FindGoal(ctx, ownerID, goalID); err != nil || goal == nil {
		if err != nil {
			return nil, err
		}
		return nil, apierror.ErrNotFound.WithMessage("goal not found")
	}
	return s.store.ListCheckpoints(ctx, ownerID, goalID, limit)
}

func (s *Service) WorldSlice(ctx context.Context, ownerID, goalID, taskID string) (map[string]map[string]any, error) {
	_, tasks, checkpoint, err := s.load(ctx, ownerID, goalID)
	if err != nil {
		return nil, err
	}
	index := taskIndex(tasks, taskID)
	if index < 0 {
		return nil, apierror.ErrNotFound.WithMessage("goal task not found")
	}
	task := tasks[index]
	wanted := make(map[string]struct{}, len(task.WorldSliceRefs))
	for _, ref := range task.WorldSliceRefs {
		wanted[ref] = struct{}{}
	}
	result := make(map[string]map[string]any)
	for deviceID, values := range checkpoint.WorldSlicesByDevice {
		if task.DeviceID != "" && task.DeviceID != deviceID {
			continue
		}
		filtered := make(map[string]any)
		for key, value := range values {
			if _, ok := wanted[key]; ok {
				filtered[key] = value
			}
		}
		if len(filtered) > 0 {
			result[deviceID] = filtered
		}
	}
	return result, nil
}

func (s *Service) CreateScheduleTrigger(ctx context.Context, ownerID, scheduleID, taskID string, scheduledAt time.Time) (*entity.ScheduleTrigger, error) {
	trigger := entity.ScheduleTrigger{TriggerID: ulid.New(), ScheduleID: scheduleID, TaskID: taskID, ScheduledAt: scheduledAt.UTC(), Attempt: 1, Status: "RUNNING"}
	if err := s.store.CreateScheduleTrigger(ctx, ownerID, trigger); err != nil {
		return nil, err
	}
	return &trigger, nil
}

func (s *Service) FinishScheduleTrigger(ctx context.Context, ownerID string, trigger entity.ScheduleTrigger, runErr error) error {
	trigger.FinishedAt = time.Now().UTC()
	trigger.Status = "COMPLETED"
	if runErr != nil {
		trigger.Status, trigger.Error = "FAILED", runErr.Error()
	}
	return s.store.UpdateScheduleTrigger(ctx, ownerID, trigger)
}

func (s *Service) load(ctx context.Context, ownerID, goalID string) (*entity.PersistentGoal, []entity.SpecialistTask, *entity.GoalCheckpoint, error) {
	goal, err := s.store.FindGoal(ctx, ownerID, goalID)
	if err != nil {
		return nil, nil, nil, err
	}
	if goal == nil {
		return nil, nil, nil, apierror.ErrNotFound.WithMessage("goal not found")
	}
	tasks, err := s.store.ListTasks(ctx, ownerID, goalID)
	if err != nil {
		return nil, nil, nil, err
	}
	checkpoint, err := s.store.LatestCheckpoint(ctx, ownerID, goalID)
	if err != nil {
		return nil, nil, nil, err
	}
	if checkpoint == nil {
		return nil, nil, nil, fmt.Errorf("goal %s has no durable checkpoint", goalID)
	}
	return goal, tasks, checkpoint, nil
}

func (s *Service) nextCheckpoint(ctx context.Context, goal entity.PersistentGoal, tasks []entity.SpecialistTask, reason string, effects, approvals []string, world map[string]map[string]any) (entity.GoalCheckpoint, error) {
	latest, err := s.store.LatestCheckpoint(ctx, goal.OwnerID, goal.GoalID)
	if err != nil {
		return entity.GoalCheckpoint{}, err
	}
	sequence := int64(1)
	if latest != nil {
		sequence = latest.Sequence + 1
	}
	return newCheckpoint(goal, tasks, world, nonEmpty(reason, "state transition"), effects, approvals, sequence)
}

func newCheckpoint(goal entity.PersistentGoal, tasks []entity.SpecialistTask, world map[string]map[string]any, reason string, effects, approvals []string, sequence int64) (entity.GoalCheckpoint, error) {
	value := entity.GoalCheckpoint{Schema: entity.Schema, CheckpointID: ulid.New(), GoalID: goal.GoalID, OwnerID: goal.OwnerID, Sequence: sequence, GoalRevision: goal.Revision, Status: goal.Status, TaskStates: append([]entity.SpecialistTask(nil), tasks...), Usage: goal.Usage, WorldSlicesByDevice: cloneWorldSlices(world), ConfirmedEffectKeys: unique(effects), PendingApprovalIDs: unique(approvals), Reason: reason, CreatedAt: time.Now().UTC()}
	checksum, err := checkpointChecksum(value)
	value.Checksum = checksum
	return value, err
}

func checkpointChecksum(value entity.GoalCheckpoint) (string, error) {
	value.Checksum = ""
	body, err := json.Marshal(value)
	if err != nil {
		return "", log.WrapError(err, "OrchestrationService.checkpointChecksum")
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func taskIndex(tasks []entity.SpecialistTask, taskID string) int {
	for i := range tasks {
		if tasks[i].TaskID == taskID {
			return i
		}
	}
	return -1
}
func runningCount(tasks []entity.SpecialistTask) int {
	count := 0
	for _, task := range tasks {
		if task.Status == orchestrationv1.TaskRunning {
			count++
		}
	}
	return count
}
func runningTaskIDs(tasks []entity.SpecialistTask) []string {
	ids := []string{}
	for _, task := range tasks {
		if task.Status == orchestrationv1.TaskRunning {
			ids = append(ids, task.TaskID)
		}
	}
	sort.Strings(ids)
	return ids
}
func dependenciesComplete(task entity.SpecialistTask, tasks []entity.SpecialistTask) bool {
	for _, id := range task.DependsOn {
		index := taskIndex(tasks, id)
		if index < 0 || tasks[index].Status != orchestrationv1.TaskCompleted {
			return false
		}
	}
	return true
}
func refreshReadyTasks(tasks []entity.SpecialistTask) {
	for index := range tasks {
		if tasks[index].Status == orchestrationv1.TaskPending && dependenciesComplete(tasks[index], tasks) {
			tasks[index].Status, tasks[index].UpdatedAt = orchestrationv1.TaskReady, time.Now().UTC()
		}
	}
}
func allTasksCompleted(tasks []entity.SpecialistTask) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		if task.Status != orchestrationv1.TaskCompleted {
			return false
		}
	}
	return true
}
func allTasksTerminal(tasks []entity.SpecialistTask) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		if task.Status != orchestrationv1.TaskCompleted && task.Status != orchestrationv1.TaskFailed && task.Status != orchestrationv1.TaskCancelled {
			return false
		}
	}
	return true
}
func allRequiredCriteriaVerified(values []entity.SuccessCriterion) bool {
	for _, value := range values {
		if value.Required && !value.Verified {
			return false
		}
	}
	return true
}
func budgetExhausted(goal entity.PersistentGoal) bool {
	return goal.Usage.Exhausted(goal.Budget) || (!goal.Deadline.IsZero() && !time.Now().UTC().Before(goal.Deadline))
}

func normalizedTaskBudget(value entity.TaskBudget, goal entity.GoalBudget, taskCount int) entity.TaskBudget {
	if value != (entity.TaskBudget{}) {
		return value
	}
	if taskCount < 1 {
		taskCount = 1
	}
	positiveShare := func(total int64) int64 {
		share := total / int64(taskCount)
		if share < 1 {
			return 1
		}
		return share
	}
	positiveIntShare := func(total int) int {
		share := total / taskCount
		if share < 1 {
			return 1
		}
		return share
	}
	return entity.TaskBudget{
		MaxTokens:        positiveShare(goal.MaxTokens),
		MaxDurationMS:    positiveShare(goal.MaxDurationMS),
		MaxSearchQueries: positiveIntShare(goal.MaxSearchQueries),
		MaxPages:         positiveIntShare(goal.MaxPages),
		MaxActions:       positiveIntShare(goal.MaxActions),
	}
}
func addUsage(left, right entity.BudgetUsage) entity.BudgetUsage {
	return entity.BudgetUsage{Tokens: left.Tokens + right.Tokens, DurationMS: left.DurationMS + right.DurationMS, SearchQueries: left.SearchQueries + right.SearchQueries, Pages: left.Pages + right.Pages, Actions: left.Actions + right.Actions}
}

func verifyCriteria(values []entity.SuccessCriterion, ids, evidence []string) {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	for index := range values {
		if _, ok := wanted[values[index].CriterionID]; ok {
			values[index].Verified = true
			if len(evidence) > 0 {
				values[index].EvidenceRef = evidence[0]
			}
		}
	}
}

func compact(values []string) []string {
	result := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return unique(result)
}
func unique(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
func union(left, right []string) []string {
	return unique(append(append([]string(nil), left...), right...))
}
func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
func mergeWorldSlices(left, right map[string]map[string]any) map[string]map[string]any {
	result := cloneWorldSlices(left)
	if result == nil {
		result = map[string]map[string]any{}
	}
	for device, values := range right {
		copy := map[string]any{}
		for key, value := range result[device] {
			copy[key] = value
		}
		for key, value := range values {
			copy[key] = value
		}
		result[device] = copy
	}
	return result
}
func cloneWorldSlices(value map[string]map[string]any) map[string]map[string]any {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]map[string]any, len(value))
	for device, values := range value {
		copy := make(map[string]any, len(values))
		for key, item := range values {
			copy[key] = item
		}
		result[device] = copy
	}
	return result
}
