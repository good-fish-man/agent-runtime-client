package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

const (
	FaultBeforeReplayPersist  = "before_replay_persist"
	FaultAfterReplayPersist   = "after_replay_persist"
	FaultBeforeLiveExecution  = "before_live_execution"
	FaultAfterLiveExecution   = "after_live_execution"
	FaultBeforeReplayComplete = "before_replay_complete"
)

type ReplayFaultInjector interface {
	Inject(context.Context, string) error
}

type ReplayFaultInjectorFunc func(context.Context, string) error

func (f ReplayFaultInjectorFunc) Inject(ctx context.Context, point string) error {
	return f(ctx, point)
}

type LiveReplayOutcome struct {
	VerificationRefs []string
	DriftReasons     []string
	SideEffects      bool
}

type LiveReplayExecutor interface {
	Reexecute(context.Context, entity.ReplaySource, dso.ReplayRequest) (LiveReplayOutcome, error)
}

type ReplayRunner struct {
	store  repository.RecoveryStore
	live   LiveReplayExecutor
	faults ReplayFaultInjector
	now    func() time.Time
}

func NewReplayRunner(store repository.RecoveryStore, live LiveReplayExecutor) *ReplayRunner {
	return &ReplayRunner{store: store, live: live, now: func() time.Time { return time.Now().UTC() }}
}

func (r *ReplayRunner) WithFaultInjector(injector ReplayFaultInjector) *ReplayRunner {
	if r != nil {
		r.faults = injector
	}
	return r
}

