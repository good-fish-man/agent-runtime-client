# Athena 1.0 Control Plane and Operations

[English](ga-operations-v1.0.md) | [简体中文](ga-operations-v1.0.zh-CN.md)

Runtime Client is the authenticated control plane for Athena 1.0. It owns user
isolation, model credentials, agents, conversations, memory, evidence,
declarative learning reviews, release governance, durable goals, device
control, plugins, backups, readiness, and Golden Journey preflight.

## GA APIs

All routes use the configured public prefix, normally
`/api/agent-runtime-client/v1`.

| Method | Path | Access | Purpose |
| --- | --- | --- | --- |
| `GET` | `/operations/readiness` | authenticated | Aggregated Runtime, database, backup, device, and domain readiness. |
| `GET` | `/operations/golden-journeys` | authenticated | Stable catalog of ten GA user journeys. |
| `POST` | `/operations/golden-journeys/run` | administrator | Non-destructive infrastructure preflight; never reports `PASS`. |
| `POST` | `/operations/golden-journeys/evidence` | administrator | Record one complete independently executed E2E suite. |
| `GET` | `/operations/health` | authenticated | Health, SLO, device, queue, and backup summary. |
| `POST` | `/operations/backups` | administrator | Create an encrypted backup. |
| `POST` | `/operations/backups/:id/verify` | administrator | Verify manifest and payload integrity. |

The run endpoint is a preflight, not a claim that a real provider,
browser account, installer, or platform signature was exercised.
`install-mode` and `safe-upgrade` remain `EXTERNAL_REQUIRED` until packaged
release tests provide external evidence.

Preflight results use `verification_level=PREFLIGHT` and can return only
`NOT_RUN`, `FAIL`, `BLOCKED`, or `EXTERNAL_REQUIRED`. An independent runner may
submit `verification_level=E2E` only as one ten-journey suite sharing one
`run_id`. Every passing step must include all catalog evidence kinds. Accepted
results are stored append-only in `os_ga_journey_result`, scoped by owner and
protected by a content SHA-256. A later preflight does not hide the last E2E
suite from readiness. `golden.suite` remains non-passing until all ten E2E
journeys pass.

Validate the catalog, shared run identity, step evidence, and JSON shape before
uploading an E2E suite:

```bash
cd /path/to/athena-protocol
go run ./cmd/validate-ga-evidence -file /path/to/golden-suite.json
```

The evidence API rejects unknown JSON fields, trailing JSON values, requests
larger than 4 MiB, incomplete suites, and mixed run IDs.

## Traceability

Every model run creates or resolves an immutable `AgentBuild` and
`RunManifest`. Device actions receive `agent_build_id`, `run_manifest_id`, a
capability instance, action ID, and trace ID. Observations must return the same
deployment provenance before they are accepted by durable storage.

## Administration

1. Replace the bootstrap `athena` password before network exposure.
2. Keep model and website credentials in their server-side vaults.
3. Require signed Providers, explicit grants, and bounded resources.
4. Create and verify encrypted backups before upgrade.
5. Review `/operations/readiness`; do not ignore `FAIL`, `BLOCKED`, or
   `EXTERNAL_REQUIRED` entries.
6. Test restore into an isolated data directory before relying on a backup.

The startup migration is additive and idempotent. The v1.0 regression suite
creates a representative v0.9 user, conversation, and memory, runs migration
twice, and verifies that all records remain intact.

## User Data and Privacy

- API keys are resolved server-side and are not returned by model or agent APIs.
- Users can access only their agents, models, conversations, memories, goals,
  evidence, and credentials unless an administrator explicitly opens an
  all-user view.
- Action observations are sanitized before persistence; transient screenshot
  bytes and credential material are not retained in ordinary control records.
- Memory and experience retention/export/delete controls are user-scoped.

## Troubleshooting

- `runtime.readiness`: inspect Runtime `/readiness` and Runtime logs.
- `database.durable`: verify PostgreSQL, DSN, schema migration, and disk space.
- `device.control`: confirm Launcher WebSocket connection, device token, user
  binding, advertised capability, and lease expiry.
- `recovery.backup`: create a backup, verify it, and inspect the backup manifest.
- A request failure should be located by `trace_id`; logs contain one complete
  source-aware error chain at the HTTP boundary.

Run `go test ./...` before release. Signed installers, notarization, real browser
flows, provider accounts, and sustained SLO validation remain external gates.
