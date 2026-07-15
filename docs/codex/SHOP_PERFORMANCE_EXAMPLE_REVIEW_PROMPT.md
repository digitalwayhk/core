# 示例 4 性能保护与同步正确性外部只读审查提示词

请只读审查示例 4 的性能保护、SharedBadger 删除/同步正确性和基准可观测性，不要修改代码。

## 审查范围

```bash
git diff 52633dc..4355984
```

- 基线：`52633dc`
- 实现提交：`4355984 perf: 补全商城写入性能保护`
- 设计：`docs/superpowers/specs/2026-07-15-shop-performance-example-design.md`
- 使用指南：`examples/04-shop-performance/README.md`

工作区存在其他未提交历史修改；只审查上述提交范围，不要把脏工作区计入结论。

## 上轮阻断项

1. `ForceDeleteLocal` 与后台 `syncBatch` 存在 TOCTOU：同步已读取内存快照后，本地删除可先删 SQLite，迟到 insert 再把订单复活。
2. Group Commit 队列已满时，`Submit` 持有 `stateMu.RLock` 阻塞发送，`Close` 可长时间无法获得写锁。

本轮还处理了上轮 P2：`PendingByUser` 全表扫描、损坏 value 时 pending 计数不减、Manage 已提交后失效失败误报业务失败，并补充背压、同步 TPS、窗口稳定度和混合长稳入口。

## 重点文件

- `pkg/persistence/database/nosql/sharedbadger.go`
- `pkg/persistence/database/nosql/badgerdb.go`
- `pkg/persistence/types/interface.go`
- `examples/04-shop-performance/models/order.go`
- `examples/04-shop-performance/models/order_batcher.go`
- `examples/04-shop-performance/models/order_write_guard.go`
- `examples/04-shop-performance/models/order_write_store.go`
- `examples/04-shop-performance/api/manage/cache_invalidation.go`
- `examples/integration/benchmetrics/throughput.go`
- `examples/integration/03-shop-inheritance/benchmark_test.go`
- `examples/integration/04-shop-performance/benchmark_test.go`
- `examples/integration/helpers.go`
- `scripts/benchmark-shop-examples.sh`

## 必查事项

### 1. 删除与在途同步

1. 按键分片锁是否真正覆盖“重新校验 Badger -> 远端事务 -> 本地确认”全过程。
2. 删除先取锁时，同步是否在锁内发现键已不存在并跳过远端 insert。
3. 同步先取锁时，`ForceDeleteLocal` 是否等待同步提交，使业务随后的 SQLite Delete 最终生效。
4. 多键批次按分片序号加锁是否无死锁；哈希碰撞是否只影响并发度而不影响正确性。
5. `fromSyncQueue` 仅为内存来源标记，是否不被 JSON 持久化，且不会让生产快照绕过重检。
6. 确定性交错测试是否在修复前真会失败，修复后无 sleep 刷绿。

### 2. Group Commit 关闭协议

1. `closing + submitters WaitGroup + requests close` 的顺序是否避免 send-on-closed、WaitGroup Add/Wait 竞态和丢失已接收请求。
2. 满队列 Submit 是否不再长时间持锁，`Close` 是否能唤醒未入队请求。
3. 已入队请求是否仍等待 `BatchInsert` 真实结果，没有退化成“入队即成功”。
4. panic/提交错误是否传递给整批；重复 Close 是否幂等。
5. 合批测试是否通过先填满队列再启 worker 确定性验证，不依赖调度运气。

### 3. 本地用户前缀键

1. 新增 `ILocalRowCode` 是否只改变 Badger 本地键，`Order.GetHash()`、SQLite Hashcode 和订单 ID 契约是否仍稳定。
2. `Order:u:<sha256-128>:<orderID>` 是否不暴露原始 UserID，且所有 Set/Get/Delete/同步确认路径使用一致键。
3. `PendingByUser` 是否真正只扫描当前 Token 用户前缀，不存在跨用户越权或全局 O(N) 回退。
4. 新增公共接口是否向后兼容，是否需要兼容登记或额外契约测试。

### 4. 背压与磁盘保护

1. 是否复用 go-zero `syncx.TimeoutLimit`，单实例在途写入上限 500、等待上限 2 秒是否真正生效。
2. pending 10,000 软阈值是否只启动 30 秒计时；降到阈值以下是否自动恢复。
3. pending 50,000 或 Badger 1 GiB 是否 fail closed，错误是否不泄露路径、UserID 或订单数据。
4. 高频背压是否使用 O(1) 缓存 pending，没有在每次 Add 扫描队列。
5. 磁盘监视 goroutine 是否在 Close 时稳定退出，无 close panic、泄漏或数据竞争。

