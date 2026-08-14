# v0.9 安全威胁模型

## 范围与信任边界

Athena 把 Frontend、公开网页、浏览器 DOM/快照、文件、Plugin 输出、模型输出和设备 Observation 文本全部视为不可信数据。已认证用户意图只具备用户权限，不能覆盖系统策略。Runtime、Control Plane 策略、签名 Protocol 合约、Launcher Trust Root 和运维人员显式审批构成可信计算边界。

数据跨越以下边界：

1. Frontend 到 Control Plane：登录态 HTTP/SSE，请求只能访问当前用户范围。
2. Runtime 到 Control Plane：内部服务 Token 与 Trace 身份。
3. Launcher 到 Control Plane：Device Token、数据库租约 Owner 与单调 Fencing Token。
4. Control Plane 到 Runtime：类型化请求，API Key 不返回浏览器。
5. 外部内容到模型：`athena.untrusted-content.v1` 数据信封，包含摘要、风险、指标和预算。
6. 发布基础设施到 Launcher：签名且有时效的 Manifest、SBOM、精确大小、Artifact Hash/签名和平台签名证据。

## 主要威胁与控制

| 威胁 | 主要控制 | 必须采用的失败方式 |
| --- | --- | --- |
| 跨用户读取 | Owner Scope Repository、用户绑定、管理员显式全局视图 | 拒绝且不泄露资源是否存在 |
| 设备冒充/重放 | Device Token、用户绑定、Lease Owner、Fencing Token、过期与幂等 | 拒绝旧 Socket/消息 |
| WebSocket 伪造 | 连接身份与服务端 Action Ownership 查询 | 拒绝并审计 |
| 凭据泄露 | Vault 间接引用、敏感字段脱敏、移除附件 Data、公开 DTO 不含 Key | 持久化/日志/模型前脱敏 |
| 直接/间接提示注入 | 类型化 Capability 路由、外部数据信封、风险指标、Action Schema 校验 | 可以引用证据，禁止当作指令执行 |
| Tool Output Poisoning | 非用户可见 Tool 结果统一包装；Action 信封独立解析校验 | 文本不能直接转成副作用 |
| Plugin 入侵 | 签名不可变包、Grant 子集、进程/资源隔离与撤销 | 终止 Provider，不拖垮核心服务 |
| 发布替换/解压炸弹 | Ed25519 Manifest、大小/SHA/签名、HTTPS 重定向策略、归档预算、保留回滚 | 保留最后已验证安装 |
| 备份篡改/部分恢复 | AES-GCM、Manifest HMAC、受保护 Key、validate-only、单事务 | 修改数据库前拒绝 |
| State/Secret 替换 | 普通文件/权限校验、有界读取、TOCTOU 身份校验、原子 fsync 写入 | 失败关闭并保留证据 |
| 多实例脑裂 | 共享数据库租约与单调 Fencing | 只允许最新 Owner 派发/回传 |
| 资源耗尽 | 队列上限、请求超时、并发 Gate、Context/Output/Archive 预算 | 拒绝或超时并计入 SLO |

## 权限矩阵

| 操作 | 普通用户 | 管理员 | Launcher/内部服务 |
| --- | --- | --- | --- |
| 查看自己的 Agent、Model、Task、Memory、Experience | 允许 | 允许 | 拒绝 |
| 查看其他用户私有资源 | 拒绝 | 仅显式管理员视图 | 拒绝 |
| 创建/修改自己的资源 | 允许 | 允许 | 拒绝 |
| 审批自己的设备动作 | 允许 | 允许 | 拒绝 |
| 全局 Plugin Trust/撤销 | 拒绝 | 允许 | 仅签名安装 |
| 健康快照 | 已登录可见 | 允许 | 仅显式内部路由 |
| 备份查看/创建/验证/恢复 | 拒绝 | 允许 | 内部 Token 仅允许更新前创建 |
| 设备连接/心跳/Observation | 拒绝 | 只读检查 | 有效 Device Token + 当前租约 |
| 轮换 Vault/Release/Backup Trust Root | 拒绝 | 离线运维流程 | Runtime 中拒绝 |

## 验证证据

自动化测试覆盖 Owner Scope、Device Token、旧 Fencing 拒绝、Observation Socket 绑定、Secret 脱敏、间接注入分类、签名包、归档预算、备份伪造/移动、Key 权限、State 符号链接/权限、队列 Drain 和超时计数。系统明确禁止绕过 CAPTCHA/2FA，必须切换用户接管。平台公证/签名、渗透测试、24/72 小时稳定性测试和灾备演练属于外部发布证据，不能用单元测试成功冒充。

安全事件必须保留 Trace ID、Action/Observation ID、Build/RunManifest ID、Lease Owner/Token、Release Manifest Digest 和相关服务日志；事件记录禁止包含凭据或原始附件 Data。
