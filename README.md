# agent-runtime-client

A standalone **DDD**-structured Go service that fronts [`agent-runtime`](https://github.com/good-fish-man/agent-runtime).
It exposes its own HTTP API (Gin) and internally calls agent-runtime over **gRPC**, reusing
agent-runtime's generated proto stubs and shared log package.

The layering mirrors `XiaoQinglong/backend/agent-frame`.

## Layout

```
api/            接入层  — Gin HTTP handlers, router, middleware
application/    应用层  — service orchestration, DTOs, assemblers
domain/         领域层  — entities, repository (gateway) interfaces, domain services
infra/          基础设施 — gRPC client implementing the domain gateway port
config/         配置加载 (YAML + env)
boot/           组合根：依赖装配 + 启动
pkg/log         结构化日志与 trace context
types/          apierror + consts
```

Dependency direction: `domain` (pure) ← `application` ← `api`; `infra` implements the
`domain/irepository` port; `boot` wires `infra → domain → application → api`.

## Run

```bash
go run . --config manifest/config/config.yaml
```

Config lives in `manifest/config/config.yaml`. Point `runtime.grpc_addr` at a running
agent-runtime gRPC server (default `localhost:18080`).

## Endpoints (v1)

| Method | Path               | Runtime RPC       |
|--------|--------------------|-------------------|
| GET    | `/healthz`         | HealthCheck       |
| POST   | `/v1/run`          | Run               |
| POST   | `/v1/run/stream`   | RunStream (SSE)   |
| POST   | `/v1/agent`        | RunAgent          |
| POST   | `/v1/agent/stream` | RunAgentStream (SSE) |
| GET    | `/health/alive`    | Liveness probe    |
| GET    | `/health/ready`    | Readiness probe (upstream runtime) |

## Public resource endpoints (agent-frame compatible)

When a shared database is configured (`db.db_host` + `db.db_name`), the service
also mounts agent-frame's HTTP resource API under a configurable prefix
(`server.public_prefix`, default `/api/xiaoqinglong/agent-frame/v1`). This makes
the client a drop-in replacement for agent-frame's HTTP surface while reusing the
same database and table schema.

Resource groups mounted under the prefix:

| Group            | Base path          | Notes |
|------------------|--------------------|-------|
| user             | `/user`            | CRUD + byQuery/byAll/page |
| config           | `/config`          | app/skills YAML read/write (always on, file-backed) |
| model            | `/model`           | CRUD + all/page |
| knowledge_base   | `/knowledge_base`  | CRUD + all/page + `:ulid/recall` |
| skill            | `/skill`           | CRUD + all/page + upload + check-name |
| agent            | `/agent`           | CRUD + all/page + upload + `:ulid/enabled` |
| channel          | `/channel`         | CRUD + all/page |
| callback         | `/callback/:channel` | channel callback skeleton |
| weixin           | `/weixin/login`, `/weixin/login/status` | scan-login skeleton |
| job              | `/job/execution`   | byId / byAgentId / page |

Groups backed by the database are only registered when the database is enabled;
otherwise the service runs as a pure runtime client and only `config`, `callback`
and `weixin` (plus the `/v1` runtime routes) are available.

> Note: `chat`, `dashboard`, `command` and `runner` groups are planned and will be
> mounted here as they are implemented.
