package pluginregistry

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/good-fish-man/agent-runtime-client/config"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/plugin"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
)

const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
	ReviewPending     = "PENDING"
	ReviewApproved    = "APPROVED"
	ReviewRejected    = "REJECTED"
	ScanPending       = "PENDING"
	ScanPassed        = "PASSED"
	ScanFailed        = "FAILED"
	maxAuditRecords   = 200
	maxManifestBytes  = 2 << 20
	maxSBOMBytes      = 8 << 20
)

type Service struct {
	data           *data.Data
	cfg            config.PluginsConfig
	mu             sync.Mutex
	runtimeBaseURL string
	httpClient     *http.Client
}

type InstallRequest struct {
	Manifest           json.RawMessage            `json:"manifest"`
	Signature          pluginv1.SignatureEnvelope `json:"signature"`
	SBOM               json.RawMessage            `json:"sbom"`
	GrantedPermissions *pluginv1.PermissionSet    `json:"granted_permissions,omitempty"`
	GrantedResources   *pluginv1.ResourceLimits   `json:"granted_resources,omitempty"`
	Visibility         string                     `json:"visibility"`
	Activate           bool                       `json:"activate"`
}

type StatusRequest struct {
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type ReviewRequest struct {
	ScanStatus       string `json:"scan_status"`
	ReviewStatus     string `json:"review_status"`
	Notes            string `json:"notes,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type Provider struct {
	ProviderID     string                    `json:"provider_id"`
	Version        string                    `json:"version"`
	Name           string                    `json:"name"`
	Description    string                    `json:"description"`
	Status         string                    `json:"status"`
	Visibility     string                    `json:"visibility"`
	ManifestSHA256 string                    `json:"manifest_sha256"`
	ScanStatus     string                    `json:"scan_status"`
	ReviewStatus   string                    `json:"review_status"`
	ReviewNotes    string                    `json:"review_notes,omitempty"`
	ApprovedBy     string                    `json:"approved_by,omitempty"`
	RevokedReason  string                    `json:"revoked_reason,omitempty"`
	Revision       int64                     `json:"revision"`
	InstalledAt    int64                     `json:"installed_at"`
	UpdatedAt      int64                     `json:"updated_at"`
	Manifest       pluginv1.ProviderManifest `json:"manifest"`
}

type trustStore struct {
	Schema string     `json:"schema"`
	Keys   []trustKey `json:"keys"`
}

type trustKey struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
	Disabled  bool   `json:"disabled"`
}

func NewService(d *data.Data, cfg config.PluginsConfig) *Service {
	return &Service{data: d, cfg: cfg, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

func (s *Service) WithRuntime(baseURL string) *Service {
	s.runtimeBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return s
}

func (s *Service) Reload(ctx context.Context) (map[string]any, error) {
	if s.runtimeBaseURL == "" {
		return nil, apierror.ErrRuntimeUnavailable.WithMessage("agent-runtime HTTP address is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.runtimeBaseURL+"/admin/plugins/reload", nil)
	if err != nil {
		return nil, fmt.Errorf("build plugin reload request: %w", err)
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, apierror.ErrRuntimeUnavailable.WithMessage("plugin registry saved, but Runtime reload failed: " + err.Error())
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read plugin reload response: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode plugin reload response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, apierror.ErrRuntimeUnavailable.WithMessage(fmt.Sprint(payload["message"]))
	}
	return payload, nil
}

func (s *Service) Install(ctx context.Context, actorID string, request InstallRequest) (*Provider, error) {
	if s == nil || s.data == nil {
		return nil, fmt.Errorf("plugin registry service is not configured")
	}
	if len(request.Manifest) == 0 || len(request.Manifest) > maxManifestBytes {
		return nil, apierror.ErrBadRequest.WithMessage("plugin manifest must be between 1 byte and 2 MiB")
	}
	if len(request.SBOM) == 0 || len(request.SBOM) > maxSBOMBytes {
		return nil, apierror.ErrBadRequest.WithMessage("plugin SBOM must be between 1 byte and 8 MiB")
	}
	var manifest pluginv1.ProviderManifest
	if err := decodeStrict(request.Manifest, &manifest); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage("invalid plugin manifest: " + err.Error())
	}
	if err := manifest.Validate(); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if !json.Valid(request.SBOM) || len(bytes.TrimSpace(request.SBOM)) == 0 {
		return nil, apierror.ErrBadRequest.WithMessage("sbom must be a JSON document")
	}
	if err := s.verifySignature(request.Signature, request.Manifest, request.SBOM); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	visibility := strings.ToLower(strings.TrimSpace(request.Visibility))
	if visibility == "" {
		visibility = VisibilityPrivate
	}
	if visibility != VisibilityPrivate && visibility != VisibilityPublic {
		return nil, apierror.ErrBadRequest.WithMessage("visibility must be private or public")
	}
	if visibility == VisibilityPublic && request.Activate {
		return nil, apierror.ErrBadRequest.WithMessage("public providers require a passed scan and approved review before activation")
	}
	permissions := manifest.Permissions
	if request.GrantedPermissions != nil {
		permissions = *request.GrantedPermissions
	}
	resources := manifest.Resources
	if request.GrantedResources != nil {
		resources = *request.GrantedResources
	}
	now := time.Now().UTC()
	status := pluginv1.StatusInstalled
	approvedBy := ""
	if request.Activate {
		status = pluginv1.StatusActive
		approvedBy = actorID
	}
	entry := pluginv1.RegistryEntry{
		Schema: pluginv1.Schema, ProviderID: manifest.ProviderID, Version: manifest.Version, Status: status,
		ManifestSHA256: pluginv1.ManifestSHA256(request.Manifest), GrantedPermissions: permissions,
		GrantedResources: resources, ApprovedBy: approvedBy, InstalledAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := entry.Validate(manifest); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	signature, _ := json.Marshal(request.Signature)
	grantedPermissions, _ := json.Marshal(permissions)
	grantedResources, _ := json.Marshal(resources)
	row := po.Provider{
		ProviderKey: manifest.ProviderID + "@" + manifest.Version, ProviderID: manifest.ProviderID, Version: manifest.Version,
		Name: manifest.Name, Description: manifest.Description, Status: status, Visibility: visibility,
		Manifest: string(request.Manifest), ManifestSHA256: entry.ManifestSHA256, Signature: string(signature), SBOM: string(request.SBOM),
		GrantedPermissions: string(grantedPermissions), GrantedResources: string(grantedResources), ScanStatus: ScanPending,
		ReviewStatus: ReviewPending, ApprovedBy: approvedBy, Revision: 1, InstalledAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	if err := s.data.DB(ctx).Model(&po.Provider{}).Where("provider_key = ? AND deleted_at = 0", row.ProviderKey).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("check plugin version: %w", err)
	}
	if count != 0 {
		return nil, apierror.ErrConflict.WithMessage("provider version is immutable; publish a new version")
	}
	packagePath, err := s.writePackage(row)
	if err != nil {
		return nil, err
	}
	if err := s.data.DB(ctx).Create(&row).Error; err != nil {
		_ = os.RemoveAll(packagePath)
		return nil, fmt.Errorf("persist plugin provider: %w", err)
	}
	if err := s.exportRegistry(ctx); err != nil {
		_ = s.data.DB(context.Background()).Where("provider_key = ?", row.ProviderKey).Delete(&po.Provider{}).Error
		_ = os.RemoveAll(packagePath)
		_ = s.exportRegistry(context.Background())
		return nil, err
	}
	return providerView(row)
}

func (s *Service) List(ctx context.Context) ([]Provider, error) {
	var rows []po.Provider
	if err := s.data.DB(ctx).Where("deleted_at = 0").Order("provider_id asc, version desc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list plugin providers: %w", err)
	}
	result := make([]Provider, 0, len(rows))
	for _, row := range rows {
		value, err := providerView(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *value)
	}
	return result, nil
}

func (s *Service) Transition(ctx context.Context, actorID, providerID, version string, request StatusRequest) (*Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.find(ctx, providerID, version)
	if err != nil {
		return nil, err
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = row.Revision
	}
	status := strings.ToUpper(strings.TrimSpace(request.Status))
	if status != pluginv1.StatusActive && status != pluginv1.StatusDisabled && status != pluginv1.StatusRevoked {
		return nil, apierror.ErrBadRequest.WithMessage("status must be ACTIVE, DISABLED, or REVOKED")
	}
	if row.Status == pluginv1.StatusRevoked && status != pluginv1.StatusRevoked {
		return nil, apierror.ErrBadRequest.WithMessage("revoked provider versions cannot be reactivated")
	}
	if status == pluginv1.StatusActive && row.Visibility == VisibilityPublic && (row.ScanStatus != ScanPassed || row.ReviewStatus != ReviewApproved) {
		return nil, apierror.ErrBadRequest.WithMessage("public providers require a passed scan and approved review")
	}
	if status == pluginv1.StatusRevoked && strings.TrimSpace(request.Reason) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("revocation reason is required")
	}
	now := time.Now().UTC()
	updates := map[string]any{"status": status, "updated_at": now.UnixMilli(), "revision": request.ExpectedRevision + 1}
	if status == pluginv1.StatusActive {
		updates["approved_by"] = actorID
	}
	if status == pluginv1.StatusRevoked {
		updates["revoked_reason"] = strings.TrimSpace(request.Reason)
	}
	result := s.data.DB(ctx).Model(&po.Provider{}).Where("provider_key = ? AND revision = ? AND deleted_at = 0", row.ProviderKey, request.ExpectedRevision).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("transition plugin provider: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, apierror.ErrConflict.WithMessage("plugin provider changed; refresh and retry")
	}
	if err := s.exportRegistry(ctx); err != nil {
		return nil, err
	}
	return s.findView(ctx, providerID, version)
}

func (s *Service) Review(ctx context.Context, actorID, providerID, version string, request ReviewRequest) (*Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.find(ctx, providerID, version)
	if err != nil {
		return nil, err
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = row.Revision
	}
	scan := strings.ToUpper(strings.TrimSpace(request.ScanStatus))
	review := strings.ToUpper(strings.TrimSpace(request.ReviewStatus))
	if !oneOf(scan, ScanPending, ScanPassed, ScanFailed) || !oneOf(review, ReviewPending, ReviewApproved, ReviewRejected) {
		return nil, apierror.ErrBadRequest.WithMessage("invalid scan_status or review_status")
	}
	status := row.Status
	if row.Visibility == VisibilityPublic && status == pluginv1.StatusActive && (scan != ScanPassed || review != ReviewApproved) {
		status = pluginv1.StatusDisabled
	}
	result := s.data.DB(ctx).Model(&po.Provider{}).Where("provider_key = ? AND revision = ? AND deleted_at = 0", row.ProviderKey, request.ExpectedRevision).Updates(map[string]any{
		"scan_status": scan, "review_status": review, "review_notes": strings.TrimSpace(request.Notes),
		"approved_by": actorID, "status": status, "updated_at": time.Now().UTC().UnixMilli(), "revision": request.ExpectedRevision + 1,
	})
	if result.Error != nil {
		return nil, fmt.Errorf("review plugin provider: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, apierror.ErrConflict.WithMessage("plugin provider changed; refresh and retry")
	}
	if err := s.exportRegistry(ctx); err != nil {
		return nil, err
	}
	return s.findView(ctx, providerID, version)
}

func (s *Service) Audit(ctx context.Context, limit int) ([]pluginv1.InvocationTrace, error) {
	if limit < 1 || limit > maxAuditRecords {
		limit = maxAuditRecords
	}
	file, err := os.Open(s.cfg.AuditPath)
	if errors.Is(err, os.ErrNotExist) {
		return []pluginv1.InvocationTrace{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open plugin audit: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	items := make([]pluginv1.InvocationTrace, 0, limit)
	for scanner.Scan() {
		var item pluginv1.InvocationTrace
		if json.Unmarshal(scanner.Bytes(), &item) != nil {
			continue
		}
		items = append(items, item)
		if len(items) > limit {
			items = items[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read plugin audit: %w", err)
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items, nil
}

func (s *Service) verifySignature(envelope pluginv1.SignatureEnvelope, manifest, sbom []byte) error {
	if envelope.Algorithm != pluginv1.SignatureEd25519 || strings.TrimSpace(envelope.KeyID) == "" {
		return fmt.Errorf("plugin signature must use Ed25519 with a trusted key_id")
	}
	data, err := os.ReadFile(s.cfg.TrustStorePath)
	if err != nil {
		return fmt.Errorf("read plugin trust store: %w", err)
	}
	var store trustStore
	if err := decodeStrict(data, &store); err != nil || store.Schema != "athena.plugin-trust.v1" {
		return fmt.Errorf("invalid plugin trust store")
	}
	for _, item := range store.Keys {
		if item.Disabled || item.KeyID != envelope.KeyID || item.Algorithm != pluginv1.SignatureEd25519 {
			continue
		}
		key, keyErr := base64.StdEncoding.DecodeString(item.PublicKey)
		signature, signatureErr := base64.StdEncoding.DecodeString(envelope.Signature)
		payload := append(append(append([]byte(nil), manifest...), '\n'), sbom...)
		if keyErr != nil || signatureErr != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(key), payload, signature) {
			return fmt.Errorf("plugin signature verification failed")
		}
		return nil
	}
	return fmt.Errorf("plugin signing key is not trusted")
}

func (s *Service) writePackage(row po.Provider) (string, error) {
	parent := filepath.Join(s.cfg.Directory, row.ProviderID)
	target := filepath.Join(parent, row.Version)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", fmt.Errorf("create plugin package root: %w", err)
	}
	if _, err := os.Stat(target); err == nil {
		return "", apierror.ErrConflict.WithMessage("provider package version already exists on disk")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect plugin package: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return "", fmt.Errorf("stage plugin package: %w", err)
	}
	defer os.RemoveAll(temporary)
	for name, content := range map[string]string{pluginv1.ManifestFile: row.Manifest, pluginv1.SignatureFile: row.Signature, pluginv1.SBOMFile: row.SBOM} {
		if err := os.WriteFile(filepath.Join(temporary, name), []byte(content), 0600); err != nil {
			return "", fmt.Errorf("write plugin package %s: %w", name, err)
		}
	}
	if err := os.Rename(temporary, target); err != nil {
		return "", fmt.Errorf("publish plugin package: %w", err)
	}
	return target, nil
}

func (s *Service) exportRegistry(ctx context.Context) error {
	var rows []po.Provider
	if err := s.data.DB(ctx).Where("deleted_at = 0").Order("provider_id asc, version asc").Find(&rows).Error; err != nil {
		return fmt.Errorf("read plugin registry snapshot: %w", err)
	}
	index := pluginv1.RegistryIndex{Schema: pluginv1.Schema, Entries: make([]pluginv1.RegistryEntry, 0, len(rows))}
	for _, row := range rows {
		var permissions pluginv1.PermissionSet
		var resources pluginv1.ResourceLimits
		if err := json.Unmarshal([]byte(row.GrantedPermissions), &permissions); err != nil {
			return fmt.Errorf("decode grants for %s: %w", row.ProviderKey, err)
		}
		if err := json.Unmarshal([]byte(row.GrantedResources), &resources); err != nil {
			return fmt.Errorf("decode resources for %s: %w", row.ProviderKey, err)
		}
		index.Entries = append(index.Entries, pluginv1.RegistryEntry{
			Schema: pluginv1.Schema, ProviderID: row.ProviderID, Version: row.Version, Status: row.Status,
			ManifestSHA256: row.ManifestSHA256, GrantedPermissions: permissions, GrantedResources: resources,
			ApprovedBy: row.ApprovedBy, RevokedReason: row.RevokedReason, InstalledAt: time.UnixMilli(row.InstalledAt).UTC(),
			UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(), Revision: row.Revision,
		})
	}
	return atomicWriteJSON(s.cfg.RegistryPath, index)
}

func (s *Service) find(ctx context.Context, providerID, version string) (*po.Provider, error) {
	var row po.Provider
	if err := s.data.DB(ctx).Where("provider_id = ? AND version = ? AND deleted_at = 0", providerID, version).First(&row).Error; err != nil {
		return nil, apierror.ErrNotFound.WithMessage("plugin provider version not found")
	}
	return &row, nil
}

func (s *Service) findView(ctx context.Context, providerID, version string) (*Provider, error) {
	row, err := s.find(ctx, providerID, version)
	if err != nil {
		return nil, err
	}
	return providerView(*row)
}

func providerView(row po.Provider) (*Provider, error) {
	var manifest pluginv1.ProviderManifest
	if err := json.Unmarshal([]byte(row.Manifest), &manifest); err != nil {
		return nil, fmt.Errorf("decode stored plugin manifest: %w", err)
	}
	return &Provider{ProviderID: row.ProviderID, Version: row.Version, Name: row.Name, Description: row.Description, Status: row.Status,
		Visibility: row.Visibility, ManifestSHA256: row.ManifestSHA256, ScanStatus: row.ScanStatus, ReviewStatus: row.ReviewStatus,
		ReviewNotes: row.ReviewNotes, ApprovedBy: row.ApprovedBy, RevokedReason: row.RevokedReason, Revision: row.Revision,
		InstalledAt: row.InstalledAt, UpdatedAt: row.UpdatedAt, Manifest: manifest}, nil
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin registry: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create plugin registry directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".registry-")
	if err != nil {
		return fmt.Errorf("stage plugin registry: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write plugin registry: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync plugin registry: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close plugin registry: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("publish plugin registry: %w", err)
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
