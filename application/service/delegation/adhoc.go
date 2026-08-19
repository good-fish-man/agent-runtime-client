package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	runtimerepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

const (
	ContextSpecialistRoleDescription = "athena.dso.specialist_role_description"
	ContextAdHocSpecialists          = "athena.dso.adhoc_specialists"
	adHocAdmissionPolicyVersion      = "dso-overlay-admission/v1"
	adHocOverlayLifetime             = 30 * time.Minute
	generalReadProfileRef            = "specialist-profile://general-read/v1"
	generalReadPromptRef             = "prompt-artifact://general-read/v1"
)

var ErrAdHocOverlayDenied = errors.New("ad-hoc specialist overlay denied")

type AdHocBuildRequest struct {
	OwnerID               string
	TaskStepRef           string
	DelegatedOutcomeRef   string
	RoleDescription       string
	RequestedCapabilities []string
	RequestedContextScope dso.ContextScope
	ParentCapabilities    []string
	ParentContextScope    dso.ContextScope
	BudgetRequest         dso.BudgetAmount
	Now                   time.Time
}

type AdHocBuildResult struct {
	BaseProfile dso.SpecialistProfile
	Overlay     dso.AdHocSpecialistOverlay
	Admission   dso.OverlayAdmissionDecision
	Spec        dso.SubagentSpec
}

type specialistResolution struct {
	ProfileRef string
	PromptRef  string
	OverlayRef string
	Role       string
	Temporary  bool
	Build      *AdHocBuildResult
}

type AdHocSpecialistBuilder struct{}

func NewAdHocSpecialistBuilder() *AdHocSpecialistBuilder { return &AdHocSpecialistBuilder{} }

func (b *AdHocSpecialistBuilder) Build(request AdHocBuildRequest) (AdHocBuildResult, error) {
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	base := generalReadSpecialistProfile()
	overlay := dso.AdHocSpecialistOverlay{
		Schema: dso.Schema, OverlayID: "overlay-" + ulid.New(), OwnerID: strings.TrimSpace(request.OwnerID),
		BaseProfileRef: base.Reference(), DelegatedOutcomeRef: strings.TrimSpace(request.DelegatedOutcomeRef),
		RoleDescription: strings.TrimSpace(request.RoleDescription), RequestedCapabilities: canonicalSpecialistStrings(request.RequestedCapabilities),
		RequestedContextScope: canonicalContextScope(request.RequestedContextScope), OutputSchemaRef: base.OutputSchemaRef,
		OutputSchemaParameters: map[string]string{"evidence_mode": "required"}, CreatedAt: now, ExpiresAt: now.Add(adHocOverlayLifetime),
	}
	overlay.ContentHash, _ = dso.AdHocSpecialistOverlayContentHash(overlay)
	inputHash, _ := dso.Hash(map[string]any{
		"owner_id": request.OwnerID, "base_profile_hash": base.DefinitionHash, "overlay_hash": overlay.ContentHash,
		"parent_capabilities": canonicalSpecialistStrings(request.ParentCapabilities), "parent_context_scope": canonicalContextScope(request.ParentContextScope),
	})
	decision := dso.OverlayAdmissionDecision{
		Schema: dso.Schema, AdmissionDecisionID: "overlay-admission-" + ulid.New(), OwnerID: overlay.OwnerID,
		OverlayRef: overlay.OverlayID, OverlayContentHash: overlay.ContentHash, BaseProfileRef: overlay.BaseProfileRef,
		BaseProfileHash: base.DefinitionHash, PolicyVersion: adHocAdmissionPolicyVersion, InputHash: inputHash,
		Decision: dso.OverlayAdmissionAllow, AdmittedCapabilities: append([]string(nil), overlay.RequestedCapabilities...),
		AdmittedContextScope: overlay.RequestedContextScope, DecidedAt: now, ExpiresAt: overlay.ExpiresAt,
	}
	validationErr := overlay.Validate(base, dso.OverlayAdmissionContext{
		ParentCapabilities: canonicalSpecialistStrings(request.ParentCapabilities),
		ParentContextScope: canonicalContextScope(request.ParentContextScope), Now: now,
	})
	if validationErr != nil {
		decision.Decision = dso.OverlayAdmissionDeny
		decision.Reasons = []string{validationErr.Error()}
		decision.AdmittedCapabilities = nil
		decision.AdmittedContextScope = dso.ContextScope{}
	}
	if err := decision.Validate(overlay, base); err != nil {
		return AdHocBuildResult{}, fmt.Errorf("validate overlay admission decision: %w", err)
	}
	result := AdHocBuildResult{BaseProfile: base, Overlay: overlay, Admission: decision}
	if validationErr != nil {
		return result, fmt.Errorf("%w: %v", ErrAdHocOverlayDenied, validationErr)
	}
	result.Spec = dso.SubagentSpec{
		SubagentSpecID: "adhoc-spec-" + ulid.New(), TaskStepRef: strings.TrimSpace(request.TaskStepRef), DelegatedOutcomeRef: overlay.DelegatedOutcomeRef,
		Role: overlay.RoleDescription, RequestedCapabilities: append([]string(nil), overlay.RequestedCapabilities...),
		RequestedContextScope: overlay.RequestedContextScope, PermissionCeilingRef: overlay.BaseProfileRef,
		RiskCeiling: base.RiskCeiling, BudgetRequest: request.BudgetRequest, OutputSchemaRef: base.OutputSchemaRef,
		DelegationPolicy: dso.DelegationPolicy{MayDelegate: false, MaxDepth: 0}, CreatedAt: now,
	}
	result.Spec.DefinitionHash = subagentSpecDefinitionHash(result.Spec)
	if err := result.Spec.Validate(); err != nil {
		return AdHocBuildResult{}, fmt.Errorf("validate ad-hoc subagent spec: %w", err)
	}
	return result, nil
}

