# 01. 架构、启动与配置

[指南首页](README.md) | [English](../en/01-architecture-startup-and-configuration.md)

## 目的

本章解释进程如何启动、依赖如何装配、哪些能力是可选的，以及服务如何安全退出。

## 进程生命周期

入口是 [`main.go`](../../../main.go)，它刻意只承担少量进程边界职责：

1. 解析配置文件参数。
2. 调用 `boot.Init` 构造应用。
3. 探测 `agent-runtime`，但不把该探测作为致命启动依赖。
4. 启动 Gin HTTP Server。
5. 等待操作系统信号或内部重启请求。
6. 停止接收新流量，并在限定时间内优雅退出。

重启循环属于进程边界。应用服务需要重启时，应通过注入的 Channel 提交请求，而不是自己启动替代进程。

## Composition Root

[`boot/init.go`](../../../boot/init.go) 是唯一应了解完整对象图的位置。它负责：

- 加载日志和进程配置；
- 打开可选关系数据库；
- 执行启动迁移和幂等 Seed；
- 创建 gRPC Runtime Client 与领域 Gateway；
- 构造领域服务、应用服务和 HTTP Handler；
- 将设备 Hub 注入 Runtime 执行；
- 将设备终态任务接入 Experience 捕获；
- 将批准后的 Learning/Deployment Artifact 接入 Runtime 请求；
- 启动 Supervisor 和后台扫描器；
- 按生命周期反向顺序注册关闭函数。

这个文件较长，是因为依赖装配是显式的。新增 Constructor 和 Wiring 是正常的；把业务逻辑移入 `boot` 才是架构退化。

## 可选数据库模式

只有配置了数据库 Host 和 Name 时才启用数据库。没有数据库时，进程仍可提供功能受限的 Runtime Proxy 和文件配置 API。依赖数据库的 Handler 保持为 `nil`，公共路由注册会跳过它们。

该模式便于诊断，但完整 Athena 控制面必须使用持久存储。

## 配置

配置类型位于 [`config`](../../../config)，示例位于 [`manifest/config/config.yaml`](../../../manifest/config/config.yaml)。

主要配置组：

| 配置组 | 控制内容 |
| --- | --- |
| `server` | 监听地址、运行模式、管理 API Prefix |
| `runtime` | gRPC 地址、Runtime HTTP 地址、调用超时 |
| `db` | 数据库类型、连接字段、连接池、迁移日志 |
| `paths` | Client/Runtime/Skills 配置、上传与托管文件路径 |
| `control` | 设备 WebSocket 认证与 Lease 行为 |
| `memory` | 长期记忆提取与检索 |
| `plugins` | Registry、信任配置与 Package 路径 |
| 编排配置 | 后台 Goal、Delegation、Learning 与 Supervisor |

密钥应来自环境变量或 Launcher 的受保护状态，而不是提交到 YAML。

## 启动迁移与 Seed

[`infra/repository/migration`](../../../infra/repository/migration) 负责 Schema 初始化。`InitTables` 会执行：

- 所有活动 PO 的 GORM Migration；
- 旧 Control Plane 快照的规范表名升级；
- 显式删除过时的敏感字段；
- 根据环境变量凭据可选创建管理员；
- Upsert 模型目录；
- 幂等创建系统公共 Agent。

迁移绝不能重置已有用户的密码、角色或启用状态。Seed 必须可以安全重复执行。

受治理发布阶段的 SQL Rollback 位于 [`migrations`](../../../migrations) 和 [`docs/migrations`](../../migrations)。

## 后台组件

根据配置，`boot` 会启动多个长于单次请求生命周期的组件：

- 设备 Control Hub 与 Outbox 处理；
- Scheduled Task 扫描；
- 持久 Delegation 编排与恢复；
- Evolution 扫描器；
- 持久 Goal Supervisor；
- 其他有生命周期管理的服务。

每个组件都必须接收 Context 或暴露 `Close`，能够在重启后恢复，并且不能依赖前端页面保持打开。

## 关闭流程

`boot.Init` 返回的应用拥有 HTTP Engine 和全部 Closer。退出时应：

1. 取消根 Context 和后台 Context；
2. 阻止 Supervisor 领取新任务；
3. 释放 Lease，或将未完成工作标记为可恢复；
4. 关闭 Runtime 和数据库连接；
5. 在进程关闭期限内完成。

## 优先阅读文件

| 文件 | 原因 |
| --- | --- |
| [`main.go`](../../../main.go) | 进程边界与重启生命周期 |
| [`boot/init.go`](../../../boot/init.go) | 完整依赖图 |
| [`config/config.go`](../../../config/config.go) | 配置模型与默认值 |
| [`manifest/config/config.yaml`](../../../manifest/config/config.yaml) | 面向运维的示例 |
| [`infra/db/db.go`](../../../infra/db/db.go) | 数据库 Driver、连接池和 GORM 日志 |
| [`infra/repository/migration/migration.go`](../../../infra/repository/migration/migration.go) | 表结构与启动数据 |

## 修改检查清单

- 新依赖是否通过 Domain/Application 接口向内指向？
- 构造是否保留在 `boot`，而不是隐藏在 Handler？
- 可选依赖关闭时，功能是否正确降级？
- 在已有数据库上重复启动是否幂等？
- 非正常退出后，后台服务能否恢复？
- 凭据是否完全避免进入日志和已提交配置？
