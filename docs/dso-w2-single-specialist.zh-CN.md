# DSO-W2 单 Specialist 垂直切片

状态：2026-08-20 本地验收通过

## 范围

DSO-W2 引入第一条受治理的动态委托链路。普通请求和直接设备命令仍由 Runtime Client 走原有 Fast Path；复合研究请求可以通过持久化 Delegation Orchestrator 创建且仅创建一个有边界的 Research Specialist。

执行主链如下：

```text
RunStream
  -> RoutePolicy
  -> DelegationProposal + DelegationDecision
  -> DelegatedOutcomeSpec + SubagentSpec
  -> ContextBuilder
  -> CapabilityView + ActorBinding + InvocationManifest
  -> SubagentRun + SubagentAttempt
  -> 受治理的 agent-runtime 调用
  -> DecisionTurn + ModelInvocation
  -> TypedCandidateResult
  -> 外部 VerificationResult
```

生产 Dispatcher 不再注册旧的请求级 `SubAgentManager`。Specialist 不能再次委托，也不会获得父级聊天历史、MCP、CLI、A2A、文件、知识库对象或无限制工具；它只能使用父级 CapabilityView 明确准入的只读能力子集。

## 安全边界

- 上下文在持久化前执行用户归属隔离、白名单筛选、分类、大小预算、内容寻址和脱敏。
- Restricted 数据不会进入切片；外部网页中的提示注入文本会被标记为不可信证据。
- InvocationManifest 只保存凭据句柄，不保存模型明文 Key。
- `agent-runtime` 校验全部 Envelope 哈希、拒绝未知字段、不可用能力和写能力，在模型看到上下文前消费保留字段，并对篡改执行 fail-closed。
- Candidate 自报成功不能改变 Outcome；只有独立采集的证据才能产生 satisfied 验证结果。

## 本地验收证据

- Fast Path：200 次采样，p95 小于 50 ms，Specialist 调用数和持久化行数均为零。
- Golden Path：复合研究任务连续运行 20 次，proposal、run、manifest、attempt、decision turn、model invocation、typed candidate 和 verification 引用链完整。
- 确定性测试夹具中的 Trace 完整率：20/20，即 100%。
- Typed Candidate schema 校验：20/20。
- W2 持久化执行制品中的明文密钥泄漏：扫描夹具为零。
- `agent-runtime-client` 与 `agent-runtime` 的完整 `go test ./...` 均通过。
- 两个服务的完整 `go vet ./...` 均通过。
- delegation repository/service 以及 runtime specialist/dispatcher 的定向 `go test -race` 均通过；macOS 链接器仅输出非致命 `LC_DYSYMTAB` 警告。

DSO-W1 的 PostgreSQL 并发门已通过。MySQL 容器门已配置在 CI，但由于本机当时无法访问容器镜像仓库而没有执行；它仍是发布门，不能表述为本地已通过。

## 退出结论

DSO-W2 本地退出门通过，可以进入 DSO-W3。当前只冻结受治理执行语义，不冻结草案 wire schema 和数据库布局。
