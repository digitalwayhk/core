---
name: use-digitalway-core
description: Use when 使用或审查 github.com/digitalwayhk/core 的服务、IRouter、基础资料 Model/业务事实 Model 分类、Model/Manage 继承、Manage 动态分库 IDBName、认证、WebSocket、缓存、本地可靠写、EventBridge、业务统计、经营分析、服务报表、多服务运行图 Runtime API、配置、集成测试、性能或兼容性时。
---

# 使用 Digitalway Core

## 本文件是唯一权威源

本目录 `docs/ai/core-skill/` 是 Core 开发规范的**唯一权威源**。`.codex/skills/`、`.claude/skills/`、`.cursor/skills/`、`.github/copilot/skills/` 下只放指向本目录的指针文件，不得保存正文副本——历史上维护两份全文导致规范双向漂移（例如"框架自动建库建表"一度只写在 Copilot 那份里，而消费方 AI 读的是另一份）。

修改规范时只改本目录。新增内容放对应主题分片，不要把所有内容堆回单个文件：单文件超过约 60 KB 会触发部分工具链的管道截断，也违反 skill 的渐进式披露要求。

## 定位

Digitalway Core 是 go-zero 与成熟依赖之上的应用组装框架。代码是最终事实，示例是用法模板，`docs/codex` 现行指南是边界与运维契约。不得从已删除的历史 skill、历史计划、审查提示词或记忆恢复当前行为。

若当前工作区是**依赖 core 的业务仓库**且本 skill 不在本地，先按 core 仓库 README「AI 助手与 Skill」与 `docs/codex/CONSUMER_AI_SKILL_SETUP.md` 运行 `scripts/link-consumer-skill.sh` 安装，再继续开发；不要只依赖记忆中的旧约定。

## 主题分片索引

| 任务 | 分片 |
| --- | --- |
| 目录结构、业务层分层、启动组合根 | [project-layout.md](project-layout.md) |
| IRouter、RouterInfo、路径规则、Public/Private、DTO | [routing-and-dto.md](routing-and-dto.md) |
| 模型分类与继承、持久化边界、**建库建表与字段迁移** | [models.md](models.md) |
| Manage CRUD、Hook 继承、`GetList` 数据源、动态分库 | [manage.md](manage.md) |
| RouterInfo 缓存、本地可靠写、write-behind、水平扩展 | [write-path-and-performance.md](write-path-and-performance.md) |
| 多服务调用、EventBridge、WebSocket、Cluster/MQ、Runtime 观测、日志 | [multiservice-and-observability.md](multiservice-and-observability.md) |
| 业务统计、经营分析、服务报表 | [stats-and-reports.md](stats-and-reports.md) |
| Casdoor 双域、Web Admin bootstrap、HTMLServer 边界 | [auth-casdoor-and-admin.md](auth-casdoor-and-admin.md) |
| 命名规范与设计流程 | [naming-and-workflow.md](naming-and-workflow.md) |
| OpenAPI 与前端调用约定 | [openapi-and-frontend.md](openapi-and-frontend.md) |
| 集成测试模板、UAT 角色拆分、发布门禁 | [testing-and-release.md](testing-and-release.md) |
| 高频错误快速自检 | [common-mistakes.md](common-mistakes.md) |

`docs/codex/` 下的现行指南仍是运维与兼容契约来源：场景选择看 `FRAMEWORK_USAGE_GUIDE.md`，配置能力看 `CONFIG_RUNTIME_CAPABILITY_MATRIX.md`，运行时看 `ROUTERINFO_RUNTIME_GUIDE.md`，日志看 `LOGGING_AUDIT_AND_STANDARD.md`，外部依赖看 `EXTERNAL_INTEGRATION_GUIDE.md`，JetStream 看 `NATS_JETSTREAM_WRITE_PATH_GUIDE.md`，性能看 `PERFORMANCE_SLO_BASELINE.md`，兼容与废弃看 `API_COMPATIBILITY_SURFACE.md`、`DEPRECATION_REGISTER.md`、`CONSUMER_COMPATIBILITY_MATRIX.md`，CI 与发布看 `CI_QUALITY_GATE_MATRIX.md`、`docs/RELEASE_POLICY.md`。

