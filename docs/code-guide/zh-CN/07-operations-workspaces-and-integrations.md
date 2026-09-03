# 07. 运维、工作区与集成

[指南首页](README.md) | [English](../en/07-operations-workspaces-and-integrations.md)

## 目的

本章说明让控制面能够长期运行并服务于单次 Chat 之外场景的辅助子系统：Health/Readiness、Backup/Recovery、Project Workspace、Config/Restart、Website Account、Dashboard、Voice Avatar 与外部 Channel。

## 生产运维

[`application/service/operations`](../../../application/service/operations) 聚合 Runtime、Database、Device Control、Delegation Recovery、Backup 与 Golden Journey Evidence。

它暴露三个相关但不同的概念：

- **Health**：组件当前是否响应；
- **SLO Evidence**：测量到的 Reliability、Latency 与 Recovery 行为；
- **GA Readiness**：必须不变量和真实 Golden Journey 是否得到证明。

Readiness 不能把 Unit Test 或 Preflight 报告为真实端到端 Browser/Device Pass。Golden Journey Evidence 按 Owner 隔离、验证完整性，并可由独立 E2E Runner 提交。

## Backup 与 Recovery

[`backup.go`](../../../application/service/operations/backup.go) 管理加密 Backup 的创建、Inventory、Verification 与 Restore。Recovery Operation 是高权限操作，必须审计。

嵌入式 PostgreSQL Binary 与 Database Data Directory 具有不同生命周期。更新打包的 Server Binary 不能删除用户数据。Restore 必须验证目标 Backup，并避免暴露密码或内部网络详情。

## Project Workspace

[`api/http/handler/public/workspace`](../../../api/http/handler/public/workspace) 是前端 Coding Workspace 使用的有界本地项目 API，支持：

- macOS、Windows 和支持的 Linux Desktop Native Folder Selection；
- Directory Import 与内存 Workspace Identity；
- 有界 File Tree 与 Text Preview；
- Text Search 与按相关性选择的 Context File；
- Structured Change 到 Patch 的构造；
- Patch Validation、Dry Run 与 Apply；
- 跨平台 Safe Path Resolution。

实现会排除大型/生成目录，限制 Entry 和 Byte，并要求每个 Path 保持在导入 Root 内。它不是自主 Runtime Filesystem Capability，不能接受任意 Shell Command。

## Config 与 Restart

[`api/http/handler/public/config`](../../../api/http/handler/public/config) 读取/写入配置的 Client、Runtime 与 Skills YAML，也代理 Runtime Status/Config 并执行受保护 Restart Check。

修改仅属于 Frontend 的 Theme Preference 不应重启 Runtime。只有改变进程所有设置时才需要重启对应服务。

Port 处理会区分当前托管进程与无关进程。Kill 无关 Port Owner 必须取得用户显式授权。

## Website Credential

[`application/service/browsercredential`](../../../application/service/browsercredential) 只在 PostgreSQL 保存 Credential Metadata，Username/Password Secret 保存在操作系统 Credential Vault，并通过 Protected Standard Input 或 Vault Reference 交给 `agent-browser`。

Agent 只获得已认证 Browser `session_id`，不会获得 Password。CAPTCHA、QR、Slider 与 2FA 会切换到可见 Human Takeover，而不是绕过保护。

Credential 按 Owner 和 Domain 隔离；URL 必须为不含内嵌凭据的绝对 HTTP(S) URL；Browser Session ID 会校验格式。

## Dashboard

[`api/http/handler/public/dashboard`](../../../api/http/handler/public/dashboard) 聚合 Count、Task、Token、Approval、Activity 与 Recent Conversation。Dashboard Metric 应来自持久执行/Chat/Model Usage，而不是硬编码百分比。

## 本地模型运维

Model HTTP Package 包含机器 Environment 检测、免费本地模型安装、Duplicate 检查、Progress Polling、本地 Runtime Lifecycle Mode，以及 Fine-tuning/Distillation Workflow。

这些操作会启动受限外部进程。Command Argument 必须显式，Output/Error 必须捕获，Job 在前端导航后仍应可查询。

## 外部集成

| Package | 职责 |
| --- | --- |
| `channel` | 外部 Conversation Channel 配置 |
| `callback` | 认证的异步 Callback 入口 |
| `weixin` | Weixin 专用适配器 |
| `job` | 异步 Job Execution Record |
| `command` | 受控 Command Request 接口 |
| `voiceavatar` | Owner-Scoped Voice Avatar Media |

每个 Adapter 应把外部 Payload 转换为已有 Application Use Case，不能创建并行的 Agent 执行实现。

## Scheduled 与 Internal Route

Launcher/后台服务通过专用 Internal Route 创建 Scheduled Work、Goal、Backup 与 Golden Journey Evidence。这些接口使用 Internal Credential 保护，普通 Bearer Token 不能访问。

## 优先阅读位置

| 模块 | 起点 |
| --- | --- |
| Health/SLO | [`application/service/operations/service.go`](../../../application/service/operations/service.go) |
| GA Readiness | [`application/service/operations/ga.go`](../../../application/service/operations/ga.go) |
| Backup/Recovery | [`application/service/operations/backup.go`](../../../application/service/operations/backup.go) |
| Workspace | [`api/http/handler/public/workspace/workspace_handler.go`](../../../api/http/handler/public/workspace/workspace_handler.go) |
| Config/Restart | [`api/http/handler/public/config`](../../../api/http/handler/public/config) |
| Website Credential | [`application/service/browsercredential/service.go`](../../../application/service/browsercredential/service.go) |
| Dashboard | [`api/http/handler/public/dashboard`](../../../api/http/handler/public/dashboard) |
| Local Model | [`api/http/handler/public/model`](../../../api/http/handler/public/model) |

## 修改检查清单

- Operational Status 是否区分 Health、Preflight 与真实 E2E Proof？
- Backup Secret 与内部 Topology 是否不进入 Response/Log？
- Workspace Path 是否可能逃逸 Root 或超过 Size Limit？
- Config Change 是否只重启拥有该配置的 Service？
- External Adapter 是否复用正常认证 Runtime Path？
- 外部进程是否有界、可取消且可观察？
