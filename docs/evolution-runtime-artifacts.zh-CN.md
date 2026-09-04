# Evolution Orchestrator 与 Runtime Artifact Resolver

[English](evolution-runtime-artifacts.md) | [简体中文](evolution-runtime-artifacts.zh-CN.md)

## 目标

该子系统在不允许模型生成并立即执行代码的前提下，闭合 Athena 的受治理学习链路：

```text
READY Experience
  -> 有界模式发现
  -> 可选的受约束 Codex 反思与合成
  -> 声明式 Skill 候选
  -> 确定性离线评测
  -> 强制人工评审
  -> 不可变 Skill/Strategy 版本
  -> 已验证 AgentBuild
  -> 单次运行 RunManifest
  -> 精确 Runtime Artifact Bundle
  -> Runtime 规划上下文
```

Evolution Orchestrator 只发现候选，不会自动批准、晋升、部署或执行。Runtime Artifact Resolver 只加载已由验证 Build 固定、并与当前 RunManifest 绑定的版本。

## Evolution Orchestrator

后台 Worker 使用有界 Keyset 分页扫描拥有可复用 `READY` Experience 的用户。用户级学习偏好具有最高优先级：关闭学习后不会生成候选。

一个模式必须同时满足以下条件：

- 至少四条相互独立的 Experience。
- 至少两次成功结果。
- 至少一个失败反例。
- 能力模式已注册、启用，并能通过现有 Learning 校验器。

符合条件的模式会生成私有、声明式 Skill 候选。启用 Codex 合成后，服务只会向 OpenAI Responses API 发送有界、去标识化的动作结构、结果、失败分类、上下文别名和能力策略。Codex 可以调整现有步骤顺序，并提出有界的恢复及验证规则；如果输出引入新的步骤身份、能力、操作、Skill 或未观测到的失败分类，服务端门禁会拒绝该输出。

合成完成后才会执行正常的离线评测，随后候选进入 `REVIEW_REQUIRED`。待评审候选会阻止重复提案；候选作出终态评审后，只有出现新的证据才能生成下一版本。确定性 Candidate ID 让多实例并发扫描保持幂等。Codex 一旦被配置为启用，调用失败会让本次提案关闭失败，不会悄悄退回确定性候选。

自动演进不会生成源码、Shell 命令、凭据、选择器、坐标或新权限。

### 配置

```yaml
evolution:
  enabled: true
  scan_interval_sec: 60
  owner_batch_size: 100
  experience_limit: 1000
  max_candidates_per_scan: 10
  minimum_novel_experiences: 2
  codex:
    enabled: false
    model: "gpt-5.6"
    api_key: "${OPENAI_API_KEY}"
    api_base: "https://api.openai.com/v1"
    reasoning_effort: "medium"
    timeout_sec: 120
    max_output_tokens: 4096
```

环境变量覆盖：

| 变量 | 用途 |
| --- | --- |
| `ARC_EVOLUTION_ENABLED` | 启用候选发现 Worker |
| `ARC_EVOLUTION_SCAN_INTERVAL_SEC` | 后台扫描周期 |
| `ARC_EVOLUTION_OWNER_BATCH_SIZE` | 每个 Keyset 页面加载的用户数 |
| `ARC_EVOLUTION_EXPERIENCE_LIMIT` | 单用户最多分析的 Experience 数 |
| `ARC_EVOLUTION_MAX_CANDIDATES_PER_SCAN` | 每轮扫描的全局候选上限 |
| `ARC_EVOLUTION_MINIMUM_NOVEL_EXPERIENCES` | 生成后续版本所需的新证据数 |
| `ARC_EVOLUTION_CODEX_ENABLED` | 显式启用受约束的 Codex 候选合成 |
| `ARC_EVOLUTION_CODEX_MODEL` | Responses API 使用的 Codex 模型 |
| `ARC_EVOLUTION_CODEX_API_KEY` | OpenAI API Key；否则示例配置会展开 `OPENAI_API_KEY` |
| `ARC_EVOLUTION_CODEX_API_BASE` | Responses API 基础地址 |
| `ARC_EVOLUTION_CODEX_REASONING_EFFORT` | `none`、`low`、`medium`、`high`、`xhigh` 或 `max` |
| `ARC_EVOLUTION_CODEX_TIMEOUT_SEC` | 单次请求超时 |
| `ARC_EVOLUTION_CODEX_MAX_OUTPUT_TOKENS` | 结构化输出 Token 上限 |

本地显式启用：

```bash
export OPENAI_API_KEY="..."
export ARC_EVOLUTION_CODEX_ENABLED=true
go run . --config manifest/config/config.local.yaml
```

请求使用严格 JSON Schema 输出和 `store: false`。如果开启 Codex 却没有可用的 API Key，服务会在启动时明确失败，而不是让已经配置的 AI 阶段静默失效。

### 用户 API

接口位于配置的公共 API 前缀下，并要求登录：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/learning/evolution/status` | 查看 Worker 状态和累计计数 |
| `POST` | `/learning/evolution/scan` | 只扫描当前登录用户的 Experience |

## Runtime Artifact Resolver

每次运行时，Runtime Client 都会创建 `RunManifest`，并解析其 `AgentBuild` 精确固定的 Artifact。只有以下条件全部成立才会放行：

- Manifest 的用户、Agent、Build ID 和 Build 校验和一致。
- Build 自身完整性校验通过。
- 每个 Skill/Strategy 都包含精确 ID、语义版本、不可变 Version ID 和 Candidate ID。
- Candidate 仍具有通过的评测结果和人工评审来源。
- 用户、公共或组织可见性允许访问。
- 数据库存储的定义字节与固定的 SHA-256 校验和一致。
- Strategy 引用的 Skill 同时存在于 Bundle 中。

Bundle 会确定性排序，并限制编码体积、Artifact 数量、计划步骤数和 Strategy 回退数。它通过保留字段 `_athena_runtime_artifacts` 传输；Runtime 会在通用请求上下文渲染给模型之前完成校验并移除该原始字段。

Runtime Client 即使处于无数据库代理模式，也会无条件删除调用方传入的同名保留字段；只有受信任的本地 Resolver 可以写入 Bundle。

Runtime Artifact 只能组织本次请求已经选择的能力。受评审的浏览器子操作可以由现有 `browser.task` 聚合能力满足，但 Artifact 不能注册工具、扩大设备授权、绕过 Policy 或执行代码。

## 失败语义

Artifact 损坏、审批来源过期、用户边界不一致、Manifest 不一致或传输数据非法时，会在模型执行前让本次运行失败。若只是当前请求缺少某项能力，则不会让整个运行失败；对应 Skill 会标记为不可用，并由基础 Planner 安全回退。

这样既能让完整性问题明确暴露，也能保留不越权的能力降级路径。
