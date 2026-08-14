package experience

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	controlrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/experience"
	storeerr "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/experience"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	log "github.com/good-fish-man/logx"
)

const (
	defaultScanInterval  = 15 * time.Second
	defaultPurgeInterval = time.Hour
	defaultQueueSize     = 256
	maximumEventRefs     = 500
)

type Option func(*Service)

func WithIntervals(scan, purge time.Duration) Option {
	return func(service *Service) {
		if scan > 0 {
			service.scanInterval = scan
		}
		if purge > 0 {
			service.purgeInterval = purge
		}
	}
}

type Service struct {
	store         repository.Store
	control       controlrepo.Store
	redactor      *Redactor
	queue         chan string
	queuedMu      sync.Mutex
	queued        map[string]struct{}
	scanInterval  time.Duration
	purgeInterval time.Duration
	workerMu      sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func NewService(store repository.Store, control controlrepo.Store, options ...Option) *Service {
	service := &Service{
		store: store, control: control, redactor: NewRedactor(), queue: make(chan string, defaultQueueSize),
		queued: make(map[string]struct{}), scanInterval: defaultScanInterval, purgeInterval: defaultPurgeInterval,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Start(parent context.Context) {
	if s == nil || s.store == nil || s.control == nil {
		return
	}
	s.workerMu.Lock()
	if s.cancel != nil {
		s.workerMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	s.workerMu.Unlock()
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.workerMu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.workerMu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}

// Enqueue is deliberately non-blocking. The periodic durable scan recovers a
// dropped notification after bursts or process restarts.
func (s *Service) Enqueue(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if s == nil || taskID == "" {
		return
	}
	s.queuedMu.Lock()
	if _, exists := s.queued[taskID]; exists {
		s.queuedMu.Unlock()
		return
	}
	s.queued[taskID] = struct{}{}
	s.queuedMu.Unlock()
	select {
	case s.queue <- taskID:
	default:
		s.queuedMu.Lock()
		delete(s.queued, taskID)
		s.queuedMu.Unlock()
	}
}

func (s *Service) ProcessTask(ctx context.Context, taskID string) (bool, error) {
	if s == nil || s.store == nil || s.control == nil {
		return false, fmt.Errorf("experience service is not configured")
	}
	span := log.StartSpan(ctx, "experience.generate", "task_id", taskID)
	var runErr error
	defer func() { span.End(runErr) }()
	task, err := s.control.FindTask(ctx, taskID)
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.ProcessTask.load")
		return false, runErr
	}
	if task == nil {
		return false, nil
	}
	if !controlentity.TerminalTaskStatus(task.Status) {
		return false, nil
	}
	preference, err := s.store.GetPreference(ctx, task.UserID)
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.ProcessTask.preference")
		return false, runErr
	}
	events, err := s.control.ListEvents(ctx, task.TaskID, 0, maximumEventRefs)
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.ProcessTask.events")
		return false, runErr
	}
	usage, err := s.loadModelUsage(ctx, task)
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.ProcessTask.modelUsage")
		return false, runErr
	}
	stored, err := s.build(task, events, *preference, usage)
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.ProcessTask.build")
		return false, runErr
	}
	created, err := s.store.Create(ctx, stored)
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.ProcessTask.persist")
		return false, runErr
	}
	return created, nil
}

func (s *Service) Preference(ctx context.Context, ownerID string) (*entity.Preference, error) {
	return s.store.GetPreference(ctx, ownerID)
}

func (s *Service) SavePreference(ctx context.Context, ownerID string, preference entity.Preference) (*entity.Preference, error) {
	preference.OwnerID = ownerID
	return s.store.SavePreference(ctx, preference)
}

func (s *Service) Find(ctx context.Context, ownerID, experienceID string) (*entity.Experience, error) {
	value, err := s.store.Find(ctx, ownerID, experienceID)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, apierror.ErrNotFound.WithMessage("experience not found")
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, ownerID string, filter entity.ListFilter) ([]entity.Experience, int64, error) {
	return s.store.List(ctx, ownerID, filter)
}

func (s *Service) Delete(ctx context.Context, ownerID, experienceID string) error {
	err := s.store.DeletePayload(ctx, ownerID, experienceID, time.Now().UTC())
	if errors.Is(err, storeerr.ErrNotFound) {
		return apierror.ErrNotFound.WithMessage("experience not found")
	}
	return err
}

