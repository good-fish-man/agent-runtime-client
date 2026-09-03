package deployment

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/deployment"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	runtimeartifact "github.com/good-fish-man/athena-protocol/draft/runtimeartifact"
	deploymentv1 "github.com/good-fish-man/athena-protocol/protocol/deployment/v1"
	log "github.com/good-fish-man/logx"
)

const (
	defaultKernelVersion          = "athena-kernel-v0.5"
	defaultPlannerVersion         = "planner-v1"
	defaultPolicyVersion          = "policy-v1"
	defaultProtocolVersion        = "athena.agent.v4"
	defaultOntologyVersion        = "none"
	defaultEvaluationSuiteVersion = "evaluation-v1"
)

type Service struct {
	store               repository.Store
	shadowEvaluator     ShadowEvaluator
	delegationArtifacts DelegationArtifactApprovalResolver
}

func NewService(store repository.Store) *Service {
	return NewServiceWithShadowEvaluator(store, NewDeclarativeShadowEvaluator())
}

func NewServiceWithShadowEvaluator(store repository.Store, evaluator ShadowEvaluator) *Service {
	if evaluator == nil {
		evaluator = NewDeclarativeShadowEvaluator()
	}
	return &Service{store: store, shadowEvaluator: evaluator}
}

type CreateBuildRequest struct {
	AgentID                   string            `json:"agent_id"`
	Version                   string            `json:"version"`
	KernelVersion             string            `json:"kernel_version"`
	PlannerVersion            string            `json:"planner_version"`
	PolicyVersion             string            `json:"policy_version"`
	ProtocolVersion           string            `json:"protocol_version"`
	SkillVersions             map[string]string `json:"skill_versions,omitempty"`
	StrategyVersions          map[string]string `json:"strategy_versions,omitempty"`
	DelegationPolicyVersions  map[string]string `json:"delegation_policy_versions,omitempty"`
	SpecialistProfileVersions map[string]string `json:"specialist_profile_versions,omitempty"`
	OntologyVersion           string            `json:"ontology_version"`
	PromptTemplateVersions    map[string]string `json:"prompt_template_versions"`
	EvaluationSuiteVersion    string            `json:"evaluation_suite_version"`
	RiskLevel                 string            `json:"risk_level"`
}

type CreatePromotionRequest struct {
	BuildID       string                  `json:"build_id"`
	CanaryPercent int                     `json:"canary_percent"`
	Thresholds    entity.CanaryThresholds `json:"thresholds"`
}

type TransitionRequest struct {
	TargetStatus     string `json:"target_status"`
	ExpectedRevision int64  `json:"expected_revision"`
	Explicit         bool   `json:"explicit"`
}

type ShadowRequest struct {
	TaskID          string           `json:"task_id"`
	Input           map[string]any   `json:"input"`
	CapabilityHints []string         `json:"capability_hints,omitempty"`
	Budget          entity.RunBudget `json:"budget"`
}

type CanarySampleRequest struct {
	ManifestID   string  `json:"manifest_id"`
	Succeeded    bool    `json:"succeeded"`
	LatencyMS    int64   `json:"latency_ms"`
	CostMicros   int64   `json:"cost_micros"`
	SafetyScore  float64 `json:"safety_score"`
	Intervention bool    `json:"intervention"`
}

type RunOutcome struct {
	Succeeded    bool
	LatencyMS    int64
	CostMicros   int64
	SafetyScore  float64
	Intervention bool
}

type CompensationRequest struct {
	ActionID     string `json:"action_id"`
	Instructions string `json:"instructions"`
}

type RollbackRequest struct {
	Reason           string                `json:"reason"`
	ExpectedRevision int64                 `json:"expected_revision"`
	Compensations    []CompensationRequest `json:"compensations,omitempty"`
}

type RunManifestInput struct {
	TaskID              string
	AgentID             string
	ModelConfigVersion  string
	CapabilityInstances []string
	DeviceID            string
	WorldRevision       int64
	KnowledgeSnapshot   string
	Budget              entity.RunBudget
	FeatureFlags        map[string]bool
}

