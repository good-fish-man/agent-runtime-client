package delegation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

var (
	ErrDelegationLearningDisabled = errors.New("delegation learning is disabled for this owner")
	ErrGovernanceGateIncomplete   = errors.New("delegation learning governance gate is incomplete")
)

type LearningReplayRunner interface {
	Run(context.Context, dso.ReplayRequest) (dso.ReplayResult, error)
}

type PlanOnlyShadowEvaluator interface {
	Evaluate(context.Context, dso.DelegationLearningCandidate, dso.DelegationEvaluationResult) (ShadowEvaluationOutcome, error)
}

type ShadowEvaluationOutcome struct {
	Baseline         dso.DelegationBenchmarkMetrics
	Candidate        dso.DelegationBenchmarkMetrics
	ReplayResultRefs []string
	Proof            dso.LearningShadowProof
	EvaluatorVersion string
}

type NoExternalEffectShadowEvaluator struct{}

func (NoExternalEffectShadowEvaluator) Evaluate(_ context.Context, candidate dso.DelegationLearningCandidate, offline dso.DelegationEvaluationResult) (ShadowEvaluationOutcome, error) {
	proofHash, err := dso.Hash(map[string]string{"candidate_hash": candidate.DefinitionHash, "offline_hash": offline.ContentHash, "mode": "PLAN_ONLY"})
	if err != nil {
		return ShadowEvaluationOutcome{}, err
	}
	return ShadowEvaluationOutcome{
		Baseline: offline.Baseline, Candidate: offline.Candidate,
		ReplayResultRefs: append([]string(nil), offline.ReplayResultRefs...),
		Proof:            dso.LearningShadowProof{Mode: "PLAN_ONLY", ProofDigest: proofHash},
		EvaluatorVersion: "dso-shadow-plan-only/v1",
	}, nil
}

type GovernedLearningService struct {
	store   repository.LearningStore
	replay  LearningReplayRunner
	shadow  PlanOnlyShadowEvaluator
	consent OwnerLearningConsent
	now     func() time.Time
}

type OwnerLearningConsent interface {
	GetPreference(context.Context, string) (*experienceentity.Preference, error)
}

func NewGovernedLearningService(store repository.LearningStore, replay LearningReplayRunner, shadow PlanOnlyShadowEvaluator) *GovernedLearningService {
	if shadow == nil {
		shadow = NoExternalEffectShadowEvaluator{}
	}
	return &GovernedLearningService{store: store, replay: replay, shadow: shadow, now: func() time.Time { return time.Now().UTC() }}
}

func (s *GovernedLearningService) WithOwnerLearningConsent(consent OwnerLearningConsent) *GovernedLearningService {
	if s != nil {
		s.consent = consent
	}
	return s
}

type LearningCandidateInput struct {
	CandidateID          string
	OwnerID              string
	Kind                 string
	SourceExperienceRefs []string
	SourceRunRefs        []string
	PolicyArtifact       *dso.DelegationPolicyArtifact
	ProfileArtifact      *dso.SpecialistProfileArtifact
	CreatedAt            time.Time
}

type OfflineEvaluationRequest struct {
	OwnerID     string
	CandidateID string
	Replays     []dso.ReplayRequest
	Baseline    dso.DelegationBenchmarkMetrics
	Candidate   dso.DelegationBenchmarkMetrics
}

type CanaryRequest struct {
	OwnerID         string
	CandidateID     string
	AllowedOwnerIDs []string
	Percent         int
	ApprovedBy      string
}

type ResolvedLearningArtifact struct {
	Source            string                           `json:"source"`
	FallbackPolicyRef string                           `json:"fallback_policy_ref"`
	Candidate         *dso.DelegationLearningCandidate `json:"candidate,omitempty"`
	Rollout           *dso.DelegationLearningRollout   `json:"rollout,omitempty"`
}

