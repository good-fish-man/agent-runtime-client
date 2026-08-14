# Athena 1.0 控制面与运维

[English](ga-operations-v1.0.md) | [简体中文](ga-operations-v1.0.zh-CN.md)

Runtime Client 是 Athena 1.0 的认证控制面，负责用户隔离、模型凭据、Agent、会话、记忆、证据、声明式学习评审、发布治理、长期 Goal、设备控制、Plugin、备份、Readiness 和 Golden Journey 预检。

## GA 接口

接口位于配置的公共前缀下，默认是 `/api/agent-runtime-client/v1`。

| 方法 | 路径 | 权限 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/operations/readiness` | 已登录 | 汇总 Runtime、数据库、备份、设备和领域服务状态。 |
| `GET` | `/operations/golden-journeys` | 已登录 | 返回固定的十条 GA 核心用户旅程。 |
| `POST` | `/operations/golden-journeys/run` | 管理员 | 执行无副作用基础设施预检。 |
| `GET` | `/operations/snapshot` | 管理员 | 查看 Health、SLO、设备、队列和备份摘要。 |
| `POST` | `/operations/backups` | 管理员 | 创建加密备份。 |
| `POST` | `/operations/backups/:id/verify` | 管理员 | 校验 Manifest 与 Payload 完整性。 |

Golden Journey 接口是预检，不代表真实模型供应商、浏览器账号、安装器或平台签名已经执行。`install-mode` 与 `safe-upgrade` 在真实安装包矩阵完成前始终是 `EXTERNAL_REQUIRED`。

## 可追溯性

每次模型运行都会创建或解析不可变 `AgentBuild` 和 `RunManifest`。设备 Action 携带 `agent_build_id`、`run_manifest_id`、Capability Instance、Action ID 和 Trace ID；Observation 必须回传一致的部署来源后才能持久化。

## 管理员流程

1. 对外开放前修改默认 `athena` 密码。
2. 模型和网站凭据只保存在服务端 Vault。
3. Provider 必须签名，并配置显式授权与资源上限。
4. 升级前创建并验证加密备份。
5. 检查 `/operations/readiness`，不能忽略 `FAIL`、`BLOCKED` 或 `EXTERNAL_REQUIRED`。
6. 在隔离数据目录中演练恢复后再依赖该备份。

## 用户数据与隐私

- 模型 API Key 只在服务端解析，模型和 Agent API 不会向浏览器返回。
- 普通用户只能访问自己的 Agent、模型、会话、记忆、Goal、证据和凭据；管理员需要显式切换才能查看全局数据。
- Action Observation 持久化前会脱敏；临时截图二进制和凭据材料不会进入普通控制记录。
- 记忆和 Experience 的保留、导出、删除按用户隔离。

## 排障

- `runtime.readiness`：检查 Runtime `/readiness` 和 Runtime 日志。
- `database.durable`：检查 PostgreSQL、DSN、迁移与磁盘空间。
- `device.control`：检查 Launcher WebSocket、Device Token、用户绑定、能力上报和 Lease 过期时间。
- `recovery.backup`：创建并验证备份，检查备份 Manifest。
- 请求错误按 `trace_id` 定位；HTTP 边界只输出一次完整、带源码位置的错误链。

发布前执行 `go test ./...`。签名安装包、公证、真实浏览器流程、供应商账号和持续 SLO 验证仍属于外部门禁。
