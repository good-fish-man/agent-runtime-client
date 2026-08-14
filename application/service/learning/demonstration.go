package learning

import (
	"context"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

var sensitiveTerms = []string{
	"api_key", "authorization", "cookie", "credential", "cvv", "otp", "passcode",
	"password", "secret", "token", "two_factor", "2fa",
}

type StartDemonstrationRequest struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
}

type RecordDemonstrationStepRequest struct {
	Capability string `json:"capability"`
	Operation  string `json:"operation"`
	Summary    string `json:"summary"`
	FieldKind  string `json:"field_kind,omitempty"`
}

type EditDemonstrationRequest struct {
	Title            string   `json:"title,omitempty"`
	StepSummaries    []string `json:"step_summaries,omitempty"`
	ExpectedRevision int64    `json:"expected_revision"`
}

func (s *Service) StartDemonstration(ctx context.Context, ownerID string, request StartDemonstrationRequest) (*entity.Demonstration, error) {
	taskID := strings.TrimSpace(request.TaskID)
	title := strings.TrimSpace(request.Title)
	if taskID == "" || title == "" {
		return nil, apierror.ErrBadRequest.WithMessage("task_id and title are required")
	}
	_, exists, err := s.store.TaskActions(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apierror.ErrNotFound.WithMessage("demonstration task not found")
	}
	now := time.Now().UTC()
	value := &entity.Demonstration{
		Schema: entity.Schema, DemonstrationID: ulid.New(), OwnerID: ownerID, TaskID: taskID,
		Status: entity.DemonstrationRecording, Title: title, Steps: []entity.DemonstrationStep{},
		Revision: 1, TraceID: traceID(ctx), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateDemonstration(ctx, *value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) RecordDemonstrationStep(ctx context.Context, ownerID, demonstrationID string, request RecordDemonstrationStepRequest) (*entity.Demonstration, error) {
	value, err := s.requireDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value.Status != entity.DemonstrationRecording {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration is not recording")
	}
	capability := strings.TrimSpace(request.Capability)
	operation := strings.TrimSpace(request.Operation)
	if capability == "" || operation == "" {
		return nil, apierror.ErrBadRequest.WithMessage("capability and operation are required")
	}
	policies, err := s.store.CapabilityPolicies(ctx, []string{capability})
	if err != nil {
		return nil, err
	}
	if policy, ok := policies[capability]; !ok || !policy.Enabled {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration can only reference a registered capability")
	}
	if directExecutor(capability) {
		return nil, apierror.ErrBadRequest.WithMessage("demonstrations cannot record direct code execution")
	}
	step := entity.DemonstrationStep{Sequence: len(value.Steps) + 1, Capability: capability, Operation: operation}
	if isSensitive(request.FieldKind) || isSensitive(request.Summary) {
		step.Summary = "Sensitive input omitted; user action required."
		step.Redacted = true
		value.PauseCount++
		value.Status = entity.DemonstrationPausedSensitive
	} else {
		step.Summary = compact(request.Summary, 240)
	}
	value.Steps = append(value.Steps, step)
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) ResumeDemonstration(ctx context.Context, ownerID, demonstrationID string) (*entity.Demonstration, error) {
	value, err := s.requireDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value.Status != entity.DemonstrationPausedSensitive {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration is not paused for sensitive input")
	}
	value.Status = entity.DemonstrationRecording
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) PreviewDemonstration(ctx context.Context, ownerID, demonstrationID string) (*entity.Demonstration, error) {
	value, err := s.requireDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value.Status != entity.DemonstrationRecording {
		return nil, apierror.ErrBadRequest.WithMessage("resume the demonstration before previewing it")
	}
	actions, exists, err := s.store.TaskActions(ctx, ownerID, value.TaskID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, apierror.ErrNotFound.WithMessage("demonstration task not found")
	}
	if len(value.Steps) == 0 {
		for _, action := range actions {
			if directExecutor(action.Capability) {
				continue
			}
			step := entity.DemonstrationStep{
				Sequence: len(value.Steps) + 1, Capability: action.Capability, Operation: firstNonEmpty(action.Operation, "invoke"),
				Summary: "Demonstrated semantic action.",
			}
			if containsSensitive(action.Arguments) {
				step.Summary = "Sensitive input omitted; user action required."
				step.Redacted = true
				value.PauseCount++
			}
			value.Steps = append(value.Steps, step)
		}
	}
	value.Status = entity.DemonstrationPreview
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) EditDemonstration(ctx context.Context, ownerID, demonstrationID string, request EditDemonstrationRequest) (*entity.Demonstration, error) {
	value, err := s.requireDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value.Status != entity.DemonstrationPreview {
		return nil, apierror.ErrBadRequest.WithMessage("only a preview can be edited")
	}
	if request.ExpectedRevision > 0 && request.ExpectedRevision != value.Revision {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration revision changed")
	}
	if title := strings.TrimSpace(request.Title); title != "" {
		value.Title = compact(title, 255)
	}
	if len(request.StepSummaries) > 0 {
		if len(request.StepSummaries) != len(value.Steps) {
			return nil, apierror.ErrBadRequest.WithMessage("step_summaries must match the preview step count")
		}
		for index := range value.Steps {
			if value.Steps[index].Redacted {
				continue
			}
			if isSensitive(request.StepSummaries[index]) {
				return nil, apierror.ErrBadRequest.WithMessage("step summary may not contain sensitive input")
			}
			value.Steps[index].Summary = compact(request.StepSummaries[index], 240)
		}
	}
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) ConfirmDemonstration(ctx context.Context, ownerID, actorID, demonstrationID string) (*entity.Demonstration, error) {
	value, err := s.requireDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value.Status != entity.DemonstrationPreview {
		return nil, apierror.ErrBadRequest.WithMessage("only a preview can be confirmed")
	}
	value.Status = entity.DemonstrationConfirmed
	value.ConfirmedBy = actorID
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) DiscardDemonstration(ctx context.Context, ownerID, demonstrationID string) (*entity.Demonstration, error) {
	value, err := s.requireDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value.Status == entity.DemonstrationConfirmed {
		return nil, apierror.ErrBadRequest.WithMessage("confirmed demonstrations are immutable")
	}
	value.Status = entity.DemonstrationDiscarded
	value.Steps = nil
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) ListDemonstrations(ctx context.Context, ownerID string, limit int) ([]entity.Demonstration, error) {
	return s.store.ListDemonstrations(ctx, ownerID, limit)
}

func (s *Service) requireDemonstration(ctx context.Context, ownerID, demonstrationID string) (*entity.Demonstration, error) {
	value, err := s.store.FindDemonstration(ctx, ownerID, demonstrationID)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, apierror.ErrNotFound.WithMessage("demonstration not found")
	}
	return value, nil
}

func (s *Service) saveDemonstration(ctx context.Context, value *entity.Demonstration) error {
	expected := value.Revision
	value.Revision++
	value.UpdatedAt = time.Now().UTC()
	value.TraceID = traceID(ctx)
	if err := s.store.SaveDemonstration(ctx, *value, expected); err != nil {
		value.Revision = expected
		return err
	}
	return nil
}

func directExecutor(capability string) bool {
	value := strings.ToLower(strings.TrimSpace(capability))
	for _, denied := range []string{"code.execute", "javascript.execute", "python.execute", "shell.execute", "terminal.execute", "terminal.run"} {
		if value == denied || strings.HasPrefix(value, denied+".") {
			return true
		}
	}
	return false
}

func isSensitive(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, term := range sensitiveTerms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func containsSensitive(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSensitive(key) || containsSensitive(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitive(child) {
				return true
			}
		}
	case string:
		return isSensitive(typed)
	}
	return false
}
