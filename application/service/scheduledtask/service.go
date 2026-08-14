package scheduledtask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	orchestrationsvc "github.com/good-fish-man/agent-runtime-client/application/service/orchestration"
	runtimesvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	agentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/agent"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	jobpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/job"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/scheduledtask"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	orchestrationv1 "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"
	log "github.com/good-fish-man/logx"
)

const (
	ActionConfirmBeforeCommit = "confirm_before_commit"
	maxConcurrentRuns         = 3
	resultLimit               = 12000
	defaultScanInterval       = time.Minute
)

type CreateRequest struct {
	UserID             string         `json:"user_id"`
	AgentID            string         `json:"agent_id"`
	SessionID          string         `json:"session_id"`
	Name               string         `json:"name"`
	TaskType           string         `json:"task_type"`
	Cron               string         `json:"cron"`
	Timezone           string         `json:"timezone"`
	Prompt             string         `json:"prompt"`
	Criteria           map[string]any `json:"criteria"`
	MisfirePolicy      string         `json:"misfire_policy"`
	RetryMax           int            `json:"retry_max"`
	RetryBackoffMS     int64          `json:"retry_backoff_ms"`
	MaxConcurrency     int            `json:"max_concurrency"`
	RiskLevel          string         `json:"risk_level"`
	ApprovalMode       string         `json:"approval_mode"`
	PreauthorizationID string         `json:"preauthorization_id"`
	Notify             *bool          `json:"notify"`
}

type UpdateRequest struct {
	Status string `json:"status"`
}
type ApprovalDecision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type Service struct {
	data          *data.Data
	runtime       *runtimesvc.RuntimeService
	cancel        context.CancelFunc
	sem           chan struct{}
	scanInterval  time.Duration
	control       *controlsvc.Hub
	orchestration *orchestrationsvc.Service
	activeMu      sync.Mutex
	active        map[string]int
}

func (s *Service) WithControlPlane(control *controlsvc.Hub, orchestration *orchestrationsvc.Service) *Service {
	s.control = control
	s.orchestration = orchestration
	return s
}

