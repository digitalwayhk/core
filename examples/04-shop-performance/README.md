# 商城性能优化示例

`04-shop-performance` 完整保留示例 3 的模型继承、Manage 继承、商品、供应商、订单、支付和 WebSocket 契约，集中演示两类可验证的性能优化：查询分层缓存与订单本地写后同步。

## 查询缓存

- `GetProducts`、`GetSuppliers`、`GetPaymentTypes` 使用 30 秒缓存。
- `GetOrders` 使用 10 秒缓存，缓存键只取 Token 解析出的可信用户 ID 摘要。
- 本地 L1 与 Badger L2 由 `RouteCache` 统一管理；同键冷加载通过可选 SingleFlight 合并。
- 商品、供应商、支付类型和订单状态变化成功后主动失效相关缓存，TTL 只负责兜底。
- 集成测试先让框架生成配置，再在临时目录启用 local L1/L2 并重启，不提交运行时配置文件。

## 下单热路径

示例 4 在不降低持久化可靠性的前提下，组合两项优化：

1. `OrderReferenceCache` 缓存下单所需的商品、供应商和价格快照；同一商品冷加载使用 go-zero `SingleFlight` 合并。商品或供应商变更后，由 `ServiceEventBridge` 发布控制事件并清理缓存，不依赖 TTL 猜测数据是否过期。
2. `OrderWriteStore` 通过原子指针提供服务启动后的热路径，避免每个下单请求竞争全局初始化锁。

事实缓存只保存构造订单所需的最小不可变快照，不缓存请求、用户、响应或完整持久化模型。EventBridge 默认只在当前服务内投递；未来水平扩展时，应显式配置可靠外发控制事件。

## 可靠 Group Commit 与写后同步

```text
AddOrder
  -> 本地下单事实缓存（冷加载才查询 SQLite）
  -> 低积压：单笔立即写入 Badger
  -> 高积压：最多 1ms / 128 笔合并为一个 Badger 事务
  -> 请求等待本批 Badger SyncWrites 成功
  -> 立即返回 DTO 并推送 WebSocket
  -> 后台批量写入 SQLite
  -> 同步确认后自动删除 Badger 副本
```

这里的 Group Commit 不是“内存入队即成功”。每个请求只有在所属批次完成 Badger `SyncWrites` 后才返回，所以已确认订单在进程异常退出后仍可恢复。队列积压少于 16 笔时逐单立即提交，避免低并发固定等待；达到阈值后才短暂聚合，以减少高并发下的 fsync 次数。

订单键为 `Order:<orderID>`，订单 ID 由框架请求上下文生成；查询 Badger 全部待同步项后再按可信 UserID 过滤。查询合并 SQLite 与 Badger，支付、删除和撤销在进入 SQLite 事务前按需汇合目标订单。Badger 写入失败、批次提交失败或写后同步初始化失败时 fail closed，不以内存成功冒充持久化成功。

`ShopService.Start/Stop` 管理订单存储生命周期。服务强制终止后，同一运行目录重启会恢复同步队列；优雅关闭会尝试冲刷积压并返回可观察错误。

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
