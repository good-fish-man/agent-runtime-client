# Durable Goals and Supervisor v0.7

Athena v0.7 turns explicitly long-running work into a finite, durable task graph. A goal is owned by one user and one Agent, carries observable success criteria and hard budgets, and is checkpointed on every state transition.

## Execution model

1. Runtime recognizes only explicit background, cross-day, cross-device, or resume-later intent.
2. `PersistentGoalCreate` submits a declarative graph with at most 64 nodes, depth 4, and 8 concurrent specialists.
3. Runtime Client uses `POST /goals/planned` to persist a goal, its graph, and the first checkpoint atomically, so a two-step request cannot leave an orphan draft.
4. Supervisor routes server specialists directly and routes browser/desktop specialists only to an online device that advertises every required capability.
5. Runtime executes through the existing Control Hub. An external effect is confirmed only by a successful device Observation; its idempotency key is written to the next checkpoint.
6. Restart recovery converts orphan `RUNNING` tasks to `READY` while retaining confirmed effect keys and scoped world state.

Each task attempt has a new `execution_id`, while its `idempotency_scope` stays stable across retries. Results from an older attempt are rejected. Device Actions derive their idempotency key from the stable scope and canonical action content, so a restart cannot replay an already observed side effect merely because generated Action IDs changed.

Checkpoints form a SHA-256 predecessor chain. Loading one checkpoint verifies its content hash; listing history additionally verifies sequence continuity and every `previous_checksum` link. A corrupt or spliced chain cannot be used for resume.

## Scheduled work

Cron is only a trigger source. A slot creates the same durable Goal, Task, Specialist Run, Checkpoint, and audit records as interactive work. `schedule_id:slot` is unique, so retries or concurrent scheduler ticks cannot create duplicate occurrences. Trigger state records attempt count, retry delay, completion, reconciliation, and notification delivery.

Pre-execution approval is fail closed. Its ID is written into the first Checkpoint before the approval card is exposed. Public Goal resume rejects a pending approval; only the owner-scoped approval service can consume that exact ID. Rejection terminates that occurrence so it does not hold a schedule concurrency slot forever.

## Safety boundaries

- Graphs are finite and every task receives a positive allocation inside the goal budget.
- The Supervisor cannot create nested persistent goals, hidden sub-agents, executable code, or unbounded loops.
- `WAITING_USER`, approval, device-offline, deadline, and budget exhaustion are durable states, never fake success.
- Specialists receive only declared world-slice keys. Nested credentials, cookies, tokens, raw screenshots, and unrestricted device state are recursively removed or bounded before checkpointing.
- Every result has attributable producer and trace provenance. Runtime results must also bind a RunManifest, AgentBuild, and model configuration fingerprint. A device specialist can complete only after a successful Observation, and any confirmed external effect retains its Observation reference.

## Operations

```yaml
orchestration:
  enabled: true
  scan_interval_sec: 3
  max_concurrent_runs: 2
```

Environment overrides are `ARC_ORCHESTRATION_ENABLED`, `ARC_ORCHESTRATION_SCAN_INTERVAL_SEC`, and `ARC_ORCHESTRATION_MAX_CONCURRENT_RUNS`. User routes are under `/goals`; `POST /goals/planned` is the owner-scoped atomic creation route, while Runtime uses token-protected `/internal/goals`. Task start, result persistence, and manual checkpoint mutation also require an exact internal service token.

Useful read APIs are:

- `GET /goals/:id` for the current Goal, graph, runs, and latest Checkpoint.
- `GET /goals/:id/checkpoints` for verified checkpoint history.
- `GET /goals/schedule-triggers?schedule_id=...` for owner-scoped trigger and notification history.
- `GET /goals/:id/tasks/:taskID/world-slice` for the task-filtered, device-scoped world view.

The Inbox shows trigger origin, execution attempts, device routing, specialist provenance, evidence/Observation/effect counts, schedule notifications, checkpoint hashes, token budget, and pause/resume controls. Complex goals should be planned in Chat; the Inbox creation form intentionally creates only the bounded Research to Synthesis template.

## Acceptance evidence

The automated suite covers a five-day travel goal with a focused clarification, offline-to-online cross-device routing, recursive World Slice redaction, token/time/query/page/action budgets, restart recovery without replaying confirmed effects, stable Action idempotency, schedule-slot deduplication, scanner lifecycle, owner-scoped approval consumption, rejection of device false-success without an Observation, failure results that cannot verify success criteria, and corrupt or broken checkpoint chains.

## Rollback

Stop the service and apply [`migrations/v0.7-orchestration-rollback.sql`](migrations/v0.7-orchestration-rollback.sql). This permanently removes goal history and must be preceded by an export when data must be retained.
