# 03. 资源、领域模型与持久化

[指南首页](README.md) | [English](../en/03-resources-domain-and-persistence.md)

## 目的

本章说明用户、Agent、Model、Skill、Knowledge Base、Channel、Job 等控制面资源复用的纵向切片结构。

## 纵向切片

传统资源通常沿以下路径流动：

```text
HTTP Handler
  -> Application DTO
  -> Application Service
  -> Assembler
  -> Domain Entity/Service
  -> Repository Port
  -> GORM Repository/Converter/PO
  -> Database
```

高级子系统不一定每层都有独立类型，但依赖仍必须通过接口指向 Domain/Application 语义。

## 各层职责

### DTO：`application/dto`

DTO 描述用例输入与输出，可以包含 JSON/Query 友好类型、分页、局部更新和认证 Actor ID。DTO 绝不能把 Secret 返回浏览器。

### Assembler：`application/assembler`

Assembler 负责 DTO 与 Domain Entity 互转，使字段映射不散落在 Handler 和 Domain Service 中。它不应查询数据库或做 Policy Decision。

### Entity：`domain/entity`

Entity 独立于 Gin 和 GORM 表达业务状态。协议密集的模块可以 Alias `athena-protocol` 中的冻结结构，避免重复定义。

### Domain Service：`domain/srv`

Domain Service 实现资源规则并依赖 Repository Port。Agent、User、Model、Skill、Knowledge Base、Channel、Job 和 Runtime 都有常规领域服务。

### Port：`domain/irepository`

Repository Interface 描述 Domain/Application 需要什么，而不是 SQL 如何编写。Runtime 也是 Port，`RuntimeGateway` 隔离生成的 gRPC 类型。

### PO：`infra/repository/po`

PO 是 GORM 表结构。数据库专属字段、Index、Soft Delete、JSON Column 和乐观 Revision 属于此层。

### Converter 与 Repository

`infra/repository/converter` 映射 PO 与 Entity；`infra/repository/repo` 使用 `Data.DB(ctx)` 实现 Port。高级子系统的事务操作大于普通 CRUD，因此有时使用 Store Interface 和专用 PO。

## 核心资源分类

### 用户

用户服务负责注册、登录、密码 Hash、Profile、Avatar、Admin Level 和 Actor Identity。Bootstrap Admin 属于安装边界，只根据环境变量凭据创建不存在的账号。

### Agent

Agent 保存用户可编辑行为与绑定，包括主 LLM、可选 Embedding/Media Model、Skill、Knowledge Base 与 Sub-Agent 声明。公共 Agent 是系统模板。Prompt 与模型凭据在服务端执行前补全，不作为浏览器往返 Secret。

### Model 与 Model Key

Model 和 Provider Key 是两个资源：

- Model 通过 ID 引用 Key Record；
- 修改一个 Key 可以更新所有引用 Model；
- 每个 Key 和私有 Model 都按 Owner 隔离；
- 本地 Provider 可以不需要 Key；
- 只有管理员能全局启停 Model；
- Runtime Mode 控制本地模型常驻、按需启动或关闭；
- 最近 24 小时 Usage、Token、Latency 和 Success Metric 来自真实执行记录。

Model Package 还负责 Catalog、Environment 检测、防重复本地安装、安装进度、Fine-tuning 与 Distillation Job。

### Skill 与 Knowledge Base

管理资源描述 Agent 允许使用什么。Runtime 只在服务端补全后获得已选择且相关的 Artifact。上传 Skill 会被验证和存储；受治理学习 Artifact 使用后续章节中的 Learning/Deployment Lifecycle。

### Memory

Memory 按用户和 Agent 隔离，支持 List、Export 和 Delete，并在执行前注入相关记忆。后台记忆提取失败不能抹掉原始 Chat 结果。

### Channel 与 Job

Channel 表示外部会话入口/出口；Job 与 Callback 跟踪异步执行和集成状态。它们必须遵守与 Web 请求相同的 Owner 与 Trace 规则。

## 数据库访问

[`infra/data/data.go`](../../../infra/data/data.go) 包装共享 GORM Handle。Repository 必须使用 `Data.DB(ctx)`，使 SQL 日志、取消和 Trace Context 跟随请求。

[`infra/db/db.go`](../../../infra/db/db.go) 支持 PostgreSQL 与 MySQL，设置连接池并安装共享 `logx` GORM Logger。PostgreSQL 使用 Simple Protocol，避免启动迁移后旧 Prepared Plan 的 Result Shape 错误。

## Transaction、Revision 与 Idempotency

简单 CRUD 可以使用单个 GORM Operation。多对象状态机应使用 Transaction 与显式乐观 Revision。持久 Action、Promotion、Goal 和 Delegation Run 必须有稳定 Idempotency Key，因为 Retry 和重启恢复是正常流程。

普通资源通常使用 `deleted_at` Soft Delete；终态 Audit/Evidence Record 可能采用 Append-Only。

## Migration 所有权

所有活动 PO 必须注册在 [`infra/repository/migration/migration.go`](../../../infra/repository/migration/migration.go)。如果 Schema 只存在于测试或手工数据库中，该功能不算完成。

修改持久化时：

- 定义已有数据的 Upgrade 行为；
- 在发布策略要求时提供 Rollback SQL；
- 保持 Seed 幂等；
- 不存储明文模型/网站凭据；
- 对破坏性修改和 Rename 增加 Migration Test。

## 优先阅读位置

| 模块 | 起点 |
| --- | --- |
| Agent 资源 | [`application/service/agent`](../../../application/service/agent) |
| Model 资源 | [`application/service/model`](../../../application/service/model) |
| User 资源 | [`application/service/user`](../../../application/service/user) |
| Domain Entity | [`domain/entity`](../../../domain/entity) |
| Repository Contract | [`domain/irepository`](../../../domain/irepository) |
| GORM Model | [`infra/repository/po`](../../../infra/repository/po) |
| Repository 实现 | [`infra/repository/repo`](../../../infra/repository/repo) |
| Schema 启动 | [`infra/repository/migration`](../../../infra/repository/migration) |

## 新增常规资源

1. 定义 Entity 与 Ownership Rule。
2. 添加 Repository Port Method。
3. 添加 PO、Index、Converter 与 Repository 实现。
4. 添加 DTO 与 Assembler。
5. 添加 Application Service 和聚焦测试。
6. 添加薄 Handler 并注册 Route。
7. 在 Migration 中注册 PO，并使用已有 Schema 测试启动。
8. 在 `boot/init.go` 装配具体依赖。
9. 更新中英文 Package Reference。
