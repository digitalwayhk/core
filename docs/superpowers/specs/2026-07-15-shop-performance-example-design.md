# 第四个商城性能优化示例规划

## 1. 文档状态

- 状态：已实现，等待外部只读审查
- 日期：2026-07-15
- 基线应用：`examples/03-shop-inheritance`
- 应用目录：`examples/04-shop-performance`
- 集成测试目录：`examples/integration/04-shop-performance`
- 服务名：`performanceshop`

## 2. 目标

第四个示例完整保留第三个示例的模型继承、Manage 继承、供应商、商品、订单、支付、认证和 WebSocket 行为，只改变可明确验证的性能路径：

1. 使用 RouterInfo 的本地 L1 与 Badger L2 分层缓存优化 Public 查询和本人订单查询。
2. 使用 `PrefixedBadgerDB[Order]` 可靠接收新增订单，再批量异步同步到 SQLite。
3. 保证刚写入 Badger、尚未同步 SQLite 的订单立即可查、可支付、可删除。
4. 使用现有 `ISyncAfterDelete` 在 SQLite 同步成功后自动清理 Badger 订单副本。
5. 补齐 RouterInfo 对 RouteCache SingleFlight 的兼容接入，防止冷缓存并发击穿 SQLite。
6. 在示例 3 和示例 4 中用同名真实 HTTP benchmark 比较查询 QPS、下单确认 TPS 和最终 SQLite 同步 TPS。

本示例必须独立启动、独立测试，不引用示例 3 的业务包。示例 3 保持原样，作为诚实的 SQLite 直读直写基线。

## 3. 范围与非目标

### 3.1 本次范围

- 缓存 `GetProducts`、`GetSuppliers`、`GetPaymentTypes` 和 `GetOrders`。
- 新增订单先持久写入 Badger，再异步批量插入 SQLite。
- 查询合并 SQLite 已同步订单和 Badger 待同步订单。
- 支付、删除、撤销待同步订单前按需刷新写回队列。
- 对成功业务变更主动清理相关缓存，TTL 仅作为兜底。
- 增加缓存隔离、主动失效、重启恢复、同步清理和 SingleFlight 集成测试。
- 为示例 3 和示例 4 增加同口径 benchmark 及中文对比脚本。

### 3.2 非目标

- 不把支付、退款、删除或 Manage 状态命令改成异步事务。
- 不引入 Redis L3、Docker 或新的第三方依赖。
- 不连接 NATS JetStream；本示例只演示当前进程内的可靠本地写回。
- 不增加面向业务用户的缓存或同步管理 API。
- 不使用固定倍数作为测试通过条件。
- 不同时加入 SQLite 索引、HTTP 压缩、分页重构等会干扰对比结论的优化。
- 不把 RouterInfo 对象池容量解释为并发上限。

## 4. 总体架构

```text
HTTP
  |
RouterInfo 请求级路由实例
  |
  +-- Public / GetOrders 查询
  |     |
  |     +-- RouteCache L1 内存
  |     +-- RouteCache L2 Badger
  |     +-- 未命中 -> SingleFlight -> Business -> SQLite/订单合并查询
  |
  +-- AddOrder
        |
        +-- SQLite 校验商品和供应商
        +-- PrefixedBadgerDB 同步持久写入
        +-- 立即返回并推送 WebSocket
        +-- 后台批量写入 SQLite
        +-- 同步成功后删除 Badger 副本
```

RouteCache L2 与订单 write-behind 必须使用不同目录、不同所有者和独立生命周期。前者是可重建缓存，后者在同步前是业务事实数据，不能共用损坏策略或清理逻辑。

依赖方向保持：

```text
Public / Private / Manage API -> business -> models -> IDataAction / OrderWriteStore
```

- API 负责可信身份、请求参数、DTO、通知和成功后的缓存失效。
- business 负责业务校验、所有权、状态机和同步前置条件。
- models 负责 SQLite 查询、Badger 写回组合和持久化，不引用 API 或 DTO。

## 5. RouterInfo 分层缓存

### 5.1 缓存配置

示例使用本地模式。框架首次运行仍先自动生成服务配置，不提交运行时配置文件；随后在生成的 `performanceshop.json` 中显式设置 RouteCache 为 `local`、开启 L2 并重启服务。集成测试必须自动完成“首次生成配置 -> 修改临时配置 -> 重启”的真实流程。

