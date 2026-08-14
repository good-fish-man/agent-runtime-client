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
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	deploymentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	deploymentrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/deployment"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
)

func TestAttachRunManifestPersistsTraceableSecretFreeSnapshot(t *testing.T) {
	dsn := fmt.Sprintf("file:runtime-manifest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&deploymentpo.AgentBuild{}, &deploymentpo.RunManifest{}, &deploymentpo.Promotion{}, &deploymentpo.Exposure{},
		&deploymentpo.ShadowResult{}, &deploymentpo.CanaryMetric{}, &deploymentpo.Rollback{}, &deploymentpo.Compensation{},
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
	}, []entity.CapabilityConfig{{ID: "browser.navigate"}}, []entity.KnowledgeBaseConfig{{ID: "kb-1", Name: "private", RetrievalURL: "https://kb.invalid", Token: "knowledge-secret", TopK: 5}}, &entity.RunOptions{MaxTotalTokens: 2048, TimeoutMs: 5000, MaxToolCalls: 8})
	if err != nil {
		t.Fatalf("attachRunManifest() error = %v", err)
	}
	if values["agent_build_id"] == "" || values["run_manifest_id"] == "" {
		t.Fatalf("runtime context = %+v", values)
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
