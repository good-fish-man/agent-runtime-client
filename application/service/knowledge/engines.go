package knowledge

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"
	"unicode"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

const localEmbeddingDimensions = 256

// Embedder is intentionally injectable. The default is deterministic and
// local; production deployments can provide a model-backed implementation.
type Embedder interface {
	Embed(context.Context, string) ([]float32, error)
}

type localEmbedder struct{ dimensions int }

func newLocalEmbedder() Embedder { return localEmbedder{dimensions: localEmbeddingDimensions} }

func (e localEmbedder) Embed(ctx context.Context, value string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dimensions := e.dimensions
	if dimensions < 32 {
		dimensions = localEmbeddingDimensions
	}
	vector := make([]float32, dimensions)
	normalized := strings.ToLower(strings.TrimSpace(value))
	tokens := strings.FieldsFunc(normalized, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	features := append([]string(nil), tokens...)
	runes := []rune(normalized)
	for size := 2; size <= 4; size++ {
		for index := 0; index+size <= len(runes); index++ {
			feature := strings.TrimSpace(string(runes[index : index+size]))
			if feature != "" {
				features = append(features, feature)
			}
		}
	}
	for _, feature := range features {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(feature))
		sum := hasher.Sum64()
		index := int(sum % uint64(dimensions))
		sign := float32(1)
		var bytes [8]byte
		binary.LittleEndian.PutUint64(bytes[:], sum)
		if bytes[7]&1 == 1 {
			sign = -1
		}
		vector[index] += sign
	}
	norm := float64(0)
	for _, item := range vector {
		norm += float64(item * item)
	}
	if norm == 0 {
		return vector, nil
	}
	norm = math.Sqrt(norm)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) / norm)
	}
	return vector, nil
}

type OntologyMigrator interface {
	Execute(context.Context, entity.OntologyMigration, entity.OntologyCandidate) (*entity.OntologyMigrationExecution, error)
}

type declarativeOntologyMigrator struct{}

func (declarativeOntologyMigrator) Execute(ctx context.Context, migration entity.OntologyMigration, candidate entity.OntologyCandidate) (*entity.OntologyMigrationExecution, error) {
	started := time.Now().UTC()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if candidate.CandidateID != migration.CandidateID || candidate.Proposed.Version != migration.ToVersion {
		return nil, fmt.Errorf("migration candidate does not match the approved ontology version")
	}
	for _, step := range migration.Plan {
		if !knowledgev1.ValidMigrationOperation(step.Operation) {
			return nil, fmt.Errorf("unsupported ontology migration operation %q", step.Operation)
		}
		switch step.Operation {
		case "ADD_ENTITY":
			if !ontologyHasEntity(candidate.Proposed.Definition, step.Target) {
				return nil, fmt.Errorf("added ontology entity %q is absent from the approved definition", step.Target)
			}
		case "ADD_RELATION":
			if !ontologyHasRelation(candidate.Proposed.Definition, step.Target) {
				return nil, fmt.Errorf("added ontology relation %q is absent from the approved definition", step.Target)
			}
		case "RENAME_DISPLAY":
			if !ontologyHasEntity(candidate.Proposed.Definition, step.Target) && !ontologyHasRelation(candidate.Proposed.Definition, step.Target) {
				return nil, fmt.Errorf("ontology display target %q is absent from the approved definition", step.Target)
			}
		}
		for key, value := range step.Parameters {
			if len(key) > 128 || len(value) > 2048 || strings.ContainsAny(value, "\x00\r\n") {
				return nil, fmt.Errorf("ontology migration parameters must be bounded declarative values")
			}
		}
	}
	planSHA, err := checksumJSON(migration.Plan)
	if err != nil {
		return nil, err
	}
	return &entity.OntologyMigrationExecution{
		ExecutorID:  "athena.ontology-migrator.v1",
		PlanSHA256:  planSHA,
		StartedAt:   started,
		CompletedAt: time.Now().UTC(),
		Success:     true,
	}, nil
}

func ontologyHasEntity(definition entity.OntologyDefinition, id string) bool {
	for _, item := range definition.Entities {
		if item.ID == id {
			return true
		}
	}
	return false
}

func ontologyHasRelation(definition entity.OntologyDefinition, id string) bool {
	for _, item := range definition.Relations {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *Service) WithEmbedder(value Embedder) *Service {
	if value != nil {
		s.embedder = value
	}
	return s
}

func (s *Service) WithOntologyMigrator(value OntologyMigrator) *Service {
	if value != nil {
		s.migrator = value
	}
	return s
}