- L1：go-zero 内存缓存。
- L2：框架 `routecache.BadgerL2`。
- L3：关闭。
- Public TTL：30 秒。
- Private `GetOrders` TTL：10 秒。
- TTL 使用框架已有 jitter，避免同批键集中失效。
- L1 容量和 L2 最大空间使用明确的示例配置；L2 路径位于服务运行目录下的独立子目录。

缓存写入失败不能改变成功业务响应；记录稳定结构化日志并按当前 best-effort 语义绕过缓存。

### 5.2 缓存键

四个查询路由实现 `IRouterCacheKey.GetCacheKey()`：

| 路由 | 缓存键维度 |
| --- | --- |
| `GetProducts` | ID、Code、Name、SupplierID、SupplierCode |
| `GetSuppliers` | ID、Code、Name |
| `GetPaymentTypes` | Code、Name |
| `GetOrders` | Token 可信 UserID 的稳定哈希 |

规则：

- Public 参数在 `Parse` 中完成 trim 和既有业务规范化，等价请求生成相同键。
- Private UserID 只来自 `req.GetUser()`，禁止读取 query、JSON body 或客户端自报字段。
- 原始 UserID 不写入 L2 key。
- HTTP 请求级 UserID 在路由实例归池前清理。
- WebSocket 订阅实例不进入对象池，继续使用握手认证后注入的会话身份。
- L1、L2 回填统一保持 `json.RawMessage` 响应语义。

### 5.3 主动失效

| 成功业务变化 | 失效范围 |
| --- | --- |
| 商品增删改、启用、禁用 | `GetProducts` 整条路由 |
| 供应商增删改、启用、禁用 | `GetSuppliers`、`GetProducts` 整条路由 |
| 支付类型增删改、启用、禁用 | `GetPaymentTypes` 整条路由 |
| 用户下单、删除、支付、撤销 | 当前用户的 `GetOrders` 键 |
| 后台确认支付、支付失败、确认退款 | 对应订单用户的 `GetOrders` 键 |

失效操作集中在示例级缓存辅助模块。模型层不得反向引用 API。只有业务持久化成功后才能失效；TTL 只是遗漏失效时的最终兜底。

## 6. 防缓存击穿

`routecache.Manager.Take()` 已持有 go-zero `syncx.SingleFlight`，但当前 RouterInfo 未使用该能力。本次通过可选接口补齐，不修改现有 `RouteCacheRuntime`：

```go
type RouteCacheTakeRuntime interface {
	TakeBestEffort(
		route string,
		source interface{},
		ttl time.Duration,
		loader func() (interface{}, error),
	) (interface{}, error)
}
```

- RouterInfo 检测到可选接口时，合并同一缓存键的并发 loader。
- 自定义运行时未实现时，继续使用原 `Get -> Do -> Set` 路径。
- loader 业务错误正常返回且不缓存。
- 缓存读取和写入错误按 best-effort 处理，不能把成功业务响应改为失败。
- 等待者使用各自 Request、Response、trace 和观察事件，不共享请求级对象。
- 同一批冷缓存请求只执行一次业务 `Do`。
- 命中、未命中、合并等待和真实 loader 次数分别统计。

确定性测试必须阻塞首个 loader，再启动多个同键请求，断言 loader 只执行一次、所有响应一致、不同键不互相阻塞。

## 7. 订单 Write-Behind

### 7.1 本地键与业务唯一性

示例 4 的订单 Badger key 使用：

```text
Order:<hash(userID)>:<productID>:<createdAtUnixSecond>
```

该键同时满足：

- 保留“同一用户、同一商品、同一秒只能下单一次”的业务唯一性。
- 可按用户哈希前缀扫描待同步订单。
- 不在本地键中暴露原始 UserID。
- SQLite `hashcode` 继续提供相同唯一约束。

`CreatedAt` 在构造订单时固定为 UTC 秒级，同一个对象写入 Badger 和 SQLite，不允许同步阶段重新生成时间或哈希。

### 7.2 OrderWriteStore

示例级 `OrderWriteStore` 是订单写回唯一所有者：

- 持有一个 `PrefixedBadgerDB[Order]`。
- 绑定 SQLite `ModelList[Order]` 并调用 `EnableWriteBehind`。
- 提供 `Add`、`PendingByUser`、`FindPendingOwned`、`Flush`、`SyncStatus` 和 `Close`。
- 启动时恢复持久化同步队列。
- 服务关闭时调用有超时的 `CloseWithTimeout`。
- 未同步完成或绑定失败必须返回可判断错误，禁止静默丢数据。
- 不承担商品校验、支付状态机、DTO 或 WebSocket。