func (r *ReplayRunner) Run(ctx context.Context, request dso.ReplayRequest) (dso.ReplayResult, error) {
	if r == nil || r.store == nil {
		return dso.ReplayResult{}, fmt.Errorf("replay runner is not configured")
	}
	if err := request.Validate(); err != nil {
		return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.validateRequest")
	}
	requestContent, err := json.Marshal(request)
	if err != nil {
		return dso.ReplayResult{}, err
	}
	existing, err := r.store.FindReplay(ctx, request.OwnerID, request.ReplayID)
	if err != nil {
		return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.findExisting")
	}
	if existing != nil {
		if existing.SourceRunID != request.SourceRunRef || existing.SourceManifestID != request.SourceManifestRef || existing.SourceManifestHash != request.SourceManifestHash || existing.Mode != request.Mode || existing.RequestContent != string(requestContent) {
			return dso.ReplayResult{}, repository.ErrIdempotencyConflict
		}
		if existing.Status == dso.ReplayCompleted || existing.Status == dso.ReplayFailed || existing.Status == dso.ReplayCancelled {
			var previous dso.ReplayResult
			if err := json.Unmarshal([]byte(existing.ResultContent), &previous); err != nil {
				return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.decodeExistingResult")
			}
			if err := previous.Validate(); err != nil {
				return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.validateExistingResult")
			}
			if existing.Status == dso.ReplayCompleted {
				return previous, nil
			}
			return previous, fmt.Errorf("previous replay ended with status %s: %s", existing.Status, existing.ErrorRef)
		}
		if request.Mode == dso.ReplayLiveReexecution {
			return dso.ReplayResult{}, fmt.Errorf("live replay %s has an unknown external outcome; reconciliation is required before retry", request.ReplayID)
		}
	}
	source, err := r.store.LoadReplaySource(ctx, request.OwnerID, request.SourceRunRef, request.SourceManifestRef)
	if err != nil {
		return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.loadSource")
	}
	if source == nil {
		return dso.ReplayResult{}, fmt.Errorf("replay source was not found inside the owner boundary")
	}
	bindings, manifest, err := replayArtifactBindings(*source)
	if err != nil {
		return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.bindArtifacts")
	}
	startedAt := r.now().UTC()
	record := entity.ReplayRecord{
		ReplayID: request.ReplayID, OwnerID: request.OwnerID, SourceRunID: request.SourceRunRef,
		SourceManifestID: request.SourceManifestRef, SourceManifestHash: request.SourceManifestHash,
		Mode: request.Mode, Status: dso.ReplayRunning, RequestedBy: request.RequestedBy,
		LiveApprovalRef: request.LiveApprovalRef, RequestContent: string(requestContent),
		CreatedAt: request.CreatedAt.UTC(), StartedAt: startedAt,
	}
	if err := r.inject(ctx, FaultBeforeReplayPersist); err != nil {
		return dso.ReplayResult{}, err
	}
	if err := r.store.CreateReplay(ctx, record, replayEvent(ctx, request.OwnerID, request.ReplayID, "ReplayStarted", 1, request.SourceRunRef, map[string]any{"mode": request.Mode, "manifest_hash": request.SourceManifestHash}, startedAt)); err != nil {
		return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.persist")
	}
	if err := r.inject(ctx, FaultAfterReplayPersist); err != nil {
		return dso.ReplayResult{}, err
	}

	result := dso.ReplayResult{
		Schema: dso.Schema, ReplayID: request.ReplayID, OwnerID: request.OwnerID,
		SourceRunRef: request.SourceRunRef, Mode: request.Mode, Status: dso.ReplayCompleted,
		SourceManifestHash: source.Manifest.ContentHash, ArtifactBindings: bindings,
		ObservationRefs:  append([]string(nil), request.ObservationRefs...),
		VerificationRefs: append([]string(nil), source.VerificationRefs...), StartedAt: startedAt,
	}
	var runErr error
	if source.Manifest.ContentHash != request.SourceManifestHash || manifest.ContentHash != request.SourceManifestHash {
		result.Status = dso.ReplayFailed
		result.DriftReasons = []string{"source_manifest_hash_changed"}
		runErr = fmt.Errorf("source invocation manifest no longer matches the requested hash")
	} else {
		switch request.Mode {
		case dso.ReplayExactConfig:
			// Artifact validation above is the entire exact-config replay. It
			// intentionally invokes no model, capability, device, or browser.
		case dso.ReplayRecordedObservationSimulation:
			if !isStringSubset(request.ObservationRefs, source.ObservationRefs) {
				result.Status = dso.ReplayFailed
				result.DriftReasons = []string{"observation_not_owned_by_source_run"}
				runErr = fmt.Errorf("recorded-observation replay contains an observation outside the source run")
			}
		case dso.ReplayLiveReexecution:
			if r.live == nil {
				result.Status = dso.ReplayFailed
				result.DriftReasons = []string{"live_executor_unavailable"}
				runErr = fmt.Errorf("live replay executor is not configured")
				break
			}
			if err := r.inject(ctx, FaultBeforeLiveExecution); err != nil {
				return dso.ReplayResult{}, err
			}
			outcome, liveErr := r.live.Reexecute(ctx, *source, request)
			if liveErr != nil {
				result.Status = dso.ReplayFailed
				result.DriftReasons = []string{"live_reexecution_failed"}
				runErr = log.WrapError(liveErr, "DelegationReplay.Run.liveReexecution")
			} else {
				result.VerificationRefs = append(result.VerificationRefs, outcome.VerificationRefs...)
				result.DriftReasons = append(result.DriftReasons, outcome.DriftReasons...)
				result.LiveSideEffects = outcome.SideEffects
			}
			if err := r.inject(ctx, FaultAfterLiveExecution); err != nil {
				return dso.ReplayResult{}, err
			}
		}
	}
	result.ArtifactBindings = canonicalReplayArtifactBindings(result.ArtifactBindings)
	result.ObservationRefs = canonicalUnique(result.ObservationRefs)
	result.VerificationRefs = canonicalUnique(result.VerificationRefs)
	result.DriftReasons = canonicalUnique(result.DriftReasons)
	result.EndedAt = r.now().UTC()
	result.ContentHash, err = dso.ReplayResultContentHash(result)
	if err != nil {
		return dso.ReplayResult{}, err
	}
	if validateErr := result.Validate(); validateErr != nil {
		return dso.ReplayResult{}, log.WrapError(validateErr, "DelegationReplay.Run.validateResult")
	}
	resultContent, err := json.Marshal(result)
	if err != nil {
		return dso.ReplayResult{}, err
	}
	if err := r.inject(ctx, FaultBeforeReplayComplete); err != nil {
		return dso.ReplayResult{}, err
	}
	errorRef := ""
	if runErr != nil {
		errorRef = strings.Join(result.DriftReasons, ",")
	}
	if err := r.store.CompleteReplay(ctx, request.OwnerID, request.ReplayID, result.Status, string(resultContent), result.ContentHash, errorRef, result.EndedAt, replayEvent(ctx, request.OwnerID, request.ReplayID, "Replay"+titleStatus(result.Status), 2, request.SourceRunRef, map[string]any{"mode": request.Mode, "result_hash": result.ContentHash, "status": result.Status}, result.EndedAt)); err != nil {
		return dso.ReplayResult{}, log.WrapError(err, "DelegationReplay.Run.complete")
	}
	return result, runErr
}

