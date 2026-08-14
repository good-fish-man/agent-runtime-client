# Security Threat Model v0.9

## Scope and trust boundaries

Athena treats the Frontend, public web content, browser DOM/snapshots, files, plugin output, model output, and device Observation text as untrusted data. Authenticated user intent is authoritative only at user privilege; it cannot replace system policy. Runtime, Control Plane policy, signed Protocol contracts, Launcher trust roots, and explicit operator approvals form the trusted computing boundary.

Data crosses these boundaries:

1. Frontend to Control Plane: authenticated HTTP/SSE requests scoped to the current user.
2. Runtime to Control Plane: internal service token plus trace identity.
3. Launcher to Control Plane: device token, database-backed lease owner, and monotonic fencing token.
4. Control Plane to Runtime: typed request models without API keys returned to the browser.
5. External content to model: `athena.untrusted-content.v1` data-only envelopes with digest, risk, indicators, and budgets.
6. Release infrastructure to Launcher: signed, expiring Manifest, SBOM, exact sizes, artifact hashes/signatures, and platform-signing evidence.

## Principal threats and controls

| Threat | Primary controls | Required failure mode |
| --- | --- | --- |
| Cross-user data access | owner-scoped repositories, user binding, admin-only global views | deny without existence disclosure |
| Device impersonation/replay | device token, user binding, lease owner, fencing token, expiry, idempotency | reject stale socket/message |
| WebSocket message forgery | authenticated connection identity and server-side Action ownership lookup | reject and audit |
| Credential leakage | Vault indirection, sensitive-field redaction, attachment data removal, no API keys in public DTOs | redact before persistence/log/model |
| Direct/indirect prompt injection | typed capability routing, data-only external envelopes, risk indicators, Action schema validation | evidence may be quoted, never executed |
| Tool-output poisoning | every non-user-visible Tool result is wrapped as untrusted data; Action envelopes are independently parsed and validated | no text-to-side-effect conversion |
| Plugin compromise | signed immutable package, grant subset, process/resource isolation, revocation | terminate provider without crashing core |
| Release substitution/zip bomb | Ed25519 Manifest, exact size/SHA/signature, HTTPS redirect policy, archive budgets, staged rollback | retain last verified install |
| Backup tampering/partial restore | AES-GCM chunks, Manifest HMAC, protected key, validate-only, single transaction | reject before database mutation |
| State/secret replacement | regular-file and permission checks, bounded reads, TOCTOU identity check, atomic fsync writes | fail closed and preserve evidence |
| Multi-instance split brain | shared database lease and monotonic fencing | only newest owner may dispatch/observe |
| Resource exhaustion | bounded queues, request timeout, concurrency gate, context/output/archive budgets | reject or time out with SLO counters |

## Authorization matrix

| Operation | User | Administrator | Launcher/internal service |
| --- | --- | --- | --- |
| Read own Agents, Models, Tasks, Memory, Experience | allow | allow | deny |
| Read another user's private resources | deny | explicit admin view only | deny |
| Create/modify own resources | allow | allow | deny |
| Approve own device action | allow | allow | deny |
| Global plugin trust/revocation | deny | allow | signed install only |
| Health snapshot | authenticated | allow | allow where explicitly routed |
| Backup list/create/verify/restore | deny | allow | pre-update create with internal token |
| Device connect/heartbeat/Observation | deny | inspect only | valid device token + current lease |
| Rotate Vault/release/backup trust roots | deny | offline operator procedure | deny at runtime |

## Verification evidence

Automated tests cover owner scope, device token and stale fencing rejection, Observation socket binding, secret redaction, indirect-injection classification, signed package validation, archive budgets, backup forgery/move detection, key permissions, state symlinks/permissions, queue drain, and timeout counters. Real CAPTCHA/2FA bypass is explicitly prohibited; Athena pauses for user takeover. Platform notarization/signing, penetration testing, 24/72-hour soak, and disaster-recovery drills remain external release evidence and must never be represented by a unit-test pass.

Security incidents must preserve trace IDs, Action/Observation IDs, build/run manifest IDs, lease owner/token, release manifest digest, and relevant component logs. Do not include credentials or raw attachment data in incident records.