// ResolveRuntimeArtifacts loads and verifies the exact reviewed definitions
// selected by a run manifest. Resolution fails closed if a build, checksum,
// approval, or owner scope no longer matches.
func (s *Service) ResolveRuntimeArtifacts(ctx context.Context, ownerID string, manifest entity.RunManifest) (*runtimeartifact.Bundle, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("deployment service is not configured")
	}
	if manifest.OwnerID != ownerID {
		return nil, apierror.ErrForbidden.WithMessage("run manifest crosses owner boundary")
	}
	build, err := s.store.FindBuild(ctx, ownerID, manifest.AgentBuildID)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.ResolveRuntimeArtifacts.build")
	}
	if build == nil {
		return nil, apierror.ErrNotFound.WithMessage("run manifest build is missing")
	}
	if err := build.Validate(); err != nil {
		return nil, log.WrapError(err, "DeploymentService.ResolveRuntimeArtifacts.buildIntegrity")
	}
	if err := manifest.Validate(*build); err != nil {
		return nil, log.WrapError(err, "DeploymentService.ResolveRuntimeArtifacts.manifestIntegrity")
	}
	skills, strategies, err := s.store.LoadApprovedRuntimeArtifacts(ctx, ownerID, authctx.OrganizationID(ctx), *build)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.ResolveRuntimeArtifacts.load")
	}
	bundle := &runtimeartifact.Bundle{
		Schema: runtimeartifact.Schema, OwnerID: ownerID, AgentID: manifest.AgentID,
		BuildID: build.BuildID, BuildChecksum: build.Checksum, ManifestID: manifest.ManifestID,
		Skills: skills, Strategies: strategies, ResolvedAt: time.Now().UTC(),
	}
	bundle.Normalize()
	if err := bundle.Validate(); err != nil {
		return nil, log.WrapError(err, "DeploymentService.ResolveRuntimeArtifacts.validate")
	}
	return bundle, nil
}

func (s *Service) CreateBuild(ctx context.Context, ownerID, actorID string, request CreateBuildRequest) (*entity.AgentBuild, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("deployment service is not configured")
	}
	request = defaults(request)
	approvals, err := s.store.ResolveApprovedArtifacts(ctx, ownerID, authctx.OrganizationID(ctx), request.SkillVersions, request.StrategyVersions)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.CreateBuild.artifacts")
	}
	approvalReferences := append([]entity.ArtifactApprovalReference(nil), approvals.References...)
	if len(request.DelegationPolicyVersions) > 0 || len(request.SpecialistProfileVersions) > 0 {
		if s.delegationArtifacts == nil {
			return nil, apierror.ErrBadRequest.WithMessage("delegation artifacts require the governed learning resolver")
		}
		delegationApprovals, resolveErr := s.delegationArtifacts.ResolveBuildApprovals(
			ctx,
			ownerID,
			request.DelegationPolicyVersions,
			request.SpecialistProfileVersions,
		)
		if resolveErr != nil {
			return nil, log.WrapError(resolveErr, "DeploymentService.CreateBuild.delegationArtifacts")
		}
		approvalReferences = append(approvalReferences, delegationApprovals...)
	}
	build, err := makeBuild(ownerID, actorID, traceID(ctx), request, approvalReferences)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateBuild(ctx, *build); err != nil {
		return nil, log.WrapError(err, "DeploymentService.CreateBuild.persist")
	}
	return build, nil
}

func (s *Service) ListBuilds(ctx context.Context, ownerID string, filter entity.BuildFilter) ([]entity.AgentBuild, int64, error) {
	return s.store.ListBuilds(ctx, ownerID, filter)
}

func (s *Service) FindBuild(ctx context.Context, ownerID, buildID string) (*entity.AgentBuild, error) {
	return s.store.FindBuild(ctx, ownerID, buildID)
}

func (s *Service) ProposePromotion(ctx context.Context, ownerID string, request CreatePromotionRequest) (*entity.Promotion, error) {
	build, err := s.store.FindBuild(ctx, ownerID, request.BuildID)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, apierror.ErrNotFound.WithMessage("agent build not found")
	}
	if err := build.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("agent build integrity check failed: " + err.Error())
	}
	previous, err := s.store.FindActivePromotion(ctx, ownerID, build.AgentID)
	if err != nil {
		return nil, err
	}
	if request.Thresholds.MinimumSamples == 0 {
		request.Thresholds = defaultThresholds()
	}
	now := time.Now().UTC()
	promotion := entity.Promotion{
		Schema: entity.Schema, PromotionID: ulid.New(), OwnerID: ownerID, AgentID: build.AgentID,
		BuildID: build.BuildID, Status: entity.StatusProposed, RiskLevel: build.RiskLevel,
		CanaryPercent: request.CanaryPercent, Thresholds: request.Thresholds,
		Verified: build.Verified, Recoverable: build.Recoverable && previous != nil, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if previous != nil {
		promotion.PreviousBuildID = previous.BuildID
	}
	if err := promotion.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreatePromotion(ctx, promotion); err != nil {
		return nil, log.WrapError(err, "DeploymentService.ProposePromotion.persist")
	}
	return &promotion, nil
}

