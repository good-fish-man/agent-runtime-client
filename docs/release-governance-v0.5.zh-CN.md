# Athena v0.5 受控发布治理

Athena v0.5 将不可变行为与单次运行状态分离，并建立从已评审学习产物到生产生效的受控链路。

## 核心契约

- `AgentBuild` 固定 Kernel、Planner、Policy、Protocol、Prompt、Ontology、评测套件以及精确的 Skill/Strategy 已评审版本。每个产物都携带不可变 Version ID、Candidate ID、Evaluation Run、评审人、评审时间和校验和，整条溯源链都会进入 Build SHA-256。
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

只有经过人工评审的提案才能进入 Shadow。调用方只能提交 Task ID 和结构化输入；服务端解析不可变 Control/Candidate Build，把同一个规范化输入摘要交给只读双跑 evaluator，并自行计算 Route、Graph、计划动作、成本、风险和副作用证明。调用方不能提交哈希或 `passed`。离开 Shadow 前，结果必须匹配精确 Build 对，所有必需检查通过，真实动作数为零，且网络、设备、凭据和世界写入计数全部为零。自动 Canary 仅允许可验证、可恢复的 R0/R1 构建；R2/R3 只能通过显式确认从 Shadow 激活。

Canary 的公开 API 不接受聚合指标或原始样本。只有受信 Runtime 完成路径会为候选运行生成样本，并绑定唯一 `RunManifest`、候选 Exposure 和精确候选 Build。仓储会锁定 Promotion，在同一事务中写入样本、重算成功率、P95 延迟、平均成本、安全、人工干预率和样本集摘要，并写入聚合指标。任一停止阈值触发时，样本、指标和暂停 Promotion 原子完成。

## 稳定分组与用户退出

Canary 使用 `owner_id + agent_id + promotion_id` 的稳定哈希分组，不会在不同任务间漂移。用户可随时退出并固定到 CONTROL；重新加入时恢复同一个确定性分组。

## 回滚与补偿

激活时记录上一构建，并原子切换活动指针。回滚会将故障 Promotion 标记为 `ROLLED_BACK`、重新激活上一构建并写入不可变审计记录。Athena 不会假装已发生的外部副作用被撤销；这类动作会生成待处理补偿指令，交由人工或后续受控执行器处理。

## 威胁模型

- 模型不能通过提示词创建或激活构建；部署接口受认证用户范围约束。
- 创建构建时拒绝未评审或可变的 Skill/Strategy 版本。
- Shadow 调用方不能声明结果、哈希、成本、风险或真实动作；若 evaluator 报告任何外部效果，服务端拒绝且不落库。
- 公开调用方不能创建聚合指标或原始样本；只有受信 Runtime 路径可写入，且数据库强制每个 Manifest 只能产生一个样本。
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