func NewService(d *data.Data, runtime *runtimesvc.RuntimeService, scanInterval ...time.Duration) *Service {
	interval := defaultScanInterval
	if len(scanInterval) > 0 && scanInterval[0] > 0 {
		interval = scanInterval[0]
	}
	return &Service{data: d, runtime: runtime, sem: make(chan struct{}, maxConcurrentRuns), scanInterval: interval, active: make(map[string]int)}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*po.ScheduledTask, error) {
	req.UserID, req.AgentID = strings.TrimSpace(req.UserID), strings.TrimSpace(req.AgentID)
	if req.UserID == "" || req.AgentID == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Prompt) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("user_id, agent_id, name, and prompt are required")
	}
	if err := validateCron(req.Cron); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	location := strings.TrimSpace(req.Timezone)
	if location == "" {
		location = "Local"
	}
	if location != "Local" {
		if _, err := time.LoadLocation(location); err != nil {
			return nil, apierror.ErrBadRequest.WithMessage("invalid timezone")
		}
	}
	var agent agentpo.SysAgent
	if err := s.data.DB(ctx).Where("ulid = ? AND deleted_at = 0 AND enabled = ?", req.AgentID, true).First(&agent).Error; err != nil {
		return nil, apierror.ErrForbidden.WithMessage("scheduled task agent is unavailable or not owned by the user")
	}
	if !agent.IsSystem && agent.CreatedBy != req.UserID {
		return nil, apierror.ErrForbidden.WithMessage("scheduled task agent is unavailable or not owned by the user")
	}
	if agent.IsSystem {
		var binding agentpo.SysAgentUserModel
		if err := s.data.DB(ctx).Where("agent_id = ? AND user_id = ?", agent.Ulid, req.UserID).First(&binding).Error; err != nil || binding.Model == "" {
			return nil, apierror.ErrModelBindingRequired.WithMessage("bind your model before scheduling this public agent")
		}
	}
	criteria, _ := json.Marshal(req.Criteria)
	typeName := strings.ToLower(strings.TrimSpace(req.TaskType))
	switch typeName {
	case "ticket", "product", "appointment", "monitor":
	default:
		typeName = "monitor"
	}
	if req.MisfirePolicy == "" {
		req.MisfirePolicy = orchestrationv1.MisfireOnce
	}
	if req.RetryMax == 0 {
		req.RetryMax = 3
	}
	if req.RetryBackoffMS == 0 {
		req.RetryBackoffMS = 1000
	}
	if req.MaxConcurrency == 0 {
		req.MaxConcurrency = 1
	}
	if req.RiskLevel == "" {
		req.RiskLevel = "R1"
	}
	if req.ApprovalMode == "" {
		req.ApprovalMode = orchestrationv1.ApprovalNone
	}
	notify := true
	if req.Notify != nil {
		notify = *req.Notify
	}
	item := &po.ScheduledTask{Ulid: ulid.New(), UserID: req.UserID, AgentID: req.AgentID, SessionID: strings.TrimSpace(req.SessionID), Name: strings.TrimSpace(req.Name), TaskType: typeName, CronExpr: strings.TrimSpace(req.Cron), Timezone: location, Prompt: strings.TrimSpace(req.Prompt), CriteriaJSON: string(criteria), ActionMode: ActionConfirmBeforeCommit, MisfirePolicy: req.MisfirePolicy, RetryMax: req.RetryMax, RetryBackoffMS: req.RetryBackoffMS, MaxConcurrency: req.MaxConcurrency, RiskLevel: req.RiskLevel, ApprovalMode: req.ApprovalMode, PreauthorizationID: strings.TrimSpace(req.PreauthorizationID), Notify: notify, Status: po.StatusActive}
	schedule := orchestrationv1.Schedule{Schema: orchestrationv1.Schema, ScheduleID: item.Ulid, OwnerID: item.UserID, Name: item.Name, Cron: item.CronExpr, Timezone: item.Timezone, MisfirePolicy: item.MisfirePolicy, Retry: orchestrationv1.RetryPolicy{MaxAttempts: item.RetryMax, BackoffMS: item.RetryBackoffMS}, MaxConcurrency: item.MaxConcurrency, RiskLevel: item.RiskLevel, ApprovalMode: item.ApprovalMode, PreauthorizationID: item.PreauthorizationID, Notify: item.Notify, Status: orchestrationv1.ScheduleActive, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := schedule.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.data.DB(ctx).Create(item).Error; err != nil {
		return nil, fmt.Errorf("create scheduled task: %w", err)
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, userID string) ([]po.ScheduledTask, error) {
	items := make([]po.ScheduledTask, 0)
	err := s.data.DB(ctx).Where("user_id = ? AND deleted_at = 0", userID).Order("created_at DESC").Find(&items).Error
	return items, err
}

func (s *Service) Update(ctx context.Context, userID, id string, req UpdateRequest) error {
	if req.Status != po.StatusActive && req.Status != po.StatusPaused {
		return apierror.ErrBadRequest.WithMessage("status must be active or paused")
	}
	result := s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).Update("status", req.Status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apierror.ErrNotFound.WithMessage("scheduled task not found")
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	result := s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).Updates(map[string]any{"deleted_at": time.Now().UnixMilli(), "status": po.StatusPaused})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apierror.ErrNotFound.WithMessage("scheduled task not found")
	}
	return nil
}

func (s *Service) ListApprovals(ctx context.Context, userID string) ([]chatpo.ChatApproval, error) {
	items := make([]chatpo.ChatApproval, 0)
	err := s.data.DB(ctx).Where("user_id = ?", userID).Order("created_at DESC").Limit(200).Find(&items).Error
	return items, err
}