func (s *GovernedLearningService) SetPreference(ctx context.Context, ownerID, updatedBy string, enabled bool, expectedRevision int64) (dso.DelegationLearningPreference, error) {
	if s == nil || s.store == nil {
		return dso.DelegationLearningPreference{}, fmt.Errorf("delegation learning service is not configured")
	}
	now := s.now().UTC()
	preference := dso.DelegationLearningPreference{Schema: dso.Schema, OwnerID: strings.TrimSpace(ownerID), Enabled: enabled, Revision: expectedRevision + 1, UpdatedBy: strings.TrimSpace(updatedBy), UpdatedAt: now}
	if err := preference.Validate(); err != nil {
		return dso.DelegationLearningPreference{}, err
	}
	value := entity.LearningPreference{OwnerID: preference.OwnerID, Enabled: preference.Enabled, Revision: preference.Revision, UpdatedBy: preference.UpdatedBy, UpdatedAt: preference.UpdatedAt}
	if err := s.store.SetLearningPreference(ctx, value, expectedRevision, learningEvent(ctx, ownerID, ownerID, "DelegationLearningPreferenceChanged", preference.Revision, map[string]any{"enabled": enabled}, now)); err != nil {
		return dso.DelegationLearningPreference{}, log.WrapError(err, "GovernedLearning.SetPreference")
	}
	return preference, nil
}

func (s *GovernedLearningService) ProposeCandidate(ctx context.Context, input LearningCandidateInput) (dso.DelegationLearningCandidate, error) {
	if s == nil || s.store == nil {
		return dso.DelegationLearningCandidate{}, fmt.Errorf("delegation learning service is not configured")
	}
	if err := s.requireLearningEnabled(ctx, input.OwnerID); err != nil {
		return dso.DelegationLearningCandidate{}, err
	}
	now := input.CreatedAt.UTC()
	if now.IsZero() {
		now = s.now().UTC()
	}
	candidateID := strings.TrimSpace(input.CandidateID)
	if candidateID == "" {
		candidateID = "dso-candidate-" + ulid.New()
	}
	candidate := dso.DelegationLearningCandidate{
		Schema: dso.Schema, CandidateID: candidateID, OwnerID: strings.TrimSpace(input.OwnerID), Kind: input.Kind,
		SourceExperienceRefs: canonicalLearningRefs(input.SourceExperienceRefs), SourceRunRefs: canonicalLearningRefs(input.SourceRunRefs),
		PolicyArtifact: input.PolicyArtifact, ProfileArtifact: input.ProfileArtifact, ActivationAllowed: false, CreatedAt: now,
	}
	candidate.DefinitionHash, _ = dso.DelegationLearningCandidateDefinitionHash(candidate)
	if err := candidate.Validate(); err != nil {
		return dso.DelegationLearningCandidate{}, log.WrapError(err, "GovernedLearning.ProposeCandidate.validate")
	}
	content, err := json.Marshal(candidate)
	if err != nil {
		return dso.DelegationLearningCandidate{}, err
	}
	record := entity.LearningCandidate{CandidateID: candidate.CandidateID, OwnerID: candidate.OwnerID, Kind: candidate.Kind, DefinitionHash: candidate.DefinitionHash, Content: string(content), CreatedAt: candidate.CreatedAt}
	if err := s.store.CreateLearningCandidate(ctx, record, learningEvent(ctx, candidate.OwnerID, candidate.CandidateID, "DelegationLearningCandidateCreated", 1, map[string]any{"kind": candidate.Kind, "definition_hash": candidate.DefinitionHash, "source_experience_refs": candidate.SourceExperienceRefs}, now)); err != nil {
		return dso.DelegationLearningCandidate{}, log.WrapError(err, "GovernedLearning.ProposeCandidate.persist")
	}
	return candidate, nil
}

func (s *GovernedLearningService) EvaluateOffline(ctx context.Context, input OfflineEvaluationRequest) (dso.DelegationEvaluationResult, error) {
	if s == nil || s.store == nil || s.replay == nil {
		return dso.DelegationEvaluationResult{}, fmt.Errorf("offline delegation evaluator requires learning store and replay runner")
	}
	if err := s.requireLearningEnabled(ctx, input.OwnerID); err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	candidate, err := s.loadCandidate(ctx, input.OwnerID, input.CandidateID)
	if err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	if len(input.Replays) < dso.MinimumLearningExperiences {
		return dso.DelegationEvaluationResult{}, fmt.Errorf("offline evaluation requires at least %d replay requests", dso.MinimumLearningExperiences)
	}
	replayRefs := make([]string, 0, len(input.Replays))
	for _, request := range input.Replays {
		request.Schema = dso.Schema
		request.OwnerID = input.OwnerID
		if request.Mode == dso.ReplayLiveReexecution {
			return dso.DelegationEvaluationResult{}, fmt.Errorf("offline evaluation cannot execute live side effects")
		}
		result, runErr := s.replay.Run(ctx, request)
		if runErr != nil {
			return dso.DelegationEvaluationResult{}, log.WrapError(runErr, "GovernedLearning.EvaluateOffline.replay["+request.ReplayID+"]")
		}
		if result.Status != dso.ReplayCompleted || result.LiveSideEffects {
			return dso.DelegationEvaluationResult{}, fmt.Errorf("offline replay %s did not complete side-effect free", result.ReplayID)
		}
		replayRefs = append(replayRefs, result.ReplayID)
	}
	evaluation := newLearningEvaluation(*candidate, dso.LearningEvaluationOffline, input.Baseline, input.Candidate, replayRefs, nil, "dso-offline-replay/v1", s.now().UTC())
	if err := evaluation.Validate(*candidate); err != nil {
		return dso.DelegationEvaluationResult{}, log.WrapError(err, "GovernedLearning.EvaluateOffline.validate")
	}
	if err := s.persistEvaluation(ctx, evaluation); err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	return evaluation, nil
}

