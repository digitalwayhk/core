# 示例 4 性能优化 P2 收口外部只读复审提示词

请只读复审示例 4 性能优化的五项 P2 收口，不要修改代码。

## 审查范围

```bash
# 本轮 P2 修复
git diff 4355984..fb01fba

# 必要时核对示例 4 性能优化的完整结果
git diff 52633dc..fb01fba
```

- 上轮实现：`4355984 perf: 补全商城写入性能保护`
- 本轮修复：`fb01fba perf: 收紧商城性能指标口径`
- 设计：`docs/superpowers/specs/2026-07-15-shop-performance-example-design.md`
- 指南：`examples/04-shop-performance/README.md`

工作区存在与本轮无关的未提交修改；只以上述提交范围为审查依据。

## 本轮目标

1. 补齐“删除先完成，旧同步快照后到”的 PrefixedBadgerDB 对称回归测试。
2. Group Commit 指标按真实决策路径区分 `ThresholdImmediateBatches`、`SingletonAggregatedBatches` 和 `AggregatedBatches`，不再仅根据批大小推测。
3. 吞吐窗口少于 30 个样本时不输出 `win-p*`、标准差和 CV，但仍报告 `win-windows` 和错误率。
4. 快照 TPS 重命名为 `LifetimeAPIConfirmedTPS` 和 `LifetimeSQLiteConvergenceTPS`，明确它们是包含启动、空闲的进程生命周期均值。
5. 03/04 混合基准使用相同的 128 用户轮转池，每组 10 个操作共享同一用户，降低单用户列表自然增长对长稳曲线的污染。

## 必查事项

### 1. 删除先于同步

1. `TestForceDeleteLocalBeforeSyncSkipsStaleSnapshot` 是否真正先通过 `getUnsyncedBatch` 取得生产队列快照，再删除本地键，最后调用 `syncBatch`。
2. 测试是否同时断言返回同步键为空、远端无行、Badger 本地键不复活。
3. 测试是否能在删除 `currentSyncItems` 的锁内重检后稳定失败，而不是只验证表面结果。

### 2. Group Commit 指标

1. `finishBatch` 是否由调用方显式传入“队列低于积压阈值”决策，而非用 `len(batch)==1` 代替语义。
2. 阈值立即路径、聚合路径但单条、真实多条合批是否分别计数。
3. 三类批次与 `CommitBatches` 是否可对账，失败批次是否仍保留正确的路径计数。
4. 新字段是否属于未发布示例内口径修正，是否存在需要登记的公共 API 破坏。

### 3. 短样本吞吐分位数

1. `benchmetrics.Report` 是否始终输出窗口数和错误指标，仅在 `Windows >= 30` 时输出分位数、均值、标准差和 CV。
2. `Summarize` 仍可在单元测试中计算确定分布，报告层的门槛是否不会损失原始数据。
3. 脚本和 README 是否说清默认 `1s` 只是烟测，稳定度至少需要 30 个窗口或更长多轮运行。

### 4. 生命周期 TPS

1. `LifetimeAPIConfirmedTPS` 是否为 `CommittedOrders / uptime`。
2. `LifetimeSQLiteConvergenceTPS` 是否为 `SyncedItems / uptime`。
3. `SQLiteActiveSyncTPS` 是否仍为 `SyncedItems / TotalDuration`，与墙钟均值明确分离。
4. 代码注释、设计文档和 README 是否没有把生命周期均值宣称为当前瞬时 TPS。

### 5. 轮转用户池与基准公平性

1. `TokenPoolFor` 是否在 `b.ResetTimer` 之前创建令牌，不把 TestToken 开销计入业务吞吐。
2. `RotatingSlot(index, 10, 128)` 是否使每组一次下单、两次订单查询使用同一个用户，然后稳定轮转。
3. 03/04 是否仍保持完全相同的 70/20/10 比例、用户池大小和映射算法。
4. 并发 worker 执行顺序不确定时，是否只影响同组请求先后，不会越权、失真或产生假绿。
5. 128 用户是否只减缓而非绝对限制长运行数据增长，README 表述是否准确。

## 已执行验证

```bash
go test ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/benchmetrics -count=1
go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/benchmetrics -count=1
go test -race ./examples/integration/03-shop-inheritance \
  ./examples/integration/04-shop-performance -count=1 -timeout=20m
go vet ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/benchmetrics ./examples/integration/03-shop-inheritance \
  ./examples/integration/04-shop-performance
bash -n scripts/benchmark-shop-examples.sh
./scripts/check-logging.sh
```

基准烟测使用单并发、`-benchtime=1x`，03/04 均通过，均输出 `win-windows=0`，未输出 `win-p*`。烟测数值不作为性能倍数承诺。

## 建议复核命令

```bash
go test ./pkg/persistence/database/nosql \
  -run 'TestForceDeleteLocalBeforeSync|TestForceDeleteLocalWaitsForInflightSync' -count=50
go test ./examples/04-shop-performance/models \
  -run 'TestOrderBatcherSnapshot|TestOrderWriteStorePerformance' -count=50
go test ./examples/integration/benchmetrics -count=50
go test -race ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/benchmetrics -count=1
go test -race ./examples/integration/03-shop-inheritance \
  ./examples/integration/04-shop-performance -count=1 -timeout=20m
go vet ./pkg/persistence/database/nosql ./examples/04-shop-performance/... \
  ./examples/integration/03-shop-inheritance ./examples/integration/04-shop-performance
./scripts/check-logging.sh
```

## 要求反馈

1. `Findings`，按 P0/P1/P2 排序；每项给出文件/行号、触发场景、影响和修复建议。
2. 逐项裁定 P2-1 到 P2-5 是否已关闭，不要只给总结论。
3. 评估 Group Commit 指标字段更名和 TPS 字段更名的公共 API 兼容性，是否需要废弃别名或兼容登记。
4. 评估新测试是否确定、修复前可失败、无 sleep/retry 刷绿。
5. 列出仍存在的测试缺口和残余风险。
6. 最终裁定：`APPROVED` 或 `CHANGES_REQUIRED`。
7. 是否允许关闭示例 4 性能优化并进入下一个示例。
