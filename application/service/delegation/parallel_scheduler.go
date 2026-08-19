package delegation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

const parallelCancellationDrain = 5 * time.Second

type ParallelClaim struct {
	Key          string
	Statement    string
	Value        string
	EvidenceRefs []string
}

type ParallelBranchResult struct {
	NodeID             string
	Role               string
	ResultID           string
	Status             string
	Content            string
	Claims             []ParallelClaim
	Evidence           []dso.AggregateEvidence
	Usage              dso.BudgetAmount
	PromptTokens       int64
	CompletionTokens   int64
	CoordinationTokens int64
	Error              string
	Attempt            int
	StartedAt          time.Time
	CompletedAt        time.Time
}

type ParallelBranchRequest struct {
	Plan              dso.ParallelSpecialistPlan
	Node              dso.SpecialistTaskNode
	EffectiveRole     string
	Attempt           int
	RemainingBudget   dso.BudgetAmount
	DependencyResults map[string]ParallelBranchResult
}

type ParallelBranchExecutor interface {
	ExecuteBranch(context.Context, ParallelBranchRequest) (ParallelBranchResult, error)
}

type ParallelBranchExecutorFunc func(context.Context, ParallelBranchRequest) (ParallelBranchResult, error)

func (f ParallelBranchExecutorFunc) ExecuteBranch(ctx context.Context, request ParallelBranchRequest) (ParallelBranchResult, error) {
	return f(ctx, request)
}

type ParallelProgress struct {
	PlanID                string
	NodeID                string
	Role                  string
	Status                string
	Attempt               int
	Running               int
	Completed             int
	Total                 int
	ConfiguredParallelism int
	EffectiveParallelism  int
	ResultID              string
	ErrorChain            string
	Message               string
	At                    time.Time
}

type ParallelProgressSink interface {
	OnParallelProgress(context.Context, ParallelProgress) error
}

type ParallelProgressSinkFunc func(context.Context, ParallelProgress) error

func (f ParallelProgressSinkFunc) OnParallelProgress(ctx context.Context, progress ParallelProgress) error {
	return f(ctx, progress)
}

type ParallelScheduleResult struct {
	Plan                 dso.ParallelSpecialistPlan
	Branches             map[string]ParallelBranchResult
	Aggregate            dso.ParallelAggregateResult
	PeakParallelism      int
	EffectiveParallelism int
	RateLimitReductions  int
	StartedAt            time.Time
	CompletedAt          time.Time
}

type ProviderRateLimitError struct {
	Provider   string
	RetryAfter time.Duration
	Err        error
}

func (e *ProviderRateLimitError) Error() string {
	if e == nil {
		return "provider rate limited"
	}
	if e.Provider == "" {
		return fmt.Sprintf("provider rate limited: %v", e.Err)
	}
	return fmt.Sprintf("provider %s rate limited: %v", e.Provider, e.Err)
}

