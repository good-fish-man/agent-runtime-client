package deployment

import (
	"context"
	"fmt"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/deployment"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
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

type Service struct{ store repository.Store }

func NewService(store repository.Store) *Service { return &Service{store: store} }

type CreateBuildRequest struct {
	AgentID                string            `json:"agent_id"`
	Version                string            `json:"version"`
	KernelVersion          string            `json:"kernel_version"`
	PlannerVersion         string            `json:"planner_version"`
	PolicyVersion          string            `json:"policy_version"`
	ProtocolVersion        string            `json:"protocol_version"`
	SkillVersions          map[string]string `json:"skill_versions,omitempty"`
	StrategyVersions       map[string]string `json:"strategy_versions,omitempty"`
	OntologyVersion        string            `json:"ontology_version"`
	PromptTemplateVersions map[string]string `json:"prompt_template_versions"`
	EvaluationSuiteVersion string            `json:"evaluation_suite_version"`
	RiskLevel              string            `json:"risk_level"`
}

type CreatePromotionRequest struct {
	BuildID       string                  `json:"build_id"`
	CanaryPercent int                     `json:"canary_percent"`
	Thresholds    entity.CanaryThresholds `json:"thresholds"`
	Verified      bool                    `json:"verified"`
	Recoverable   bool                    `json:"recoverable"`
}

type TransitionRequest struct {
	TargetStatus     string `json:"target_status"`
	ExpectedRevision int64  `json:"expected_revision"`
	Explicit         bool   `json:"explicit"`
}

type ShadowRequest struct {
	TaskID                string `json:"task_id"`
	ProductionRouteHash   string `json:"production_route_hash"`
	CandidateRouteHash    string `json:"candidate_route_hash"`
	ProductionGraphHash   string `json:"production_graph_hash"`
	CandidateGraphHash    string `json:"candidate_graph_hash"`
	ProductionActionsHash string `json:"production_actions_hash"`
	CandidateActionsHash  string `json:"candidate_actions_hash"`
	ProductionCostMicros  int64  `json:"production_cost_micros"`
	CandidateCostMicros   int64  `json:"candidate_cost_micros"`
	ProductionRisk        string `json:"production_risk"`
	CandidateRisk         string `json:"candidate_risk"`
	LatencyMS             int64  `json:"latency_ms"`
	Passed                bool   `json:"passed"`
	Summary               string `json:"summary"`
}

type CanaryMetricRequest struct {
	SampleCount       int     `json:"sample_count"`
	SuccessRate       float64 `json:"success_rate"`
	P95LatencyMS      int64   `json:"p95_latency_ms"`
	AverageCostMicros int64   `json:"average_cost_micros"`
	SafetyScore       float64 `json:"safety_score"`
	InterventionRate  float64 `json:"intervention_rate"`
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

func (s *Service) CreateBuild(ctx context.Context, ownerID, actorID string, request CreateBuildRequest) (*entity.AgentBuild, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("deployment service is not configured")
	}
	request = defaults(request)
	allowed, err := s.store.ApprovedArtifactVersions(ctx, ownerID)
	if err != nil {
		return nil, log.WrapError(err, "DeploymentService.CreateBuild.artifacts")
	}
	if err := validateArtifactVersions(request.SkillVersions, allowed.Skills, "skill"); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := validateArtifactVersions(request.StrategyVersions, allowed.Strategies, "strategy"); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	build, err := makeBuild(ownerID, actorID, request)
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
		Verified: request.Verified, Recoverable: request.Recoverable, Revision: 1, CreatedAt: now, UpdatedAt: now,
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
			if result.Passed && result.NoExternalSideEffects && result.ExecutedActionCount == 0 {
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
		if len(metrics) == 0 || metrics[0].SampleCount < promotion.Thresholds.MinimumSamples || metrics[0].StopTriggered {
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

func (s *Service) RecordShadow(ctx context.Context, ownerID, promotionID string, request ShadowRequest) (*entity.ShadowResult, error) {
	promotion, err := s.store.FindPromotion(ctx, ownerID, promotionID)
	if err != nil {
		return nil, err
	}
	if promotion == nil || promotion.Status != entity.StatusShadow {
		return nil, apierror.ErrBadRequest.WithMessage("promotion is not in SHADOW")
	}
	shadow := entity.ShadowResult{
		ShadowID: ulid.New(), PromotionID: promotionID, OwnerID: ownerID, TaskID: request.TaskID,
		ProductionRouteHash: request.ProductionRouteHash, CandidateRouteHash: request.CandidateRouteHash,
		ProductionGraphHash: request.ProductionGraphHash, CandidateGraphHash: request.CandidateGraphHash,
		ProductionActionsHash: request.ProductionActionsHash, CandidateActionsHash: request.CandidateActionsHash,
		ProductionCostMicros: request.ProductionCostMicros, CandidateCostMicros: request.CandidateCostMicros,
		ProductionRisk: request.ProductionRisk, CandidateRisk: request.CandidateRisk, LatencyMS: request.LatencyMS,
		NoExternalSideEffects: true, ExecutedActionCount: 0, Passed: request.Passed,
		Summary: strings.TrimSpace(request.Summary), CreatedAt: time.Now().UTC(),
	}
	if err := shadow.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if err := s.store.CreateShadowResult(ctx, shadow); err != nil {
		return nil, err
	}
	return &shadow, nil
}

func (s *Service) ListShadowResults(ctx context.Context, ownerID, promotionID string, limit int) ([]entity.ShadowResult, error) {
	return s.store.ListShadowResults(ctx, ownerID, promotionID, limit)
}

func (s *Service) RecordCanaryMetric(ctx context.Context, ownerID, promotionID string, request CanaryMetricRequest) (*entity.CanaryMetric, *entity.Promotion, error) {
	promotion, err := s.store.FindPromotion(ctx, ownerID, promotionID)
	if err != nil {
		return nil, nil, err
	}
	if promotion == nil || promotion.Status != entity.StatusCanary {
		return nil, nil, apierror.ErrBadRequest.WithMessage("promotion is not in CANARY")
	}
	metric := entity.CanaryMetric{
		MetricID: ulid.New(), PromotionID: promotionID, OwnerID: ownerID,
		SampleCount: request.SampleCount, SuccessRate: request.SuccessRate, P95LatencyMS: request.P95LatencyMS,
		AverageCostMicros: request.AverageCostMicros, SafetyScore: request.SafetyScore,
		InterventionRate: request.InterventionRate, CreatedAt: time.Now().UTC(),
	}
	stop, reason := promotion.Thresholds.Evaluate(metric)
	metric.StopTriggered, metric.StopReason = stop, reason
	var update *entity.Promotion
	if stop {
		before := promotion.Revision
		promotion.Status = entity.StatusPaused
		promotion.Revision++
		promotion.UpdatedAt = metric.CreatedAt
		update = promotion
		if err := s.store.SaveCanaryMetric(ctx, metric, update, before); err != nil {
			return nil, nil, err
		}
	} else if err := s.store.SaveCanaryMetric(ctx, metric, nil, 0); err != nil {
		return nil, nil, err
	}
	return &metric, update, nil
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
	bucket, variant := deploymentv1.AssignVariant(ownerID, agentID, promotion.PromotionID, promotion.CanaryPercent, false)
	exposure = &entity.Exposure{ExposureID: ulid.New(), PromotionID: promotion.PromotionID, OwnerID: ownerID, AgentID: agentID, Bucket: bucket, Variant: variant, CreatedAt: time.Now().UTC()}
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
	build, err := makeBuild(ownerID, actorID, request)
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

func makeBuild(ownerID, actorID string, request CreateBuildRequest) (*entity.AgentBuild, error) {
	now := time.Now().UTC()
	build := &entity.AgentBuild{
		Schema: entity.Schema, BuildID: ulid.New(), OwnerID: ownerID, AgentID: strings.TrimSpace(request.AgentID), Version: strings.TrimSpace(request.Version),
		KernelVersion: request.KernelVersion, PlannerVersion: request.PlannerVersion, PolicyVersion: request.PolicyVersion,
		ProtocolVersion: request.ProtocolVersion, SkillVersions: copyMap(request.SkillVersions), StrategyVersions: copyMap(request.StrategyVersions),
		OntologyVersion: request.OntologyVersion, PromptTemplateVersions: copyMap(request.PromptTemplateVersions),
		EvaluationSuiteVersion: request.EvaluationSuiteVersion, RiskLevel: strings.ToUpper(strings.TrimSpace(request.RiskLevel)),
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

func validateArtifactVersions(requested, approved map[string]string, kind string) error {
	for id, version := range requested {
		if approved[id] != version {
			return fmt.Errorf("%s %s@%s is not an approved immutable version", kind, id, version)
		}
	}
	return nil
}

func copyMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
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
