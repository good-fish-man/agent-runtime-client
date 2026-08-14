# Production Readiness Evidence v0.9

This document is an evidence index, not a declaration that external release
work has happened. A unit test cannot substitute for notarization, a
penetration test, a 72-hour soak, or a restore drill against production-like
data.

## Automated gates

| Gate | Reproducible command | Expected result |
| --- | --- | --- |
| Protocol validation and untrusted-content envelopes | `go test ./protocol/operations/v1 ./protocol/v4 ./sdk/safety` in `athena-protocol` | pass |
| Runtime admission, timeout, prompt/tool-output isolation | `go test -race ./internal/operations ./internal/eino ./internal/prompt ./internal/research` in `agent-runtime` | pass |
| Device lease/fencing, authenticated backup, privacy deletion | `go test -race ./application/service/control ./application/service/operations ./application/service/memory ./infra/repository/repo/experience` | pass |
| API, repository, migration, and owner-scope regression | `go test ./...` in `agent-runtime-client` | pass |
| Launcher state, signed Manifest, archive budgets, atomic install | `go test -race ./internal/launcher/deployment ./internal/release ./internal/state` in `athena-launcher` | pass |
| Frontend type and production bundle | `npm run lint && npm run build` in `frontend/agent-ui` | pass |

The v1.0 protocol-freeze test is intentionally updated only during the v1.0
freeze. A changed frozen fingerprint before that step is a visible failure, not
an ignored test.

## User data lifecycle

- `GET /memory/export` and `GET /experience/export` export only the authenticated owner's retained data.
- `DELETE /memory` requires `DELETE ALL MEMORY`; private Memory fields are erased from tombstones.
- `DELETE /experience` requires `DELETE ALL EXPERIENCE`; payloads, redactions, and derived evaluation fixtures/runs are physically removed while an irreversible audit tombstone remains.
- Experience retention is user-controlled and the periodic purge uses the same payload-deletion path.
- The Frontend exposes combined JSON export and separate Memory/Experience deletion controls.

No destructive v0.9 database rewrite is introduced. Startup migration remains
additive; an encrypted, authenticated backup is required before component
replacement. Failed restore is transactional and must not be reported as
successful.

## External release gates

The release evidence bundle must keep each item `EXTERNAL_REQUIRED` until an
operator attaches the dated artifact or transcript:

| Gate | Required evidence |
| --- | --- |
| Security review | threat-model sign-off plus independent API/WebSocket/Vault penetration report with no open P0/P1 |
| macOS | Developer ID signature verification, notarization ticket, install/update/rollback transcript on arm64 and amd64 |
| Windows | Authenticode verification and install/update/rollback transcript on supported Windows x64 |
| Linux | allowlisted package signature and AppImage/install/update/rollback transcript on x86-64 and arm64 |
| Stability | 24-hour and 72-hour soak reports with SLO snapshots and zero lost Task events |
| Fault injection | network jitter, database restart, process crash, disk-full, reconnect, and retry evidence |
| Disaster recovery | backup, verify, validate-only, restore, identity check, and read-only golden-task transcript |
| Load | multi-user/multi-device concurrency report including queue rejection, timeout, p95 dispatch, and duplicate-effect counters |

## Incident and log evidence

Launcher installations expose `~/.athena/logs/launcher.log`,
`postgres.log`, `agent-runtime.log`, and `agent-runtime-client.log`. Preserve the
Trace ID, Task/Action/Observation IDs, AgentBuild and RunManifest IDs, device
lease owner/fencing token, signed release-manifest digest, and relevant
redacted logs. Never attach raw credentials, cookies, tokens, or private
attachment bytes.
