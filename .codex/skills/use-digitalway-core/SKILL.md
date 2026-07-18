---
name: use-digitalway-core
description: Use when 使用或审查 github.com/digitalwayhk/core 的服务、IRouter、Model/Manage 继承、认证、WebSocket、缓存、本地可靠写、EventBridge、配置、集成测试、性能或兼容性时。
---

# 使用 Digitalway Core

## 定位

Digitalway Core 是 go-zero 与成熟依赖之上的应用组装框架。代码是最终事实，示例是用法模板，`docs/codex` 现行指南是边界与运维契约。不得从已删除的 Copilot skill、历史计划、审查提示词或记忆恢复当前行为。

具体 API、目录、模型、Manage、WebSocket、认证和测试模板见 `references/core-backend-api.md`。

## 示例选择

| 场景 | 标准示例 | 核心能力 |
| --- | --- | --- |
| 最简平台服务 | `examples/01-simple-shop` | contract、models、DTO、Manage/Public/Private、TestToken、用户 WebSocket、真实集成测试 |
| 业务编排与状态机 | `examples/02-shop-payment` | API -> business -> models、跨模型事务、支付状态机、受控 Manage 命令 |
| 模型和 Manage 继承 | `examples/03-shop-inheritance` | Shop/BaseData/Business 多层模型、Manage Hook 继承、只读子表、联合有效性 |
| 性能优化 | `examples/04-shop-performance` | RouterInfo L1/L2/L3、EventBridge 主动失效、SingleFlight、Badger 可靠本地写、Group Commit、基准与分位数 |
| Casdoor 身份生命周期 | `examples/05-shop-casdoor-rbac` | Auth/Manage 双域、三类 Hook、撤销世代、Webhook、幂等审计、领域分包与 facade |
| Redis 多服务 | `examples/06-shop-microservices` | 统一 Manage Hook、受限 Public `WithInternalCallers`、买家 Private、数字业务 ID、`requestID` 幂等、永久 `SupplierOrder`、Redis 发现、mTLS、Outbox/Inbox |

对应真实进程测试位于 `examples/integration/01-simple-shop`至 `05-shop-casdoor-rbac`，多服务还必须同时参考 `examples/integration/06-shop-microservices` 和 `06-shop-microservices-three-process`；通用进程、HTTP、TestToken 和 WebSocket 能力只复用 `examples/integration/helpers.go`。

## 目录决策

- 最简 CRUD 按 01 平铺 `models/api`；出现跨模型规则时增加无请求状态的 `business`。
- 出现基础资料、交易业务或身份审计继承时，按 03/05 拆分 `common/basedata/transaction/identity`；根包只做兼容门面。
- 示例 06 这类多服务也必须按 05 的方式拆每个服务内部目录：`models/common` 定义服务级基础模型、数据库名和 TraceID，`models/basedata` 放基础资料，`models/transaction` 或对应业务域放交易事实，`models/internal/store` 统一 `IDataAction`，`models/schema` 统一建表，根 `models` 只保留 `models.go` 兼容门面，不放具体模型或持久化实现。
- `api/manage` 同样按 05 语义拆分：`api/manage/common` 放权限、owner、全服务最基础 `ServiceManage[T]`，`api/manage/basedata` 放 `BaseDataManage[T]`、基础资料 Manage 和命令，`api/manage/transaction` 放 `TransactionManage[T]`、订单/支付/投影等业务 Manage，`api/manage/audit` 放审计/身份事件；根 `api/manage` 只保留 `manage.go` 门面和路由注册入口。
- 多服务示例中每个服务都必须拥有独立的 Manage 继承树：`common.ServiceManage[T]` 继承框架可选 `manage.HookedManageService[T]`，`basedata.BaseDataManage[T]` 和 `transaction.TransactionManage[T]` 再继承本服务 `ServiceManage[T]`，每个具体 Manage 只能继承本目录的基础资料或业务基座，不能直接嵌入 `manage.ManageService[T]`。服务级权限、owner 限域、禁用主体拦截、分页、审计和日志这类横切逻辑必须写在 `common.ServiceManage[T]` 或更靠近根部的抽象基座，具体 Manage 只描述业务目标对象和业务动作，不到处重复鉴权或日志。自定义 Manage 命令的 `Do` 必须先调用 owner 的 `DoBefore` 复用服务级权限，再执行业务动作；不要另造 `CommandBefore` 这类命令专用旁路。
- `contract` 必须无依赖；DTO 只放 `api/dto`；API 依赖 business，business 依赖 models，不得反向引用。
- 单元测试与实现同目录；跨子包契约测试留 facade 根包；真实进程测试只放 `examples/integration/<service>`；固定样本放 `testdata/`。示例 06 三进程 UAT 必须按角色拆文件：买家、供应商、管理员各自文件保存本角色功能闭环和异常权限断言，完整业务流程测试只组合这些角色步骤，不把所有角色逻辑堆在一个大测试函数里。
- 新增或重排代码默认按 struct 拆文件：一个业务 struct 一个源文件；同文件出现多个 struct 只允许紧密配套的小请求/响应/测试桩，并必须保持可读。禁止把多个模型、多个 Manage、多个 Router 或多个 DTO 聚在一个大文件里。
- 每个源文件开头都必须有中文文件级注释，说明该文件在服务/目录中的能力边界；每个导出的 public 类型、函数、方法和变量必须有中文注释，复杂 private 函数也要补充意图说明。测试文件的文件级注释要写清测试的业务闭环、角色或边界，不允许只靠测试名让人猜。

