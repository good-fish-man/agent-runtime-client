# Package 参考

[指南首页](README.md) | [English](../en/package-reference.md)

本页列出仓库中每个 Go Package 目录。生成/构建产物和纯文档目录不在列表中。

## 进程与装配

| Package | 用途 |
| --- | --- |
| `.` | 进程入口、重启循环、HTTP 生命周期与 Signal 处理 |
| `boot` | Composition Root，构造依赖并管理后台生命周期 |
| `config` | YAML/环境变量配置类型、默认值与校验 |

## HTTP API

| Package | 用途 |
| --- | --- |
| `api/http` | Gin Engine 与顶层 HTTP Concern |
| `api/http/router` | Runtime 执行与 Health 路由 |
| `api/http/router/public` | 管理/公共 API 路由目录 |
| `api/http/middleware` | Trace、Auth、Recovery、CORS、Error 与 Stream-Aware Logging |
| `api/http/handler/runtime` | Runtime Run/Stream/Media/Resume/Stop 与 SSE Handler |
| `api/http/handler/control` | Desktop Device WebSocket/Control Handler |
| `api/http/handler/public/agent` | Agent CRUD、Binding 与 Deployment 相关 API |
| `api/http/handler/public/browsercredential` | 网站账号 Metadata 与认证 Session Login API |
| `api/http/handler/public/callback` | 外部 Callback 入口 |
| `api/http/handler/public/channel` | Channel 配置与操作 |
| `api/http/handler/public/command` | 受控 Command Execution 请求接口 |
| `api/http/handler/public/config` | Client/Runtime/Skills Config 与 Restart/Status API |
| `api/http/handler/public/dashboard` | Dashboard Metric 与 Recent Activity 聚合 |
| `api/http/handler/public/delegationlearning` | 受治理 Delegation Learning Lifecycle API |
| `api/http/handler/public/delegationops` | Delegation Replay、Export、Delete API |
| `api/http/handler/public/deployment` | Build、Shadow、Canary、Promotion、Manifest、Rollback API |
| `api/http/handler/public/experience` | Experience、Preference、Export、Search、Evaluation API |
| `api/http/handler/public/job` | 异步 Job 管理 API |
| `api/http/handler/public/knowledge` | Evidence Claim、Contradiction、Snapshot、Ontology API |
| `api/http/handler/public/knowledge_base` | 用户 Knowledge Base CRUD 与 Recall Test |
| `api/http/handler/public/learning` | Candidate、Demonstration、Evolution Status/Scan API |
| `api/http/handler/public/memory` | 长期 Memory List/Create/Export/Delete API |
| `api/http/handler/public/model` | Model/Key/Catalog、本地安装、Lifecycle、Training API |
| `api/http/handler/public/operations` | Health、SLO、Readiness、Backup、Golden Journey API |
| `api/http/handler/public/orchestration` | Durable Goal、Task、Schedule、Checkpoint API |
| `api/http/handler/public/pluginregistry` | Provider Install、Scan、Review、Transition、Audit API |
| `api/http/handler/public/scheduledtask` | 周期 Task 与 Approval API |
| `api/http/handler/public/skill` | Skill CRUD/Upload/Name Validation API |
| `api/http/handler/public/user` | Register、Login、Profile、Avatar 与 Admin User API |
| `api/http/handler/public/voiceavatar` | 上传 Voice Avatar Media API |
| `api/http/handler/public/weixin` | Weixin Adapter API |
| `api/http/handler/public/workspace` | 有界本地项目 Tree/Search/Context/Patch API |

## Application DTO 与 Assembler

| Package | 用途 |
| --- | --- |
| `application/dto/agent` | Agent Use Case Request/Response |
| `application/dto/channel` | Channel Request/Response |
| `application/dto/job` | Job Request/Response |
| `application/dto/knowledge_base` | Knowledge Base Request/Response |
| `application/dto/model` | Model、Key、Catalog、Usage、Runtime Mode DTO |
| `application/dto/runtime` | Runtime Invocation、Stream、Media、Control DTO |
| `application/dto/skill` | Skill Request/Response |
| `application/dto/user` | Identity/User Request/Response |
| `application/assembler/agent` | Agent DTO/Entity Mapping |
| `application/assembler/channel` | Channel DTO/Entity Mapping |
| `application/assembler/job` | Job DTO/Entity Mapping |
| `application/assembler/knowledge_base` | Knowledge Base DTO/Entity Mapping |
| `application/assembler/model` | Model DTO/Entity/Catalog Mapping |
| `application/assembler/runtime` | HTTP/Application Runtime Input Mapping |
| `application/assembler/skill` | Skill DTO/Entity Mapping |
| `application/assembler/user` | User DTO/Entity Mapping |

