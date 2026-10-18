# 示例 07：订单服务水平扩展

本示例演示商城订单量增长后，`shop-order` 通过多实例水平扩展、本地可靠写、异步同步远程 order 权威库和标准 EventBridge 事件保持吞吐与一致性。

最终数据库按业务域拆分，不按技术实例拆分。多个 order 实例共享同一个 MySQL 远程 order 权威库；每个实例拥有自己的 Badger 本地 pending store，用于可靠接收、崩溃恢复、批量同步和故障重试。

## 目标

- 复用 06 的 `shop-user`、`shop-supplier`、`shop-order` 三服务边界。
- 重点扩展 `shop-order`：同一逻辑服务启动多个副本，普通用户下单可被路由到任意副本。
- `shop-order` 的管理配置只有一份权威数据，例如支付类型、订单规则和最小下单数量。
- 下单事实先写入当前副本 Badger 本地可靠 pending store，再异步同步到共享 MySQL 远程 order 权威库。
- 所有业务事实、pending、Outbox、Inbox 和投影记录 `TraceID`、`ServiceName`、`ServiceInstanceID`，便于跨服务追踪。
- 示例必须验证 `AutoMachineID=true`，扩容后自动分配唯一 MachineID，不依赖人工配置固定编号。

## 服务边界

| 服务 | 对外能力 | 内部能力 | 水平扩展要求 |
| --- | --- | --- | --- |
| `shop-user` | 普通用户注册、资料、地址、下单入口、订单查询、WebSocket 订单订阅 | 调用 supplier/order 的受限 Public API | 入口服务可扩展，但缓存只放在入口 facade |
| `shop-supplier` | 供应商后台资料、产品上下架 | 提供供应商和商品受限 Public API | 保持供应商域权威库独立；07 未接入供应商订单事件消费，事务投影演示见 06 |
| `shop-order` | 不暴露外部端口，只面向管理员 Manage | 提供下单、撤单、支付、支付类型、订单查询等受限 Public API | 多副本共享 MySQL 远程 order 权威库，每个副本独立 Badger pending |

内部专用 Public API 必须使用 `WithInternalCallers`，调用方身份只能来自可信 ServiceContext 或 mTLS SAN，不能相信 HTTP Header 或请求体自报。

## 数据与一致性

```text
buyer request
  -> shop-user private facade
  -> shop-order public API（resolver 选择任意 order 副本）
  -> order 副本 Badger pending store 持久成功后返回
  -> order 副本后台同步循环批量写入共享 MySQL 远程 order 权威库
  -> order 权威事实与 Outbox 同 MySQL 事务写入
  -> sc.UseOutbox(models.OutboxStore{}) 发布标准事件
  -> EventBridge 投递 OrderCreated/OrderStatusChanged/PaymentChanged
  -> user 消费事件失效订单缓存并推送在线用户 WebSocket
```

本地 pending 是可靠写入队列，不是缓存，也不是 SQLite 业务表。服务重启、扩容、缩容或同步失败时，pending 必须可恢复、可重试、可观测。`shop-order` 使用框架 `ReliableWriteStore[Order]`，只绑定一次业务 `OrderWriteBehindTarget`；订单 target 负责 `UserID+RequestID` 幂等 upsert 和同事务写 Outbox，Group Commit、背压、磁盘指标、pending ACK 与关闭恢复由框架统一处理。同步使用框架自适应 worker，单批最多 512 条，32 条触发或最长收集 20ms，积压有进展时立即继续；取消原业务 ticker，不在每批热路径重新扫描全部 pending。target 在新批次前检查服务准入，MySQL 提交后唤醒标准发布器。runtime 与资源托管成功后才启动 worker。

`Service` 持有实例级 `OrderWriteRuntime`，路由、`LocalOrderWriter`、查询和同步器都通过显式 `OrderWriteAccess` 注入。store 由 `ServiceContext.UseResource` 关闭，不存在包级全局 store registry。`ModelList` 只服务 Manage/视图/低频管理 CRUD；下单、支付、撤单链路必须走 business + 专用 target。

