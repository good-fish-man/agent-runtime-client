package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	experiencesvc "github.com/good-fish-man/agent-runtime-client/application/service/experience"
	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	experiencerepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/experience"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/learning"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
	log "github.com/good-fish-man/logx"
)

const (
	minimumEvidenceCount = 4
	minimumSuccessCount  = 2
	defaultMinimumScore  = 0.75
	maximumEvidenceCount = 20
)

type ExperienceStore interface {
	GetPreference(context.Context, string) (*experienceentity.Preference, error)
	Create(context.Context, *experienceentity.StoredExperience) (bool, error)
	Find(context.Context, string, string) (*experienceentity.Experience, error)
	List(context.Context, string, experienceentity.ListFilter) ([]experienceentity.Experience, int64, error)
}

type OfflineEvaluator interface {
	CreateFixture(context.Context, string, experiencesvc.CreateFixtureRequest) (*experienceentity.EvaluationFixture, error)
	CreateSuite(context.Context, string, experiencesvc.CreateSuiteRequest) (*experienceentity.EvaluationSuite, error)
	RunCandidateSuite(context.Context, string, experiencesvc.RunSuiteRequest, experiencesvc.CandidateReplaySpec) (*experienceentity.EvaluationRun, []experienceentity.EvaluationResult, error)
}

type Service struct {
	store       repository.Store
	experiences ExperienceStore
	evaluator   OfflineEvaluator
	synthesizer CandidateSynthesizer
}

func NewService(store repository.Store, experiences experiencerepo.Store, evaluator *experiencesvc.Service) *Service {
	return &Service{store: store, experiences: experiences, evaluator: evaluator}
}

func NewServiceWithDependencies(store repository.Store, experiences ExperienceStore, evaluator OfflineEvaluator) *Service {
	return &Service{store: store, experiences: experiences, evaluator: evaluator}
}

type GenerateCandidateRequest struct {
	Kind           string             `json:"kind"`
	ID             string             `json:"id"`
	Version        string             `json:"version"`
	Description    string             `json:"description"`
	ExperienceIDs  []string           `json:"experience_ids,omitempty"`
	Visibility     string             `json:"visibility,omitempty"`
	MinimumScore   float64            `json:"minimum_score,omitempty"`
	PreferredSkill string             `json:"preferred_skill,omitempty"`
	FallbackOrder  []string           `json:"fallback_order,omitempty"`
	Condition      []entity.Predicate `json:"condition,omitempty"`
}