`PROJECT_REVIEW_ACTION_PLAN.md`、`plans/`、`*_PROMPT.md`、`*_REVIEW.md` 是历史审计证据，不是新实现的默认规范。

## 示例选择

| 场景 | 标准示例 | 核心能力 |
| --- | --- | --- |
| 最简平台服务 | `examples/01-simple-shop` | contract、models、DTO、Manage/Public/Private、TestToken、用户 WebSocket、真实集成测试 |
| 业务编排与状态机 | `examples/02-shop-payment` | API -> business -> models、跨模型事务、支付状态机、受控 Manage 命令 |
| 模型和 Manage 继承 | `examples/03-shop-inheritance` | Shop/BaseData/Business 多层模型、Manage Hook 继承、只读子表、联合有效性 |
| 性能优化 | `examples/04-shop-performance` | RouterInfo L1/L2/L3、EventBridge 主动失效、SingleFlight、`OrderWriteRuntime` + `UseWriteBehind`、Group Commit、基准与分位数 |
| Casdoor 身份生命周期 | `examples/05-shop-casdoor-rbac` | Auth/Manage 双域、三类 Hook、撤销世代、Webhook、幂等审计、领域分包与 facade |
| Redis 多服务 | `examples/06-shop-microservices` | 统一 Manage Hook、受限 Public `WithInternalCallers`、买家 Private、数字业务 ID、`requestID` 幂等、永久 `SupplierOrder`、Redis 发现、mTLS、Outbox/Inbox |
| 订单水平扩展 | `examples/07-shop-order-scale` | Order 多副本、`AutoMachineID=true`、ServiceInstanceID、实例级 `OrderWriteRuntime`、共享 MySQL 远程权威库、`OrderRule` 配置同步、Prometheus scrape、Runtime 运行图验收 |
| 业务统计、经营分析与服务报表 | `examples/07-shop-order-scale/order-service` | `stats.StatSpec`、OLTP/ClickHouse 引擎、快照 `Store`、`stats.Dashboard`、`stats.ReportDef`、Manage API 与服务子菜单 |
| 多服务运行图（框架） | Admin `MonitorSystem` + ServerManage Runtime API | `POST /api/servermanage/runtimetopology`、`runtimeservice`；ClusterProvider + Prometheus；指标 `null+state` |

对应真实进程测试位于 `examples/integration/01-simple-shop` 至 `05-shop-casdoor-rbac`；多服务还必须同时参考 `examples/integration/06-shop-microservices` 与 `06-shop-microservices-three-process`；水平扩展参考 `examples/integration/07-shop-order-scale` 与 `07-shop-order-scale-multi-process`。通用进程、HTTP、TestToken 和 WebSocket 能力只复用 `examples/integration/helpers.go`。

## 模型分类是首要设计

拿到需求后先拆数据结构，再设计 API。业务域持久化 Model 必须先按**数据生命周期**分成两大类；"基础"不是继承树中的上层，也不等于 Go 类型名带 `Base`：

| 分类 | 判定 | 典型数据 | 默认处理方式 |
| --- | --- | --- | --- |
| 基础资料 Model（主数据/原数据） | 用户需要中相对稳定、集合规模和增长速度可预期，供其他业务引用 | 商品、商户/供应商、订单类型、支付类型 | Code/Name 等稳定标识、启用/禁用、引用保护；被业务引用后通常禁用而非删除 |
| 业务事实 Model | 必须保存所依赖基础资料的 ID，由用户业务事件持续产生；数量无上限、增长频率不可预知 | 订单、支付流水、库存流水、结算记录 | 受控创建、幂等、状态机、只读/受控命令、不可任意删除；按容量设计索引、归档、分区或高吞吐写 |