规则与远端幂等的单条只读查询使用模型层独立 action，不额外开启事务或读取分页总数；多条读取需要一致性快照时仍使用事务。默认规则未配置时不缓存回退值，管理员新增后下一次读取即可生效。远端幂等键不存在可继续接单，数据库读取失败必须返回错误。

## 管理配置同步

`shop-order` 管理 API 只维护一份共享业务配置，包括：

- 支付类型。
- 订单规则。
- 最小下单数量。
- 是否允许撤单或支付的状态规则。

任意 order 副本处理下单时，都必须读取同一份 MySQL 规则权威数据。配置变更后通过标准 EventBridge 发布控制事件，入口 facade 只在 `shop-user` 缓存并主动失效。

## 自动水平扩展约束

- 示例必须启用 `AutoMachineID=true`，不能为 order 副本硬编码 `MachineID=1/2/3`。
- 默认 `legacy` 模式通过 ClusterProvider 申请 MachineID lease；显式 `stable` 模式使用独立编号账本，发现注册不分配或释放编号。两者都须在 Snowflake 初始化前完成绑定。本示例设置 `SHOP_IDENTITY_ROOT` 及 `SHOP_GENERATOR_*` 后启用 stable，独占状态卷不得由两个活跃副本共用。
- 每个副本必须拥有唯一 `ServiceInstanceID`，并记录到业务事实、pending、Outbox、Inbox、同步状态和诊断日志；`ServiceInstanceIP` 只用于诊断。
- 本地 pending 目录按服务与已绑定编号隔离。legacy 的 AutoMachineID 变化会进入新目录，不会自动接管旧编号的 pending；缩容或重新分配前必须完成 drain，或由编排层显式把旧独立卷交给恢复流程。stable 必须保留身份及独立编号状态，并按现行恢复契约接管原卷，不能仅凭相同 IP 接管。
- Docker 扩容时不固定暴露多个 order 业务端口；副本通过配置的发现机制注册，调用方只通过 ServiceResolver 选择实例。
- `shop-order` scale 模板不传固定 `-p/-grpc`；需要覆盖容器内监听端口时使用 `SHOP_ORDER_HTTP_PORT/SHOP_ORDER_GRPC_PORT`。
- 注册发现机制不应绑定 Redis；Redis、局域网发现或其他中间件由配置决定，示例只验证框架抽象可用。
- Docker `--scale shop-order=N` 模板不能挂载共享 named volume；每个副本的本地 pending 必须由容器私有目录或编排平台独立卷承载。

下单返回 `accepted` 表示当前副本已经可靠持久化到 Badger；同步到 MySQL 前，其他 order 副本无法合并该副本的本地 pending，这是示例有意展示的最终一致窗口。user-service 会对 `UserID+RequestID` 生成稳定 OrderID，order-service 也会在 Public 下单时探测 MySQL 幂等键，避免常规重试拿到不同订单号。

这里有两个有意保留的示例边界：

- 07 重点验证 `shop-order` 的水平扩展。当前 `shop-user` 的 `UserID+RequestID -> OrderID` 稳定映射是进程内辅助，能覆盖单入口实例的重试；如果生产中 `shop-user` 也要多副本扩容或重启后继续保证未同步窗口内订单号不漂移，应把该映射提升到 Redis/MySQL 等共享幂等存储，或采用可审计的确定性订单号策略。
- Public 下单打开远程幂等探测后采用 fail-closed：本地 pending 未命中时会先查共享 MySQL 幂等键，MySQL 不可达则拒绝本次接单，而不是降级成可能产生双 pending 的纯本地接单。`Benchmark07LocalOrderAccept` 不打开该探测，只度量 04 风格本地可靠写的热路径。

## 业务统计与经营分析看板

`shop-order` 演示框架 `pkg/persistence/entity/stats`：

