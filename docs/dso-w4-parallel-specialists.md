# DSO-W4 Parallel Specialist DAG

Status: accepted locally on 2026-08-20

## Scope

DSO-W4 adds bounded parallel specialist execution without creating a second execution authority. Every branch still follows the frozen specialist chain from proposal through verification. The scheduler only coordinates immutable task nodes and durable results:

```text
ParallelSpecialistPlan
  -> dependency-aware scheduler
  -> Specialist Run A / B / C
  -> persisted branch observations and evidence
  -> Evidence Specialist Run D
  -> typed aggregate with conflicts preserved
  -> one final visible stream result
```

The initial research graph contains three independent source roles and one evidence-synthesis role. The synthesis node cannot start until all source nodes reach a terminal state. Parallel execution is opt-in through the run context; the production default remains the existing single-specialist path.

## Governance And Budget Rules

- `max_parallelism`, `max_runs`, per-node budgets, and the total plan budget are validated before execution.
- The scheduler never starts more workers than the effective parallelism limit.
- A provider rate-limit signal lowers effective parallelism for later work.
- Retries and replacement roles consume the same node budget and attempt limit.
- An insufficient parent budget is rejected before any branch is created.
- Cancellation drains running workers and records terminal cancellation rather than claiming success.
- A branch cannot start if its `RUNNING` transition cannot be persisted.
- Prompt and completion tokens are captured per runtime invocation and summed exactly in the final stream event.

## Evidence Aggregation

- Only completed or explicitly partial branch results may contribute evidence.
- Evidence references must resolve to collected evidence; unsupported claims remain unsupported.
- Canonical URLs remove fragments and tracking parameters before duplicate calculation.
- Contradictory values are retained as alternatives with a `CONFLICTING` status.
- The configured minimum evidence count is enforced per claim.
- Coordination-token and duplicate-fetch ratios are emitted as inspectable aggregate metrics.

## Durable State And UI

The control plane persists `os_dso_parallel_plan`, `os_dso_parallel_node`, and `os_dso_parallel_aggregate`. Node transitions use optimistic revisions and idempotent event keys. Completion stores the typed aggregate atomically with the terminal event.

The chat stream exposes the DAG identity, role, dependencies, node status, configured parallelism, and effective parallelism. The UI renders these as a live specialist execution graph while preserving exactly one final answer.

## Reproducible Acceptance Evidence

- Three independent roots reached a measured peak parallelism of three; the fourth node waited for all dependencies.
- Retry, replacement, partial-result, user-intervention, budget rejection, rate-limit reduction, cancellation, and progress-persistence failure paths are covered.
- A synthetic evidence comparison raised supported evidence coverage from one single-agent source to three independent sources.
- The acceptance fixture reports `0%` duplicate URL fetches and `5%` coordination-token overhead, below the `15%` and `25%` gates.
- A conflicting-source fixture preserves both alternatives instead of silently merging them.
- An integrated run persists one plan, four nodes, four complete specialist run chains, four model invocations, and one aggregate.
- The final integrated stream reports exact token accounting: `480` prompt, `160` completion, `640` total.
- The frontend receives durable progress events, no branch text deltas, and exactly one final `Done` event.
- `go test ./...` passed in `agent-runtime-client`.
- `go test ./draft/dso/v0alpha` passed in `athena-protocol`.
- `npm run lint` and `npm run build` passed in `frontend/agent-ui`.

## Exit Decision

DSO-W4 exits locally. The parallel path remains disabled by default until later replay and canary gates provide production evidence. DSO-W5 may begin.
