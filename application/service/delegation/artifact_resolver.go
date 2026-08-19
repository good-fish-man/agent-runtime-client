package delegation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

const (
	researchProfileRef = "specialist-profile://research/v1"
	researchPromptRef  = "prompt-artifact://research-specialist/v1"
	candidateSchemaRef = "schema://athena.dso.typed-candidate-result/v0alpha"
)

type ArtifactResolveInput struct {
	OwnerID              string
	RunID                string
	ParentRunManifestID  string
	SubagentSpecID       string
	DelegatedOutcomeID   string
	ActorBindingID       string
	DeviceID             string
	EnvironmentRef       string
	RuntimeBuildRef      string
	AdmittedCapabilities []string
	Context              ContextBundle
	Model                runtimeentity.ModelConfig
	Now                  time.Time
}

type ResolvedArtifacts struct {
	ContextBundle  ContextBundle
	CapabilityView dso.CapabilityView
	ActorBinding   dso.ActorBinding
	Manifest       dso.InvocationManifest
	Records        delegationentity.InvocationBundle
}

type ArtifactResolver struct{}

func NewArtifactResolver() *ArtifactResolver { return &ArtifactResolver{} }

func (r *ArtifactResolver) Resolve(input ArtifactResolveInput) (ResolvedArtifacts, error) {
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	input.Now = input.Now.UTC()
	if strings.TrimSpace(input.ParentRunManifestID) == "" {
		return ResolvedArtifacts{}, fmt.Errorf("parent run manifest is required")
	}
	capabilityView := dso.CapabilityView{
		CapabilityViewID: "capability-" + input.RunID, SubagentRunRef: input.RunID,
		Capabilities: append([]string(nil), input.AdmittedCapabilities...), RiskCeiling: "low",
		ExpiresAt: input.Now.Add(10 * time.Minute),
	}
	capabilityHashInput := capabilityView
	capabilityHashInput.ContentHash = ""
	capabilityView.ContentHash, _ = dso.Hash(capabilityHashInput)
	if err := capabilityView.Validate(); err != nil {
		return ResolvedArtifacts{}, fmt.Errorf("validate capability view: %w", err)
	}

	device := strings.TrimSpace(input.DeviceID)
	if device == "" {
		device = "server-runtime"
	}
	environment := strings.TrimSpace(input.EnvironmentRef)
	if environment == "" {
		environment = "server"
	}
	actor := dso.ActorBinding{
		ActorBindingID: input.ActorBindingID, DeviceRef: device, EnvironmentRef: environment,
		ValidUntil: input.Now.Add(10 * time.Minute),
	}
	if err := actor.Validate(input.Now); err != nil {
		return ResolvedArtifacts{}, fmt.Errorf("validate actor binding: %w", err)
	}

	modelParameters := map[string]any{
		"provider": input.Model.Provider, "name": input.Model.Name, "api_base": input.Model.APIBase,
		"temperature": input.Model.Temperature, "max_tokens": input.Model.MaxTokens, "top_p": input.Model.TopP,
	}
	modelParametersHash, err := dso.Hash(modelParameters)
	if err != nil {
		return ResolvedArtifacts{}, err
	}
	modelRef := strings.Trim(strings.TrimSpace(input.Model.Provider)+"/"+strings.TrimSpace(input.Model.Name), "/")
	if modelRef == "" {
		return ResolvedArtifacts{}, fmt.Errorf("specialist model is required")
	}
	modelBuildRef := input.Model.Name
	if value, ok := input.Model.ExtraFields["model_build_ref"].(string); ok && strings.TrimSpace(value) != "" {
		modelBuildRef = strings.TrimSpace(value)
	}
	runtimeBuild := strings.TrimSpace(input.RuntimeBuildRef)
	if runtimeBuild == "" {
		runtimeBuild = "agent-runtime/current"
	}
	manifest := dso.InvocationManifest{
		InvocationManifestID: "manifest-" + input.RunID, ParentRunManifestRef: input.ParentRunManifestID,
		SubagentSpecRef: input.SubagentSpecID, DelegatedOutcomeRef: input.DelegatedOutcomeID,
		SpecialistProfileRef: researchProfileRef, PromptArtifactRef: researchPromptRef,
		ContextSliceRef: input.Context.Slice.ContextSliceID, ContextHash: input.Context.Slice.ContentHash,
		ModelRef: modelRef, ModelBuildRef: modelBuildRef, ModelParametersHash: modelParametersHash,
		OutputSchemaRef: candidateSchemaRef, CapabilityViewRef: capabilityView.CapabilityViewID,
		RuntimeBuildRef: runtimeBuild, CreatedAt: input.Now,
	}
	if strings.TrimSpace(input.Model.APIKey) != "" {
		manifest.SecretHandleRefs = []string{"credential://model/" + sanitizeRef(modelRef)}
	}
	manifestHashInput := manifest
	manifestHashInput.ContentHash = ""
	manifest.ContentHash, _ = dso.Hash(manifestHashInput)
	if err := manifest.Validate(); err != nil {
		return ResolvedArtifacts{}, fmt.Errorf("validate invocation manifest: %w", err)
	}

	contextContent, err := json.Marshal(input.Context)
	if err != nil {
		return ResolvedArtifacts{}, err
	}
	capabilityContent, _ := json.Marshal(capabilityView)
	actorContent, _ := json.Marshal(actor)
	actorHash, _ := dso.Hash(actor)
	manifestContent, _ := json.Marshal(manifest)
	records := delegationentity.InvocationBundle{
		ContextSlice:   delegationentity.ImmutableRecord{ID: input.Context.Slice.ContextSliceID, OwnerID: input.OwnerID, RunID: input.RunID, ContentHash: input.Context.Slice.ContentHash, Content: string(contextContent), CreatedAt: input.Now},
		CapabilityView: delegationentity.ImmutableRecord{ID: capabilityView.CapabilityViewID, OwnerID: input.OwnerID, RunID: input.RunID, ContentHash: capabilityView.ContentHash, Content: string(capabilityContent), CreatedAt: input.Now},
		ActorBinding:   delegationentity.ImmutableRecord{ID: actor.ActorBindingID, OwnerID: input.OwnerID, RunID: input.RunID, ContentHash: actorHash, Content: string(actorContent), CreatedAt: input.Now},
		Manifest:       delegationentity.ImmutableRecord{ID: manifest.InvocationManifestID, OwnerID: input.OwnerID, RunID: input.RunID, ContentHash: manifest.ContentHash, Content: string(manifestContent), CreatedAt: input.Now},
	}
	return ResolvedArtifacts{ContextBundle: input.Context, CapabilityView: capabilityView, ActorBinding: actor, Manifest: manifest, Records: records}, nil
}

func sanitizeRef(value string) string {
	value = strings.NewReplacer("/", "-", " ", "-", ":", "-").Replace(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	return value
}