1. **Model 层** `order-service/models/stats/order_stats.go` 只声明 `StatSpec`（事实表、时间粒度、维度、count/sum/avg），不写 SQL。  
2. **任务层** `business.SharedStatsRunner` 定时（默认 2 分钟）执行 Spec，结果写入进程内 `OrderStatsStore`（覆盖写、无 TTL 过期）。  
3. **原始快照 API** `POST /api/manage/shop-order/bizstats/query` 只读 Store。  
4. **标准分析 API** `POST /api/manage/shop-order/analysis` 返回 `stats.Dashboard`，与 Admin **分析页**（`/dashboard/analysis`）的 `AnalysisData` 对齐；指标名、Tab 文案、排名标题均由 `layout` 动态下发。

预置 Spec：

| code | 说明 |
| --- | --- |
| `order.by_day` | 按天汇总行数与金额 |
| `order.by_day_product` | 按天×商品（展示用订单快照 ProductCode/Name） |
| `order.by_month_supplier` | 按月×供应商 |

示例：

```bash
# 经营分析看板（Admin 分析页同源）
curl -s -X POST 'http://127.0.0.1:<port>/api/manage/shop-order/analysis' \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"refresh":true,"grain":"day"}'

# 原始统计快照
curl -s -X POST 'http://127.0.0.1:<port>/api/manage/shop-order/bizstats/query' \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"code":"order.by_day_product","refresh":true}'
```

约定：**每个业务服务**应提供 Manage 路径 `/api/manage/{service}/analysis`，响应 `stats.Dashboard`。Admin 通过服务选择器切换数据源。

维度展示：`DisplayFields` 为空且配置了 `BaseModel` 时默认 `Name`；07 商品主数据在 supplier，故商品/供应商用 `DisplayFromFact` 读订单快照列。

### 服务级报表（与 Dashboard 分离）

报表挂在**服务子菜单**下（可多报表），路径 `/report/{service}/{code}`，不占用全局「分析页」。

| 接口 | 说明 |
| --- | --- |
| `POST /api/manage/shop-order/reports` | 报表目录（菜单用） |
| `POST /api/manage/shop-order/reports/view` | 单报表数据（图/表/排名/钻取/跳转） |

预置报表：

| code | 菜单 | 数据 Spec |
| --- | --- | --- |
| `order-daily` | 日趋势报表 | `order.by_day` |
| `order-by-product` | 商品销售 | `order.by_day_product` |
| `order-by-supplier` | 供应商月报 | `order.by_month_supplier` |
| `order-product-share` | 商品占比 | `order.by_day_product`（饼图） |

前端：侧栏「订单服务 → 报表 → …」；支持刷新、钻取到下一报表、跳转 Manage 订单列表。

### 统计引擎（OLTP → ClickHouse）

同一 `StatSpec` 可切换后端，API/Store 不变：

| 环境变量 | 含义 | 默认 |
| --- | --- | --- |
| `CORE_STATS_ENGINE` | `oltp` 或 `clickhouse` | `oltp` |
| `CORE_STATS_FALLBACK_OLTP` | CH 失败时是否回退 OLTP | `true` |
| `CORE_STATS_AUTO_ENSURE` | 刷新前是否 Ensure（CH 建 MV） | `true` |

- **OLTP（默认）**：`stats.OLTPEngine` 扫权威库聚合。  
- **ClickHouse**：`stats.CompileClickHouse(spec)` → `BusinessDimensionConfig` / MV DDL；`ClickHouseEngine.Ensure` 调现有 `SaveBusinessViewConfig` + `CreateBusinessViewFromConfig`；`Refresh` 读 MV。  
- **事实表入库**（OLTP→CH）不在引擎内，由 CDC/批量 Ingest 负责；未入库时 CH Refresh 失败可回退 OLTP。

预览全部 MV 计划（无网络）：

```go
plans, _ := stats.CompileAllClickHouse(stats.CompileClickHouseOptions{SourceTable: "order"})
```

## 缓存规则

缓存只放在面向外部流量的入口服务 facade：

