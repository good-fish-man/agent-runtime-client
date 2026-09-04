# Evolution Orchestrator and Runtime Artifact Resolver

[English](evolution-runtime-artifacts.md) | [简体中文](evolution-runtime-artifacts.zh-CN.md)

## Purpose

This subsystem closes Athena's governed learning loop without allowing a model to create and immediately execute code:

```text
READY Experience records
  -> bounded pattern discovery
  -> optional constrained Codex reflection and synthesis
  -> declarative Skill candidate
  -> deterministic offline evaluation
  -> mandatory human review
  -> immutable Skill/Strategy version
  -> verified AgentBuild
  -> per-run RunManifest
  -> exact Runtime Artifact Bundle
  -> Runtime planning context
```

The Evolution Orchestrator discovers candidates. It never approves, promotes, deploys, or executes them. The Runtime Artifact Resolver only loads versions already pinned by a verified build and bound to the current run manifest.

## Evolution Orchestrator

The background worker scans owners with reusable `READY` Experience records using bounded keyset pagination. User-level learning preferences are authoritative: no candidate is generated when learning is disabled.

A pattern is eligible only when it has:

- At least four independent Experience records.
- At least two successful outcomes.
- At least one failed counterexample.
- A registered, enabled capability pattern accepted by the existing Learning validator.

Eligible patterns produce private, declarative Skill candidates. When Codex synthesis is enabled, the service sends only bounded, de-identified action structure, outcomes, failure classes, context aliases, and capability policy to the OpenAI Responses API. Codex may reorder existing steps and propose bounded recovery and verification rules, but server guardrails reject new step identities, capabilities, operations, skills, or unobserved failure classes.

The normal offline evaluation pipeline runs after synthesis and before the candidate enters `REVIEW_REQUIRED`. A pending candidate blocks duplicate proposals. After a terminal review decision, another version requires new evidence. Deterministic candidate IDs make concurrent scans idempotent. A configured Codex failure fails that proposal closed; it does not silently replace the model output with a deterministic candidate.

Automatic evolution never generates source code, shell commands, credentials, selectors, coordinates, or new permissions.

### Configuration

```yaml
evolution:
  enabled: true
  scan_interval_sec: 60
  owner_batch_size: 100
  experience_limit: 1000
  max_candidates_per_scan: 10
  minimum_novel_experiences: 2
  codex:
    enabled: false
    model: "gpt-5.6"
    api_key: "${OPENAI_API_KEY}"
    api_base: "https://api.openai.com/v1"
    reasoning_effort: "medium"
    timeout_sec: 120
    max_output_tokens: 4096
```

Environment overrides:

| Variable | Purpose |
| --- | --- |
| `ARC_EVOLUTION_ENABLED` | Enable the discovery worker |
| `ARC_EVOLUTION_SCAN_INTERVAL_SEC` | Background scan interval |
| `ARC_EVOLUTION_OWNER_BATCH_SIZE` | Owners loaded per keyset page |
| `ARC_EVOLUTION_EXPERIENCE_LIMIT` | Maximum Experience records examined per owner |
| `ARC_EVOLUTION_MAX_CANDIDATES_PER_SCAN` | Global proposal limit per scan |
| `ARC_EVOLUTION_MINIMUM_NOVEL_EXPERIENCES` | New evidence required for a subsequent version |
| `ARC_EVOLUTION_CODEX_ENABLED` | Opt in to constrained Codex candidate synthesis |
| `ARC_EVOLUTION_CODEX_MODEL` | Codex model used by the Responses API |
| `ARC_EVOLUTION_CODEX_API_KEY` | OpenAI API key; otherwise `OPENAI_API_KEY` is expanded from the sample config |
| `ARC_EVOLUTION_CODEX_API_BASE` | Responses API base URL |
| `ARC_EVOLUTION_CODEX_REASONING_EFFORT` | `none`, `low`, `medium`, `high`, `xhigh`, or `max` |
| `ARC_EVOLUTION_CODEX_TIMEOUT_SEC` | Per-request timeout |
| `ARC_EVOLUTION_CODEX_MAX_OUTPUT_TOKENS` | Structured output token ceiling |

For a local opt-in run:

```bash
export OPENAI_API_KEY="..."
export ARC_EVOLUTION_CODEX_ENABLED=true
go run . --config manifest/config/config.local.yaml
```

The API request uses strict JSON Schema output and `store: false`. Enabling Codex without a usable API key fails startup instead of leaving the configured AI stage inactive.

### User API

Routes use the configured public API prefix and require authentication:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/learning/evolution/status` | Inspect worker state and counters |
| `POST` | `/learning/evolution/scan` | Scan only the authenticated user's Experience records |

## Runtime Artifact Resolver

For every run, Runtime Client creates a `RunManifest` and resolves the exact artifacts pinned by its `AgentBuild`. Resolution fails closed unless all of the following remain true:

- Manifest owner, agent, build ID, and build checksum match.
- The build passes its integrity validation.
- Every Skill/Strategy uses an exact ID, semantic version, immutable version ID, and candidate ID.
- The candidate still has a passed evaluation and human review provenance.
- Owner, public, or organization visibility permits access.
- Stored definition bytes match the pinned SHA-256 checksum.
- Strategy references are present in the same bundle.

The bundle is sorted deterministically and bounded by encoded size, artifact count, plan steps, and strategy fallbacks. It is transported through the reserved `_athena_runtime_artifacts` context key. Runtime validates and consumes this key before generic request context is rendered for the model.

Runtime Client always removes a caller-supplied value under this reserved key, even in database-free proxy mode. Only the trusted local resolver may attach a bundle.

Runtime artifacts can only organize capabilities already selected for the request. A reviewed browser child operation may be satisfied by the existing `browser.task` aggregate capability, but artifacts cannot register a tool, expand device grants, bypass policy, or run executable code.

## Failure Semantics

Artifact corruption, stale approval provenance, owner mismatch, manifest mismatch, or malformed transport data fails the run before model execution. Missing request capabilities do not fail the run; the affected Skill is marked unavailable and omitted from planning.

This distinction keeps integrity failures visible while allowing capability-safe fallback to the base planner.