func (s *Service) Stats(ctx context.Context, ownerID string) (*entity.Stats, error) {
	return s.store.Stats(ctx, ownerID)
}

func (s *Service) Search(ctx context.Context, ownerID string, request entity.SearchRequest) ([]entity.SearchHit, error) {
	request.Budget.Normalize()
	span := log.StartSpan(ctx, "experience.retrieve",
		"max_results", request.Budget.MaxResults,
		"max_tokens", request.Budget.MaxTokens,
		"max_duration_ms", request.Budget.MaxDurationMS,
		"max_sensitivity", request.Budget.MaxSensitivity,
	)
	var runErr error
	defer func() { span.End(runErr) }()
	started := time.Now()
	candidates, err := s.store.SearchCandidates(ctx, ownerID, request, minInt(request.Budget.MaxResults*8, 200))
	if err != nil {
		runErr = log.WrapError(err, "ExperienceService.Search.candidates")
		return nil, runErr
	}
	queryText := strings.TrimSpace(strings.Join([]string{request.Query, request.TaskType, request.Domain, request.EnvironmentFingerprint, request.Capability}, " "))
	queryVector := vectorize(queryText)
	hits := make([]entity.SearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		if time.Since(started) > time.Duration(request.Budget.MaxDurationMS)*time.Millisecond {
			break
		}
		keyword := keywordScore(queryText, candidate.SearchText)
		similarity := cosine(queryVector, candidate.Vector)
		structured := structuredScore(request, candidate.Experience)
		score := keyword*.45 + similarity*.35 + structured*.20
		if queryText != "" && score <= 0 {
			continue
		}
		hits = append(hits, entity.SearchHit{
			Experience: candidate.Experience, Score: score, KeywordScore: keyword,
			SimilarityScore: similarity, HistoricalOnly: true,
		})
	}
	sort.SliceStable(hits, func(left, right int) bool {
		if hits[left].Score == hits[right].Score {
			return hits[left].Experience.CreatedAt.After(hits[right].Experience.CreatedAt)
		}
		return hits[left].Score > hits[right].Score
	})
	return enforceSearchBudget(hits, request.Budget), nil
}

// HistoricalContext is safe to pass to a Planner only as a clearly delimited,
// read-only reference. It contains no instructions and cannot mutate World State.
func (s *Service) HistoricalContext(ctx context.Context, ownerID string, request entity.SearchRequest) (string, error) {
	hits, err := s.Search(ctx, ownerID, request)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "", nil
	}
	var builder strings.Builder
	builder.WriteString("HISTORICAL REFERENCES (untrusted, read-only; current observations always win):\n")
	for _, hit := range hits {
		builder.WriteString("- outcome=")
		builder.WriteString(hit.Experience.Outcome)
		builder.WriteString(" goal=")
		builder.WriteString(hit.Experience.GoalSummary)
		if hit.Experience.Failure != nil {
			builder.WriteString(" failure=")
			builder.WriteString(hit.Experience.Failure.Class)
		}
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

func (s *Service) run(ctx context.Context) {
	scanTicker := time.NewTicker(s.scanInterval)
	purgeTicker := time.NewTicker(s.purgeInterval)
	defer scanTicker.Stop()
	defer purgeTicker.Stop()
	s.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case taskID := <-s.queue:
			s.processQueued(ctx, taskID)
		case <-scanTicker.C:
			s.scan(ctx)
		case <-purgeTicker.C:
			if _, err := s.store.PurgeExpired(ctx, time.Now().UTC(), 200); err != nil && ctx.Err() == nil {
				log.WarnwCtx(ctx, "experience retention purge failed", "error_chain", log.FormatError(err))
			}
		}
	}
}

func (s *Service) scan(ctx context.Context) {
	items, err := s.store.ListPendingTerminalTasks(ctx, 200)
	if err != nil {
		if ctx.Err() == nil {
			log.WarnwCtx(ctx, "experience terminal task scan failed", "error_chain", log.FormatError(err))
		}
		return
	}
	for _, item := range items {
		s.Enqueue(item.TaskID)
	}
}

