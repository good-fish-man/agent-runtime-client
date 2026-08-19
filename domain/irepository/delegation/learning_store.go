package delegation

import (
	"context"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
)

type LearningStore interface {
	GetLearningPreference(context.Context, string) (entity.LearningPreference, error)
	SetLearningPreference(context.Context, entity.LearningPreference, int64, entity.Event) error
	CreateLearningCandidate(context.Context, entity.LearningCandidate, entity.Event) error
	FindLearningCandidate(context.Context, string, string) (*entity.LearningCandidate, error)
	ListLearningCandidates(context.Context, string, int) ([]entity.LearningCandidate, error)
	CreateLearningEvaluation(context.Context, entity.LearningEvaluation, entity.Event) error
	FindLearningEvaluation(context.Context, string, string) (*entity.LearningEvaluation, error)
	ListLearningEvaluations(context.Context, string, string) ([]entity.LearningEvaluation, error)
	CreateLearningReview(context.Context, entity.LearningReview, entity.Event) error
	FindApprovedLearningReview(context.Context, string, string) (*entity.LearningReview, error)
	ListLearningReviews(context.Context, string, string) ([]entity.LearningReview, error)
	CreateLearningRollout(context.Context, entity.LearningRollout, entity.Event) error
	FindLearningRollout(context.Context, string, string) (*entity.LearningRollout, error)
	FindEffectiveLearningRollout(context.Context, string, string) (*entity.LearningRollout, error)
	ListLearningRollouts(context.Context, string, int) ([]entity.LearningRollout, error)
	TransitionLearningRollout(context.Context, string, string, int64, string, string, string, time.Time, entity.Event) error
	CreateLearningBenchmark(context.Context, entity.LearningBenchmark, entity.Event) error
	ListLearningBenchmarks(context.Context, string, string) ([]entity.LearningBenchmark, error)
}
