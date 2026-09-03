# 长期 Goal 与 Supervisor v0.7

Athena v0.7 将用户明确要求的长期任务转换为有限、耐久的任务图。每个 Goal 只属于一个用户和一个 Agent，包含可观察的成功标准和硬预算；每次状态变化都会生成带校验和的 Checkpoint。

## 执行模型

1. Runtime 只把明确提出后台、跨天、跨设备或稍后恢复的请求识别为长期 Goal。
2. `PersistentGoalCreate` 提交声明式任务图，最多 64 个节点、深度 4、并发专家 8 个。
3. Runtime Client 通过 `POST /goals/planned` 原子保存 Goal、任务图和首个 Checkpoint，避免两段式创建留下孤立 Draft。
4. Supervisor 直接执行服务端专家；浏览器和桌面专家只会路由到同时在线且具备全部所需能力的设备。
5. Runtime 继续使用现有 Control Hub。外部副作用只有收到成功的真实设备 Observation 后才算确认，其幂等键会写入下一个 Checkpoint。
6. 服务重启时，孤立的 `RUNNING` 任务恢复为 `READY`，同时保留已确认副作用和按设备隔离的世界状态。

每次任务尝试都有新的 `execution_id`，而 `idempotency_scope` 在重试和重启后保持稳定。系统会拒绝旧执行实例迟到的结果；设备 Action 的幂等键由稳定作用域和规范化动作内容生成，因此生成的 Action ID 改变也不会重复已观察到的外部副作用。

Checkpoint 组成 SHA-256 前驱链。读取单个 Checkpoint 会验证内容哈希，读取历史还会验证序号连续性与每个 `previous_checksum` 链接。被篡改或拼接的链不能用于恢复。

## 定时任务

Cron 只负责触发。每个时间槽都会创建与交互任务完全相同的 Goal、Task、Specialist Run、Checkpoint 和审计记录。`schedule_id:slot` 唯一约束可阻止重试或并发 Tick 创建重复任务；Trigger 会持久记录尝试次数、退避、完成、对账和通知送达状态。

执行前审批采用默认拒绝。审批 ID 会先写入首个 Checkpoint，再向用户展示审批卡片。公共 Goal Resume 无法越过未决审批，只有校验过用户归属的审批服务才能消费完全匹配的 ID；拒绝后会终止本次触发，避免永久占用调度并发槽。

## 安全边界

- 任务图必须有限，每个任务的预算必须为正且不能超过 Goal 总预算。
- Supervisor 禁止递归创建长期 Goal、隐藏子 Agent、生成代码直接执行或无限循环。
- `WAITING_USER`、审批、设备离线、截止时间和预算耗尽都会落盘，绝不伪装成成功。
- 专家只能读取声明的 world slice；嵌套凭据、Cookie、Token、原始截图和无限制设备状态会被递归裁剪，不能写入 Goal。
- 每个专家结果必须有 Producer、Trace 和运行来源。Runtime 结果还必须绑定 RunManifest、AgentBuild 和模型配置指纹；设备 Specialist 只有收到成功 Observation 才能完成，外部副作用必须同时保留 Observation 引用。

## 运维配置

```yaml
orchestration:
  enabled: true
  scan_interval_sec: 3
  max_concurrent_runs: 2
```

对应环境变量是 `ARC_ORCHESTRATION_ENABLED`、`ARC_ORCHESTRATION_SCAN_INTERVAL_SEC` 和 `ARC_ORCHESTRATION_MAX_CONCURRENT_RUNS`。用户接口位于 `/goals`；`POST /goals/planned` 提供用户级原子创建，Runtime 通过带内部令牌的 `/internal/goals` 创建已规划 Goal。任务启动、结果写入和手工 Checkpoint 写入同样要求严格匹配的内部服务令牌。

常用只读接口：

- `GET /goals/:id`：当前 Goal、任务图、运行记录和最新 Checkpoint。
- `GET /goals/:id/checkpoints`：经过验证的 Checkpoint 历史。
- `GET /goals/schedule-triggers?schedule_id=...`：按用户隔离的触发与通知历史。
- `GET /goals/:id/tasks/:taskID/world-slice`：按任务字段过滤、按设备隔离的世界视图。

Inbox 会显示触发来源、执行尝试、设备路由、Specialist 来源、证据/Observation/副作用数量、调度通知、Checkpoint 哈希、Token 预算和暂停/恢复按钮。复杂 Goal 应在 Chat 中规划；Inbox 的创建表单只提供有边界的「Research → Synthesis」模板。

## 验收证据

自动化测试覆盖五天旅行规划与追问、离线等待后跨设备接管、递归过滤 World Slice、Token/时间/查询/页面/Action 预算、重启恢复不重复已确认副作用、稳定 Action 幂等、时间槽去重、调度扫描器生命周期、用户归属审批消费、无 Observation 的设备假成功拒绝、失败结果不能验证成功条件，以及损坏或断裂 Checkpoint 链拒绝恢复。

## 回滚

停止服务后执行 [`migrations/v0.7-orchestration-rollback.sql`](migrations/v0.7-orchestration-rollback.sql)。该操作会永久删除 Goal 历史，需要保留时应先导出。
