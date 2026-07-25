# 商城性能优化示例

`04-shop-performance` 完整保留示例 3 的模型继承、Manage 继承、商品、供应商、订单、支付和 WebSocket 契约，集中演示两类可验证的性能优化：查询分层缓存与订单本地写后同步。

## 查询缓存

- `GetProducts`、`GetSuppliers`、`GetPaymentTypes` 使用 30 秒缓存。
- `GetOrders` 使用 10 秒缓存，缓存键只取 Token 解析出的可信用户 ID 摘要。
- 路由调用 `UseCache` 后即使用默认 L1，无需修改配置；本示例另外显式启用 Badger L2。同键冷加载通过可选 SingleFlight 合并。
- 商品、供应商、支付类型和订单状态变化成功后主动失效相关缓存，TTL 只负责兜底。
- L1 默认按进程/容器有效内存的 2% 自动解析总字节预算（下限 16 MiB、上限 256 MiB）；超过 `MaxValueBytes` 的响应正常返回但不进入任何缓存层。
- 集成测试先让框架生成配置，再在临时目录显式启用 Badger L2 并重启，不提交运行时配置文件。

## 下单热路径

示例 4 在不降低持久化可靠性的前提下，组合两项优化：

1. `OrderReferenceCache` 缓存下单所需的商品、供应商和价格快照；同一商品冷加载使用 go-zero `SingleFlight` 合并。商品或供应商变更后，由 `ServiceEventBridge` 发布控制事件并清理缓存，不依赖 TTL 猜测数据是否过期。
2. `ShopService` 为当前 `ServiceContext` 创建一个 `OrderWriteRuntime`，所有路由和业务服务显式注入同一实例，不使用包级 store registry。

事实缓存只保存构造订单所需的最小不可变快照，不缓存请求、用户、响应或完整持久化模型。EventBridge 默认只在当前服务内投递；未来水平扩展时，应显式配置可靠外发控制事件。

## 可靠 Group Commit 与写后同步

```text
CreateOrder
  -> 本地下单事实缓存（冷加载才查询 SQLite）
  -> ReliableWriteStore.Save
  -> 最多 1ms / 128 个并发请求合并为 Badger 事务
  -> 请求等待本批 Badger SyncWrites 成功
  -> 立即返回 DTO 并推送 WebSocket
  -> 后台批量写入 SQLite
  -> 同步确认后自动删除 Badger 副本
```

这里的 Group Commit 不是“内存入队即成功”。每个请求只有在所属批次完成 Badger `SyncWrites` 后才返回，所以已确认订单在进程异常退出后仍可恢复。聚合、队列容量、panic 隔离和部分提交前缀统一由框架 `BatchCommitter` 处理。

写后同步通过 `ReliableWriteStore.UseWriteBehind(WriteBehindTarget)` 绑定一次远端汇合目标。04 使用 `ModelListWriteBehindTarget` 兼容 SQLite 示例；Group Commit、背压、pending ACK、磁盘指标与关闭由框架统一处理，业务适配器只保留订单校验、用户前缀查询和 SQLite 可见数据合并。

订单 `GetHash()` 仍是 `orderID`；Badger 通过 `ILocalRowCode` 使用 `Order:u:<UserID摘要>:<orderID>`，因此可直接扫描当前可信用户的 pending 前缀，不再遍历全局积压，也不在磁盘键中暴露原始 UserID。查询合并 SQLite 与 Badger，支付、删除和撤销在进入 SQLite 事务前按需汇合目标订单。

删除先可靠写入 tombstone，再执行有界 `ForceSyncAll`；成功响应表示 SQLite 已确认删除。物理本地清理由框架 ACK 与 `IsSyncAfterDelete` 完成，业务不调用 `PurgeLocal`。

## 容量保护与可观测性

- 框架 `WriteAdmissionController` 内部使用 go-zero `syncx.TimeoutLimit`，保护单实例最多 500 个在途订单写入；超出部分最多等待 2 秒。
- pending 软/硬阈值是 10,000/50,000；软阈值持续 30 秒或达到硬阈值时拒绝新写入。
- 磁盘硬上限是 1 GiB，每次准入直接读取 Badger 原生 LSM + VLog 大小，不再遍历目录。生产项目必须根据磁盘配额重新配置，不应照搬数字。
- `OrderWriteRuntime.Metrics()` 映射框架 Group Commit、同步、pending、磁盘和背压快照，不暴露 store 指针。

