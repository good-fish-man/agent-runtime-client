# 长期 Goal 与 Supervisor v0.7

Athena v0.7 将用户明确要求的长期任务转换为有限、耐久的任务图。每个 Goal 只属于一个用户和一个 Agent，包含可观察的成功标准和硬预算；每次状态变化都会生成带校验和的 Checkpoint。

## 执行模型

1. Runtime 只把明确提出后台、跨天、跨设备或稍后恢复的请求识别为长期 Goal。
2. `PersistentGoalCreate` 提交声明式任务图，最多 64 个节点、深度 4、并发专家 8 个。
3. Runtime Client 原子保存 Goal、任务、专家结果、调度触发记录和 Checkpoint。
4. Supervisor 直接执行服务端专家；浏览器和桌面专家只会路由到同时在线且具备全部所需能力的设备。
5. Runtime 继续使用现有 Control Hub。外部副作用只有收到成功的真实设备 Observation 后才算确认，其幂等键会写入下一个 Checkpoint。
6. 服务重启时，孤立的 `RUNNING` 任务恢复为 `READY`，同时保留已确认副作用和按设备隔离的世界状态。

## 安全边界

- 任务图必须有限，每个任务的预算必须为正且不能超过 Goal 总预算。
- Supervisor 禁止递归创建长期 Goal、隐藏子 Agent、生成代码直接执行或无限循环。
- `WAITING_USER`、审批、设备离线、截止时间和预算耗尽都会落盘，绝不伪装成成功。
- 专家只能读取声明的 world slice；凭据、Cookie、原始截图和无限制设备状态不会写入 Goal。
- 每个专家结果必须包含 RunManifest、AgentBuild、模型配置指纹、设备、Trace、预算和证据来源。

## 运维配置

```yaml
orchestration:
  enabled: true
  scan_interval_sec: 3
  max_concurrent_runs: 2
```

对应环境变量是 `ARC_ORCHESTRATION_ENABLED`、`ARC_ORCHESTRATION_SCAN_INTERVAL_SEC` 和 `ARC_ORCHESTRATION_MAX_CONCURRENT_RUNS`。用户接口位于 `/goals`；Runtime 通过带内部令牌的 `/internal/goals` 创建已规划 Goal。

Inbox 会显示 Goal、专家状态、Token 预算、Checkpoint 和暂停/恢复按钮。复杂 Goal 应在 Chat 中规划；Inbox 的创建表单只提供有边界的「Research → Synthesis」模板。

## 回滚

停止服务后执行 [`migrations/v0.7-orchestration-rollback.sql`](migrations/v0.7-orchestration-rollback.sql)。该操作会永久删除 Goal 历史，需要保留时应先导出。
