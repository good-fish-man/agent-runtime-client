package worldmodel

import (
	"context"
	"fmt"
	"strings"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

type providerStore interface {
	CreateWorldProvider(context.Context, controlentity.WorldProvider) error
	FindWorldProvider(context.Context, string, string) (*controlentity.WorldProvider, error)
	ListWorldProviders(context.Context, string) ([]controlentity.WorldProvider, error)
	UpdateWorldProvider(context.Context, controlentity.WorldProvider, int64) error
	DeleteWorldProvider(context.Context, string, string) error
	RecordWorldProviderHealth(context.Context, string, string, string, string, time.Time) error
}

type externalProvider interface {
	Health(context.Context, controlentity.WorldProvider) error
	Query(context.Context, controlentity.WorldProvider, controlentity.WorldQuery) (*controlentity.WorldSnapshot, []string, error)
}

func (a *Service) CreateProvider(ctx context.Context, ownerID string, request controlentity.WorldProviderRequest) (*controlentity.WorldProvider, error) {
	store, err := a.providerStore()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	value := providerFromRequest(ownerID, controlentity.NewID("world-provider"), request, now)
	if err := a.validateProviderBinding(ctx, &value); err != nil {
		return nil, err
	}
	if err := store.CreateWorldProvider(ctx, value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (a *Service) UpdateProvider(ctx context.Context, ownerID, providerID string, request controlentity.WorldProviderRequest) (*controlentity.WorldProvider, error) {
	store, err := a.providerStore()
	if err != nil {
		return nil, err
	}
	current, err := store.FindWorldProvider(ctx, ownerID, providerID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("world provider not found")
	}
	expected := request.ExpectedRevision
	if expected < 1 {
		expected = current.Revision
	}
	if expected != current.Revision {
		return nil, fmt.Errorf("world provider revision changed")
	}
	updated := providerFromRequest(ownerID, providerID, request, time.Now().UTC())
	updated.CreatedAt = current.CreatedAt
	updated.Revision = expected + 1
	updated.HealthStatus = controlentity.WorldProviderHealthUnknown
	if err := a.validateProviderBinding(ctx, &updated); err != nil {
		return nil, err
	}
	if err := store.UpdateWorldProvider(ctx, updated, expected); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (a *Service) ListProviders(ctx context.Context, ownerID string) ([]controlentity.WorldProvider, error) {
	store, err := a.providerStore()
	if err != nil {
		return nil, err
	}
	return store.ListWorldProviders(ctx, ownerID)
}

func (a *Service) DeleteProvider(ctx context.Context, ownerID, providerID string) error {
	store, err := a.providerStore()
	if err != nil {
		return err
	}
	return store.DeleteWorldProvider(ctx, ownerID, providerID)
}

func (a *Service) TestProvider(ctx context.Context, ownerID, providerID string) (*controlentity.WorldProvider, error) {
	store, err := a.providerStore()
	if err != nil {
		return nil, err
	}
	provider, err := store.FindWorldProvider(ctx, ownerID, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("world provider not found")
	}
	checkedAt := time.Now().UTC()
	healthErr := adapterFor(*provider).Health(ctx, *provider)
	status, message := controlentity.WorldProviderHealthAvailable, "Connection succeeded"
	if healthErr != nil {
		status, message = controlentity.WorldProviderHealthFailed, boundedProviderMessage(healthErr.Error())
	}
	if err := store.RecordWorldProviderHealth(ctx, ownerID, providerID, status, message, checkedAt); err != nil {
		return nil, err
	}
	provider.HealthStatus, provider.HealthMessage, provider.LastCheckedAt, provider.UpdatedAt = status, message, &checkedAt, checkedAt
	return provider, nil
}

func (a *Service) QueryProvider(ctx context.Context, ownerID, providerID string, query controlentity.WorldQuery) (*controlentity.WorldProviderQueryResult, error) {
	store, err := a.providerStore()
	if err != nil {
		return nil, err
	}
	provider, err := store.FindWorldProvider(ctx, ownerID, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("world provider not found")
	}
	if !provider.Enabled {
		return nil, fmt.Errorf("world provider is disabled")
	}
	query.Normalize()
	if err := query.Validate(); err != nil {
		return nil, err
	}
	snapshot, warnings, err := adapterFor(*provider).Query(ctx, *provider, query)
	if err != nil {
		return nil, err
	}
	_, ontology, err := a.resolveOntologyWithPack(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if err := a.validator.ValidateSnapshot(snapshot, ontology); err != nil {
		return nil, fmt.Errorf("external provider returned ontology-incompatible data: %w", err)
	}
	sortWorld(snapshot.Entities, snapshot.Relations, snapshot.Facts)
	snapshot.Schema = SnapshotSchema
	snapshot.TaskID = ""
	snapshot.OntologyPack, snapshot.OntologyVersion, snapshot.OntologyChecksum = provider.OntologyPack, provider.OntologyVersion, ontology.Checksum
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	checksum, err := snapshotChecksum(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.Checksum, snapshot.SnapshotID = checksum, "external-snapshot-"+checksum[:24]
	return &controlentity.WorldProviderQueryResult{ProviderID: provider.ProviderID, ProviderKind: provider.Kind, Authoritative: false, Snapshot: *snapshot, Warnings: warnings}, nil
}

func (a *Service) providerStore() (providerStore, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("world state authority is unavailable")
	}
	store, ok := a.store.(providerStore)
	if !ok {
		return nil, fmt.Errorf("world provider registry is unavailable")
	}
	return store, nil
}

func (a *Service) validateProviderBinding(ctx context.Context, provider *controlentity.WorldProvider) error {
	pack, ontology, err := a.resolveOntologyWithPack(ctx, provider.OwnerID)
	if err != nil {
		return err
	}
	if provider.OntologyPack != pack.PackID || provider.OntologyVersion != ontology.Version {
		return fmt.Errorf("world provider ontology binding %s@%s does not match active %s@%s", provider.OntologyPack, provider.OntologyVersion, pack.PackID, ontology.Version)
	}
	return provider.Validate()
}

func providerFromRequest(ownerID, providerID string, request controlentity.WorldProviderRequest, now time.Time) controlentity.WorldProvider {
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	confidence := request.DefaultConfidence
	if confidence == 0 {
		confidence = 0.75
	}
	ttl := request.TTLSeconds
	if ttl == 0 {
		ttl = 3600
	}
	timeout := request.TimeoutMS
	if timeout == 0 {
		timeout = 5000
	}
	kind := strings.ToUpper(strings.TrimSpace(request.Kind))
	authMode := strings.ToUpper(strings.TrimSpace(request.AuthMode))
	if authMode == "" {
		authMode = controlentity.WorldProviderAuthNone
	}
	return controlentity.WorldProvider{
		ProviderID: providerID, OwnerID: strings.TrimSpace(ownerID), Name: strings.TrimSpace(request.Name), Kind: kind,
		Endpoint: strings.TrimSpace(request.Endpoint), Database: strings.TrimSpace(request.Database), QueryTemplate: strings.TrimSpace(request.QueryTemplate),
		AuthMode: authMode, CredentialEnv: strings.TrimSpace(request.CredentialEnv), AllowPrivateNetwork: request.AllowPrivateNetwork,
		OntologyPack: strings.TrimSpace(request.OntologyPack), OntologyVersion: strings.TrimSpace(request.OntologyVersion), DefaultConfidence: confidence,
		TTLSeconds: ttl, TimeoutMS: timeout, Enabled: enabled, ReadOnly: true, Capabilities: providerCapabilities(kind),
		HealthStatus: controlentity.WorldProviderHealthUnknown, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func providerCapabilities(kind string) []string {
	switch kind {
	case controlentity.WorldProviderSPARQL:
		return []string{"query", "rdf", "owl", "shacl_projection"}
	case controlentity.WorldProviderNeo4j:
		return []string{"query", "property_graph", "cypher"}
	case controlentity.WorldProviderTypeDB:
		return []string{"query", "typed_graph", "typeql"}
	default:
		return []string{"query", "athena_world_snapshot"}
	}
}

func boundedProviderMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
