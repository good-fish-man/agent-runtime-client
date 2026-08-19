package delegation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

func TestExclusiveActionLeaseHasOneCrossInstanceWriter(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for index := 0; index < 20; index++ {
		if err := store.CreateActionChain(ctx, testActionChain(index)); err != nil {
			t.Fatal(err)
		}
	}
	var successes atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			lease := entity.ResourceLease{
				LeaseID: fmt.Sprintf("lease-%d", index), OwnerID: "owner-1", RunID: fmt.Sprintf("plan-run-%d", index),
				ResourceRef: "browser://session/session-1/tab/tab-1", ResourceVersion: "page-v1",
				Mode: entity.LeaseExclusiveWrite, ActionAttemptID: fmt.Sprintf("action-attempt-%d", index),
				OwnerInstanceID: fmt.Sprintf("instance-%d", index), Status: entity.LeaseActive,
				AcquiredAt: testNow, HeartbeatAt: testNow, ExpiresAt: testNow.Add(time.Minute), Revision: 1,
			}
			err := store.AcquireActionLease(ctx, lease, "page-v1", "page-v1", testNow, event(fmt.Sprintf("lease-event-%d", index), fmt.Sprintf("action-attempt-%d", index), 2))
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, repository.ErrResourceBusy) {
				t.Errorf("unexpected lease error: %v", err)
			}
		}(index)
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("exclusive writer count=%d, want 1", successes.Load())
	}
}

func TestAcquireActionLeaseRejectsStaleResourceVersion(t *testing.T) {
	store, _ := newTestStore(t)
	chain := testActionChain(1)
	if err := store.CreateActionChain(context.Background(), chain); err != nil {
		t.Fatal(err)
	}
	lease := entity.ResourceLease{
		LeaseID: "lease-stale", OwnerID: "owner-1", RunID: chain.PlanRun.PlanRunID,
		ResourceRef: chain.Plan.ResourceRef, ResourceVersion: "page-v1", Mode: entity.LeaseExclusiveWrite,
		ActionAttemptID: chain.Attempt.ActionAttemptID, OwnerInstanceID: "instance-1", Status: entity.LeaseActive,
		AcquiredAt: testNow, HeartbeatAt: testNow, ExpiresAt: testNow.Add(time.Minute), Revision: 1,
	}
	err := store.AcquireActionLease(context.Background(), lease, "page-v1", "page-v2", testNow, event("lease-stale-event", chain.Attempt.ActionAttemptID, 2))
	if !errors.Is(err, repository.ErrResourceStale) {
		t.Fatalf("stale resource error=%v", err)
	}
}

func TestCompleteActionChainReturnsObservationToSameSpecialistAttempt(t *testing.T) {
	store, db := newTestStore(t)
	chain := testActionChain(1)
	chain.PlanRun.SubagentRunID = "specialist-run-1"
	chain.PlanRun.SubagentAttemptID = "specialist-attempt-1"
	if err := store.CreateActionChain(context.Background(), chain); err != nil {
		t.Fatal(err)
	}
	completion := entity.ActionCompletion{
		OwnerID: "owner-1", PlanRunID: chain.PlanRun.PlanRunID, ActionAttemptID: chain.Attempt.ActionAttemptID,
		AttemptStatus: entity.ActionSucceeded, PlanStatus: entity.PlanRunCompleted,
		ObservationID: "observation-1", ResourceVersionAfter: "page-v2",
		AttemptContent: `{"observation_id":"observation-1"}`, PlanContent: `{}`,
		RecordedAt: testNow.Add(time.Second), EndedAt: testNow.Add(time.Second),
		Event: event("action-completed-event", chain.Attempt.ActionAttemptID, 2),
	}
	if err := store.CompleteActionChain(context.Background(), completion); err != nil {
		t.Fatal(err)
	}
	var turn po.DecisionTurn
	if err := db.Where("attempt_id = ?", chain.PlanRun.SubagentAttemptID).Take(&turn).Error; err != nil {
		t.Fatal(err)
	}
	if turn.DecisionType != dso.DecisionReceiveObservation || turn.Content != completion.AttemptContent {
		t.Fatalf("observation turn was not attached to specialist attempt: %+v", turn)
	}
}

func testActionChain(index int) entity.ActionChain {
	suffix := fmt.Sprint(index)
	return entity.ActionChain{
		Proposal:         entity.ActionProposalRecord{ActionProposalID: "action-proposal-" + suffix, OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1", DecisionTurnID: "turn-1", Capability: "browser.action", Operation: "click", ResourceRef: "browser://session/session-1/tab/tab-1", ResourceVersion: "page-v1", InputHash: fmt.Sprintf("%064d", index+1), Content: `{}`, CreatedAt: testNow},
		Plan:             entity.PlanCandidateRecord{PlanCandidateID: "plan-" + suffix, OwnerID: "owner-1", TaskStepID: "task-1", ActionProposalID: "action-proposal-" + suffix, ResourceRef: "browser://session/session-1/tab/tab-1", ResourceVersion: "page-v1", DefinitionHash: fmt.Sprintf("%064d", index+2), Content: `{}`, CreatedAt: testNow},
		ExecutionContext: entity.ExecutionContextRecord{ExecutionContextID: "execution-" + suffix, OwnerID: "owner-1", TaskStepID: "task-1", ContentHash: fmt.Sprintf("%064d", index+3), Content: `{}`, CreatedAt: testNow},
		Policy:           entity.ActionPolicyDecisionRecord{PolicyDecisionID: "policy-" + suffix, OwnerID: "owner-1", PlanCandidateID: "plan-" + suffix, ActionProposalID: "action-proposal-" + suffix, WorldReadSetHash: fmt.Sprintf("%064d", index+4), InputHash: fmt.Sprintf("%064d", index+5), PolicyVersion: "v1", Decision: "allow", Content: `{}`, DecidedAt: testNow, ExpiresAt: testNow.Add(time.Minute)},
		PlanRun:          entity.ActionPlanRunRecord{PlanRunID: "plan-run-" + suffix, OwnerID: "owner-1", PlanCandidateID: "plan-" + suffix, ExecutionContextID: "execution-" + suffix, Status: entity.PlanRunRunning, Revision: 1, Content: `{}`, StartedAt: testNow},
		Attempt:          entity.GovernedActionAttemptRecord{ActionAttemptID: "action-attempt-" + suffix, OwnerID: "owner-1", PlanRunID: "plan-run-" + suffix, PlanCandidateID: "plan-" + suffix, PolicyDecisionID: "policy-" + suffix, ActionProposalID: "action-proposal-" + suffix, ResourceVersionBefore: "page-v1", Status: entity.ActionPolicyAllowed, Revision: 1, Content: `{}`, CreatedAt: testNow, UpdatedAt: testNow},
		Event:            event("action-chain-event-"+suffix, "action-attempt-"+suffix, 1),
	}
}
