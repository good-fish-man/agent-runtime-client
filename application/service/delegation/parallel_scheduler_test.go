package delegation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

func TestParallelSchedulerRunsIndependentBranchesBeforeDependency(t *testing.T) {
	plan := parallelTestPlan(t, 3, []dso.SpecialistTaskNode{
		parallelTestNode("a", nil, dso.FailureRetry, 1),
		parallelTestNode("b", nil, dso.FailureRetry, 1),
		parallelTestNode("c", nil, dso.FailureRetry, 1),
		parallelTestNode("evidence", []string{"a", "b", "c"}, dso.FailureUsePartial, 1),
	})
	started := make(chan string, 4)
	release := make(chan struct{})
	var active atomic.Int32
	var peak atomic.Int32
	executor := ParallelBranchExecutorFunc(func(ctx context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- request.Node.NodeID
		if request.Node.NodeID != "evidence" {
			select {
			case <-release:
			case <-ctx.Done():
				return ParallelBranchResult{}, ctx.Err()
			}
			return parallelSuccessfulResult(request, nil), nil
		}
		if len(request.DependencyResults) != 3 {
			return ParallelBranchResult{}, fmt.Errorf("dependency results = %d", len(request.DependencyResults))
		}
		return parallelSuccessfulResult(request, []ParallelClaim{{Key: "answer", Statement: "answer", Value: "supported", EvidenceRefs: []string{"evidence-evidence"}}}), nil
	})

	resultCh := make(chan ParallelScheduleResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := NewParallelScheduler().Run(context.Background(), plan, executor, nil)
		resultCh <- result
		errCh <- err
	}()
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case id := <-started:
			if id == "evidence" {
				t.Fatal("dependent node started before its dependencies completed")
			}
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for three concurrent roots; saw %v", seen)
		}
	}
	close(release)
	select {
	case id := <-started:
		if id != "evidence" {
			t.Fatalf("unexpected fourth node %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("dependent evidence node did not start")
	}
	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("parallel run failed: %v", err)
	}
	if result.PeakParallelism != 3 || peak.Load() != 3 {
		t.Fatalf("peak parallelism = %d / %d, want 3", result.PeakParallelism, peak.Load())
	}
	if result.Aggregate.Status != dso.ParallelPlanCompleted {
		t.Fatalf("aggregate status = %s", result.Aggregate.Status)
	}
}

func TestParallelSchedulerReducesConcurrencyAfterRateLimit(t *testing.T) {
	node := parallelTestNode("rate-limited", nil, dso.FailureRetry, 2)
	plan := parallelTestPlan(t, 3, []dso.SpecialistTaskNode{node})
	var attempts atomic.Int32
	executor := ParallelBranchExecutorFunc(func(_ context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
		if attempts.Add(1) == 1 {
			return ParallelBranchResult{Usage: dso.BudgetAmount{Tokens: 10}}, &ProviderRateLimitError{Provider: "test", Err: errors.New("429")}
		}
		return parallelSuccessfulResult(request, []ParallelClaim{{Key: "answer", Statement: "answer", Value: "ok", EvidenceRefs: []string{"evidence-rate-limited"}}}), nil
	})
	result, err := NewParallelScheduler().Run(context.Background(), plan, executor, nil)
	if err != nil {
		t.Fatalf("parallel run failed: %v", err)
	}
	if attempts.Load() != 2 || result.RateLimitReductions != 1 || result.EffectiveParallelism != 2 {
		t.Fatalf("attempts=%d reductions=%d effective=%d", attempts.Load(), result.RateLimitReductions, result.EffectiveParallelism)
	}
}

func TestParallelSchedulerAppliesReplacementPartialAndUserPolicies(t *testing.T) {
	tests := []struct {
		name       string
		node       dso.SpecialistTaskNode
		executor   ParallelBranchExecutorFunc
		wantStatus string
		wantCalls  int32
	}{
		{
			name: "replacement", node: func() dso.SpecialistTaskNode {
				node := parallelTestNode("node", nil, dso.FailureReplace, 2)
				node.ReplacementRoles = []string{"backup"}
				return node
			}(),
			executor: func(_ context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
				if request.Attempt == 1 {
					return ParallelBranchResult{}, errors.New("primary unavailable")
				}
				if request.EffectiveRole != "backup" {
					return ParallelBranchResult{}, fmt.Errorf("replacement role = %q", request.EffectiveRole)
				}
				return parallelSuccessfulResult(request, nil), nil
			},
			wantStatus: dso.ParallelNodeCompleted, wantCalls: 2,
		},
		{
			name: "partial", node: parallelTestNode("node", nil, dso.FailureUsePartial, 1),
			executor: func(_ context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
				result := parallelSuccessfulResult(request, []ParallelClaim{{Key: "answer", Statement: "answer", Value: "partial", EvidenceRefs: []string{"evidence-node"}}})
				return result, errors.New("source timeout")
			},
			wantStatus: dso.ParallelNodePartial, wantCalls: 1,
		},
		{
			name: "ask user", node: parallelTestNode("node", nil, dso.FailureAskUser, 1),
			executor: func(context.Context, ParallelBranchRequest) (ParallelBranchResult, error) {
				return ParallelBranchResult{}, errors.New("ambiguous target")
			},
			wantStatus: dso.ParallelNodeWaitingUser, wantCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			executor := ParallelBranchExecutorFunc(func(ctx context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
				calls.Add(1)
				return test.executor(ctx, request)
			})
			result, err := NewParallelScheduler().Run(context.Background(), parallelTestPlan(t, 1, []dso.SpecialistTaskNode{test.node}), executor, nil)
			if err != nil {
				t.Fatalf("parallel run failed: %v", err)
			}
			if got := result.Branches[test.node.NodeID].Status; got != test.wantStatus {
				t.Fatalf("status = %q, want %q", got, test.wantStatus)
			}
			if calls.Load() != test.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), test.wantCalls)
			}
		})
	}
}

