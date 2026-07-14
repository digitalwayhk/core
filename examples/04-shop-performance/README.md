# 商城性能优化示例

`04-shop-performance` 完整保留示例 3 的模型继承、Manage 继承、商品、供应商、订单、支付和 WebSocket 契约，集中演示两类可验证的性能优化：查询分层缓存与订单本地写后同步。

## 查询缓存

- `GetProducts`、`GetSuppliers`、`GetPaymentTypes` 使用 30 秒缓存。
- `GetOrders` 使用 10 秒缓存，缓存键只取 Token 解析出的可信用户 ID 摘要。
- 本地 L1 与 Badger L2 由 `RouteCache` 统一管理；同键冷加载通过可选 SingleFlight 合并。
- 商品、供应商、支付类型和订单状态变化成功后主动失效相关缓存，TTL 只负责兜底。
- 集成测试先让框架生成配置，再在临时目录启用 local L1/L2 并重启，不提交运行时配置文件。

## 订单写后同步

```text
AddOrder
  -> SQLite 校验商品与供应商
  -> PrefixedBadgerDB 同步持久写入
  -> 立即返回 DTO 并推送 WebSocket
  -> 后台批量写入 SQLite
  -> 同步确认后自动删除 Badger 副本
```

订单键为 `Order:<hash(userID)>:<productID>:<UTC Unix 秒>`，既支持按用户扫描，又不暴露原始用户 ID。查询合并 SQLite 与 Badger，支付、删除和撤销在进入 SQLite 事务前按需汇合目标订单。Badger 写入失败或写后同步初始化失败时 fail closed，不以内存成功冒充持久化成功。

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

完整设计见 `docs/superpowers/specs/2026-07-15-shop-performance-example-design.md`。