## Application Service

| Package | 用途 |
| --- | --- |
| `application/service/agent` | Agent CRUD、Ownership、Model/Binding Rule |
| `application/service/browsercredential` | OS Vault 网站凭据与 Login Handoff |
| `application/service/channel` | Channel Use Case 编排 |
| `application/service/control` | Device Hub、Action/Observation、Lease、Recovery、Approval |
| `application/service/delegation` | Durable Specialist、Policy、Budget、Parallel、Replay、Learning |
| `application/service/deployment` | Immutable Build、Manifest、Shadow/Canary/Promotion/Rollback |
| `application/service/experience` | 脱敏 Experience、Retrieval、Statistic、Evaluation |
| `application/service/job` | Job Use Case 编排 |
| `application/service/knowledge` | Evidence Claim、Contradiction、Retrieval、Ontology Governance |
| `application/service/knowledge_base` | Knowledge Base Use Case |
| `application/service/learning` | Declarative Candidate、Demonstration、Governed Evolution |
| `application/service/memory` | Owner/Agent-Scoped Long-Term Memory Control |
| `application/service/model` | Model Config、Key、Catalog、Usage、Ownership |
| `application/service/operations` | Health/SLO、GA Readiness、Backup、Golden Journey Evidence |
| `application/service/orchestration` | Durable Goal、Task Graph、Checkpoint、Background Supervisor |
| `application/service/pluginregistry` | Signed Capability Provider Lifecycle 与 Audit |
| `application/service/runtime` | Request Hydration、Runtime Call、Device Continuation、Record |
| `application/service/scheduledtask` | Recurring Schedule、Polling、Approval、Execution History |
| `application/service/skill` | Skill Use Case |
| `application/service/user` | Register/Login/Profile/Admin User Use Case |

## Domain Entity

| Package | 用途 |
| --- | --- |
| `domain/entity/agent` | Agent 业务表示 |
| `domain/entity/channel` | Channel 业务表示 |
| `domain/entity/chat` | Conversation/Session/Message/Token Model |
| `domain/entity/control` | 规范 Device Action/Observation/Session Protocol Alias |
| `domain/entity/delegation` | Proposal、Decision、Run、Attempt、Budget、Evidence |
| `domain/entity/deployment` | Build、Promotion、Manifest、Rollout/Rollback Entity |
| `domain/entity/experience` | Experience 与 Evaluation Domain Type |
| `domain/entity/job` | Job Domain 表示 |
| `domain/entity/knowledge` | Claim、Evidence、Contradiction、Ontology Domain Type |
| `domain/entity/knowledge_base` | Knowledge Base 业务表示 |
| `domain/entity/learning` | Candidate、Skill/Strategy Version、Demonstration Type |
| `domain/entity/model` | Model、Key、Catalog、Training、Usage Concept |
| `domain/entity/orchestration` | Durable Goal/Task/Checkpoint/Schedule Protocol Alias |
| `domain/entity/runtime` | Runtime Input/Output/Stream/Media/Capability Type |
| `domain/entity/skill` | Skill 业务表示 |
| `domain/entity/user` | User Identity/Profile 表示 |

## Domain Port 与 Service

| Package | 用途 |
| --- | --- |
| `domain/irepository/agent` | Agent Persistence Port |
| `domain/irepository/channel` | Channel Persistence Port |
| `domain/irepository/chat` | Conversation/Statistic Persistence Port |
| `domain/irepository/control` | Device/Task/Action/World State Durable Store Port |
| `domain/irepository/delegation` | Delegation Durable Authority/Store Port |
| `domain/irepository/deployment` | Build 与 Release Governance Store Port |
| `domain/irepository/experience` | Experience/Evaluation Store Port |
| `domain/irepository/job` | Job Persistence Port |
| `domain/irepository/knowledge` | Evidence Knowledge Store Port |
| `domain/irepository/knowledge_base` | Knowledge Base Persistence Port |
| `domain/irepository/learning` | Candidate/Artifact/Demonstration Store Port |
| `domain/irepository/model` | Model/Key/Catalog/Usage Persistence Port |
| `domain/irepository/orchestration` | Durable Goal Store/Claim Port |
| `domain/irepository/runtime` | `agent-runtime` Execution Gateway Port |
| `domain/irepository/skill` | Skill Persistence Port |
| `domain/irepository/user` | User Persistence Port |
| `domain/srv/agent` | Agent Domain Service |
| `domain/srv/channel` | Channel Domain Service |
| `domain/srv/job` | Job Domain Service |
| `domain/srv/knowledge_base` | Knowledge Base Domain Service |
| `domain/srv/model` | Model Domain Service |
| `domain/srv/runtime` | Runtime Gateway Port 的 Domain Facade |
| `domain/srv/skill` | Skill Domain Service |
| `domain/srv/user` | User Domain Service |