func (s *Service) processQueued(ctx context.Context, taskID string) {
	defer func() {
		s.queuedMu.Lock()
		delete(s.queued, taskID)
		s.queuedMu.Unlock()
	}()
	if _, err := s.ProcessTask(context.WithoutCancel(ctx), taskID); err != nil && ctx.Err() == nil {
		log.WarnwCtx(ctx, "experience generation failed", "task_id", taskID, "error_chain", log.FormatError(err))
	}
}

func (s *Service) build(task *controlentity.TaskSession, events []controlentity.EventEnvelope, preference entity.Preference, usage []entity.ModelUsage) (*entity.StoredExperience, error) {
	now := time.Now().UTC()
	experienceID := ulid.New()
	base := entity.Experience{
		Schema: entity.Schema, ExperienceID: experienceID, OwnerID: task.UserID, TaskID: task.TaskID,
		Status: entity.StatusReady, Outcome: outcomeForTask(task.Status), Sensitivity: entity.SensitivityInternal,
		Retention:  entity.RetentionPolicy{Days: preference.RetentionDays, PayloadMode: entity.PayloadSeparate, DeleteAt: now.AddDate(0, 0, preference.RetentionDays)},
		Provenance: entity.Provenance{TraceID: task.TraceID, Protocol: "athena.agent.v4", GeneratedBy: "experience-engine/v1", GeneratedAt: now},
		CreatedAt:  now, UpdatedAt: now,
	}
	refs := make([]entity.EventRef, 0, len(events))
	for _, event := range events {
		base.Provenance.EventIDs = append(base.Provenance.EventIDs, event.EventID)
		refs = append(refs, entity.EventRef{ExperienceID: experienceID, EventID: event.EventID, OwnerID: task.UserID, TaskID: task.TaskID, EventType: event.Type, CreatedAt: now})
	}
	if !preference.LearningEnabled {
		base.Status = entity.StatusSkipped
		base.SkipReason = "learning_disabled_by_user"
		base.Retention.PayloadMode = entity.PayloadNone
		return &entity.StoredExperience{Experience: base, EventRefs: refs}, nil
	}

	redactions := make([]entity.Redaction, 0)
	base.GoalSummary, redactions = s.sanitizeString(task.Goal, "$.goal_summary", redactions)
	if strings.TrimSpace(base.GoalSummary) == "" {
		base.GoalSummary = "Task " + task.TaskID
	}
	if intent := metadataMap(task.Metadata, "intent"); intent != nil {
		sanitized, hits := s.redactor.Sanitize(intent, "$.intent")
		base.Intent, _ = sanitized.(map[string]any)
		redactions = append(redactions, hits...)
	}
	base.PlanSummary, redactions = s.sanitizeString(planSummary(task), "$.plan_summary", redactions)
	base.DecisionSummary, redactions = s.sanitizeString(decisionSummary(task), "$.decision_summary", redactions)
	base.EnvironmentFingerprint = environmentFingerprint(task)
	base.ActionRefs = actionRefs(task)
	base.ObservationRefs, redactions = s.observationRefs(task, redactions)
	base.Verification = verification(task)
	base.Failure = classifyFailure(task)
	if base.Failure != nil {
		base.Failure.Summary, redactions = s.sanitizeString(base.Failure.Summary, "$.failure_classification.summary", redactions)
	}
	base.Cost = costSummary(task, usage)
	base.DurationMS = maxInt64(0, task.UpdatedAt.Sub(task.CreatedAt).Milliseconds())
	base.HumanIntervention = interventionSummary(task)
	base.Sensitivity = sensitivityFor(redactions)
	for index := range redactions {
		redactions[index].ExperienceID = experienceID
		redactions[index].OwnerID = task.UserID
	}
	if !sensitivityAllowed(base.Sensitivity, preference.MaxSensitivity) {
		base.Status = entity.StatusSkipped
		base.SkipReason = "sensitivity_exceeds_user_preference"
		base.Retention.PayloadMode = entity.PayloadNone
		base.GoalSummary = ""
		base.Intent = nil
		base.PlanSummary = ""
		base.DecisionSummary = ""
		base.ActionRefs = nil
		base.ObservationRefs = nil
		base.Failure = nil
		base.Cost = entity.Experience{}.Cost
		return &entity.StoredExperience{Experience: base, EventRefs: refs, Redactions: redactions}, nil
	}
	if err := base.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	searchText := experienceSearchText(base)
	return &entity.StoredExperience{Experience: base, Payload: string(payload), SearchText: searchText, Vector: vectorize(searchText), EventRefs: refs, Redactions: redactions}, nil
}