- `shop-user` 查询供应商和产品可缓存，并通过 Supplier/Product 事件主动失效。
- `shop-user` 查询支付类型可缓存，并通过 PaymentType 事件主动失效。
- `shop-user` 查询本人订单可短 TTL 缓存，并通过订单/支付事件按 UserID 失效。
- `shop-supplier` 和 `shop-order` 作为内部权威服务，其受限 Public API 不重复 `UseCache`。

## 测试要求

- 单进程集成测试验证 07 的业务语义、规则配置、pending 同步和事件投递。
- 多进程 UAT 必须按角色拆分：买家、供应商、管理员分别拥有可单独运行的闭环测试。
- 角色测试需要其他角色拥有的关键数据时，只能调用对应角色 fixture 准备，例如买家下单前组合管理员基础条件和供应商商品，不在买家测试中隐藏创建逻辑。
- 多 order 副本 UAT 必须验证自动 MachineID、唯一 ServiceInstanceID、本地 pending 目录隔离、共享 MySQL 远程权威库和 resolver 多节点调用。
- 依赖远程 MySQL 权威库的测试必须显式设置 `CORE_TEST_MYSQL=1`；未设置时普通全仓测试不会探测开发机的 `127.0.0.1:3306`。启用后 MySQL 不可达、凭据错误或建表失败都会直接令测试失败。
- Docker 多进程 UAT 默认由 `SHOP_RUN_DOCKER_UAT=1` 显式启用；CI 可把这些测试放到夜间或外部依赖 job，普通单元测试只保留 compose/config 静态约束。
- 有 WebSocket 的买家角色必须覆盖真实订阅、订单事件投递和其他买家隔离。
- 07 完成后增加基准性能测试，对比 06 与 07 在订单写入吞吐、延迟分位数和失败恢复上的差异。

使用 `docker-compose.integration.yml` 的 MySQL 运行远程权威库测试：

```bash
CORE_TEST_MYSQL=1 \
SHOP_ORDER_REMOTE_MYSQL_HOST=127.0.0.1 \
SHOP_ORDER_REMOTE_MYSQL_PORT=13306 \
SHOP_ORDER_REMOTE_MYSQL_USER=core_test \
SHOP_ORDER_REMOTE_MYSQL_PASSWORD=core_test_password \
SHOP_ORDER_REMOTE_MYSQL_DATABASE=core_test \
GOCACHE=/private/tmp/core-codex-gocache \
rtk proxy go test ./examples/07-shop-order-scale/order-service/... \
  ./examples/integration/07-shop-order-scale -count=1
```

## 基准测试

07 提供 06/07 写路径对比 benchmark：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale \
  -run '^$' -bench 'Benchmark(06|07)' -benchtime=10s -count=1 -v