func (s *Service) TransitionPromotion(ctx context.Context, ownerID, actorID, promotionID string, request TransitionRequest) (*entity.Promotion, error) {
	promotion, err := s.store.FindPromotion(ctx, ownerID, promotionID)
	if err != nil {
		return nil, err
	}
	if promotion == nil {
		return nil, apierror.ErrNotFound.WithMessage("promotion not found")
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = promotion.Revision
	}
	target := strings.ToUpper(strings.TrimSpace(request.TargetStatus))
	if !deploymentv1.CanTransition(promotion.Status, target) {
		return nil, apierror.ErrBadRequest.WithMessage("invalid promotion transition: " + promotion.Status + " -> " + target)
	}
	if target == entity.StatusActive && promotion.Status == entity.StatusShadow {
		if (promotion.RiskLevel == entity.RiskR0 || promotion.RiskLevel == entity.RiskR1) || !request.Explicit {
			return nil, apierror.ErrBadRequest.WithMessage("low-risk builds must pass canary; R2/R3 activation requires explicit confirmation")
		}
	}
	if target == entity.StatusCanary && !promotion.CanAutoCanary() {
		return nil, apierror.ErrBadRequest.WithMessage("automatic canary is restricted to verified, recoverable R0/R1 builds")
	}
	if promotion.Status == entity.StatusShadow && (target == entity.StatusCanary || target == entity.StatusActive) {
		results, listErr := s.store.ListShadowResults(ctx, ownerID, promotionID, 100)
		if listErr != nil {
			return nil, listErr
		}
		passed := false
		for _, result := range results {
			if result.Passed && result.NoExternalSideEffects && result.ExecutedActionCount == 0 &&
				result.ProductionBuildID == promotion.PreviousBuildID && result.CandidateBuildID == promotion.BuildID {
				control, controlErr := s.store.FindBuild(ctx, ownerID, result.ProductionBuildID)
				candidate, candidateErr := s.store.FindBuild(ctx, ownerID, result.CandidateBuildID)
				if controlErr != nil || candidateErr != nil || control == nil || candidate == nil || control.Checksum != result.ProductionBuildHash || candidate.Checksum != result.CandidateBuildHash {
					continue
				}
				passed = true
				break
			}
		}
		if !passed {
			return nil, apierror.ErrBadRequest.WithMessage("promotion needs a passed, side-effect-free shadow result")
		}
	}
	if promotion.Status == entity.StatusCanary && target == entity.StatusActive {
		metrics, listErr := s.store.ListCanaryMetrics(ctx, ownerID, promotionID, 1)
		if listErr != nil {
			return nil, listErr
		}
		if len(metrics) == 0 || metrics[0].SampleCount < promotion.Thresholds.MinimumSamples || metrics[0].StopTriggered || metrics[0].LatestSampleID == "" || metrics[0].SamplesDigest == "" {
			return nil, apierror.ErrBadRequest.WithMessage("promotion needs a healthy canary metric at the minimum sample size")
		}
		if stop, _ := promotion.Thresholds.Evaluate(metrics[0]); stop {
			return nil, apierror.ErrBadRequest.WithMessage("latest canary metric exceeds a stop threshold")
		}
	}
	if target == entity.StatusReviewed || target == entity.StatusActive {
		promotion.ApprovedBy = actorID
	}
	promotion.Status = target
	promotion.Revision = request.ExpectedRevision + 1
	promotion.UpdatedAt = time.Now().UTC()
	if err := promotion.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if target == entity.StatusActive {
		previous, findErr := s.store.FindActivePromotion(ctx, ownerID, promotion.AgentID)
		if findErr != nil {
			return nil, findErr
		}
		if err := s.store.ActivatePromotion(ctx, *promotion, previous, request.ExpectedRevision); err != nil {
			return nil, err
		}
		return promotion, nil
	}
	if err := s.store.UpdatePromotion(ctx, *promotion, request.ExpectedRevision); err != nil {
		return nil, err
	}
	return promotion, nil
}

func (s *Service) ListPromotions(ctx context.Context, ownerID string, filter entity.PromotionFilter) ([]entity.Promotion, int64, error) {
	return s.store.ListPromotions(ctx, ownerID, filter)
}

