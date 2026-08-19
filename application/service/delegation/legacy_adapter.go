package delegation

import (
	"fmt"
	"strings"
	"time"

	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	legacy "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"
)

// AdaptLegacyTask translates an orchestration/v2 SpecialistTask into a DSO
// proposal. It intentionally stops at SUBMITTED: only the governed delegation
// policy and durable Orchestrator may accept the proposal or create a Run.
func AdaptLegacyTask(goal legacy.PersistentGoal, task legacy.SpecialistTask, createdAt time.Time) (dso.DelegationProposal, error) {
	if strings.TrimSpace(goal.GoalID) == "" || strings.TrimSpace(goal.OwnerID) == "" || strings.TrimSpace(task.TaskID) == "" {
		return dso.DelegationProposal{}, fmt.Errorf("legacy goal owner, goal id, and task id are required")
	}
	if task.GoalID != "" && task.GoalID != goal.GoalID {
		return dso.DelegationProposal{}, fmt.Errorf("legacy task belongs to a different goal")
	}
	createdAt = createdAt.UTC()
	verification := make([]string, 0, len(goal.SuccessCriteria))
	for _, criterion := range goal.SuccessCriteria {
		if value := strings.TrimSpace(criterion.Description); value != "" {
			verification = append(verification, value)
		}
	}
	inputHash, err := dso.Hash(struct {
		GoalID       string
		GoalRevision int64
		Task         legacy.SpecialistTask
	}{GoalID: goal.GoalID, GoalRevision: goal.Revision, Task: task})
	if err != nil {
		return dso.DelegationProposal{}, err
	}
	suffix := inputHash[:16]
	outcome := dso.DelegatedOutcomeSpec{
		DelegatedOutcomeID:       "legacy-outcome-" + suffix,
		ParentOutcomeRef:         "legacy-goal-" + goal.GoalID,
		TaskStepRef:              task.TaskID,
		TargetSpecRef:            "legacy-task-target-" + task.TaskID,
		DelegatedEffectClauses:   []string{strings.TrimSpace(task.Objective)},
		MustPreserve:             append([]string(nil), goal.Constraints...),
		VerificationRequirements: verification,
		ContributionType:         dso.ContributionSatisfy,
		CreatedAt:                createdAt,
	}
	if outcome.DelegatedEffectClauses[0] == "" {
		return dso.DelegationProposal{}, fmt.Errorf("legacy task objective is required")
	}
	outcome.DefinitionHash, err = dso.Hash(outcome)
	if err != nil {
		return dso.DelegationProposal{}, err
	}
	spec := dso.SubagentSpec{
		SubagentSpecID:        "legacy-spec-" + suffix,
		TaskStepRef:           task.TaskID,
		DelegatedOutcomeRef:   outcome.DelegatedOutcomeID,
		Role:                  strings.ToLower(strings.TrimSpace(task.Specialist)),
		RequestedCapabilities: append([]string(nil), task.RequiredCapabilities...),
		RequestedContextScope: dso.ContextScope{ContentRefs: append([]string(nil), task.WorldSliceRefs...), AllowedClasses: []string{dso.ClassInternal}, MaxBytes: 1 << 20},
		PermissionCeilingRef:  "legacy-goal-policy-" + goal.GoalID,
		RiskCeiling:           "medium",
		BudgetRequest: dso.BudgetAmount{
			Tokens: task.Budget.MaxTokens, Actions: int64(task.Budget.MaxActions), Queries: int64(task.Budget.MaxSearchQueries),
			Pages: int64(task.Budget.MaxPages), WallClockMS: task.Budget.MaxDurationMS,
		},
		OutputSchemaRef:  "athena.orchestration.v2.SpecialistResult",
		DelegationPolicy: dso.DelegationPolicy{MayDelegate: false, MaxDepth: 0},
		CreatedAt:        createdAt,
	}
	if spec.Role == "" {
		spec.Role = "specialist"
	}
	spec.DefinitionHash, err = dso.Hash(spec)
	if err != nil {
		return dso.DelegationProposal{}, err
	}
	proposal := dso.DelegationProposal{
		Schema: dso.Schema, ProposalID: "legacy-proposal-" + suffix, OwnerID: goal.OwnerID, GoalID: goal.GoalID,
		TaskStepRef: task.TaskID, DraftOutcome: outcome, DraftSubagentSpec: spec,
		RequestedCapabilitySet: append([]string(nil), task.RequiredCapabilities...),
		RequestedContextScope:  spec.RequestedContextScope,
		CostBenefitEstimate: dso.CostBenefitEstimate{
			ExpectedQualityGain: 0.5, CoordinationCost: 0.2,
			ExpectedLatencyMS: task.Budget.MaxDurationMS, ExpectedTokens: task.Budget.MaxTokens,
		},
		Reasons:   []string{"adapted from orchestration/v2 specialist task; execution remains disabled until governed admission"},
		InputHash: inputHash, Status: dso.ProposalSubmitted, Revision: 1,
		CreatedBy: "legacy-orchestration-adapter", CreatedAt: createdAt,
	}
	if err := proposal.Validate(); err != nil {
		return dso.DelegationProposal{}, fmt.Errorf("adapted legacy proposal is invalid: %w", err)
	}
	return proposal, nil
}
