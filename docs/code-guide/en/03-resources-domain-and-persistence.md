# 03. Resources, Domain Model, and Persistence

[Guide index](README.md) | [简体中文](../zh-CN/03-resources-domain-and-persistence.md)

## Purpose

This chapter explains the repeated vertical-slice structure used for users, Agents, models, Skills, knowledge bases, channels, jobs, and related control-plane resources.

## The Vertical Slice

A conventional resource follows this path:

```text
HTTP handler
  -> application DTO
  -> application service
  -> assembler
  -> domain entity/service
  -> repository port
  -> GORM repository/converter/PO
  -> database
```

Not every advanced subsystem uses every layer, but dependencies must still point through interfaces toward domain/application meaning.

## Layer Responsibilities

### DTOs: `application/dto`

DTOs describe use-case input and output. They may contain JSON/query-friendly types, paging, partial updates, and authenticated actor IDs. They must not carry secrets back to the browser.

### Assemblers: `application/assembler`

Assemblers translate DTOs to domain entities and entities back to response DTOs. They keep field mapping out of handlers and domain services. They should not query the database or make policy decisions.

### Entities: `domain/entity`

Entities express business state independently of Gin and GORM. Protocol-heavy areas may alias frozen structures from `athena-protocol` instead of redefining them.

### Domain services: `domain/srv`

Domain services implement resource rules and depend on repository ports. Conventional CRUD services exist for Agent, user, model, Skill, knowledge base, channel, job, and Runtime.

### Ports: `domain/irepository`

Repository interfaces describe what the domain/application needs, not how SQL is written. Runtime is also a port: `RuntimeGateway` hides generated gRPC details.

### Persistence objects: `infra/repository/po`

POs are GORM table shapes. Persistence-only fields, indexes, soft-delete timestamps, JSON columns, and optimistic revisions belong here.

### Converters and repositories

`infra/repository/converter` maps PO and entity forms. `infra/repository/repo` implements ports using `Data.DB(ctx)`. Advanced subsystems sometimes use store interfaces and POs directly because their transactional operations are larger than CRUD.

## Core Resource Families

### Users

User services own registration, login, password hashing, profiles, avatars, admin level, and actor identity. Bootstrap administration is an installation concern and only creates a missing account from environment credentials.

### Agents

Agents store user-editable behavior and bindings, including the primary LLM, optional embedding/media models, Skills, knowledge bases, and sub-Agent declarations. Public Agents are system-owned templates. Prompts and model credentials are hydrated server-side for execution and are not returned as browser round-trip secrets.

### Models and model keys

Models and provider keys are separate resources:

- a model references a key record by ID;
- changing one key can update all referencing models;
- each key and private model is owner-scoped;
- local providers may not require a key;
- only administrators may globally enable/disable a model;
- Runtime mode controls always-on, on-demand, or disabled local execution; and
- 24-hour usage, tokens, latency, and success metrics are enriched from recorded executions.

The model package also owns catalog browsing, environment inspection, duplicate-safe local installation, installation progress, fine-tuning, and distillation jobs.

### Skills and knowledge bases

The management resources describe what an Agent may use. Runtime receives only selected/relevant artifacts after server-side hydration. Uploaded Skills are validated and stored; governed learning artifacts use the separate learning/deployment lifecycle described later.

### Memory

Memory records are user- and Agent-scoped. The service supports list/export/delete controls and injects relevant memory at execution time. Background extraction failures must not erase the original chat result.

### Channels and jobs

Channels represent external conversation ingress/egress. Jobs and callbacks track asynchronous execution/integration state. These packages use the same owner and trace rules as direct web requests.

## Database Access

[`infra/data/data.go`](../../../infra/data/data.go) wraps the shared GORM handle. Every repository call should use `Data.DB(ctx)` so SQL logging, cancellation, and trace context follow the request.

[`infra/db/db.go`](../../../infra/db/db.go) supports PostgreSQL and MySQL, configures the pool, and installs the shared `logx` GORM logger. PostgreSQL uses simple protocol to avoid stale prepared-plan result shapes after startup migrations.

## Transactions, Revisions, and Idempotency

Simple CRUD may use one GORM operation. Multi-object state machines should use transactions and explicit optimistic revisions. Durable actions, promotions, goals, and delegation runs require stable idempotency keys because retries and restart recovery are normal behavior.

Soft deletion normally uses `deleted_at`, but terminal audit/evidence records may be append-only instead.

## Migration Ownership

All active POs must be listed in [`infra/repository/migration/migration.go`](../../../infra/repository/migration/migration.go). A feature is incomplete if its schema exists only in a test or manual database.

When changing persistence:

- define upgrade behavior for existing data;
- provide rollback SQL where the release policy requires it;
- keep seeds idempotent;
- never store plaintext model/site credentials; and
- add migration tests for destructive or renamed structures.

## Read These Files First

| Area | Starting point |
| --- | --- |
| Agent resource | [`application/service/agent`](../../../application/service/agent) |
| Model resource | [`application/service/model`](../../../application/service/model) |
| User resource | [`application/service/user`](../../../application/service/user) |
| Domain entities | [`domain/entity`](../../../domain/entity) |
| Repository contracts | [`domain/irepository`](../../../domain/irepository) |
| GORM models | [`infra/repository/po`](../../../infra/repository/po) |
| Repository implementations | [`infra/repository/repo`](../../../infra/repository/repo) |
| Schema startup | [`infra/repository/migration`](../../../infra/repository/migration) |

## Adding a Conventional Resource

1. Define entity and ownership rules.
2. Add repository port methods.
3. Add PO, indexes, converter, and repository implementation.
4. Add DTOs and an assembler.
5. Add the application service and focused tests.
6. Add a thin handler and register routes.
7. Register the PO in migration and test startup against an existing schema.
8. Wire concrete dependencies in `boot/init.go`.
9. Update both code-guide package references.
