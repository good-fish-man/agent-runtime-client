# v0.9 生产就绪证据

本文档是证据索引，不代表外部发布工作已经完成。单元测试不能替代平台公证、渗透测试、72 小时稳定性测试或基于生产等价数据的恢复演练。

## 自动化门禁

| 门禁 | 可复现命令 | 预期结果 |
| --- | --- | --- |
| Protocol 校验与不可信内容信封 | 在 `athena-protocol` 执行 `go test ./protocol/operations/v1 ./protocol/v4 ./sdk/safety` | 通过 |
| Runtime 背压、超时、Prompt/Tool Output 隔离 | 在 `agent-runtime` 执行 `go test -race ./internal/operations ./internal/eino ./internal/prompt ./internal/research` | 通过 |
| 设备租约/Fencing、认证备份、隐私删除 | 执行 `go test -race ./application/service/control ./application/service/operations ./application/service/memory ./infra/repository/repo/experience` | 通过 |
| API、Repository、Migration 与 Owner Scope 回归 | 在 `agent-runtime-client` 执行 `go test ./...` | 通过 |
| Launcher State、签名 Manifest、解压预算与原子安装 | 在 `athena-launcher` 执行 `go test -race ./internal/launcher/deployment ./internal/release ./internal/state` | 通过 |
| Frontend 类型与生产构建 | 在 `frontend/agent-ui` 执行 `npm run lint && npm run build` | 通过 |

v1.0 Protocol Freeze 指纹只在 v1.0 冻结阶段更新。在此之前检测到指纹变化必须显式失败，不能忽略。

## 用户数据生命周期

- `GET /memory/export` 与 `GET /experience/export` 只导出当前认证用户保留的数据。
- `DELETE /memory` 必须传入 `DELETE ALL MEMORY`；Memory 删除标记中的私有字段会被清空。
- `DELETE /experience` 必须传入 `DELETE ALL EXPERIENCE`；Payload、脱敏记录及派生评测用例/运行会物理删除，只保留不可逆审计删除标记。
- Experience 保留期由用户控制，定时清理复用同一条 Payload 删除路径。
- Frontend 提供合并 JSON 导出，以及分别清除 Memory/Experience 的入口。

v0.9 不引入破坏性数据库重写。启动 Migration 保持增量；替换组件前必须创建经过认证的加密备份。恢复失败必须事务回滚，不能报告成功。

## 外部发布门禁

在运维人员附加带日期的产物或记录前，下列项目必须保持 `EXTERNAL_REQUIRED`：

| 门禁 | 必需证据 |
| --- | --- |
| 安全评审 | Threat Model 签字及独立 API/WebSocket/Vault 渗透报告，且无未关闭 P0/P1 |
| macOS | Developer ID 验签、公证 Ticket、arm64/amd64 安装/升级/回滚记录 |
| Windows | Authenticode 验签及受支持 Windows x64 安装/升级/回滚记录 |
| Linux | 白名单包签名及 x86-64/arm64 AppImage 安装/升级/回滚记录 |
| 稳定性 | 24/72 小时测试报告、SLO 快照、Task Event 零丢失 |
| 故障注入 | 网络抖动、数据库重启、进程崩溃、磁盘满、重连与重试证据 |
| 灾备 | 备份、验证、只校验恢复、正式恢复、身份检查和只读 Golden Task 记录 |
| 压测 | 多用户/多设备并发报告，包含队列拒绝、超时、Dispatch p95 与重复副作用计数 |

## 事件与日志证据

Launcher 安装可查看 `~/.athena/logs/launcher.log`、`postgres.log`、`agent-runtime.log` 与 `agent-runtime-client.log`。事件证据应保留 Trace ID、Task/Action/Observation ID、AgentBuild/RunManifest ID、设备 Lease Owner/Fencing Token、签名 Manifest 摘要和相关脱敏日志。禁止附加原始凭据、Cookie、Token 或私有附件字节。