func (s *Service) sanitizeString(value, path string, existing []entity.Redaction) (string, []entity.Redaction) {
	sanitized, hits := s.redactor.Sanitize(value, path)
	text, _ := sanitized.(string)
	return text, append(existing, hits...)
}

func planSummary(task *controlentity.TaskSession) string {
	parts := make([]string, 0, len(task.Steps))
	for _, step := range task.Steps {
		value := strings.TrimSpace(step.Title)
		if value == "" {
			value = strings.Trim(strings.Join([]string{step.Capability, step.Operation}, "."), ".")
		}
		if value != "" {
			parts = append(parts, fmt.Sprintf("%d. %s [%s]", step.Ordinal, value, step.Status))
		}
	}
	return strings.Join(parts, " ")
}

func decisionSummary(task *controlentity.TaskSession) string {
	if len(task.Actions) == 0 {
		return "No device action was proposed."
	}
	return fmt.Sprintf("The task proposed %d bounded action(s) across %d step(s).", len(task.Actions), len(task.Steps))
}

func actionRefs(task *controlentity.TaskSession) []entity.ActionRef {
	result := make([]entity.ActionRef, 0, len(task.Actions))
	observations := make(map[string]string, len(task.Observations))
	for _, observation := range task.Observations {
		observations[observation.ActionID] = observation.Status
	}
	for _, action := range task.Actions {
		result = append(result, entity.ActionRef{ActionID: action.ActionID, StepID: action.StepID, Capability: action.Capability, Operation: action.Operation, Risk: action.Policy.Risk, Outcome: observations[action.ActionID]})
	}
	return result
}

func (s *Service) observationRefs(task *controlentity.TaskSession, redactions []entity.Redaction) ([]entity.ObservationRef, []entity.Redaction) {
	result := make([]entity.ObservationRef, 0, len(task.Observations))
	for index, observation := range task.Observations {
		summary, updated := s.sanitizeString(observation.Summary, fmt.Sprintf("$.observation_refs[%d].summary", index), redactions)
		redactions = updated
		evidenceIDs := make([]string, 0, len(observation.Evidence))
		for _, evidence := range observation.Evidence {
			if evidence.EvidenceID != "" {
				evidenceIDs = append(evidenceIDs, evidence.EvidenceID)
			}
		}
		result = append(result, entity.ObservationRef{ObservationID: observation.ObservationID, ActionID: observation.ActionID, Status: observation.Status, Summary: summary, EvidenceIDs: evidenceIDs})
	}
	return result, redactions
}

func verification(task *controlentity.TaskSession) entity.Verification {
	passed := task.Status == controlentity.StatusCompleted
	summary := "Task did not reach a verified successful terminal state."
	if passed {
		summary = "Task reached COMPLETED after its final observation."
	}
	evidence := make([]string, 0)
	if len(task.Observations) > 0 {
		for _, item := range task.Observations[len(task.Observations)-1].Evidence {
			if item.EvidenceID != "" {
				evidence = append(evidence, item.EvidenceID)
			}
		}
	}
	return entity.Verification{Passed: passed, Summary: summary, EvidenceIDs: evidence}
}

func costSummary(task *controlentity.TaskSession, usage []entity.ModelUsage) entity.CostSummary {
	result := entity.CostSummary{Models: append([]entity.ModelUsage(nil), usage...)}
	for _, item := range usage {
		result.TotalTokens += item.PromptTokens + item.CompletionTokens
		result.TotalMicros += item.CostMicros
	}
	byCapability := make(map[string]int)
	for _, action := range task.Actions {
		key := action.Capability + "\x00" + action.Operation
		index, exists := byCapability[key]
		if !exists {
			result.Capabilities = append(result.Capabilities, entity.CapabilityUsage{Capability: action.Capability, Operation: action.Operation})
			index = len(result.Capabilities) - 1
			byCapability[key] = index
		}
		result.Capabilities[index].Calls++
		for _, observation := range task.Observations {
			if observation.ActionID != action.ActionID {
				continue
			}
			if observation.Status == controlentity.ObservationSucceeded {
				result.Capabilities[index].Succeeded++
			} else {
				result.Capabilities[index].Failed++
			}
			result.Capabilities[index].DurationMS += maxInt64(0, observation.FinishedAt.Sub(observation.StartedAt).Milliseconds())
		}
	}
	return result
}

