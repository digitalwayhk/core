# PrefixedBadgerDB WriteBehind Target Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Do not use subagents unless the user explicitly re-enables delegation.

**Goal:** 把 `PrefixedBadgerDB` 从 `EnableWriteBehind(ModelList)` 主路径改为“本地可靠队列 + 可插拔 WriteBehindTarget”，并让 04/07 的业务写入层复用框架能力、减少示例业务复杂度。

**Architecture:** `PrefixedBadgerDB` 继续负责 Badger 本地可靠写、pending 队列、批量读取、ACK、重试、指标和关闭恢复；远端同步由新的 `WriteBehindTarget[T]` 处理。`ModelList/IDataAction` 迁移为兼容 target，Manage API 继续使用 `ModelList/IDataAction`，业务热路径优先使用专用 SQL target 或业务 target。

**Tech Stack:** Go、Badger、GORM/MySQL、SQLite、Digitalway Core `IModel`、`IDataAction`、`ModelList`、examples 04/07。

---

### Task 1: 为 PrefixedBadgerDB 增加 WriteBehindTarget 抽象

**Files:**
- Create: `pkg/persistence/database/nosql/writebehind_target.go`
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Test: `pkg/persistence/database/nosql/sharedbadger_writebehind_target_test.go`

- [ ] **Step 1: 写失败测试**

在 `pkg/persistence/database/nosql/sharedbadger_writebehind_target_test.go` 中增加测试：绑定自定义 target 后，`Set` 创建 pending，`ForceSyncAll` 调用 target，target 返回确认 key 后 pending 归零。

- [ ] **Step 2: 定义 target 类型**

新增：

```go
package nosql

import "context"

type WriteBehindTarget[T any] interface {
	SyncBatch(ctx context.Context, items []*SyncQueueItem[T]) (*WriteBehindResult, error)
}

type WriteBehindResult struct {
	ConfirmedKeys []string
	RetryKeys     []string
	DeadKeys      []string
}
```

- [ ] **Step 3: 在 PrefixedBadgerDB 中增加 UseWriteBehind**

在 `PrefixedBadgerDB[T]` 中增加 `syncTarget WriteBehindTarget[T]`，并实现：

```go
func (p *PrefixedBadgerDB[T]) UseWriteBehind(target WriteBehindTarget[T]) error
```

它复用 `EnableWriteBehind` 的安全配置检查：`SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicyFail`。

- [ ] **Step 4: 运行测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestWriteBehindTarget' -count=1 -v
```

Expected: 新 target 测试通过。

---

### Task 2: 把同步执行从 ModelList 分支改为 target 优先

**Files:**
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Test: `pkg/persistence/database/nosql/sharedbadger_writebehind_target_test.go`

- [ ] **Step 1: 写失败测试**

新增测试：同时设置 `syncTarget` 和旧 `syncList` 时，`processSyncQueue` 必须使用 target，不调用 `IDataAction`。

- [ ] **Step 2: 修改 syncBatch**

在 `syncBatch` 或等价同步入口中增加 target 分支：

```go
if target := p.getWriteBehindTarget(); target != nil {
	return p.syncBatchWithTarget(context.Background(), unsyncedItems, target)
}
```

`syncBatchWithTarget` 只负责：

1. 调用 target；
2. 根据 `ConfirmedKeys` 标记 wrapper synced；
3. 删除同步队列索引；
4. 对 `DeadKeys` 记录稳定日志但不静默丢失；
5. 返回已确认 keys。

- [ ] **Step 3: 保留旧 ModelList 路径**

旧 `EnableWriteBehind(list)` 改为内部创建 `ModelListWriteBehindTarget[T]`，不要让主同步逻辑直接依赖 `ModelList`。

- [ ] **Step 4: 运行回归**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -count=1
```

Expected: 旧 write-behind 测试和新 target 测试都通过。

---

### Task 3: 提供 ModelList/IDataAction 兼容 target

**Files:**
- Create: `pkg/persistence/database/nosql/model_list_writebehind_target.go`
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Test: `pkg/persistence/database/nosql/sharedbadger_writebehind_test.go`

- [ ] **Step 1: 新增兼容 target**

实现：

```go
type ModelListWriteBehindTarget[T types.IModel] struct {
	list *entity.ModelList[T]
}
```

