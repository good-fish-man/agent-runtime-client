# 08. 测试与扩展指南

[指南首页](README.md) | [English](../en/08-testing-and-extension-guide.md)

## 目的

本章说明如何验证修改，以及如何增加行为而不创建第二套执行路径、不削弱控制面的安全保证。

## 测试层级

### Unit Test

确定性 Policy、Mapping、Validation、Redaction、State Transition 与 Aggregation Test 应靠近 Package，并且无需外部服务即可运行。

### Repository Test

Repository/Store Test 验证 Owner Scoping、Transaction、Idempotency、Optimistic Revision、Lease 与 Migration。使用仓库 Test Database Helper，不要假设开发者本地数据库存在。

### Integration Test

Integration Test 跨越 Application/Domain/Infra Seam，例如 Experience 到 Learning、Approved Artifact Resolution 或 Runtime Mapping Round Trip。没有真实设备时，不能声称证明了真实 Desktop Effect。

### E2E 与 Golden Journey

真实 Browser、Desktop、Installer、Backup/Restore 和 Signed Package Journey 需要外部 E2E。Result 通过 Operations 持久化，并与 Unit/Preflight Success 区分。

## 标准命令

```bash
go test ./...
go vet ./...
gofmt -w <changed-go-files>
git diff --check
```

迭代时运行聚焦 Test，声明完成前运行完整 Suite。

## 使用 Trace 排查

每个用户请求应有一个 Trace ID 贯穿：

```text
HTTP Middleware
  -> Context-Aware Application Log
  -> GORM Logger
  -> gRPC Metadata
  -> Runtime Log
  -> Device Task/Action/Observation Record
```

在有意义的 Operation Boundary 包装 Error，使 Error Chain 说明失败位置。完整 Terminal Chain 只记录一次，不要在每次 Return 重复打印同一错误。

长操作应记录 Structured Start/End Event，包括 Duration、Operation、Model/Tool/Capability Identity、Trace ID 与 Terminal Status。默认绝不能记录 Credential 或未脱敏 Prompt。

## 新增 Endpoint

1. 决定属于 Runtime、Management、Control 还是 Internal API。
2. 定义 DTO/Use Case Contract，不直接绑定 PO。
3. 在 Service/Store 执行 Authorization 与 Owner Scoping。
4. 保持 Handler 简洁并使用共享 Response/Error Type。
5. 在权威 Router 注册 Route。
6. 添加 Handler Validation/Auth Test 与 Service Behavior Test。
7. 更新中英文路由/子系统文档。

## 新增 Capability 或 Device Action

1. 在共享 Athena Protocol 定义 Capability/Action，不使用 Frontend-Only JSON。
2. 指定 Risk、Approval、Timeout、Idempotency、Expected Effect 与 Observation Schema。
3. 增加 Device Capability Report 和 Executor 支持。
4. 通过 Hub 路由，并验证 Lease/Fencing。
5. 将真实 Observation 送回 Runtime Continuation。
6. 验证 Effect 后才能报告 Success。
7. 测试 Cancel、Timeout、Reconnect、Duplicate Delivery 与 Stale Result。

如果 Universal Semantic Browser Capability 能表达 Action，不要在 Client 添加网站专用业务逻辑。

## 新增持久状态机

将不可变 Definition/Decision 与可变 Execution Attempt 分开。典型对象包括 Proposal、Policy Decision、Run、Attempt、Event、Observation、Verification 与 Terminal Summary。

必须具备：

- Stable ID 与 Idempotency Key；
- Optimistic Revision 或 Transaction Lock；
- Active Ownership 的 Lease/Fencing；
- Append-Only Audit Event；
- 显式 Terminal State；
- Restart Recovery；
- 拒绝 Late/Stale Result。

## 新增 Evolution 能力

Online Execution 可以产生不可变 Experience Evidence。独立 Offline/Governed Path 聚合 Pattern 并提出 Declarative Candidate。Candidate 必须通过 Evaluation 和 Review，之后不可变 Build 才能向 Runtime 暴露它。

生成代码不能作为 Learned Skill 直接执行。

## 架构护栏

- 禁止 Handler 直接通过 GORM 实现业务逻辑。
- 禁止 API Key、Password、完整私有 Prompt 进入 Browser Response/Log。
- 没有相关 Observation/Effect Evidence 时禁止 Action Success。
- 禁止 Dynamic Specialist 绕过主执行链。
- 禁止一次在线成功直接 Promotion Learning Candidate。
- 禁止后台能力绑定 Frontend 生命周期。
- 禁止未经过 Signature/Hash/Permission Governance 的 Package 激活。
- 禁止兼容性 Workaround 静默破坏当前 Protocol Semantics。

## 测试位置

| Concern | 常见位置 |
| --- | --- |
| HTTP Binding/Auth | `api/http/**/*_test.go` |
| Use Case Behavior | `application/service/**/*_test.go` |
| DTO/Entity Mapping | `application/assembler`、`infra/runtime` Test |
| Domain Rule | `domain/**/*_test.go` |
| Store/Migration | `infra/repository/**/*_test.go` |
| Runtime Protocol Mapping | `infra/runtime/*_test.go` |
| Release/E2E Evidence | 外部 Runner + Operations Evidence API |

## 文档完成标准

代码职责或行为变化时：

- 同时更新中英文对应章节；
- Path 变化时更新[包参考](package-reference.md)；
- 保持 Source Link 有效；
- 说明不变量和数据流，而不只写新 Type Name；
- 详细协议应链接来源文档，避免复制 Schema 后漂移。
