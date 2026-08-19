package delegation

import (
	"testing"

	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	legacy "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"
)

func TestAdaptLegacyTaskProducesSubmittedProposalWithoutExecutionState(t *testing.T) {
	goal := legacy.PersistentGoal{
		GoalID: "goal-1", OwnerID: "owner-1", Revision: 3,
		Constraints:     []string{"do not modify external state"},
		SuccessCriteria: []legacy.SuccessCriterion{{CriterionID: "criterion-1", Description: "cite two independent sources", Required: true}},
	}
	task := legacy.SpecialistTask{
		TaskID: "task-1", GoalID: "goal-1", Specialist: legacy.SpecialistResearch,
		Objective: "compare the candidate systems", RequiredCapabilities: []string{"internet.search", "internet.fetch"},
		WorldSliceRefs: []string{"world-slice-1"}, Budget: legacy.TaskBudget{MaxTokens: 5000, MaxDurationMS: 60000, MaxSearchQueries: 5, MaxPages: 10},
	}
	proposal, err := AdaptLegacyTask(goal, task, orchestratorNow)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != dso.ProposalSubmitted || proposal.DraftSubagentSpec.DelegationPolicy.MayDelegate {
		t.Fatalf("adapter bypassed governed admission: %+v", proposal)
	}
	if proposal.DraftOutcome.DelegatedEffectClauses[0] != task.Objective || proposal.DraftSubagentSpec.BudgetRequest.Tokens != task.Budget.MaxTokens {
		t.Fatalf("adapter lost task semantics: %+v", proposal)
	}
	if err := proposal.Validate(); err != nil {
		t.Fatal(err)
	}
}
