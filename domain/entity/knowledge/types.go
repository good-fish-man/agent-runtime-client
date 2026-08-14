package knowledge

import knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"

const Schema = knowledgev1.Schema

type Provenance = knowledgev1.Provenance
type Evidence = knowledgev1.Evidence
type Claim = knowledgev1.Claim
type Contradiction = knowledgev1.Contradiction
type Snapshot = knowledgev1.Snapshot
type OntologyPack = knowledgev1.OntologyPack
type OntologyVersion = knowledgev1.OntologyVersion
type OntologyCandidate = knowledgev1.OntologyCandidate
type OntologyMigration = knowledgev1.OntologyMigration
type RetrievalQuery = knowledgev1.RetrievalQuery
type RetrievalHit = knowledgev1.RetrievalHit

type ClaimFilter struct {
	Subject       string
	Predicate     string
	Scopes        []string
	Sensitivities []string
	Statuses      []string
	Limit         int
}

type EvidenceFilter struct {
	SourceTypes []string
	Scopes      []string
	Limit       int
}