func (s *ExecutionService) resolveSpecialist(ctx context.Context, ownerID, taskStepID, outcomeID, role, roleDescription, explicitProfile string, requested, parent []string, scope dso.ContextScope, budget dso.BudgetAmount, allowAdHoc bool) (specialistResolution, error) {
	if explicitProfile != "" {
		if profile, ok := reviewedSpecialistProfile(role, explicitProfile); ok {
			return specialistResolution{ProfileRef: profile.Reference(), PromptRef: profile.PromptArtifactRef, Role: profile.Role}, nil
		}
	}
	if s.learning != nil && explicitProfile == "" {
		resolved, err := s.learning.Resolve(ctx, ownerID, dso.LearningCandidateSpecialistProfile, "low", taskStepID)
		if err != nil {
			log.Warnf(ctx, "governed specialist profile resolution failed; using reviewed baseline: %v", err)
		} else if resolved.Candidate != nil && resolved.Candidate.ProfileArtifact != nil {
			artifact := resolved.Candidate.ProfileArtifact
			if role == "" || role == artifact.Role {
				return specialistResolution{ProfileRef: "specialist-profile://" + artifact.ArtifactID + "/" + artifact.Version, PromptRef: artifact.PromptArtifactRef, Role: artifact.Role}, nil
			}
		}
	}
	if profile, ok := reviewedSpecialistProfile(role, ""); ok {
		return specialistResolution{ProfileRef: profile.Reference(), PromptRef: profile.PromptArtifactRef, Role: profile.Role}, nil
	}
	if !allowAdHoc {
		profile, _ := reviewedSpecialistProfile("research_specialist", researchProfileRef)
		return specialistResolution{ProfileRef: profile.Reference(), PromptRef: profile.PromptArtifactRef, Role: profile.Role}, nil
	}
	if s.adHocStore == nil || s.adHocBuilder == nil {
		return specialistResolution{}, fmt.Errorf("no reviewed specialist profile matches role %q and ad-hoc governance is unavailable", role)
	}
	build, buildErr := s.adHocBuilder.Build(AdHocBuildRequest{
		OwnerID: ownerID, TaskStepRef: taskStepID, DelegatedOutcomeRef: outcomeID, RoleDescription: roleDescription,
		RequestedCapabilities: requested, RequestedContextScope: scope,
		ParentCapabilities: parent, ParentContextScope: scope, BudgetRequest: budget, Now: s.now().UTC(),
	})
	if err := s.persistAdHocAdmission(ctx, build); err != nil {
		return specialistResolution{}, err
	}
	if buildErr != nil {
		return specialistResolution{}, buildErr
	}
	return specialistResolution{
		ProfileRef: build.BaseProfile.Reference(), PromptRef: build.BaseProfile.PromptArtifactRef,
		OverlayRef: build.Overlay.OverlayID, Role: build.Overlay.RoleDescription, Temporary: true, Build: &build,
	}, nil
}

