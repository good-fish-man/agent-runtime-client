# Package Reference

[Guide index](README.md) | [简体中文](../zh-CN/package-reference.md)

This is an inventory of every Go package directory. Generated/build output and documentation-only directories are intentionally excluded.

## Process and Composition

| Package | Purpose |
| --- | --- |
| `.` | Process entry point, restart loop, HTTP lifecycle, signal handling |
| `boot` | Composition root; constructs dependencies and owns background lifetimes |
| `config` | YAML/environment configuration types, defaults, and validation |

## HTTP API

| Package | Purpose |
| --- | --- |
| `api/http` | Gin engine construction and top-level HTTP concerns |
| `api/http/router` | Runtime execution and health routes |
| `api/http/router/public` | Management/public API route catalog |
| `api/http/middleware` | trace, auth, recovery, CORS, errors, and stream-aware logging |
| `api/http/handler/runtime` | Runtime run/stream/media/resume/stop HTTP and SSE handlers |
| `api/http/handler/control` | desktop device WebSocket/control handlers |
| `api/http/handler/public/agent` | Agent CRUD, bindings, deployment-facing Agent APIs |
| `api/http/handler/public/browsercredential` | website account metadata and authenticated-session login endpoints |
| `api/http/handler/public/callback` | external callback ingress |
| `api/http/handler/public/channel` | channel configuration and channel-facing operations |
| `api/http/handler/public/command` | controlled command execution request surface |
| `api/http/handler/public/config` | Client/Runtime/Skills config and restart/status endpoints |
| `api/http/handler/public/dashboard` | dashboard metric and recent-activity aggregation |
| `api/http/handler/public/delegationlearning` | governed delegation-learning lifecycle endpoints |
| `api/http/handler/public/delegationops` | delegation replay, export, and deletion endpoints |
| `api/http/handler/public/deployment` | build, shadow, canary, promotion, manifest, rollback APIs |
| `api/http/handler/public/experience` | Experience, preferences, export, search, and evaluation APIs |
| `api/http/handler/public/job` | asynchronous job management endpoints |
| `api/http/handler/public/knowledge` | evidence claims, contradictions, snapshots, and ontology APIs |
| `api/http/handler/public/knowledge_base` | user knowledge-base CRUD and recall tests |
| `api/http/handler/public/learning` | candidates, demonstrations, evolution status/scan APIs |
| `api/http/handler/public/memory` | long-term memory list/create/export/delete APIs |
| `api/http/handler/public/model` | model/key/catalog, local install, lifecycle, training APIs |
| `api/http/handler/public/operations` | health, SLO, readiness, backup, Golden Journey APIs |
| `api/http/handler/public/orchestration` | durable Goal, task, schedule, and checkpoint APIs |
| `api/http/handler/public/pluginregistry` | provider installation, scan, review, transition, audit APIs |
| `api/http/handler/public/scheduledtask` | recurring task and approval APIs |
| `api/http/handler/public/skill` | Skill CRUD/upload/name validation APIs |
| `api/http/handler/public/user` | registration, login, profile, avatar, and admin user APIs |
| `api/http/handler/public/voiceavatar` | uploaded voice-avatar media APIs |
| `api/http/handler/public/weixin` | Weixin adapter endpoints |
| `api/http/handler/public/workspace` | bounded local project tree/search/context/patch APIs |

## Application DTOs and Assemblers

| Package | Purpose |
| --- | --- |
| `application/dto/agent` | Agent use-case request/response types |
| `application/dto/channel` | channel request/response types |
| `application/dto/job` | job request/response types |
| `application/dto/knowledge_base` | knowledge-base request/response types |
| `application/dto/model` | model, key, catalog, usage, and runtime-mode DTOs |
| `application/dto/runtime` | Runtime invocation, stream, media, and control DTOs |
| `application/dto/skill` | Skill request/response types |
| `application/dto/user` | identity/user request/response types |
| `application/assembler/agent` | Agent DTO/entity mapping |
| `application/assembler/channel` | channel DTO/entity mapping |
| `application/assembler/job` | job DTO/entity mapping |
| `application/assembler/knowledge_base` | knowledge-base DTO/entity mapping |
| `application/assembler/model` | model DTO/entity/catalog mapping |
| `application/assembler/runtime` | HTTP/application Runtime input mapping |
| `application/assembler/skill` | Skill DTO/entity mapping |
| `application/assembler/user` | user DTO/entity mapping |

