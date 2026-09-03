# 07. Operations, Workspaces, and Integrations

[Guide index](README.md) | [简体中文](../zh-CN/07-operations-workspaces-and-integrations.md)

## Purpose

This chapter covers the supporting subsystems that make the control plane operable and useful outside a single chat request: health/readiness, backup/recovery, project workspaces, configuration/restart, website accounts, dashboards, voice avatars, and external channels.

## Production Operations

[`application/service/operations`](../../../application/service/operations) aggregates evidence from Runtime, database, device control, delegation recovery, backups, and Golden Journeys.

It exposes three related but different concepts:

- **health**: is a component currently responding;
- **SLO evidence**: measured reliability/latency/recovery behavior; and
- **GA readiness**: are required invariants and real Golden Journeys proven.

Readiness must not report a unit test or preflight as a real end-to-end browser/device pass. Golden Journey evidence is owner-scoped, integrity checked, and can be submitted by an independent E2E runner.

## Backup and Recovery

[`backup.go`](../../../application/service/operations/backup.go) manages encrypted backup creation, inventory, verification, and restore. Recovery operations are privileged and audited.

The embedded PostgreSQL binaries and the database data directory have different lifecycles. Updating a packaged server binary must not delete user data. Restore must validate the selected backup and avoid exposing passwords or internal network details.

## Project Workspaces

[`api/http/handler/public/workspace`](../../../api/http/handler/public/workspace) is a bounded local-project API used by the frontend coding workspace. It supports:

- native folder selection on macOS/Windows and supported Linux desktops;
- directory import and an in-memory workspace identity;
- bounded file tree and text preview;
- text search and relevance-selected context files;
- structured change-to-patch construction;
- patch validation/dry run/application; and
- platform-safe path resolution.

The implementation excludes large/generated directories, caps entries and bytes, and requires every requested path to remain inside the imported root. It is not the autonomous Runtime filesystem capability and should not receive arbitrary shell commands.

## Configuration and Restart

[`api/http/handler/public/config`](../../../api/http/handler/public/config) reads/writes configured Client, Runtime, and Skills YAML files. It also proxies Runtime status/configuration and performs guarded restart checks.

Changing frontend-only theme preferences should not restart Runtime. Service restart is appropriate only when a changed process-owned setting requires it.

Port handling distinguishes the currently managed process from an unrelated process. Killing an unrelated port owner requires explicit user authorization.

## Website Credentials

[`application/service/browsercredential`](../../../application/service/browsercredential) stores only credential metadata in PostgreSQL. The username/password secret is stored in the OS credential vault and is passed to `agent-browser` through protected standard input or a vault reference.

The Agent receives an authenticated browser `session_id`, not the password. CAPTCHA, QR, slider, and 2FA states switch to visible human takeover rather than attempting to bypass protections.

Credentials are owner- and domain-scoped, URLs must be absolute HTTP(S) URLs without embedded credentials, and browser session IDs are validated.

## Dashboard

[`api/http/handler/public/dashboard`](../../../api/http/handler/public/dashboard) aggregates counts, tasks, tokens, approvals, activities, and recent conversations. Dashboard metrics should come from persisted execution/chat/model usage, not hard-coded percentages.

## Local Model Operations

The model HTTP package includes machine environment detection, free local-model installation, duplicate detection, progress polling, local runtime lifecycle modes, and fine-tuning/distillation workflows.

These operations launch bounded external processes. Command arguments must be explicit, output/errors must be captured, and jobs must remain queryable after frontend navigation.

## External Integrations

| Package | Role |
| --- | --- |
| `channel` | external conversation channel configuration |
| `callback` | authenticated/asynchronous callback ingress |
| `weixin` | Weixin-specific adapter |
| `job` | asynchronous job execution records |
| `command` | controlled command request surface |
| `voiceavatar` | owner-scoped uploaded voice-avatar media |

Each adapter should translate external payloads into existing application use cases. It should not create a parallel Agent execution implementation.

## Scheduled and Internal Routes

Launchers/background services use dedicated internal routes for scheduled work, Goals, backups, and Golden Journey evidence. These are protected by internal credentials and should remain unavailable to ordinary bearer tokens.

## Read These Files First

| Area | Starting point |
| --- | --- |
| Health/SLO | [`application/service/operations/service.go`](../../../application/service/operations/service.go) |
| GA readiness | [`application/service/operations/ga.go`](../../../application/service/operations/ga.go) |
| Backup/recovery | [`application/service/operations/backup.go`](../../../application/service/operations/backup.go) |
| Workspaces | [`api/http/handler/public/workspace/workspace_handler.go`](../../../api/http/handler/public/workspace/workspace_handler.go) |
| Config/restart | [`api/http/handler/public/config`](../../../api/http/handler/public/config) |
| Website credentials | [`application/service/browsercredential/service.go`](../../../application/service/browsercredential/service.go) |
| Dashboard | [`api/http/handler/public/dashboard`](../../../api/http/handler/public/dashboard) |
| Local models | [`api/http/handler/public/model`](../../../api/http/handler/public/model) |

## Change Checklist

- Does an operational status distinguish health, preflight, and real E2E proof?
- Are backup secrets and internal topology absent from responses/logs?
- Can workspace paths escape the imported root or exceed size limits?
- Does a config change restart only the service that owns it?
- Do external adapters reuse the normal authenticated Runtime path?
- Are external processes bounded, cancellable, and observable?
