package delegation

import (
	"context"
	"errors"
	"testing"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
)

func TestParallelPlanLifecycleIsDurableAndIdempotent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	bundle := parallelPlanBundleFixture()
	if err := store.CreateParallelPlan(ctx, bundle); err != nil {
		t.Fatalf("create parallel plan: %v", err)
	}
	if err := store.CreateParallelPlan(ctx, bundle); err != nil {
		t.Fatalf("idempotent plan replay: %v", err)
	}
	transition := entity.ParallelNodeTransition{
		OwnerID: "owner-1", PlanID: "parallel-plan-1", NodeID: "official", ExpectedRevision: 1,
		Role: "official", Status: "COMPLETED", Attempt: 1, ResultID: "result-official", Content: `{"status":"COMPLETED"}`,
		UpdatedAt: testNow.Add(1), Event: parallelEvent("parallel-node-completed", 2),
	}
	if err := store.TransitionParallelNode(ctx, transition); err != nil {
		t.Fatalf("transition node: %v", err)
	}
	if err := store.TransitionParallelNode(ctx, transition); err != nil {
		t.Fatalf("idempotent node replay: %v", err)
	}
	conflict := transition
	conflict.Status = "FAILED"
	if err := store.TransitionParallelNode(ctx, conflict); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("stale different transition error = %v", err)
	}
	completion := entity.ParallelPlanCompletion{
		OwnerID: "owner-1", PlanID: "parallel-plan-1", ExpectedRevision: 1, Status: "COMPLETED", CompletedAt: testNow.Add(2),
		Aggregate: entity.ParallelAggregate{AggregateID: "aggregate-1", PlanID: "parallel-plan-1", OwnerID: "owner-1", Status: "COMPLETED", Content: `{"status":"COMPLETED"}`, CreatedAt: testNow.Add(2)},
		Event:     parallelEvent("parallel-plan-completed", 3),
	}
	if err := store.CompleteParallelPlan(ctx, completion); err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	if err := store.CompleteParallelPlan(ctx, completion); err != nil {
		t.Fatalf("idempotent completion replay: %v", err)
	}
	plan, nodes, aggregate, err := store.FindParallelPlan(ctx, "owner-1", "parallel-plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.Status != "COMPLETED" || plan.Revision != 2 || len(nodes) != 2 || aggregate == nil || aggregate.AggregateID != "aggregate-1" {
		t.Fatalf("stored lifecycle is incomplete: plan=%+v nodes=%+v aggregate=%+v", plan, nodes, aggregate)
	}
}

func TestCreateParallelPlanRejectsCrossOwnerNodeAtomically(t *testing.T) {
	store, _ := newTestStore(t)
	bundle := parallelPlanBundleFixture()
	bundle.Nodes[1].OwnerID = "owner-2"
	if err := store.CreateParallelPlan(context.Background(), bundle); err == nil {
		t.Fatal("cross-owner plan was accepted")
	}
	plan, nodes, aggregate, err := store.FindParallelPlan(context.Background(), "owner-1", bundle.Plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if plan != nil || len(nodes) != 0 || aggregate != nil {
		t.Fatalf("failed transaction left partial data: plan=%+v nodes=%+v aggregate=%+v", plan, nodes, aggregate)
	}
}

func parallelPlanBundleFixture() entity.ParallelPlanBundle {
	return entity.ParallelPlanBundle{
		Plan: entity.ParallelPlan{
			PlanID: "parallel-plan-1", OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1", Status: "RUNNING",
			DefinitionHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Content: `{"parallel_plan_id":"parallel-plan-1"}`,
			Revision: 1, CreatedAt: testNow, UpdatedAt: testNow,
		},
		Nodes: []entity.ParallelNode{
			{PlanID: "parallel-plan-1", NodeID: "official", OwnerID: "owner-1", Role: "official", Status: "PENDING", Content: `{"node_id":"official"}`, Revision: 1, CreatedAt: testNow, UpdatedAt: testNow},
			{PlanID: "parallel-plan-1", NodeID: "evidence", OwnerID: "owner-1", Role: "evidence", Status: "WAITING_DEPENDENCY", Content: `{"node_id":"evidence"}`, Revision: 1, CreatedAt: testNow, UpdatedAt: testNow},
		},
		Event: parallelEvent("parallel-plan-created", 1),
	}
}

func parallelEvent(id string, sequence int64) entity.Event {
	return entity.Event{
		EventID: id, OwnerID: "owner-1", AggregateType: "parallel_plan", AggregateID: "parallel-plan-1",
		Sequence: sequence, Type: id, IdempotencyKey: "parallel-event-" + id,
		TraceID: "trace-1", CausationID: "task-1", Payload: `{}`, CreatedAt: testNow,
	}
}
