package control

import (
	"strings"
	"testing"
	"time"
)

func TestWorldProviderCredentialsUseDedicatedEnvironmentNamespace(t *testing.T) {
	provider := WorldProvider{
		ProviderID: "provider-1", OwnerID: "owner-1", Name: "Graph", Kind: WorldProviderSPARQL,
		Endpoint: "https://graph.example.com/sparql", QueryTemplate: "SELECT * WHERE { ?s ?p ?o }",
		AuthMode: WorldProviderAuthBearer, CredentialEnv: "ATHENA_WORLD_PROVIDER_GRAPH_TOKEN",
		OntologyPack: "core", OntologyVersion: "1.0.0", DefaultConfidence: .8,
		TTLSeconds: 300, TimeoutMS: 5000, Enabled: true, ReadOnly: true,
		Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := provider.Validate(); err != nil {
		t.Fatal(err)
	}
	provider.CredentialEnv = "ATHENA_DB_PASSWORD"
	if err := provider.Validate(); err == nil || !strings.Contains(err.Error(), "ATHENA_WORLD_PROVIDER_*") {
		t.Fatalf("Validate() error = %v, want dedicated namespace rejection", err)
	}
}