func (e *ProviderRateLimitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type ParallelScheduler struct {
	now func() time.Time
}

func NewParallelScheduler() *ParallelScheduler {
	return &ParallelScheduler{now: func() time.Time { return time.Now().UTC() }}
}

type parallelNodeState struct {
	node          dso.SpecialistTaskNode
	status        string
	attempt       int
	effectiveRole string
	consumed      dso.BudgetAmount
	resultID      string
	errorChain    string
}

type parallelExecutionResult struct {
	nodeID string
	result ParallelBranchResult
	err    error
}

func (s *ParallelScheduler) Run(ctx context.Context, plan dso.ParallelSpecialistPlan, executor ParallelBranchExecutor, sink ParallelProgressSink) (ParallelScheduleResult, error) {
	result := ParallelScheduleResult{Plan: plan, Branches: make(map[string]ParallelBranchResult), EffectiveParallelism: plan.MaxParallelism}
	if s == nil || executor == nil {
		return result, fmt.Errorf("parallel scheduler requires an executor")
	}
	if err := plan.Validate(); err != nil {
		return result, fmt.Errorf("validate parallel plan: %w", err)
	}
	if sink == nil {
		sink = ParallelProgressSinkFunc(func(context.Context, ParallelProgress) error { return nil })
	}
	started := s.now()
	result.StartedAt = started
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	states := make(map[string]*parallelNodeState, len(plan.Nodes))
	for _, node := range plan.Nodes {
		status := dso.ParallelNodePending
		if len(node.DependsOn) > 0 {
			status = dso.ParallelNodeWaiting
		}
		states[node.NodeID] = &parallelNodeState{node: node, status: status, effectiveRole: node.Role}
	}
	completion := make(chan parallelExecutionResult, parallelCompletionCapacity(plan.Nodes))
	running, terminal, peak := 0, 0, 0
	effectiveParallelism := plan.MaxParallelism
	var cancelAt time.Time

	for terminal < len(states) {
		if runCtx.Err() != nil && cancelAt.IsZero() {
			cancelAt = s.now()
			cancel()
			for _, state := range states {
				if state.status == dso.ParallelNodePending || state.status == dso.ParallelNodeWaiting || state.status == dso.ParallelNodeRetrying {
					state.status = dso.ParallelNodeCancelled
					terminal++
					if err := emitParallelProgress(context.WithoutCancel(ctx), sink, plan, state, running, terminal, effectiveParallelism, "parent cancelled", s.now()); err != nil {
						return result, fmt.Errorf("record cancelled parallel node: %w", err)
					}
				}
			}
		}

		launched := false
		if runCtx.Err() == nil {
			for _, id := range sortedParallelNodeIDs(states) {
				if running >= effectiveParallelism {
					break
				}
				state := states[id]
				if state.status != dso.ParallelNodePending && state.status != dso.ParallelNodeWaiting && state.status != dso.ParallelNodeRetrying {
					continue
				}
				ready, blocked := parallelDependenciesReady(state.node, states)
				if blocked {
					status, message := dependencyFailureOutcome(state.node)
					state.status = status
					terminal++
					if err := emitParallelProgress(ctx, sink, plan, state, running, terminal, effectiveParallelism, message, s.now()); err != nil {
						return result, fmt.Errorf("record blocked parallel node: %w", err)
					}
					continue
				}
				if !ready {
					state.status = dso.ParallelNodeWaiting
					continue
				}
				remaining := subtractBudget(state.node.BudgetRequest, state.consumed)
				if budgetEmpty(remaining) {
					state.status = dso.ParallelNodeBudgetRejected
					terminal++
					if err := emitParallelProgress(ctx, sink, plan, state, running, terminal, effectiveParallelism, "branch budget exhausted", s.now()); err != nil {
						return result, fmt.Errorf("record budget-rejected parallel node: %w", err)
					}
					continue
				}
				state.attempt++
				state.status = dso.ParallelNodeRunning
				state.resultID = ""
				state.errorChain = ""
				running++
				if running > peak {
					peak = running
				}
				launched = true
				request := ParallelBranchRequest{
					Plan: plan, Node: state.node, EffectiveRole: state.effectiveRole,
					Attempt: state.attempt, RemainingBudget: remaining,
					DependencyResults: parallelDependencyResults(state.node, result.Branches),
				}
				if err := emitParallelProgress(ctx, sink, plan, state, running, terminal, effectiveParallelism, "branch started", s.now()); err != nil {
					return result, fmt.Errorf("record running parallel node: %w", err)
				}
				go func(nodeID string, request ParallelBranchRequest) {
					branch, err := executor.ExecuteBranch(runCtx, request)
					completion <- parallelExecutionResult{nodeID: nodeID, result: branch, err: err}
				}(id, request)
			}
		}

		if terminal >= len(states) {
			break
		}
		if running == 0 {
			if !launched {
				return result, fmt.Errorf("parallel scheduler made no progress")
			}
			continue
		}

		var completed parallelExecutionResult
		if !cancelAt.IsZero() {
			remaining := parallelCancellationDrain - s.now().Sub(cancelAt)
			if remaining <= 0 {
				terminal += markRunningNodesCancelled(states, result.Branches, s.now())
				running = 0
				break
			}
			timer := time.NewTimer(remaining)
			select {
			case completed = <-completion:
				timer.Stop()
			case <-timer.C:
				terminal += markRunningNodesCancelled(states, result.Branches, s.now())
				running = 0
				break
			}
		} else {
			select {
			case completed = <-completion:
			case <-ctx.Done():
				cancelAt = s.now()
				cancel()
				continue
			}
		}
		if completed.nodeID == "" {
			continue
		}
		running--
		state := states[completed.nodeID]
		completed.result.NodeID = completed.nodeID
		completed.result.Role = state.effectiveRole
		completed.result.Attempt = state.attempt
		if completed.result.CompletedAt.IsZero() {
			completed.result.CompletedAt = s.now()
		}
		state.consumed = state.consumed.Add(completed.result.Usage)
		if !state.consumed.FitsWithin(state.node.BudgetRequest) {
			completed.result.Status = dso.ParallelNodeBudgetRejected
			completed.result.Error = "branch reported usage beyond its reserved budget"
			state.status = dso.ParallelNodeBudgetRejected
			state.resultID = completed.result.ResultID
			state.errorChain = completed.result.Error
			result.Branches[completed.nodeID] = completed.result
			terminal++
			if err := emitParallelProgress(context.WithoutCancel(ctx), sink, plan, state, running, terminal, effectiveParallelism, completed.result.Error, s.now()); err != nil {
				return result, fmt.Errorf("record overspent parallel node: %w", err)
			}
			continue
		}

		var rateLimit *ProviderRateLimitError
		if errors.As(completed.err, &rateLimit) {
			if effectiveParallelism > 1 {
				effectiveParallelism--
				result.RateLimitReductions++
			}
			if state.attempt < state.node.MaxAttempts && runCtx.Err() == nil {
				state.status = dso.ParallelNodeRetrying
				state.errorChain = rateLimit.Error()
				if err := emitParallelProgress(ctx, sink, plan, state, running, terminal, effectiveParallelism, rateLimit.Error(), s.now()); err != nil {
					return result, fmt.Errorf("record rate-limited parallel node: %w", err)
				}
				continue
			}
		}
		if completed.err != nil {
			completed.result.Error = completed.err.Error()
			if retryParallelNode(state) && runCtx.Err() == nil {
				state.errorChain = completed.result.Error
				if err := emitParallelProgress(ctx, sink, plan, state, running, terminal, effectiveParallelism, completed.result.Error, s.now()); err != nil {
					return result, fmt.Errorf("record retrying parallel node: %w", err)
				}
				continue
			}
		}
		state.status = terminalParallelNodeStatus(state.node, completed.result, completed.err, runCtx.Err())
		state.resultID = completed.result.ResultID
		state.errorChain = completed.result.Error
		completed.result.Status = state.status
		result.Branches[completed.nodeID] = completed.result
		terminal++
		if err := emitParallelProgress(context.WithoutCancel(ctx), sink, plan, state, running, terminal, effectiveParallelism, completed.result.Error, s.now()); err != nil {
			return result, fmt.Errorf("record terminal parallel node: %w", err)
		}
	}

	result.PeakParallelism = peak
	result.EffectiveParallelism = effectiveParallelism
	result.CompletedAt = s.now()
	result.Aggregate = AggregateParallelResults(plan, result.Branches, result.CompletedAt, ctx.Err())
	if err := result.Aggregate.Validate(); err != nil {
		return result, fmt.Errorf("validate parallel aggregate: %w", err)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

func retryParallelNode(state *parallelNodeState) bool {
	if state == nil || state.attempt >= state.node.MaxAttempts {
		return false
	}
	switch state.node.FailureStrategy {
	case dso.FailureRetry:
		state.status = dso.ParallelNodeRetrying
		return true
	case dso.FailureReplace:
		if index := state.attempt - 1; index >= 0 && index < len(state.node.ReplacementRoles) {
			state.effectiveRole = state.node.ReplacementRoles[index]
			state.status = dso.ParallelNodeRetrying
			return true
		}
	}
	return false
}

func terminalParallelNodeStatus(node dso.SpecialistTaskNode, result ParallelBranchResult, err, contextErr error) string {
	if contextErr != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return dso.ParallelNodeCancelled
	}
	if err == nil {
		if result.Status == dso.ParallelNodePartial {
			return dso.ParallelNodePartial
		}
		return dso.ParallelNodeCompleted
	}
	if node.FailureStrategy == dso.FailureUsePartial && (len(result.Claims) > 0 || len(result.Evidence) > 0) {
		return dso.ParallelNodePartial
	}
	if node.FailureStrategy == dso.FailureAskUser {
		return dso.ParallelNodeWaitingUser
	}
	return dso.ParallelNodeFailed
}

func parallelDependenciesReady(node dso.SpecialistTaskNode, states map[string]*parallelNodeState) (ready, blocked bool) {
	for _, dependency := range node.DependsOn {
		state := states[dependency]
		if state == nil {
			return false, true
		}
		switch state.status {
		case dso.ParallelNodeCompleted, dso.ParallelNodePartial:
			continue
		case dso.ParallelNodeFailed, dso.ParallelNodeBudgetRejected, dso.ParallelNodeCancelled, dso.ParallelNodeWaitingUser:
			if node.FailureStrategy == dso.FailureUsePartial || node.FailureStrategy == dso.FailureAskUser {
				continue
			}
			return false, true
		default:
			return false, false
		}
	}
	return true, false
}

func dependencyFailureOutcome(node dso.SpecialistTaskNode) (string, string) {
	if node.FailureStrategy == dso.FailureAskUser {
		return dso.ParallelNodeWaitingUser, "dependency failed; user input required"
	}
	return dso.ParallelNodeFailed, "required dependency failed"
}

func parallelDependencyResults(node dso.SpecialistTaskNode, results map[string]ParallelBranchResult) map[string]ParallelBranchResult {
	values := make(map[string]ParallelBranchResult, len(node.DependsOn))
	for _, id := range node.DependsOn {
		if result, ok := results[id]; ok {
			values[id] = result
		}
	}
	return values
}

func sortedParallelNodeIDs(states map[string]*parallelNodeState) []string {
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func emitParallelProgress(ctx context.Context, sink ParallelProgressSink, plan dso.ParallelSpecialistPlan, state *parallelNodeState, running, completed, effective int, message string, at time.Time) error {
	return sink.OnParallelProgress(ctx, ParallelProgress{
		PlanID: plan.ParallelPlanID, NodeID: state.node.NodeID, Role: state.effectiveRole,
		Status: state.status, Attempt: state.attempt, Running: running, Completed: completed, Total: len(plan.Nodes),
		ConfiguredParallelism: plan.MaxParallelism, EffectiveParallelism: effective,
		ResultID: state.resultID, ErrorChain: state.errorChain, Message: message, At: at,
	})
}

func subtractBudget(total, consumed dso.BudgetAmount) dso.BudgetAmount {
	return dso.BudgetAmount{
		Tokens: maxInt64(0, total.Tokens-consumed.Tokens), MoneyMicros: maxInt64(0, total.MoneyMicros-consumed.MoneyMicros),
		Actions: maxInt64(0, total.Actions-consumed.Actions), Queries: maxInt64(0, total.Queries-consumed.Queries),
		Pages: maxInt64(0, total.Pages-consumed.Pages), ComputeMS: maxInt64(0, total.ComputeMS-consumed.ComputeMS),
		WallClockMS: maxInt64(0, total.WallClockMS-consumed.WallClockMS),
	}
}

func budgetEmpty(value dso.BudgetAmount) bool {
	return value.Tokens == 0 && value.MoneyMicros == 0 && value.Actions == 0 && value.Queries == 0 && value.Pages == 0 && value.ComputeMS == 0 && value.WallClockMS == 0
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func markRunningNodesCancelled(states map[string]*parallelNodeState, results map[string]ParallelBranchResult, at time.Time) int {
	count := 0
	for id, state := range states {
		if state.status != dso.ParallelNodeRunning {
			continue
		}
		state.status = dso.ParallelNodeCancelled
		results[id] = ParallelBranchResult{NodeID: id, Role: state.effectiveRole, Status: state.status, Attempt: state.attempt, Error: "cancellation drain timeout", CompletedAt: at}
		count++
	}
	return count
}

func parallelCompletionCapacity(nodes []dso.SpecialistTaskNode) int {
	capacity := 0
	for _, node := range nodes {
		capacity += node.MaxAttempts
	}
	if capacity < len(nodes) {
		return len(nodes)
	}
	return capacity
}
