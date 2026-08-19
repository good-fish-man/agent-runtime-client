package deployment

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
)

// DelegationArtifactApprovalResolver verifies that declarative delegation
// artifacts completed the independent learning governance pipeline before
// they are embedded in an immutable AgentBuild.
type DelegationArtifactApprovalResolver interface {
	ResolveBuildApprovals(
		ctx context.Context,
		ownerID string,
		policyVersions map[string]string,
		profileVersions map[string]string,
	) ([]entity.ArtifactApprovalReference, error)
}

func (s *Service) WithDelegationArtifactResolver(resolver DelegationArtifactApprovalResolver) *Service {
	if s != nil {
		s.delegationArtifacts = resolver
	}
	return s
}