## Application Services

| Package | Purpose |
| --- | --- |
| `application/service/agent` | Agent CRUD, ownership, model/binding rules |
| `application/service/browsercredential` | OS-vault website credentials and login handoff |
| `application/service/channel` | channel use-case orchestration |
| `application/service/control` | device Hub, Action/Observation, leases, recovery, approvals |
| `application/service/delegation` | durable specialists, policy, budgets, parallelism, replay, learning |
| `application/service/deployment` | immutable builds, manifests, shadow/canary/promotion/rollback |
| `application/service/experience` | sanitized Experience, retrieval, statistics, and evaluation |
| `application/service/job` | job use-case orchestration |
| `application/service/knowledge` | evidence claims, contradictions, retrieval, ontology governance |
| `application/service/knowledge_base` | knowledge-base use cases |
| `application/service/learning` | declarative candidates, demonstrations, governed Evolution |
| `application/service/memory` | owner/Agent-scoped long-term memory controls |
| `application/service/model` | model configuration, keys, catalog, usage, ownership |
| `application/service/operations` | health/SLO, GA readiness, backups, Golden Journey evidence |
| `application/service/orchestration` | durable Goals, task graph, checkpoints, background Supervisor |
| `application/service/pluginregistry` | signed Capability Provider lifecycle and audit |
| `application/service/runtime` | request hydration, Runtime calls, device continuation, recording |
| `application/service/scheduledtask` | recurring schedules, polling, approvals, execution history |
| `application/service/skill` | Skill use cases |
| `application/service/user` | registration/login/profile/admin user use cases |

## Domain Entities

| Package | Purpose |
| --- | --- |
| `domain/entity/agent` | Agent business representation |
| `domain/entity/channel` | channel business representation |
| `domain/entity/chat` | conversation/session/message/token models |
| `domain/entity/control` | canonical device Action/Observation/session protocol aliases |
| `domain/entity/delegation` | delegation proposals, decisions, runs, attempts, budgets, evidence |
| `domain/entity/deployment` | builds, promotions, manifests, rollout/rollback entities |
| `domain/entity/experience` | Experience and evaluation domain types |
| `domain/entity/job` | job domain representation |
| `domain/entity/knowledge` | claims, evidence, contradictions, ontology domain types |
| `domain/entity/knowledge_base` | knowledge-base business representation |
| `domain/entity/learning` | candidate, Skill/Strategy version, demonstration types |
| `domain/entity/model` | model, key, catalog, training, and usage concepts |
| `domain/entity/orchestration` | durable Goal/task/checkpoint/schedule protocol aliases |
| `domain/entity/runtime` | Runtime input, output, stream, media, capability types |
| `domain/entity/skill` | Skill business representation |
| `domain/entity/user` | user identity/profile representation |

## Domain Ports and Services

| Package | Purpose |
| --- | --- |
| `domain/irepository/agent` | Agent persistence port |
| `domain/irepository/channel` | channel persistence port |
| `domain/irepository/chat` | conversation/statistics persistence port |
| `domain/irepository/control` | device/task/action/world-state durable store port |
| `domain/irepository/delegation` | delegation durable authority/store port |
| `domain/irepository/deployment` | build and release-governance store port |
| `domain/irepository/experience` | Experience/evaluation store port |
| `domain/irepository/job` | job persistence port |
| `domain/irepository/knowledge` | evidence knowledge store port |
| `domain/irepository/knowledge_base` | knowledge-base persistence port |
| `domain/irepository/learning` | candidate/artifact/demonstration store port |
| `domain/irepository/model` | model/key/catalog/usage persistence port |
| `domain/irepository/orchestration` | durable Goal/store/claim port |
| `domain/irepository/runtime` | `agent-runtime` execution gateway port |
| `domain/irepository/skill` | Skill persistence port |
| `domain/irepository/user` | user persistence port |
| `domain/srv/agent` | Agent domain service |
| `domain/srv/channel` | channel domain service |
| `domain/srv/job` | job domain service |
| `domain/srv/knowledge_base` | knowledge-base domain service |
| `domain/srv/model` | model domain service |
| `domain/srv/runtime` | Runtime domain facade over the gateway port |
| `domain/srv/skill` | Skill domain service |
| `domain/srv/user` | user domain service |

