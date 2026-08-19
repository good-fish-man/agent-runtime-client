# DSO W7: Governed Delegation Learning

## Purpose

W7 turns successful dynamic-specialist runs into **declarative candidates**, not executable code. A candidate cannot affect production until it completes an independent governance chain:

```text
Experience records
  -> declarative policy/profile candidate
  -> side-effect-free offline replay
  -> human review
  -> plan-only shadow
  -> low-risk allowlisted canary
  -> Single Agent / Static Specialist / Dynamic DSO benchmark
  -> explicit promotion
  -> immutable AgentBuild approval reference
```

Every failed or missing gate resolves to `delegation-policy://rule-baseline/v1`.

## Safety Invariants

- Candidate definitions cannot contain executable commands, source code, provider hooks, or secrets.
- Candidate records are immutable and have `activation_allowed=false`.
- Offline evaluation rejects live re-execution and requires at least three replay records.
- Shadow runs are `PLAN_ONLY` and prove zero world writes, network requests, device actions, and credential reads.
- Canary exposure is restricted to allowlisted owners, low-risk tasks, a stable cohort, and at most 25 percent.
- Failed benchmark evidence is persisted and synchronously rolls the rollout back.
- Disabling learning immediately restores the rule-policy fallback.
- Only `PROMOTED` artifacts with valid benchmark, evaluation, shadow, and human-review lineage can enter a default `AgentBuild`.
- AgentBuild binds the exact artifact ID, version, candidate hash, rollout ID, shadow evaluation, reviewer, and review time.

## Persistence

| Table | Purpose |
| --- | --- |
| `os_dso_learning_preference` | Per-owner opt-in and revision |
| `os_dso_learning_candidate` | Immutable declarative candidates |
| `os_dso_learning_evaluation` | Offline and shadow evidence |
| `os_dso_learning_review` | Independent human decisions |
| `os_dso_learning_rollout` | Canary, promotion, rollback, and disable lifecycle |
| `os_dso_learning_benchmark` | Three-way benchmark evidence |

All writes include an append-only delegation event with request trace provenance.

## API

The authenticated owner boundary applies to every route under `/api/agent-runtime-client/v1/delegation-learning`.

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/delegation-learning` | Full governance snapshot and scanner status |
| `PUT` | `/delegation-learning/preference` | Enable or disable learning with optimistic revision |
| `POST` | `/delegation-learning/candidates` | Submit a declarative candidate |
| `POST` | `/delegation-learning/candidates/:id/offline-evaluation` | Run recorded replay evaluation |
| `POST` | `/delegation-learning/candidates/:id/review` | Record human approval or rejection |
| `POST` | `/delegation-learning/candidates/:id/shadow` | Run zero-side-effect shadow evaluation |
| `POST` | `/delegation-learning/candidates/:id/canary` | Start low-risk canary exposure |
| `POST` | `/delegation-learning/rollouts/:id/benchmark` | Persist benchmark and trigger regression rollback |
| `POST` | `/delegation-learning/rollouts/:id/promote` | Explicitly promote after all gates pass |
| `POST` | `/delegation-learning/rollouts/:id/disable` | Immediately disable and fall back |

## Automatic Candidate Discovery

The evolution scanner imports reviewed specialist-profile candidates only after at least three successful historical runs. It creates a profile candidate and a matching delegation-policy candidate idempotently. It never evaluates, reviews, shadows, canaries, or promotes them automatically.

## Acceptance Evidence

Automated tests verify:

- unreviewed and canary-only artifacts cannot enter AgentBuild;
- learning opt-out prevents candidate creation and exposure;
- shadow external effects are rejected;
- stable low-risk canary routing and high-risk fallback;
- benchmark regression rolls back within the synchronous request, below the one-minute target;
- promotion is traceable to source experiences, replay, shadow, review, and benchmark;
- disabled artifacts are immediately ineligible for runtime resolution and new builds.

Production promotion still requires real representative canary samples and an operator-reviewed benchmark. Unit tests intentionally do not fabricate that external evidence.
