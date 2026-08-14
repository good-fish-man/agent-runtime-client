package knowledge

import knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"

const Schema = knowledgev1.Schema

type Provenance = knowledgev1.Provenance
type Evidence = knowledgev1.Evidence
type ClaimRelation = knowledgev1.ClaimRelation
type Claim = knowledgev1.Claim
type Contradiction = knowledgev1.Contradiction
type ContradictionResolution = knowledgev1.ContradictionResolution
type Snapshot = knowledgev1.Snapshot
type OntologyPack = knowledgev1.OntologyPack
type OntologyVersion = knowledgev1.OntologyVersion
type OntologyDefinition = knowledgev1.OntologyDefinition
type OntologyEntity = knowledgev1.OntologyEntity
type OntologyRelation = knowledgev1.OntologyRelation
type OntologyCandidate = knowledgev1.OntologyCandidate
type OntologyEvaluation = knowledgev1.OntologyEvaluation
type OntologyEvaluationCheck = knowledgev1.OntologyEvaluationCheck
type OntologyMigration = knowledgev1.OntologyMigration
type OntologyMigrationStep = knowledgev1.OntologyMigrationStep
type OntologyMigrationExecution = knowledgev1.OntologyMigrationExecution
type RetrievalQuery = knowledgev1.RetrievalQuery
type RetrievalHit = knowledgev1.RetrievalHit

type ClaimFilter struct {
	OwnerIDs        []string
	OrganizationIDs []string
	Subject         string
	Predicate       string
	Scopes          []string
	Sensitivities   []string
	Statuses        []string
	Limit           int
}

type EvidenceFilter struct {
	SourceTypes []string
	Scopes      []string
	Limit       int
}
