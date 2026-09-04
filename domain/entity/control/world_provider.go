package control

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	WorldProviderAthenaHTTP = "ATHENA_HTTP"
	WorldProviderSPARQL     = "SPARQL"
	WorldProviderNeo4j      = "NEO4J"
	WorldProviderTypeDB     = "TYPEDB"

	WorldProviderHealthUnknown   = "UNKNOWN"
	WorldProviderHealthAvailable = "AVAILABLE"
	WorldProviderHealthFailed    = "FAILED"

	WorldProviderAuthNone   = "NONE"
	WorldProviderAuthBearer = "BEARER"
	WorldProviderAuthBasic  = "BASIC"
)

// WorldProvider is a read-only external world-model connection. CredentialEnv
// names a server-side environment variable; credential values are never stored.
type WorldProvider struct {
	ProviderID          string     `json:"provider_id"`
	OwnerID             string     `json:"owner_id,omitempty"`
	Name                string     `json:"name"`
	Kind                string     `json:"kind"`
	Endpoint            string     `json:"endpoint"`
	Database            string     `json:"database,omitempty"`
	QueryTemplate       string     `json:"query_template,omitempty"`
	AuthMode            string     `json:"auth_mode"`
	CredentialEnv       string     `json:"credential_env,omitempty"`
	AllowPrivateNetwork bool       `json:"allow_private_network"`
	OntologyPack        string     `json:"ontology_pack"`
	OntologyVersion     string     `json:"ontology_version"`
	DefaultConfidence   float64    `json:"default_confidence"`
	TTLSeconds          int64      `json:"ttl_seconds"`
	TimeoutMS           int        `json:"timeout_ms"`
	Enabled             bool       `json:"enabled"`
	ReadOnly            bool       `json:"read_only"`
	Capabilities        []string   `json:"capabilities"`
	HealthStatus        string     `json:"health_status"`
	HealthMessage       string     `json:"health_message,omitempty"`
	LastCheckedAt       *time.Time `json:"last_checked_at,omitempty"`
	Revision            int64      `json:"revision"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type WorldProviderRequest struct {
	Name                string  `json:"name"`
	Kind                string  `json:"kind"`
	Endpoint            string  `json:"endpoint"`
	Database            string  `json:"database,omitempty"`
	QueryTemplate       string  `json:"query_template,omitempty"`
	AuthMode            string  `json:"auth_mode"`
	CredentialEnv       string  `json:"credential_env,omitempty"`
	AllowPrivateNetwork bool    `json:"allow_private_network"`
	OntologyPack        string  `json:"ontology_pack"`
	OntologyVersion     string  `json:"ontology_version"`
	DefaultConfidence   float64 `json:"default_confidence"`
	TTLSeconds          int64   `json:"ttl_seconds"`
	TimeoutMS           int     `json:"timeout_ms"`
	Enabled             *bool   `json:"enabled,omitempty"`
	ExpectedRevision    int64   `json:"expected_revision,omitempty"`
}

type WorldProviderQueryResult struct {
	ProviderID    string        `json:"provider_id"`
	ProviderKind  string        `json:"provider_kind"`
	Authoritative bool          `json:"authoritative"`
	Snapshot      WorldSnapshot `json:"snapshot"`
	Warnings      []string      `json:"warnings,omitempty"`
}

func (p WorldProvider) Validate() error {
	if strings.TrimSpace(p.ProviderID) == "" || strings.TrimSpace(p.OwnerID) == "" || strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("provider_id, owner_id, and name are required")
	}
	switch p.Kind {
	case WorldProviderAthenaHTTP, WorldProviderSPARQL, WorldProviderNeo4j, WorldProviderTypeDB:
	default:
		return fmt.Errorf("unsupported world provider kind %q", p.Kind)
	}
	parsed, err := url.Parse(strings.TrimSpace(p.Endpoint))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("provider endpoint must be an absolute HTTP(S) URL without credentials or a fragment")
	}
	if len(p.QueryTemplate) > 64*1024 || strings.ContainsRune(p.QueryTemplate, '\x00') {
		return fmt.Errorf("provider query template is invalid or too large")
	}
	if p.Kind != WorldProviderAthenaHTTP && strings.TrimSpace(p.QueryTemplate) == "" {
		return fmt.Errorf("provider query_template is required for %s", p.Kind)
	}
	if (p.Kind == WorldProviderNeo4j || p.Kind == WorldProviderTypeDB) && strings.TrimSpace(p.Database) == "" {
		return fmt.Errorf("provider database is required for %s", p.Kind)
	}
	if p.AuthMode != WorldProviderAuthNone && p.AuthMode != WorldProviderAuthBearer && p.AuthMode != WorldProviderAuthBasic {
		return fmt.Errorf("unsupported provider auth_mode %q", p.AuthMode)
	}
	if p.AuthMode != WorldProviderAuthNone && (!validEnvironmentName(p.CredentialEnv) || !strings.HasPrefix(p.CredentialEnv, "ATHENA_WORLD_PROVIDER_")) {
		return fmt.Errorf("authenticated providers require an ATHENA_WORLD_PROVIDER_* credential_env")
	}
	if strings.TrimSpace(p.OntologyPack) == "" || strings.TrimSpace(p.OntologyVersion) == "" {
		return fmt.Errorf("provider ontology_pack and ontology_version are required")
	}
	if p.DefaultConfidence <= 0 || p.DefaultConfidence > 1 {
		return fmt.Errorf("provider default_confidence must be greater than 0 and at most 1")
	}
	if p.TTLSeconds < 1 || p.TTLSeconds > int64((365*24*time.Hour)/time.Second) {
		return fmt.Errorf("provider ttl_seconds must be between 1 and one year")
	}
	if p.TimeoutMS < 250 || p.TimeoutMS > 30_000 {
		return fmt.Errorf("provider timeout_ms must be between 250 and 30000")
	}
	if !p.ReadOnly {
		return fmt.Errorf("external world providers must be read-only")
	}
	return nil
}

func validEnvironmentName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for index, r := range value {
		if (r >= 'A' && r <= 'Z') || r == '_' || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
