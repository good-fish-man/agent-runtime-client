# 外部世界模型接入

Athena 将外部图谱和世界模型作为只读 Provider。外部结果先被投影为
`WorldSnapshot`，再通过当前生效的 Ontology Validator；只有本地
`worldmodel.Service.CommitObservation` 可以修改权威世界状态。

## 支持范围

| Provider | 接口 | 适用系统 |
| --- | --- | --- |
| `ATHENA_HTTP` | `POST` WorldQuery，返回 WorldSnapshot | 自定义世界模型、GraphRAG 或其他系统的桥接服务 |
| `SPARQL` | SPARQL 1.1 Protocol，只允许 `SELECT` | RDF/OWL 图谱、Amazon Neptune、标准 Triplestore |
| `NEO4J` | Neo4j Query API v2，只允许只读 Cypher | Neo4j，以及以 Neo4j 为存储的 Graphiti/Mem0 部署 |
| `TYPEDB` | TypeDB HTTP `/v1/query`，只允许 read transaction | TypeDB 3.x typed knowledge graph |

Graphiti 还可以通过它的 Neo4j 后端接入。Microsoft GraphRAG 没有稳定的通用
远程图查询协议，推荐在其 Python Query API 外提供一个 `ATHENA_HTTP` 桥接层。
Mem0 当前版本如果不提供可遍历的图查询接口，也应使用 `ATHENA_HTTP` 将检索结果
转换为带证据和 TTL 的 Athena 实体或事实。

## 管理流程

管理员在 **World Model > 外部提供方** 中完成以下步骤：

1. 选择协议、配置端点和只读投影查询。
2. 将 Provider 绑定到当前已批准的 `ontology_pack@version`。
3. 只保存专用凭据环境变量名，例如 `ATHENA_WORLD_PROVIDER_GRAPH_TOKEN`；真实凭据由服务进程环境提供。
4. 执行连接测试，再执行只读预览。
5. 预览结果通过本体校验后才返回；它仍标记为 `authoritative: false`，不会自动写入本地世界状态。

Provider 配置、健康状态和乐观锁 revision 存储在 `os_world_provider`。端点默认禁止
私网和特殊地址；私有部署需要管理员显式启用 `allow_private_network`。响应上限为 4 MiB，
请求具有超时、重定向和只读语句限制。

## 投影约定

SPARQL 变量名、Neo4j 返回字段或 TypeDB fetch key 可以使用以下稳定别名：

| 对象 | 必需字段 | 常用可选字段 |
| --- | --- | --- |
| Entity | `athena_kind=entity`, `id`, `type` | `canonical_name` |
| Relation | `athena_kind=relation`, `id`, `source_id`, `target_id`, `predicate` | 其他属性 |
| Fact | `athena_kind=fact`, `id`, `subject_id`, `subject_type`, `predicate`, `value` | `value_type` |

Neo4j 推荐返回单个 `athena` map：

```cypher
MATCH (n)
RETURN {
  athena_kind: 'entity',
  id: n.athena_id,
  type: head(labels(n)),
  canonical_name: coalesce(n.name, n.athena_id)
} AS athena
LIMIT $limit
```

查询模板中的 `{{limit}}` 会由服务端替换。Neo4j 还可使用 `$text` 和 `$limit`
参数。Provider 设置的 confidence 是上限而非可信度提升器；TTL 到期后结果默认不参与查询。

## 本体规划

管理员在 **World Model > 本体规划** 创建 Ontology Pack，编辑实体、关系和验证规则，
选择已有 Evidence 后提交候选版本。候选必须通过服务端离线评测，并在 **本体审核** 中由
人工批准。创建和批准候选都不会直接修改生产本体；应用新版本仍需显式迁移流程。

Codex 只能生成 Ontology Candidate。它不能绕过离线评测、人工审核或迁移步骤，也不能
直接修改生产本体。

## 参考协议

- [W3C SPARQL 1.1 Protocol](https://www.w3.org/TR/sparql11-protocol/)
- [W3C OWL 2 Overview](https://www.w3.org/TR/owl2-overview/)
- [W3C SHACL](https://www.w3.org/TR/shacl/)
- [Neo4j Query API](https://neo4j.com/docs/query-api/current/query/)
- [TypeDB HTTP API](https://typedb.com/docs/reference/typedb-http-api/)
- [Graphiti](https://help.getzep.com/graphiti/getting-started/overview)
- [Microsoft GraphRAG](https://microsoft.github.io/graphrag/)