## Infrastructure

| Package | 用途 |
| --- | --- |
| `infra/data` | Context-Bound 共享 GORM Handle |
| `infra/db` | PostgreSQL/MySQL Connection、Pool 与 GORM Logging |
| `infra/pkg` | Infra Conversion/Helper |
| `infra/runtime` | gRPC Runtime Client、Mapping、Stream Consumption、Trace Metadata |
| `infra/repository/migration` | 启动 Schema Migration 与幂等 Seed |
| `infra/repository/converter/agent` | Agent Entity/PO Mapping |
| `infra/repository/converter/channel` | Channel Entity/PO Mapping |
| `infra/repository/converter/job` | Job Entity/PO Mapping |
| `infra/repository/converter/knowledge_base` | Knowledge Base Entity/PO Mapping |
| `infra/repository/converter/model` | Model Entity/PO Mapping |
| `infra/repository/converter/skill` | Skill Entity/PO Mapping |
| `infra/repository/converter/user` | User Entity/PO Mapping |
| `infra/repository/po/agent` | Agent GORM Table |
| `infra/repository/po/browser` | Website Credential Metadata Table |
| `infra/repository/po/channel` | Channel GORM Table |
| `infra/repository/po/chat` | Chat/Session/Token/Usage Table |
| `infra/repository/po/control` | Device/Task/Action/Observation/World State Table |
| `infra/repository/po/delegation` | Delegation Authority/Budget/Event/Result Table |
| `infra/repository/po/deployment` | Build/Manifest/Promotion/Canary/Rollback Table |
| `infra/repository/po/experience` | Experience/Redaction/Evaluation Table |
| `infra/repository/po/job` | Job Execution Table |
| `infra/repository/po/knowledge` | Claim/Evidence/Ontology Table |
| `infra/repository/po/knowledge_base` | Knowledge Base GORM Table |
| `infra/repository/po/learning` | Candidate/Version/Demonstration Table |
| `infra/repository/po/memory` | Long-Term Memory Table |
| `infra/repository/po/model` | Model/Key/Catalog/Training/Usage Table |
| `infra/repository/po/operations` | Golden Journey Evidence Table |
| `infra/repository/po/orchestration` | Goal/Task/Checkpoint/Schedule Table |
| `infra/repository/po/plugin` | Provider Package/Review/Audit Table |
| `infra/repository/po/runtime` | Durable Media Job Table |
| `infra/repository/po/scheduledtask` | Recurring Task/Approval/History Table |
| `infra/repository/po/skill` | Skill GORM Table |
| `infra/repository/po/user` | User GORM Table |
| `infra/repository/repo/agent` | Agent Repository 实现 |
| `infra/repository/repo/channel` | Channel Repository 实现 |
| `infra/repository/repo/control` | Device/Control/World State Store 实现 |
| `infra/repository/repo/delegation` | Delegation Store 实现 |
| `infra/repository/repo/deployment` | Deployment Store 实现 |
| `infra/repository/repo/experience` | Experience/Evaluation Store 实现 |
| `infra/repository/repo/job` | Job Repository 实现 |
| `infra/repository/repo/knowledge` | Evidence Knowledge Store 实现 |
| `infra/repository/repo/knowledge_base` | Knowledge Base Repository 实现 |
| `infra/repository/repo/learning` | Learning Store 实现 |
| `infra/repository/repo/model` | Model/Key/Catalog Repository 实现 |
| `infra/repository/repo/operations` | Operations Evidence Store 实现 |
| `infra/repository/repo/orchestration` | Durable Goal Store 实现 |
| `infra/repository/repo/runtime` | Media Job Repository 实现 |
| `infra/repository/repo/skill` | Skill Repository 实现 |
| `infra/repository/repo/user` | User Repository 实现 |

## 共享 Helper 与公共 Type

| Package | 用途 |
| --- | --- |
| `pkg/authctx` | `context.Context` 中的认证 User/Admin Identity |
| `pkg/query` | 通用 Query、Paging 与 Sorting Type |
| `pkg/ulid` | Stable ULID Generation |
| `pkg/validate` | 共享 Request/Domain Validation Helper |
| `types/apierror` | 稳定 Public Error Code/Status/Message |
| `types/consts` | Route、Context、Trace 与 Service Constant |
| `types/response` | 标准 JSON Response Envelope |

`pkg/errtrace` 当前是空的兼容目录；新的 Error Chain 行为应进入共享 `logx` 库，而不是维护第二套本地实现。
