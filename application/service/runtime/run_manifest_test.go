package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	deploymentsvc "github.com/good-fish-man/agent-runtime-client/application/service/deployment"
	knowledgesvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge"
	knowledgeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	deploymentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	knowledgepo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	deploymentrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/deployment"
	knowledgerepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/knowledge"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	runtimeartifact "github.com/good-fish-man/athena-protocol/draft/runtimeartifact"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

func TestAttachRunManifestPersistsTraceableSecretFreeSnapshot(t *testing.T) {
	dsn := fmt.Sprintf("file:runtime-manifest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&deploymentpo.AgentBuild{}, &deploymentpo.RunManifest{}, &deploymentpo.Promotion{}, &deploymentpo.Exposure{},
		&deploymentpo.ShadowResult{}, &deploymentpo.CanaryMetric{}, &deploymentpo.CanarySample{}, &deploymentpo.Rollback{}, &deploymentpo.Compensation{},
		&learningpo.Skill{}, &learningpo.Strategy{},
	); err != nil {
		t.Fatal(err)
	}
	store := deploymentrepo.NewStore(data.New(db))
	deploymentService := deploymentsvc.NewService(store)
	service := &RuntimeService{}
	service.SetDeploymentService(deploymentService)
	ctx := authctx.WithUserID(context.Background(), "owner-1")
	values := map[string]any{"agent_id": "agent-1", "world_revision": int64(7)}
	err = service.attachRunManifest(ctx, values, "task-1", "device-1", map[string]entity.ModelConfig{
		"default": {Provider: "openai", Name: "gpt-test", APIKey: "must-never-persist", APIBase: "https://example.invalid"},
	}, []entity.CapabilityConfig{{ID: "browser.navigate"}, {ID: "browser.open"}, {ID: "browser.navigate"}}, []entity.KnowledgeBaseConfig{{ID: "kb-1", Name: "private", RetrievalURL: "https://kb.invalid", Token: "knowledge-secret", TopK: 5}}, &entity.RunOptions{MaxTotalTokens: 2048, TimeoutMs: 5000, MaxToolCalls: 8})
	if err != nil {
		t.Fatalf("attachRunManifest() error = %v", err)
	}
	if values["agent_build_id"] == "" || values["run_manifest_id"] == "" || values["model_config_version"] == "" {
		t.Fatalf("runtime context = %+v", values)
	}
	bundle, err := runtimeartifact.Decode(values[runtimeartifact.ContextKey])
	if err != nil {
		t.Fatalf("runtime artifact bundle = %v", err)
	}
	if bundle.BuildID != values["agent_build_id"] || bundle.ManifestID != values["run_manifest_id"] || len(bundle.Skills) != 0 || len(bundle.Strategies) != 0 {
		t.Fatalf("runtime artifact bundle = %+v", bundle)
	}
	capabilityValues, ok := values["capabilities"].([]string)
	if !ok || len(capabilityValues) != 2 || capabilityValues[0] != "browser.navigate" || capabilityValues[1] != "browser.open" {
		t.Fatalf("normalized capabilities = %#v", values["capabilities"])
	}
	manifests, err := deploymentService.ListRunManifests(ctx, "owner-1", "agent-1", 10)
	if err != nil || len(manifests) != 1 {
		t.Fatalf("ListRunManifests() = %+v, %v", manifests, err)
	}
	manifest := manifests[0]
	if manifest.WorldRevision != 7 || manifest.Budget.MaxTokens != 2048 || manifest.Budget.MaxActions != 8 || manifest.AgentBuildID != values["agent_build_id"] {
		t.Fatalf("manifest = %+v", manifest)
	}
	if strings.Contains(fmt.Sprintf("%+v", manifest), "must-never-persist") || strings.Contains(fmt.Sprintf("%+v", manifest), "knowledge-secret") {
		t.Fatal("run manifest persisted a model or knowledge credential")
	}
}

