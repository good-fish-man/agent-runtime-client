package pluginregistry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/good-fish-man/agent-runtime-client/config"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/plugin"
	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
)

func TestSignedPrivateProviderLifecycle(t *testing.T) {
	service, request := testService(t, VisibilityPrivate)
	provider, err := service.Install(context.Background(), "admin", request)
	if err != nil {
		t.Fatal(err)
	}
	if provider.Status != pluginv1.StatusActive || provider.Revision != 1 {
		t.Fatalf("unexpected installed provider: %+v", provider)
	}
	items, err := service.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("list providers: items=%d err=%v", len(items), err)
	}
	disabled, err := service.Transition(context.Background(), "admin", provider.ProviderID, provider.Version, StatusRequest{Status: pluginv1.StatusDisabled, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != pluginv1.StatusDisabled || disabled.Revision != 2 {
		t.Fatalf("unexpected transition: %+v", disabled)
	}
	index := readIndex(t, service.cfg.RegistryPath)
	if len(index.Entries) != 1 || index.Entries[0].Status != pluginv1.StatusDisabled || index.Entries[0].Revision != 2 {
		t.Fatalf("registry snapshot did not follow database state: %+v", index)
	}
	if _, err := os.Stat(filepath.Join(service.cfg.Directory, provider.ProviderID, provider.Version, pluginv1.ManifestFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), "admin", request); err == nil {
		t.Fatal("immutable provider version was overwritten")
	}
}

func TestPublicProviderRequiresScanAndReview(t *testing.T) {
	service, request := testService(t, VisibilityPublic)
	request.Activate = false
	provider, err := service.Install(context.Background(), "admin", request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(context.Background(), "admin", provider.ProviderID, provider.Version, StatusRequest{Status: pluginv1.StatusActive, ExpectedRevision: 1}); err == nil {
		t.Fatal("unreviewed public provider was activated")
	}
	reviewed, err := service.Review(context.Background(), "reviewer", provider.ProviderID, provider.Version, ReviewRequest{ScanStatus: ScanPassed, ReviewStatus: ReviewApproved, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.Transition(context.Background(), "admin", provider.ProviderID, provider.Version, StatusRequest{Status: pluginv1.StatusActive, ExpectedRevision: reviewed.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != pluginv1.StatusActive || active.ApprovedBy != "admin" {
		t.Fatalf("reviewed public provider did not activate: %+v", active)
	}
}

func TestTamperedProviderDoesNotReachDiskOrDatabase(t *testing.T) {
	service, request := testService(t, VisibilityPrivate)
	request.Manifest = append(append(json.RawMessage(nil), request.Manifest...), ' ')
	request.Manifest[len(request.Manifest)-1] = '\n'
	request.Manifest = append(request.Manifest, []byte(` `)...)
	request.SBOM = json.RawMessage(`{"bomFormat":"tampered"}`)
	if _, err := service.Install(context.Background(), "admin", request); err == nil {
		t.Fatal("tampered package was installed")
	}
	var count int64
	if err := service.data.DB(context.Background()).Model(&po.Provider{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tampered package persisted %d rows", count)
	}
}

func testService(t *testing.T, visibility string) (*Service, InstallRequest) {
	t.Helper()
	dsn := "file:" + url.QueryEscape(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.Provider{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := config.PluginsConfig{Directory: filepath.Join(root, "packages"), RegistryPath: filepath.Join(root, "registry.json"), TrustStorePath: filepath.Join(root, "trust-store.json"), AuditPath: filepath.Join(root, "audit.jsonl")}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := trustStore{Schema: "athena.plugin-trust.v1", Keys: []trustKey{{KeyID: "test", Algorithm: pluginv1.SignatureEd25519, PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}}
	writeTestJSON(t, cfg.TrustStorePath, trust)
	manifest := testManifest()
	manifestBytes, _ := json.Marshal(manifest)
	sbom := json.RawMessage(`{"bomFormat":"CycloneDX","specVersion":"1.5"}`)
	signature := ed25519.Sign(privateKey, append(append(append([]byte(nil), manifestBytes...), '\n'), sbom...))
	request := InstallRequest{Manifest: manifestBytes, SBOM: sbom, Visibility: visibility, Activate: true, Signature: pluginv1.SignatureEnvelope{Algorithm: pluginv1.SignatureEd25519, KeyID: "test", Signature: base64.StdEncoding.EncodeToString(signature)}}
	return NewService(data.New(db), cfg), request
}

func testManifest() pluginv1.ProviderManifest {
	capabilityID := "com.example.fixture.read"
	objectSchema := map[string]any{"type": "object", "properties": map[string]any{}}
	return pluginv1.ProviderManifest{
		Schema: pluginv1.Schema, ProviderID: "com.example.fixture", Name: "Fixture", Version: "0.8.0",
		Description: "Signed fixture provider", MinRuntimeVersion: "0.8.0",
		Capabilities: []pluginv1.Capability{{ID: capabilityID, Description: "Read fixture", InputSchema: objectSchema, OutputSchema: objectSchema, ReadOnly: true, Risk: pluginv1.RiskR0, ObservationContract: "fixture.observation.v1"}},
		Platforms:    []pluginv1.Platform{{OS: "any", Arch: "any"}}, RiskFloor: pluginv1.RiskR0,
		Resources:   pluginv1.ResourceLimits{MaxExecutionMS: 1000, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxConcurrency: 1, MaxMemoryMB: 32, MaxCPUMillis: 250},
		HealthCheck: pluginv1.HealthCheck{Operation: capabilityID, TimeoutMS: 500}, Observation: pluginv1.ObservationContract{Schema: objectSchema},
		Runtime: pluginv1.RuntimeSpec{Kind: pluginv1.RuntimeStaticJSON, StaticResponses: map[string]json.RawMessage{capabilityID: json.RawMessage(`{}`)}},
		SBOMRef: pluginv1.SBOMFile, IssuedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
	}
}

func readIndex(t *testing.T, path string) pluginv1.RegistryIndex {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var index pluginv1.RegistryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	return index
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