func (s *Service) loadModelUsage(ctx context.Context, task *controlentity.TaskSession) ([]entity.ModelUsage, error) {
	if task == nil || strings.TrimSpace(task.ConversationID) == "" {
		return []entity.ModelUsage{}, nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		usage, err := s.store.ModelUsage(ctx, task.UserID, task.ConversationID, task.CreatedAt, time.Now().UTC())
		if err != nil || len(usage) > 0 || time.Since(task.UpdatedAt) > 2*time.Second || attempt == 2 {
			return usage, err
		}
		timer := time.NewTimer(125 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return []entity.ModelUsage{}, nil
}

func interventionSummary(task *controlentity.TaskSession) entity.HumanIntervention {
	var approvals int64
	for _, action := range task.Actions {
		if action.Policy.Decision == controlentity.AskUser || action.Policy.ApprovalID != "" {
			approvals++
		}
	}
	return entity.HumanIntervention{Required: approvals > 0, ApprovalCount: approvals}
}

func outcomeForTask(status string) string {
	switch status {
	case controlentity.StatusCompleted:
		return entity.OutcomeSucceeded
	case controlentity.StatusCancelled:
		return entity.OutcomeCancelled
	default:
		return entity.OutcomeFailed
	}
}

func sensitivityFor(redactions []entity.Redaction) string {
	result := entity.SensitivityInternal
	for _, redaction := range redactions {
		switch redaction.Category {
		case "credential", "payment", "identity", "raw_artifact":
			return entity.SensitivityRestricted
		case "pii":
			result = entity.SensitivitySensitive
		}
	}
	return result
}

func sensitivityAllowed(value, maximum string) bool {
	rank := map[string]int{entity.SensitivityInternal: 1, entity.SensitivitySensitive: 2, entity.SensitivityRestricted: 3}
	return rank[value] <= rank[maximum]
}

func environmentFingerprint(task *controlentity.TaskSession) string {
	parts := []string{task.DeviceID}
	for _, action := range task.Actions {
		parts = append(parts, action.Capability, action.Operation, action.Protocol)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func experienceSearchText(value entity.Experience) string {
	parts := []string{value.GoalSummary, value.PlanSummary, value.DecisionSummary, value.Outcome, value.EnvironmentFingerprint}
	if value.Failure != nil {
		parts = append(parts, value.Failure.Class, value.Failure.Summary)
	}
	for _, action := range value.ActionRefs {
		parts = append(parts, action.Capability, action.Operation)
	}
	for _, observation := range value.ObservationRefs {
		parts = append(parts, observation.Summary)
	}
	return strings.Join(parts, " ")
}

func structuredScore(request entity.SearchRequest, value entity.Experience) float64 {
	var matched, total float64
	if request.EnvironmentFingerprint != "" {
		total++
		if request.EnvironmentFingerprint == value.EnvironmentFingerprint {
			matched++
		}
	}
	if request.FailureClass != "" {
		total++
		if value.Failure != nil && request.FailureClass == value.Failure.Class {
			matched++
		}
	}
	if request.Outcome != "" {
		total++
		if request.Outcome == value.Outcome {
			matched++
		}
	}
	if request.Capability != "" {
		total++
		for _, action := range value.ActionRefs {
			if action.Capability == request.Capability {
				matched++
				break
			}
		}
	}
	if total == 0 {
		return 0
	}
	return matched / total
}

func enforceSearchBudget(hits []entity.SearchHit, budget entity.SearchBudget) []entity.SearchHit {
	result := make([]entity.SearchHit, 0, minInt(len(hits), budget.MaxResults))
	tokens := 0
	for _, hit := range hits {
		if len(result) >= budget.MaxResults {
			break
		}
		estimated := len([]rune(experienceSearchText(hit.Experience))) / 4
		if estimated < 1 {
			estimated = 1
		}
		if tokens+estimated > budget.MaxTokens {
			break
		}
		tokens += estimated
		result = append(result, hit)
	}
	return result
}

func metadataMap(metadata map[string]interface{}, key string) map[string]any {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	result := make(map[string]any)
	if json.Unmarshal(data, &result) != nil {
		return nil
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
