package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	deploymententity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

// ResolveBuildApprovals is the final fail-closed gate between governed
// learning and immutable AgentBuild assembly. Canary exposure is deliberately
// insufficient: only a promoted artifact with a currently valid full lineage
// can enter the default build.
func (s *GovernedLearningService) ResolveBuildApprovals(
	ctx context.Context,
	ownerID string,
	policyVersions map[string]string,
	profileVersions map[string]string,
) ([]deploymententity.ArtifactApprovalReference, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("delegation learning service is not configured")
	}
	ownerID = strings.TrimSpace(ownerID)
	if err := s.requireLearningEnabled(ctx, ownerID); err != nil {
		return nil, err
	}
	requested := make([]requestedLearningArtifact, 0, len(policyVersions)+len(profileVersions))
	for artifactID, version := range policyVersions {
		requested = append(requested, requestedLearningArtifact{Kind: dso.LearningCandidateDelegationPolicy, ArtifactID: strings.TrimSpace(artifactID), Version: strings.TrimSpace(version)})
	}
	for artifactID, version := range profileVersions {
		requested = append(requested, requestedLearningArtifact{Kind: dso.LearningCandidateSpecialistProfile, ArtifactID: strings.TrimSpace(artifactID), Version: strings.TrimSpace(version)})
	}
	sort.Slice(requested, func(i, j int) bool {
		if requested[i].Kind == requested[j].Kind {
			return requested[i].ArtifactID < requested[j].ArtifactID
		}
		return requested[i].Kind < requested[j].Kind
	})
	for _, value := range requested {
		if value.ArtifactID == "" || value.Version == "" {
			return nil, fmt.Errorf("delegation build artifact id and version are required")
		}
	}

	rollouts, err := s.store.ListLearningRollouts(ctx, ownerID, 500)
	if err != nil {
		return nil, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.listRollouts")
	}
	result := make([]deploymententity.ArtifactApprovalReference, 0, len(requested))
	for _, value := range requested {
		approval, resolveErr := s.resolvePromotedBuildApproval(ctx, ownerID, value, rollouts)
		if resolveErr != nil {
			return nil, resolveErr
		}
		result = append(result, approval)
	}
	return result, nil
}

type requestedLearningArtifact struct {
	Kind       string
	ArtifactID string
	Version    string
}

func (s *GovernedLearningService) resolvePromotedBuildApproval(
	ctx context.Context,
	ownerID string,
	requested requestedLearningArtifact,
	rollouts []delegationentity.LearningRollout,
) (deploymententity.ArtifactApprovalReference, error) {
	for _, record := range rollouts {
		if record.Status != dso.LearningRolloutPromoted || record.Kind != requested.Kind {
			continue
		}
		candidate, err := s.loadCandidate(ctx, ownerID, record.CandidateID)
		if err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.candidate")
		}
		artifactID, version := learningArtifactIdentity(*candidate)
		if artifactID != requested.ArtifactID || version != requested.Version {
			continue
		}
		rollout, err := decodeLearningRollout(record)
		if err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.rollout")
		}
		candidate, review, offline, shadow, err := s.loadExactGovernanceChain(ctx, ownerID, rollout)
		if err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.lineage")
		}
		if err := rollout.Validate(*candidate, *review, *offline, *shadow); err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.validateRollout")
		}
		benchmarks, err := s.store.ListLearningBenchmarks(ctx, ownerID, rollout.RolloutID)
		if err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.benchmarks")
		}
		if len(benchmarks) == 0 || !benchmarks[len(benchmarks)-1].Passed {
			return deploymententity.ArtifactApprovalReference{}, fmt.Errorf("%w: promoted artifact %s@%s has no passing benchmark", ErrGovernanceGateIncomplete, requested.ArtifactID, requested.Version)
		}
		var report dso.DelegationBenchmarkReport
		latest := benchmarks[len(benchmarks)-1]
		if err := json.Unmarshal([]byte(latest.Content), &report); err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.decodeBenchmark")
		}
		if report.CandidateRef != candidate.CandidateID || report.ContentHash != latest.ContentHash || !report.PassesPromotion() {
			return deploymententity.ArtifactApprovalReference{}, fmt.Errorf("%w: promoted artifact benchmark lineage is invalid", ErrGovernanceGateIncomplete)
		}
		if err := report.Validate(); err != nil {
			return deploymententity.ArtifactApprovalReference{}, log.WrapError(err, "GovernedLearning.ResolveBuildApprovals.validateBenchmark")
		}
		return deploymententity.ArtifactApprovalReference{
			Kind: requested.Kind, ArtifactID: requested.ArtifactID, Version: requested.Version,
			VersionID: rollout.RolloutID, CandidateID: candidate.CandidateID,
			EvaluationRunID: shadow.EvaluationID, ReviewedBy: review.ReviewerID,
			ReviewedAt: review.ReviewedAt, Checksum: candidate.DefinitionHash, Verified: true,
		}, nil
	}
	return deploymententity.ArtifactApprovalReference{}, fmt.Errorf("%w: promoted artifact %s@%s was not found", ErrGovernanceGateIncomplete, requested.ArtifactID, requested.Version)
}

func learningArtifactIdentity(candidate dso.DelegationLearningCandidate) (string, string) {
	if candidate.PolicyArtifact != nil {
		return candidate.PolicyArtifact.ArtifactID, candidate.PolicyArtifact.Version
	}
	if candidate.ProfileArtifact != nil {
		return candidate.ProfileArtifact.ArtifactID, candidate.ProfileArtifact.Version
	}
	return "", ""
}
