# 示例 4 性能优化实现外部只读审查提示词

请只读审查商城性能优化示例及 RouterInfo 缓存加载合并实现，不要修改代码。

## 审查范围

```bash
git diff aff21d0..7b50d05
```

- 设计规格：`docs/superpowers/specs/2026-07-15-shop-performance-example-design.md`
- 设计提交：`aff21d0`
- 实现提交：`7b50d05`
- 重点目录：
  - `examples/04-shop-performance`
  - `examples/integration/04-shop-performance`
  - `examples/integration/03-shop-inheritance/benchmark_test.go`
  - `examples/integration/helpers.go`
  - `pkg/server/routecache`
  - `pkg/server/types/routerinfo.go`
  - `pkg/server/types/route_runtime.go`
  - `scripts/benchmark-shop-examples.sh`

工作区可能存在范围外历史修改。只审查上述提交范围，不要把未提交或范围外文件计入本任务结论。

## 必查事项

### 1. RouterInfo 与 SingleFlight

1. `RouteCacheTakeRuntime` 是否保持可选兼容，不强迫现有自定义 `RouteCacheRuntime` 修改。
2. 同一缓存键冷加载是否只执行一次 `Do`，不同键不互相阻塞。
3. loader 业务错误是否原样返回且不缓存。
4. L1/L2 读写故障是否 best-effort 旁路，不把成功业务响应改成失败。
5. 等待请求是否仍使用自己的 Request、Response、trace 与观察通知。

### 2. 查询缓存与失效

1. Public 三个查询键是否覆盖全部筛选字段且字段边界无歧义。
2. `GetOrders` 缓存键是否只来自 Token 解析后的可信 UserID，且不暴露原始 ID。
3. 商品、供应商、支付类型 Manage 增删改和启停是否只在成功后失效正确路由。
4. 订单新增、删除、支付、撤销与后台支付命令是否只清理对应用户键。
5. L1/L2 返回值是否保持 `json.RawMessage` 响应语义。
6. 集成测试是否真实完成首次生成配置、启用 local L1/L2、同目录重启。

### 3. OrderWriteStore

1. Badger 是否使用 `SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicy=fail` 且 write-behind 无 TTL。
2. `Order:<hash(userID)>:<productID>:<UTC 秒>` 是否支持用户前缀扫描和秒级唯一性，且不泄露 UserID。
3. 下单返回成功前是否已完成 Badger 持久写入和同步队列登记。
4. SQLite 与 Badger 合并读是否按 OrderID 去重、Badger 过渡版本优先并稳定倒序。
5. 支付、删除、撤销是否在 SQLite 未命中时确认本人 pending，再串行 Flush 后进入原事务。
6. SQLite 同步成功后的 `ISyncAfterDelete` 清理，以及业务删除后的本地副本清理，是否会误删更新版本或复活旧订单。
7. 同步失败是否保留 pending；初始化失败是否在同一生命周期 fail closed；Stop 是否返回关闭/积压错误。
8. 强杀后同目录重启是否真实恢复同步队列，而非测试重新创建订单。
9. `IDataAction.Clone()` 的使用是否消除了共享 SQLite adapter 数据竞争，同时仍共享底层连接池。

### 4. 兼容性与生命周期

1. 示例 3 业务实现是否保持不变，只增加 benchmark 和通用测试夹具兼容能力。
2. `IService.Start/Stop` 是否正确拥有订单写后同步生命周期。
3. 进程停止、强杀、重启辅助方法是否会泄漏子进程、文件描述符或误删临时目录。
4. API/JSON/认证/用户所有权/支付状态机/WebSocket 契约是否与示例 3 一致。

### 5. Benchmark 真实性

1. 03/04 是否使用同名接口、相同夹具、完整 HTTP 响应读取和相同并发矩阵。
2. benchmark 子进程是否关闭 race，而功能验收仍使用 race。
3. AddOrder 是否用独立 Token 避免秒级唯一约束造成假失败。
4. `req/s`、`orders/s`、P50、P95、B/op、allocs/op 是否由计时内真实请求产生。
5. 对比脚本是否保留原始输出、生成中文报告、不修改基线且不设置固定提升倍数门禁。

## 验证命令

```bash
go test -race ./examples/04-shop-performance/... -count=1
go test -race ./examples/integration/04-shop-performance -count=1 -timeout=20m
go test -race ./pkg/server/routecache ./pkg/server/types -count=1
go vet ./examples/04-shop-performance/... ./examples/integration/04-shop-performance
./scripts/check-logging.sh

SHOP_BENCHTIME=1x BENCHMARK_ARTIFACT_DIR=/tmp/shop-benchmark-review \
  ./scripts/benchmark-shop-examples.sh
```

## 输出格式

请输出：

1. `Findings`，按 P0/P1/P2 排序，每项提供文件和行号、触发场景、影响及修复建议。
2. RouterInfo SingleFlight、L1/L2 主动失效、订单 write-behind、崩溃恢复是否分别关闭设计目标。
3. API、JSON、认证、状态机、WebSocket 与示例 3 的兼容性评估。
4. benchmark 是否公平、可复现，是否存在误导性指标。
5. 测试缺口和残余风险。
6. 最终裁定：`APPROVED` 或 `CHANGES_REQUIRED`。
7. 是否允许关闭示例 4 并进入下一个示例。
