# 02. HTTP API, Identity, and Security

[Guide index](README.md) | [简体中文](../zh-CN/02-http-api-identity-and-security.md)

## Purpose

The `api/http` tree is Athena's transport boundary. It translates HTTP, SSE, and WebSocket traffic into application calls and translates domain/application errors into stable public responses.

Handlers should not contain repository queries or orchestration policy.

## HTTP Engine

[`api/http/http.go`](../../../api/http/http.go) builds the Gin engine and installs shared middleware. Routes are divided into three surfaces:

| Surface | Location | Use |
| --- | --- | --- |
| Runtime API | [`api/http/router`](../../../api/http/router) | health, capabilities, run, stream, media, resume, stop |
| Management API | [`api/http/router/public`](../../../api/http/router/public) | users, Agents, models, governance, operations, and integrations |
| Device control | [`api/http/handler/control`](../../../api/http/handler/control) | long-lived desktop WebSocket and related control endpoints |

Management routes are mounted under `server.public_prefix`. Runtime invocation routes remain under `/v1`.

## Middleware Order

[`api/http/middleware`](../../../api/http/middleware) contains cross-cutting transport behavior:

- **trace** accepts a trusted incoming trace ID or generates one, stores it in Gin and `request.Context`, and returns it in response headers;
- **recovery** catches panics and turns them into a stable internal error;
- **request/response logging** records status, duration, byte counts, and bounded bodies while treating streams specially;
- **authentication** resolves bearer tokens and user/admin identity;
- **internal authentication** protects launcher/service callbacks;
- **CORS** controls browser origins; and
- **error rendering** maps application errors to the public envelope.

Trace context must be established before database, gRPC, or application work so all downstream logs use the same request identity.

## Identity and Authorization

[`pkg/authctx`](../../../pkg/authctx) is the application-facing identity seam. Middleware writes authenticated identity to `context.Context`; services read it from there or receive the resolved owner ID explicitly.

Key rules:

- normal users may read and mutate only owner-scoped data;
- administrators use explicit admin endpoints when cross-user visibility is required;
- model API keys and site passwords are never included in browser responses;
- internal endpoints require an internal service credential, not an ordinary user token;
- changing an ID in a URL must not bypass owner checks; and
- public/system Agents are selectable, but user-owned model credentials remain private.

## Handlers

Handlers are organized by public resource. A normal handler should:

1. bind path/query/body input;
2. validate transport-level requirements;
3. resolve the authenticated owner/admin context;
4. call one application service method; and
5. return through [`types/response`](../../../types/response).

Use [`types/apierror`](../../../types/apierror) for stable status/code/message behavior. Wrap internal errors with operation names, but log the terminal chain only once at the boundary.

## Runtime SSE

Streaming endpoints in [`api/http/handler/runtime`](../../../api/http/handler/runtime) convert typed Runtime events into Server-Sent Events.

Important invariants:

- do not buffer the full response;
- flush each visible event promptly;
- preserve event type, trace ID, usage, Action, and Observation metadata;
- cancel upstream gRPC/device work when the request context is canceled;
- do not emit an error and then a contradictory success terminal; and
- skip or account for protocol-only chunks without presenting them as empty assistant answers.

## Device WebSocket

The control handler upgrades an authenticated HTTP connection and delegates protocol ownership to the control Hub. The WebSocket is not a second business implementation: messages must use the canonical Action/Observation protocol types.

The device connection carries registration, capability snapshots, heartbeats, actions, observations, progress, approvals, cancellation, and reconnection state.

## Public API Families

The management router includes:

- authentication and profile/avatar APIs;
- Agent, model, model-key, Skill, knowledge-base, memory, and workspace APIs;
- website credentials and scheduled tasks;
- experience, evaluation, learning, deployment, evidence knowledge, and Goals;
- delegation operations and governed delegation learning;
- plugin registry and production operations;
- channels, callbacks, Weixin, jobs, commands, dashboard, voice avatars, and service configuration.

Read [`api/http/router/public/router.go`](../../../api/http/router/public/router.go) as the authoritative endpoint inventory.

## Security Boundaries

| Boundary | Required protection |
| --- | --- |
| Browser to Client | bearer identity, CORS, owner checks, bounded input |
| Launcher/device to Client | device/internal token, lease, capability binding, replay protection |
| Client to Runtime | forwarded trace and hydrated server-side credentials |
| Client to database | context-bound queries and owner predicates |
| Client to local files/processes | safe paths, allowlists, size/time limits, no shell interpolation |
| Plugin installation | signature, package hash, scanner evidence, human review, permission grant |

## Read These Files First

| File | Why |
| --- | --- |
| [`api/http/http.go`](../../../api/http/http.go) | engine and middleware assembly |
| [`api/http/router/router.go`](../../../api/http/router/router.go) | Runtime routes |
| [`api/http/router/public/router.go`](../../../api/http/router/public/router.go) | complete management route catalog |
| [`api/http/middleware/trace.go`](../../../api/http/middleware/trace.go) | request identity propagation |
| [`api/http/middleware/auth.go`](../../../api/http/middleware/auth.go) | user/admin authentication |
| [`api/http/middleware/req_body.go`](../../../api/http/middleware/req_body.go) | bounded, stream-aware request logging |
| [`types/apierror`](../../../types/apierror) | stable API errors |
| [`types/response`](../../../types/response) | public response envelope |

## Change Checklist

- Is authorization enforced in the service/repository as well as the UI?
- Does every downstream call use `c.Request.Context()`?
- Are secret fields absent from DTOs, logs, and error messages?
- Does stream cancellation reach gRPC and device execution?
- Can the endpoint return only one terminal outcome?
- Are input sizes and local file/process operations bounded?
