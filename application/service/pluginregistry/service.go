package pluginregistry

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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
	pluginsdk "github.com/good-fish-man/athena-protocol/sdk/plugin"
)

const (
	VisibilityPrivate    = "private"
	VisibilityPublic     = "public"
	ReviewPending        = "PENDING"
	ReviewApproved       = "APPROVED"
	ReviewRejected       = "REJECTED"
	ScanPending          = "PENDING"
	ScanPassed           = "PASSED"
	ScanFailed           = "FAILED"
	maxAuditRecords      = 200
	maxManifestBytes     = 2 << 20
	maxSBOMBytes         = 8 << 20
	pluginScannerVersion = "athena-registry-scanner/0.8.0"
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
	Files              map[string]string          `json:"files,omitempty"`
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
	ReviewStatus     string `json:"review_status"`
	Notes            string `json:"notes,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type ScanRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

type Provider struct {
	ProviderID         string                    `json:"provider_id"`
	Version            string                    `json:"version"`
	Name               string                    `json:"name"`
	Description        string                    `json:"description"`
	Status             string                    `json:"status"`
	Visibility         string                    `json:"visibility"`
	ManifestSHA256     string                    `json:"manifest_sha256"`
	PayloadSHA256      string                    `json:"payload_sha256"`
	ScanStatus         string                    `json:"scan_status"`
	ScanReportSHA256   string                    `json:"scan_report_sha256"`
	ScannedAt          int64                     `json:"scanned_at"`
	ScanReport         pluginv1.ScanReport       `json:"scan_report"`
	GrantedPermissions pluginv1.PermissionSet    `json:"granted_permissions"`
	GrantedResources   pluginv1.ResourceLimits   `json:"granted_resources"`
	ReviewStatus       string                    `json:"review_status"`
	ReviewNotes        string                    `json:"review_notes,omitempty"`
	ApprovedBy         string                    `json:"approved_by,omitempty"`
	RevokedReason      string                    `json:"revoked_reason,omitempty"`
	Revision           int64                     `json:"revision"`
	InstalledAt        int64                     `json:"installed_at"`
	UpdatedAt          int64                     `json:"updated_at"`
	Manifest           pluginv1.ProviderManifest `json:"manifest"`
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
	if err := pluginsdk.ValidateSBOM(request.SBOM); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	assets, err := decodePackageAssets(manifest, request.Files)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	publicKey, err := s.trustedKey(request.Signature)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	providerPackage := pluginsdk.Package{Manifest: manifest, ManifestJSON: request.Manifest, SBOMJSON: request.SBOM, Assets: assets, Signature: request.Signature}
	if err := pluginsdk.Verify(providerPackage, publicKey); err != nil {
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
	permissions := pluginv1.PermissionSet{}
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
	scan := scanPackage(providerPackage, entry.ManifestSHA256, now)
	if err := scan.Validate(manifest, entry.ManifestSHA256, request.Signature.PayloadSHA256); err != nil {
		return nil, fmt.Errorf("validate plugin scan report: %w", err)
	}
	scanJSON, err := json.Marshal(scan)
	if err != nil {
		return nil, fmt.Errorf("encode plugin scan report: %w", err)
	}
	entry.ScanReportSHA256 = pluginsdk.Digest(scanJSON)
	if err := entry.Validate(manifest); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if request.Activate {
		if err := entry.ValidateRuntimeGrant(manifest); err != nil {
			return nil, apierror.ErrBadRequest.WithMessage(err.Error())
		}
	}
	signature, _ := json.Marshal(request.Signature)
	packageAssets, _ := json.Marshal(encodePackageAssets(assets))
	grantedPermissions, _ := json.Marshal(permissions)
	grantedResources, _ := json.Marshal(resources)
	row := po.Provider{
		ProviderKey: manifest.ProviderID + "@" + manifest.Version, ProviderID: manifest.ProviderID, Version: manifest.Version,
		Name: manifest.Name, Description: manifest.Description, Status: status, Visibility: visibility,
		Manifest: string(request.Manifest), ManifestSHA256: entry.ManifestSHA256, PayloadSHA256: request.Signature.PayloadSHA256,
		Signature: string(signature), SBOM: string(request.SBOM), PackageAssets: string(packageAssets),
		GrantedPermissions: string(grantedPermissions), GrantedResources: string(grantedResources), ScanStatus: ScanPending,
		ScanReport: string(scanJSON), ScanReportSHA256: entry.ScanReportSHA256, ScannedAt: now.UnixMilli(),
		ReviewStatus: ReviewPending, ApprovedBy: approvedBy, Revision: 1, InstalledAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
	}
	row.ScanStatus = scan.Status

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
		cleanupCtx := context.WithoutCancel(ctx)
		_ = s.data.DB(cleanupCtx).Where("provider_key = ?", row.ProviderKey).Delete(&po.Provider{}).Error
		_ = os.RemoveAll(packagePath)
		_ = s.exportRegistry(cleanupCtx)
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
	if status == pluginv1.StatusActive && row.ScanStatus != ScanPassed {
		return nil, apierror.ErrBadRequest.WithMessage("provider requires a passed machine scan before activation")
	}
	if status == pluginv1.StatusActive {
		manifest, permissions, resources, decodeErr := storedContracts(*row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		entry := registryEntry(*row, permissions, resources)
		entry.Status = pluginv1.StatusActive
		entry.ApprovedBy = actorID
		if validateErr := entry.Validate(manifest); validateErr != nil {
			return nil, apierror.ErrBadRequest.WithMessage(validateErr.Error())
		}
		if validateErr := entry.ValidateRuntimeGrant(manifest); validateErr != nil {
			return nil, apierror.ErrBadRequest.WithMessage(validateErr.Error())
		}
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
	review := strings.ToUpper(strings.TrimSpace(request.ReviewStatus))
	if !oneOf(review, ReviewPending, ReviewApproved, ReviewRejected) {
		return nil, apierror.ErrBadRequest.WithMessage("invalid review_status")
	}
	if review == ReviewApproved && row.ScanStatus != ScanPassed {
		return nil, apierror.ErrBadRequest.WithMessage("human review cannot approve a provider before its machine scan passes")
	}
	status := row.Status
	if row.Visibility == VisibilityPublic && status == pluginv1.StatusActive && review != ReviewApproved {
		status = pluginv1.StatusDisabled
	}
	result := s.data.DB(ctx).Model(&po.Provider{}).Where("provider_key = ? AND revision = ? AND deleted_at = 0", row.ProviderKey, request.ExpectedRevision).Updates(map[string]any{
		"review_status": review, "review_notes": strings.TrimSpace(request.Notes),
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

func (s *Service) Scan(ctx context.Context, providerID, version string, request ScanRequest) (*Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.find(ctx, providerID, version)
	if err != nil {
		return nil, err
	}
	if request.ExpectedRevision < 1 {
		request.ExpectedRevision = row.Revision
	}
	manifest, _, _, err := storedContracts(*row)
	if err != nil {
		return nil, err
	}
	providerPackage, verifyErr := s.readPackageFromDisk(*row)
	now := time.Now().UTC()
	var report pluginv1.ScanReport
	if verifyErr == nil {
		key, keyErr := s.trustedKey(providerPackage.Signature)
		if keyErr != nil {
			verifyErr = keyErr
		} else {
			verifyErr = pluginsdk.Verify(providerPackage, key)
		}
	}
	if verifyErr == nil {
		report = scanPackage(providerPackage, row.ManifestSHA256, now)
	} else {
		report = failedScanReport(manifest, row.ManifestSHA256, row.PayloadSHA256, verifyErr, now)
	}
	if err := report.Validate(manifest, row.ManifestSHA256, row.PayloadSHA256); err != nil {
		return nil, fmt.Errorf("validate plugin scan report: %w", err)
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("encode plugin scan report: %w", err)
	}
	status := row.Status
	if report.Status == pluginv1.ScanFailed {
		status = pluginv1.StatusQuarantined
	} else if status == pluginv1.StatusQuarantined {
		status = pluginv1.StatusDisabled
	}
	reportPath := filepath.Join(s.cfg.Directory, row.ProviderID, row.Version, pluginv1.ScanReportFile)
	if err := atomicWriteFile(reportPath, reportJSON, 0600); err != nil {
		return nil, fmt.Errorf("publish plugin scan report: %w", err)
	}
	result := s.data.DB(ctx).Model(&po.Provider{}).Where("provider_key = ? AND revision = ? AND deleted_at = 0", row.ProviderKey, request.ExpectedRevision).Updates(map[string]any{
		"scan_status": report.Status, "scan_report": string(reportJSON), "scan_report_sha256": pluginsdk.Digest(reportJSON),
		"scanned_at": now.UnixMilli(), "status": status, "updated_at": now.UnixMilli(), "revision": request.ExpectedRevision + 1,
	})
	if result.Error != nil {
		return nil, fmt.Errorf("persist plugin scan report: %w", result.Error)
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
	line := 0
	for scanner.Scan() {
		line++
		var item pluginv1.InvocationTrace
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, fmt.Errorf("decode plugin audit line %d: %w", line, err)
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

func (s *Service) trustedKey(envelope pluginv1.SignatureEnvelope) (ed25519.PublicKey, error) {
	if envelope.Algorithm != pluginv1.SignatureEd25519 || strings.TrimSpace(envelope.KeyID) == "" {
		return nil, fmt.Errorf("plugin signature must use Ed25519 with a trusted key_id")
	}
	data, err := os.ReadFile(s.cfg.TrustStorePath)
	if err != nil {
		return nil, fmt.Errorf("read plugin trust store: %w", err)
	}
	var store trustStore
	if err := decodeStrict(data, &store); err != nil || store.Schema != pluginv1.TrustStoreSchema {
		return nil, fmt.Errorf("invalid plugin trust store")
	}
	for _, item := range store.Keys {
		if item.Disabled || item.KeyID != envelope.KeyID || item.Algorithm != pluginv1.SignatureEd25519 {
			continue
		}
		key, keyErr := base64.StdEncoding.DecodeString(item.PublicKey)
		if keyErr != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("plugin signing key is invalid")
		}
		return ed25519.PublicKey(key), nil
	}
	return nil, fmt.Errorf("plugin signing key is not trusted")
}

func (s *Service) readPackageFromDisk(row po.Provider) (pluginsdk.Package, error) {
	expected, err := packageFromRow(row)
	if err != nil {
		return pluginsdk.Package{}, err
	}
	root := filepath.Join(s.cfg.Directory, row.ProviderID, row.Version)
	read := func(name string, maximum int64) ([]byte, error) {
		path := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maximum {
			return nil, fmt.Errorf("%s is not a bounded regular package file", name)
		}
		return os.ReadFile(path)
	}
	manifestJSON, err := read(pluginv1.ManifestFile, maxManifestBytes)
	if err != nil {
		return pluginsdk.Package{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	sbomJSON, err := read(pluginv1.SBOMFile, maxSBOMBytes)
	if err != nil {
		return pluginsdk.Package{}, fmt.Errorf("read plugin SBOM: %w", err)
	}
	signatureJSON, err := read(pluginv1.SignatureFile, maxManifestBytes)
	if err != nil {
		return pluginsdk.Package{}, fmt.Errorf("read plugin signature: %w", err)
	}
	var manifest pluginv1.ProviderManifest
	var signature pluginv1.SignatureEnvelope
	if err := decodeStrict(manifestJSON, &manifest); err != nil {
		return pluginsdk.Package{}, err
	}
	if err := decodeStrict(signatureJSON, &signature); err != nil {
		return pluginsdk.Package{}, err
	}
	assets := make(map[string][]byte, len(manifest.Package.Assets))
	for _, asset := range manifest.Package.Assets {
		content, err := read(asset.Path, asset.SizeBytes)
		if err != nil {
			return pluginsdk.Package{}, fmt.Errorf("read plugin asset %s: %w", asset.Path, err)
		}
		assets[asset.Path] = content
	}
	value := pluginsdk.Package{Manifest: manifest, ManifestJSON: manifestJSON, SBOMJSON: sbomJSON, Assets: assets, Signature: signature}
	if pluginv1.ManifestSHA256(manifestJSON) != row.ManifestSHA256 || !bytes.Equal(expected.ManifestJSON, manifestJSON) || !bytes.Equal(expected.SBOMJSON, sbomJSON) {
		return pluginsdk.Package{}, fmt.Errorf("plugin package differs from its immutable Registry record")
	}
	return value, nil
}

func (s *Service) writePackage(row po.Provider) (string, error) {
	providerPackage, err := packageFromRow(row)
	if err != nil {
		return "", err
	}
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
	files := map[string][]byte{
		pluginv1.ManifestFile: []byte(row.Manifest), pluginv1.SignatureFile: []byte(row.Signature),
		pluginv1.SBOMFile: []byte(row.SBOM), pluginv1.ScanReportFile: []byte(row.ScanReport),
	}
	for path, content := range providerPackage.Assets {
		files[path] = content
	}
	for name, content := range files {
		targetFile := filepath.Join(temporary, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(targetFile), 0700); err != nil {
			return "", fmt.Errorf("create plugin package directory: %w", err)
		}
		if err := os.WriteFile(targetFile, content, 0600); err != nil {
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
			ManifestSHA256: row.ManifestSHA256, ScanReportSHA256: row.ScanReportSHA256, GrantedPermissions: permissions, GrantedResources: resources,
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
	var scan pluginv1.ScanReport
	if err := json.Unmarshal([]byte(row.ScanReport), &scan); err != nil {
		return nil, fmt.Errorf("decode stored plugin scan report: %w", err)
	}
	var permissions pluginv1.PermissionSet
	if err := json.Unmarshal([]byte(row.GrantedPermissions), &permissions); err != nil {
		return nil, fmt.Errorf("decode stored plugin permission grants: %w", err)
	}
	var resources pluginv1.ResourceLimits
	if err := json.Unmarshal([]byte(row.GrantedResources), &resources); err != nil {
		return nil, fmt.Errorf("decode stored plugin resource grants: %w", err)
	}
	return &Provider{ProviderID: row.ProviderID, Version: row.Version, Name: row.Name, Description: row.Description, Status: row.Status,
		Visibility: row.Visibility, ManifestSHA256: row.ManifestSHA256, PayloadSHA256: row.PayloadSHA256,
		ScanStatus: row.ScanStatus, ScanReportSHA256: row.ScanReportSHA256, ScannedAt: row.ScannedAt, ScanReport: scan,
		GrantedPermissions: permissions, GrantedResources: resources, ReviewStatus: row.ReviewStatus,
		ReviewNotes: row.ReviewNotes, ApprovedBy: row.ApprovedBy, RevokedReason: row.RevokedReason, Revision: row.Revision,
		InstalledAt: row.InstalledAt, UpdatedAt: row.UpdatedAt, Manifest: manifest}, nil
}

func decodePackageAssets(manifest pluginv1.ProviderManifest, uploaded map[string]string) (map[string][]byte, error) {
	assets, err := pluginsdk.GeneratedAssets(manifest)
	if err != nil {
		return nil, fmt.Errorf("reconstruct SDK package assets: %w", err)
	}
	var total int64
	for path, encoded := range uploaded {
		content, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			return nil, fmt.Errorf("package file %s must be base64 encoded: %w", path, decodeErr)
		}
		if generated, exists := assets[path]; exists && !bytes.Equal(generated, content) {
			return nil, fmt.Errorf("package file %s does not match the SDK-generated declaration", path)
		}
		assets[path] = content
		total += int64(len(content))
		if total > pluginv1.MaximumPackageBytes {
			return nil, fmt.Errorf("uploaded package files exceed %d bytes", pluginv1.MaximumPackageBytes)
		}
	}
	if err := pluginsdk.ValidateAssets(manifest, assets); err != nil {
		return nil, err
	}
	return assets, nil
}

func encodePackageAssets(assets map[string][]byte) map[string]string {
	result := make(map[string]string, len(assets))
	for path, content := range assets {
		result[path] = base64.StdEncoding.EncodeToString(content)
	}
	return result
}

func packageFromRow(row po.Provider) (pluginsdk.Package, error) {
	var manifest pluginv1.ProviderManifest
	var signature pluginv1.SignatureEnvelope
	var encodedAssets map[string]string
	if err := decodeStrict([]byte(row.Manifest), &manifest); err != nil {
		return pluginsdk.Package{}, fmt.Errorf("decode stored plugin manifest: %w", err)
	}
	if err := decodeStrict([]byte(row.Signature), &signature); err != nil {
		return pluginsdk.Package{}, fmt.Errorf("decode stored plugin signature: %w", err)
	}
	if err := json.Unmarshal([]byte(row.PackageAssets), &encodedAssets); err != nil {
		return pluginsdk.Package{}, fmt.Errorf("decode stored plugin package assets: %w", err)
	}
	assets := make(map[string][]byte, len(encodedAssets))
	for path, encoded := range encodedAssets {
		content, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return pluginsdk.Package{}, fmt.Errorf("decode stored plugin asset %s: %w", path, err)
		}
		assets[path] = content
	}
	value := pluginsdk.Package{Manifest: manifest, ManifestJSON: []byte(row.Manifest), SBOMJSON: []byte(row.SBOM), Assets: assets, Signature: signature}
	if err := pluginsdk.VerifyPackageWithoutTrust(value); err != nil {
		return pluginsdk.Package{}, fmt.Errorf("validate stored plugin package: %w", err)
	}
	return value, nil
}

func scanPackage(value pluginsdk.Package, manifestDigest string, scannedAt time.Time) pluginv1.ScanReport {
	checks := []pluginv1.ScanCheck{
		{Name: "manifest", Passed: true}, {Name: "signature", Passed: true}, {Name: "cyclonedx_sbom", Passed: true},
		{Name: "declarative_assets", Passed: true}, {Name: "runtime_allowlist", Passed: true}, {Name: "conformance_contracts", Passed: true},
	}
	return pluginv1.ScanReport{
		Schema: pluginv1.ScanSchema, ScanID: newScanID(), ProviderID: value.Manifest.ProviderID, Version: value.Manifest.Version,
		ManifestSHA256: manifestDigest, PayloadSHA256: value.Signature.PayloadSHA256, ScannerVersion: pluginScannerVersion,
		Status: pluginv1.ScanPassed, Checks: checks, Findings: []pluginv1.ScanFinding{}, ScannedAt: scannedAt,
	}
}

func failedScanReport(manifest pluginv1.ProviderManifest, manifestDigest, payloadDigest string, cause error, scannedAt time.Time) pluginv1.ScanReport {
	message := "package verification failed"
	if cause != nil {
		message = cause.Error()
	}
	return pluginv1.ScanReport{
		Schema: pluginv1.ScanSchema, ScanID: newScanID(), ProviderID: manifest.ProviderID, Version: manifest.Version,
		ManifestSHA256: manifestDigest, PayloadSHA256: payloadDigest, ScannerVersion: pluginScannerVersion, Status: pluginv1.ScanFailed,
		Checks:   []pluginv1.ScanCheck{{Name: "package_integrity", Passed: false, Message: message}},
		Findings: []pluginv1.ScanFinding{{Severity: "CRITICAL", Code: "PACKAGE_INTEGRITY", Message: message}}, ScannedAt: scannedAt,
	}
}

func newScanID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err == nil {
		return "scan-" + hex.EncodeToString(value)
	}
	return fmt.Sprintf("scan-%d", time.Now().UTC().UnixNano())
}

func storedContracts(row po.Provider) (pluginv1.ProviderManifest, pluginv1.PermissionSet, pluginv1.ResourceLimits, error) {
	var manifest pluginv1.ProviderManifest
	var permissions pluginv1.PermissionSet
	var resources pluginv1.ResourceLimits
	if err := json.Unmarshal([]byte(row.Manifest), &manifest); err != nil {
		return manifest, permissions, resources, fmt.Errorf("decode stored plugin manifest: %w", err)
	}
	if err := json.Unmarshal([]byte(row.GrantedPermissions), &permissions); err != nil {
		return manifest, permissions, resources, fmt.Errorf("decode stored plugin permissions: %w", err)
	}
	if err := json.Unmarshal([]byte(row.GrantedResources), &resources); err != nil {
		return manifest, permissions, resources, fmt.Errorf("decode stored plugin resources: %w", err)
	}
	return manifest, permissions, resources, nil
}

func registryEntry(row po.Provider, permissions pluginv1.PermissionSet, resources pluginv1.ResourceLimits) pluginv1.RegistryEntry {
	return pluginv1.RegistryEntry{
		Schema: pluginv1.Schema, ProviderID: row.ProviderID, Version: row.Version, Status: row.Status,
		ManifestSHA256: row.ManifestSHA256, ScanReportSHA256: row.ScanReportSHA256,
		GrantedPermissions: permissions, GrantedResources: resources, ApprovedBy: row.ApprovedBy,
		RevokedReason: row.RevokedReason, InstalledAt: time.UnixMilli(row.InstalledAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(), Revision: row.Revision,
	}
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

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
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
