# DSO-W4 并行 Specialist 执行图

状态：2026-08-20 本地验收通过

## 范围

DSO-W4 增加了有预算边界的并行 Specialist 执行，但没有创建第二套执行权威。每个分支仍完整经过已冻结的 Specialist 主链；调度器只负责协调不可变任务节点和持久化结果：

```text
ParallelSpecialistPlan
  -> 依赖感知调度器
  -> Specialist Run A / B / C
  -> 持久化分支 Observation 与 Evidence
  -> Evidence Specialist Run D
  -> 保留冲突的类型化聚合
  -> 唯一一次最终可见流结果
```

首个研究执行图包含三个独立来源角色和一个证据综合角色。综合节点必须等待所有来源节点进入终态后才能启动。并行执行通过运行上下文显式启用；生产默认仍走现有单 Specialist 路径。

## 治理与预算规则

- 执行前校验 `max_parallelism`、`max_runs`、节点预算和计划总预算。
- 调度器不会启动超过有效并发上限的 Worker。
- Provider 限流会降低后续任务的有效并发度。
- 重试和替补角色共享当前节点的预算与尝试次数上限。
- 父任务预算不足时，在创建任何分支前直接拒绝。
- 取消后会排空运行中的 Worker 并记录取消终态，不会误报成功。
- 节点的 `RUNNING` 状态无法持久化时，该分支不得启动。
- 每次 Runtime 调用分别记录 Prompt 和 Completion Token，并在最终流事件中精确汇总。

## 证据聚合

- 只有已完成或明确标记为部分完成的分支可以贡献证据。
- Claim 引用必须能解析到实际收集的 Evidence；证据不足时保持 unsupported。
- 计算重复率前会规范化 URL，移除片段和跟踪参数。
- 相互矛盾的值以 `CONFLICTING` 和多个备选值保留，不会静默合并。
- 每个 Claim 都执行计划配置的最小证据数量门禁。
- 聚合结果会输出可审计的协调 Token 比例和重复抓取比例。

## 持久化与界面

控制面持久化 `os_dso_parallel_plan`、`os_dso_parallel_node` 和 `os_dso_parallel_aggregate`。节点转换使用乐观 Revision 和幂等事件键；完成计划时，类型化聚合与终态事件原子写入。

聊天流会输出执行图 ID、角色、依赖、节点状态、配置并发度和有效并发度。前端将其渲染为实时 Specialist 执行图，同时保证只展示一个最终答案。

## 可复现验收证据

- 三个独立根节点达到实测峰值并发 `3`，第四个节点等待全部依赖完成。
- 已覆盖重试、替补、部分结果、等待用户、预算拒绝、限流降并发、取消和进度持久化失败路径。
- 合成证据对比把受支持证据覆盖从单 Agent 的 `1` 个来源提高到 `3` 个独立来源。
- 验收用例的重复 URL 比例为 `0%`，协调 Token 开销为 `5%`，低于 `15%` 和 `25%` 门禁。
- 冲突来源用例会保留两个备选结论，不会静默合并。
- 集成执行持久化 `1` 个计划、`4` 个节点、`4` 条完整 Specialist 主链、`4` 次模型调用和 `1` 个聚合结果。
- 最终集成流精确报告 `480` Prompt、`160` Completion、`640` Total Token。
- 前端收到持久化进度事件、零分支文本 Delta，以及唯一一次最终 `Done` 事件。
- `agent-runtime-client` 的 `go test ./...` 通过。
- `athena-protocol` 的 `go test ./draft/dso/v0alpha` 通过。
- `frontend/agent-ui` 的 `npm run lint` 和 `npm run build` 通过。

## 阶段结论

DSO-W4 本地退出门通过。并行路径在后续回放和 Canary 门禁提供生产证据前仍默认关闭。可以进入 DSO-W5。