type ReviewRequest struct {
	Decision         string `json:"decision"`
	Note             string `json:"note,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type UpdateCandidateRequest struct {
	Skill            *entity.SkillDefinition    `json:"skill,omitempty"`
	Strategy         *entity.StrategyDefinition `json:"strategy,omitempty"`
	ExpectedRevision int64                      `json:"expected_revision"`
}

type ReevaluateCandidateRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

func (s *Service) GenerateCandidate(ctx context.Context, ownerID string, request GenerateCandidateRequest) (*entity.Candidate, error) {
	return s.generateCandidate(ctx, ownerID, request, generationOptions{})
}

type generationOptions struct {
	CandidateID    string
	Origin         string
	EvidenceDigest string
}

func (s *Service) generateCandidate(ctx context.Context, ownerID string, request GenerateCandidateRequest, options generationOptions) (*entity.Candidate, error) {
	if s == nil || s.store == nil || s.experiences == nil || s.evaluator == nil {
		return nil, fmt.Errorf("learning service is not configured")
	}
	span := log.StartSpan(ctx, "learning.candidate.generate", "owner_id", ownerID, "kind", request.Kind)
	var resultErr error
	defer func() { span.End(resultErr) }()

	experiences, err := s.selectEvidence(ctx, ownerID, request.ExperienceIDs)
	if err != nil {
		resultErr = log.WrapError(err, "LearningService.GenerateCandidate.selectEvidence")
		return nil, resultErr
	}
	pattern, selected, err := selectPatternEvidence(experiences)
	if err != nil {
		resultErr = apierror.ErrBadRequest.WithMessage(err.Error())
		return nil, resultErr
	}

	candidateID := strings.TrimSpace(options.CandidateID)
	if candidateID == "" {
		candidateID = ulid.New()
	}
	minimumScore := request.MinimumScore
	if minimumScore <= 0 {
		minimumScore = defaultMinimumScore
	}
	if minimumScore > 1 {
		resultErr = apierror.ErrBadRequest.WithMessage("minimum_score must be in (0,1]")
		return nil, resultErr
	}
	visibility := normalizeVisibility(request.Visibility)
	_, err = organizationScope(ctx, visibility)
	if err != nil {
		resultErr = err
		return nil, resultErr
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		version = "0.1.0"
	}
	id := normalizeID(request.ID)
	if id == "" {
		id = "learned." + normalizeID(strings.ReplaceAll(pattern, "|", "."))
	}
	description := strings.TrimSpace(request.Description)
	if description == "" {
		description = "Reviewed pattern learned from " + fmt.Sprint(len(selected)) + " independent experiences."
	}
	kind := normalizeKind(request.Kind)
	policy, err := s.validationPolicy(ctx, ownerID, selected)
	if err != nil {
		resultErr = log.WrapError(err, "LearningService.GenerateCandidate.policy")
		return nil, resultErr
	}
	var skill *entity.SkillDefinition
	var strategy *entity.StrategyDefinition
	switch kind {
	case entity.CandidateSkill:
		skill = buildSkill(id, version, description, ownerID, visibility, "preflight", minimumScore, selected, policy)
		if strings.TrimSpace(options.Origin) != "" {
			skill.Metadata["origin"] = strings.TrimSpace(options.Origin)
			skill.Metadata["evidence_pattern"] = pattern
			skill.Metadata["evidence_digest"] = strings.TrimSpace(options.EvidenceDigest)
		}
		if err := skill.Validate(policy); err != nil {
			resultErr = apierror.ErrBadRequest.WithMessage(err.Error())
			return nil, resultErr
		}
	case entity.CandidateStrategy:
		preferred := strings.TrimSpace(request.PreferredSkill)
		if preferred == "" {
			resultErr = apierror.ErrBadRequest.WithMessage("strategy candidate requires preferred_skill")
			return nil, resultErr
		}
		condition := request.Condition
		if len(condition) == 0 {
			condition = []entity.Predicate{{Field: "experience.pattern", Operator: "equals", Value: pattern}}
		}
		strategy = buildStrategy(id, version, description, ownerID, visibility, preferred, request.FallbackOrder, condition)
		if err := strategy.Validate(policy); err != nil {
			resultErr = apierror.ErrBadRequest.WithMessage(err.Error())
			return nil, resultErr
		}
	default:
		resultErr = apierror.ErrBadRequest.WithMessage("candidate kind must be SKILL or STRATEGY")
		return nil, resultErr
	}
	if s.synthesizer != nil {
		synthesis, synthErr := s.synthesizer.Synthesize(ctx, candidateSynthesisInput(
			ownerID, candidateID, kind, pattern, selected, policy, skill, strategy,
		))
		if synthErr != nil {
			resultErr = log.WrapError(synthErr, "LearningService.GenerateCandidate.synthesize")
			return nil, resultErr
		}
		if synthErr := applyCandidateSynthesis(skill, strategy, selected, synthesis); synthErr != nil {
			resultErr = apierror.ErrBadRequest.WithMessage("Codex candidate failed synthesis guardrails: " + synthErr.Error())
			return nil, resultErr
		}
		switch kind {
		case entity.CandidateSkill:
			if synthErr := skill.Validate(policy); synthErr != nil {
				resultErr = apierror.ErrBadRequest.WithMessage("Codex skill failed static validation: " + synthErr.Error())
				return nil, resultErr
			}
			pattern = skillActionPattern(*skill)
		case entity.CandidateStrategy:
			if synthErr := strategy.Validate(policy); synthErr != nil {
				resultErr = apierror.ErrBadRequest.WithMessage("Codex strategy failed static validation: " + synthErr.Error())
				return nil, resultErr
			}
		}
		log.Infow(ctx, "Codex evolution candidate synthesized",
			"candidate_id", candidateID, "kind", kind, "model", synthesis.Model,
			"response_id", synthesis.ResponseID, "input_digest", synthesis.InputDigest,
		)
	}
	replay, err := buildCandidateReplaySpec(candidateID, kind, pattern, skill, strategy)
	if err != nil {
		resultErr = log.WrapError(err, "LearningService.GenerateCandidate.replaySpec")
		return nil, resultErr
	}
	fixtureIDs, suiteID, run, runResults, err := s.evaluateEvidence(ctx, ownerID, candidateID, selected, request.ID, replay)
	if err != nil {
		resultErr = log.WrapError(err, "LearningService.GenerateCandidate.evaluate")
		return nil, resultErr
	}
	_ = fixtureIDs

	now := time.Now().UTC()
	trace := traceID(ctx)
	evidenceSummary, evidenceRows := summarizeEvidence(candidateID, ownerID, trace, pattern, selected, now)
	baselineRate := float64(evidenceSummary.SuccessCount) / float64(len(selected))
	evaluationSummary := summarizeEvaluation(run, runResults, baselineRate, minimumScore)
	candidate := entity.Candidate{
		Schema: entity.Schema, CandidateID: candidateID, OwnerID: ownerID,
		Kind: kind, Status: entity.LifecycleReviewRequired,
		Evidence: evidenceSummary, Evaluation: evaluationSummary, Revision: 1,
		TraceID: trace, CreatedAt: now, UpdatedAt: now,
	}
	switch candidate.Kind {
	case entity.CandidateSkill:
		skill.EvaluationSuite.SuiteID = suiteID
		candidate.Skill = skill
	case entity.CandidateStrategy:
		candidate.Strategy = strategy
	}
	if err := candidate.Validate(policy); err != nil {
		resultErr = apierror.ErrBadRequest.WithMessage(err.Error())
		return nil, resultErr
	}
	evaluation := entity.CandidateEvaluation{
		EvaluationID: ulid.New(), CandidateID: candidateID, OwnerID: ownerID, RunID: run.RunID,
		Summary: evaluationSummary, TraceID: trace, CreatedAt: now,
	}
	if err := s.store.CreateCandidate(ctx, candidate, evidenceRows, evaluation); err != nil {
		resultErr = log.WrapError(err, "LearningService.GenerateCandidate.persist")
		return nil, resultErr
	}
	return &candidate, nil
}

func (s *Service) FindCandidate(ctx context.Context, ownerID, candidateID string) (*entity.Candidate, []entity.CandidateEvidence, []entity.CandidateEvaluation, error) {
	candidate, err := s.store.FindCandidate(ctx, ownerID, candidateID)
	if err != nil || candidate == nil {
		return candidate, nil, nil, err
	}
	evidence, err := s.store.ListEvidence(ctx, ownerID, candidateID)
	if err != nil {
		return nil, nil, nil, err
	}
	evaluations, err := s.store.ListEvaluations(ctx, ownerID, candidateID)
	if err != nil {
		return nil, nil, nil, err
	}
	return candidate, evidence, evaluations, nil
}

func (s *Service) ListCandidates(ctx context.Context, ownerID string, filter entity.CandidateFilter) ([]entity.Candidate, int64, error) {
	return s.store.ListCandidates(ctx, ownerID, filter)
}

func (s *Service) UpdateCandidate(ctx context.Context, ownerID, candidateID string, request UpdateCandidateRequest) (*entity.Candidate, error) {
	candidate, err := s.store.FindCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, apierror.ErrNotFound.WithMessage("learning candidate not found")
	}
	if candidate.Status != entity.LifecycleReviewRequired && candidate.Status != entity.LifecycleEvaluating {
		return nil, apierror.ErrBadRequest.WithMessage("only candidates awaiting evaluation or review can be edited")
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = candidate.Revision
	}
	switch candidate.Kind {
	case entity.CandidateSkill:
		if request.Skill == nil || request.Strategy != nil {
			return nil, apierror.ErrBadRequest.WithMessage("skill candidate edit requires only skill")
		}
		candidate.Skill = request.Skill
		candidate.Strategy = nil
		candidate.Skill.OwnerID = ownerID
		_, scopeErr := organizationScope(ctx, candidate.Skill.Visibility)
		if scopeErr != nil {
			return nil, scopeErr
		}
		candidate.Skill.LifecycleState = entity.LifecycleEvaluating
	case entity.CandidateStrategy:
		if request.Strategy == nil || request.Skill != nil {
			return nil, apierror.ErrBadRequest.WithMessage("strategy candidate edit requires only strategy")
		}
		candidate.Strategy = request.Strategy
		candidate.Skill = nil
		candidate.Strategy.OwnerID = ownerID
		_, scopeErr := organizationScope(ctx, candidate.Strategy.Visibility)
		if scopeErr != nil {
			return nil, scopeErr
		}
		candidate.Strategy.LifecycleState = entity.LifecycleEvaluating
	default:
		return nil, apierror.ErrBadRequest.WithMessage("unsupported candidate kind")
	}
	// A human edit changes the artifact. Clear the old replay result before
	// validation so a changed threshold cannot reuse or block on stale metrics.
	candidate.Status = entity.LifecycleEvaluating
	candidate.Evaluation = entity.EvaluationSummary{}
	candidate.ReviewNote = ""
	candidate.ReviewedBy = ""
	candidate.ReviewedAt = time.Time{}
	policy, err := s.candidatePolicy(ctx, ownerID, *candidate)
	if err != nil {
		return nil, err
	}
	if err := candidate.Validate(policy); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("edited candidate failed static validation: " + err.Error())
	}
	candidate.Revision = request.ExpectedRevision + 1
	candidate.TraceID = traceID(ctx)
	candidate.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateCandidate(ctx, *candidate, request.ExpectedRevision); err != nil {
		return nil, err
	}
	return candidate, nil
}

func (s *Service) ReevaluateCandidate(ctx context.Context, ownerID, candidateID string, request ReevaluateCandidateRequest) (*entity.Candidate, error) {
	candidate, evidenceRows, _, err := s.FindCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, apierror.ErrNotFound.WithMessage("learning candidate not found")
	}
	if candidate.Status != entity.LifecycleReviewRequired && candidate.Status != entity.LifecycleEvaluating {
		return nil, apierror.ErrBadRequest.WithMessage("only candidates awaiting evaluation or review can be re-evaluated")
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = candidate.Revision
	}
	policy, err := s.candidatePolicy(ctx, ownerID, *candidate)
	if err != nil {
		return nil, err
	}
	if err := candidate.Validate(policy); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("candidate failed static validation: " + err.Error())
	}
	evidence := make([]experienceentity.Experience, 0, len(evidenceRows))
	for _, row := range evidenceRows {
		item, findErr := s.experiences.Find(ctx, ownerID, row.ExperienceID)
		if findErr != nil {
			return nil, log.WrapError(findErr, "LearningService.ReevaluateCandidate.findEvidence")
		}
		if item == nil {
			return nil, apierror.ErrBadRequest.WithMessage("candidate evidence is no longer available: " + row.ExperienceID)
		}
		evidence = append(evidence, *item)
	}
	minimumScore := defaultMinimumScore
	name := candidateID
	if candidate.Skill != nil {
		minimumScore = candidate.Skill.EvaluationSuite.MinimumScore
		name = candidate.Skill.ID
	} else if candidate.Strategy != nil {
		name = candidate.Strategy.ID
	}
	replay, err := buildCandidateReplaySpec(candidateID, candidate.Kind, candidate.Evidence.Pattern, candidate.Skill, candidate.Strategy)
	if err != nil {
		return nil, log.WrapError(err, "LearningService.ReevaluateCandidate.replaySpec")
	}
	_, suiteID, run, results, err := s.evaluateEvidence(ctx, ownerID, candidateID, evidence, name, replay)
	if err != nil {
		return nil, log.WrapError(err, "LearningService.ReevaluateCandidate.evaluate")
	}
	if candidate.Skill != nil {
		candidate.Skill.EvaluationSuite.SuiteID = suiteID
	}
	baseline := 0.0
	if len(evidence) > 0 {
		baseline = float64(candidate.Evidence.SuccessCount) / float64(len(evidence))
	}
	candidate.Evaluation = summarizeEvaluation(run, results, baseline, minimumScore)
	candidate.Status = entity.LifecycleReviewRequired
	if candidate.Skill != nil {
		candidate.Skill.LifecycleState = entity.LifecycleReviewRequired
	}
	if candidate.Strategy != nil {
		candidate.Strategy.LifecycleState = entity.LifecycleReviewRequired
	}
	candidate.ReviewNote = ""
	candidate.Revision = request.ExpectedRevision + 1
	candidate.TraceID = traceID(ctx)
	candidate.UpdatedAt = time.Now().UTC()
	if err := candidate.Validate(policy); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("re-evaluated candidate failed the evaluation gate: " + err.Error())
	}
	evaluation := entity.CandidateEvaluation{
		EvaluationID: ulid.New(), CandidateID: candidateID, OwnerID: ownerID,
		RunID: run.RunID, Summary: candidate.Evaluation, TraceID: candidate.TraceID,
		CreatedAt: candidate.UpdatedAt,
	}
	if err := s.store.SaveCandidateEvaluation(ctx, *candidate, evaluation, request.ExpectedRevision); err != nil {
		return nil, err
	}
	return candidate, nil
}

func (s *Service) ReviewCandidate(ctx context.Context, ownerID, reviewerID, candidateID string, request ReviewRequest) (*entity.Candidate, error) {
	candidate, err := s.store.FindCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		return nil, apierror.ErrNotFound.WithMessage("learning candidate not found")
	}
	decision := strings.ToUpper(strings.TrimSpace(request.Decision))
	note := strings.TrimSpace(request.Note)
	if decision != "APPROVE" && decision != "REJECT" {
		return nil, apierror.ErrBadRequest.WithMessage("review decision must be APPROVE or REJECT")
	}
	if strings.TrimSpace(reviewerID) == "" || note == "" {
		return nil, apierror.ErrBadRequest.WithMessage("reviewer identity and a review note are required")
	}
	if candidate.Status != entity.LifecycleReviewRequired {
		return nil, apierror.ErrBadRequest.WithMessage("candidate is not awaiting review")
	}
	organizationID, err := organizationScopeForCandidate(ctx, *candidate)
	if err != nil {
		return nil, err
	}
	policy, err := s.candidatePolicy(ctx, ownerID, *candidate)
	if err != nil {
		return nil, err
	}
	if err := candidate.Validate(policy); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("candidate no longer passes static validation: " + err.Error())
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = candidate.Revision
	}
	reviewed := *candidate
	reviewed.Status = entity.LifecycleRejected
	if decision == "APPROVE" {
		reviewed.Status = entity.LifecycleApproved
	}
	if reviewed.Skill != nil {
		definition := *reviewed.Skill
		definition.LifecycleState = reviewed.Status
		reviewed.Skill = &definition
	}
	if reviewed.Strategy != nil {
		definition := *reviewed.Strategy
		definition.LifecycleState = reviewed.Status
		reviewed.Strategy = &definition
	}
	reviewed.ReviewNote = note
	reviewed.ReviewedBy = reviewerID
	reviewed.ReviewedAt = time.Now().UTC()
	reviewed.Revision = request.ExpectedRevision + 1
	reviewed.UpdatedAt = reviewed.ReviewedAt
	if err := reviewed.Validate(policy); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("review decision failed the candidate gate: " + err.Error())
	}
	return s.store.ReviewCandidate(ctx, ownerID, candidateID, decision, note, reviewerID, organizationID, request.ExpectedRevision)
}

func (s *Service) ListSkills(ctx context.Context, ownerID string, limit int) ([]entity.Skill, error) {
	return s.store.ListSkills(ctx, ownerID, authctx.OrganizationID(ctx), limit)
}

func (s *Service) ListStrategies(ctx context.Context, ownerID string, limit int) ([]entity.Strategy, error) {
	return s.store.ListStrategies(ctx, ownerID, authctx.OrganizationID(ctx), limit)
}

func (s *Service) selectEvidence(ctx context.Context, ownerID string, requested []string) ([]experienceentity.Experience, error) {
	items, _, err := s.experiences.List(ctx, ownerID, experienceentity.ListFilter{Status: experienceentity.StatusReady, Limit: 200})
	if err != nil {
		return nil, err
	}
	requestedSet := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		if id = strings.TrimSpace(id); id != "" {
			requestedSet[id] = struct{}{}
		}
	}
	selected := make([]experienceentity.Experience, 0, len(items))
	for _, item := range items {
		if len(requestedSet) > 0 {
			if _, ok := requestedSet[item.ExperienceID]; !ok {
				continue
			}
		}
		selected = append(selected, item)
	}
	if len(requestedSet) > 0 && len(selected) != len(requestedSet) {
		return nil, fmt.Errorf("one or more experiences are missing or do not belong to the current user")
	}
	return selected, nil
}

func selectPatternEvidence(items []experienceentity.Experience) (string, []experienceentity.Experience, error) {
	type group struct {
		pattern   string
		successes []experienceentity.Experience
		failures  []experienceentity.Experience
	}
	groups := make(map[string]*group)
	for _, item := range items {
		if len(item.ActionRefs) == 0 {
			continue
		}
		pattern := actionPattern(item.ActionRefs)
		current := groups[pattern]
		if current == nil {
			current = &group{pattern: pattern}
			groups[pattern] = current
		}
		switch item.Outcome {
		case experienceentity.OutcomeSucceeded:
			current.successes = append(current.successes, item)
		case experienceentity.OutcomeFailed:
			current.failures = append(current.failures, item)
		}
	}
	ordered := make([]*group, 0, len(groups))
	for _, values := range groups {
		ordered = append(ordered, values)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i].successes) == len(ordered[j].successes) {
			return ordered[i].pattern < ordered[j].pattern
		}
		return len(ordered[i].successes) > len(ordered[j].successes)
	})
	if len(ordered) == 0 || len(ordered[0].successes) < minimumSuccessCount {
		return "", nil, fmt.Errorf("candidate needs at least two successful experiences with the same semantic action pattern")
	}

	hasMatchingFailure := false
	hasMinimumEvidence := false
	var evidenceErr error
	for _, candidate := range ordered {
		if len(candidate.successes) < minimumSuccessCount {
			break
		}
		if len(candidate.failures) == 0 {
			continue
		}
		hasMatchingFailure = true
		if len(candidate.successes)+len(candidate.failures) < minimumEvidenceCount {
			continue
		}
		hasMinimumEvidence = true
		selected := boundedPatternEvidence(candidate.successes, candidate.failures)
		if err := validateIndependentEvidence(selected); err != nil {
			if evidenceErr == nil {
				evidenceErr = err
			}
			continue
		}
		return candidate.pattern, selected, nil
	}
	if !hasMatchingFailure {
		return "", nil, fmt.Errorf("candidate needs at least one failed counterexample with the same semantic action pattern")
	}
	if !hasMinimumEvidence {
		return "", nil, fmt.Errorf("candidate needs at least four independent experiences")
	}
	return "", nil, evidenceErr
}

func boundedPatternEvidence(successes, failures []experienceentity.Experience) []experienceentity.Experience {
	successLimit := len(successes)
	if successLimit >= maximumEvidenceCount {
		successLimit = maximumEvidenceCount - 1
	}
	selected := append([]experienceentity.Experience{}, successes[:successLimit]...)
	failureLimit := maximumEvidenceCount - len(selected)
	if failureLimit > len(failures) {
		failureLimit = len(failures)
	}
	return append(selected, failures[:failureLimit]...)
}

func (s *Service) evaluateEvidence(ctx context.Context, ownerID, candidateID string, evidence []experienceentity.Experience, name string, replay experiencesvc.CandidateReplaySpec) ([]string, string, *experienceentity.EvaluationRun, []experienceentity.EvaluationResult, error) {
	fixtureIDs := make([]string, 0, len(evidence))
	for _, item := range evidence {
		environmentVersion := strings.TrimSpace(item.EnvironmentFingerprint)
		if environmentVersion == "" {
			return nil, "", nil, nil, fmt.Errorf("experience %s has no environment fingerprint", item.ExperienceID)
		}
		fixture, err := s.evaluator.CreateFixture(ctx, ownerID, experiencesvc.CreateFixtureRequest{
			ExperienceID: item.ExperienceID, Name: "candidate " + candidateID + " evidence", EnvironmentVersion: environmentVersion,
		})
		if err != nil {
			return nil, "", nil, nil, err
		}
		fixtureIDs = append(fixtureIDs, fixture.FixtureID)
	}
	suite, err := s.evaluator.CreateSuite(ctx, ownerID, experiencesvc.CreateSuiteRequest{
		Name: "Candidate " + firstNonEmpty(strings.TrimSpace(name), candidateID), FixtureIDs: fixtureIDs,
	})
	if err != nil {
		return nil, "", nil, nil, err
	}
	run, results, err := s.evaluator.RunCandidateSuite(ctx, ownerID, experiencesvc.RunSuiteRequest{
		SuiteID: suite.SuiteID, Seed: 1, CandidateID: candidateID, BaselineID: "historical-experience",
	}, replay)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return fixtureIDs, suite.SuiteID, run, results, nil
}

func buildCandidateReplaySpec(candidateID, kind, pattern string, skill *entity.SkillDefinition, strategy *entity.StrategyDefinition) (experiencesvc.CandidateReplaySpec, error) {
	var artifact any
	recovery := make([]string, 0)
	verificationRequired := false
	switch kind {
	case entity.CandidateSkill:
		if skill == nil || strategy != nil {
			return experiencesvc.CandidateReplaySpec{}, fmt.Errorf("skill candidate replay requires only a skill definition")
		}
		artifact = skill
		for _, path := range skill.RecoveryPaths {
			recovery = append(recovery, path.On)
		}
		for _, rule := range skill.VerificationRules {
			verificationRequired = verificationRequired || rule.EvidenceRequired
		}
	case entity.CandidateStrategy:
		if strategy == nil || skill != nil {
			return experiencesvc.CandidateReplaySpec{}, fmt.Errorf("strategy candidate replay requires only a strategy definition")
		}
		artifact = strategy
		if len(strategy.FallbackOrder) > 0 || strategy.RetryBudget.MaxAttempts > 1 {
			recovery = append(recovery, "*")
		}
		verificationRequired = strategy.ObservationPolicy.RequireEvidence
		for _, rule := range strategy.VerificationPolicy {
			verificationRequired = verificationRequired && rule.EvidenceRequired
		}
	default:
		return experiencesvc.CandidateReplaySpec{}, fmt.Errorf("unsupported candidate replay kind %q", kind)
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		return experiencesvc.CandidateReplaySpec{}, err
	}
	digest := sha256.Sum256(encoded)
	return experiencesvc.CandidateReplaySpec{
		ArtifactID: candidateID, ArtifactChecksum: hex.EncodeToString(digest[:]), Kind: kind,
		ActionPattern: strings.TrimSpace(pattern), RecoveryConditions: uniqueStrings(recovery),
		VerificationEvidenceRequired: verificationRequired,
	}, nil
}

func (s *Service) validationPolicy(ctx context.Context, ownerID string, evidence []experienceentity.Experience) (entity.ValidationPolicy, error) {
	capabilities := make([]string, 0)
	for _, action := range evidence[0].ActionRefs {
		capabilities = append(capabilities, action.Capability)
	}
	policies, err := s.store.CapabilityPolicies(ctx, capabilities)
	if err != nil {
		return entity.ValidationPolicy{}, err
	}
	approved, err := s.store.ApprovedSkills(ctx, ownerID, authctx.OrganizationID(ctx))
	if err != nil {
		return entity.ValidationPolicy{}, err
	}
	return entity.ValidationPolicy{Capabilities: policies, ApprovedSkills: approved}, nil
}

func (s *Service) candidatePolicy(ctx context.Context, ownerID string, candidate entity.Candidate) (entity.ValidationPolicy, error) {
	capabilities := make([]string, 0)
	if candidate.Skill != nil {
		capabilities = candidate.Skill.RequiredCapabilities
	}
	policies, err := s.store.CapabilityPolicies(ctx, capabilities)
	if err != nil {
		return entity.ValidationPolicy{}, err
	}
	approved, err := s.store.ApprovedSkills(ctx, ownerID, authctx.OrganizationID(ctx))
	if err != nil {
		return entity.ValidationPolicy{}, err
	}
	return entity.ValidationPolicy{Capabilities: policies, ApprovedSkills: approved}, nil
}

func buildSkill(id, version, description, ownerID, visibility, suiteID string, minimumScore float64, evidence []experienceentity.Experience, policy entity.ValidationPolicy) *entity.SkillDefinition {
	steps := make([]entity.TaskStep, 0, len(evidence[0].ActionRefs))
	capabilities := make([]string, 0, len(evidence[0].ActionRefs))
	seen := make(map[string]struct{})
	previous := ""
	for index, action := range evidence[0].ActionRefs {
		stepID := fmt.Sprintf("step-%d", index+1)
		dependencies := []string(nil)
		if previous != "" {
			dependencies = []string{previous}
		}
		operation := strings.TrimSpace(action.Operation)
		if operation == "" {
			operation = "invoke"
		}
		steps = append(steps, entity.TaskStep{ID: stepID, Capability: action.Capability, Operation: operation, DependsOn: dependencies})
		previous = stepID
		if _, ok := seen[action.Capability]; !ok {
			seen[action.Capability] = struct{}{}
			capabilities = append(capabilities, action.Capability)
		}
	}
	stepIDs := make([]string, 0, len(steps))
	for _, step := range steps {
		stepIDs = append(stepIDs, step.ID)
	}
	failureClasses := make([]string, 0)
	for _, item := range evidence {
		if item.Outcome != experienceentity.OutcomeFailed {
			continue
		}
		failureClass := "OUTCOME_FAILED"
		if item.Failure != nil && strings.TrimSpace(item.Failure.Class) != "" {
			failureClass = item.Failure.Class
		}
		failureClasses = append(failureClasses, failureClass)
	}
	recoveryPaths := make([]entity.RecoveryPath, 0, len(failureClasses))
	for _, failureClass := range uniqueStrings(failureClasses) {
		recoveryPaths = append(recoveryPaths, entity.RecoveryPath{On: failureClass, StepIDs: append([]string(nil), stepIDs...), MaxAttempts: 1})
	}
	siteScopes := make([]string, 0, len(evidence))
	for _, item := range evidence {
		siteScopes = append(siteScopes, evidenceSiteScope(item))
	}
	siteScopes = uniqueStrings(siteScopes)
	return &entity.SkillDefinition{
		ID: id, Version: version, Description: description,
		InputSchema:  learningv2.JSONSchema{"type": "object", "additionalProperties": true},
		OutputSchema: learningv2.JSONSchema{"type": "object", "additionalProperties": true},
		Preconditions: []entity.Predicate{
			{Field: "context.environment_fingerprint", Operator: "exists"},
			{Field: "context.capabilities", Operator: "contains_all", Value: capabilities},
		},
		RequiredCapabilities: capabilities, TaskGraphTemplate: entity.TaskGraphTemplate{Steps: steps},
		RecoveryPaths:     recoveryPaths,
		VerificationRules: []entity.VerificationRule{{Field: "task.status", Operator: "equals", Expected: "COMPLETED", EvidenceRequired: true}},
		RiskCeiling:       composedPolicyRisk(capabilities, policy),
		EvaluationSuite:   entity.EvaluationSuiteRef{SuiteID: suiteID, MinimumSample: minimumEvidenceCount, MinimumScore: minimumScore},
		OwnerID:           ownerID, Visibility: visibility, LifecycleState: entity.LifecycleReviewRequired,
		Metadata: map[string]string{
			"generalization_scope": "cross-context",
			"site_scopes":          strings.Join(siteScopes, ","),
			"source_contexts":      fmt.Sprint(len(evidence)),
		},
	}
}

func buildStrategy(id, version, description, ownerID, visibility, preferred string, fallbacks []string, condition []entity.Predicate) *entity.StrategyDefinition {
	return &entity.StrategyDefinition{
		ID: id, Version: version, Description: description, Condition: condition,
		PreferredSkill: preferred, FallbackOrder: uniqueStrings(fallbacks),
		ObservationPolicy:  learningv2.ObservationPolicy{RequiredFields: []string{"task.status"}, RequireEvidence: true},
		RetryBudget:        learningv2.RetryBudget{MaxAttempts: 1, MaxDurationMS: 30000},
		VerificationPolicy: []entity.VerificationRule{{Field: "task.status", Operator: "equals", Expected: "COMPLETED", EvidenceRequired: true}},
		RiskCeiling:        entity.RiskMedium, OwnerID: ownerID, Visibility: visibility, LifecycleState: entity.LifecycleReviewRequired,
	}
}

func summarizeEvidence(candidateID, ownerID, trace, pattern string, values []experienceentity.Experience, now time.Time) (entity.EvidenceSummary, []entity.CandidateEvidence) {
	summary := entity.EvidenceSummary{Pattern: pattern, ExperienceIDs: make([]string, 0, len(values)), Contexts: make([]entity.EvidenceContext, 0, len(values))}
	rows := make([]entity.CandidateEvidence, 0, len(values))
	for _, value := range values {
		relation := "SUPPORTING_SUCCESS"
		if value.Outcome == experienceentity.OutcomeSucceeded {
			summary.SuccessCount++
		} else {
			relation = "FAILURE_COUNTEREXAMPLE"
			summary.FailureCount++
			summary.Counterexamples++
		}
		summary.ExperienceIDs = append(summary.ExperienceIDs, value.ExperienceID)
		failureCondition := ""
		if value.Failure != nil {
			failureCondition = value.Failure.Class
		}
		summary.Contexts = append(summary.Contexts, entity.EvidenceContext{
			ExperienceID: value.ExperienceID, EnvironmentFingerprint: value.EnvironmentFingerprint,
			SiteScope: evidenceSiteScope(value), Outcome: value.Outcome, FailureCondition: failureCondition,
		})
		rows = append(rows, entity.CandidateEvidence{
			EvidenceID: ulid.New(), CandidateID: candidateID, OwnerID: ownerID,
			ExperienceID: value.ExperienceID, Relation: relation, Outcome: value.Outcome,
			Summary: compact(value.GoalSummary, 240), TraceID: trace, CreatedAt: now,
		})
	}
	return summary, rows
}

func validateIndependentEvidence(values []experienceentity.Experience) error {
	experienceIDs := make(map[string]struct{}, len(values))
	taskIDs := make(map[string]struct{}, len(values))
	contexts := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.ExperienceID) == "" || strings.TrimSpace(value.TaskID) == "" {
			return fmt.Errorf("candidate evidence requires experience_id and task_id")
		}
		if _, duplicate := experienceIDs[value.ExperienceID]; duplicate {
			return fmt.Errorf("candidate evidence repeats experience %s", value.ExperienceID)
		}
		if _, duplicate := taskIDs[value.TaskID]; duplicate {
			return fmt.Errorf("candidate evidence must come from independent tasks; task %s is repeated", value.TaskID)
		}
		experienceIDs[value.ExperienceID] = struct{}{}
		taskIDs[value.TaskID] = struct{}{}
		environment := strings.TrimSpace(value.EnvironmentFingerprint)
		site := evidenceSiteScope(value)
		if environment == "" || site == "unknown" {
			return fmt.Errorf("experience %s must include an environment fingerprint and explicit site scope", value.ExperienceID)
		}
		contexts[environment+"\x00"+site] = struct{}{}
	}
	if len(contexts) < 2 {
		return fmt.Errorf("candidate evidence must span at least two independent environment/site contexts")
	}
	return nil
}

func evidenceSiteScope(value experienceentity.Experience) string {
	if site := findSiteScope(value.Intent); site != "" {
		return site
	}
	for _, action := range value.ActionRefs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(action.Capability)), "browser.") {
			return "unknown"
		}
	}
	return "not-applicable"
}

func findSiteScope(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"site_scope", "site", "domain", "hostname", "host", "origin", "url"} {
			if child, ok := typed[key]; ok {
				if site := normalizeSiteScope(fmt.Sprint(child)); site != "" {
					return site
				}
			}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if site := findSiteScope(typed[key]); site != "" {
				return site
			}
		}
	case []any:
		for _, child := range typed {
			if site := findSiteScope(child); site != "" {
				return site
			}
		}
	}
	return ""
}

func normalizeSiteScope(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "<nil>" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		return strings.TrimPrefix(parsed.Hostname(), "www.")
	}
	value = strings.TrimPrefix(value, "www.")
	if strings.ContainsAny(value, " /?#") {
		return ""
	}
	return value
}

func summarizeEvaluation(run *experienceentity.EvaluationRun, results []experienceentity.EvaluationResult, baseline, minimum float64) entity.EvaluationSummary {
	passed := 0
	for _, result := range results {
		if result.Passed {
			passed++
		}
	}
	rate := 0.0
	if len(results) > 0 {
		rate = float64(passed) / float64(len(results))
	}
	interval := wilsonInterval(passed, len(results), 1.96)
	return entity.EvaluationSummary{
		RunID: run.RunID, SampleSize: len(results), SuccessRate: rate, BaselineRate: baseline,
		Delta: rate - baseline, SafetyScore: run.Metrics.SafetyScore, Confidence: interval,
		Passed: len(results) >= minimumEvidenceCount && rate >= minimum && run.Metrics.SafetyScore >= 1,
	}
}

func wilsonInterval(successes, samples int, z float64) entity.ConfidenceInterval {
	if samples == 0 {
		return entity.ConfidenceInterval{Level: 0.95}
	}
	n := float64(samples)
	p := float64(successes) / n
	z2 := z * z
	center := (p + z2/(2*n)) / (1 + z2/n)
	margin := z * math.Sqrt((p*(1-p)+z2/(4*n))/n) / (1 + z2/n)
	return entity.ConfidenceInterval{Lower: math.Max(0, center-margin), Upper: math.Min(1, center+margin), Level: 0.95}
}

func actionPattern(actions []experienceentity.ActionRef) string {
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		operation := strings.TrimSpace(action.Operation)
		if operation == "" {
			operation = "invoke"
		}
		parts = append(parts, action.Capability+"."+operation)
	}
	return strings.Join(parts, "|")
}

func composedPolicyRisk(capabilities []string, policy entity.ValidationPolicy) string {
	risk := entity.RiskLow
	for _, capability := range capabilities {
		next := policy.Capabilities[capability].Risk
		if learningv2.RiskRank(next) > learningv2.RiskRank(risk) {
			risk = next
		}
	}
	return risk
}

func normalizeRisk(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "R1", "MEDIUM", "REVERSIBLE":
		return entity.RiskMedium
	case "R2", "HIGH", "EXTERNAL_WRITE":
		return entity.RiskHigh
	case "R3", "CRITICAL", "SENSITIVE":
		return entity.RiskCritical
	default:
		return entity.RiskLow
	}
}

func normalizeKind(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return entity.CandidateSkill
	}
	return value
}

func normalizeVisibility(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return entity.VisibilityPrivate
	}
	return value
}

func organizationScope(ctx context.Context, visibility string) (string, error) {
	if normalizeVisibility(visibility) != entity.VisibilityTeam {
		return "", nil
	}
	organizationID := strings.TrimSpace(authctx.OrganizationID(ctx))
	if organizationID == "" {
		return "", apierror.ErrBadRequest.WithMessage("TEAM visibility requires the current user to belong to an organization")
	}
	return organizationID, nil
}

func organizationScopeForCandidate(ctx context.Context, candidate entity.Candidate) (string, error) {
	visibility := ""
	if candidate.Skill != nil {
		visibility = candidate.Skill.Visibility
	} else if candidate.Strategy != nil {
		visibility = candidate.Strategy.Visibility
	}
	return organizationScope(ctx, visibility)
}

func normalizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastSeparator := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastSeparator = false
			continue
		}
		if !lastSeparator && builder.Len() > 0 {
			builder.WriteByte('.')
			lastSeparator = true
		}
	}
	result := strings.Trim(builder.String(), ".")
	if result != "" && result[0] >= '0' && result[0] <= '9' {
		result = "learned." + result
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
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

func compact(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "candidate"
}

func traceID(ctx context.Context) string {
	return log.ReqID(ctx)
}