订单实现：

```go
func (own *Order) IsSyncAfterDelete() bool { return true }
```

SQLite 同步成功并确认本地版本未变化后，由框架异步删除 Badger 副本。

### 7.3 下单流程

```text
校验 Token 用户
  -> SQLite 查询商品和供应商并确认启用
  -> 构造完整订单及价格、商品、供应商快照
  -> Badger 同步持久写入并登记同步队列
  -> 清理当前用户 GetOrders 缓存
  -> 返回 DTO 并推送一次 WebSocket
  -> 后台批量 Insert SQLite
  -> 同步成功后自动清理 Badger
```

Badger 必须使用可靠写回配置：

- `SyncWrites=true`
- `DetectConflicts=true`
- `CorruptionPolicy=fail`
- write-behind 条目不设置 TTL

Badger 写入失败时下单失败，不允许降级为内存成功。

### 7.4 查询和状态变更

`GetOrders`：

1. 查询 SQLite 中当前用户订单。
2. 按用户哈希前缀扫描 Badger 中待同步订单。
3. 按 OrderID 去重；过渡窗口内 Badger 更新版本优先。
4. 按 ID 和 CreatedAt 稳定倒序。
5. 转换为与示例 3 相同的 DTO。

支付、删除和撤销：

1. 先按可信 UserID 查询 SQLite。
2. SQLite 未找到时检查本人 Badger 待同步订单。
3. Badger 也不存在时统一返回“订单不存在或无权操作”。
4. Badger 存在时调用 `ForceSyncAll()`，然后重试 SQLite。
5. 重试成功后进入示例 3 原有 SQLite 事务和状态机。

`ForceSyncAll()` 会顺带同步当前积压批次，这是本示例明确展示的批量削峰行为。本次不新增框架级单订单同步 API。

## 8. 生命周期、恢复与日志

订单写回使用 `sync.Once` 惰性初始化并持久保存初始化错误；所有依赖该存储的 API 在使用前都经过同一初始化门禁。`ShopService` 同时实现启动和停止生命周期：

- `Start()` 只负责提前触发同一个初始化入口进行预热，因为该 hook 在服务启动后异步执行且不能返回错误。
- 如果请求先于预热到达，则由请求路径完成同一个幂等初始化。
- 初始化失败后所有订单读写必须 fail closed 并返回稳定公开错误；不得因为异步 `Start()` 无返回值而假装写入成功。
- `Stop()` 停止接收新写入，尝试刷新 pending，并关闭订单 Badger。
- 关闭超时或仍有 pending 时记录结构化错误并保留本地目录。
- 重启后自动重建同步索引并继续同步。

日志只记录稳定事件、服务、路由、批次大小、pending 数量、耗时和错误类型；不得记录 Token、原始 UserID、请求体、订单对象或支付数据。

建议事件：

- `order_write_behind_started`
- `order_write_behind_flush_completed`
- `order_write_behind_flush_failed`
- `order_write_behind_close_pending`
- `route_cache_load_coalesced`
- `route_cache_bypassed`

## 9. 集成测试

示例 4 完整复制示例 3 的 Manage、Public、Private 和 WebSocket 行为测试，并增加：

1. Public 规范化参数产生稳定缓存键。
2. GetOrders 按 Token 用户严格隔离。
3. 四个缓存路由的主动失效立即生效。
4. 下单后 SQLite 未同步时 GetOrders 立即可见。
5. 待同步订单可以立即支付和删除。
6. SQLite 与 Badger 重叠窗口不返回重复订单。
7. 同步成功后 Badger 订单最终删除。
8. 同步失败保留 pending，恢复后继续同步。
9. 服务重启后恢复同步队列。
10. 服务关闭时 pending 不静默丢失。
11. WebSocket 在异步写入模式下只向当前用户推送一次。
12. SingleFlight 同键冷缓存只执行一次 loader，不同键互不阻塞。

测试必须使用真实进程、自动生成配置、临时目录、真实 HTTP、内建 TestToken、SQLite、Badger 和真实 WebSocket。业务同步等待使用状态轮询或确定性信号，不用固定 sleep 刷绿。

## 10. 性能基准

### 10.1 对比接口

