# 05. 委派、Goal 与调度

[指南首页](README.md) | [English](../en/05-delegation-goals-and-scheduling.md)

## 目的

该部分允许 Athena 把工作拆分给临时 Specialist，并在前端关闭后继续执行持久多步骤 Goal。它受到严格治理：Sub-Agent 不能绕过 Policy、Budget、Capability、Evidence 或 Verification。

## 持久 Specialist Delegation

[`application/service/delegation`](../../../application/service/delegation) 管理动态 Specialist 执行，标准链路为：

```text
Task Intent
  -> DelegatedOutcomeSpec
  -> DelegationProposal
  -> DelegationDecision
  -> Immutable Specialist Spec/Artifact
  -> Durable Run 与 Attempt
  -> 正常 Runtime Execution Chain
  -> Evidence 与 VerificationResult
  -> Aggregated Result
```

Delegation 是 Control Plane Decision。Runtime 执行已准入 Specialist，但不能自行创造不受治理的可执行身份。

## 使用同一执行语义

Specialist 必须经过与主 Agent 相同的冻结执行链：

```text
Outcome -> Grounding -> Plan -> Policy -> Run -> ActionAttempt
        -> Observation -> Verification -> Experience
```

不存在简化版 Sub-Agent 执行路径。Browser/Desktop Effect 仍需真实 Device Hub 和 Observation Evidence。

## Context 与 Capability 隔离

[`context_builder.go`](../../../application/service/delegation/context_builder.go) 构造最小权限 Specialist Context。除非明确需要，否则移除无关 Conversation、File、Knowledge、Skill、MCP/CLI/A2A、Internal Sub-Agent Structure 与 Visual Input。

Specialist 获得有界 `CapabilityView`，而不是整个 Registry。敏感值在持久化或委派前脱敏。

Specialist 不能递归创建更多 Specialist，父编排 Authority 始终负责 Plan 与 Aggregation。

## Policy、Budget 与 Artifact

Delegation 将 Proposal 和 Decision 分开记录。Decision 绑定 Policy Version、Context/Evidence Hash、Risk、Actor、Admitted Capability 与 Expiry。

执行开始前预留 Budget，终态时 Commit 或 Release。[`artifact_resolver.go`](../../../application/service/delegation/artifact_resolver.go) 为 Invocation 解析不可变、已批准的 Skill/Strategy/Build Artifact。

## Durable Authority 与恢复

[`orchestrator.go`](../../../application/service/delegation/orchestrator.go) 是 Delegation Run 的唯一持久 Authority，管理 Leader Election、Lease、Fencing、Heartbeat、Attempt、Outbox Event、Cancellation 与 Recovery。

来自过期或被替换 Lease 的 Late Result 会被拒绝。只有在 Policy、Idempotency 和 Side-Effect Evidence 允许时才能 Resume/Retry。

## 并行 Specialist

Parallel Scheduler 执行显式 DAG，而不是无界 Fan-out：

- Dependency 决定 Ready；
- Concurrency 与 Budget 有界；
- 独立 Branch 可以并行；
- Branch Failure Policy 控制 Partial Result 或 Replacement；
- Aggregation 去重 Evidence 并报告 Contradiction；
- User/Approval Wait 会传播而不是被隐藏。

相关文件为 `parallel_scheduler.go`、`parallel_execution.go` 和 `parallel_aggregator.go`。

## Ad-Hoc Specialist

临时 Specialist 只能通过相同的 Proposal、Policy、Capability、Budget 与 Audit 路径。它是声明式 Spec，不是直接执行的生成代码。

## 受治理 Delegation Learning

完成的 Delegated Outcome 可以成为 Learning Candidate 的证据。学习链与在线执行分离：

```text
Delegated Experience
  -> Candidate
  -> Offline Evaluation
  -> Human Review
  -> Shadow
  -> Canary
  -> Promotion 或 Disable
```

一次成功不能静默变成生产 Skill。

W1-W7 设计与验收文档位于 [`docs`](../../) 中的 `dso-w*.md` 及其中文版本。

## Durable Goal

[`application/service/orchestration`](../../../application/service/orchestration) 管理长期 Goal 与 Task Graph。Goal 包含有限 Plan、Task Dependency、Revision、Budget、Schedule/Approval State 和 Checkpoint。

Application Service 创建并转换 Goal；[`Supervisor`](../../../application/service/orchestration/supervisor.go) 领取 Runnable Task，通过 Runtime 或兼容 Device 执行，记录 Result 并推进 Graph。

关键保证：

- 前端关闭后 Goal 继续；
- Optimistic Revision 防止 Lost Update；
- Stable Execution ID 使 Retry 幂等；
- Pause 会取消 Active Work 并拒绝 Late Result；
- Checkpoint Hash 检测损坏的恢复状态；
- Device Action 需要真实 Observation Evidence；
- Task/World Slice 按 Owner 隔离。

## Scheduled Task

[`application/service/scheduledtask`](../../../application/service/scheduledtask) 管理周期用户任务、Approval Decision、Execution History 与 Polling Control Plane。

Schedule Trigger 使用稳定 Slot Identity，避免重启后重复触发。Internal Create Route 受 Service Auth 保护；用户 List/Update/Delete/Approval Route 受认证并按 Owner 隔离。

Scheduled Task 与 Durable Goal 有关联但不相同：Schedule 决定何时创建/激活工作，Goal Task Graph 决定持久多步骤工作如何推进。

## 优先阅读文件

| 文件 | 用途 |
| --- | --- |
| [`application/service/delegation/execution.go`](../../../application/service/delegation/execution.go) | 在线 Specialist 路由与执行 |
| [`application/service/delegation/orchestrator.go`](../../../application/service/delegation/orchestrator.go) | Durable Authority 与恢复 |
| [`application/service/delegation/context_builder.go`](../../../application/service/delegation/context_builder.go) | 最小权限 Context |
| [`application/service/delegation/policy.go`](../../../application/service/delegation/policy.go) | Delegation Policy |
| [`domain/entity/delegation`](../../../domain/entity/delegation) | 持久 Delegation Model |
| [`application/service/orchestration/service.go`](../../../application/service/orchestration/service.go) | Goal 用例 |
| [`application/service/orchestration/supervisor.go`](../../../application/service/orchestration/supervisor.go) | 后台 Goal Runner |
| [`application/service/scheduledtask`](../../../application/service/scheduledtask) | 周期任务 Control Plane |

## 修改检查清单

- Specialist 是否仍经过正常 Policy/Action/Observation/Verification Chain？
- Context 和 Capability Set 是否最小且显式？
- Proposal、Decision、Run 与 Attempt 是否为独立持久概念？
- Budget 是否 Transactional Reserve，终态是否幂等？
- 重启恢复能否证明 Side Effect 是否已发生？
- Goal 与 Schedule Transition 是否拒绝过期 Revision 和重复 Slot？
