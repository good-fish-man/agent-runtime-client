# Athena Learning Gate v0.4

中文版：[learning-gate-v0.4.zh-CN.md](learning-gate-v0.4.zh-CN.md)

## Scope

v0.4 turns repeated, sanitized `Experience` records into declarative `Skill` or `Strategy` candidates. It does not generate or execute Go, JavaScript, Shell, Python, or arbitrary commands. Approval creates an immutable, checksummed version; it never changes an existing Agent, default model, policy, or production route.

## Pipeline

```text
Experience evidence
  -> repeated semantic pattern
  -> schema and capability validation
  -> permission and composed-risk analysis
  -> deterministic offline replay (seed=1)
  -> baseline and Wilson 95% confidence interval
  -> human edit / re-evaluate / approve or reject
  -> immutable SkillVersion or StrategyVersion
```

The evidence gate requires at least four independent Experiences, two matching successes, and one failed counterexample. Candidate edits are validated again. Re-evaluation reuses the original evidence and appends a new evaluation instead of overwriting history. Optimistic revisions reject stale reviews and edits.

## API

All routes are user-scoped under `/api/agent-runtime-client/v1/learning`.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/candidates/generate` | Build and evaluate a candidate from Experience IDs |
| `GET` | `/candidates` | List the current user's candidates |
| `GET` | `/candidates/:id` | Read definition, evidence, and evaluation history |
| `PUT` | `/candidates/:id` | Edit a declarative definition and rerun static gates |
| `POST` | `/candidates/:id/re-evaluate` | Append a deterministic offline evaluation |
| `POST` | `/candidates/:id/review` | Approve or reject with an expected revision |
| `GET` | `/skills`, `/strategies` | List approved, manually selectable artifacts |
| `POST` | `/demonstrations` | Explicitly start a private demonstration |

Demonstrations record semantic capability and operation names only. Password, token, cookie, OTP, 2FA, and equivalent fields pause capture and persist a redacted placeholder.

## Storage and rollback

v0.4 adds `os_learning_candidate`, `os_candidate_evidence`, `os_candidate_evaluation`, `os_skill`, `os_skill_version`, `os_strategy`, `os_strategy_version`, and `os_demonstration`. Version rows are append-only and include SHA-256 checksums. See [v0.4-learning-rollback.sql](migrations/v0.4-learning-rollback.sql) for a scoped rollback.

Before rollback, stop the client and export approved definitions if they must be retained. The rollback intentionally does not modify Agent configuration, Experiences, tasks, users, credentials, or models.

## Threat model

| Threat | Control |
| --- | --- |
| Prompt injection becomes executable code | DSL rejects executor capabilities and script/command/credential-shaped fields |
| Candidate requests an unregistered or disabled capability | Validation requires the persisted capability registry policy |
| Candidate lowers composed risk | Declared ceiling must be at least the maximum referenced capability risk |
| Candidate expands credentials or auth scope | Credential fields and new executors are forbidden in declarative arguments |
| One lucky run becomes a Skill | Independent evidence, counterexample, replay, minimum sample, and confidence interval are required |
| Stale reviewer overwrites another decision | Revision-checked update and transactional materialization |
| Sensitive demonstration data reaches PostgreSQL | Capture pauses and stores only a redacted semantic placeholder |
| Approval silently changes production | Approved versions are returned as `manual_only` and are not wired into Agent execution |

## Verification and benchmark

The deterministic test suite covers schema validation, unregistered capabilities, direct executors, risk escalation, insufficient evidence, failed counterexamples, sensitive demonstrations, stale revisions, safe edit, re-evaluation history, and transactional materialization.

Local acceptance command:

```bash
GOCACHE=/tmp/athena-client-gocache go test ./...
GOCACHE=/tmp/athena-client-gocache go vet ./...
```

The offline benchmark uses seed `1`, four fixtures, a historical baseline, success rate, safety score, delta, and Wilson 95% interval. A candidate passes only when sample size is at least four, score meets its declared threshold (default `0.75`), and safety is `1.0`. This benchmark validates the gate, not real-world generalization; larger site-specific suites remain required before manual deployment.

## Distribution status

This version adds runtime data and UI contracts only. It does not produce a new downloadable executable, SBOM, signature, or release manifest. Distribution signing and provenance gates are introduced in later roadmap versions.
