package knowledge

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
)

type Store interface {
	CreateEvidence(context.Context, []entity.Evidence) error
	CreateKnowledge(context.Context, []entity.Evidence, entity.Claim, []entity.Contradiction, []string) error
	FindClaim(context.Context, string, string) (*entity.Claim, error)
	ListClaims(context.Context, string, entity.ClaimFilter) ([]entity.Claim, error)
	FindEvidence(context.Context, string, []string) ([]entity.Evidence, error)
	ListEvidence(context.Context, string, entity.EvidenceFilter) ([]entity.Evidence, error)
	ListContradictions(context.Context, string, bool, int) ([]entity.Contradiction, error)
	CreateSnapshot(context.Context, entity.Snapshot) error
	ListSnapshots(context.Context, string, int) ([]entity.Snapshot, error)

	CreateOntologyPack(context.Context, entity.OntologyPack) error
	CreateOntologyPackWithVersion(context.Context, entity.OntologyPack, entity.OntologyVersion) error
	ListOntologyPacks(context.Context, string) ([]entity.OntologyPack, error)
	CreateOntologyVersion(context.Context, entity.OntologyVersion) error
	CreateOntologyCandidate(context.Context, entity.OntologyCandidate) error
	FindOntologyCandidate(context.Context, string, string) (*entity.OntologyCandidate, error)
	ReviewOntologyCandidate(context.Context, entity.OntologyCandidate, int64, *entity.OntologyVersion) error
	ApplyOntologyMigration(context.Context, entity.OntologyMigration) error
}
