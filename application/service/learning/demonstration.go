package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	experiencesvc "github.com/good-fish-man/agent-runtime-client/application/service/experience"
	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

var sensitiveTerms = []string{
	"api_key", "apikey", "authorization", "bearer", "cookie", "credential", "cvv", "otp", "passcode",
	"password", "private_key", "secret", "session", "token", "two_factor", "2fa",
}

const maximumDemonstrationSteps = 100

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
	} else if !operationAllowed(policy.Operations, operation) {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration operation is not registered for the capability")
	}
	if directExecutor(capability) {
		return nil, apierror.ErrBadRequest.WithMessage("demonstrations cannot record direct code execution")
	}
	if len(value.Steps) >= maximumDemonstrationSteps {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration step limit reached")
	}
	step := entity.DemonstrationStep{
		Sequence: len(value.Steps) + 1, Capability: capability, Operation: operation,
		Summary: semanticStepSummary(capability, operation),
	}
	if isSensitive(request.FieldKind) || isSensitive(request.Summary) {
		step.Summary = "Sensitive input omitted; user action required."
		step.Redacted = true
		value.PauseCount++
		value.Status = entity.DemonstrationPausedSensitive
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
		if len(actions) == 0 {
			return nil, apierror.ErrBadRequest.WithMessage("demonstration task has no semantic actions to preview")
		}
		capabilities := make([]string, 0, len(actions))
		for _, action := range actions {
			capabilities = append(capabilities, action.Capability)
		}
		policies, policyErr := s.store.CapabilityPolicies(ctx, capabilities)
		if policyErr != nil {
			return nil, policyErr
		}
		paused := false
		for _, action := range actions {
			if directExecutor(action.Capability) {
				return nil, apierror.ErrBadRequest.WithMessage("demonstration task contains a direct code executor")
			}
			policy, ok := policies[action.Capability]
			if !ok || !policy.Enabled || !operationAllowed(policy.Operations, firstNonEmpty(action.Operation, "invoke")) {
				return nil, apierror.ErrBadRequest.WithMessage("demonstration task references an unavailable semantic action")
			}
			step := entity.DemonstrationStep{
				Sequence: len(value.Steps) + 1, Capability: action.Capability, Operation: firstNonEmpty(action.Operation, "invoke"),
				Summary: semanticStepSummary(action.Capability, firstNonEmpty(action.Operation, "invoke")),
			}
			if containsSensitive(action.Arguments) {
				step.Summary = "Sensitive input omitted; user action required."
				step.Redacted = true
				value.PauseCount++
				paused = true
			}
			value.Steps = append(value.Steps, step)
		}
		if paused {
			value.Status = entity.DemonstrationPausedSensitive
			if err := s.saveDemonstration(ctx, value); err != nil {
				return nil, err
			}
			return value, nil
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
	if strings.TrimSpace(actorID) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("demonstration confirmation requires an actor")
	}
	if value.Status == entity.DemonstrationConfirmed {
		if err := s.ensureDemonstrationExperience(ctx, value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if value.Status != entity.DemonstrationPreview {
		return nil, apierror.ErrBadRequest.WithMessage("only a preview can be confirmed")
	}
	if len(value.Steps) == 0 {
		return nil, apierror.ErrBadRequest.WithMessage("empty demonstration cannot be confirmed")
	}
	capabilities := make([]string, 0, len(value.Steps))
	for _, step := range value.Steps {
		capabilities = append(capabilities, step.Capability)
	}
	policies, err := s.store.CapabilityPolicies(ctx, capabilities)
	if err != nil {
		return nil, err
	}
	for _, step := range value.Steps {
		policy, ok := policies[step.Capability]
		if !ok || !policy.Enabled || directExecutor(step.Capability) || !operationAllowed(policy.Operations, step.Operation) {
			return nil, apierror.ErrBadRequest.WithMessage("demonstration contains an action that is no longer available")
		}
	}
	value.Status = entity.DemonstrationConfirmed
	value.ConfirmedBy = actorID
	value.ConfirmedAt = time.Now().UTC()
	if err := s.saveDemonstration(ctx, value); err != nil {
		return nil, err
	}
	if err := s.ensureDemonstrationExperience(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) ensureDemonstrationExperience(ctx context.Context, value *entity.Demonstration) error {
	if s == nil || s.experiences == nil {
		return fmt.Errorf("demonstration experience store is not configured")
	}
	if value == nil || value.Status != entity.DemonstrationConfirmed || value.ConfirmedAt.IsZero() {
		return fmt.Errorf("only a confirmed demonstration can become learning evidence")
	}
	preference, err := s.experiences.GetPreference(ctx, value.OwnerID)
	if err != nil {
		return err
	}
	if preference == nil {
		return fmt.Errorf("experience preference is unavailable")
	}
	preference.Normalize()
	if !preference.LearningEnabled || value.PauseCount > 0 || demonstrationHasRedactedStep(value.Steps) {
		return nil
	}
	actions, exists, err := s.store.TaskActions(ctx, value.OwnerID, value.TaskID)
	if err != nil {
		return err
	}
	if !exists {
		return apierror.ErrNotFound.WithMessage("demonstration task not found")
	}
	capabilities := make([]string, 0, len(value.Steps))
	for _, step := range value.Steps {
		capabilities = append(capabilities, step.Capability)
	}
	policies, err := s.store.CapabilityPolicies(ctx, capabilities)
	if err != nil {
		return err
	}
	stored, eligible, err := buildDemonstrationExperience(value, actions, policies, *preference)
	if err != nil || !eligible {
		return err
	}
	created, err := s.experiences.Create(ctx, stored)
	if err != nil {
		return err
	}
	if created {
		return nil
	}
	existing, err := s.experiences.Find(ctx, value.OwnerID, stored.Experience.ExperienceID)
	if err != nil {
		return err
	}
	if existing == nil || existing.TaskID != stored.Experience.TaskID || existing.Provenance.GeneratedBy != "human-demonstration/v2" {
		return fmt.Errorf("demonstration experience idempotency conflict")
	}
	return nil
}

func buildDemonstrationExperience(value *entity.Demonstration, actions []entity.SemanticAction, policies map[string]entity.CapabilityPolicy, preference experienceentity.Preference) (*experienceentity.StoredExperience, bool, error) {
	if value == nil || value.Status != entity.DemonstrationConfirmed || value.ConfirmedAt.IsZero() {
		return nil, false, fmt.Errorf("confirmed demonstration is required")
	}
	redactor := experiencesvc.NewRedactor()
	sanitizedTitle, titleRedactions := redactor.Sanitize(value.Title, "$.goal_summary")
	goal, _ := sanitizedTitle.(string)
	if strings.TrimSpace(goal) == "" || len(titleRedactions) > 0 {
		return nil, false, nil
	}
	siteScope := demonstrationSiteScope(actions)
	hasBrowserStep := false
	actionRefs := make([]experienceentity.ActionRef, 0, len(value.Steps))
	planParts := make([]string, 0, len(value.Steps))
	searchParts := []string{goal}
	for _, step := range value.Steps {
		if step.Redacted {
			return nil, false, nil
		}
		policy, ok := policies[step.Capability]
		if !ok || !policy.Enabled || directExecutor(step.Capability) || !operationAllowed(policy.Operations, step.Operation) {
			return nil, false, fmt.Errorf("demonstration contains an unavailable semantic action")
		}
		sanitizedSummary, summaryRedactions := redactor.Sanitize(step.Summary, "$.steps["+strconv.Itoa(step.Sequence)+"].summary")
		summary, _ := sanitizedSummary.(string)
		if strings.TrimSpace(summary) == "" || len(summaryRedactions) > 0 {
			return nil, false, nil
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(step.Capability)), "browser.") {
			hasBrowserStep = true
		}
		actionRefs = append(actionRefs, experienceentity.ActionRef{
			ActionID: value.DemonstrationID + ":step:" + strconv.Itoa(step.Sequence),
			StepID:   "step-" + strconv.Itoa(step.Sequence), Capability: step.Capability,
			Operation: step.Operation, Risk: policy.Risk, Outcome: experienceentity.OutcomeSucceeded,
		})
		planParts = append(planParts, strconv.Itoa(step.Sequence)+". "+summary)
		searchParts = append(searchParts, step.Capability, step.Operation, summary)
	}
	if hasBrowserStep && siteScope == "" {
		return nil, false, nil
	}
	if siteScope == "" {
		siteScope = "not-applicable"
	}
	preference.Normalize()
	now := value.ConfirmedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	experienceID := demonstrationExperienceID(value.DemonstrationID)
	verificationEvidence := "demonstration:" + value.DemonstrationID + ":human-confirmation"
	experience := experienceentity.Experience{
		Schema: experienceentity.Schema, ExperienceID: experienceID, OwnerID: value.OwnerID,
		TaskID: "demonstration:" + value.DemonstrationID, Status: experienceentity.StatusReady,
		GoalSummary: goal, TaskType: "human_demonstration", Domain: "learning",
		Intent:                 map[string]any{"source": "human_demonstration", "site_scope": siteScope},
		EnvironmentFingerprint: "human-demonstration:" + value.TaskID,
		PlanSummary:            strings.Join(planParts, " "), DecisionSummary: "User confirmed the sanitized semantic demonstration preview.",
		ActionRefs: actionRefs, Outcome: experienceentity.OutcomeSucceeded,
		Verification:      experienceentity.Verification{Passed: true, Summary: "Human-confirmed semantic demonstration.", EvidenceIDs: []string{verificationEvidence}},
		HumanIntervention: experienceentity.HumanIntervention{Required: true, ApprovalCount: 1},
		Sensitivity:       experienceentity.SensitivityInternal,
		Retention:         experienceentity.RetentionPolicy{Days: preference.RetentionDays, PayloadMode: experienceentity.PayloadSeparate, DeleteAt: now.AddDate(0, 0, preference.RetentionDays)},
		Provenance:        experienceentity.Provenance{TraceID: value.TraceID, Protocol: entity.Schema, GeneratedBy: "human-demonstration/v2", GeneratedAt: now},
		CreatedAt:         now, UpdatedAt: now,
	}
	if err := experience.Validate(); err != nil {
		return nil, false, err
	}
	payload, err := json.Marshal(experience)
	if err != nil {
		return nil, false, err
	}
	return &experienceentity.StoredExperience{
		Experience: experience, Payload: string(payload), SearchText: strings.Join(searchParts, " "),
	}, true, nil
}

func demonstrationHasRedactedStep(steps []entity.DemonstrationStep) bool {
	for _, step := range steps {
		if step.Redacted {
			return true
		}
	}
	return false
}

func demonstrationExperienceID(demonstrationID string) string {
	return "demo-exp-" + strings.TrimSpace(demonstrationID)
}

func demonstrationSiteScope(actions []entity.SemanticAction) string {
	for _, action := range actions {
		if site := safeSiteScope(action.Arguments); site != "" {
			return site
		}
	}
	return ""
}

func safeSiteScope(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			switch normalizedKey {
			case "domain", "host", "hostname", "href", "origin", "site", "site_scope", "target_url", "url":
				if site := normalizeSiteScope(fmt.Sprint(typed[key])); site != "" {
					return site
				}
			}
		}
		for _, key := range keys {
			if site := safeSiteScope(typed[key]); site != "" {
				return site
			}
		}
	case []any:
		for _, item := range typed {
			if site := safeSiteScope(item); site != "" {
				return site
			}
		}
	}
	return ""
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
	value.ConfirmedBy = ""
	value.ConfirmedAt = time.Time{}
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

func semanticStepSummary(capability, operation string) string {
	return "Semantic action: " + strings.TrimSpace(capability) + "." + strings.TrimSpace(operation)
}

func operationAllowed(operations []string, operation string) bool {
	if len(operations) == 0 {
		return false
	}
	operation = strings.TrimSpace(operation)
	for _, allowed := range operations {
		if strings.TrimSpace(allowed) == operation {
			return true
		}
	}
	return false
}