它内部复用当前 `batchInsertWithErrorHandling`、`batchUpdateWithErrorHandling`、`batchDeleteWithErrorHandling` 的语义，保持旧行为。

- [ ] **Step 2: 标记旧 API**

`EnableWriteBehind(list *entity.ModelList[T])` 保留，但注释改为：

```go
// Deprecated: 业务热路径请使用 UseWriteBehind(target)。此方法仅为 ModelList/IDataAction 兼容层。
```

`SetSyncDB` 继续 deprecated。

- [ ] **Step 3: 运行兼容测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestEnableWriteBehind|TestLegacySetSyncDB|TestLocal_|TestSync_' -count=1
```

Expected: 原有语义不变。

---

### Task 4: 提供内置 SQL WriteBehindTarget

**Files:**
- Create: `pkg/persistence/database/nosql/sql_writebehind_target.go`
- Test: `pkg/persistence/database/nosql/sql_writebehind_target_test.go`

- [ ] **Step 1: 定义 SQL target 接口**

不要让 `PrefixedBadgerDB` 直接依赖 MySQL 细节；提供一个轻量 store 接口：

```go
type SQLWriteBehindStore[T any] interface {
	UpsertBatch(ctx context.Context, items []*T) ([]*T, error)
	DeleteBatch(ctx context.Context, items []*T) error
}
```

- [ ] **Step 2: 实现 SQLWriteBehindTarget**

`SQLWriteBehindTarget[T]` 把 `SyncQueueItem` 按 operation 分组，调用 store，成功后确认对应 key。

- [ ] **Step 3: 写测试**

用内存 fake store 验证 insert/update/delete 分组和确认 key。

- [ ] **Step 4: 运行测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestSQLWriteBehindTarget' -count=1 -v
```

Expected: PASS。

---

### Task 5: 把 04 的 OrderWriteStore 改为框架 target 写法

**Files:**
- Modify: `examples/04-shop-performance/models/order_write_store.go`
- Modify: `examples/04-shop-performance/models/order_write_store_test.go`
- Test: `examples/integration/04-shop-performance`

- [ ] **Step 1: 写回归测试**

确认 04 仍满足：

1. API 返回前 Badger 已可靠提交；
2. SQLite 同步失败时 pending 保留；
3. `FlushPendingOrder` 能触发汇合；
4. `PerformanceSnapshot` 还能读到 pending 和同步指标。

- [ ] **Step 2: 替换 EnableWriteBehind**

把：

```go
db.EnableWriteBehind(entity.NewModelList[Order](action))
```

改成：

```go
db.UseWriteBehind(nosql.NewModelListWriteBehindTarget(entity.NewModelList[Order](action)))
```

第一阶段只迁移接口，不改变 04 的远端写语义。

- [ ] **Step 3: 删除业务层重复同步状态代码**

04 仍保留 `OrderWriteStore`，但只留下业务入口、背压、指标封装；不要在业务层复制 Badger pending 队列和 ACK 逻辑。

- [ ] **Step 4: 运行测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/... ./examples/integration/04-shop-performance -count=1
```

Expected: PASS。

---

### Task 6: 把 07 的 RemoteOrderSyncer 下沉为 OrderWriteBehindTarget

**Files:**
- Create: `examples/07-shop-order-scale/order-service/business/order_writebehind_target.go`
- Modify: `examples/07-shop-order-scale/order-service/models/transaction/order_write_store.go`
- Modify: `examples/07-shop-order-scale/order-service/business/remote_order_syncer.go`
- Test: `examples/07-shop-order-scale/order-service/business/order_syncer_test.go`

- [ ] **Step 1: 写失败测试**

新增测试：`OrderWriteBehindTarget.SyncBatch` 在一个 MySQL 事务中完成：

1. `UserID+RequestID` 幂等 upsert；
2. `OrderCreated` Outbox 写入；
3. 返回确认的 Badger key；
4. 重复 requestID 返回同一个远端 OrderID，不产生第二条 Outbox。

- [ ] **Step 2: 实现 OrderWriteBehindTarget**

把当前 `RemoteOrderSyncer.syncOne` 的核心逻辑移动到 target：

```go
type OrderWriteBehindTarget struct{}

