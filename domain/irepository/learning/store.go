package learning

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
)

type Store interface {
	CapabilityPolicies(context.Context, []string) (map[string]entity.CapabilityPolicy, error)
	ApprovedSkills(context.Context, string, string) (map[string]bool, error)
	CreateCandidate(context.Context, entity.Candidate, []entity.CandidateEvidence, entity.CandidateEvaluation) error
	FindCandidate(context.Context, string, string) (*entity.Candidate, error)
	ListCandidates(context.Context, string, entity.CandidateFilter) ([]entity.Candidate, int64, error)
	UpdateCandidate(context.Context, entity.Candidate, int64) error
	SaveCandidateEvaluation(context.Context, entity.Candidate, entity.CandidateEvaluation, int64) error
	ReviewCandidate(context.Context, string, string, string, string, string, string, int64) (*entity.Candidate, error)
	ListEvidence(context.Context, string, string) ([]entity.CandidateEvidence, error)
	ListEvaluations(context.Context, string, string) ([]entity.CandidateEvaluation, error)
	ListSkills(context.Context, string, string, int) ([]entity.Skill, error)
	ListStrategies(context.Context, string, string, int) ([]entity.Strategy, error)

	CreateDemonstration(context.Context, entity.Demonstration) error
	FindDemonstration(context.Context, string, string) (*entity.Demonstration, error)
	ListDemonstrations(context.Context, string, int) ([]entity.Demonstration, error)
	SaveDemonstration(context.Context, entity.Demonstration, int64) error
	TaskActions(context.Context, string, string) ([]entity.SemanticAction, bool, error)
}
