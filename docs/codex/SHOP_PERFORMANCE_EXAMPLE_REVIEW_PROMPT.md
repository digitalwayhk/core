# 示例 4 下单性能优化外部只读审查提示词

请只读审查示例 4 的下单热路径优化，不要修改代码。

## 审查范围

```bash
git diff 1e1d6d6..f68c874
```

- 基线提交：`1e1d6d6`
- 性能实现提交：`43962b0`
- RouterInfo 注册前置：`7435bc9`
- Manage 路由并发修复：`4a19517`
- 示例 1 订阅路由并发修复：`f68c874`
- 设计规格：`docs/superpowers/specs/2026-07-15-shop-performance-example-design.md`
- 重点文件：
  - `examples/04-shop-performance/models/order_batcher.go`
  - `examples/04-shop-performance/models/order_write_store.go`
  - `examples/04-shop-performance/business/order_reference_cache.go`
  - `examples/04-shop-performance/business/order.go`
  - `examples/04-shop-performance/service.go`
  - `examples/04-shop-performance/api/manage/productmanage.go`
  - `examples/04-shop-performance/api/manage/suppliermanage.go`
  - `pkg/persistence/database/nosql/sharedbadger.go`
  - `pkg/server/router/routerinfooption.go`
  - `pkg/server/router/routerinforegistry.go`
  - `pkg/server/router/servicerouter.go`
  - `pkg/server/types/routerinfo.go`

工作区可能存在范围外历史修改。只审查上述提交范围，不要把未提交文件计入本任务结论。

`7435bc9..f68c874` 是示例 4 在 `7b50d05` 中已引用、但此前遗漏提交的 RouterInfo 冻结前置。请同时确认这些前置提交只完成注册期 option、单例解析、注销和并发只读契约，没有把 RouterInfo 对象池误当成并发限制。

## 优化目标

1. 缓存下单所需的最小商品、供应商和价格事实，避免每次下单重复读取 SQLite。
2. 同一商品冷加载使用 go-zero `syncx.SingleFlight` 合并。
3. 商品或供应商变更后，只通过服务专属 `EventBridge` 控制事件清理事实缓存。
4. 服务启动后通过原子指针取得长期存活的 `OrderWriteStore`，移除请求热路径的全局锁竞争。
5. 保持 Badger `SyncWrites=true`，低积压立即提交；高积压把最多 128 笔、最多 1ms 内的订单合并为一个可靠事务。
6. 请求必须等待所属批次 Badger 提交成功，不能以内存入队冒充持久化成功。

## 必查事项

### 1. 可靠 Group Commit

1. `Submit` 是否只有在所属 `BatchInsert` 成功后才返回成功。
2. 队列关闭、并发 Submit、通道满、重复 Close 时是否可能 panic、死锁、丢请求或泄漏 goroutine。
3. 低于 16 笔积压时是否立即逐单提交，不固定等待 1ms。
4. 达到阈值后是否最多等待 1ms、最多合并 128 笔，且批次顺序稳定。
5. 批次提交错误和 panic 是否通知该批全部等待者；Close 是否排空已接收请求并返回错误。
6. Badger `SyncWrites`、冲突检测、写后同步队列和故障恢复语义是否被保持。
7. `Close` 是否先停止并排空 Group Commit，再 Flush SQLite，最后关闭 Badger。

### 2. 订单键与本地删除

1. `Order:<orderID>` 是否由接口层 `req.NewID()` 提供，零 ID 是否 fail closed。
2. ID 键是否消除了原“用户+商品+秒”进程锁，同时保持 Badger/SQLite 合并去重正确。
3. `PendingByUser` 扫描后是否只按模型中的可信 UserID 返回本人订单，是否有越权风险。
4. `ForceDeleteLocal` 是否在同一 Badger 事务删除数据键与同步索引，并正确维护 pending 计数。
5. 先清本地 pending 再删 SQLite 是否避免删除后订单被合并读复活；失败顺序是否可能造成数据丢失或状态不一致。
6. `ForceDeleteLocal` 与后台同步并发时是否存在 ABA、计数漂移、旧版本同步或误删新版本问题。

### 3. 下单事实缓存

1. 缓存是否只保存最小不可变快照，不保存请求、用户、响应或完整可变 Model。
2. `SingleFlight` 是否只合并同一 ProductID，不同商品不互相阻塞。
3. 商品不存在、商品禁用、供应商不存在或禁用时，错误是否不缓存。
4. Manage 持久化成功后是否通过 `ServiceEventBridge` 控制事件失效；失败路径是否会产生误导性业务结果。
5. generation 是否阻止失效前开始的迟到加载把旧值重新写回；并发 Get/Invalidate 是否有 data race。
6. `Start/Stop` 是否正确绑定 ServiceContext 生命周期，重复启动或停止是否泄漏订阅。
7. 当前 `External=false` 的本地默认语义是否被文档准确说明，是否没有虚假宣称多节点一致。

