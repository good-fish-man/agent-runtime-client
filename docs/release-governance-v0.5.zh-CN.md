# Athena v0.5 受控发布治理

Athena v0.5 将不可变行为与单次运行状态分离，并建立从已评审学习产物到生产生效的受控链路。

## 核心契约

- `AgentBuild` 固定 Kernel、Planner、Policy、Protocol、Prompt、Ontology、评测套件以及精确的 Skill/Strategy 已评审版本，并保存不可变 SHA-256 校验和。
- `RunManifest` 为每次任务记录实际选择的构建校验和、模型配置指纹、能力实例、设备、用户范围、世界版本、知识快照、预算、功能开关及 Canary 分组。
- 两者都不保存运行时密钥。模型 API Key 和知识库 Token 只参与不可逆配置指纹计算。

## 发布状态机

```text
PROPOSED -> REVIEWED -> SHADOW -> CANARY -> ACTIVE
                       |          |          |
                       +-------> PAUSED <----+
                                      |
                                  ROLLED_BACK -> RETIRED
```

只有经过人工评审的提案才能进入 Shadow。离开 Shadow 前，至少需要一条通过的评测结果，并且真实动作数必须为零、无外部副作用。自动 Canary 仅允许可验证、可恢复的 R0/R1 构建；R2/R3 只能通过显式确认从 Shadow 激活。

Canary 激活前，最新指标必须达到最低样本量，并同时满足成功率、延迟、成本、安全和人工干预阈值。任一停止阈值被触发时，指标记录与暂停 Promotion 在同一事务中完成。

## 稳定分组与用户退出

Canary 使用 `owner_id + agent_id + promotion_id` 的稳定哈希分组，不会在不同任务间漂移。用户可随时退出并固定到 CONTROL；重新加入时恢复同一个确定性分组。

## 回滚与补偿

激活时记录上一构建，并原子切换活动指针。回滚会将故障 Promotion 标记为 `ROLLED_BACK`、重新激活上一构建并写入不可变审计记录。Athena 不会假装已发生的外部副作用被撤销；这类动作会生成待处理补偿指令，交由人工或后续受控执行器处理。

## 威胁模型

- 模型不能通过提示词创建或激活构建；部署接口受认证用户范围约束。
- 创建构建时拒绝未评审或可变的 Skill/Strategy 版本。
- Shadow 记录始终声明零真实动作和无外部副作用。
- 乐观 Revision 防止并发发布、激活、暂停和回滚互相覆盖。
- 构建、Promotion、Exposure、Manifest、Metric 和 Rollback 查询必须包含 Owner 条件。
- 测试明确覆盖 R2/R3 自动 Canary 禁止及密钥不落库。

## 验证

```bash
go test ./application/service/deployment ./application/service/runtime ./infra/repository/repo/deployment ./api/http/router/public ./infra/repository/migration
go test ./...
go vet ./...
```

数据库对象回退脚本位于 `docs/migrations/v0.5-deployment-rollback.sql`。执行前必须导出审计数据；脚本不会掩盖数据删除。