示例 3 与示例 4 增加同名 benchmark：

```text
BenchmarkGetProducts
BenchmarkGetSuppliers
BenchmarkGetPaymentTypes
BenchmarkGetOrders
BenchmarkAddOrder
```

主对比：

- 示例 3 查询：SQLite 直读。
- 示例 4 查询：预热后的 RouterInfo L1 稳定命中。
- 示例 3 下单：SQLite 提交后确认。
- 示例 4 下单：Badger 持久写入并进入同步队列后确认。

示例 4 另报告：

- Badger L2 组件读取性能。
- 真实进程冷 L1、热 L2 的首次回填延迟。
- pending 最大值。
- SQLite 最终同步条数、耗时和 `sync_orders/s`。

L2 首次回填不得冒充稳定态 HTTP QPS。

### 10.2 公平性规则

- 两个示例使用相同数据规模、筛选参数、Token 数量、商品组合和 HTTP 客户端配置。
- 并发矩阵为 1、`GOMAXPROCS`、`4*GOMAXPROCS`。
- 查询在计时前预热；03 预热 SQLite，04 预热 L1。
- AddOrder 预生成足够的 TestToken 与商品组合，不能因每秒唯一约束产生业务失败。
- HTTP 复用连接；请求体可预编码，但计时内必须发送请求并完整读取响应体。
- 每个 benchmark 使用独立临时目录和数据库。
- 性能进程不使用 `-race` 构建；功能验收仍运行 race。
- benchmark 不设置固定提升倍数，只输出实际数据和提升比例。

统一报告：

- `req/s` 或 `orders/s`
- `ns/op`
- `B/op`
- `allocs/op`
- 固定次数采样得到的 P50/P95
- 04 的 `sync_orders/s` 和最终一致性结果

`scripts/benchmark-shop-examples.sh` 连续运行 03/04 benchmark，保留原始 Go 输出并生成中文 Markdown 对比表。脚本不得修改基线，也不得把性能波动作为普通 CI 失败。

## 11. 其他性能能力取舍

- `WithPoolSize` 只作为热路由容量注释和分配诊断；它控制可保留实例数，不限制并发，也不宣称必然提升 QPS。
- RouteCache 已有 JSON 单次序列化与 `json.RawMessage` 回填，示例直接复用。
- SQLite 索引、分页、HTTP 压缩和 Redis L3 留给后续独立示例，避免混淆本次对比原因。
- 不在业务层重复实现连接池、重试器、队列或 SingleFlight。

## 12. 实施顺序

本示例直接依据本文开发，不再创建拆分式 TDD 计划。

1. 从示例 3 建立独立的示例 4 目录、服务名和集成测试目录。
2. 先补 RouteCache 可选 SingleFlight 接口及确定性框架测试。
3. 为四个查询路由声明缓存、稳定缓存键和统一失效辅助模块。
4. 建立 `OrderWriteStore`、订单用户前缀键和服务生命周期。
5. 改造 AddOrder、GetOrders 及状态变更前按需同步路径。
6. 完成主动失效矩阵和 WebSocket 一次投递约束。
7. 补齐示例 4 功能、恢复、关闭和并发集成测试。
8. 为示例 3、示例 4 增加同名 benchmark 和对比脚本。
9. 运行完整验证并生成外部只读审查提示词。

## 13. 验收

```bash
go test -race ./examples/04-shop-performance/... -count=1
go test -race ./examples/integration/04-shop-performance -count=1 -timeout=20m
go test -race ./pkg/server/routecache ./pkg/server/types -count=1
go vet ./examples/04-shop-performance/... ./examples/integration/04-shop-performance
./scripts/check-logging.sh

go test ./examples/integration/03-shop-inheritance -run '^$' -bench 'Benchmark(Get|Add)' -benchmem
go test ./examples/integration/04-shop-performance -run '^$' -bench 'Benchmark(Get|Add)' -benchmem
./scripts/benchmark-shop-examples.sh
```

完成条件：

- 示例 3 和示例 4 的业务响应、认证、所有权、状态机和 WebSocket 契约一致。
- 示例 4 的缓存隔离、主动失效、SingleFlight 和 write-behind 恢复测试通过。
- Badger 确认的订单最终全部进入 SQLite，且同步成功后本地副本被清理。
- 03/04 benchmark 使用相同口径并可复现输出原始结果和中文对比表。
- 所有聚焦测试、race、vet 和日志门禁通过。