func (s *GovernedLearningService) Review(ctx context.Context, ownerID, candidateID, reviewerID, decision string, reasons []string) (dso.DelegationReviewDecision, error) {
	if err := s.requireLearningEnabled(ctx, ownerID); err != nil {
		return dso.DelegationReviewDecision{}, err
	}
	candidate, err := s.loadCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return dso.DelegationReviewDecision{}, err
	}
	offline, err := s.findPassingEvaluation(ctx, ownerID, candidateID, dso.LearningEvaluationOffline)
	if err != nil {
		return dso.DelegationReviewDecision{}, err
	}
	now := s.now().UTC()
	review := dso.DelegationReviewDecision{
		Schema: dso.Schema, ReviewID: "dso-review-" + ulid.New(), OwnerID: ownerID,
		CandidateRef: candidate.CandidateID, CandidateHash: candidate.DefinitionHash,
		OfflineEvaluationRef: offline.EvaluationID, OfflineEvaluationHash: offline.ContentHash,
		Decision: strings.ToUpper(strings.TrimSpace(decision)), Reasons: canonicalLearningRefs(reasons),
		ReviewerID: strings.TrimSpace(reviewerID), HumanConfirmed: true, ReviewedAt: now,
	}
	review.ContentHash, _ = dso.DelegationReviewContentHash(review)
	if err := review.Validate(*candidate, *offline); err != nil {
		return dso.DelegationReviewDecision{}, log.WrapError(err, "GovernedLearning.Review.validate")
	}
	content, err := json.Marshal(review)
	if err != nil {
		return dso.DelegationReviewDecision{}, err
	}
	record := entity.LearningReview{ReviewID: review.ReviewID, OwnerID: review.OwnerID, CandidateID: review.CandidateRef, EvaluationID: review.OfflineEvaluationRef, Decision: review.Decision, ReviewerID: review.ReviewerID, ContentHash: review.ContentHash, Content: string(content), CreatedAt: review.ReviewedAt}
	if err := s.store.CreateLearningReview(ctx, record, learningEvent(ctx, ownerID, review.ReviewID, "DelegationLearningCandidateReviewed", 1, map[string]any{"candidate_ref": candidateID, "decision": review.Decision, "reviewer_id": reviewerID}, now)); err != nil {
		return dso.DelegationReviewDecision{}, log.WrapError(err, "GovernedLearning.Review.persist")
	}
	return review, nil
}

func (s *GovernedLearningService) EvaluateShadow(ctx context.Context, ownerID, candidateID string) (dso.DelegationEvaluationResult, error) {
	if err := s.requireLearningEnabled(ctx, ownerID); err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	candidate, err := s.loadCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	if _, err := s.loadApprovedReview(ctx, ownerID, candidateID); err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	offline, err := s.findPassingEvaluation(ctx, ownerID, candidateID, dso.LearningEvaluationOffline)
	if err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	outcome, err := s.shadow.Evaluate(ctx, *candidate, *offline)
	if err != nil {
		return dso.DelegationEvaluationResult{}, log.WrapError(err, "GovernedLearning.EvaluateShadow.planOnly")
	}
	evaluation := newLearningEvaluation(*candidate, dso.LearningEvaluationShadow, outcome.Baseline, outcome.Candidate, outcome.ReplayResultRefs, &outcome.Proof, outcome.EvaluatorVersion, s.now().UTC())
	if err := evaluation.Validate(*candidate); err != nil {
		return dso.DelegationEvaluationResult{}, log.WrapError(err, "GovernedLearning.EvaluateShadow.validate")
	}
	if err := s.persistEvaluation(ctx, evaluation); err != nil {
		return dso.DelegationEvaluationResult{}, err
	}
	return evaluation, nil
}

