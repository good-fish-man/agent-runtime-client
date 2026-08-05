package scheduledtask

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	runtimesvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	agentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/agent"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	jobpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/job"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/scheduledtask"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	log "github.com/good-fish-man/logx"
)

const (
	ActionConfirmBeforeCommit = "confirm_before_commit"
	maxConcurrentRuns         = 3
	resultLimit               = 12000
	defaultScanInterval       = time.Minute
)

type CreateRequest struct {
	UserID    string         `json:"user_id"`
	AgentID   string         `json:"agent_id"`
	SessionID string         `json:"session_id"`
	Name      string         `json:"name"`
	TaskType  string         `json:"task_type"`
	Cron      string         `json:"cron"`
	Timezone  string         `json:"timezone"`
	Prompt    string         `json:"prompt"`
	Criteria  map[string]any `json:"criteria"`
}

type UpdateRequest struct {
	Status string `json:"status"`
}
type ApprovalDecision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type Service struct {
	data         *data.Data
	runtime      *runtimesvc.RuntimeService
	cancel       context.CancelFunc
	sem          chan struct{}
	scanInterval time.Duration
}

func NewService(d *data.Data, runtime *runtimesvc.RuntimeService, scanInterval ...time.Duration) *Service {
	interval := defaultScanInterval
	if len(scanInterval) > 0 && scanInterval[0] > 0 {
		interval = scanInterval[0]
	}
	return &Service{data: d, runtime: runtime, sem: make(chan struct{}, maxConcurrentRuns), scanInterval: interval}
}

func InternalTokenValid(value string) bool {
	want := strings.TrimSpace(os.Getenv(consts.EnvInternalServiceToken))
	value = strings.TrimSpace(value)
	return want != "" && len(value) == len(want) && subtle.ConstantTimeCompare([]byte(value), []byte(want)) == 1
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
	item := &po.ScheduledTask{UserID: req.UserID, AgentID: req.AgentID, SessionID: strings.TrimSpace(req.SessionID), Name: strings.TrimSpace(req.Name), TaskType: typeName, CronExpr: strings.TrimSpace(req.Cron), Timezone: location, Prompt: strings.TrimSpace(req.Prompt), CriteriaJSON: string(criteria), ActionMode: ActionConfirmBeforeCommit, Status: po.StatusActive}
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
	return nil
}

func (s *Service) Start(parent context.Context) {
	if s == nil || s.data == nil || s.runtime == nil || s.cancel != nil {
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
		location := time.Local
		if task.Timezone != "" && task.Timezone != "Local" {
			if loaded, err := time.LoadLocation(task.Timezone); err == nil {
				location = loaded
			}
		}
		now := time.Now().In(location)
		if !cronMatches(task.CronExpr, now) {
			continue
		}
		slot := now.Format("200601021504")
		claim := s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ? AND status = ? AND (last_slot = '' OR last_slot <> ?)", task.Ulid, po.StatusActive, slot).Updates(map[string]any{"last_slot": slot, "last_run_at": time.Now().UnixMilli(), "last_status": jobpo.JobStatusRunning})
		if claim.Error != nil || claim.RowsAffected == 0 {
			continue
		}
		select {
		case s.sem <- struct{}{}:
			go func() { defer func() { <-s.sem }(); s.execute(task) }()
		default:
			log.Warnf("scheduled task concurrency limit reached; task=%s", task.Ulid)
		}
	}
}

func (s *Service) execute(task po.ScheduledTask) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(authctx.WithUserID(context.Background(), task.UserID), 5*time.Minute)
	defer cancel()
	job := jobpo.JobExecutionPO{AgentId: task.AgentID, SessionId: task.SessionID, Status: jobpo.JobStatusRunning, TriggerTime: started.UnixMilli(), StartedAt: started.UnixMilli(), InputSummary: truncate(task.Prompt, 500)}
	var agent agentpo.SysAgent
	_ = s.data.DB(ctx).Where("ulid = ?", task.AgentID).First(&agent).Error
	job.AgentName = agent.Name
	if err := s.data.DB(ctx).Create(&job).Error; err != nil {
		log.Errorf("create scheduled execution log failed: %v", err)
	}
	prompt := task.Prompt + "\n\n[BACKGROUND MONITOR SAFETY] Query and report availability only. Do not purchase, reserve, submit an appointment, accept terms, enter payment, bypass queues, or solve CAPTCHA. Compare findings against every requested criterion. Start the final answer with exactly ACTION_REQUIRED: only when the criteria are satisfied; otherwise start with exactly NO_ACTION:. Include the exact option, price, source URL, and timestamp."
	result, err := s.runtime.Run(ctx, &runtimedto.RunReq{Prompt: prompt, Context: map[string]any{"user_id": task.UserID, "agent_id": task.AgentID, "session_id": task.SessionID, "scheduled_task_id": task.Ulid, "background_monitor": true}, RequestID: "scheduled-" + ulid.New()})
	finished := time.Now()
	updates := map[string]any{"last_run_at": started.UnixMilli(), "execution_count": task.ExecutionCount + 1}
	job.FinishedAt, job.LatencyMs = finished.UnixMilli(), finished.Sub(started).Milliseconds()
	if err != nil {
		job.Status, job.ErrorMsg = jobpo.JobStatusFailed, truncate(err.Error(), resultLimit)
		updates["last_status"], updates["last_error"] = job.Status, job.ErrorMsg
	} else {
		content := truncate(result.Content, resultLimit)
		job.Status, job.OutputSummary, job.OutputFull, job.TokensUsed = jobpo.JobStatusSuccess, truncate(content, 1000), content, int(result.TokensUsed)
		updates["last_status"], updates["last_result"], updates["last_error"] = job.Status, content, ""
		if strings.HasPrefix(strings.TrimSpace(content), "ACTION_REQUIRED:") {
			params, _ := json.Marshal(map[string]any{"scheduled_task_id": task.Ulid, "task_name": task.Name, "task_type": task.TaskType, "result": content, "next_step": "Open chat to verify details and complete the action interactively."})
			approval := chatpo.ChatApproval{UserId: task.UserID, AgentId: task.AgentID, MessageId: job.Ulid, ToolName: "Scheduled result review", RiskLevel: "high", Parameters: string(params), Status: "pending"}
			if createErr := s.data.DB(ctx).Create(&approval).Error; createErr != nil {
				log.Errorf("create scheduled result approval failed: %v", createErr)
			}
		}
	}
	_ = s.data.DB(ctx).Model(&jobpo.JobExecutionPO{}).Where("ulid = ?", job.Ulid).Updates(&job).Error
	_ = s.data.DB(ctx).Model(&po.ScheduledTask{}).Where("ulid = ?", task.Ulid).Updates(updates).Error
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