func (s *Service) EvaluateShadow(ctx context.Context, ownerID, promotionID string, request ShadowRequest) (*entity.ShadowResult, error) {
	promotion, err := s.store.FindPromotion(ctx, ownerID, promotionID)
	if err != nil {
		return nil, err
	}
	if promotion == nil || promotion.Status != entity.StatusShadow {
		return nil, apierror.ErrBadRequest.WithMessage("promotion is not in SHADOW")
	}
	if strings.TrimSpace(request.TaskID) == "" || len(request.Input) == 0 {
		return nil, apierror.ErrBadRequest.WithMessage("shadow task_id and structured input are required")
	}
	if promotion.PreviousBuildID == "" {
		return nil, apierror.ErrBadRequest.WithMessage("shadow needs an active control build")
	}
	controlBuild, err := s.store.FindBuild(ctx, ownerID, promotion.PreviousBuildID)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.controlBuild")
	}
	candidateBuild, err := s.store.FindBuild(ctx, ownerID, promotion.BuildID)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.candidateBuild")
	}
	if controlBuild == nil || candidateBuild == nil {
		return nil, apierror.ErrBadRequest.WithMessage("shadow control or candidate build is missing")
	}
	request.Budget = defaultShadowBudget(request.Budget)
	inputDigest, err := digest(request.Input)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("shadow input is not serializable")
	}
	started := time.Now()
	controlPlan, err := s.shadowEvaluator.Evaluate(ctx, ShadowEvaluationInput{
		TaskID: request.TaskID, InputDigest: inputDigest, Input: request.Input, Build: *controlBuild,
		Budget: request.Budget, CapabilityHints: unique(request.CapabilityHints),
	})
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.control")
	}
	candidatePlan, err := s.shadowEvaluator.Evaluate(ctx, ShadowEvaluationInput{
		TaskID: request.TaskID, InputDigest: inputDigest, Input: request.Input, Build: *candidateBuild,
		Budget: request.Budget, CapabilityHints: unique(request.CapabilityHints),
	})
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.candidate")
	}
	controlRouteHash, _ := digest(controlPlan.Route)
	candidateRouteHash, _ := digest(candidatePlan.Route)
	controlGraphHash, _ := digest(controlPlan.Graph)
	candidateGraphHash, _ := digest(candidatePlan.Graph)
	controlActionsHash, _ := digest(controlPlan.PlannedActions)
	candidateActionsHash, _ := digest(candidatePlan.PlannedActions)
	controlProof, err := proofFor(*controlBuild, inputDigest, controlPlan.Effects)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.controlProof")
	}
	candidateProof, err := proofFor(*candidateBuild, inputDigest, candidatePlan.Effects)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.candidateProof")
	}
	checks := []entity.ShadowCheck{
		{ID: "same_input", Required: true, Passed: inputDigest != "", Detail: "control and candidate received the same canonical input digest"},
		{ID: "control_plan_only", Required: true, Passed: noEffects(controlPlan.Effects), Detail: "control evaluator cannot call network, device, credentials, or world writes"},
		{ID: "candidate_plan_only", Required: true, Passed: noEffects(candidatePlan.Effects), Detail: "candidate evaluator cannot call network, device, credentials, or world writes"},
		{ID: "candidate_risk_bounded", Required: true, Passed: riskRank(candidatePlan.RiskLevel) <= riskRank(candidateBuild.RiskLevel), Detail: "candidate plan risk must not exceed its reviewed build"},
		{ID: "cost_budget", Required: true, Passed: candidatePlan.EstimatedCostMicros <= request.Budget.MaxCostMicros, Detail: "candidate estimated cost must remain inside the shadow budget"},
		{ID: "action_budget", Required: true, Passed: len(candidatePlan.PlannedActions) <= request.Budget.MaxActions, Detail: "candidate planned action count must remain inside the shadow budget"},
		{ID: "plan_complete", Required: true, Passed: len(controlPlan.Route) > 0 && len(candidatePlan.Route) > 0 && len(controlPlan.Graph) > 0 && len(candidatePlan.Graph) > 0, Detail: "both builds produced a route and graph"},
	}
	passed := true
	for _, check := range checks {
		if check.Required && !check.Passed {
			passed = false
		}
	}
	trace := traceID(ctx)
	if trace == "" {
		trace = "shadow-" + request.TaskID
	}
	shadow := entity.ShadowResult{
		Schema: entity.Schema, ShadowID: ulid.New(), PromotionID: promotionID, OwnerID: ownerID,
		TaskID: request.TaskID, TraceID: trace, InputDigest: inputDigest, EvaluatorVersion: s.shadowEvaluator.Version(),
		ProductionBuildID: controlBuild.BuildID, CandidateBuildID: candidateBuild.BuildID,
		ProductionBuildHash: controlBuild.Checksum, CandidateBuildHash: candidateBuild.Checksum,
		ProductionRouteHash: controlRouteHash, CandidateRouteHash: candidateRouteHash,
		ProductionGraphHash: controlGraphHash, CandidateGraphHash: candidateGraphHash,
		ProductionActionsHash: controlActionsHash, CandidateActionsHash: candidateActionsHash,
		ProductionCostMicros: controlPlan.EstimatedCostMicros, CandidateCostMicros: candidatePlan.EstimatedCostMicros,
		ProductionRisk: controlPlan.RiskLevel, CandidateRisk: candidatePlan.RiskLevel,
		ProductionPlannedActions: len(controlPlan.PlannedActions), CandidatePlannedActions: len(candidatePlan.PlannedActions),
		ProductionProof: controlProof, CandidateProof: candidateProof, Checks: checks,
		LatencyMS: time.Since(started).Milliseconds(), NoExternalSideEffects: noEffects(controlPlan.Effects) && noEffects(candidatePlan.Effects),
		ExecutedActionCount: 0, Passed: passed, Summary: shadowSummary(controlPlan, candidatePlan, passed), CreatedAt: time.Now().UTC(),
	}
	if err := shadow.Validate(); err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.validate")
	}
	if err := s.store.CreateShadowResult(ctx, shadow); err != nil {
		return nil, log.WrapError(err, "DeploymentService.EvaluateShadow.persist")
	}
	return &shadow, nil
}