func (OrderWriteBehindTarget) SyncBatch(ctx context.Context, items []*nosql.SyncQueueItem[models.Order]) (*nosql.WriteBehindResult, error)
```

- [ ] **Step 3: 简化 RemoteOrderSyncer**

`RemoteOrderSyncer.DrainOnce` 改成调用 `PrefixedBadgerDB.ForceSyncBatch(limit)` 或等价公共方法，不再自己读 pending、循环 sync、删本地。

- [ ] **Step 4: 简化 Service pending loop**

`order-service/service.go` 的 pending loop 只负责定时触发 Badger write-behind，并在成功后 `sc.NotifyOutbox()`。

- [ ] **Step 5: 运行 07 测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/... ./examples/integration/07-shop-order-scale -count=1
```

Expected: PASS，本地 MySQL 不可用的 UAT 可按现有逻辑 skip。

---

### Task 7: 更新 07 Docker 多副本 UAT

**Files:**
- Modify: `examples/integration/07-shop-order-scale-multi-process/*.go`
- Test: `examples/integration/07-shop-order-scale-multi-process`

- [ ] **Step 1: 保持角色 UAT 不退化**

确认 buyer/supplier/admin 角色 fixture 仍按角色准备关键数据，不把其他角色造数逻辑写回买家测试。

- [ ] **Step 2: 验证新 write-behind target**

Docker UAT 必须继续验证：

1. 两个 order 副本 discovery 可见；
2. MachineID 不同；
3. ServiceInstanceID 不同；
4. 同 requestID 重试返回同 OrderID；
5. 买家 WebSocket 收到创建/支付/撤单事件；
6. 其他买家收不到事件。

- [ ] **Step 3: 运行 Docker UAT**

Run:

```bash
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^(TestDockerUATBuyerRoleFlow|TestDockerUATSupplierRoleFlow|TestDockerUATAdminRoleFlow|TestDockerComposeOrderScaleUAT)$' -count=1 -v
```

Expected: PASS。

---

### Task 8: 更新文档和能力

**Files:**
- Modify: `.codex/skills/use-digitalway-core/SKILL.md`
- Modify: `examples/04-shop-performance/README.md`
- Modify: `examples/07-shop-order-scale/README.md`
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`

- [ ] **Step 1: 写入新的边界规则**

新增规则：

1. `PrefixedBadgerDB` 是业务热路径可靠写组件；
2. Manage API 继续使用 `ModelList/IDataAction`；
3. 业务高吞吐写入必须使用 `UseWriteBehind(WriteBehindTarget)`；
4. `EnableWriteBehind(ModelList)` 只是兼容层，不作为新业务默认方案；
5. 业务示例不得复制 pending ACK、同步确认和队列恢复逻辑。

- [ ] **Step 2: 更新 04/07 README**

说明 04 使用通用 ModelList 兼容 target，07 使用订单业务 target。

- [ ] **Step 3: 运行文档关键字检查**

Run:

```bash
rtk rg 'EnableWriteBehind\\(|SetSyncDB\\(|UseWriteBehind\\(' .codex docs examples pkg
```

Expected: 新文档不再推荐 `EnableWriteBehind(ModelList)` 给业务热路径。

---

### Task 9: 最终验证和提交

**Files:**
- All modified files.

- [ ] **Step 1: 格式化**

Run:

```bash
rtk gofmt -w pkg/persistence/database/nosql examples/04-shop-performance examples/07-shop-order-scale examples/integration/07-shop-order-scale-multi-process
```

- [ ] **Step 2: 核心测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/... ./examples/integration/04-shop-performance -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/... ./examples/integration/07-shop-order-scale ./examples/integration/07-shop-order-scale-multi-process -count=1
```

- [ ] **Step 3: Docker 真 UAT**

Run:

```bash
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^(TestDockerUATBuyerRoleFlow|TestDockerUATSupplierRoleFlow|TestDockerUATAdminRoleFlow|TestDockerComposeOrderScaleUAT)$' -count=1 -v
```

- [ ] **Step 4: 提交**

Run:

```bash
rtk git status --short
rtk git add pkg/persistence/database/nosql examples/04-shop-performance examples/07-shop-order-scale examples/integration/07-shop-order-scale-multi-process .codex/skills/use-digitalway-core/SKILL.md docs/codex/FRAMEWORK_USAGE_GUIDE.md
rtk git commit -m "feat(persistence): add write-behind target for reliable local writes"
```

