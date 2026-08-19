# DSO-W1 持久化委派权威

状态：实现已完成；本地 PostgreSQL 门槛已通过；MySQL 门槛已固化到 CI，等待能够访问 MySQL 镜像的环境执行。

## 范围

DSO-W1 将 Specialist 委派所有权从请求内存迁移到 Control Plane 数据库。Control Plane 现在是 Proposal 接收记录、逻辑 Run、可恢复 Attempt、预算、取消、Lease 与审计事件的唯一权威。

## 已交付

- Proposal、Decision、DelegatedOutcome、SubagentSpec、Manifest、Run、Attempt、DecisionTurn、ModelInvocation、预算账户/预留、资源 Lease、候选结果和 DSO Event 的 PO 与 Repository。
- 内容绑定幂等的 Accepted Delegation 原子事务。
- Optimistic Revision 与行锁 Attempt 所有权；同一逻辑 Run 最多一个有效 Attempt owner。
- Attempt Lease/Heartbeat、启动恢复、迟到结果隔离、孤儿 Run 重排队和 Deadline 过期。
- 分层预算账本：Reserve、按真实使用量 Commit、Release 未使用额度，以及取消清理。
- 携带 Trace、Causation、Aggregate Sequence 和 Idempotency 的事务 Outbox。
- 已接入应用启动/关闭生命周期的 Delegation Orchestrator。
- 旧 `orchestration/v2` Adapter 只能生成 `SUBMITTED` DSO Proposal，不能接收或执行旧 Specialist。
- `.github/workflows/dso-w1-databases.yml` 中的 PostgreSQL/MySQL 集成门槛。

## 自动验证的不变量

1. 并发抢占只产生一个有效 Attempt owner。
2. 旧 owner 失效后，迟到结果不能覆盖新执行边界。
3. 并发预留下始终满足 `Consumed + Reserved <= Total`。
4. Commit 只计入真实消耗，未使用额度归还父预算。
5. Cancel 会撤销活跃资源 Lease 并释放剩余预算。
6. Outbox 发布失败时事件保持未发布，可安全重试。
7. 恢复覆盖 CREATED、RUNNING、WAITING_OBSERVATION、CANCEL_REQUESTED、孤儿 Run 和 Deadline 过期边界。
8. 每次恢复转换都产生可追踪、可关联因果的审计事件。

## 验收证据

本地已通过：

```bash
go test ./...
go vet ./...
go test -race ./application/service/delegation ./infra/repository/repo/delegation
ATHENA_DSO_TEST_POSTGRES_DSN='...' go test -count=1 -run TestDatabaseConcurrencyGate/postgres ./infra/repository/repo/delegation
```

MySQL 使用 `ATHENA_DSO_TEST_MYSQL_DSN` 和 `/mysql` 选择器运行同一套测试，并已进入数据库 CI。当前实现会话中，本机 Docker 无法访问 Docker Hub 和公共 ECR 镜像源，因此这里明确记录为环境限制，不会伪装成通过结果。

## W2 边界

W1 不允许旧的请求级 Subagent 实现执行。W2 必须让每个被接收的 Attempt 绑定经过验证的 InvocationManifest，并通过该持久化权威运行 DecisionTurn 循环。
