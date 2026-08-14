package deployment

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
)

type Store interface {
	ResolveApprovedArtifacts(context.Context, string, map[string]string, map[string]string) (entity.ArtifactApprovals, error)
	CreateBuild(context.Context, entity.AgentBuild) error
	FindBuild(context.Context, string, string) (*entity.AgentBuild, error)
	FindBuildByChecksum(context.Context, string, string, string) (*entity.AgentBuild, error)
	ListBuilds(context.Context, string, entity.BuildFilter) ([]entity.AgentBuild, int64, error)

	CreatePromotion(context.Context, entity.Promotion) error
	FindPromotion(context.Context, string, string) (*entity.Promotion, error)
	FindActivePromotion(context.Context, string, string) (*entity.Promotion, error)
	FindCanaryPromotion(context.Context, string, string) (*entity.Promotion, error)
	FindPromotionForBuild(context.Context, string, string, string) (*entity.Promotion, error)
	ListPromotions(context.Context, string, entity.PromotionFilter) ([]entity.Promotion, int64, error)
	UpdatePromotion(context.Context, entity.Promotion, int64) error
	ActivatePromotion(context.Context, entity.Promotion, *entity.Promotion, int64) error

	FindExposure(context.Context, string, string, string) (*entity.Exposure, error)
	CreateExposure(context.Context, entity.Exposure) error
	SetExposurePreference(context.Context, string, string, bool, string) error
	CreateShadowResult(context.Context, entity.ShadowResult) error
	ListShadowResults(context.Context, string, string, int) ([]entity.ShadowResult, error)
	AppendCanarySample(context.Context, entity.CanarySample, entity.Promotion) (*entity.CanaryMetric, *entity.Promotion, error)
	ListCanarySamples(context.Context, string, string, int) ([]entity.CanarySample, error)
	ListCanaryMetrics(context.Context, string, string, int) ([]entity.CanaryMetric, error)

	CreateRunManifest(context.Context, entity.RunManifest) error
	FindRunManifest(context.Context, string, string) (*entity.RunManifest, error)
	ListRunManifests(context.Context, string, string, int) ([]entity.RunManifest, error)
	RollbackPromotion(context.Context, entity.Promotion, *entity.Promotion, entity.Rollback, []entity.Compensation, int64) error
	ListRollbacks(context.Context, string, string, int) ([]entity.Rollback, error)
}
