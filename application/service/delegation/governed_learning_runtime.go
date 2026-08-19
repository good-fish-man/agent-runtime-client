package delegation

import (
	"context"
	"strings"

	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

func (s *ExecutionService) applyGovernedLearningRoute(ctx context.Context, input *runtimeentity.RunInput, baseline RouteDecision) RouteDecision {
	if s == nil || s.learning == nil || input == nil || baseline.Route == RouteSpecialist || directAction.MatchString(strings.TrimSpace(input.Prompt)) {
		return baseline
	}
	ownerID := contextString(input.Context, "user_id")
	if ownerID == "" {
		return baseline
	}
	taskID := firstNonEmpty(contextString(input.Context, "task_id"), contextString(input.Context, "goal_id"), "route-evaluation")
	resolved, err := s.learning.Resolve(ctx, ownerID, dso.LearningCandidateDelegationPolicy, "low", taskID)
	if err != nil {
		log.Warnf(ctx, "governed delegation policy resolution failed; using rule baseline: %v", err)
		return baseline
	}
	if resolved.Candidate == nil || resolved.Candidate.PolicyArtifact == nil {
		return baseline
	}
	for _, rule := range resolved.Candidate.PolicyArtifact.Rules {
		if !containsLearningRef(rule.TaskClasses, "research") || !researchSignals.MatchString(input.Prompt) {
			continue
		}
		requested := append([]string(nil), rule.RequiredCapabilities...)
		if len(requested) == 0 {
			requested = []string{"internet.fetch", "internet.search"}
		}
		return RouteDecision{
			Route:                 RouteSpecialist,
			Reasons:               append(append([]string(nil), baseline.Reasons...), "governed_learning:"+resolved.Candidate.CandidateID),
			RequestedCapabilities: requested,
		}
	}
	return baseline
}