func replayArtifactBindings(source entity.ReplaySource) ([]dso.ReplayArtifactBinding, dso.InvocationManifest, error) {
	manifest, err := dso.DecodeInvocationManifest([]byte(source.Manifest.Content))
	if err != nil {
		return nil, dso.InvocationManifest{}, err
	}
	if source.Manifest.ContentHash != manifest.ContentHash || source.ContextSlice.ContentHash != manifest.ContextHash || source.Manifest.OwnerID != source.Run.OwnerID || source.ContextSlice.OwnerID != source.Run.OwnerID || source.CapabilityView.OwnerID != source.Run.OwnerID || source.ActorBinding.OwnerID != source.Run.OwnerID {
		return nil, dso.InvocationManifest{}, fmt.Errorf("replay source artifacts do not match the immutable manifest or owner")
	}
	referenceHash := func(value string) string {
		hash, _ := dso.Hash(strings.TrimSpace(value))
		return hash
	}
	bindings := []dso.ReplayArtifactBinding{
		{Kind: "actor_binding", Ref: source.ActorBinding.ID, ContentHash: source.ActorBinding.ContentHash},
		{Kind: "capability_view", Ref: source.CapabilityView.ID, ContentHash: source.CapabilityView.ContentHash},
		{Kind: "context_slice", Ref: source.ContextSlice.ID, ContentHash: source.ContextSlice.ContentHash},
		{Kind: "delegated_outcome", Ref: source.DelegatedOutcome.ID, ContentHash: source.DelegatedOutcome.DefinitionHash},
		{Kind: "invocation_manifest", Ref: source.Manifest.ID, ContentHash: source.Manifest.ContentHash},
		{Kind: "model", Ref: manifest.ModelRef + "@" + manifest.ModelBuildRef, ContentHash: manifest.ModelParametersHash},
		{Kind: "prompt_artifact", Ref: manifest.PromptArtifactRef, ContentHash: referenceHash(manifest.PromptArtifactRef)},
		{Kind: "runtime_build", Ref: manifest.RuntimeBuildRef, ContentHash: referenceHash(manifest.RuntimeBuildRef)},
		{Kind: "specialist_profile", Ref: manifest.SpecialistProfileRef, ContentHash: referenceHash(manifest.SpecialistProfileRef + "|" + manifest.SpecialistOverlayRef)},
		{Kind: "subagent_spec", Ref: source.SubagentSpec.ID, ContentHash: source.SubagentSpec.DefinitionHash},
	}
	return canonicalReplayArtifactBindings(bindings), manifest, nil
}

func (r *ReplayRunner) inject(ctx context.Context, point string) error {
	if r == nil || r.faults == nil {
		return nil
	}
	if err := r.faults.Inject(ctx, point); err != nil {
		return log.WrapError(err, "DelegationReplay.fault."+point)
	}
	return nil
}

func replayEvent(ctx context.Context, ownerID, replayID, eventType string, sequence int64, causationID string, payload any, at time.Time) entity.Event {
	encoded, _ := json.Marshal(payload)
	traceID := log.ReqID(ctx)
	if traceID == "" {
		traceID = "dso-replay-" + ulid.New()
	}
	return entity.Event{EventID: "event-" + ulid.New(), OwnerID: ownerID, AggregateType: "dso_replay", AggregateID: replayID, Sequence: sequence, Type: eventType, IdempotencyKey: replayID + ":" + eventType + fmt.Sprintf(":%d", sequence), TraceID: traceID, CausationID: causationID, Payload: string(encoded), CreatedAt: at}
}

func isStringSubset(values, ceiling []string) bool {
	allowed := make(map[string]struct{}, len(ceiling))
	for _, value := range ceiling {
		allowed[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}

func canonicalUnique(values []string) []string {
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

func canonicalReplayArtifactBindings(values []dso.ReplayArtifactBinding) []dso.ReplayArtifactBinding {
	result := append([]dso.ReplayArtifactBinding(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Ref < result[j].Ref
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

func titleStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

var errInjectedReplayFault = errors.New("injected replay fault")