func (s *Service) ListShadowResults(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.ShadowResult, error) {
	return s.store.ListShadowResults(ctx, ownerID, promotionID, limit)
}

func (s *Service) RecordCanarySample(ctx context.Context, ownerID, promotionID string, request CanarySampleRequest) (*entity.CanaryMetric, *entity.Promotion, error) {
	promotion, err := s.store.FindPromotion(ctx, ownerID, promotionID)
	if err != nil {
		return nil, nil, err
	}
	if promotion == nil || promotion.Status != entity.StatusCanary {
		return nil, nil, apierror.ErrBadRequest.WithMessage("promotion is not in CANARY")
	}
	manifest, err := s.store.FindRunManifest(ctx, ownerID, strings.TrimSpace(request.ManifestID))
	if err != nil {
		return nil, nil, log.WrapError(err, "DeploymentService.RecordCanarySample.manifest")
	}
	if manifest == nil {
		return nil, nil, apierror.ErrNotFound.WithMessage("run manifest not found")
	}
	exposure, err := s.store.FindExposure(ctx, promotionID, ownerID, promotion.AgentID)
	if err != nil {
		return nil, nil, log.WrapError(err, "DeploymentService.RecordCanarySample.exposure")
	}
	if exposure == nil || exposure.ExposureID != manifest.ExposureID || exposure.OptedOut || exposure.Variant != entity.VariantCandidate {
		return nil, nil, apierror.ErrBadRequest.WithMessage("run manifest is not assigned to the candidate canary cohort")
	}
	now := time.Now().UTC()
	trace := traceID(ctx)
	if trace == "" {
		trace = "canary-" + manifest.ManifestID
	}
	sample := entity.CanarySample{
		Schema: entity.Schema, SampleID: ulid.New(), PromotionID: promotionID, OwnerID: ownerID,
		ManifestID: manifest.ManifestID, ExposureID: manifest.ExposureID, AgentBuildID: manifest.AgentBuildID,
		TaskID: manifest.TaskID, Succeeded: request.Succeeded, LatencyMS: request.LatencyMS,
		CostMicros: request.CostMicros, SafetyScore: request.SafetyScore, Intervention: request.Intervention,
		TraceID: trace, CreatedAt: now,
	}
	if err := sample.Validate(*manifest, *promotion); err != nil {
		return nil, nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	metric, update, err := s.store.AppendCanarySample(ctx, sample, *promotion)
	if err != nil {
		return nil, nil, log.WrapError(err, "DeploymentService.RecordCanarySample.persist")
	}
	return metric, update, nil
}

func (s *Service) ListCanarySamples(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.CanarySample, error) {
	return s.store.ListCanarySamples(ctx, ownerID, promotionID, limit)
}

// RecordRunOutcome is the trusted Runtime completion path. Control runs and
// opted-out runs are intentionally ignored; only manifests assigned to the
// currently active candidate cohort become Canary samples.
func (s *Service) RecordRunOutcome(ctx context.Context, ownerID, manifestID string, outcome RunOutcome) (*entity.CanaryMetric, *entity.Promotion, error) {
	manifest, err := s.store.FindRunManifest(ctx, ownerID, strings.TrimSpace(manifestID))
	if err != nil {
		return nil, nil, err
	}
	if manifest == nil {
		return nil, nil, apierror.ErrNotFound.WithMessage("run manifest not found")
	}
	promotion, err := s.store.FindCanaryPromotion(ctx, ownerID, manifest.AgentID)
	if err != nil || promotion == nil || promotion.BuildID != manifest.AgentBuildID || manifest.ExposureID == "" {
		return nil, nil, err
	}
	return s.RecordCanarySample(ctx, ownerID, promotion.PromotionID, CanarySampleRequest{
		ManifestID: manifest.ManifestID, Succeeded: outcome.Succeeded, LatencyMS: outcome.LatencyMS,
		CostMicros: outcome.CostMicros, SafetyScore: outcome.SafetyScore, Intervention: outcome.Intervention,
	})
}

func (s *Service) ListCanaryMetrics(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.CanaryMetric, error) {
	return s.store.ListCanaryMetrics(ctx, ownerID, promotionID, limit)
}

func (s *Service) Exposure(ctx context.Context, ownerID, agentID string) (*entity.Exposure, error) {
	promotion, err := s.store.FindCanaryPromotion(ctx, ownerID, agentID)
	if err != nil || promotion == nil {
		return nil, err
	}
	exposure, err := s.store.FindExposure(ctx, promotion.PromotionID, ownerID, agentID)
	if err != nil || exposure != nil {
		return exposure, err
	}
	optedOut, _, err := s.store.FindLatestExposurePreference(ctx, ownerID, agentID)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.Exposure.preference")
	}
	bucket, variant := deploymentv1.AssignVariant(ownerID, agentID, promotion.PromotionID, promotion.CanaryPercent, optedOut)
	exposure = &entity.Exposure{ExposureID: ulid.New(), PromotionID: promotion.PromotionID, OwnerID: ownerID, AgentID: agentID, Bucket: bucket, Variant: variant, OptedOut: optedOut, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateExposure(ctx, *exposure); err != nil {
		if existing, findErr := s.store.FindExposure(ctx, promotion.PromotionID, ownerID, agentID); findErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	return exposure, nil
}

func (s *Service) SetExperimentOptOut(ctx context.Context, ownerID, agentID string, optedOut bool) error {
	promotion, err := s.store.FindCanaryPromotion(ctx, ownerID, agentID)
	if err != nil {
		return err
	}
	if promotion == nil {
		return apierror.ErrBadRequest.WithMessage("agent has no active canary experiment")
	}
	exposure, err := s.store.FindExposure(ctx, promotion.PromotionID, ownerID, agentID)
	if err != nil {
		return err
	}
	bucket, variant := deploymentv1.AssignVariant(ownerID, agentID, promotion.PromotionID, promotion.CanaryPercent, optedOut)
	if exposure == nil {
		exposure = &entity.Exposure{
			ExposureID: ulid.New(), PromotionID: promotion.PromotionID, OwnerID: ownerID,
			AgentID: agentID, Bucket: bucket, Variant: variant, OptedOut: optedOut, CreatedAt: time.Now().UTC(),
		}
		if err := s.store.CreateExposure(ctx, *exposure); err != nil {
			return log.WrapError(err, "DeploymentService.SetExperimentOptOut.create")
		}
		return nil
	}
	if err := s.store.SetExposurePreference(ctx, ownerID, agentID, optedOut, variant); err != nil {
		return log.WrapError(err, "DeploymentService.SetExperimentOptOut.update")
	}
	return nil
}

func (s *Service) Rollback(ctx context.Context, ownerID, actorID, promotionID string, request RollbackRequest) (*entity.Rollback, []entity.Compensation, error) {
	promotion, err := s.store.FindPromotion(ctx, ownerID, promotionID)
	if err != nil {
		return nil, nil, err
	}
	if promotion == nil {
		return nil, nil, apierror.ErrNotFound.WithMessage("promotion not found")
	}
	if promotion.Status != entity.StatusActive && promotion.Status != entity.StatusCanary && promotion.Status != entity.StatusPaused {
		return nil, nil, apierror.ErrBadRequest.WithMessage("rollback requires an ACTIVE, CANARY, or PAUSED promotion")
	}
	if promotion.PreviousBuildID == "" {
		return nil, nil, apierror.ErrBadRequest.WithMessage("promotion has no previous build")
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = promotion.Revision
	}
	previous, err := s.store.FindPromotionForBuild(ctx, ownerID, promotion.AgentID, promotion.PreviousBuildID)
	if err != nil {
		return nil, nil, err
	}
	if previous == nil {
		return nil, nil, apierror.ErrBadRequest.WithMessage("previous build promotion is unavailable")
	}
	now := time.Now().UTC()
	rollback := entity.Rollback{RollbackID: ulid.New(), PromotionID: promotionID, OwnerID: ownerID, AgentID: promotion.AgentID, FromBuildID: promotion.BuildID, ToBuildID: promotion.PreviousBuildID, Reason: strings.TrimSpace(request.Reason), RequestedBy: actorID, CreatedAt: now}
	if rollback.Reason == "" {
		rollback.Reason = "manual rollback"
	}
	compensations := make([]entity.Compensation, 0, len(request.Compensations))
	for _, value := range request.Compensations {
		if strings.TrimSpace(value.ActionID) == "" || strings.TrimSpace(value.Instructions) == "" {
			return nil, nil, apierror.ErrBadRequest.WithMessage("compensation action_id and instructions are required")
		}
		compensations = append(compensations, entity.Compensation{CompensationID: ulid.New(), RollbackID: rollback.RollbackID, OwnerID: ownerID, ActionID: value.ActionID, Status: "PENDING", Instructions: value.Instructions, CreatedAt: now})
	}
	promotion.Status = entity.StatusRolledBack
	promotion.Revision = request.ExpectedRevision + 1
	promotion.UpdatedAt = now
	previous.Status = entity.StatusActive
	previous.Revision++
	previous.UpdatedAt = now
	if err := s.store.RollbackPromotion(ctx, *promotion, previous, rollback, compensations, request.ExpectedRevision); err != nil {
		return nil, nil, err
	}
	return &rollback, compensations, nil
}

func (s *Service) CreateRunManifest(ctx context.Context, ownerID string, input RunManifestInput) (*entity.RunManifest, error) {
	active, err := s.store.FindActivePromotion(ctx, ownerID, input.AgentID)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, apierror.ErrBadRequest.WithMessage("agent has no active build")
	}
	selected := active
	var exposure *entity.Exposure
	canary, err := s.store.FindCanaryPromotion(ctx, ownerID, input.AgentID)
	if err != nil {
		return nil, err
	}
	if canary != nil {
		exposure, err = s.Exposure(ctx, ownerID, input.AgentID)
		if err != nil {
			return nil, err
		}
		if exposure != nil && exposure.Variant == entity.VariantCandidate && !exposure.OptedOut {
			selected = canary
		}
	}
	build, err := s.store.FindBuild(ctx, ownerID, selected.BuildID)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, apierror.ErrBadRequest.WithMessage("selected build is missing")
	}
	manifest := entity.RunManifest{
		Schema: entity.Schema, ManifestID: ulid.New(), TaskID: input.TaskID, OwnerID: ownerID, AgentID: input.AgentID,
		AgentBuildID: build.BuildID, ModelConfigVersion: input.ModelConfigVersion,
		CapabilityInstances: unique(input.CapabilityInstances), DeviceID: input.DeviceID, UserScope: ownerID,
		WorldRevision: input.WorldRevision, KnowledgeSnapshot: input.KnowledgeSnapshot,
		Budget: input.Budget, FeatureFlags: input.FeatureFlags, BuildChecksum: build.Checksum, CreatedAt: time.Now().UTC(),
	}
	if exposure != nil {
		manifest.ExposureID = exposure.ExposureID
	}
	if err := manifest.Validate(*build); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateRunManifest(ctx, manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (s *Service) EnsureBaselineBuild(ctx context.Context, ownerID, actorID, agentID, promptFingerprint string) (*entity.AgentBuild, error) {
	active, err := s.store.FindActivePromotion(ctx, ownerID, agentID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return s.store.FindBuild(ctx, ownerID, active.BuildID)
	}
	request := defaults(CreateBuildRequest{AgentID: agentID, Version: "baseline-v1", RiskLevel: entity.RiskR1, PromptTemplateVersions: map[string]string{"system": firstNonEmpty(promptFingerprint, "current")}})
	build, err := makeBuild(ownerID, actorID, traceID(ctx), request, nil)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateBuild(ctx, *build); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	promotion := entity.Promotion{Schema: entity.Schema, PromotionID: ulid.New(), OwnerID: ownerID, AgentID: agentID, BuildID: build.BuildID, Status: entity.StatusActive, RiskLevel: build.RiskLevel, Thresholds: defaultThresholds(), Verified: true, Recoverable: true, ApprovedBy: actorID, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := promotion.Validate(); err != nil {
		return nil, err
	}
	if err := s.store.CreatePromotion(ctx, promotion); err != nil {
		return nil, err
	}
	return build, nil
}

func (s *Service) ListRunManifests(ctx context.Context, ownerID, agentID string, limit int) ([]entity.RunManifest, error) {
	return s.store.ListRunManifests(ctx, ownerID, agentID, limit)
}

func (s *Service) ListRollbacks(ctx context.Context, ownerID, agentID string, limit int) ([]entity.Rollback, error) {
	return s.store.ListRollbacks(ctx, ownerID, agentID, limit)
}

func makeBuild(ownerID, actorID, trace string, request CreateBuildRequest, approvals []entity.ArtifactApprovalReference) (*entity.AgentBuild, error) {
	now := time.Now().UTC()
	buildID := ulid.New()
	if strings.TrimSpace(trace) == "" {
		trace = "build-" + buildID
	}
	approvals = append([]entity.ArtifactApprovalReference(nil), approvals...)
	sort.Slice(approvals, func(i, j int) bool {
		if approvals[i].Kind == approvals[j].Kind {
			return approvals[i].ArtifactID < approvals[j].ArtifactID
		}
		return approvals[i].Kind < approvals[j].Kind
	})
	build := &entity.AgentBuild{
		Schema: entity.Schema, BuildID: buildID, OwnerID: ownerID, AgentID: strings.TrimSpace(request.AgentID), Version: strings.TrimSpace(request.Version),
		KernelVersion: request.KernelVersion, PlannerVersion: request.PlannerVersion, PolicyVersion: request.PolicyVersion,
		ProtocolVersion: request.ProtocolVersion, SkillVersions: copyMap(request.SkillVersions), StrategyVersions: copyMap(request.StrategyVersions),
		DelegationPolicyVersions: copyMap(request.DelegationPolicyVersions), SpecialistProfileVersions: copyMap(request.SpecialistProfileVersions),
		OntologyVersion: request.OntologyVersion, PromptTemplateVersions: copyMap(request.PromptTemplateVersions),
		EvaluationSuiteVersion: request.EvaluationSuiteVersion, ArtifactApprovals: approvals,
		RiskLevel: strings.ToUpper(strings.TrimSpace(request.RiskLevel)), Verified: true, Recoverable: true, TraceID: trace,
		CreatedBy: actorID, CreatedAt: now,
	}
	checksum, err := build.ComputeChecksum()
	if err != nil {
		return nil, err
	}
	build.Checksum = checksum
	if err := build.Validate(); err != nil {
		return nil, err
	}
	return build, nil
}

func defaults(request CreateBuildRequest) CreateBuildRequest {
	if request.KernelVersion == "" {
		request.KernelVersion = defaultKernelVersion
	}
	if request.PlannerVersion == "" {
		request.PlannerVersion = defaultPlannerVersion
	}
	if request.PolicyVersion == "" {
		request.PolicyVersion = defaultPolicyVersion
	}
	if request.ProtocolVersion == "" {
		request.ProtocolVersion = defaultProtocolVersion
	}
	if request.OntologyVersion == "" {
		request.OntologyVersion = defaultOntologyVersion
	}
	if request.EvaluationSuiteVersion == "" {
		request.EvaluationSuiteVersion = defaultEvaluationSuiteVersion
	}
	if request.RiskLevel == "" {
		request.RiskLevel = entity.RiskR1
	}
	if request.Version == "" {
		request.Version = "0.5.0"
	}
	if len(request.PromptTemplateVersions) == 0 {
		request.PromptTemplateVersions = map[string]string{"system": "prompt-v1"}
	}
	return request
}

func defaultThresholds() entity.CanaryThresholds {
	return entity.CanaryThresholds{MinimumSuccessRate: 0.9, MaximumP95LatencyMS: 15000, MaximumAverageCostMicros: 100000, MinimumSafetyScore: 1, MaximumInterventionRate: 0.1, MinimumSamples: 20}
}

func copyMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func defaultShadowBudget(value entity.RunBudget) entity.RunBudget {
	if value.MaxTokens <= 0 {
		value.MaxTokens = 4000
	}
	if value.MaxCostMicros <= 0 {
		value.MaxCostMicros = 100000
	}
	if value.MaxDurationMS <= 0 {
		value.MaxDurationMS = 30000
	}
	if value.MaxActions <= 0 {
		value.MaxActions = 32
	}
	return value
}

func shadowSummary(control, candidate ShadowPlan, passed bool) string {
	status := "failed"
	if passed {
		status = "passed"
	}
	return fmt.Sprintf("server-side shadow %s: control=%d planned actions/%d micros, candidate=%d planned actions/%d micros", status, len(control.PlannedActions), control.EstimatedCostMicros, len(candidate.PlannedActions), candidate.EstimatedCostMicros)
}

func traceID(ctx context.Context) string {
	return strings.TrimSpace(log.ReqID(ctx))
}

func unique(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for _, value := range input {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
