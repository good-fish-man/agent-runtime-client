# Athena v0.6 证据知识

Athena v0.6 不再把无法追溯的记忆直接当作事实，而是建立带来源、作用域和时效的知识系统。系统先保存 Evidence，只有在校验确认存在可访问证据后，陈述才能成为 Claim。

## 知识模型

- `Evidence` 保存来源类型、地址、摘录、服务端判定的信任档案、权威度、新鲜度、作用域、敏感级别、观测时间和不可变来源信息。
- `Claim` 保存主语、谓词、值、置信度、证据引用、用户或组织作用域、有效期、语义向量、关系和冲突状态。
- `Contradiction` 保留互相竞争的声明，不会静默选择其中一个覆盖另一个。
- `KnowledgeSnapshot` 固定一次检索实际使用的 Claim、Evidence 和 Ontology 版本。
- `Belief` 只是检索结果派生出的读取视图，不会被当成绝对事实持久化，也不能直接修改策略。

调用方不能自行声明“官方来源”、权威度、新鲜度、可访问状态、来源链或有效 Claim 状态。研究任务发现的网页只能先保存为 `PAGE_OBSERVATION` Evidence；用户明确确认必须走单独接口，并始终保留在用户作用域。单一网页观测不能直接创建 Claim。公共 Claim 不能依赖用户确认或更窄的私有证据；时效性 Claim 会获得受限的有效期。

## 混合检索

检索会综合结构化主语/谓词匹配、关键词重合、已持久化的语义向量、受限关系遍历、时间有效性、来源权威度和新鲜度。每次检索都有明确的结果数、Token、耗时、作用域、组织、最低权威度、来源类型、敏感级别和关系深度预算。

结果会返回支持它的 Evidence、命中信号、明确的 `FACT`、`CONFLICTED`、`EXPIRED`、`STALE_EVIDENCE` 或 `RETRACTED` 判定、预算消耗以及不可变快照 ID。用户和组织隔离在仓储层执行，而不是只依赖 HTTP Handler。Runtime 会把受限的证据上下文注入模型提示词，并把本次快照 checksum 绑定到最终 RunManifest。

未解决冲突不会被静默覆盖。评审人必须填写说明，并可通过 `POST /knowledge/contradictions/:id/resolve` 保留一条 Claim、将全部 Claim 标记为不确定，或全部撤回。

## 受控 Ontology

Ontology Pack 按版本管理，并使用强类型实体与关系定义。新定义必须先成为带服务端离线评估检查和证据引用的 Candidate，经过人工审批后才会生成版本；应用版本还必须经过独立迁移审批，由服务端迁移器执行强类型步骤并生成工具执行回执。调用方提交的布尔值不能冒充评估、审批或执行结果。

首次创建 Pack 与版本、审批 Candidate 与生成版本、创建迁移记录与切换当前版本指针都在事务中完成，并进行 revision 校验。任何中间失败都会整体回滚，不会留下半完成状态。

## 公共 API 边界

- `POST /knowledge/evidence/confirmations` 保存用户明确确认，并由服务端分配信任元数据。
- `POST /knowledge/claims` 只能基于同一用户拥有且可访问的已持久化 Evidence 创建用户 Claim。
- `POST /knowledge/retrieve` 执行有预算的混合检索，并创建带 checksum 的快照。
- Ontology Candidate 评审、迁移评审和迁移执行是三个独立操作。

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
