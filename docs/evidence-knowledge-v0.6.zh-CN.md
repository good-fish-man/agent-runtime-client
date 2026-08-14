# Athena v0.6 证据知识

Athena v0.6 不再把无法追溯的记忆直接当作事实，而是建立带来源、作用域和时效的知识系统。系统先保存 Evidence，只有在校验确认存在可访问证据后，陈述才能成为 Claim。

## 知识模型

- `Evidence` 保存来源类型、地址、摘录、权威度、新鲜度、作用域、敏感级别、观测时间和不可变来源信息。
- `Claim` 保存主语、谓词、值、置信度、证据引用、所有者作用域、有效期和冲突状态。
- `Contradiction` 保留互相竞争的声明，不会静默选择其中一个覆盖另一个。
- `KnowledgeSnapshot` 固定一次检索实际使用的 Claim、Evidence 和 Ontology 版本。
- `Belief` 只是检索结果派生出的读取视图，不会被当成绝对事实持久化，也不能直接修改策略。

研究任务发现的网页只能先保存为 `PAGE_OBSERVATION` Evidence。单一网页观测不能直接创建 Claim。公共 Claim 不能依赖用户确认或更窄的私有证据；时效性 Claim 必须声明过期时间。

## 混合检索

检索会综合结构化主语/谓词匹配、关键词重合、确定性的本地向量信号、关系提示、时间有效性、来源权威度和新鲜度。每次检索都有明确的结果数、Token、耗时、作用域和敏感级别预算。

结果会返回支持它的 Evidence、命中信号、冲突和过期标识、预算消耗以及不可变快照 ID。所有者隔离在仓储层执行，而不是只依赖 HTTP Handler。

## 受控 Ontology

Ontology Pack 按版本管理。新定义必须先成为带评估结果和证据引用的离线 Candidate，经过人工审批后才会生成版本；应用版本还必须通过显式迁移工具执行。

首次创建 Pack 与版本、审批 Candidate 与生成版本、创建迁移记录与切换当前版本指针都在事务中完成。任何中间失败都会整体回滚，不会留下半完成状态。

## 数据表

服务启动时会初始化八张表：

- `os_knowledge_claim`
- `os_evidence`
- `os_contradiction`
- `os_knowledge_snapshot`
- `os_ontology_pack`
- `os_ontology_version`
- `os_ontology_candidate`
- `os_ontology_migration`

## 验证

```bash
go test ./application/service/knowledge ./application/service/runtime ./infra/repository/repo/knowledge ./api/http/router/public ./infra/repository/migration
go test ./...
go vet ./...
```

数据库回滚脚本位于 `docs/migrations/v0.6-knowledge-rollback.sql`。执行前请先导出证据和审计记录。