func (s *GovernedLearningService) StartCanary(ctx context.Context, request CanaryRequest) (dso.DelegationLearningRollout, error) {
	if err := s.requireLearningEnabled(ctx, request.OwnerID); err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	candidate, review, offline, shadow, err := s.loadGovernanceChain(ctx, request.OwnerID, request.CandidateID)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	now := s.now().UTC()
	rollout := dso.DelegationLearningRollout{
		Schema: dso.Schema, RolloutID: "dso-rollout-" + ulid.New(), OwnerID: request.OwnerID,
		CandidateRef: candidate.CandidateID, CandidateHash: candidate.DefinitionHash,
		ReviewRef: review.ReviewID, OfflineEvaluationRef: offline.EvaluationID, ShadowEvaluationRef: shadow.EvaluationID,
		Status: dso.LearningRolloutCanary, RiskCeiling: "low", CanaryPercent: request.Percent,
		AllowedOwnerIDs: canonicalLearningRefs(request.AllowedOwnerIDs), FallbackPolicyRef: dso.RulePolicyFallbackRef,
		Revision: 1, ApprovedBy: strings.TrimSpace(request.ApprovedBy), CreatedAt: now, UpdatedAt: now,
	}
	rollout.ContentHash, _ = dso.DelegationRolloutContentHash(rollout)
	if err := rollout.Validate(*candidate, *review, *offline, *shadow); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.StartCanary.validate")
	}
	content, err := json.Marshal(rollout)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	record := entity.LearningRollout{RolloutID: rollout.RolloutID, OwnerID: rollout.OwnerID, CandidateID: rollout.CandidateRef, Kind: candidate.Kind, Status: rollout.Status, RiskCeiling: rollout.RiskCeiling, Revision: rollout.Revision, ContentHash: rollout.ContentHash, Content: string(content), CreatedAt: rollout.CreatedAt, UpdatedAt: rollout.UpdatedAt}
	if err := s.store.CreateLearningRollout(ctx, record, learningEvent(ctx, request.OwnerID, rollout.RolloutID, "DelegationLearningCanaryStarted", 1, map[string]any{"candidate_ref": request.CandidateID, "canary_percent": request.Percent, "allowed_owner_ids": rollout.AllowedOwnerIDs}, now)); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.StartCanary.persist")
	}
	return rollout, nil
}

func (s *GovernedLearningService) RecordBenchmark(ctx context.Context, ownerID, rolloutID string, report dso.DelegationBenchmarkReport) (dso.DelegationLearningRollout, error) {
	rolloutRecord, err := s.store.FindLearningRollout(ctx, ownerID, rolloutID)
	if err != nil || rolloutRecord == nil {
		if err == nil {
			err = fmt.Errorf("learning rollout not found")
		}
		return dso.DelegationLearningRollout{}, err
	}
	rollout, err := decodeLearningRollout(*rolloutRecord)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	if rollout.Status != dso.LearningRolloutCanary {
		return dso.DelegationLearningRollout{}, fmt.Errorf("%w: benchmark requires an active canary rollout", ErrGovernanceGateIncomplete)
	}
	report.Schema, report.OwnerID, report.CandidateRef = dso.Schema, ownerID, rolloutRecord.CandidateID
	if strings.TrimSpace(report.ReportID) == "" {
		report.ReportID = "dso-benchmark-" + ulid.New()
	}
	report.CreatedAt = s.now().UTC()
	report.ContentHash, _ = dso.DelegationBenchmarkContentHash(report)
	if err := report.Validate(); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.RecordBenchmark.validate")
	}
	content, _ := json.Marshal(report)
	benchmark := entity.LearningBenchmark{ReportID: report.ReportID, OwnerID: ownerID, CandidateID: report.CandidateRef, RolloutID: rolloutID, Passed: report.PassesPromotion(), ContentHash: report.ContentHash, Content: string(content), CreatedAt: report.CreatedAt}
	if err := s.store.CreateLearningBenchmark(ctx, benchmark, learningEvent(ctx, ownerID, report.ReportID, "DelegationLearningBenchmarkRecorded", 1, map[string]any{"rollout_id": rolloutID, "passed": benchmark.Passed, "primary_metric": report.PrimaryImprovement}, report.CreatedAt)); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.RecordBenchmark.persist")
	}
	if benchmark.Passed {
		return rollout, nil
	}
	// Regression rollback is synchronous and therefore completes inside the
	// one-minute acceptance budget without waiting for a background scheduler.
	return s.transitionRollout(ctx, rollout, dso.LearningRolloutRolledBack, "metric_regression")
}

