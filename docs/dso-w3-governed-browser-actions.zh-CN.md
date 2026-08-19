# DSO-W3 受治理浏览器动作链

状态：2026-08-20 本地验收通过

## 范围

DSO-W3 在不创建第二套执行权威的前提下，把 Specialist 决策接入真实桌面浏览器动作。每个可变浏览器操作统一经过以下持久化链路：

```text
DecisionTurn
  -> ActionProposal
  -> 不可变 PlanCandidate
  -> ExecutionContext
  -> 独立 PolicyDecision
  -> PlanRun
  -> GovernedActionAttempt
  -> 资源版本二次校验
  -> 动作级 ResourceLease
  -> ActionDispatcher / Control Hub
  -> Launcher observation
  -> VerificationResult
  -> 同一 Specialist Attempt 下的 receive_observation DecisionTurn
```

`ActionProposal` 本身没有执行权。策略拒绝不会到达设备 dispatcher；需要审批的动作保持等待状态；设备没有返回 observation 时必须记录为 `UNKNOWN_OUTCOME`，绝不伪造成功。

## 资源权威

- Launcher 使用 `browser://session/<session>/tab/<tab>` 标识浏览器资源。
- `resource_version` 优先采用感知指纹，缺失时使用确定性内容哈希。
- Runtime Client 在规划时读取一次资源，并在获取租约前立即再次读取。
- 页面版本变化会在设备执行前使动作失效。
- 只读操作使用共享租约，可变操作使用独占租约。
- 可空唯一 active key 为同一标签页提供跨实例单写者围栏。

## 持久化

以下记录均可独立审计：

- `os_dso_action_proposal`
- `os_dso_plan_candidate`
- `os_dso_execution_context`
- `os_dso_action_policy_decision`
- `os_dso_action_plan_run`
- `os_dso_governed_action_attempt`
- `os_dso_action_verification`

原有 `os_resource_lease`、`os_decision_turn` 和事件流补全完整追踪。设备 observation 会回写原 Specialist Attempt，而不是形成另一条旁路运行。

## 本地验收证据

- 50/50 次确定性浏览器受治理运行均持久化完整 proposal、plan、policy、run、attempt、lease、observation 与 verification 链。
- 同一标签页 20 个并发写者的数据库竞态中只有一个独占租约成功。
- 页面漂移和过期 expected version 均在设备分发前被拒绝。
- 策略拒绝时设备分发次数为零。
- 分发前取消时设备动作数为零。
- observation 丢失会进入 `UNKNOWN_OUTCOME`，并保留原始错误链。
- 设备 observation 以 `receive_observation` turn 追加到同一 Specialist Attempt。
- delegation 包不直接依赖 Launcher，所有执行必须通过 `ActionDispatcher`。
- `agent-runtime-client` 全量 `go test ./...` 通过。
- 协议校验和浏览器资源身份单元测试通过。

Launcher 的完整浏览器测试包含本地 HTTP listener。当执行沙箱禁止 loopback listener 时，该部分保留为发布环境门禁；在限制外执行通过前，本文件不会把它写成本地通过项。

## 退出结论

DSO-W3 本地退出门通过，可以进入 DSO-W4。受治理执行语义已接受，但草案 wire schema 和数据库布局仍可在协议冻结里程碑前演进。
