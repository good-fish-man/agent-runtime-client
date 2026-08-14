# Athena v0.5 Release Governance

Athena v0.5 separates immutable behavior from per-run state and introduces a governed path from a reviewed learning artifact to production use.

## Contracts

- `AgentBuild` pins kernel, planner, policy, protocol, prompt, ontology, evaluation suite, and exact reviewed Skill/Strategy versions. Its SHA-256 checksum is immutable.
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

Only a human-reviewed proposal can enter Shadow. Leaving Shadow requires at least one passed result with zero executed actions and no external side effects. Automatic Canary is restricted to verified, recoverable R0/R1 builds. R2/R3 builds can only move from Shadow to Active through an explicit activation request.

Canary activation requires the latest metric to meet the configured minimum sample count and all success, latency, cost, safety, and intervention thresholds. Crossing a stop threshold atomically records the metric and pauses the promotion.

## Stable exposure and opt-out

Canary assignment is a deterministic hash of `owner_id`, `agent_id`, and `promotion_id`. It does not drift between tasks. A user may leave an experiment at any time and is routed to CONTROL. Rejoining restores the same deterministic assignment.

## Rollback and compensation

Activation records the previous build and atomically switches the active pointer. Rollback marks the failed promotion `ROLLED_BACK`, reactivates the previous build, and stores an immutable rollback record. Athena does not pretend that already-executed external side effects were undone; each such action can create a pending compensation instruction for an operator or a later governed executor.

## Threat model

- A model cannot construct or activate a build through Runtime prompts; deployment endpoints require the authenticated user scope.
- Unreviewed or mutable Skill/Strategy versions are rejected when a build is created.
- Shadow requests cannot declare real actions; persisted results always state zero executed actions and no external side effects.
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
