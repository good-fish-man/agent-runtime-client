package deployment

import deploymentv1 "github.com/good-fish-man/athena-protocol/protocol/deployment/v1"

const (
	Schema           = deploymentv1.Schema
	StatusProposed   = deploymentv1.StatusProposed
	StatusReviewed   = deploymentv1.StatusReviewed
	StatusShadow     = deploymentv1.StatusShadow
	StatusCanary     = deploymentv1.StatusCanary
	StatusActive     = deploymentv1.StatusActive
	StatusPaused     = deploymentv1.StatusPaused
	StatusRolledBack = deploymentv1.StatusRolledBack
	StatusRetired    = deploymentv1.StatusRetired
	VariantControl   = deploymentv1.VariantControl
	VariantCandidate = deploymentv1.VariantCandidate
	RiskR0           = deploymentv1.RiskR0
	RiskR1           = deploymentv1.RiskR1
	RiskR2           = deploymentv1.RiskR2
	RiskR3           = deploymentv1.RiskR3
)

type AgentBuild = deploymentv1.AgentBuild
type RunBudget = deploymentv1.RunBudget
type RunManifest = deploymentv1.RunManifest
type CanaryThresholds = deploymentv1.CanaryThresholds
type Promotion = deploymentv1.Promotion
type Exposure = deploymentv1.Exposure
type ShadowResult = deploymentv1.ShadowResult
type CanaryMetric = deploymentv1.CanaryMetric
type Rollback = deploymentv1.Rollback
type Compensation = deploymentv1.Compensation

type BuildFilter struct {
	AgentID string
	Limit   int
	Offset  int
}

type PromotionFilter struct {
	AgentID string
	Status  string
	Limit   int
	Offset  int
}

type ArtifactVersions struct {
	Skills     map[string]string
	Strategies map[string]string
}