---

## Phase 2: 生产级可靠 WriteBehind 增强规划

第二阶段必须在第一阶段接口稳定、04/07 行为保持一致并通过 UAT 后再做。不要把这些增强混进第一阶段，否则会同时改变接口、同步语义和运维语义，风险过大。

### Task 10: RetryPolicy 与退避调度

**Files:**
- Create: `pkg/persistence/database/nosql/writebehind_retry_policy.go`
- Modify: `pkg/persistence/database/nosql/writebehind_target.go`
- Test: `pkg/persistence/database/nosql/writebehind_retry_policy_test.go`

- [ ] **Step 1: 定义 RetryPolicy**

增加：

```go
type WriteBehindRetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	JitterRatio float64
}
```

默认策略：

```go
MaxAttempts: 10
BaseDelay:   200 * time.Millisecond
MaxDelay:    30 * time.Second
JitterRatio: 0.2
```

- [ ] **Step 2: 扩展 SyncQueueItem metadata**

为 pending wrapper 增加或兼容保存：

```go
AttemptCount int
NextAttemptAt time.Time
LastError string
LastAttemptAt time.Time
```

旧数据缺字段时按零值兼容。

- [ ] **Step 3: 修改 pending 扫描**

`getUnsyncedBatch` 只返回 `NextAttemptAt` 为空或小于当前时间的条目。

- [ ] **Step 4: 测试**

验证临时失败后不会立即重试，超过 `NextAttemptAt` 后才重新进入批次。

---

### Task 11: Dead Letter / Poison Pending 隔离

**Files:**
- Create: `pkg/persistence/database/nosql/writebehind_deadletter.go`
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Test: `pkg/persistence/database/nosql/writebehind_deadletter_test.go`

- [ ] **Step 1: 定义 DeadLetter 结构**

```go
type WriteBehindDeadLetter[T any] struct {
	Key          string
	Item         *T
	Operation    string
	AttemptCount int
	LastError    string
	DeadAt       time.Time
}
```

- [ ] **Step 2: 实现 dead-letter 存储**

在 Badger 中使用独立 key 前缀：

```go
__dw_deadletter__:{prefix}:{originalKey}
```

- [ ] **Step 3: target 返回 DeadKeys 时隔离**

`DeadKeys` 不再阻塞后续 pending；框架把对应条目移动到 dead-letter 区，并从 sync queue 移除。

- [ ] **Step 4: 暴露查询和重试方法**

```go
ListDeadLetters(limit int) ([]*WriteBehindDeadLetter[T], error)
RetryDeadLetter(key string) error
DeleteDeadLetter(key string) error
```

- [ ] **Step 5: 测试**

验证一条毒丸进入 dead-letter 后，同批其他条目仍能 ACK。

---

### Task 12: SyncResult 分类和幂等冲突语义

**Files:**
- Modify: `pkg/persistence/database/nosql/writebehind_target.go`
- Modify: `examples/07-shop-order-scale/order-service/business/order_writebehind_target.go`
- Test: `pkg/persistence/database/nosql/writebehind_result_test.go`
- Test: `examples/07-shop-order-scale/order-service/business/order_syncer_test.go`

- [ ] **Step 1: 定义按 key 的结果**

把粗粒度 `ConfirmedKeys/RetryKeys/DeadKeys` 扩展为：

```go
type WriteBehindItemResult struct {
	Key     string
	Status  WriteBehindItemStatus
	Message string
}

type WriteBehindItemStatus string

const (
	WriteBehindConfirmed WriteBehindItemStatus = "confirmed"
	WriteBehindRetry     WriteBehindItemStatus = "retry"
	WriteBehindDead      WriteBehindItemStatus = "dead"
)
```

- [ ] **Step 2: 07 target 分类**

07 订单同步中：

- `UserID+RequestID` 已存在且 fingerprint 一致：`confirmed`
- fingerprint 不一致：`dead`
- MySQL 临时连接错误：`retry`
- Outbox 唯一冲突但 payload 一致：`confirmed`

- [ ] **Step 3: 测试**

验证重复 requestID 不产生第二订单、不产生第二 Outbox，并返回 `confirmed`。

