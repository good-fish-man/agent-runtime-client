# Athena 学习门禁 v0.4

English: [learning-gate-v0.4.md](learning-gate-v0.4.md)

## 范围

v0.4 将重复、已脱敏的 `Experience` 提炼为声明式 `Skill` 或 `Strategy` 候选。系统不生成或执行 Go、JavaScript、Shell、Python，也不接受任意命令。批准操作只创建带校验和的不可变版本，不会修改已有 Agent、默认模型、策略或生产路由。

## 流程

```text
Experience 证据
  -> 重复语义模式
  -> Schema 与 Capability 校验
  -> 权限与组合风险分析
  -> 确定性离线回放（seed=1）
  -> 基线与 Wilson 95% 置信区间
  -> 人工编辑 / 重评 / 批准或拒绝
  -> 不可变 SkillVersion 或 StrategyVersion
```

证据门槛要求至少四条独立 Experience、两条相同模式成功证据，以及一条与成功证据具有相同语义动作模式的失败反例。每个样本必须包含结构化环境指纹、站点范围、结果和失败条件；入选证据必须来自独立任务，并覆盖至少两个“环境/站点”上下文。编辑后必须重新通过静态门禁；重评复用原始证据及其环境版本并追加 Evaluation，不覆盖历史。编辑、重评和评审都使用 revision 乐观锁，拒绝过期操作。

自动生成的 Skill 必须包含显式前置条件、针对每类已观察失败的有界恢复路径、证据型验证规则和跨上下文来源摘要。可执行参数禁止固定 URL、CSS/XPath Selector、屏幕坐标、Prompt、脚本、凭据和直接代码执行器。网页或用户提供的不可信文本只作为证据，绝不会复制进可执行声明式定义。

## API

所有接口位于 `/api/agent-runtime-client/v1/learning`，并按当前用户隔离。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/candidates/generate` | 从 Experience ID 生成并评测候选 |
| `GET` | `/candidates` | 查询当前用户候选 |
| `GET` | `/candidates/:id` | 查询定义、证据与评测历史 |
| `PUT` | `/candidates/:id` | 编辑声明式定义并重跑静态门禁 |
| `POST` | `/candidates/:id/re-evaluate` | 追加一次确定性离线评测 |
| `POST` | `/candidates/:id/review` | 携带 expected revision 批准或拒绝 |
| `GET` | `/skills`、`/strategies` | 查询已批准、仅手动可选的版本 |
| `POST` | `/demonstrations` | 显式开启私有演示记录 |

演示只记录语义 Capability 和 Operation。遇到密码、Token、Cookie、OTP、2FA 等字段会暂停，并且仅保存脱敏占位符。

## 存储与回滚

v0.4 新增 `os_learning_candidate`、`os_candidate_evidence`、`os_candidate_evaluation`、`os_skill`、`os_skill_version`、`os_strategy`、`os_strategy_version` 和 `os_demonstration`。版本记录只追加，并带 SHA-256 校验和。限定范围的回滚脚本见 [v0.4-learning-rollback.sql](migrations/v0.4-learning-rollback.sql)。

回滚前应停止 Client；如需保留已批准定义，应先导出。本脚本不会修改 Agent 配置、Experience、任务、用户、凭据或模型数据。

## 威胁模型

| 威胁 | 控制措施 |
| --- | --- |
| Prompt Injection 变成可执行代码 | DSL 拒绝执行器 Capability，以及脚本、命令、凭据形态字段 |
| 站点样本变成通用执行路径中的硬编码 | 同一语义模式必须覆盖结构化独立上下文，并拒绝固定 URL、Selector 和坐标 |
| 不相关失败被当作反例 | 失败证据必须与成功证据具有相同语义动作模式 |
| 候选引用未注册或已停用能力 | 校验必须读取持久化 Capability Registry 策略 |
| 候选降低组合风险 | 声明风险上限不得低于所引用能力的最高风险 |
| 候选扩大凭据或认证范围 | 声明式参数禁止 Credential 字段和新 Runtime Executor |
| 一次偶然成功直接成为 Skill | 必须有独立证据、失败反例、回放、最小样本和置信区间 |
| 过期评审覆盖新决定 | revision 校验与评审/物化同事务 |
| 敏感演示数据进入 PostgreSQL | 自动暂停，只持久化脱敏语义占位符 |
| 批准后静默改变生产 | 返回 `manual_only`，不接入现有 Agent 执行路径 |

## 验证与 Benchmark

确定性测试覆盖 Schema、DAG 环路、有界恢复、未注册能力、直接执行器、风险升级、站点绑定参数、Prompt Injection 隔离、跨上下文泛化、同模式失败反例、敏感演示、过期 revision、安全编辑、重评历史和事务物化。

本地验收命令：

```bash
GOCACHE=/tmp/athena-client-gocache go test ./...
GOCACHE=/tmp/athena-client-gocache go vet ./...
```

离线 Benchmark 使用固定种子 `1`、四个 Fixture、历史基线、成功率、安全分、Delta 和 Wilson 95% 置信区间。样本不少于四条、得分达到声明阈值（默认 `0.75`）且安全分为 `1.0` 才通过。该 Benchmark 验证的是门禁本身，不代表真实场景已充分泛化；手动部署前仍需更大的站点与环境评测集。

## 分发状态

本版本只增加运行时数据与 UI 契约，不生成新的下载包、SBOM、签名或 Release Manifest。分发签名和供应链门禁将在后续路线图版本实现。
