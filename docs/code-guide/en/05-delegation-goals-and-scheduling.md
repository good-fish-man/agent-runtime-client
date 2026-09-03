# 05. Delegation, Goals, and Scheduling

[Guide index](README.md) | [简体中文](../zh-CN/05-delegation-goals-and-scheduling.md)

## Purpose

This part allows Athena to divide work among temporary specialists and to continue durable multi-step goals without keeping the frontend open. It is intentionally governed: a sub-Agent is not a shortcut around policy, budget, capability, evidence, or verification.

## Durable Specialist Delegation

[`application/service/delegation`](../../../application/service/delegation) owns dynamic specialist execution. The normal chain is:

```text
Task intent
  -> DelegatedOutcomeSpec
  -> DelegationProposal
  -> DelegationDecision
  -> immutable specialist specification/artifacts
  -> durable run and attempt
  -> normal Runtime execution chain
  -> evidence and VerificationResult
  -> aggregated result
```

Delegation is a control-plane decision. Runtime executes the admitted specialist but does not invent an ungoverned executable identity.

## Same Execution Semantics

A specialist must use the same frozen execution chain as the main Agent:

```text
Outcome -> grounding -> plan -> policy -> run -> action attempt
        -> observation -> verification -> experience
```

There is no simplified sub-Agent execution path. Browser or desktop effects still require the real device Hub and Observation evidence.

## Context and Capability Isolation

[`context_builder.go`](../../../application/service/delegation/context_builder.go) constructs a least-privilege specialist context. It removes unrelated conversation, files, knowledge, Skills, MCP/CLI/A2A declarations, internal sub-Agent structures, and visual inputs unless they are explicitly required.

The specialist receives a bounded `CapabilityView`, not the entire registry. Sensitive values are redacted before persistence or delegation.

Specialists cannot recursively create more specialists. The parent orchestration authority remains responsible for planning and aggregation.

## Policy, Budget, and Artifacts

Delegation records proposal and decision separately. The decision binds policy version, context/evidence hashes, risk, actor, admitted capabilities, and expiry.

Execution reserves budget before work begins and commits/releases it at terminal state. [`artifact_resolver.go`](../../../application/service/delegation/artifact_resolver.go) resolves immutable approved Skills/Strategies/build artifacts for the invocation.

## Durable Authority and Recovery

[`orchestrator.go`](../../../application/service/delegation/orchestrator.go) is the sole durable authority for delegation runs. It manages leader election, leases, fencing, heartbeats, attempts, outbox events, cancellation, and recovery.

Late results from an expired/replaced lease are rejected. Recovery may resume or retry only when policy, idempotency, and side-effect evidence allow it.

## Parallel Specialists

The parallel scheduler executes an explicit DAG, not an unbounded fan-out:

- dependencies determine readiness;
- concurrency and budget are bounded;
- independent branches may run together;
- branch failure policy controls partial results or replacement;
- aggregation deduplicates evidence and reports contradictions; and
- user/approval waits propagate instead of being hidden.

The relevant files are `parallel_scheduler.go`, `parallel_execution.go`, and `parallel_aggregator.go`.

## Ad-Hoc Specialists

Temporary specialists are allowed only through the same proposal, policy, capability, budget, and audit path. They are declarative specifications, not generated code that executes directly.

## Governed Delegation Learning

Completed delegated outcomes can become evidence for a learning candidate. The learning path is separate from online execution:

```text
delegated experiences
  -> candidate
  -> offline evaluation
  -> human review
  -> shadow
  -> canary
  -> promotion or disable
```

One successful run cannot silently become a production Skill.

The W1-W7 design and acceptance documents are available in [`docs`](../../) as `dso-w*.md` and matching Chinese versions.

## Durable Goals

[`application/service/orchestration`](../../../application/service/orchestration) owns long-running goals and task graphs. A Goal contains a finite plan, task dependencies, revision, budget, schedule/approval state, and checkpoints.

The application service creates and transitions Goals. The [`Supervisor`](../../../application/service/orchestration/supervisor.go) claims runnable tasks, executes them through Runtime or a compatible device, records results, and advances the graph.

Key guarantees:

- a Goal continues without an open frontend;
- optimistic revisions prevent lost updates;
- stable execution IDs make retries idempotent;
- pause cancels active work and rejects late results;
- checkpoint hashes detect corrupted resume state;
- device actions need real Observation evidence; and
- task/world slices remain owner-scoped.

## Scheduled Tasks

[`application/service/scheduledtask`](../../../application/service/scheduledtask) owns recurring user jobs, approval decisions, execution history, and the polling control plane.

Schedule triggers use stable slot identities to avoid duplicate firing after restart. Internal creation routes are protected by service authentication; user-facing list/update/delete/approval routes are authenticated and owner-scoped.

Scheduled tasks and durable Goals are related but not identical: a schedule decides *when* to create/activate work, while the Goal task graph controls *how* durable multi-step work proceeds.

## Read These Files First

| File | Purpose |
| --- | --- |
| [`application/service/delegation/execution.go`](../../../application/service/delegation/execution.go) | online specialist routing and execution |
| [`application/service/delegation/orchestrator.go`](../../../application/service/delegation/orchestrator.go) | durable authority and recovery |
| [`application/service/delegation/context_builder.go`](../../../application/service/delegation/context_builder.go) | least-privilege context |
| [`application/service/delegation/policy.go`](../../../application/service/delegation/policy.go) | delegation policy |
| [`domain/entity/delegation`](../../../domain/entity/delegation) | durable delegation model |
| [`application/service/orchestration/service.go`](../../../application/service/orchestration/service.go) | Goal use cases |
| [`application/service/orchestration/supervisor.go`](../../../application/service/orchestration/supervisor.go) | background Goal runner |
| [`application/service/scheduledtask`](../../../application/service/scheduledtask) | recurring task control plane |

## Change Checklist

- Does a specialist still traverse the normal policy/action/observation/verification chain?
- Is its context and capability set minimal and explicit?
- Are proposal, decision, run, and attempt separate durable concepts?
- Are budgets reserved transactionally and terminal outcomes idempotent?
- Can restart recovery prove whether a side effect already occurred?
- Do Goal and schedule transitions reject stale revisions and duplicate slots?
