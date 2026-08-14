# v0.9 运维与恢复手册

## 控制面

登录用户可以读取 `/operations/health`。查看备份、创建、验证和恢复必须使用管理员会话。Launcher 的预更新接口只接受 `X-Athena-Internal-Token`；未配置 Token 时所有内部请求都会被拒绝。

设备所有权使用数据库租约和单调递增的 Fencing Token。同一时刻只有当前租约持有者可以保持设备在线；旧进程不能覆盖新持有者状态，断连也必须先验证所有权和租约后才能标记离线。

## 加密备份

Launcher 配置 `pg_dump`、`pg_restore`、备份目录和 256 位密钥文件的绝对路径。PostgreSQL custom-format 数据直接流式进入分块 AES-GCM 加密，不在磁盘写明文 dump。每个 Artifact 和规范化 Manifest 都有 SHA-256。

恢复流程：

1. `POST /operations/backups` 创建恢复点。
2. `POST /operations/backups/{id}/verify` 校验 Manifest、Artifact Hash、大小和所有 AES-GCM 认证标签。
3. 调用 restore 且设置 `validate_only=true`，只验证不修改数据库。
4. 正式恢复还必须提供 `confirmation: "RESTORE {id}"` 和已验证的 Manifest SHA-256。

Launcher 替换本地版本前会先创建备份。备份失败时旧服务继续运行，更新保持可重试状态。

## State 丢失

Launcher 会把安装身份同步到 `~/.athena/secrets` 下权限为 0600 的恢复文件。`state.json` 丢失时，数据库密码、Browser Vault Key、备份 Key、本机内部 Token 和 Device ID 会从恢复文件找回。如果 state 与恢复文件冲突，启动会失败，而不是静默轮换身份。

应将 `~/.athena/secrets` 与数据库备份分开保管。如果 state 和备份加密 Key 同时丢失，加密备份按安全设计无法恢复。

## 恢复演练

1. 停止用户流量并创建最新备份。
2. 验证目标恢复点。
3. 先执行 validate-only。
4. 通过独立渠道确认 Backup ID 与 Manifest Hash。
5. 停止 Worker，执行恢复，重启后检查 `/operations/health`。
6. 验证登录、Agent、会话、Goal、Plugin Registry 和一个只读任务。
7. 保留日志与 Manifest 作为演练证据。