```

对比项：

- `Benchmark06CreateOrderDirect`：06 同步写远程订单权威库和 Outbox。
- `Benchmark07LocalOrderAccept`：07 单副本只写本地 pending 的饱和曲线；这里的并发数不是副本数。
- `Benchmark07HorizontalOrderAccept`：创建 `1/2/4` 个独立 MachineID、独立 Badger 路径的副本，请求轮询分发，报告聚合吞吐和单副本平均吞吐。可通过 `SHOP_BENCH_REPLICAS` 覆盖副本矩阵，`SHOP_BENCH_CONCURRENCIES` 表示每副本并发。
- `Benchmark07DrainPendingOnce`：07 后台同步 pending 到 MySQL 远程权威库。

水平扩容 benchmark 的多个 Badger 目录在逻辑和文件路径上完全隔离，但同机运行时仍共享宿主机 CPU 和物理磁盘，因此结果只用于同机同次趋势评估，不作为固定提升倍数门禁；真实容量评估仍需要按部署拓扑、独立卷、远程库和 MQ 配置重新压测。

真实三进程写入压测使用 `examples/integration/07-shop-order-scale-multi-process` 的 `TestStableOrderWriteLoad`，默认跳过。 `CORE_TEST_LOAD_SECONDS` 可设为 1–600，省略为 30 秒；`CORE_TEST_LOAD_EVIDENCE_DIR` 必须是每轮独立的绝对路径，显式压测将本地身份与 pending 保留在其 `state-*` 子目录，测试结束不自动删除。它保留 HTTP、mTLS、远程幂等探测、本地可靠写、共享 MySQL、Outbox 与 Redis 消费 ACK，并逐笔核对已确认订单；必须显式配置专属依赖与证据目录。历史复现命令、2000 次/秒基线及三层积压见 [压测记录](../../docs/codex/ORDER_WRITE_LOAD_2026-10-18.md)，本次优化及回归结果见 [实施审计](../../docs/codex/TRANSACTIONAL_MESSAGE_PLAN_REVIEW_2026-10-18.md)。

独立的原生 go-zero 下单实现放在 [go-zero-reference](go-zero-reference/README.md)，应用不调用 Core 封装；使用 `TestNativeGoZeroOrderWriteLoad` 复用同一负载与数据核对。它比较当前下单及订单创建事件闭环，不替代整个 07 的功能；实现差异与容量证据见 [对照报告](../../docs/codex/GO_ZERO_ORDER_COMPARISON_2026-10-18.md)。

## 质量门禁

- 禁止新增 `api/call`。
- 禁止内部权威 Public API 重复入口 facade 缓存。
- 发布使用 `sc.UseOutboxWithOptions`，显式设置 4 个 key 并发；订阅使用 `sc.SubscribeEvent`，显式设置 8 个 key 并发。创建、状态和支付事件统一按 OrderID 分键，同 key 串行。多个 publisher 的全局顺序不由该设置保证。07 `OutboxStore` 通过 Core `message.Run` / `ConfirmPublishedWith` 在当前 MySQL 事务中锁定完整 ID 集合、批量确认发布状态，不自写发布循环、不等待所有消费服务完成。
- 用户 facade 只做缓存失效和 WebSocket 通知，`sync.Map` 去重仅在进程内有效；重启后可能重复通知，也不提供离线用户必达。不能把此处 ACK 或缓存/WebSocket 副作用宣称为持久事务型 Inbox。数据库业务的消费幂等接入参考 06 Supplier 的 `message.ConsumeInboxWith`，记录复用方式仍由具体项目与使用者确认。
- 禁止固定水平副本 MachineID；必须使用 `AutoMachineID=true` 并测试自动分配。
- 禁止把最终业务库按技术副本拆分；只允许按业务域拆库。
- 禁止用每进程 SQLite 冒充共享远程权威库；07 order 权威库必须是 MySQL 等真实共享网络数据库。

## 稳定订单副本

bootstrap 可通过 SHOP_IDENTITY_ROOT、SHOP_GENERATOR_REDIS_ADDR、SHOP_GENERATOR_NAMESPACE、SHOP_GENERATOR_EPOCH 显式启用 stable；每个副本使用独立持久 root 和实例 ID，并保留原 SHOP_LOCAL_PENDING_DIR。SHOP_REDIS_DISCOVERY_PREFIX 可用于隔离演示。初始化及历史编号/时间上界迁移见 [指南](../../docs/codex/STABLE_INSTANCE_IDENTITY_GUIDE.md)。不能复制身份卷扩容，或通过 TTL 回收编号。

`TestStableNativeOrderReplicas` 使用专用 Redis/MySQL 启动真实四进程，验证双 order 独立编号、下单幂等、共享权威订单查询和原卷重启。现有 Compose 默认保留 legacy，不能把此原生进程测试称为 Compose stable 部署验收。


2026-10-18 的 `5b04799` 验证了事务消息接入、重复确认计数与自动恢复；六轮累计 805096 个确认请求逐键匹配且最终队列排空。持续 2000/s 的 600 秒目标仍未达标，不能将示例参数当作生产容量承诺；详细失败结果与建议见上述实施审计。
