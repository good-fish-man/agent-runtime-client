package delegation

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

func TestExecutionServicePersistsAndStreamsParallelSpecialistDAG(t *testing.T) {
	service, runtime, db := newExecutionFixture(t, true)
	input := executionInput("Compare Isaac Sim, Gazebo and AWSIM using current evidence and sources")
	input.Context[ContextParallelSpecialists] = true
	var mu sync.Mutex
	var events []*runtimeentity.StreamEvent
	handled, err := service.MaybeRunStream(context.Background(), input, func(event *runtimeentity.StreamEvent) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		return nil
	})
	if err != nil || !handled {
		t.Fatalf("parallel run handled=%v err=%v", handled, err)
	}
	if runtime.calls.Load() != 4 {
		t.Fatalf("specialist runtime calls = %d, want 4", runtime.calls.Load())
	}
	for _, test := range []struct {
		name  string
		model any
		want  int64
	}{
		{"parallel plan", &po.ParallelPlan{}, 1}, {"parallel nodes", &po.ParallelNode{}, 4},
		{"parallel aggregate", &po.ParallelAggregate{}, 1}, {"subagent runs", &po.Run{}, 4},
		{"model invocations", &po.ModelInvocation{}, 4},
	} {
		var count int64
		if countErr := db.Model(test.model).Count(&count).Error; countErr != nil || count != test.want {
			t.Fatalf("%s count=%d want=%d err=%v", test.name, count, test.want, countErr)
		}
	}
	var planRow po.ParallelPlan
	if err := db.First(&planRow).Error; err != nil {
		t.Fatal(err)
	}
	var plan dso.ParallelSpecialistPlan
	if err := json.Unmarshal([]byte(planRow.Content), &plan); err != nil {
		t.Fatalf("decode persisted plan: %v", err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("persisted plan invalid: %v", err)
	}
	var aggregateRow po.ParallelAggregate
	if err := db.First(&aggregateRow).Error; err != nil {
		t.Fatal(err)
	}
	var aggregate dso.ParallelAggregateResult
	if err := json.Unmarshal([]byte(aggregateRow.Content), &aggregate); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.Validate(); err != nil {
		t.Fatalf("persisted aggregate invalid: %v", err)
	}
	if aggregate.Status != dso.ParallelPlanCompleted || len(aggregate.Claims) != 1 || aggregate.Claims[0].Status != dso.AggregateClaimSupported {
		t.Fatalf("aggregate did not satisfy evidence gate: %+v", aggregate)
	}
	var manifests []po.InvocationManifest
	if err := db.Order("manifest_id ASC").Find(&manifests).Error; err != nil {
		t.Fatal(err)
	}
	manifestText := ""
	for _, manifest := range manifests {
		manifestText += manifest.Content
	}
	for _, ref := range []string{parallelOfficialProfileRef, parallelIndependentProfileRef, parallelRecencyProfileRef, parallelEvidenceProfileRef} {
		if !strings.Contains(manifestText, ref) {
			t.Fatalf("manifest does not contain specialist profile %q", ref)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	progress, done, branchDeltas := 0, 0, 0
	var doneEvent *runtimeentity.DoneEvent
	for _, event := range events {
		switch event.Type {
		case runtimeentity.StreamTypeProgress:
			progress++
			if event.Progress == nil || event.Progress.ActionID != plan.ParallelPlanID || event.Progress.State["nodes"] == nil {
				t.Fatalf("progress lacks DAG identity: %+v", event.Progress)
			}
		case runtimeentity.StreamTypeDone:
			done++
			doneEvent = event.Done
		case runtimeentity.StreamTypeDelta:
			branchDeltas++
		}
	}
	if progress < 8 || done != 1 || branchDeltas != 0 {
		t.Fatalf("stream contract progress=%d done=%d branch_deltas=%d", progress, done, branchDeltas)
	}
	if doneEvent == nil || doneEvent.PromptTokens != 480 || doneEvent.CompletionTokens != 160 || doneEvent.TotalTokens != 640 {
		t.Fatalf("done token accounting = %+v, want prompt=480 completion=160 total=640", doneEvent)
	}
}

func TestBuildParallelResearchPlanRejectsInsufficientBudget(t *testing.T) {
	_, err := buildParallelResearchPlan("owner", "goal", "task", dso.BudgetAmount{Tokens: 799, Queries: 4, Pages: 4, WallClockMS: 4000}, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), ErrParallelBudgetInsufficient.Error()) {
		t.Fatalf("insufficient budget was accepted: %v", err)
	}
}