### 4. 原子热路径与生命周期

1. `activeOrderWriteStore` 是否只在完整初始化成功后发布，并在切换路径或停止前清空。
2. 原子快路径是否可能返回正在关闭、已关闭或属于旧运行目录的 store。
3. 初始化失败是否仍由同一 state 稳定返回，未绕过 fail-closed。
4. 全局互斥锁、store closeMu、batcher stateMu、flushMu 的锁序是否可能形成死锁。
5. 测试是否真实证明热路径不等待 `globalOrderWriteStoreMu`。

### 5. RouterInfo 前置完整性

1. `DefaultRouterInfoWithOptions` / `NewRouterInfoWithOptions` 是否只在首次注册冻结前应用 option；再次解析同一 owner 时是否只读。
2. 注册索引是否以服务所有权隔离，歧义解析是否 fail closed，ServiceContext 关闭后是否注销。
3. Manage 泛型路由与示例 1 GetOrders 是否不再在每次 `RouterInfo()` 调用时写冻结字段。
4. `TestRouterInfoConcurrentResolveIsReadOnly` 与 `TestManageRouterInfoConcurrentResolveIsReadOnly` 是否能在 `-race` 下真实覆盖并发解析。
5. 兼容导出字段与新增 Getter 是否存在 API、锁序或注册生命周期回归。

### 6. API、可靠性与兼容性

1. 下单成功前是否已经完成可靠本地持久化；进程异常退出后已确认订单是否可恢复。
2. DTO、认证、用户所有权、订单状态机、WebSocket 一次推送语义是否保持。
3. Group Commit 或事实缓存故障是否泄露内部错误、Token、UserID 或订单内容。
4. 新增 `ForceDeleteLocal` 是否是合理的框架公共 API，是否需要额外契约测试或兼容性登记。
5. 纯低并发场景是否因调度策略增加不可接受的尾延迟。

### 7. Benchmark 真实性

同机、同参数、三轮中位数：

| 并发 | 示例 3 | 示例 4 | 提升 |
| ---: | ---: | ---: | ---: |
| 1 | 2,884 orders/s | 6,324 orders/s | 119.3% |
| 16 | 8,275 orders/s | 13,216 orders/s | 59.7% |
| 64 | 8,751 orders/s | 24,866 orders/s | 184.1% |

参数：Apple M3 Max，`-benchtime=5s -count=3`，不使用 race。

请检查：

1. 示例 3 与 4 是否使用相同真实 HTTP 口径、数据准备、客户端连接复用和响应读取。
2. 示例 4 的提升是否主要来自事实缓存、原子快路径和高并发 Group Commit，而非跳过可靠持久化或跳过响应校验。
3. 低/中/高并发行为是否与阈值设计一致，是否存在 benchmark 特化代码。
4. README 是否明确这些数字只是同机样本，不是跨机器承诺。

## 验证命令

```bash
go test -race ./examples/04-shop-performance/... -count=1
go test -race ./examples/integration/04-shop-performance -count=1 -timeout=20m
go test -race ./pkg/server/router ./pkg/server/types -count=1
go vet ./examples/04-shop-performance/... ./examples/integration/04-shop-performance
./scripts/check-logging.sh

go test ./examples/04-shop-performance/models \
  -run 'TestOrderBatcher|TestOrderWriteStoreFastPath' -count=20

go test ./examples/integration/03-shop-inheritance -run '^$' \
  -bench '^BenchmarkAddOrder$' -benchmem -benchtime=5s -count=3 -timeout=20m
go test ./examples/integration/04-shop-performance -run '^$' \
  -bench '^BenchmarkAddOrder$' -benchmem -benchtime=5s -count=3 -timeout=20m
```

## 输出格式

请输出：

1. `Findings`，按 P0/P1/P2 排序；每项提供文件与行号、触发场景、影响和修复建议。
2. 可靠 Group Commit、事实缓存/EventBridge 失效、原子 store 热路径、本地删除是否分别达到目标。
3. API、认证、用户隔离、状态机、WebSocket 和崩溃恢复兼容性评估。
4. benchmark 是否公平、可复现，是否存在牺牲可靠性换吞吐的误导。
5. 测试缺口与残余风险。
6. 最终裁定：`APPROVED` 或 `CHANGES_REQUIRED`。
7. 是否允许关闭本轮示例 4 性能优化。
