package worldmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	knowledgeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

func TestValidatorAcceptsTypedGraphAndOperationalState(t *testing.T) {
	ontology := testOntology(t)
	patch := testWorldPatch([]controlentity.WorldMutation{
		{Operation: "set", Path: "/browser/title", Value: "Athena"},
		{Operation: "set", Path: "/entities/agent-1", Value: map[string]any{"type": "Agent"}},
		{Operation: "set", Path: "/entities/device-1", Value: map[string]any{"type": "Device"}},
		{Operation: "set", Path: "/relations/use-1", Value: map[string]any{"predicate": "uses", "source_id": "agent-1", "target_id": "device-1"}},
		{Operation: "set", Path: "/facts/use-1", Value: map[string]any{"subject_type": "Agent", "predicate": "uses", "value_type": "Device"}},
	})
	if err := (Validator{}).Validate(nil, patch, ontology); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidatorRejectsUnknownEntityType(t *testing.T) {
	patch := testWorldPatch([]controlentity.WorldMutation{{
		Operation: "set", Path: "/entities/agent-1", Value: map[string]any{"type": "UnreviewedType"},
	}})
	err := (Validator{}).Validate(nil, patch, testOntology(t))
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("Validate() error = %v, want unknown type", err)
	}
}

func TestValidatorRejectsRelationCardinalityMismatch(t *testing.T) {
	patch := testWorldPatch([]controlentity.WorldMutation{
		{Operation: "set", Path: "/entities/agent-1", Value: map[string]any{"type": "Agent"}},
		{Operation: "set", Path: "/entities/agent-2", Value: map[string]any{"type": "Agent"}},
		{Operation: "set", Path: "/relations/use-1", Value: map[string]any{"predicate": "uses", "source_id": "agent-1", "target_id": "agent-2"}},
	})
	err := (Validator{}).Validate(nil, patch, testOntology(t))
	if err == nil || !strings.Contains(err.Error(), "expects Agent -> Device") {
		t.Fatalf("Validate() error = %v, want relation type mismatch", err)
	}
}

func TestValidatorRejectsTamperedOntology(t *testing.T) {
	ontology := testOntology(t)
	ontology.Checksum = strings.Repeat("0", 64)
	err := (Validator{}).Validate(nil, testWorldPatch([]controlentity.WorldMutation{{Operation: "set", Path: "/entities/a", Value: map[string]any{"type": "Agent"}}}), ontology)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Validate() error = %v, want checksum mismatch", err)
	}
}

func TestAuthorityPinsWildcardRevisionBeforeCommit(t *testing.T) {
	store := &fakeWorldStore{
		task:  &controlentity.TaskSession{TaskID: "task-1", UserID: "owner-1"},
		world: &controlentity.WorldState{TaskID: "task-1", Revision: 4, State: map[string]any{}, UpdatedAt: time.Now().UTC()},
	}
	authority := NewService(store)
	ontology := testOntology(t)
	authority.SetOntologyResolver(fakeOntologyResolver{pack: &knowledgeentity.OntologyPack{PackID: ontology.PackID, OwnerID: "owner-1", Current: ontology.Version}, version: ontology})
	patch := testWorldPatch([]controlentity.WorldMutation{{
		Operation: "set", Path: "/entities/agent-1", Value: map[string]any{"type": "Agent"},
	}})
	observation := controlentity.Observation{TaskID: "task-1", Evidence: patch.Evidence, WorldPatch: &patch}
	if err := authority.CommitObservation(context.Background(), observation); err != nil {
		t.Fatal(err)
	}
	if store.saved == nil || store.saved.WorldPatch == nil || store.saved.WorldPatch.BaseRevision != 4 {
		t.Fatalf("saved observation = %+v", store.saved)
	}
}

func TestAuthorityIssuesOwnerScopedImmutableSnapshot(t *testing.T) {
	updated := time.Date(2026, time.September, 4, 10, 0, 0, 0, time.UTC)
	store := &fakeWorldStore{
		task:  &controlentity.TaskSession{TaskID: "task-1", UserID: "owner-1"},
		world: &controlentity.WorldState{TaskID: "task-1", Revision: 9, State: map[string]any{"browser": map[string]any{"title": "Athena"}}, UpdatedAt: updated},
	}
	ontology := testOntology(t)
	authority := NewService(store)
	authority.SetOntologyResolver(fakeOntologyResolver{pack: &knowledgeentity.OntologyPack{PackID: ontology.PackID, OwnerID: "owner-1", Current: ontology.Version}, version: ontology})
	snapshot, err := authority.Snapshot(context.Background(), "owner-1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != SnapshotSchema || snapshot.Revision != 9 || snapshot.OntologyChecksum != ontology.Checksum || len(snapshot.Checksum) != 64 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, err := authority.Snapshot(context.Background(), "owner-2", "task-1"); err == nil {
		t.Fatal("cross-owner snapshot read was accepted")
	}
}