## Infrastructure

| Package | Purpose |
| --- | --- |
| `infra/data` | context-bound shared GORM handle |
| `infra/db` | PostgreSQL/MySQL connection, pool, and GORM logging |
| `infra/pkg` | infrastructure conversion/helpers |
| `infra/runtime` | gRPC Runtime client, mappings, stream consumption, trace metadata |
| `infra/repository/migration` | startup schema migration and idempotent seed data |
| `infra/repository/converter/agent` | Agent entity/PO mapping |
| `infra/repository/converter/channel` | channel entity/PO mapping |
| `infra/repository/converter/job` | job entity/PO mapping |
| `infra/repository/converter/knowledge_base` | knowledge-base entity/PO mapping |
| `infra/repository/converter/model` | model entity/PO mapping |
| `infra/repository/converter/skill` | Skill entity/PO mapping |
| `infra/repository/converter/user` | user entity/PO mapping |
| `infra/repository/po/agent` | Agent GORM tables |
| `infra/repository/po/browser` | website credential metadata tables |
| `infra/repository/po/channel` | channel GORM tables |
| `infra/repository/po/chat` | chat/session/token/usage GORM tables |
| `infra/repository/po/control` | device/task/action/observation/world-state tables |
| `infra/repository/po/delegation` | delegation authority, budget, event, result tables |
| `infra/repository/po/deployment` | build/manifest/promotion/canary/rollback tables |
| `infra/repository/po/experience` | Experience/redaction/evaluation tables |
| `infra/repository/po/job` | job execution tables |
| `infra/repository/po/knowledge` | claim/evidence/ontology tables |
| `infra/repository/po/knowledge_base` | knowledge-base GORM tables |
| `infra/repository/po/learning` | candidate/version/demonstration tables |
| `infra/repository/po/memory` | long-term memory tables |
| `infra/repository/po/model` | model/key/catalog/training/usage tables |
| `infra/repository/po/operations` | Golden Journey evidence tables |
| `infra/repository/po/orchestration` | Goal/task/checkpoint/schedule tables |
| `infra/repository/po/plugin` | provider package/review/audit tables |
| `infra/repository/po/runtime` | durable media job tables |
| `infra/repository/po/scheduledtask` | recurring task/approval/history tables |
| `infra/repository/po/skill` | Skill GORM tables |
| `infra/repository/po/user` | user GORM tables |
| `infra/repository/repo/agent` | Agent repository implementation |
| `infra/repository/repo/channel` | channel repository implementation |
| `infra/repository/repo/control` | device/control/world-state store implementation |
| `infra/repository/repo/delegation` | delegation store implementation |
| `infra/repository/repo/deployment` | deployment store implementation |
| `infra/repository/repo/experience` | Experience/evaluation store implementation |
| `infra/repository/repo/job` | job repository implementation |
| `infra/repository/repo/knowledge` | evidence knowledge store implementation |
| `infra/repository/repo/knowledge_base` | knowledge-base repository implementation |
| `infra/repository/repo/learning` | learning store implementation |
| `infra/repository/repo/model` | model/key/catalog repository implementation |
| `infra/repository/repo/operations` | operations evidence store implementation |
| `infra/repository/repo/orchestration` | durable Goal store implementation |
| `infra/repository/repo/runtime` | media job repository implementation |
| `infra/repository/repo/skill` | Skill repository implementation |
| `infra/repository/repo/user` | user repository implementation |

## Shared Helpers and Public Types

| Package | Purpose |
| --- | --- |
| `pkg/authctx` | authenticated user/admin identity in `context.Context` |
| `pkg/query` | generic query, paging, and sorting types |
| `pkg/ulid` | stable ULID generation |
| `pkg/validate` | shared request/domain validation helpers |
| `types/apierror` | stable public error codes/status/messages |
| `types/consts` | route, context, trace, and service constants |
| `types/response` | standard JSON response envelope |

`pkg/errtrace` is currently an empty compatibility directory; new error-chain behavior belongs in the shared `logx` library rather than a second local implementation.