先画两条**平行**继承支路，再放具体模型：

```text
entity.Model
└── ServiceModel                  # 仅承载服务公共持久化能力，不叫"基础资料"
    ├── BaseDataModel             # 基础资料支路
    │   ├── Product
    │   ├── Supplier
    │   └── OrderType
    └── BusinessModel             # 业务事实支路
        ├── Order                 # ProductID/SupplierID/OrderTypeID + 必要快照
        └── PaymentRecord         # OrderID/PaymentTypeID + 业务事实
```

两类具体模型不得互相继承，也不得让 `BusinessModel` 继承 `BaseDataModel`。共享数据库名、TraceID、租户字段和 DataAction 的 `ServiceModel` 只是**服务公共模型基座**；它不参与业务分类。Outbox、Inbox、审计日志等技术记录属于基础设施持久化模型，单独放 `internal/store`/`audit`。

业务事实保存基础资料 ID 作为关联权威，并按审计需要保存名称、编码、价格等历史快照。若一个"商品"结构同时承载无上限的库存或价格变动，应拆成稳定 `Product` 基础资料与 `InventoryRecord`/`PriceRecord` 业务事实。

详细继承写法、框架结构体选择与哈希规则见 [models.md](models.md)。

## 不可违反的契约

1. public/private URL 为 `/api/{service}/{router}`；Manage 为 `/api/manage/{service}/{manage}/{operation}`；ServerManage 为 `/api/servermanage/{router}`。`public`/`private` 不进入 URL。
2. 服务名放无依赖 `contract`；RouterInfo 注册后 Path、ServiceName、Method、Auth 等元数据冻结，只通过 Getter 读取。
3. private 身份只读 `req.GetUser()`/claims，缓存键和 WebSocket 订阅不信任客户端 UserID。
4. **数据访问分两套，不可混用职责**：Manage 走 `ModelList` 获得框架筛选/排序/分页；public/private 走 models 业务方法（内部用集中获取的 `IDataAction`），复杂编排放 business；高吞吐写再升级为专用 store + 本地可靠写 + `UseWriteBehind`。共用 model 结构体，不共用访问方式。详见 [manage.md](manage.md) 与 [write-path-and-performance.md](write-path-and-performance.md)。
5. Model 必须先按生命周期归入基础资料或业务事实两条平行支路，不能按"谁是继承上层"判断。模型嵌入指针必须在 `NewModel()` 初始化；`GetHash` 表达真实业务唯一性；引用后的基础资料通常只能禁用，不能删除。
6. **建库、建表和字段迁移由框架自动完成，业务代码不得自建。** 禁止 `CREATE TABLE`/`init.sql`、migration 目录、版本化迁移框架或业务代码直接调用 GORM `AutoMigrate`。破坏性变更（删列、改类型、加约束）不自动执行，需走发布流程。机制与边界见 [models.md](models.md)。
7. public/private 返回独立 DTO 并实现 `GetResponse()`，不直接序列化深度继承的持久化模型。
8. WebSocket 只面向最终外部用户；内部同步调用默认 gRPC，HTTP 仅显式发送前备用，内部异步事件用 EventBridge。发布只在 `Start()` 声明 `sc.UseOutbox(...)`，订阅只用 `sc.SubscribeEvent(event.Subscription{...})`；业务不手写 Outbox worker 或双套订阅。
9. `UseCache` 是 API 级唯一启用声明；默认 local L1，L2/shared 才需显式配置；控制事件通过 EventBridge 主动失效。多服务缓存只放在面向外部流量的入口服务 facade。
10. Badger pending 是未同步业务事实，不是可丢弃缓存；高 TPS 写路径只能在本地持久成功后确认。
11. Casdoor Auth/Manage 是可同时启用的两个独立认证域，Secret 必须隔离；Auth Token 只进 Private/用户 WebSocket，Manage Token 只进 Manage。`/api/web/bootstrap` 只属于启用 `ViewPort` 的 HTMLServer 开发/测试视图。详见 [auth-casdoor-and-admin.md](auth-casdoor-and-admin.md)。
12. 优先复用 go-zero/成熟客户端；不支持的配置值 fail closed，不得伪装可用。
13. 日志使用 `logx` 稳定事件和字段，不记录 token、payload/body/response、SQL、参数或对象 dump。
14. 修改公共 Go API、HTTP/JSON、配置或错误前后运行兼容/发布契约并登记迁移。
15. 跨进程调用直接构造目标 API，但 Go 目录名与稳定服务名不同时必须在注册前用 `WithServiceName` 和 `WithPath` 显式声明；地址只由 ClusterProvider + ServiceResolver 解析。
16. 跨服务控制事件使用逻辑服务消费组、可返回 error 的 Handler、成功后 ACK、pending reclaim 和 Inbox 幂等；业务事实与 Outbox 必须同事务。TraceID 从最外层请求生成并透传；EventID 仍是事件幂等键。
17. gRPC Client 复用 zrpc；每个 ServiceContext 独立管理 grpc-go Server。跨主机生产使用 mTLS 或已有双向身份的 mesh，禁止 insecure。
18. 内部专用 Public 必须用 `WithInternalCallers` 声明白名单；HTTP 和调用方自报字段不能建立内部身份，拒绝必须早于 Parse。匿名 `/api/openapi` 必须过滤这类路由。
19. 多角色自管理优先复用同一 Manage 和 Search/Do Hook 自动限域，不复制平台/本人两套 API；权限、日志和通用限域只在抽象层实现一次；自定义命令走 owner `DoBefore`。
20. 每个服务必须有服务公共模型基座承载 `GetLocalDBName`/`GetRemoteDBName`、数据库名和 `TraceID`；两条业务支路继承它。不要在每个具体模型重复写数据库名或 TraceID。
21. 水平扩展必须区分服务水平扩展、业务拆库和技术分片。默认不按服务实例拆最终业务库；多实例先写本地可靠 pending，再异步同步到同一个业务域远程权威库。
22. 自动水平扩展必须启用 `AutoMachineID=true`，验证 ClusterProvider lease、ServiceInstanceID、多副本发现、本地 pending 目录隔离、共享远程权威库和优雅下线恢复；不得硬编码固定 MachineID，也不得把注册发现写死到 Redis。
23. 高吞吐业务写必须**先可靠写入本地 pending，再异步同步到远程权威库**，经 `UseWriteBehind(WriteBehindTarget)` 绑定目标。不得把每进程私有库冒充共享 remote，也不得用 Manage 的 `ModelList` 表轮询充当业务写热路径。
24. 幂等边界必须在 README 写明当前扩展范围与降级风险，不得隐式失败或假装仍保持全局幂等。
25. 性能 benchmark 使用示例 04 的模式：fixture 在计时外准备数据，按能力拆分负载，使用 `ReportAllocs/ResetTimer/StopTimer` 与分位数指标；创建业务主键必须用框架 `req.NewID()` 或同源 Snowflake worker。
26. 示例、能力代码与测试必须保持中文注释契约：文件开头说明本文件能力；所有导出类型、方法、函数必须有中文注释；测试注释说明验证的场景、角色和边界。
27. 多服务业务必须提供真实多进程 UAT，并按角色或调用方拆分可单独运行的角色闭环测试。
28. 只要服务实现 WebSocket 能力，集成测试和 UAT 就必须用真实 WebSocket 覆盖登录、按真实 RouterInfo 路径订阅、事件结构、当前用户投递、其他用户隔离与异常边界。
29. 多服务运维观测使用 ServerManage Runtime API，窗口仅 `15s|5m|1h`。ClusterProvider 是实例与地址权威，Prometheus 是历史指标权威；指标缺失必须返回 `null` 并带 `state`，禁止把未采集伪装成零。不得恢复已废弃的 `RouterStats`/`/api/servermanage/statistics`。
30. 高吞吐写路径必须使用实例级 `OrderWriteRuntime`（或等价注入访问面）+ `ServiceContext.UseResource` 管理生命周期；禁止包级全局 store registry。示例 04/07 的 `StartOrderWriteStore`/`StopOrderWriteStore` 已删除，不存在可调用版本；`SetSyncDB`、`EnableWriteBehind(ModelList)` 仍存在但仅为兼容层。
31. 有序可靠投递等加性 MQ 契约以 `docs/codex/API_COMPATIBILITY_SURFACE.md` 与当前测试为准；未声明 requirement 时保持零值兼容，不得假装所有 Provider 都已支持有序语义。
32. **库类型与连接获取集中在 models**：在服务公共模型/持久化组合根声明 `LocalDataAction`/`RemoteDataAction`/`ManageDataAction` 等入口；切换 SQLite→MySQL 只改这些实现，不改遍业务 API。
33. **Manage 动态分库**走 `IDBName` + 空 `Database` MySQL + 标准 `LoadList`，不用 `OnSearchBefore`+`stop=true` 自研列表。缺分库键时的 fail-closed 必须同时约束 `GetRemoteDBName` 与 `GetLocalDBName`（实现会在前者为空时回退后者）。硬条件与易踩坑见 [manage.md](manage.md)。
34. **业务统计、经营分析与服务报表不是零接线自动 CRUD**：必须声明并 `stats.Register` 全局唯一 `StatSpec.Code`，在任务层刷新服务自己的 `stats.Store`，API 只读快照。`ReportDef` 只描述展示，不会自动创建事实数据、Runner 或 API。详见 [stats-and-reports.md](stats-and-reports.md)。
35. 新增或重排代码默认按 struct 拆文件：一个业务 struct 一个源文件。禁止把多个模型、多个 Manage、多个 Router 或多个 DTO 聚在一个大文件里。