func TestServiceResolvesEntityIdentityBeforeOntologyValidation(t *testing.T) {
	store := &fakeWorldStore{task: &controlentity.TaskSession{TaskID: "task-1", UserID: "owner-1"}, world: &controlentity.WorldState{TaskID: "task-1", Revision: 2, State: map[string]any{"entities": map[string]any{"entity-existing": map[string]any{"type": "Agent", "canonical_name": "Athena"}}}}, entities: []controlentity.WorldEntity{{EntityID: "entity-existing", Scope: "task:task-1", Type: "Agent", CanonicalName: "Athena"}}}
	service := NewService(store)
	ontology := testOntology(t)
	service.SetOntologyResolver(fakeOntologyResolver{pack: &knowledgeentity.OntologyPack{PackID: ontology.PackID, OwnerID: "owner-1", Current: ontology.Version}, version: ontology})
	patch := testWorldPatch([]controlentity.WorldMutation{{Operation: "set", Path: "/entities/entity-new", Value: map[string]any{"type": "Agent", "canonical_name": " athena "}}})
	observation := controlentity.Observation{TaskID: "task-1", ObservationID: "observation-1", Evidence: patch.Evidence, WorldPatch: &patch}
	if err := service.CommitObservation(context.Background(), observation); err != nil {
		t.Fatal(err)
	}
	if got := store.saved.WorldPatch.Mutations[0].Path; got != "/entities/entity-existing" {
		t.Fatalf("resolved path = %q", got)
	}
}

func TestServiceRecordsAmbiguousEntityResolutionConflict(t *testing.T) {
	store := &fakeWorldStore{task: &controlentity.TaskSession{TaskID: "task-1", UserID: "owner-1"}, world: &controlentity.WorldState{TaskID: "task-1", State: map[string]any{}}, entities: []controlentity.WorldEntity{{EntityID: "a", Type: "Agent", CanonicalName: "Athena"}, {EntityID: "b", Type: "Agent", CanonicalName: "athena"}}}
	service := NewService(store)
	ontology := testOntology(t)
	service.SetOntologyResolver(fakeOntologyResolver{pack: &knowledgeentity.OntologyPack{PackID: ontology.PackID, OwnerID: "owner-1", Current: ontology.Version}, version: ontology})
	patch := testWorldPatch([]controlentity.WorldMutation{{Operation: "set", Path: "/entities/new", Value: map[string]any{"type": "Agent", "canonical_name": "Athena"}}})
	err := service.CommitObservation(context.Background(), controlentity.Observation{TaskID: "task-1", ObservationID: "observation-1", Evidence: patch.Evidence, WorldPatch: &patch})
	if err == nil || store.conflict == nil || store.saved != nil {
		t.Fatalf("error=%v conflict=%+v saved=%+v", err, store.conflict, store.saved)
	}
}

