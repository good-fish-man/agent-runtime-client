# DSO W7：受治理的委派学习

## 目标

W7 会把成功的动态专家运行沉淀为**声明式候选**，而不是可直接执行的代码。候选只有完成下面的独立治理链，才可能影响生产：

```text
历史 Experience
  -> 声明式策略/专家画像候选
  -> 无副作用离线回放
  -> 人工审核
  -> 仅规划 Shadow
  -> 低风险白名单 Canary
  -> Single Agent / Static Specialist / Dynamic DSO 三路基准
  -> 显式晋级
  -> 写入不可变 AgentBuild 审批引用
```

任何缺失或失败的关卡都会回退到 `delegation-policy://rule-baseline/v1`。

## 安全不变量

- 候选定义禁止携带可执行命令、源代码、Provider Hook 或秘密信息。
- 候选记录不可变，并且永久保持 `activation_allowed=false`。
- 离线评估拒绝真实重执行，至少需要三条回放记录。
- Shadow 只能使用 `PLAN_ONLY`，并证明没有世界写入、网络请求、设备动作和凭据读取。
- Canary 只允许低风险任务、白名单用户、稳定分桶，比例不超过 25%。
- 失败的基准报告仍会持久化，并在当前请求内同步触发回滚。
- 关闭学习后立即恢复规则策略基线。
- 只有 `PROMOTED` 且基准、离线评估、Shadow、人工审核谱系完整的制品才能进入默认 `AgentBuild`。
- AgentBuild 精确绑定制品 ID、版本、候选哈希、Rollout ID、Shadow 评估、审核人和审核时间。

## 数据表

| 表 | 用途 |
| --- | --- |
| `os_dso_learning_preference` | 用户级开关和乐观锁版本 |
| `os_dso_learning_candidate` | 不可变声明式候选 |
| `os_dso_learning_evaluation` | 离线与 Shadow 证据 |
| `os_dso_learning_review` | 独立人工审核 |
| `os_dso_learning_rollout` | Canary、晋级、回滚和禁用生命周期 |
| `os_dso_learning_benchmark` | 三路基准证据 |

每次写入都会附带包含请求 Trace 的追加式委派事件。

## API

`/api/agent-runtime-client/v1/delegation-learning` 下的所有接口都强制使用当前认证用户作为 Owner 边界。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/delegation-learning` | 获取完整治理快照和扫描器状态 |
| `PUT` | `/delegation-learning/preference` | 使用乐观锁开启或关闭学习 |
| `POST` | `/delegation-learning/candidates` | 提交声明式候选 |
| `POST` | `/delegation-learning/candidates/:id/offline-evaluation` | 执行历史回放评估 |
| `POST` | `/delegation-learning/candidates/:id/review` | 记录人工批准或拒绝 |
| `POST` | `/delegation-learning/candidates/:id/shadow` | 执行零副作用 Shadow |
| `POST` | `/delegation-learning/candidates/:id/canary` | 启动低风险 Canary |
| `POST` | `/delegation-learning/rollouts/:id/benchmark` | 记录基准并在回退时触发回滚 |
| `POST` | `/delegation-learning/rollouts/:id/promote` | 所有关卡通过后显式晋级 |
| `POST` | `/delegation-learning/rollouts/:id/disable` | 立即禁用并回退 |

## 自动候选发现

演化扫描器只会导入至少有三次成功历史运行、已经形成画像候选的专家经验。它会幂等地创建专家画像候选和对应的委派策略候选，但不会自动完成评估、审核、Shadow、Canary 或晋级。

## 验收证据

自动化测试覆盖：

- 未审核和仅处于 Canary 的制品不能进入 AgentBuild；
- 用户关闭学习后不能创建候选或产生曝光；
- Shadow 出现外部副作用时必须拒绝；
- 低风险稳定 Canary 和高风险强制回退；
- 基准回退在同步请求内完成，小于一分钟目标；
- 晋级可以追溯到 Experience、Replay、Shadow、人工审核和 Benchmark；
- 禁用后立即失去运行时解析和新构建资格。

生产晋级仍然必须使用真实、有代表性的 Canary 样本并由操作员审核基准结果。单元测试不会伪造这类外部生产证据。
