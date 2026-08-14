# Durable Goals and Supervisor v0.7

Athena v0.7 turns explicitly long-running work into a finite, durable task graph. A goal is owned by one user and one Agent, carries observable success criteria and hard budgets, and is checkpointed on every state transition.

## Execution model

1. Runtime recognizes only explicit background, cross-day, cross-device, or resume-later intent.
2. `PersistentGoalCreate` submits a declarative graph with at most 64 nodes, depth 4, and 8 concurrent specialists.
3. Runtime Client persists the goal, tasks, specialist results, schedule triggers, and checksummed checkpoints atomically.
4. Supervisor routes server specialists directly and routes browser/desktop specialists only to an online device that advertises every required capability.
5. Runtime executes through the existing Control Hub. An external effect is confirmed only by a successful device Observation; its idempotency key is written to the next checkpoint.
6. Restart recovery converts orphan `RUNNING` tasks to `READY` while retaining confirmed effect keys and scoped world state.

## Safety boundaries

- Graphs are finite and every task receives a positive allocation inside the goal budget.
- The Supervisor cannot create nested persistent goals, hidden sub-agents, executable code, or unbounded loops.
- `WAITING_USER`, approval, device-offline, deadline, and budget exhaustion are durable states, never fake success.
- Specialists receive only declared world-slice keys. Credentials, cookies, raw screenshots, and unrestricted device state are not copied into goals.
- Every result contains RunManifest, AgentBuild, model configuration fingerprint, device, trace, usage, and evidence provenance.

## Operations

```yaml
orchestration:
  enabled: true
  scan_interval_sec: 3
  max_concurrent_runs: 2
```

Environment overrides are `ARC_ORCHESTRATION_ENABLED`, `ARC_ORCHESTRATION_SCAN_INTERVAL_SEC`, and `ARC_ORCHESTRATION_MAX_CONCURRENT_RUNS`. User routes are under `/goals`; Runtime creates planned goals through the token-protected `/internal/goals` route.

The Inbox shows goals, specialist states, token budget, checkpoints, and pause/resume controls. Complex goals should be planned in Chat; the Inbox creation form intentionally creates only the bounded Research to Synthesis template.

## Rollback

Stop the service and apply [`migrations/v0.7-orchestration-rollback.sql`](migrations/v0.7-orchestration-rollback.sql). This permanently removes goal history and must be preceded by an export when data must be retained.