func (s *GovernedLearningService) Promote(ctx context.Context, ownerID, rolloutID, approvedBy string) (dso.DelegationLearningRollout, error) {
	if err := s.requireLearningEnabled(ctx, ownerID); err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	record, err := s.store.FindLearningRollout(ctx, ownerID, rolloutID)
	if err != nil || record == nil {
		if err == nil {
			err = fmt.Errorf("learning rollout not found")
		}
		return dso.DelegationLearningRollout{}, err
	}
	benchmarks, err := s.store.ListLearningBenchmarks(ctx, ownerID, rolloutID)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	if len(benchmarks) == 0 || !benchmarks[len(benchmarks)-1].Passed {
		return dso.DelegationLearningRollout{}, fmt.Errorf("%w: latest canary benchmark is not passing", ErrGovernanceGateIncomplete)
	}
	rollout, err := decodeLearningRollout(*record)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	latest := benchmarks[len(benchmarks)-1]
	var report dso.DelegationBenchmarkReport
	if err := json.Unmarshal([]byte(latest.Content), &report); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.Promote.decodeBenchmark")
	}
	if report.ContentHash != latest.ContentHash || report.CandidateRef != rollout.CandidateRef || !report.PassesPromotion() {
		return dso.DelegationLearningRollout{}, fmt.Errorf("%w: latest benchmark provenance is invalid", ErrGovernanceGateIncomplete)
	}
	if err := report.Validate(); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.Promote.validateBenchmark")
	}
	rollout.ApprovedBy = strings.TrimSpace(approvedBy)
	return s.transitionRollout(ctx, rollout, dso.LearningRolloutPromoted, "human_promotion")
}

func (s *GovernedLearningService) Disable(ctx context.Context, ownerID, rolloutID, requestedBy string) (dso.DelegationLearningRollout, error) {
	record, err := s.store.FindLearningRollout(ctx, ownerID, rolloutID)
	if err != nil || record == nil {
		if err == nil {
			err = fmt.Errorf("learning rollout not found")
		}
		return dso.DelegationLearningRollout{}, err
	}
	rollout, err := decodeLearningRollout(*record)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	rollout.ApprovedBy = strings.TrimSpace(requestedBy)
	return s.transitionRollout(ctx, rollout, dso.LearningRolloutDisabled, "instant_disable")
}

func (s *GovernedLearningService) Resolve(ctx context.Context, ownerID, kind, riskLevel, taskID string) (ResolvedLearningArtifact, error) {
	fallback := ResolvedLearningArtifact{Source: "RULE_POLICY", FallbackPolicyRef: dso.RulePolicyFallbackRef}
	if err := s.requireLearningEnabled(ctx, ownerID); errors.Is(err, ErrDelegationLearningDisabled) {
		return fallback, nil
	} else if err != nil {
		return fallback, err
	}
	record, err := s.store.FindEffectiveLearningRollout(ctx, ownerID, kind)
	if err != nil {
		return fallback, err
	}
	if record == nil {
		return fallback, nil
	}
	rollout, err := decodeLearningRollout(*record)
	if err != nil {
		return fallback, log.WrapError(err, "GovernedLearning.Resolve.decodeRollout")
	}
	if rollout.Status == dso.LearningRolloutCanary {
		if strings.ToLower(strings.TrimSpace(riskLevel)) != "low" || !containsLearningRef(rollout.AllowedOwnerIDs, ownerID) || stableLearningBucket(ownerID+"|"+taskID+"|"+rollout.RolloutID) >= rollout.CanaryPercent {
			return fallback, nil
		}
	}
	candidate, review, offline, shadow, err := s.loadExactGovernanceChain(ctx, ownerID, rollout)
	if err != nil {
		return fallback, log.WrapError(err, "GovernedLearning.Resolve.governanceChain")
	}
	if err := rollout.Validate(*candidate, *review, *offline, *shadow); err != nil {
		return fallback, log.WrapError(err, "GovernedLearning.Resolve.validateRollout")
	}
	return ResolvedLearningArtifact{Source: rollout.Status, FallbackPolicyRef: dso.RulePolicyFallbackRef, Candidate: candidate, Rollout: &rollout}, nil
}

