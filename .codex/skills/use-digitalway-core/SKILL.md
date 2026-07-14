---
name: use-digitalway-core
description: 当任务涉及 github.com/digitalwayhk/core 的服务、IRouter、ModelList、Manage CRUD、认证、WebSocket、Cluster、Transport、MQ/EventBridge、配置、测试或框架消费方兼容性时使用。
---

# 使用 Digitalway Core

## 开始前

Digitalway Core 是 go-zero 与成熟依赖之上的应用组装框架。`examples/01-simple-shop` 是最简平台应用的标准样例，覆盖模型、持久化、DTO、Manage/Public/Private API、认证、WebSocket、服务组合与真实集成测试。`examples/02-shop-payment` 是进阶业务样例，覆盖 API -> business -> models 分层、跨模型事务、支付状态机、Manage hook、自定义命令和支付结果 WebSocket。创建或审查普通业务服务时先核对第一个样例；涉及业务编排或受控后台命令时再核对第二个样例。不要从已删除的 Copilot skill、旧文档或记忆恢复行为。

按任务读取：

- API、模型、Manage、WebSocket、启动、集成测试与版本引用：`references/core-backend-api.md`
- 最简平台应用源码：`examples/01-simple-shop`
- 业务层与支付状态机样例：`examples/02-shop-payment`
- 标准集成测试公共能力：`examples/integration/helpers.go`
- 最简平台应用集成测试模板：`examples/integration/01-simple-shop`
- 业务状态机集成测试模板：`examples/integration/02-shop-payment`
- 场景与成熟度：`docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- 配置能力：`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- 日志与错误：`docs/codex/LOGGING_AUDIT_AND_STANDARD.md`

## 标准样例索引

| 能力 | 标准实现 |
| --- | --- |
| 无依赖服务契约 | `examples/01-simple-shop/contract` |
| 模型初始化、哈希、校验 | `examples/01-simple-shop/models/product.go`、`order.go` |
| 模型持久化边界 | `models/data_action.go`、`product_persistence.go`、`order_persistence.go` |
| 对外 DTO | `api/dto` |
| 完整/只读 Manage | `api/manage/productmanage.go`、`ordermanage.go` |
| 可选条件 Public 查询 | `api/public/getproducts.go` |
| 身份与所有权 Private API | `api/private` |
| 最终用户 WebSocket 订阅 | `api/private/getorders.go` |
| 服务组合根 | `service.go`、`main/main.go` |
| 真实进程集成测试 | `examples/integration/01-simple-shop` |
| API、业务层、模型三层编排 | `examples/02-shop-payment/business` |
| 跨模型事务与支付状态机 | `business/payment.go`、`business/order.go`、`models/data_action.go` |
| Manage hook 与自定义命令 | `api/manage/productmanage.go`、`paymenttypemanage.go`、`paymentrecord_commands.go` |
| 支付状态 WebSocket | `api/private/getorders.go`、`api/private/common.go` |
| 进阶真实进程集成测试 | `examples/integration/02-shop-payment` |

## 核心决策

1. 普通路由实现 `types.IRouter`；public/private 路径为 `/api/{service}/{structLower}`，目录决定认证但不进入 URL。
2. 服务名和跨服务共享基础类型放入无依赖 `contract` 包；`IService.ServiceName()` 返回其中的稳定常量。不要为路由重复定义 Path 常量，路由契约来自已注册的 `RouterInfo()`。注册期差异只通过 `router.WithMethod`、`WithPath`、`WithAuth`、`WithPathType`、`WithPoolSize` 等 Option 声明，运行期只通过 Getter 读取；不得直接修改 `RouterInfo` 导出兼容字段。
3. private 身份只读 `req.GetUser()`/claims，不信任请求字段。
4. Manage 使用 `NewManageService[T](owner)`，路径为 `/api/manage/{service}/{manage}/{operation}`。
5. Manage CRUD 通过 `entity.NewModelList[T](nil)` 操作；public/private 只调用模型封装的 `IDataAction` 持久化方法，不直接依赖 GORM/SQLite。数据库实现只在模型持久化边界选择，不沿 Service -> API -> Model 传递。
6. 嵌入 `*Model`/`*BaseModel` 必须在 `NewModel()` 初始化。`GetHash` 表达真实业务唯一性；`AddValid`/`UpdateValid` 同时保护字段和唯一性。
7. public/private 返回独立 `dto`，不得直接序列化可能深度嵌入的持久化模型；实现 `IRouterResponse.GetResponse()` 供 OpenAPI 描述。
8. WebSocket 只面向最终外部用户。内部服务通信使用 TransportSelector 与 EventBridge；private WebSocket 路由必须从会话注入可信身份并按用户隔离和过滤通知。
9. 先复用 go-zero/成熟客户端；Digitalway 抽象只保留路由、模型、MachineID、Provider 切换、事件和跨节点通知等领域契约。
10. 配置字段不等于支持。`Unsupported` 值必须 fail closed；QUIC/MQ transport 和内建 Kafka/RabbitMQ/RocketMQ 不得伪装可用。
11. CORS origin、TrustedProxies 和外部依赖必须显式配置。默认单元测试不依赖 Docker。
12. 日志使用 `logx` 稳定事件和字段；不记录 token、TOTP、payload/body/response、SQL、参数或对象 dump。
13. 修改公共 Go API、路由、JSON、配置或错误前，运行兼容性/发布契约并登记迁移。

## 工作流

1. 普通平台服务先读 `examples/01-simple-shop`；涉及业务层、跨模型事务、状态机或 Manage 自定义命令时再读 `examples/02-shop-payment`。
2. 先写失败测试，再做最小实现；不绕过 ServiceContext，Manage 不绕过 ModelList，普通 API 不绕过模型持久化方法。
3. 为服务创建集成测试时，复用 `examples/integration/helpers.go`，并以 `examples/integration/01-simple-shop` 为目录模板：每个 API/command 一个子测试，按 Manage/Public/Private 分文件，同时保留 `TestManageAPIs`、`TestPublicAPIs`、`TestPrivateAPIs` 整组入口。
4. 集成测试必须启动真实进程，使用自动生成配置、临时数据目录、真实 HTTP、内建 TestToken 和真实 WebSocket；测试结束必须关闭进程并清理临时目录。
5. 对外部能力同时检查 config Validate、factory、真实启动链、lifecycle owner 和 integration gate。
6. 运行 `gofmt`、定向测试、`./scripts/check-logging.sh`；跨模块变更再运行 `release-contract` 和对应 race/CI gate。

## 审查重点

- URL 中错误加入 `/public` 或 `/private`。
- 请求身份/trace 存入共享单例。
- `RouterInfo()` 取得已注册单例后再写 `Method`、`Path`、`Auth`、`ServiceName` 等冻结元数据；应改为 `DefaultRouterInfoWithOptions(own, router.With...)` 并通过 Getter 读取。
- 模型嵌入指针未初始化，或无 Code 模型误用 BaseModel。
- ManageService 传错 owner，导致 hook 不执行。
- public/private 直接返回持久化模型，或 DTO 混入集成测试公共 helpers。
- private WebSocket 接受客户端 UserID、跨用户投递，或为无额外启停动作的路由实现空生命周期回调。
- 集成测试重新实现进程/TestToken/WebSocket 公共能力，或只测 handler 而未经过真实启动链。
- 静默接受不支持配置、重复制造连接池/重试/日志/队列。
- 外部测试默认连接本机服务，或失败后残留 goroutine、容器和锁。
- 日志/响应泄露内部错误和业务数据。
