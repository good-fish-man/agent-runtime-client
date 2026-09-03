# Athena 1.0 控制面与运维

[English](ga-operations-v1.0.md) | [简体中文](ga-operations-v1.0.zh-CN.md)

Runtime Client 是 Athena 1.0 的认证控制面，负责用户隔离、模型凭据、Agent、会话、记忆、证据、声明式学习评审、发布治理、长期 Goal、设备控制、Plugin、备份、Readiness 和 Golden Journey 预检。

## GA 接口

接口位于配置的公共前缀下，默认是 `/api/agent-runtime-client/v1`。

| 方法 | 路径 | 权限 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/operations/readiness` | 已登录 | 汇总 Runtime、数据库、备份、设备和领域服务状态。 |
| `GET` | `/operations/golden-journeys` | 已登录 | 返回固定的十条 GA 核心用户旅程。 |
| `POST` | `/operations/golden-journeys/run` | 管理员 | 执行无副作用基础设施预检；永远不会返回 `PASS`。 |
| `POST` | `/internal/operations/golden-journeys/evidence` | 可信 E2E Runner | 保存一套由独立执行器真实运行的完整 E2E 证据。必须携带 `X-Athena-Internal-Token` 和明确的 `owner_id`；浏览器管理员会话不能写入发布证据。 |
| `GET` | `/operations/health` | 已登录 | 查看 Health、SLO、设备、队列和备份摘要。 |
| `POST` | `/operations/backups` | 管理员 | 创建加密备份。 |
| `POST` | `/operations/backups/:id/verify` | 管理员 | 校验 Manifest 与 Payload 完整性。 |

预检接口不代表真实模型供应商、浏览器账号、安装器或平台签名已经执行。`install-mode` 与 `safe-upgrade` 在真实安装包矩阵完成前始终是 `EXTERNAL_REQUIRED`。

预检结果使用 `verification_level=PREFLIGHT`，只能返回 `NOT_RUN`、`FAIL`、`BLOCKED` 或 `EXTERNAL_REQUIRED`。独立执行器只能用同一个 `run_id` 一次提交十条 `verification_level=E2E` 结果；每个通过步骤必须包含目录要求的全部证据类型。服务按用户把结果追加写入 `os_ga_journey_result`，并使用内容 SHA-256 防篡改。之后再次运行预检不会遮蔽最近一次 E2E 证据；只有十条真实旅程全部通过，`golden.suite` 才会通过。

上传 E2E 套件前，可离线校验固定目录、共同的运行标识、步骤证据和 JSON 结构：

```bash
cd /path/to/athena-protocol
go run ./cmd/validate-ga-evidence -file /path/to/golden-suite.json
```

内部证据接口会拒绝无效服务令牌、缺失或非法 Owner、未知 JSON 字段、尾随 JSON、超过 4 MiB 的请求、不完整套件和混合 `run_id`。

## 可追溯性

每次模型运行都会创建或解析不可变 `AgentBuild` 和 `RunManifest`。设备 Action 携带 `agent_build_id`、`run_manifest_id`、Capability Instance、Action ID 和 Trace ID；Observation 必须回传一致的部署来源后才能持久化。

## 管理员流程

1. 首次登录后、对外开放前轮换安装生成的管理员引导密码。
2. 模型和网站凭据只保存在服务端 Vault。
3. Provider 必须签名，并配置显式授权与资源上限。
4. 升级前创建并验证加密备份。
5. 检查 `/operations/readiness`，不能忽略 `FAIL`、`BLOCKED` 或 `EXTERNAL_REQUIRED`。
6. 在隔离数据目录中演练恢复后再依赖该备份。

启动迁移保持增量与幂等。v1.0 回归测试会先创建代表性的 v0.9 用户、会话和记忆，再连续执行两次迁移并确认数据仍然完整。

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