## 现行指南索引

| 任务 | 必读文档 |
| --- | --- |
| 场景和能力选择 | `docs/codex/FRAMEWORK_USAGE_GUIDE.md` |
| 配置是否真正接入运行时 | `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md` |
| RouterInfo、对象池、EventBridge、缓存、WebSocket 和生命周期 | `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md` |
| 日志级别、字段和敏感信息 | `docs/codex/LOGGING_AUDIT_AND_STANDARD.md` |
| Docker 外部依赖集成 | `docs/codex/EXTERNAL_INTEGRATION_GUIDE.md` |
| NATS JetStream 可靠写路径 | `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md` |
| 性能、容量、RED/USE 和 SLO | `docs/codex/PERFORMANCE_SLO_BASELINE.md` |
| go-zero 与成熟能力复用 | `docs/codex/GO_ZERO_REUSE_AUDIT.md` |
| 自定义 Socket 升级到 gRPC | `docs/codex/GRPC_TRANSPORT_MIGRATION.md` |
| 无用代码和架构债 | `docs/codex/DEAD_CODE_AUDIT.md`、`ARCHITECTURE_HARDENING.md` |
| 公共 API、废弃和消费方兼容 | `docs/codex/API_COMPATIBILITY_SURFACE.md`、`DEPRECATION_REGISTER.md`、`CONSUMER_COMPATIBILITY_MATRIX.md` |
| CI 和发布门禁 | `docs/codex/CI_QUALITY_GATE_MATRIX.md`、`docs/RELEASE_POLICY.md` |

`PROJECT_REVIEW_ACTION_PLAN.md`、`plans/`、`*_PROMPT.md`、`*_REVIEW.md` 和 `COMPLETED_TASKS_IMPLEMENTATION_REVIEW.md` 是历史审计证据，不是新实现的默认规范。

## 不可违反的契约

1. public/private URL 为 `/api/{service}/{router}`；Manage 为 `/api/manage/{service}/{manage}/{operation}`。
2. 服务名放无依赖 `contract`；RouterInfo 注册后 Path、ServiceName、Method、Auth 等元数据冻结，只通过 Getter 读取。
3. private 身份只读 `req.GetUser()`/claims，缓存键和 WebSocket 订阅不信任客户端 UserID。
4. Manage CRUD 不绕过 ModelList；public/private 不直接依赖 GORM/SQLite；`IDataAction` 实现只在 models 边界选择。
5. 模型嵌入指针必须在 `NewModel()` 初始化；`GetHash` 表达真实业务唯一性；引用后的基础资料只能禁用，不能删除。
6. public/private 返回独立 DTO 并实现 `GetResponse()`，不直接序列化深度继承的持久化模型。
7. WebSocket 只面向最终外部用户；内部同步调用默认使用 gRPC，HTTP 仅显式发送前备用，内部异步事件使用 EventBridge。服务发布事件只在 `Start()` 中声明 `sc.UseOutbox(models.OutboxStore{})`，订阅只使用统一 `sc.SubscribeEvent(event.Subscription{Subject, EventType, Reliable, Handler})`；业务不再手写 Outbox worker、`SubscribeExternalControl` 或同时注册内外两套订阅。`EventType` 可为空，表示订阅该 Subject 下全部事件类型。
8. `UseCache` 是 API 级唯一启用声明；默认 local L1，L2/shared 才需显式配置；控制事件通过 EventBridge 主动失效。多服务 public/private 缓存只放在面向外部流量的入口服务 facade，例如 06 的 user-service；supplier/order 这类内部权威服务的 Public API 不再重复缓存，避免展示缓存与权威校验缓存双层失效。
9. Badger pending 是未同步业务事实，不是可丢弃缓存；高 TPS 写路径只能在本地持久成功后确认。
10. Casdoor Auth/Manage 是独立域，分离 Client、Access/Refresh/Webhook Secret；Callback、Refresh、REST 和 WebSocket 共享撤销权威。
11. 优先复用 go-zero/成熟客户端；不支持的配置值 fail closed，不得伪装可用。
12. 日志使用 `logx` 稳定事件和字段，不记录 token、payload/body/response、SQL、参数或对象 dump；Manage 生命周期日志参考 05 的 `ShopManage.logManageResult`，统一事件名 `shop_manage_operation_failed/succeeded` 和字段 `owner/phase/service/route/trace_id/code`，不要按服务发明 `shop_user_manage...` 之类事件。
13. 修改公共 Go API、HTTP/JSON、配置或错误前后运行兼容/发布契约并登记迁移。
14. 跨进程调用直接构造目标 API，但 Go 目录名与稳定服务名不同时必须在注册前用 `WithServiceName` 和 `WithPath` 显式声明；地址只由 ClusterProvider + ServiceResolver 解析，新代码不读 `AttachServices`，也不启用第二套 zrpc 服务发现。
15. 跨服务控制事件使用逻辑服务消费组、可返回 error 的 Handler、成功后 ACK、pending reclaim 和 Inbox 幂等；业务事实与 Outbox 必须同事务。`OutboxStore` 只实现 `LoadPending/MarkPublished`，不关心服务名、消费者或 MQ；当前服务名由 `ServiceContext` 作为事件 Source，Subject 来自 Outbox 记录，谁订阅谁消费。TraceID 从最外层请求生成并透传到内部调用、业务事实、Outbox、事件 Metadata、Inbox 和本地投影；EventID 仍是事件幂等键，不用 TraceID 代替。
16. gRPC Client 复用 zrpc；每个 ServiceContext 独立管理 grpc-go Server。跨主机生产使用 mTLS 或已有双向身份的 mesh，禁止 insecure。
17. 内部专用 Public 必须用 `WithInternalCallers` 声明白名单；同进程只信源 ServiceContext，远程只信已验证且与 `SourceService` 一致的 mTLS SAN，HTTP 和调用方自报字段不能建立内部身份，拒绝必须早于 Parse。
18. 多角色自管理优先复用同一 Manage 和 Search/Do Hook 自动限域，不复制平台/本人两套 API；复杂服务优先使用 `manage.HookedManageService[T]` 提供的细粒度 `On...Before/On...After` 辅助基类，再由服务级、基础资料级和业务级基座逐层覆盖；权限、日志和通用限域只在抽象层实现一次，具体 Manage 不重复；自定义命令也走 owner `DoBefore`，不增加命令专用 Hook 旁路；跨服务引用删除保护使用可靠事件形成的本地永久 `SupplierOrder`，不在删除 Hook 中同步查询远端。
19. 每个服务必须有服务级基础模型承载 `GetLocalDBName/GetRemoteDBName`、数据库名和 `TraceID`；基础资料模型和业务事实模型继承它，具体模型再继承基础资料或业务事实模型。不要在每个具体模型上重复写数据库名或 TraceID 字段。
20. 示例 06 三个服务必须使用三个不同本地库名，并由各自 `models/common` 的基础模型决定；不能共享同一 SQLite 文件，也不能把库名散落在具体模型里。
21. 示例、能力代码和测试必须保持中文注释契约：文件开头先说明本文件提供的能力；所有 public API、导出类型、导出方法和导出函数必须有中文注释；复杂 private 逻辑按读者理解成本补注释；测试文件注释必须说明验证的场景、角色和边界。

