# DSO W6 生产加固

W6 将耐久 Specialist 编排完善为可运维、可恢复的系统，但不会仅凭单元测试宣称已经生产就绪。本地自动化证据和外部发布门禁保持严格分离。

## 运行边界

- `EXACT_CONFIG` 只校验不可变 Invocation Manifest 和全部绑定 Artifact，绝不调用模型、浏览器、设备、Capability 或外部服务。
- `RECORDED_OBSERVATION_SIMULATION` 只能消费来源 Run 已拥有的 Observation 引用，不能产生副作用。
- `LIVE_REEXECUTION` 必须携带显式审批引用，并注入可校验 Policy 的执行器。生产组合根默认不注入真实执行器，因此默认 fail-closed。
- 已进入终态的 Replay 不可变且幂等，重复请求只返回之前的结果。
- 真实 Replay 在外部调用后崩溃会留下结果未知的 `RUNNING` 状态。Athena 要求人工或受控流程对账，绝不自动再次执行。

## 故障矩阵

| 故障点 | 恢复规则 | 防重复副作用 |
| --- | --- | --- |
| Replay 持久化之前 | 使用相同请求重试 | 尚未发生外部调用 |
| Replay 持久化之后 | 确定性模式可恢复；真实模式必须对账 | 不重复真实执行 |
| 真实执行之前 | 对账已保存的 `RUNNING` 状态 | 不自动重执行 |
| 真实执行之后、写入终态之前 | 标记结果未知并对账 | 不自动重执行 |
| Scheduler Owner 崩溃 | Standby 在租约过期后用更高 fencing token 接管 | 旧 Owner 无法重新成为当前 Owner |
| Worker 迟到结果 | Attempt revision 和 lease 校验拒绝结果 | 记录为 fenced 证据 |
| 取消传播 | 记录取消请求到终态的耗时 | SLO 目标 P95 不超过 5 秒 |

## HA 与诊断

委派 Scheduler 使用数据库租约和单调递增的 fencing token。默认租约为 20 秒，因此健康 Standby 能在 30 秒以内接管过期 Owner。Operations 页面展示最近 24 小时 DSO 指标：

- 可用率至少 99.9%
- 取消传播 P95 不超过 5 秒
- 已确认重复副作用必须为 0
- 已恢复 Attempt 和已隔离迟到结果数量

任一目标不满足，或无法采集诊断数据时，统一健康快照会降级为 `DEGRADED`。

## 数据生命周期

- 导出严格限定当前 Owner；若秘密特征到达导出边界，系统会阻止导出。
- 删除按 Owner 和显式 Cutoff 在事务内执行，并保留不可变 Retention Tombstone。
- HTTP 删除接口要求完整输入 `DELETE DELEGATION DATA`。
- Replay 结果只保存引用与哈希，不保存凭据或 Provider 原始载荷。

## 威胁模型

| 威胁 | 已实现控制 | 剩余门禁 |
| --- | --- | --- |
| 跨用户 Replay 或 Observation 注入 | 所有查询包含 Owner 条件，并校验 Observation 属于来源 Run | 多租户渗透测试 |
| 崩溃后重复不可逆动作 | 未知结果必须对账，真实执行不自动重试 | 真实设备故障注入 |
| Scheduler 脑裂 | 数据库租约、revision CAS、单调 fencing token | 多节点数据库故障切换演练 |
| 伪造 Replay 审批 | 默认没有真实执行器；未来执行器必须解析独立且未过期的 `PolicyDecision` | 启用前安全评审 |
| 导出或 Replay 泄密 | 协议只传引用，导出边界扫描秘密特征 | 独立 DLP 评审 |
| 破坏性数据请求 | 认证 Owner 绑定和精确确认短语 | 发布前备份恢复演练 |

## 自动化证据

以下本地门禁已经通过：

```text
go test ./application/service/delegation ./application/service/operations \
  ./api/http/handler/public/delegationops ./api/http/router/public ./boot \
  ./infra/repository/repo/delegation ./infra/repository/migration
npm run lint
npm run build
```

覆盖 Replay 模式隔离、跨 Owner 拒绝、外来 Observation 拒绝、终态幂等、崩溃恢复、真实执行结果未知、HA fencing/接管、生命周期隔离、诊断、认证路由和破坏性确认。

## 外部发布门禁

W6 本地实现完成，但生产启用仍需以下外部证据：

1. 多节点 PostgreSQL 与服务故障切换报告，证明接管少于 30 秒且没有重复确认副作用。
2. 预生产长时间压测报告，证明可用率至少 99.9%，取消传播 P95 小于 5 秒。
3. 独立渗透测试和威胁模型评审。
4. 已签字的数据保留、导出、删除、备份和恢复评审。
5. 在启用 `LIVE_REEXECUTION` 前完成可校验 Policy 的真实 Replay 执行器评审。