func (s *Service) DecideApproval(ctx context.Context, userID, id string, decision ApprovalDecision) error {
	var approval chatpo.ChatApproval
	if err := s.data.DB(ctx).Where("ulid = ? AND user_id = ? AND status = ?", id, userID, "pending").First(&approval).Error; err != nil {
		return apierror.ErrNotFound.WithMessage("pending approval not found")
	}
	status := "rejected"
	if decision.Approved {
		status = "approved"
	}
	result := s.data.DB(ctx).Model(&chatpo.ChatApproval{}).Where("ulid = ? AND user_id = ? AND status = ?", id, userID, "pending").Updates(map[string]any{"status": status, "approved_by": userID, "approved_at": time.Now().UnixMilli(), "reason": strings.TrimSpace(decision.Reason)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apierror.ErrNotFound.WithMessage("pending approval not found")
	}
	var payload struct {
		GoalID string `json:"goal_id"`
	}
	if s.orchestration != nil && json.Unmarshal([]byte(approval.Parameters), &payload) == nil && payload.GoalID != "" {
		if decision.Approved {
			if _, err := s.orchestration.ResumeApproved(ctx, userID, payload.GoalID, approval.Ulid, 0); err != nil {
				_ = s.restorePendingApproval(ctx, userID, approval.Ulid, status)
				return log.WrapError(err, "ScheduledTaskService.DecideApproval.resumeGoal")
			}
		} else {
			if _, err := s.orchestration.RejectApproval(ctx, userID, payload.GoalID, approval.Ulid, nonEmptyDecisionReason(decision.Reason), 0); err != nil {
				_ = s.restorePendingApproval(ctx, userID, approval.Ulid, status)
				return log.WrapError(err, "ScheduledTaskService.DecideApproval.rejectGoal")
			}
		}
	}
	return nil
}

func (s *Service) Start(parent context.Context) {
	if s == nil || s.data == nil || s.orchestration == nil || s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	go s.loop(ctx)
}

func (s *Service) Stop() {
	if s != nil && s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *Service) loop(ctx context.Context) {
	interval := s.scanInterval
	if interval <= 0 {
		interval = defaultScanInterval
	}
	log.Infof("scheduled task scanner started; interval=%s", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	var tasks []po.ScheduledTask
	if err := s.data.DB(ctx).Where("status = ? AND deleted_at = 0", po.StatusActive).Find(&tasks).Error; err != nil {
		log.Errorf("scheduled task scan failed: %v", err)
		return
	}
	for index := range tasks {
		task := tasks[index]
		s.reconcileTerminalTriggers(ctx, task)
		location := time.Local
		if task.Timezone != "" && task.Timezone != "Local" {
			if loaded, err := time.LoadLocation(task.Timezone); err == nil {
				location = loaded
			}
		}
		now := time.Now().In(location)
		slot, due := dueSlot(task, now)
		if !due {
			continue
		}
		if s.activeTriggerCount(ctx, task) >= task.MaxConcurrency {
			log.Warnf("scheduled task durable concurrency limit reached; task=%s limit=%d", task.Ulid, task.MaxConcurrency)
			continue
		}
		if !s.acquire(task.Ulid, task.MaxConcurrency) {
			log.Warnf("scheduled task per-task concurrency limit reached; task=%s limit=%d", task.Ulid, task.MaxConcurrency)
			continue
		}
		select {
		case s.sem <- struct{}{}:
		default:
			s.release(task.Ulid)
			log.Warnf("scheduled task global concurrency limit reached; task=%s", task.Ulid)
			continue
		}
		claim := s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ? AND status = ? AND (last_slot = '' OR last_slot <> ?)", task.Ulid, po.StatusActive, slot).Updates(map[string]any{"last_slot": slot, "last_run_at": time.Now().UnixMilli(), "last_status": jobpo.JobStatusRunning})
		if claim.Error != nil || claim.RowsAffected == 0 {
			<-s.sem
			s.release(task.Ulid)
			continue
		}
		go func(slot string) { defer func() { <-s.sem; s.release(task.Ulid) }(); s.execute(task, slot) }(slot)
	}
}

func (s *Service) execute(task po.ScheduledTask, slot string) {
	started := time.Now().UTC()
	ctx, cancel := context.WithTimeout(authctx.WithUserID(context.Background(), task.UserID), 30*time.Second)
	defer cancel()
	prompt := task.Prompt + "\n\n[BACKGROUND MONITOR SAFETY] Query and report availability only. Do not purchase, reserve, submit an appointment, accept terms, enter payment, bypass queues, or solve CAPTCHA. Compare findings against every requested criterion. Start the final answer with exactly ACTION_REQUIRED: only when the criteria are satisfied; otherwise start with exactly NO_ACTION:. Include the exact option, price, source URL, and timestamp."
	requireApproval := task.ApprovalMode == orchestrationv1.ApprovalBeforeRun
	approvalID := ""
	if requireApproval {
		approvalID = ulid.New()
	}
	state, err := s.orchestration.CreateScheduledGoal(ctx, task.UserID, orchestrationsvc.CreateScheduledGoalRequest{
		CreateGoalRequest: orchestrationsvc.CreateGoalRequest{
			AgentID: task.AgentID, ConversationID: task.SessionID, Objective: prompt,
			Constraints:     []string{"background monitor is read-only", "external commitment requires an interactive approval"},
			SuccessCriteria: []orchestrationv1.SuccessCriterion{{Description: scheduledCriterion(task), Required: true}},
			ApprovalPolicy:  orchestrationv1.ApprovalPolicy{RequireBeforeRisks: []string{task.RiskLevel}, PreauthorizationID: task.PreauthorizationID},
		},
		ScheduleID: task.Ulid, Slot: slot, ScheduledAt: started,
		Retry:  orchestrationv1.RetryPolicy{MaxAttempts: task.RetryMax, BackoffMS: task.RetryBackoffMS},
		Notify: task.Notify, RequireApproval: requireApproval, PendingApprovalID: approvalID,
		Task: orchestrationsvc.PlanTaskRequest{TaskID: "scheduled-" + ulid.New(), Depth: 1, Specialist: orchestrationv1.SpecialistResearch, Objective: prompt, RequiredCapabilities: []string{"internet.search", "internet.fetch"}},
	})
	if err != nil {
		message := truncate(err.Error(), resultLimit)
		log.ErrorwCtx(ctx, "scheduled trigger could not create durable goal", "schedule_id", task.Ulid, "slot", slot, "error_chain", log.FormatError(err))
		_ = s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ?", task.Ulid).Updates(map[string]any{"last_status": jobpo.JobStatusFailed, "last_error": message}).Error
		return
	}
	if state.Existing {
		return
	}
	updates := map[string]any{"last_run_at": started.UnixMilli(), "execution_count": task.ExecutionCount + 1, "last_status": "queued", "last_error": "", "last_result": ""}
	_ = s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ?", task.Ulid).Updates(updates).Error
	if requireApproval {
		params, _ := json.Marshal(map[string]any{"scheduled_task_id": task.Ulid, "goal_id": state.State.Goal.GoalID, "trigger_id": state.Trigger.TriggerID, "task_name": task.Name, "task_type": task.TaskType, "next_step": "Approve to place this standard durable task on the Supervisor queue."})
		approval := chatpo.ChatApproval{Ulid: approvalID, UserId: task.UserID, AgentId: task.AgentID, MessageId: state.Trigger.TriggerID, ToolName: "Scheduled task execution", RiskLevel: task.RiskLevel, Parameters: string(params), Status: "pending"}
		if createErr := s.data.DB(ctx).Create(&approval).Error; createErr != nil {
			log.ErrorwCtx(ctx, "create scheduled pre-execution approval failed", "trigger_id", state.Trigger.TriggerID, "error_chain", log.FormatError(createErr))
			if _, cancelErr := s.orchestration.RejectApproval(ctx, task.UserID, state.State.Goal.GoalID, approvalID, "pre-execution approval could not be persisted", 0); cancelErr != nil {
				log.ErrorwCtx(ctx, "cancel schedule after approval persistence failure failed", "trigger_id", state.Trigger.TriggerID, "error_chain", log.FormatError(cancelErr))
			}
		}
	}
}

func (s *Service) restorePendingApproval(ctx context.Context, userID, approvalID, status string) error {
	return s.data.DB(ctx).Model(&chatpo.ChatApproval{}).Where("ulid = ? AND user_id = ? AND status = ?", approvalID, userID, status).Updates(map[string]any{"status": "pending", "approved_by": "", "approved_at": 0, "reason": ""}).Error
}

func (s *Service) acquire(taskID string, limit int) bool {
	if limit < 1 {
		limit = 1
	}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.active == nil {
		s.active = make(map[string]int)
	}
	if s.active[taskID] >= limit {
		return false
	}
	s.active[taskID]++
	return true
}

func (s *Service) release(taskID string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.active[taskID] <= 1 {
		delete(s.active, taskID)
		return
	}
	s.active[taskID]--
}

func (s *Service) activeTriggerCount(ctx context.Context, task po.ScheduledTask) int {
	if s.orchestration == nil {
		return 0
	}
	triggers, err := s.orchestration.ListScheduleTriggers(ctx, task.UserID, task.Ulid, 500)
	if err != nil {
		log.ErrorwCtx(ctx, "list active schedule triggers failed", "schedule_id", task.Ulid, "error_chain", log.FormatError(err))
		return task.MaxConcurrency
	}
	count := 0
	for _, trigger := range triggers {
		switch trigger.Status {
		case orchestrationv1.ScheduleTriggerQueued, orchestrationv1.ScheduleTriggerRunning, orchestrationv1.ScheduleTriggerWaitingUser, orchestrationv1.ScheduleTriggerWaitingDevice:
			count++
		}
	}
	return count
}

func (s *Service) reconcileTerminalTriggers(ctx context.Context, task po.ScheduledTask) {
	if s.orchestration == nil {
		return
	}
	triggers, err := s.orchestration.ListScheduleTriggers(ctx, task.UserID, task.Ulid, 200)
	if err != nil {
		log.ErrorwCtx(ctx, "list schedule triggers for reconciliation failed", "schedule_id", task.Ulid, "error_chain", log.FormatError(err))
		return
	}
	for _, trigger := range triggers {
		if !trigger.ReconciledAt.IsZero() || !terminalTrigger(trigger.Status) {
			continue
		}
		s.reconcileTerminalTrigger(ctx, task, trigger)
	}
}

func (s *Service) reconcileTerminalTrigger(ctx context.Context, task po.ScheduledTask, trigger orchestrationv1.ScheduleTrigger) {
	finished := trigger.FinishedAt
	if finished.IsZero() {
		finished = time.Now().UTC()
	}
	status := jobpo.JobStatusSuccess
	if trigger.Status == orchestrationv1.ScheduleTriggerFailed || trigger.Status == orchestrationv1.ScheduleTriggerCancelled {
		status = jobpo.JobStatusFailed
	}
	var agent agentpo.SysAgent
	_ = s.data.DB(ctx).Where("ulid = ?", task.AgentID).First(&agent).Error
	job := jobpo.JobExecutionPO{Ulid: "schedule-" + trigger.TriggerID, AgentId: task.AgentID, AgentName: agent.Name, SessionId: task.SessionID, Status: status, TriggerTime: trigger.ScheduledAt.UnixMilli(), StartedAt: trigger.StartedAt.UnixMilli(), FinishedAt: finished.UnixMilli(), InputSummary: truncate(task.Prompt, 500), OutputSummary: truncate(trigger.Summary, 1000), OutputFull: truncate(trigger.Summary, resultLimit), ErrorMsg: truncate(trigger.Error, resultLimit)}
	if !trigger.StartedAt.IsZero() {
		job.LatencyMs = finished.Sub(trigger.StartedAt).Milliseconds()
	}
	var existing int64
	if err := s.data.DB(ctx).Model(&jobpo.JobExecutionPO{}).Where("ulid = ?", job.Ulid).Count(&existing).Error; err == nil && existing == 0 {
		if err := s.data.DB(ctx).Create(&job).Error; err != nil {
			log.ErrorwCtx(ctx, "persist reconciled schedule execution failed", "trigger_id", trigger.TriggerID, "error_chain", log.FormatError(err))
			return
		}
	}
	updates := map[string]any{"last_status": status, "last_result": truncate(trigger.Summary, resultLimit), "last_error": truncate(trigger.Error, resultLimit)}
	if err := s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ?", task.Ulid).Updates(updates).Error; err != nil {
		log.ErrorwCtx(ctx, "update reconciled schedule status failed", "trigger_id", trigger.TriggerID, "error_chain", log.FormatError(err))
		return
	}
	notificationStatus, notificationError := orchestrationv1.NotificationDisabled, ""
	if trigger.Notify {
		notificationStatus = orchestrationv1.NotificationSent
		if strings.TrimSpace(task.SessionID) == "" {
			notificationStatus, notificationError = orchestrationv1.NotificationFailed, "schedule has no conversation session for notification delivery"
		} else {
			metadata, _ := json.Marshal(map[string]any{"scheduled_task_id": task.Ulid, "goal_id": trigger.GoalID, "trigger_id": trigger.TriggerID, "run_id": trigger.RunID, "trigger_status": trigger.Status})
			message := chatpo.ChatMessage{Ulid: "schedule-notify-" + trigger.TriggerID, SessionId: task.SessionID, Role: "assistant", Content: trigger.Summary, Status: "success", Metadata: chatpo.StringJSON{Val: string(metadata)}}
			if err := s.data.DB(ctx).Create(&message).Error; err != nil {
				var count int64
				if countErr := s.data.DB(ctx).Model(&chatpo.ChatMessage{}).Where("ulid = ?", message.Ulid).Count(&count).Error; countErr != nil || count == 0 {
					notificationStatus, notificationError = orchestrationv1.NotificationFailed, err.Error()
				}
			}
		}
	}
	if err := s.orchestration.MarkScheduleTriggerReconciled(ctx, task.UserID, trigger, notificationStatus, notificationError); err != nil {
		log.ErrorwCtx(ctx, "mark schedule trigger reconciled failed", "trigger_id", trigger.TriggerID, "error_chain", log.FormatError(err))
	}
	if strings.HasPrefix(strings.TrimSpace(trigger.Summary), "ACTION_REQUIRED:") {
		params, _ := json.Marshal(map[string]any{"scheduled_task_id": task.Ulid, "goal_id": trigger.GoalID, "trigger_id": trigger.TriggerID, "result": trigger.Summary, "next_step": "Open chat to verify details and complete the action interactively."})
		approval := chatpo.ChatApproval{UserId: task.UserID, AgentId: task.AgentID, MessageId: trigger.TriggerID, ToolName: "Scheduled result review", RiskLevel: "high", Parameters: string(params), Status: "pending"}
		var count int64
		if s.data.DB(ctx).Model(&chatpo.ChatApproval{}).Where("message_id = ? AND tool_name = ?", trigger.TriggerID, approval.ToolName).Count(&count).Error == nil && count == 0 {
			_ = s.data.DB(ctx).Create(&approval).Error
		}
	}
}

func terminalTrigger(status string) bool {
	return status == orchestrationv1.ScheduleTriggerCompleted || status == orchestrationv1.ScheduleTriggerFailed || status == orchestrationv1.ScheduleTriggerCancelled
}

func scheduledCriterion(task po.ScheduledTask) string {
	description := "return a timestamped, attributable result for the scheduled monitor"
	if value := strings.TrimSpace(task.CriteriaJSON); value != "" && value != "{}" && value != "null" {
		description += "; requested criteria: " + truncate(value, 1000)
	}
	return description
}

func dueSlot(task po.ScheduledTask, now time.Time) (string, bool) {
	if cronMatches(task.CronExpr, now) {
		return now.Format("200601021504"), true
	}
	if task.MisfirePolicy != orchestrationv1.MisfireOnce || task.LastRunAt <= 0 {
		return "", false
	}
	last := time.UnixMilli(task.LastRunAt).In(now.Location()).Truncate(time.Minute)
	current := now.Truncate(time.Minute)
	for cursor, checked := last.Add(time.Minute), 0; cursor.Before(current) && checked < 1440; cursor, checked = cursor.Add(time.Minute), checked+1 {
		if cronMatches(task.CronExpr, cursor) {
			return "misfire-" + cursor.Format("200601021504"), true
		}
	}
	return "", false
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func nonEmptyDecisionReason(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "scheduled execution rejected by the user"
}