func (s *GovernedLearningService) Snapshot(ctx context.Context, ownerID string) (entity.LearningSnapshot, error) {
	preference, err := s.store.GetLearningPreference(ctx, ownerID)
	if err != nil {
		return entity.LearningSnapshot{}, err
	}
	candidates, err := s.store.ListLearningCandidates(ctx, ownerID, 200)
	if err != nil {
		return entity.LearningSnapshot{}, err
	}
	rollouts, err := s.store.ListLearningRollouts(ctx, ownerID, 200)
	if err != nil {
		return entity.LearningSnapshot{}, err
	}
	snapshot := entity.LearningSnapshot{Preference: preference, Candidates: candidates, Rollouts: rollouts}
	for _, candidate := range candidates {
		evaluations, listErr := s.store.ListLearningEvaluations(ctx, ownerID, candidate.CandidateID)
		if listErr != nil {
			return entity.LearningSnapshot{}, listErr
		}
		reviews, listErr := s.store.ListLearningReviews(ctx, ownerID, candidate.CandidateID)
		if listErr != nil {
			return entity.LearningSnapshot{}, listErr
		}
		snapshot.Evaluations = append(snapshot.Evaluations, evaluations...)
		snapshot.Reviews = append(snapshot.Reviews, reviews...)
	}
	for _, rollout := range rollouts {
		benchmarks, listErr := s.store.ListLearningBenchmarks(ctx, ownerID, rollout.RolloutID)
		if listErr != nil {
			return entity.LearningSnapshot{}, listErr
		}
		snapshot.Benchmarks = append(snapshot.Benchmarks, benchmarks...)
	}
	return snapshot, nil
}

func (s *GovernedLearningService) requireLearningEnabled(ctx context.Context, ownerID string) error {
	preference, err := s.store.GetLearningPreference(ctx, strings.TrimSpace(ownerID))
	if err != nil {
		return err
	}
	if !preference.Enabled {
		return ErrDelegationLearningDisabled
	}
	if s.consent != nil {
		global, err := s.consent.GetPreference(ctx, strings.TrimSpace(ownerID))
		if err != nil {
			return err
		}
		if global != nil && !global.LearningEnabled {
			return ErrDelegationLearningDisabled
		}
	}
	return nil
}

func (s *GovernedLearningService) loadCandidate(ctx context.Context, ownerID, candidateID string) (*dso.DelegationLearningCandidate, error) {
	record, err := s.store.FindLearningCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("delegation learning candidate not found")
	}
	var candidate dso.DelegationLearningCandidate
	if err := json.Unmarshal([]byte(record.Content), &candidate); err != nil {
		return nil, log.WrapError(err, "GovernedLearning.loadCandidate.decode")
	}
	if err := candidate.Validate(); err != nil || candidate.DefinitionHash != record.DefinitionHash {
		if err == nil {
			err = fmt.Errorf("candidate record hash does not match content")
		}
		return nil, log.WrapError(err, "GovernedLearning.loadCandidate.validate")
	}
	return &candidate, nil
}

