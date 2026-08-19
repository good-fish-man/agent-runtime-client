# DSO W6 Production Hardening

W6 turns durable specialist orchestration into an operable recovery system. It does not declare production readiness from unit tests alone. Automated evidence and external release gates are intentionally separated.

## Runtime boundaries

- `EXACT_CONFIG` verifies the immutable invocation manifest and every bound artifact. It never invokes a model, browser, device, capability, or external service.
- `RECORDED_OBSERVATION_SIMULATION` can consume only observation references already owned by the source run. It cannot create side effects.
- `LIVE_REEXECUTION` requires an explicit approval reference and an injected policy-verifying executor. The production composition root supplies no live executor, so the default is fail-closed.
- A terminal replay is immutable and idempotent. A retry returns the previous result.
- A live replay left in `RUNNING` after a crash has an unknown external outcome. Athena requires reconciliation and never executes it again automatically.

## Failure matrix

| Failure point | Recovery rule | Duplicate side-effect protection |
| --- | --- | --- |
| Before replay persistence | Retry with the same request | No external call occurred |
| After replay persistence | Deterministic modes may resume; live mode requires reconciliation | Live execution is not repeated |
| Before live execution | Reconcile persisted `RUNNING` state | No automatic re-execution |
| After live execution, before completion | Mark outcome unknown and reconcile | No automatic re-execution |
| Scheduler owner crash | Standby acquires the expired lease with a higher fencing token | Stale owners cannot become current owners |
| Late worker result | Durable attempt revision and lease checks reject it | Result is counted as fenced evidence |
| Cancellation | Record cancel request and measure terminal propagation | SLO target is P95 <= 5 seconds |

## HA and diagnostics

The delegation scheduler uses a database lease with a monotonic fencing token. The default lease is 20 seconds, so a healthy standby can take over an expired owner within 30 seconds. The Operations page reports a rolling 24-hour DSO snapshot:

- availability target: at least 99.9%
- cancellation propagation P95 target: at most 5 seconds
- duplicate confirmed side effects: exactly zero
- recovered attempts and fenced late results

The health snapshot becomes `DEGRADED` when a target is violated or diagnostics cannot be measured.

## Data lifecycle

- Export is owner-scoped and blocked when secret-like material reaches the export boundary.
- Deletion is owner-scoped, cutoff-based, transactional, and leaves an immutable retention tombstone.
- The HTTP deletion endpoint requires the exact phrase `DELETE DELEGATION DATA`.
- Replay results contain references and hashes, not credentials or raw provider payloads.

## Threat model

| Threat | Control | Residual gate |
| --- | --- | --- |
| Cross-owner replay or observation injection | Owner predicates on every lookup plus source-run subset validation | Multi-tenant penetration test |
| Repeated irreversible action after crash | Unknown-outcome reconciliation and no live auto-retry | Real device fault injection |
| Stale scheduler split brain | Database lease, revision CAS, monotonic fencing token | Multi-node database failover exercise |
| Forged replay approval | Live executor is absent by default; future executor must resolve an unexpired independent `PolicyDecision` | Security review before enabling live replay |
| Secret leakage in export or replay | Reference-only protocol and export boundary scanner | Independent data-loss-prevention review |
| Destructive lifecycle request | Authenticated owner binding and exact confirmation | Backup/restore drill before release |

## Automated evidence

The following local gates pass:

```text
go test ./application/service/delegation ./application/service/operations \
  ./api/http/handler/public/delegationops ./api/http/router/public ./boot \
  ./infra/repository/repo/delegation ./infra/repository/migration
npm run lint
npm run build
```

Covered cases include replay mode separation, cross-owner rejection, foreign-observation rejection, terminal idempotency, crash recovery, unknown live outcomes, HA fencing/failover, lifecycle isolation, diagnostics, authenticated routes, and destructive confirmation.

## External release gates

W6 is locally complete, but production activation remains blocked until all of these external artifacts exist:

1. A multi-node PostgreSQL and service failover report proving takeover below 30 seconds without duplicate confirmed effects.
2. A pre-production soak report demonstrating at least 99.9% availability and cancellation P95 below 5 seconds.
3. An independent penetration-test and threat-model review.
4. A signed data-retention, export, deletion, backup, and restore review.
5. A policy-verifying live replay executor review before `LIVE_REEXECUTION` is enabled.
