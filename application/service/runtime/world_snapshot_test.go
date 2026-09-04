package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	worldmodel "github.com/good-fish-man/agent-runtime-client/application/service/worldmodel"
	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	knowledgeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	controlrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/control"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

func TestInjectWorldSnapshotReplacesCallerFieldsWithAuthoritySnapshot(t *testing.T) {
	definition := knowledgev1.OntologyDefinition{
		Entities:  []knowledgev1.OntologyEntity{{ID: "Task"}},
		Relations: []knowledgev1.OntologyRelation{},
	}
	body, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ontology := &knowledgeentity.OntologyVersion{
		Schema: knowledgev1.Schema, VersionID: "version-1", PackID: "pack-1", OwnerID: "owner-1", Version: "1.0.0",
		ValidationRules: []knowledgev1.ValidationRule{{SubjectType: "Task", Predicate: "status", ValueType: "Task"}},
		Definition:      definition, Checksum: hex.EncodeToString(sum[:]), Status: knowledgev1.OntologyApproved,
	}
	store := &runtimeWorldStore{
		task:  &controlentity.TaskSession{TaskID: "task-1", UserID: "owner-1"},
		world: &controlentity.WorldState{TaskID: "task-1", Revision: 12, State: map[string]any{"status": "observed"}, UpdatedAt: time.Now().UTC()},
	}
	hub := controlsvc.NewHub(store)
	hub.SetOntologyResolver(runtimeOntologyResolver{
		pack: &knowledgeentity.OntologyPack{PackID: "pack-1", OwnerID: "owner-1", Current: "1.0.0"}, version: ontology,
	})
	service := &RuntimeService{}
	service.SetControlHub(hub)
	values := map[string]any{
		"world_snapshot": map[string]any{"revision": 999}, "world_revision": int64(999),
		"ontology_version": "attacker-controlled",
	}
	ctx := authctx.WithUserID(context.Background(), "owner-1")
	if err := service.injectWorldSnapshot(ctx, values, "task-1"); err != nil {
		t.Fatal(err)
	}
	if values["world_revision"] != int64(12) || values["ontology_version"] != "1.0.0" {
		t.Fatalf("injected context = %+v", values)
	}
	snapshot, ok := values["world_snapshot"].(*worldmodel.Snapshot)
	if !ok || snapshot.Revision != 12 || snapshot.OntologyVersion != "1.0.0" {
		t.Fatalf("world snapshot = %#v", values["world_snapshot"])
	}
	if values["world_snapshot_checksum"] == "" || values["ontology_checksum"] != ontology.Checksum {
		t.Fatalf("snapshot checksums were not injected: %+v", values)
	}
	if context, ok := values["ontology_context"].(*worldmodel.OntologyContext); !ok || context.Version != "1.0.0" {
		t.Fatalf("ontology context was not injected: %#v", values["ontology_context"])
	}
}

func (s *runtimeWorldStore) QueryWorld(context.Context, string, controlentity.WorldQuery) ([]controlentity.WorldEntity, []controlentity.WorldRelation, []controlentity.WorldFact, error) {
	return nil, nil, nil, nil
}

type runtimeWorldStore struct {
	controlrepo.Store
	task  *controlentity.TaskSession
	world *controlentity.WorldState
}

func (s *runtimeWorldStore) FindTask(context.Context, string) (*controlentity.TaskSession, error) {
	return s.task, nil
}

func (s *runtimeWorldStore) FindWorldState(context.Context, string) (*controlentity.WorldState, error) {
	return s.world, nil
}

type runtimeOntologyResolver struct {
	pack    *knowledgeentity.OntologyPack
	version *knowledgeentity.OntologyVersion
}

func (r runtimeOntologyResolver) ResolveActiveOntology(context.Context, string) (*knowledgeentity.OntologyPack, *knowledgeentity.OntologyVersion, error) {
	return r.pack, r.version, nil
}
