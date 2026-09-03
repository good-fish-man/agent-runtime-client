# 02. HTTP API、身份与安全

[指南首页](README.md) | [English](../en/02-http-api-identity-and-security.md)

## 目的

`api/http` 是 Athena 的传输边界。它把 HTTP、SSE 和 WebSocket 请求转换为应用调用，并把 Domain/Application Error 转换为稳定公共响应。

Handler 不应包含 Repository 查询或编排策略。

## HTTP Engine

[`api/http/http.go`](../../../api/http/http.go) 构造 Gin Engine 并安装共享 Middleware。路由分为三类：

| 接口面 | 位置 | 用途 |
| --- | --- | --- |
| Runtime API | [`api/http/router`](../../../api/http/router) | Health、Capability、Run、Stream、Media、Resume、Stop |
| 管理 API | [`api/http/router/public`](../../../api/http/router/public) | 用户、Agent、模型、治理、运维和集成 |
| 设备控制 | [`api/http/handler/control`](../../../api/http/handler/control) | 桌面设备长连接 WebSocket 与控制接口 |

管理路由挂载在 `server.public_prefix` 下，Runtime 调用路由保留在 `/v1`。

## Middleware 顺序

[`api/http/middleware`](../../../api/http/middleware) 包含横切的传输行为：

- **trace**：接收可信 Trace ID 或生成新 ID，写入 Gin 与 `request.Context`，并返回到响应头；
- **recovery**：捕获 Panic 并转换为稳定内部错误；
- **请求/响应日志**：记录状态、耗时、字节数和受限 Body，并对 Stream 特殊处理；
- **authentication**：解析 Bearer Token 与用户/管理员身份；
- **internal authentication**：保护 Launcher/服务回调；
- **CORS**：限制浏览器 Origin；
- **error rendering**：将应用错误映射为公共 Envelope。

Trace Context 必须在数据库、gRPC 和应用工作之前建立，确保下游日志使用同一请求身份。

## 身份与授权

[`pkg/authctx`](../../../pkg/authctx) 是应用层读取身份的边界。Middleware 将身份写入 `context.Context`；Service 从 Context 读取，或接收已经解析的 Owner ID。

核心规则：

- 普通用户只能读取和修改自己的数据；
- 管理员需要查看跨用户数据时必须使用显式 Admin Endpoint；
- 模型 API Key 和网站密码绝不能出现在浏览器响应；
- Internal Endpoint 使用内部服务凭据，不能使用普通用户 Token；
- 修改 URL 中的 ID 不能绕过 Owner 校验；
- 公共 Agent 可以选择，但用户模型凭据仍然私有。

## Handler 职责

普通 Handler 应：

1. 绑定 Path、Query 和 Body；
2. 校验传输层必填项；
3. 解析已认证 Owner/Admin Context；
4. 调用一个 Application Service 方法；
5. 通过 [`types/response`](../../../types/response) 返回。

稳定状态码、错误码与消息使用 [`types/apierror`](../../../types/apierror)。内部错误应在有意义的 Operation 边界包装，但完整错误链只在最终边界集中记录一次。

## Runtime SSE

[`api/http/handler/runtime`](../../../api/http/handler/runtime) 中的 Stream Endpoint 把 Runtime 类型化 Event 转换为 SSE。

关键不变量：

- 不缓存完整响应；
- 及时 Flush 每个可见 Event；
- 保留 Event Type、Trace ID、Usage、Action 与 Observation Metadata；
- HTTP Context 取消时向上游 gRPC/设备工作传播取消；
- 不能先输出错误，再输出矛盾的成功终态；
- 协议内部 Chunk 不应显示成空 Assistant 消息。

## 设备 WebSocket

Control Handler 升级认证后的 HTTP 连接，并将协议所有权交给 Control Hub。WebSocket 不是第二套业务实现，消息必须使用规范 Action/Observation 协议类型。

连接承载设备注册、Capability Snapshot、Heartbeat、Action、Observation、Progress、Approval、Cancellation 和重连状态。

## 公共 API 分类

管理路由包括：

- 认证、Profile 与 Avatar；
- Agent、Model、Model Key、Skill、Knowledge Base、Memory 与 Workspace；
- 网站凭据与 Scheduled Task；
- Experience、Evaluation、Learning、Deployment、Evidence Knowledge 与 Goal；
- Delegation 运维和受治理 Delegation Learning；
- Plugin Registry 与生产运维；
- Channel、Callback、Weixin、Job、Command、Dashboard、Voice Avatar 与服务配置。

权威 Endpoint 清单以 [`api/http/router/public/router.go`](../../../api/http/router/public/router.go) 为准。

## 安全边界

| 边界 | 必要保护 |
| --- | --- |
| Browser 到 Client | Bearer 身份、CORS、Owner 校验、受限输入 |
| Launcher/Device 到 Client | Device/Internal Token、Lease、Capability Binding、防重放 |
| Client 到 Runtime | 转发 Trace，使用服务端补全凭据 |
| Client 到数据库 | Context Query 与 Owner 条件 |
| Client 到本地文件/进程 | Safe Path、Allowlist、大小/时间限制、禁止 Shell 拼接 |
| Plugin 安装 | Signature、Package Hash、Scanner Evidence、人工审核与权限授权 |

## 优先阅读文件

| 文件 | 原因 |
| --- | --- |
| [`api/http/http.go`](../../../api/http/http.go) | Engine 与 Middleware 装配 |
| [`api/http/router/router.go`](../../../api/http/router/router.go) | Runtime 路由 |
| [`api/http/router/public/router.go`](../../../api/http/router/public/router.go) | 完整管理路由目录 |
| [`api/http/middleware/trace.go`](../../../api/http/middleware/trace.go) | 请求身份传播 |
| [`api/http/middleware/auth.go`](../../../api/http/middleware/auth.go) | 用户/管理员认证 |
| [`api/http/middleware/req_body.go`](../../../api/http/middleware/req_body.go) | 有界、Stream-Aware 请求日志 |
| [`types/apierror`](../../../types/apierror) | 稳定 API Error |
| [`types/response`](../../../types/response) | 公共响应 Envelope |

## 修改检查清单

- 授权是否不仅依赖 UI，而是在 Service/Repository 中执行？
- 所有下游调用是否使用 `c.Request.Context()`？
- DTO、日志和错误消息中是否没有敏感字段？
- Stream 取消能否到达 gRPC 与设备执行？
- Endpoint 是否只会返回一个终态？
- 输入大小和本地文件/进程操作是否有界？
