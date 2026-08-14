# Athena v0.5 Release Governance

Athena v0.5 separates immutable behavior from per-run state and introduces a governed path from a reviewed learning artifact to production use.

## Contracts

- `AgentBuild` pins kernel, planner, policy, protocol, prompt, ontology, evaluation suite, and exact reviewed Skill/Strategy versions. Every selected artifact includes its immutable version ID, candidate ID, evaluation run, reviewer, review time, and checksum. The build SHA-256 covers this lineage.
- `RunManifest` records the exact build checksum selected for one task together with the model configuration fingerprint, capability instances, device, user scope, world revision, knowledge snapshot, budget, feature flags, and Canary exposure.
- Runtime secrets are never stored in either object. Model API keys and knowledge tokens are represented only by non-reversible configuration fingerprints.

## Promotion state machine

```text
PROPOSED -> REVIEWED -> SHADOW -> CANARY -> ACTIVE
                       |          |          |
                       +-------> PAUSED <----+
                                      |
                                  ROLLED_BACK -> RETIRED
```

Only a human-reviewed proposal can enter Shadow. A Shadow caller submits only a task ID and structured input. The server resolves the immutable control and candidate builds, gives both the same canonical input digest, runs a plan-only evaluator, and computes route, graph, planned-action, cost, risk, and side-effect proofs. Callers cannot submit hashes or a `passed` flag. Leaving Shadow requires a result for the exact build pair with all required checks passing, zero executed actions, and zero network, device, credential, or world-write effects. Automatic Canary is restricted to verified, recoverable R0/R1 builds. R2/R3 builds can only move from Shadow to Active through an explicit activation request.

Canary metrics and samples cannot be posted through the public API. The trusted Runtime completion path emits one raw sample for each candidate run, referencing a unique `RunManifest` assigned to the candidate exposure and exact candidate build. The repository locks the Promotion, inserts the sample, recomputes success, p95 latency, average cost, safety, intervention rate, and a sample-set digest, then records the metric in one transaction. Crossing a stop threshold atomically records the sample and metric and pauses the promotion.

## Stable exposure and opt-out

Canary assignment is a deterministic hash of `owner_id`, `agent_id`, and `promotion_id`. It does not drift between tasks. A user may leave an experiment at any time and is routed to CONTROL. Rejoining restores the same deterministic assignment.

## Rollback and compensation

Activation records the previous build and atomically switches the active pointer. Rollback marks the failed promotion `ROLLED_BACK`, reactivates the previous build, and stores an immutable rollback record. Athena does not pretend that already-executed external side effects were undone; each such action can create a pending compensation instruction for an operator or a later governed executor.

## Threat model

- A model cannot construct or activate a build through Runtime prompts; deployment endpoints require the authenticated user scope.
- Unreviewed or mutable Skill/Strategy versions are rejected when a build is created.
- Shadow requests cannot declare outcomes, hashes, costs, risks, or real actions. The server rejects evaluators that report any external effect and does not persist their result.
- Public callers cannot create aggregate metrics or raw samples. The trusted Runtime path is the only writer, and the database enforces one sample per manifest.
- Optimistic revisions prevent concurrent promotion, activation, pause, and rollback updates from silently overwriting each other.
- Owner filters are mandatory on build, promotion, exposure, manifest, metric, and rollback reads.
- R2/R3 automatic Canary and secret persistence are rejected by validation and covered by tests.

## Verification

```bash
go test ./application/service/deployment ./application/service/runtime ./infra/repository/repo/deployment ./api/http/router/public ./infra/repository/migration
go test ./...
go vet ./...
```

Rollback of the database objects is documented in `docs/migrations/v0.5-deployment-rollback.sql`. Export audit data before applying it; the script intentionally refuses to hide data loss.