func (s *ExecutionService) persistAdHocAdmission(ctx context.Context, build AdHocBuildResult) error {
	overlayContent, err := json.Marshal(build.Overlay)
	if err != nil {
		return err
	}
	admissionContent, err := json.Marshal(build.Admission)
	if err != nil {
		return err
	}
	status := delegationentity.AdHocOverlayAllowed
	if build.Admission.Decision == dso.OverlayAdmissionDeny {
		status = delegationentity.AdHocOverlayDenied
	}
	traceID := log.ReqID(ctx)
	if traceID == "" {
		traceID = "dso-" + ulid.New()
	}
	return s.adHocStore.CreateAdHocAdmission(ctx, delegationentity.AdHocAdmissionBundle{
		Overlay: delegationentity.AdHocOverlay{
			OverlayID: build.Overlay.OverlayID, OwnerID: build.Overlay.OwnerID, BaseProfileRef: build.Overlay.BaseProfileRef,
			ContentHash: build.Overlay.ContentHash, Status: status, Content: string(overlayContent),
			ExpiresAt: build.Overlay.ExpiresAt, CreatedAt: build.Overlay.CreatedAt,
		},
		Admission: delegationentity.OverlayAdmission{
			DecisionID: build.Admission.AdmissionDecisionID, OverlayID: build.Overlay.OverlayID, OwnerID: build.Overlay.OwnerID,
			Decision: build.Admission.Decision, PolicyVersion: build.Admission.PolicyVersion, InputHash: build.Admission.InputHash,
			Content: string(admissionContent), CreatedAt: build.Admission.DecidedAt,
		},
		Event: delegationentity.Event{
			EventID: "event-" + ulid.New(), OwnerID: build.Overlay.OwnerID, AggregateType: "adhoc_overlay",
			AggregateID: build.Overlay.OverlayID, Sequence: 1, Type: "AdHocOverlayAdmissionDecided",
			IdempotencyKey: build.Overlay.OverlayID + ":admission:1", TraceID: traceID,
			CausationID: build.Overlay.DelegatedOutcomeRef, Payload: string(admissionContent), CreatedAt: build.Admission.DecidedAt,
		},
	})
}

func emitTemporarySpecialistProgress(emit runtimerepo.StreamFunc, traceID, goalID string, resolution specialistResolution, at time.Time) error {
	if emit == nil || !resolution.Temporary || resolution.Build == nil {
		return nil
	}
	return emit(&runtimeentity.StreamEvent{
		Type: runtimeentity.StreamTypeProgress, EmittedAt: at, TraceID: traceID,
		Progress: &controlentity.Progress{
			Protocol: controlentity.Protocol, Type: controlentity.TypeProgress, TaskID: goalID,
			ActionID: resolution.Build.Overlay.OverlayID, TraceID: traceID, Sequence: 1, Revision: 1,
			Capability: "specialist.resolve", Stage: "temporary_specialist_admitted", Message: "Temporary specialist admitted",
			Progress: 5, State: map[string]any{
				"temporary_specialist": true, "role": resolution.Build.Overlay.RoleDescription,
				"overlay_id": resolution.Build.Overlay.OverlayID, "overlay_hash": resolution.Build.Overlay.ContentHash,
				"base_profile_ref": resolution.Build.Overlay.BaseProfileRef, "admission_decision_id": resolution.Build.Admission.AdmissionDecisionID,
			}, SentAt: at,
		},
	})
}

func (s *ExecutionService) recordAdHocOutcome(ctx context.Context, resolution specialistResolution, ownerID, runID string, verification delegationentity.VerificationResult, runErr error, at time.Time) error {
	if !resolution.Temporary || resolution.Build == nil {
		return nil
	}
	status := delegationentity.AdHocOutcomeFailed
	if runErr == nil && verification.Status == delegationentity.VerificationSatisfied {
		status = delegationentity.AdHocOutcomeSuccess
	}
	outcome := delegationentity.AdHocRunOutcome{
		OutcomeID: "adhoc-outcome-" + ulid.New(), OverlayID: resolution.Build.Overlay.OverlayID,
		OwnerID: ownerID, RunID: runID, Status: status, EvidenceRefs: verification.EvidenceRefs, CreatedAt: at,
	}
	if err := s.adHocStore.RecordAdHocOutcome(ctx, outcome); err != nil {
		return err
	}
	if status != delegationentity.AdHocOutcomeSuccess {
		return nil
	}
	_, err := maybeCreateProfileCandidate(ctx, s.adHocStore, resolution.Build.Overlay, at)
	return err
}

