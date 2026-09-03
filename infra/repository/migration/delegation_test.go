package migration

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
)

func TestInitTablesCreatesDurableDelegationAuthoritySchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	store := data.New(db)
	if err := InitTables(context.Background(), store, BootstrapAdmin{}); err != nil {
		t.Fatal(err)
	}
	if err := InitTables(context.Background(), store, BootstrapAdmin{}); err != nil {
		t.Fatalf("delegation migration is not idempotent: %v", err)
	}
	for name, table := range map[string]any{
		"proposal": &po.Proposal{}, "decision": &po.Decision{}, "delegated_outcome": &po.DelegatedOutcome{},
		"subagent_spec": &po.SubagentSpec{}, "invocation_manifest": &po.InvocationManifest{},
		"subagent_run": &po.Run{}, "subagent_attempt": &po.Attempt{}, "decision_turn": &po.DecisionTurn{},
		"model_invocation": &po.ModelInvocation{}, "budget_account": &po.BudgetAccount{},
		"action_proposal": &po.ActionProposal{}, "plan_candidate": &po.PlanCandidate{},
		"execution_context": &po.ExecutionContext{}, "action_policy_decision": &po.ActionPolicyDecision{},
		"action_plan_run": &po.ActionPlanRun{}, "governed_action_attempt": &po.GovernedActionAttempt{},
		"action_verification": &po.ActionVerification{},
		"learning_preference": &po.LearningPreference{}, "learning_candidate": &po.LearningCandidate{},
		"learning_evaluation": &po.LearningEvaluation{}, "learning_review": &po.LearningReview{},
		"learning_rollout": &po.LearningRollout{}, "learning_benchmark": &po.LearningBenchmark{},
		"budget_reservation": &po.BudgetReservation{}, "resource_lease": &po.ResourceLease{},
		"candidate_result": &po.CandidateResult{}, "event": &po.Event{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("W3 delegation table %s was not created", name)
		}
	}
}
