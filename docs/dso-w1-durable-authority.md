# DSO-W1 Durable Delegation Authority

Status: implementation complete; local PostgreSQL gate passed; MySQL gate is enforced by CI and awaits an environment with a reachable MySQL image.

## Scope

DSO-W1 moves specialist delegation ownership from request-local runtime memory into the Control Plane database. The Control Plane is now authoritative for proposal admission records, logical runs, recoverable attempts, budgets, cancellation, leases, and audit events.

## Delivered

- Durable PO and repository models for proposals, decisions, delegated outcomes, subagent specifications, manifests, runs, attempts, decision turns, model invocations, budget accounts/reservations, resource leases, candidate results, and DSO events.
- Atomic accepted-delegation creation with content-bound idempotency.
- Optimistic revisions plus row-locked Attempt ownership; one logical Run can have at most one live Attempt owner.
- Attempt lease/heartbeat support, startup recovery, stale-result fencing, orphan Run requeue, and deadline expiration.
- Hierarchical budget ledger operations: Reserve, Commit actual usage, Release unused capacity, and cancellation cleanup.
- Transactional Outbox events carrying trace, causation, aggregate sequence, and idempotency metadata.
- A lifecycle-managed Delegation Orchestrator wired into application startup and shutdown.
- A legacy `orchestration/v2` adapter that can only produce a `SUBMITTED` DSO proposal. It cannot admit or execute a legacy specialist.
- PostgreSQL/MySQL integration gates in `.github/workflows/dso-w1-databases.yml`.

## Invariants Under Test

1. Concurrent acquisition produces exactly one active Attempt owner.
2. An expired owner cannot commit a late result after a replacement boundary.
3. `Consumed + Reserved <= Total` under concurrent reservations.
4. Commit charges only actual usage; unconsumed capacity returns to the parent account.
5. Cancellation revokes active resource leases and releases remaining reservations.
6. Outbox delivery failures leave events unpublished for safe retry.
7. Recovery handles CREATED, RUNNING, WAITING_OBSERVATION, CANCEL_REQUESTED, orphan, and deadline-expired boundaries.
8. Every recovery transition emits traceable, causally linked audit data.

## Verification Evidence

Passed locally:

```bash
go test ./...
go vet ./...
go test -race ./application/service/delegation ./infra/repository/repo/delegation
ATHENA_DSO_TEST_POSTGRES_DSN='...' go test -count=1 -run TestDatabaseConcurrencyGate/postgres ./infra/repository/repo/delegation
```

The MySQL command is identical except for `ATHENA_DSO_TEST_MYSQL_DSN` and the `/mysql` test selector. It runs automatically in the database-gate workflow. The local Docker environment could not reach either Docker Hub or the public ECR mirror during this implementation session; this is recorded as an environment limitation, not a passing result.

## W2 Boundary

W1 does not let the old request-local subagent implementation execute. W2 must bind every admitted Attempt to a validated InvocationManifest and run its DecisionTurn loop through this durable authority.