---

### Task 13: 批次 checkpoint 与恢复对账

**Files:**
- Create: `pkg/persistence/database/nosql/writebehind_checkpoint.go`
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Test: `pkg/persistence/database/nosql/writebehind_checkpoint_test.go`

- [ ] **Step 1: 定义 checkpoint**

```go
type WriteBehindCheckpoint struct {
	BatchID      string
	StartedAt    time.Time
	FinishedAt   *time.Time
	TargetName   string
	ItemCount    int
	Confirmed    int
	Retry        int
	Dead         int
}
```

- [ ] **Step 2: 同步前写 checkpoint**

每批同步开始前写入 checkpoint；同步结束后更新结果。

- [ ] **Step 3: 启动时恢复**

启动时如发现未完成 checkpoint，不直接确认任何 pending，只记录恢复事件并重新扫描 sync queue。

- [ ] **Step 4: 测试**

模拟同步中 panic，重启后 pending 仍在，下一轮可以重新同步。

---

### Task 14: WriteBehind 指标和观测

**Files:**
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Create: `pkg/persistence/database/nosql/writebehind_metrics.go`
- Test: `pkg/persistence/database/nosql/writebehind_metrics_test.go`

- [ ] **Step 1: 定义指标快照**

```go
type WriteBehindMetrics struct {
	PendingCount       int
	RetryCount         int
	DeadLetterCount    int
	Attempts           uint64
	Succeeded          uint64
	Retried            uint64
	Dead               uint64
	LastSuccessAt      time.Time
	LastFailureAt      time.Time
	LastBatchDuration  time.Duration
	AverageBatchSize   float64
}
```

- [ ] **Step 2: 对外暴露**

```go
func (p *PrefixedBadgerDB[T]) GetWriteBehindMetrics() WriteBehindMetrics
```

- [ ] **Step 3: 测试**

同步成功、retry、dead-letter 都要更新指标。

---

### Task 15: 运维查询和人工处理接口

**Files:**
- Create: `service/manage/writebehind/`
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Test: `pkg/persistence/database/nosql/writebehind_admin_test.go`

- [ ] **Step 1: 提供框架级查询能力**

先提供 Go API，不急着挂 HTTP：

```go
ListPending(limit int)
ListRetry(limit int)
ListDeadLetters(limit int)
RetryDeadLetter(key string)
DeleteDeadLetter(key string)
```

- [ ] **Step 2: 示例 07 暴露只读诊断**

07 order 管理端可以查询 pending/retry/dead，但不能直接修改订单事实。

- [ ] **Step 3: 测试**

验证 dead-letter 可以人工 retry，retry 后进入 pending 队列。

---

### Task 16: 优雅停机和 drain 策略

**Files:**
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Modify: `examples/04-shop-performance/service.go`
- Modify: `examples/07-shop-order-scale/order-service/service.go`
- Test: `pkg/persistence/database/nosql/writebehind_shutdown_test.go`

- [ ] **Step 1: 定义关闭选项**

```go
type WriteBehindShutdownOptions struct {
	StopAccepting bool
	DrainTimeout  time.Duration
	SyncTimeout   time.Duration
}
```

- [ ] **Step 2: 实现 CloseWriteBehind**

关闭流程：

1. 停止接受新写入；
2. 等待 batcher 排空；
3. 尝试 drain pending；
4. 超时后保留 pending；
5. 返回包含 pending 数的错误。

- [ ] **Step 3: 测试**

远端不可用时关闭不得丢 pending；远端可用时能尽量 drain。

---

## 可靠性判断

这种方案可靠，前提是 `PrefixedBadgerDB` 仍然坚持三条不变：

1. API 成功只表示本地 Badger 已可靠落盘，不表示远端已同步；
2. ACK 必须由 target 返回确认 key 后才删除 pending 队列；
3. 远端业务事实和 Outbox 必须在 target 的远端事务里一起提交。

比现在更好的方案不是回到 `ModelList`，而是让 `PrefixedBadgerDB` 成为可靠本地队列框架，让 SQL/业务同步成为 target。再往生产级走，可以在 target 层支持毒丸隔离、重试退避、批次检查点、幂等冲突分类和观测指标，但这些都应该在框架内，不应该散落在 04/07 业务示例里。