### 5. 指标语义

1. Group Commit 的 submitted/committed/batches/immediate/aggregated/failures/max batch/max queue/耗时是否在正确时机记录。
2. SharedBadger `SyncMetrics` 的 attempts/failures/synced items/max pending/耗时/最后成功时间是否无重复或遗漏。
3. `APIConfirmedTPS`、`SQLiteConvergenceTPS`、`SQLiteActiveSyncTPS` 的分母和命名是否准确，是否可能误导运维。
4. 指标读写是否在 `-race` 下无竞争，是否不给写入热路径增加高成本扫描或全局锁。
5. `ForceDeleteLocal` 在 value 损坏时是否以同步索引为 pending 权威事实并正确扣减计数。

### 6. 吞吐窗口与混合基准

1. `benchmetrics.Collector` 请求热路径是否只做原子计数，Stop 是否必然结束 goroutine。
2. `win-p01/p05/p50/p95/p99` 是否表示每秒吞吐窗口分布，而非请求延迟分位数。
3. 尾部不完整窗口、CV、标准差和错误率计算是否正确；小样本是否会给出误导数字。
4. 03/04 `BenchmarkMixedWorkload` 是否严格使用相同 70% 商品查询、20% 订单查询、10% 下单口径。
5. benchmark 临时把日志调为 `error` 是否只作用于临时进程，03/04 是否公平，普通集成测试是否仍保留日志。
6. `scripts/benchmark-shop-examples.sh` 是否包含 Mixed 且仍只读源码/生成临时产物。

### 7. Manage 失效和文档

1. 数据持久化成功后 EventBridge 失效失败是否记录结构化错误，但不再把已成功写入伪装成业务失败。
2. 公开路由缓存是否仍先立即失效；失效失败后是否有 TTL 兜底。
3. README 是否说清背压数字是示例值、历史高并发报告是改造前样本，且没有把 API TPS 冒充 SQLite TPS。
4. 15 分钟长稳和 pprof 命令是否可执行、不进日常 CI。

## 已执行验证

```bash
go test ./pkg/persistence/database/nosql -count=1
go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/integration/benchmetrics -count=1 -timeout=5m
go test -race ./examples/integration/04-shop-performance -count=1 -timeout=20m
go vet ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/benchmetrics ./examples/integration/03-shop-inheritance \
  ./examples/integration/04-shop-performance
go test ./internal/compat ./pkg/persistence/types -count=1
bash -n scripts/benchmark-shop-examples.sh
./scripts/check-logging.sh
```

短混合基准冒烟（Apple M3 Max，100 并发，2s，只用于验证指标输出）：

- 示例 3：约 3,424 req/s，窗口 CV 23.4%，错误率 0。
- 示例 4：约 10,461 req/s，窗口 CV 0.46%，错误率 0。

这两个数字不是正式性能承诺，不应作为固定倍数门禁。

## 建议复核命令

```bash
go test ./pkg/persistence/database/nosql \
  -run 'TestForceDeleteLocal|TestSyncMetrics' -count=20
go test ./examples/04-shop-performance/models \
  -run 'TestOrderBatcher|TestOrderWriteGuard|TestOrderWriteStore' -count=20
go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... -count=1 -timeout=10m
go test -race ./examples/integration/04-shop-performance -count=1 -timeout=20m
go test ./examples/integration/benchmetrics -count=50
go vet ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/03-shop-inheritance ./examples/integration/04-shop-performance
./scripts/check-logging.sh
```

## 输出格式

1. `Findings`，按 P0/P1/P2 排序；每项给出文件/行号、触发场景、影响和修复建议。
2. 明确裁定上轮 P1-1（删除复活）和 P1-2（满队列关闭）是否分别关闭。
3. 评估 `ILocalRowCode`、`SyncMetrics`、`GetCachedPendingSyncCount` 新增公共 API 的兼容性。
4. 评估背压、指标、混合基准和中文文档是否达到演示目标。
5. 列出测试缺口和残余风险。
6. 最终裁定：`APPROVED` 或 `CHANGES_REQUIRED`。
7. 是否允许关闭示例 4 性能优化并进入下一个示例。