func maybeCreateProfileCandidate(ctx context.Context, store delegationrepo.AdHocStore, overlay dso.AdHocSpecialistOverlay, now time.Time) (*dso.SpecialistProfileCandidate, error) {
	idHash, _ := dso.Hash(map[string]any{"owner_id": overlay.OwnerID, "overlay_hash": overlay.ContentHash})
	candidateID := "profile-candidate-" + idHash[:24]
	if existing, err := store.FindProfileCandidate(ctx, overlay.OwnerID, candidateID); err != nil {
		return nil, err
	} else if existing != nil {
		var candidate dso.SpecialistProfileCandidate
		if err := json.Unmarshal([]byte(existing.Content), &candidate); err != nil {
			return nil, fmt.Errorf("decode existing profile candidate: %w", err)
		}
		return &candidate, nil
	}
	outcomes, err := store.ListSuccessfulAdHocOutcomes(ctx, overlay.OwnerID, overlay.OverlayID)
	if err != nil {
		return nil, err
	}
	runRefs := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		runRefs = append(runRefs, outcome.RunID)
	}
	runRefs = canonicalSpecialistStrings(runRefs)
	if len(runRefs) < dso.MinimumProfileCandidateRuns {
		return nil, nil
	}
	candidate := dso.SpecialistProfileCandidate{
		Schema: dso.Schema, CandidateID: candidateID, OwnerID: overlay.OwnerID,
		BaseProfileRef: overlay.BaseProfileRef, SourceOverlayRef: overlay.OverlayID, OverlayContentHash: overlay.ContentHash,
		SuccessfulRunRefs: runRefs, Status: dso.ProfileCandidateReviewRequired, ActivationAllowed: false, CreatedAt: now.UTC(),
	}
	candidate.DefinitionHash, _ = dso.SpecialistProfileCandidateDefinitionHash(candidate)
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	content, err := json.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	traceID := log.ReqID(ctx)
	if traceID == "" {
		traceID = "dso-" + ulid.New()
	}
	if err := store.CreateProfileCandidate(ctx, delegationentity.ProfileCandidate{
		CandidateID: candidate.CandidateID, OwnerID: candidate.OwnerID, OverlayID: overlay.OverlayID,
		BaseProfileRef: candidate.BaseProfileRef, ContentHash: candidate.DefinitionHash, Status: delegationentity.ProfileReviewNeeded,
		Content: string(content), CreatedAt: candidate.CreatedAt,
	}, delegationentity.Event{
		EventID: "event-" + ulid.New(), OwnerID: candidate.OwnerID, AggregateType: "specialist_profile_candidate",
		AggregateID: candidate.CandidateID, Sequence: 1, Type: "SpecialistProfileCandidateCreated",
		IdempotencyKey: candidate.CandidateID + ":created:1", TraceID: traceID, CausationID: overlay.OverlayID,
		Payload: string(content), CreatedAt: candidate.CreatedAt,
	}); err != nil {
		return nil, err
	}
	return &candidate, nil
}

func reviewedSpecialistProfile(role, explicitRef string) (dso.SpecialistProfile, bool) {
	role, explicitRef = strings.TrimSpace(role), strings.TrimSpace(explicitRef)
	for _, profile := range reviewedSpecialistProfiles() {
		if explicitRef != "" {
			if profile.Reference() == explicitRef && (role == "" || profile.Role == role) {
				return profile, true
			}
			continue
		}
		if profile.Role == role {
			return profile, true
		}
	}
	return dso.SpecialistProfile{}, false
}

func reviewedSpecialistProfiles() []dso.SpecialistProfile {
	definitions := []struct{ id, role, prompt string }{
		{"research", "research_specialist", researchPromptRef},
		{"research-parallel", "general_research_specialist", parallelPromptPrefix + "generic/v1"},
		{"research-official", "official_source_specialist", parallelPromptPrefix + "official/v1"},
		{"research-independent", "independent_source_specialist", parallelPromptPrefix + "independent/v1"},
		{"research-recency", "recency_specialist", parallelPromptPrefix + "recency/v1"},
		{"evidence-synthesis", "evidence_specialist", parallelPromptPrefix + "evidence/v1"},
	}
	result := make([]dso.SpecialistProfile, 0, len(definitions)+1)
	for _, definition := range definitions {
		result = append(result, reviewedProfile(definition.id, definition.role, definition.prompt))
	}
	result = append(result, generalReadSpecialistProfile())
	return result
}

func generalReadSpecialistProfile() dso.SpecialistProfile {
	return reviewedProfile("general-read", "general_read_specialist", generalReadPromptRef)
}

func reviewedProfile(id, role, prompt string) dso.SpecialistProfile {
	profile := dso.SpecialistProfile{
		Schema: dso.Schema, ProfileID: id, Version: "v1", Role: role,
		Capabilities: []string{"browser.observe", "browser.read", "browser.search", "filesystem.list", "filesystem.read", "filesystem.search", "github.search", "internet.fetch", "internet.search"},
		ContextScope: dso.ContextScope{AllowedClasses: []string{dso.ClassInternal, dso.ClassPublic}, MaxBytes: defaultContextBytes},
		RiskCeiling:  "low", PromptArtifactRef: prompt, OutputSchemaRef: candidateSchemaRef,
		ReviewedBy: "athena-security-baseline", ReviewedAt: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), ProductionApproved: true,
	}
	profile.DefinitionHash, _ = dso.SpecialistProfileDefinitionHash(profile)
	return profile
}

func canonicalSpecialistStrings(values []string) []string {
	values = uniqueStrings(values)
	sort.Strings(values)
	return values
}

func canonicalContextScope(value dso.ContextScope) dso.ContextScope {
	value.AllowedClasses = canonicalSpecialistStrings(value.AllowedClasses)
	value.ContentRefs = canonicalSpecialistStrings(value.ContentRefs)
	return value
}
