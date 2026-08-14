package knowledge

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
)

type Store interface {
	CreateEvidence(context.Context, []entity.Evidence) error
	CreateKnowledge(context.Context, entity.Claim, []entity.Contradiction, []string) error
	FindClaim(context.Context, string, string) (*entity.Claim, error)
	FindClaims(context.Context, string, []string) ([]entity.Claim, error)
	ListClaims(context.Context, string, entity.ClaimFilter) ([]entity.Claim, error)
	SearchClaims(context.Context, string, []string, entity.ClaimFilter) ([]entity.Claim, error)
	FindEvidence(context.Context, string, []string) ([]entity.Evidence, error)
	ListEvidence(context.Context, string, entity.EvidenceFilter) ([]entity.Evidence, error)
	ListContradictions(context.Context, string, bool, int) ([]entity.Contradiction, error)
	FindContradiction(context.Context, string, string) (*entity.Contradiction, error)
	ResolveContradiction(context.Context, entity.Contradiction, map[string]string) error
	CreateSnapshot(context.Context, entity.Snapshot) error
	BindSnapshotToRun(context.Context, string, string, string, int64) error
	ListSnapshots(context.Context, string, int) ([]entity.Snapshot, error)

	CreateOntologyPack(context.Context, entity.OntologyPack) error
	CreateOntologyPackWithVersion(context.Context, entity.OntologyPack, entity.OntologyVersion) error
	FindOntologyPack(context.Context, string, string) (*entity.OntologyPack, error)
	FindOntologyVersion(context.Context, string, string, string) (*entity.OntologyVersion, error)
	ListOntologyPacks(context.Context, string) ([]entity.OntologyPack, error)
	CreateOntologyCandidate(context.Context, entity.OntologyCandidate) error
	FindOntologyCandidate(context.Context, string, string) (*entity.OntologyCandidate, error)
	ListOntologyCandidates(context.Context, string, int) ([]entity.OntologyCandidate, error)
	ReviewOntologyCandidate(context.Context, entity.OntologyCandidate, int64, *entity.OntologyVersion) error
	CreateOntologyMigration(context.Context, entity.OntologyMigration) error
	FindOntologyMigration(context.Context, string, string) (*entity.OntologyMigration, error)
	ReviewOntologyMigration(context.Context, entity.OntologyMigration, string) error
	ApplyOntologyMigration(context.Context, entity.OntologyMigration) error
}