func TestParallelSchedulerRejectsBranchOverspend(t *testing.T) {
	node := parallelTestNode("overspend", nil, dso.FailureUsePartial, 1)
	plan := parallelTestPlan(t, 1, []dso.SpecialistTaskNode{node})
	executor := ParallelBranchExecutorFunc(func(_ context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
		result := parallelSuccessfulResult(request, []ParallelClaim{{Key: "answer", Statement: "answer", Value: "untrusted", EvidenceRefs: []string{"evidence-overspend"}}})
		result.Usage.Tokens = request.Node.BudgetRequest.Tokens + 1
		return result, nil
	})
	result, err := NewParallelScheduler().Run(context.Background(), plan, executor, nil)
	if err != nil {
		t.Fatalf("parallel run failed: %v", err)
	}
	if got := result.Branches[node.NodeID].Status; got != dso.ParallelNodeBudgetRejected {
		t.Fatalf("status = %s", got)
	}
	if len(result.Aggregate.Claims) != 0 {
		t.Fatalf("budget-rejected claims leaked into aggregate: %+v", result.Aggregate.Claims)
	}
}

func TestParallelSchedulerPropagatesParentCancellation(t *testing.T) {
	plan := parallelTestPlan(t, 2, []dso.SpecialistTaskNode{
		parallelTestNode("a", nil, dso.FailureRetry, 1),
		parallelTestNode("b", nil, dso.FailureRetry, 1),
	})
	started := make(chan struct{}, 2)
	var stopped atomic.Int32
	executor := ParallelBranchExecutorFunc(func(ctx context.Context, _ ParallelBranchRequest) (ParallelBranchResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		stopped.Add(1)
		return ParallelBranchResult{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewParallelScheduler().Run(ctx, plan, executor, nil)
		done <- err
	}()
	for range plan.Nodes {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("branch did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not drain within one second")
	}
	if stopped.Load() != int32(len(plan.Nodes)) {
		t.Fatalf("stopped branches = %d", stopped.Load())
	}
}

func TestParallelSchedulerStopsWhenProgressCannotBePersisted(t *testing.T) {
	plan := parallelTestPlan(t, 1, []dso.SpecialistTaskNode{parallelTestNode("a", nil, dso.FailureRetry, 1)})
	executed := atomic.Bool{}
	executor := ParallelBranchExecutorFunc(func(context.Context, ParallelBranchRequest) (ParallelBranchResult, error) {
		executed.Store(true)
		return ParallelBranchResult{}, nil
	})
	sink := ParallelProgressSinkFunc(func(context.Context, ParallelProgress) error { return errors.New("database unavailable") })
	_, err := NewParallelScheduler().Run(context.Background(), plan, executor, sink)
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("progress persistence error was lost: %v", err)
	}
	if executed.Load() {
		t.Fatal("branch executed after its RUNNING state failed to persist")
	}
}

func TestAggregateParallelResultsRetainsConflictAndDeduplicatesEvidence(t *testing.T) {
	plan := parallelTestPlan(t, 2, []dso.SpecialistTaskNode{
		parallelTestNode("a", nil, dso.FailureUsePartial, 1),
		parallelTestNode("b", nil, dso.FailureUsePartial, 1),
	})
	branches := map[string]ParallelBranchResult{
		"a": {ResultID: "result-a", Status: dso.ParallelNodeCompleted, Claims: []ParallelClaim{{Key: "release.date", Statement: "Release date", Value: "2026-08-20", EvidenceRefs: []string{"https://example.com/release?utm_source=a"}}}, Evidence: []dso.AggregateEvidence{{EvidenceRef: "evidence-a", URL: "https://example.com/release?utm_source=a"}}, Usage: dso.BudgetAmount{Tokens: 100}, CoordinationTokens: 10},
		"b": {ResultID: "result-b", Status: dso.ParallelNodeCompleted, Claims: []ParallelClaim{{Key: "release.date", Statement: "Release date", Value: "2026-08-21", EvidenceRefs: []string{"evidence-b"}}}, Evidence: []dso.AggregateEvidence{{EvidenceRef: "evidence-b", URL: "https://example.com/release"}}, Usage: dso.BudgetAmount{Tokens: 100}, CoordinationTokens: 10},
	}
	result := AggregateParallelResults(plan, branches, time.Now(), nil)
	if err := result.Validate(); err != nil {
		t.Fatalf("aggregate invalid: %v", err)
	}
	if len(result.Evidence) != 1 || result.DuplicateFetchRatio != .5 {
		t.Fatalf("evidence=%d duplicate_ratio=%v", len(result.Evidence), result.DuplicateFetchRatio)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != dso.AggregateClaimConflicting || len(result.Claims[0].Alternatives) != 2 {
		t.Fatalf("conflict was not retained: %+v", result.Claims)
	}
	if result.CoordinationTokenRatio != .1 {
		t.Fatalf("coordination ratio = %v", result.CoordinationTokenRatio)
	}
}

func TestParallelEvidenceCoverageComparison(t *testing.T) {
	nodes := []dso.SpecialistTaskNode{
		parallelTestNode("official", nil, dso.FailureUsePartial, 1),
		parallelTestNode("independent", nil, dso.FailureUsePartial, 1),
		parallelTestNode("recent", nil, dso.FailureUsePartial, 1),
	}
	plan := parallelTestPlan(t, 3, nodes)
	plan.MinimumEvidencePerClaim = 2
	plan.DefinitionHash = parallelPlanHash(t, plan)
	branches := make(map[string]ParallelBranchResult, len(nodes))
	for _, node := range nodes {
		request := ParallelBranchRequest{Node: node}
		branches[node.NodeID] = parallelSuccessfulResult(request, []ParallelClaim{{Key: "recommendation", Statement: "Recommendation", Value: "use supported option", EvidenceRefs: []string{"evidence-" + node.NodeID}}})
		branch := branches[node.NodeID]
		branch.Status = dso.ParallelNodeCompleted
		branch.Usage.Tokens = 100
		branch.CoordinationTokens = 5
		branches[node.NodeID] = branch
	}
	result := AggregateParallelResults(plan, branches, time.Now(), nil)
	if err := result.Validate(); err != nil {
		t.Fatalf("aggregate invalid: %v", err)
	}
	const singleAgentEvidenceCoverage = 1
	if len(result.Claims) != 1 || result.Claims[0].Status != dso.AggregateClaimSupported || len(result.Claims[0].EvidenceRefs) <= singleAgentEvidenceCoverage {
		t.Fatalf("parallel evidence did not improve baseline: %+v", result.Claims)
	}
	if result.DuplicateFetchRatio >= .15 || result.CoordinationTokenRatio >= .25 {
		t.Fatalf("parallel overhead outside gate: duplicate=%v coordination=%v", result.DuplicateFetchRatio, result.CoordinationTokenRatio)
	}
}

func parallelTestPlan(t *testing.T, parallelism int, nodes []dso.SpecialistTaskNode) dso.ParallelSpecialistPlan {
	t.Helper()
	total := dso.BudgetAmount{}
	for _, node := range nodes {
		total = total.Add(node.BudgetRequest)
	}
	plan := dso.ParallelSpecialistPlan{
		Schema: dso.Schema, ParallelPlanID: "parallel-plan-test", OwnerID: "owner-test", GoalRef: "goal-test", TaskStepRef: "task-test",
		MaxParallelism: parallelism, MaxRuns: maxTestInt(parallelism, len(nodes)), MinimumEvidencePerClaim: 1,
		TotalBudget: total, Nodes: nodes, CreatedAt: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}
	plan.DefinitionHash = parallelPlanHash(t, plan)
	if err := plan.Validate(); err != nil {
		t.Fatalf("test plan invalid: %v", err)
	}
	return plan
}

func parallelTestNode(id string, dependencies []string, strategy string, maxAttempts int) dso.SpecialistTaskNode {
	return dso.SpecialistTaskNode{
		NodeID: id, Role: "research", TaskRef: "task://" + id, DependsOn: dependencies,
		BudgetRequest:   dso.BudgetAmount{Tokens: 100, Queries: 1, Pages: 2, WallClockMS: 1000},
		FailureStrategy: strategy, MaxAttempts: maxAttempts, OutputSchemaRef: "schema://claims",
	}
}

func parallelSuccessfulResult(request ParallelBranchRequest, claims []ParallelClaim) ParallelBranchResult {
	ref := "evidence-" + request.Node.NodeID
	return ParallelBranchResult{
		NodeID: request.Node.NodeID, Role: request.EffectiveRole, ResultID: "result-" + request.Node.NodeID,
		Claims: claims, Evidence: []dso.AggregateEvidence{{EvidenceRef: ref, URL: "https://example.com/" + request.Node.NodeID}},
		Usage: dso.BudgetAmount{Tokens: 20, Queries: 1, Pages: 1, WallClockMS: 10},
	}
}

func parallelPlanHash(t *testing.T, plan dso.ParallelSpecialistPlan) string {
	t.Helper()
	digest, err := dso.ParallelSpecialistPlanDefinitionHash(plan)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func maxTestInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
