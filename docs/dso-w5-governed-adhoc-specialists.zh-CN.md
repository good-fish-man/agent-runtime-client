# DSO-W5 受治理动态临时 Specialist

状态：2026-08-20 本地验收通过

## 范围

DSO-W5 在 Registry 缺少精确角色时，以受审基础 Profile 和声明式临时 Overlay 形成 Specialist。它不会生成可执行代码、创建 Provider、修改系统 Prompt、暴露 Secret，也不会直接写入生产 Profile Registry。

当前解析顺序为：

```text
精确受审 Profile
  -> 受审 general-read Profile + 受限 Overlay（显式开启）
  -> 受审 research fallback（生产默认）
```

动态路径必须由内部运行标志 `athena.dso.adhoc_specialists=true` 显式启用。未启用时，未知角色的临时 Specialist 生产曝光率为零，并回退到受审 Research Profile。

## 不可变治理对象

- `SpecialistProfile` 经过评审、内容寻址和生产批准，定义 Capability、Context、Prompt、Output 与 Risk 上限。
- `AdHocSpecialistOverlay` 只能描述当前角色和 Outcome、缩小 Capability 与 Context，并设置有边界的输出 Schema 参数。
- `OverlayAdmissionDecision` 独立绑定 Overlay Hash、基础 Profile Hash、父任务 Capability/Context 快照、Policy 版本和有效期。
- `SpecialistProfileCandidate` 至少需要三次独立成功运行，并始终保持 `REVIEW_REQUIRED` 与 `activation_allowed=false`。
- `InvocationManifest.specialist_overlay_ref` 让实际使用的临时定义可回放、可审计。

严格 JSON 解码会拒绝所有未知字段。Overlay 类型本身不提供 Prompt、Provider、Secret、Command、Script、Code 或 Executor 字段。

## 持久化与隔离

控制面持久化：

- `os_dso_adhoc_overlay`
- `os_dso_overlay_admission`
- `os_dso_adhoc_run_outcome`
- `os_dso_profile_candidate`

Overlay 与 Admission 原子写入。所有查询和 Outcome 写入都按 Owner 隔离。同一次运行可以幂等重放，冲突重放会被拒绝。临时 Overlay 最长一小时过期，其他用户无法解析或复用。

## 安全门禁

- 请求 Capability 必须同时是受审基础 Profile 和父任务 Capability 的严格子集。
- 即使父集合意外包含，Terminal、Shell、Command、Python、Payment、Purchase、File Delete 和 Filesystem Write 仍会被拒绝。
- Context Class、引用和字节上限只能同时缩小基础与父任务边界。
- Prompt Override、脚本标记、Shell Pipeline、可执行代码模式和 Secret 特征会被拒绝并记录原因。
- 禁止替换受审 Output Schema。
- 临时 Specialist 不能继续委派。
- 多次成功只能创建评审候选，绝不会自动激活或修改生产 Profile。

## 本地验收证据

- 安全的只读 Overlay 成功准入，并完整经过同一条 Proposal -> Decision -> Run -> Attempt -> Verification 主链。
- Terminal、Payment、File Delete、Prompt Injection 和 Secret 用例均被拒绝并保留审计原因。
- 未开启功能标志的未知角色创建 `0` 条 Overlay，并使用受审 Research Profile。
- 跨 Owner 查询 Overlay 和 Admission 返回空。
- 第 1、2 次成功不创建 Candidate；第 3 次只创建一个不可激活的 `REVIEW_REQUIRED` Candidate。
- InvocationManifest 绑定精确 Overlay Ref，前端流展示临时角色、基础 Profile 和 Admission Decision。
- 精确持久化重放成功；冲突 Outcome 重放返回幂等冲突。
- `agent-runtime-client` 的 `go test ./...` 通过。
- `athena-protocol` 的 `go test ./draft/dso/v0alpha` 通过。
- `frontend/agent-ui` 的 `npm run lint` 与 `npm run build` 通过。

## 阶段结论

DSO-W5 本地退出门通过。动态 Overlay 路径在 W6 的 Replay、Chaos 和生产加固证据通过前保持显式开启。可以进入 DSO-W6。

本地退出不宣称已经获得相对 Main Agent fallback 的生产统计显著质量增益。依据路线门禁，自动路由仍保持关闭。