## 工作流

1. 选择最近示例，再读对应现行指南，然后核对当前代码。
2. 先写失败测试；不绕过 ServiceContext、ModelList、模型持久化和认证生命周期。
3. 集成测试启动真实进程，使用自动生成配置、临时数据目录、真实 HTTP/WebSocket；普通业务用 TestToken，Casdoor 生命周期用 Fake Casdoor。
4. 运行 `gofmt`、定向测试、race、`./scripts/check-logging.sh`；跨模块变更再运行 `release-contract` 和对应 CI gate。

## 审查红旗

- RouterInfo 冻结后修改元数据，或在共享单例中保存请求、用户、trace、response。
- Manage owner 绑定错误、子类覆盖 Hook 却丢失必需父级规则、具体 Manage 直接嵌入框架 `ManageService` 绕过服务级/基础资料级/业务级基座、具体 Manage 重复实现服务级权限/日志，或用通用 CRUD 绕过状态机。
- public/private 直接返回持久化模型，DTO 混入公共测试 helpers。
- WebSocket 接受客户端 UserID、跨用户投递，或内部服务用 WebSocket 通信。
- 内部同步调用重新保存静态地址、启用自定义 Socket、让 zrpc 自带发现绕过 Core Resolver，或在生产跨主机使用 insecure gRPC。
- 业务服务自己保存 Outbox worker、轮询发布事件、直接调用 `SubscribeExternalControl`，或者让发布方知道消费者是谁；标准方式是 `sc.UseOutbox` 和 `sc.SubscribeEvent`。
- 为内部服务复制 `api/call` 路由、把 Public 当作天然外网开放、相信 Header/`SourceService` 自报身份，或在受限路由 Parse 后才鉴权。
- 在 skill、文档或代码注释里把示例 06 描述成 `api/call` 目标，或暗示需要复制调用 API。
- 把多个模型/Manage/Router/DTO struct 塞进一个大文件，或者把具体模型/Manage 实现留在根 `models`、根 `api/manage`，或者绕过服务级基础模型/服务级 Manage 基座在具体模型或具体 Manage 上重复声明公共行为。
- `UseCache` 依赖全局开关、缓存键缺少身份/筛选维度、只靠 TTL 不主动失效、内部权威服务 Public 重复缓存入口 facade 已缓存的数据，或把 write-behind pending 当缓存删除。
- 集成测试重复实现通用进程/TestToken/WebSocket 能力，只测 handler，或默认依赖 Docker/外部服务。
- 日志/响应泄露内部错误、Token、Claims、Header、请求或业务数据。
