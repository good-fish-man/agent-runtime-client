package deployment

import (
	"context"
	"strings"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
)

type delegationApprovalResolverStub struct{}

func (delegationApprovalResolverStub) ResolveBuildApprovals(_ context.Context, _ string, policies, profiles map[string]string) ([]entity.ArtifactApprovalReference, error) {
	result := make([]entity.ArtifactApprovalReference, 0, len(policies)+len(profiles))
	appendApproval := func(kind, artifactID, version string) {
		result = append(result, entity.ArtifactApprovalReference{
			Kind: kind, ArtifactID: artifactID, Version: version, VersionID: "rollout-1",
			CandidateID: "candidate-1", EvaluationRunID: "shadow-1", ReviewedBy: "reviewer-1",
			ReviewedAt: time.Now().UTC(), Checksum: strings.Repeat("a", 64), Verified: true,
		})
	}
	for artifactID, version := range policies {
		appendApproval("DELEGATION_POLICY", artifactID, version)
	}
	for artifactID, version := range profiles {
		appendApproval("SPECIALIST_PROFILE", artifactID, version)
	}
	return result, nil
}

func TestCreateBuildRequiresGovernedDelegationResolver(t *testing.T) {
	service := newDeploymentTestService(t)
	request := CreateBuildRequest{
		AgentID: "agent-dso", DelegationPolicyVersions: map[string]string{"policy-research": "v3"},
		SpecialistProfileVersions: map[string]string{"profile-research": "v2"},
	}
	if _, err := service.CreateBuild(context.Background(), "owner-1", "owner-1", request); err == nil {
		t.Fatal("build accepted delegation artifacts without a governance resolver")
	}
	service.WithDelegationArtifactResolver(delegationApprovalResolverStub{})
	build, err := service.CreateBuild(context.Background(), "owner-1", "owner-1", request)
	if err != nil {
		t.Fatal(err)
	}
	if build.DelegationPolicyVersions["policy-research"] != "v3" || build.SpecialistProfileVersions["profile-research"] != "v2" || len(build.ArtifactApprovals) != 2 {
		t.Fatalf("governed build=%+v", build)
	}
}