func TestSPARQLProviderProjectsReadOnlyResultsThroughOntologyValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.FormValue("query") == "" {
			t.Fatalf("unexpected SPARQL request: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{"results":{"bindings":[{"kind":{"type":"literal","value":"entity"},"id":{"type":"uri","value":"agent-1"},"type":{"type":"literal","value":"Agent"},"canonical_name":{"type":"literal","value":"Athena"}}]}}`))
	}))
	defer server.Close()

	store := &fakeWorldStore{}
	service := NewService(store)
	ontology := testOntology(t)
	service.SetOntologyResolver(fakeOntologyResolver{pack: &knowledgeentity.OntologyPack{PackID: ontology.PackID, OwnerID: "owner-1", Current: ontology.Version}, version: ontology})
	provider, err := service.CreateProvider(context.Background(), "owner-1", controlentity.WorldProviderRequest{
		Name: "RDF graph", Kind: controlentity.WorldProviderSPARQL, Endpoint: server.URL,
		QueryTemplate: "SELECT ?kind ?id ?type ?canonical_name WHERE { ?id ?p ?o } LIMIT {{limit}}",
		AuthMode:      controlentity.WorldProviderAuthNone, AllowPrivateNetwork: true,
		OntologyPack: ontology.PackID, OntologyVersion: ontology.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.QueryProvider(context.Background(), "owner-1", provider.ProviderID, controlentity.WorldQuery{Schema: "athena.world-query.v1", AsOf: time.Now().UTC(), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Authoritative || len(result.Snapshot.Entities) != 1 || result.Snapshot.Entities[0].Scope != "provider:"+provider.ProviderID || len(result.Snapshot.Entities[0].Evidence) != 1 {
		t.Fatalf("provider result = %+v", result)
	}
}

func TestValidatorRejectsExternalSnapshotWithUnknownSchema(t *testing.T) {
	now := time.Now().UTC()
	snapshot := &controlentity.WorldSnapshot{
		Schema: "athena.world-snapshot.future", OntologyPack: "core-1", OntologyVersion: "1.0.0",
		Entities: []controlentity.WorldEntity{{
			EntityID: "agent-1", Type: "Agent", Evidence: []controlentity.EvidenceRef{{EvidenceID: "evidence-1", Kind: "provider"}},
			Confidence: .8, ObservedAt: now, ExpiresAt: now.Add(time.Minute),
		}},
	}
	err := (Validator{}).ValidateSnapshot(snapshot, testOntology(t))
	if err == nil || !strings.Contains(err.Error(), SnapshotSchema) {
		t.Fatalf("ValidateSnapshot() error = %v, want schema rejection", err)
	}
}

func TestProviderReadOnlyGuardsRejectMutationLanguages(t *testing.T) {
	if readOnlySPARQL("INSERT DATA { <a> <b> <c> }") || readOnlyCypher("MATCH (n) DELETE n RETURN n") || readOnlyTypeQL("match $x isa thing; delete $x;") {
		t.Fatal("provider accepted a mutating graph query")
	}
}

func testWorldPatch(mutations []controlentity.WorldMutation) controlentity.WorldPatch {
	return controlentity.WorldPatch{OntologyPack: "core-1", OntologyVersion: "1.0.0", Evidence: []controlentity.EvidenceRef{{EvidenceID: "evidence-1", Kind: "test"}}, Confidence: .9, TTLSeconds: 3600, Mutations: mutations}
}

func testOntology(t *testing.T) *knowledgeentity.OntologyVersion {
	t.Helper()
	definition := knowledgev1.OntologyDefinition{
		Entities:  []knowledgev1.OntologyEntity{{ID: "Agent"}, {ID: "Device"}},
		Relations: []knowledgev1.OntologyRelation{{ID: "uses", SourceType: "Agent", TargetType: "Device"}},
	}
	body, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return &knowledgeentity.OntologyVersion{
		Schema: knowledgev1.Schema, VersionID: "ontology-version-1", PackID: "core-1", OwnerID: "owner-1", Version: "1.0.0",
		ValidationRules: []knowledgev1.ValidationRule{{SubjectType: "Agent", Predicate: "uses", ValueType: "Device", Required: true}},
		Definition:      definition, Checksum: hex.EncodeToString(sum[:]), Status: knowledgev1.OntologyApproved,
	}
}

type fakeWorldStore struct {
	task      *controlentity.TaskSession
	world     *controlentity.WorldState
	saved     *controlentity.Observation
	entities  []controlentity.WorldEntity
	conflict  *controlentity.WorldConflict
	providers map[string]controlentity.WorldProvider
}

func (s *fakeWorldStore) SaveObservation(_ context.Context, value controlentity.Observation) error {
	s.saved = &value
	return nil
}

func (s *fakeWorldStore) FindTask(context.Context, string) (*controlentity.TaskSession, error) {
	return s.task, nil
}

func (s *fakeWorldStore) FindWorldState(context.Context, string) (*controlentity.WorldState, error) {
	return s.world, nil
}

func (s *fakeWorldStore) QueryWorld(context.Context, string, controlentity.WorldQuery) ([]controlentity.WorldEntity, []controlentity.WorldRelation, []controlentity.WorldFact, error) {
	return s.entities, nil, nil, nil
}
func (s *fakeWorldStore) RecordWorldConflict(_ context.Context, value controlentity.WorldConflict) error {
	s.conflict = &value
	return nil
}
func (s *fakeWorldStore) ListWorldConflicts(context.Context, string, string, int) ([]controlentity.WorldConflict, error) {
	return nil, nil
}
func (s *fakeWorldStore) ResolveWorldConflict(context.Context, string, string, string, int64) (*controlentity.WorldConflict, error) {
	return nil, nil
}
func (s *fakeWorldStore) CreateWorldProvider(_ context.Context, value controlentity.WorldProvider) error {
	if s.providers == nil {
		s.providers = map[string]controlentity.WorldProvider{}
	}
	s.providers[value.ProviderID] = value
	return nil
}
func (s *fakeWorldStore) FindWorldProvider(_ context.Context, ownerID, providerID string) (*controlentity.WorldProvider, error) {
	value, ok := s.providers[providerID]
	if !ok || value.OwnerID != ownerID {
		return nil, nil
	}
	return &value, nil
}
func (s *fakeWorldStore) ListWorldProviders(_ context.Context, ownerID string) ([]controlentity.WorldProvider, error) {
	result := []controlentity.WorldProvider{}
	for _, value := range s.providers {
		if value.OwnerID == ownerID {
			result = append(result, value)
		}
	}
	return result, nil
}
func (s *fakeWorldStore) UpdateWorldProvider(_ context.Context, value controlentity.WorldProvider, _ int64) error {
	s.providers[value.ProviderID] = value
	return nil
}
func (s *fakeWorldStore) DeleteWorldProvider(_ context.Context, _, providerID string) error {
	delete(s.providers, providerID)
	return nil
}
func (s *fakeWorldStore) RecordWorldProviderHealth(_ context.Context, ownerID, providerID, status, message string, checkedAt time.Time) error {
	value := s.providers[providerID]
	if value.OwnerID == ownerID {
		value.HealthStatus, value.HealthMessage, value.LastCheckedAt = status, message, &checkedAt
		s.providers[providerID] = value
	}
	return nil
}

type fakeOntologyResolver struct {
	pack    *knowledgeentity.OntologyPack
	version *knowledgeentity.OntologyVersion
}

func (r fakeOntologyResolver) ResolveActiveOntology(context.Context, string) (*knowledgeentity.OntologyPack, *knowledgeentity.OntologyVersion, error) {
	return r.pack, r.version, nil
}