func (s *GovernedLearningService) findPassingEvaluation(ctx context.Context, ownerID, candidateID, stage string) (*dso.DelegationEvaluationResult, error) {
	values, err := s.store.ListLearningEvaluations(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	for index := len(values) - 1; index >= 0; index-- {
		if values[index].Stage == stage && values[index].Passed {
			var result dso.DelegationEvaluationResult
			if err := json.Unmarshal([]byte(values[index].Content), &result); err != nil {
				return nil, err
			}
			return &result, nil
		}
	}
	return nil, fmt.Errorf("%w: passing %s evaluation not found", ErrGovernanceGateIncomplete, stage)
}

func (s *GovernedLearningService) loadApprovedReview(ctx context.Context, ownerID, candidateID string) (*dso.DelegationReviewDecision, error) {
	record, err := s.store.FindApprovedLearningReview(ctx, ownerID, candidateID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("%w: approved human review not found", ErrGovernanceGateIncomplete)
	}
	var review dso.DelegationReviewDecision
	if err := json.Unmarshal([]byte(record.Content), &review); err != nil {
		return nil, err
	}
	return &review, nil
}

func (s *GovernedLearningService) loadGovernanceChain(ctx context.Context, ownerID, candidateID string) (*dso.DelegationLearningCandidate, *dso.DelegationReviewDecision, *dso.DelegationEvaluationResult, *dso.DelegationEvaluationResult, error) {
	candidate, err := s.loadCandidate(ctx, ownerID, candidateID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	offline, err := s.findPassingEvaluation(ctx, ownerID, candidateID, dso.LearningEvaluationOffline)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	review, err := s.loadApprovedReview(ctx, ownerID, candidateID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := review.Validate(*candidate, *offline); err != nil {
		return nil, nil, nil, nil, err
	}
	shadow, err := s.findPassingEvaluation(ctx, ownerID, candidateID, dso.LearningEvaluationShadow)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := shadow.Validate(*candidate); err != nil {
		return nil, nil, nil, nil, err
	}
	return candidate, review, offline, shadow, nil
}

func (s *GovernedLearningService) loadExactGovernanceChain(ctx context.Context, ownerID string, rollout dso.DelegationLearningRollout) (*dso.DelegationLearningCandidate, *dso.DelegationReviewDecision, *dso.DelegationEvaluationResult, *dso.DelegationEvaluationResult, error) {
	candidate, err := s.loadCandidate(ctx, ownerID, rollout.CandidateRef)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	loadEvaluation := func(evaluationID, expectedStage string) (*dso.DelegationEvaluationResult, error) {
		record, loadErr := s.store.FindLearningEvaluation(ctx, ownerID, evaluationID)
		if loadErr != nil || record == nil {
			if loadErr == nil {
				loadErr = fmt.Errorf("%s evaluation not found", expectedStage)
			}
			return nil, loadErr
		}
		var evaluation dso.DelegationEvaluationResult
		if loadErr = json.Unmarshal([]byte(record.Content), &evaluation); loadErr != nil {
			return nil, loadErr
		}
		if evaluation.Stage != expectedStage || !evaluation.Passed || evaluation.ContentHash != record.ContentHash {
			return nil, fmt.Errorf("%s evaluation does not match the rollout lineage", expectedStage)
		}
		if loadErr = evaluation.Validate(*candidate); loadErr != nil {
			return nil, loadErr
		}
		return &evaluation, nil
	}
	offline, err := loadEvaluation(rollout.OfflineEvaluationRef, dso.LearningEvaluationOffline)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	shadow, err := loadEvaluation(rollout.ShadowEvaluationRef, dso.LearningEvaluationShadow)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	reviewRecords, err := s.store.ListLearningReviews(ctx, ownerID, candidate.CandidateID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var review *dso.DelegationReviewDecision
	for _, record := range reviewRecords {
		if record.ReviewID != rollout.ReviewRef {
			continue
		}
		var value dso.DelegationReviewDecision
		if err := json.Unmarshal([]byte(record.Content), &value); err != nil {
			return nil, nil, nil, nil, err
		}
		if value.ContentHash != record.ContentHash {
			return nil, nil, nil, nil, fmt.Errorf("learning review content hash does not match its record")
		}
		review = &value
		break
	}
	if review == nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: rollout review was not found", ErrGovernanceGateIncomplete)
	}
	if err := review.Validate(*candidate, *offline); err != nil {
		return nil, nil, nil, nil, err
	}
	return candidate, review, offline, shadow, nil
}

func (s *GovernedLearningService) persistEvaluation(ctx context.Context, evaluation dso.DelegationEvaluationResult) error {
	content, err := json.Marshal(evaluation)
	if err != nil {
		return err
	}
	record := entity.LearningEvaluation{EvaluationID: evaluation.EvaluationID, OwnerID: evaluation.OwnerID, CandidateID: evaluation.CandidateRef, Stage: evaluation.Stage, Passed: evaluation.Passed, ContentHash: evaluation.ContentHash, Content: string(content), CreatedAt: evaluation.EvaluatedAt}
	return log.WrapError(s.store.CreateLearningEvaluation(ctx, record, learningEvent(ctx, evaluation.OwnerID, evaluation.EvaluationID, "DelegationLearning"+strings.Title(strings.ToLower(evaluation.Stage))+"Evaluated", 1, map[string]any{"candidate_ref": evaluation.CandidateRef, "passed": evaluation.Passed, "content_hash": evaluation.ContentHash}, evaluation.EvaluatedAt)), "GovernedLearning.persistEvaluation")
}

func (s *GovernedLearningService) transitionRollout(ctx context.Context, rollout dso.DelegationLearningRollout, status, reason string) (dso.DelegationLearningRollout, error) {
	previousRevision := rollout.Revision
	rollout.Status = status
	rollout.Revision++
	rollout.UpdatedAt = s.now().UTC()
	if status != dso.LearningRolloutCanary {
		rollout.CanaryPercent = 0
		rollout.AllowedOwnerIDs = nil
	}
	rollout.ContentHash, _ = dso.DelegationRolloutContentHash(rollout)
	content, err := json.Marshal(rollout)
	if err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	eventType := "DelegationLearningRollout" + strings.Title(strings.ToLower(status))
	if err := s.store.TransitionLearningRollout(ctx, rollout.OwnerID, rollout.RolloutID, previousRevision, status, rollout.ContentHash, string(content), rollout.UpdatedAt, learningEvent(ctx, rollout.OwnerID, rollout.RolloutID, eventType, rollout.Revision, map[string]any{"status": status, "reason": reason, "candidate_ref": rollout.CandidateRef}, rollout.UpdatedAt)); err != nil {
		return dso.DelegationLearningRollout{}, log.WrapError(err, "GovernedLearning.transitionRollout")
	}
	return rollout, nil
}

func newLearningEvaluation(candidate dso.DelegationLearningCandidate, stage string, baseline, candidateMetrics dso.DelegationBenchmarkMetrics, replayRefs []string, proof *dso.LearningShadowProof, evaluator string, now time.Time) dso.DelegationEvaluationResult {
	improvements := improvedLearningMetrics(candidateMetrics, baseline)
	evaluation := dso.DelegationEvaluationResult{
		Schema: dso.Schema, EvaluationID: "dso-evaluation-" + ulid.New(), OwnerID: candidate.OwnerID,
		CandidateRef: candidate.CandidateID, CandidateHash: candidate.DefinitionHash, Stage: stage,
		ReplayResultRefs: canonicalLearningRefs(replayRefs), Baseline: baseline, Candidate: candidateMetrics,
		ImprovedMetrics: improvements, SafetyRegressed: candidateMetrics.SafetyScore < baseline.SafetyScore,
		ShadowProof: proof, Passed: len(improvements) > 0 && candidateMetrics.SafetyScore >= baseline.SafetyScore,
		EvaluatorVersion: evaluator, EvaluatedAt: now,
	}
	evaluation.ContentHash, _ = dso.DelegationEvaluationContentHash(evaluation)
	return evaluation
}

func improvedLearningMetrics(candidate, baseline dso.DelegationBenchmarkMetrics) []string {
	values := make([]string, 0, 4)
	if candidate.QualityScore > baseline.QualityScore {
		values = append(values, "quality_score")
	}
	if candidate.RecoveryRate > baseline.RecoveryRate {
		values = append(values, "recovery_rate")
	}
	if candidate.P95LatencyMS < baseline.P95LatencyMS {
		values = append(values, "p95_latency_ms")
	}
	if candidate.AverageCostMicros < baseline.AverageCostMicros {
		values = append(values, "average_cost_micros")
	}
	sort.Strings(values)
	return values
}

func decodeLearningRollout(record entity.LearningRollout) (dso.DelegationLearningRollout, error) {
	var rollout dso.DelegationLearningRollout
	if err := json.Unmarshal([]byte(record.Content), &rollout); err != nil {
		return dso.DelegationLearningRollout{}, err
	}
	if rollout.ContentHash != record.ContentHash || rollout.Revision != record.Revision || rollout.Status != record.Status {
		return dso.DelegationLearningRollout{}, fmt.Errorf("learning rollout record does not match immutable content")
	}
	return rollout, nil
}

func learningEvent(ctx context.Context, ownerID, aggregateID, eventType string, sequence int64, payload any, at time.Time) entity.Event {
	encoded, _ := json.Marshal(payload)
	traceID := log.ReqID(ctx)
	if traceID == "" {
		traceID = "dso-learning-" + ulid.New()
	}
	return entity.Event{EventID: "event-" + ulid.New(), OwnerID: ownerID, AggregateType: "dso_learning", AggregateID: aggregateID, Sequence: sequence, Type: eventType, IdempotencyKey: aggregateID + ":" + eventType + fmt.Sprintf(":%d", sequence), TraceID: traceID, CausationID: aggregateID, Payload: string(encoded), CreatedAt: at}
}

func canonicalLearningRefs(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsLearningRef(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func stableLearningBucket(value string) int {
	digest := sha256.Sum256([]byte(value))
	return int(binary.BigEndian.Uint32(digest[:4]) % 100)
}