## 工作流

1. 选择最近示例，再读对应主题分片与现行指南，然后核对当前代码。
2. 先写失败测试；不绕过 ServiceContext、Manage 的 ModelList、models 层 DataAction/业务 store 与认证生命周期。
3. 集成测试启动真实进程，使用自动生成配置、临时数据目录、真实 HTTP/WebSocket；普通业务用 TestToken，Casdoor 生命周期用 Fake Casdoor。
4. 运行 `gofmt`、定向测试、race、`./scripts/check-logging.sh`；跨模块变更再运行 `release-contract` 和对应 CI gate。
5. 发现指南与代码不一致时，以代码、测试和公开契约为准，并回写本目录对应分片。

## 审查红旗

高频反模式的完整清单见 [common-mistakes.md](common-mistakes.md)。最常见的几类：

- RouterInfo 冻结后修改元数据，或在共享单例中保存请求、用户、trace、response。
- 把"基础 Model"理解成继承树最上层或 `entity.BaseModel`；让业务事实继承基础资料支路；业务事实不保存基础资料 ID。
- public/private 直接 `NewModelList` 做业务读写，或把 Manage CRUD/Search 当业务接口。
- 手写 `CREATE TABLE`/`CREATE DATABASE`、`init.sql`、`migrations/` 目录或在业务代码调用 GORM `AutoMigrate`。
- Manage 在 `OnSearchBefore` 手写列表并 `stop=true`，破坏标准筛选、排序、分页与 `SearchAfter`。
- 动态分库时 MySQL `config.Database` 非空、缺分库键时回退到默认业务库、或指望一次 Search 跨多个分库。
- WebSocket 接受客户端 UserID、跨用户投递，或内部服务用 WebSocket 通信。
- Runtime 聚合把未采集指标写成 0；浏览器直连 Prometheus 或其他副本 `/metrics`。