`ShopService.Start` 使用 `ServiceName/DataCenterID/MachineID` 解析实例目录并通过 `ServiceContext.UseResource` 注册 store；`Stop` 先 Unbind runtime，随后由框架逆序关闭资源。关闭不访问 SQLite，未汇合 pending 会保留并返回 `PendingSyncError`。

## 运行

```bash
go run ./examples/04-shop-performance/main -view 0
```

服务名为 `performanceshop`。首次运行会自动生成 `server.json` 和 `performanceshop.json`。

## 测试

```bash
go test -race ./examples/04-shop-performance/... -count=1
go test -race ./examples/integration/04-shop-performance -count=1 -timeout=20m
go vet ./examples/04-shop-performance/... ./examples/integration/04-shop-performance
```

集成测试覆盖完整 Manage/Public/Private/WebSocket 行为、缓存主动失效、异步订单立即可见、崩溃恢复、SQLite 最终落库和 Badger 自动清理。

## 性能对比

```bash
./scripts/benchmark-shop-examples.sh
```

脚本使用相同数据与真实 HTTP 请求比较示例 3 和示例 4，输出 1、`GOMAXPROCS`、`4*GOMAXPROCS` 三档并发的原始 Go benchmark 和中文 Markdown 表。结果只用于同机同次分析，不设固定提升倍数门禁。

在 Apple M3 Max、`-benchtime=5s -count=3` 的一次同机中位数对比中，下单 TPS 为：

| 并发 | 示例 3 SQLite | 示例 4 优化后 | 提升 |
| ---: | ---: | ---: | ---: |
| 1 | 3,338 | 6,834 | 104.7% |
| 16 | 8,067 | 13,500 | 67.3% |
| 64 | 8,160 | 24,649 | 202.1% |

该表来自干净提交 `d43467f`，用于解释优化方向，不是跨机器性能承诺。正式比较应在同一提交、同一机器、空闲环境下重新运行三轮以上并取中位数。基准客户端显式复用 HTTP 连接，避免高并发时因临时端口耗尽而得到虚假失败。

完整设计见 `docs/superpowers/specs/2026-07-15-shop-performance-example-design.md`。

100、500、1000 并发下的 QPS、TPS 与 P50/P95/P99 正式结果见 `docs/codex/SHOP_HIGH_CONCURRENCY_BENCHMARK_REPORT.md`。

该报告是容量保护加入前的历史样本，依然可用于解释缓存和 Group Commit 的收益，但不代表新版本的最终数字。

### 吞吐稳定度

benchmark 现在同时输出两类分位数：

- `p50-ns/p95-ns/p99-ns`：单请求延迟分布。
- `win-p01/s` 到 `win-p99/s`：每秒吞吐窗口的分布；`win-cv-pct` 是变异系数，越低说明越稳定。只有收集到至少 30 个完整窗口时才输出这些分布指标；更短的运行只输出 `win-windows`、错误数和错误率。

`BenchmarkMixedWorkload` 使用 70% 商品查询、20% 本人订单查询、10% 下单，03/04 口径一致。每 10 次操作共享同一用户，然后在 128 个预生成用户中轮转，使长稳读成本不再被单用户订单列表快速增长主导。长稳不进入日常 CI：

```bash
SHOP_BENCH_CONCURRENCIES=500 go test ./examples/integration/04-shop-performance \
  -run '^$' -bench '^BenchmarkMixedWorkload$' -benchtime=15m -count=1 -timeout=20m
```

长稳结束后应同时记录性能快照，确认 pending 能排空、SQLite 收敛 TPS 稳定、Badger 磁盘不无界增长、同步失败和背压拒绝符合预期。

### CPU 与内存定位

只有在基准证明分配或某个热点是瓶颈后才继续优化：

```bash
SHOP_BENCH_CONCURRENCIES=500 go test ./examples/integration/04-shop-performance \
  -run '^$' -bench '^BenchmarkAddOrder$' -benchtime=30s -count=1 \
  -cpuprofile /tmp/shop04-cpu.pprof -memprofile /tmp/shop04-mem.pprof -timeout=10m
go tool pprof -http=:0 /tmp/shop04-cpu.pprof
go tool pprof -http=:0 /tmp/shop04-mem.pprof
```

benchmark 子进程会把服务日志级别临时调为 `error`，减少 info 访问日志 I/O 干扰；普通集成测试保持默认日志便于失败诊断。