func TestAttachRunManifestRejectsCallerSuppliedArtifactCarrierWithoutDeployment(t *testing.T) {
	service := &RuntimeService{}
	values := map[string]any{
		"agent_id":                 "agent-1",
		runtimeartifact.ContextKey: map[string]any{"schema": runtimeartifact.Schema, "untrusted": true},
	}
	if err := service.attachRunManifest(context.Background(), values, "task-1", "device-1", nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, exists := values[runtimeartifact.ContextKey]; exists {
		t.Fatal("caller-supplied runtime artifact carrier was forwarded")
	}
}

func TestKnowledgeSnapshotIsBoundToRunManifest(t *testing.T) {
	dsn := fmt.Sprintf("file:runtime-knowledge-manifest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&deploymentpo.AgentBuild{}, &deploymentpo.RunManifest{}, &deploymentpo.Promotion{}, &deploymentpo.Exposure{},
		&deploymentpo.ShadowResult{}, &deploymentpo.CanaryMetric{}, &deploymentpo.CanarySample{}, &deploymentpo.Rollback{}, &deploymentpo.Compensation{},
		&learningpo.Skill{}, &learningpo.Strategy{},
		&knowledgepo.Claim{}, &knowledgepo.Evidence{}, &knowledgepo.Contradiction{}, &knowledgepo.Snapshot{}, &knowledgepo.OntologyPack{}, &knowledgepo.OntologyVersion{}, &knowledgepo.OntologyCandidate{}, &knowledgepo.OntologyMigration{},
	); err != nil {
		t.Fatal(err)
	}
	dataHandle := data.New(db)
	deploymentService := deploymentsvc.NewService(deploymentrepo.NewStore(dataHandle))
	knowledgeService := knowledgesvc.NewService(knowledgerepo.NewStore(dataHandle))
	evidence, err := knowledgeService.CreateUserEvidence(context.Background(), "owner-1", knowledgesvc.CreateUserEvidenceRequest{Title: "Confirmed requirement", Excerpt: "An official translation is required", Sensitivity: knowledgev1.SensitivityInternal})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := knowledgeService.CreateClaim(context.Background(), "owner-1", knowledgesvc.CreateClaimRequest{Claim: knowledgeentity.Claim{Subject: "foreign-license-conversion", Predicate: "requires", Value: "official translation", Scope: knowledgev1.ScopeUser, Sensitivity: knowledgev1.SensitivityInternal}, EvidenceRefs: []string{evidence.EvidenceID}}); err != nil {
		t.Fatal(err)
	}
	service := &RuntimeService{}
	service.SetDeploymentService(deploymentService)
	service.SetKnowledgeService(knowledgeService)
	ctx := authctx.WithUserID(context.Background(), "owner-1")
	values := map[string]any{"agent_id": "agent-1"}
	if err := service.injectKnowledge(ctx, values, "foreign license conversion official translation"); err != nil {
		t.Fatal(err)
	}
	snapshotID, _ := values["knowledge_snapshot_id"].(string)
	checksum, _ := values["knowledge_snapshot_checksum"].(string)
	if snapshotID == "" || checksum == "" || values["knowledge_context"] == nil {
		t.Fatalf("knowledge context was not injected: %+v", values)
	}
	if err := service.attachRunManifest(ctx, values, "task-knowledge", "device-1", map[string]entity.ModelConfig{"default": {Provider: "openai", Name: "gpt-test"}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	manifests, err := deploymentService.ListRunManifests(ctx, "owner-1", "agent-1", 10)
	if err != nil || len(manifests) != 1 || manifests[0].KnowledgeSnapshot != checksum {
		t.Fatalf("manifest=%+v error=%v", manifests, err)
	}
	snapshots, err := knowledgeService.ListSnapshots(ctx, "owner-1", 10)
	if err != nil || len(snapshots) != 1 || snapshots[0].SnapshotID != snapshotID || snapshots[0].RunManifestID != manifests[0].ManifestID || snapshots[0].BoundAt == nil {
		t.Fatalf("snapshot=%+v error=%v", snapshots, err)
	}
}

func TestEstimatedRunCostUsesServerSideModelRates(t *testing.T) {
	models := map[string]entity.ModelConfig{
		"default": {
			Name: "gpt-priced",
			ExtraFields: map[string]any{
				"model_id":                    "model-priced",
				inputCostMicrosPerMillionKey:  int64(2_000_000),
				outputCostMicrosPerMillionKey: int64(8_000_000),
			},
		},
	}
	metadata := &entity.ResponseMetadata{ModelUsage: []entity.ModelUsageMetadata{{
		ModelID: "model-priced", Model: "gpt-priced", PromptTokens: 500_000, CompletionTokens: 250_000,
	}}}
	if got, want := estimatedRunCostMicros(metadata, models), int64(3_000_000); got != want {
		t.Fatalf("estimatedRunCostMicros() = %d, want %d", got, want)
	}
}

func TestModelFingerprintExtrasExcludeSecrets(t *testing.T) {
	values := map[string]any{
		"model_id": "model-1", "runtime_mode": "remote", inputCostMicrosPerMillionKey: int64(10),
		"api_key": "must-not-be-fingerprinted", "authorization": "must-not-be-fingerprinted",
	}
	got := modelFingerprintExtras(values)
	if got["model_id"] != "model-1" || got[inputCostMicrosPerMillionKey] != int64(10) {
		t.Fatalf("modelFingerprintExtras() lost allowlisted metadata: %+v", got)
	}
	if _, ok := got["api_key"]; ok {
		t.Fatal("model fingerprint included api_key")
	}
	if _, ok := got["authorization"]; ok {
		t.Fatal("model fingerprint included authorization")
	}
}
