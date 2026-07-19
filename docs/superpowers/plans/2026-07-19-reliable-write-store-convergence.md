# ReliableWriteStore 框架收敛 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 04/07 重复的 Group Commit、背压、磁盘指标、有界同步和 store 生命周期收敛为支持可靠保存、更新、删除的框架级 `ReliableWriteStore[T]`。

**Architecture:** `ServiceContext` 只管理通用 `ManagedResource`，避免依赖具体 persistence 类型；`nosql` 根据服务身份解析实例隔离路径，并用 `ReliableWriteStore[T]` 组合 `PrefixedBadgerDB[T]`、`BatchCommitter[T]`、`WriteAdmissionController` 和指标。04/07 各自保存实例级 typed runtime handle，业务领域事务和远端 `WriteBehindTarget` 保持原有边界，不使用全局 store registry。

**Tech Stack:** Go 1.26、Badger v3、go-zero `syncx.TimeoutLimit`、泛型、`context`、`testify/require`、MySQL、Docker Compose。

---

所有新增 Go 文件必须以中文文件级注释说明能力边界；所有导出类型、函数、方法和错误变量必须有中文注释。测试文件的文件级注释必须写明验证场景，不能只依赖测试名。

## 文件结构

框架新增文件按单一职责拆分：

- `pkg/server/router/resource_manager.go`：通用资源注册、逆序关闭和错误汇总。
- `pkg/server/router/resource_manager_test.go`：资源隔离、注册边界、关闭顺序和幂等测试。
- `pkg/persistence/database/nosql/reliable_write_store_config.go`：服务身份、路径、batch、背压和关闭配置。
- `pkg/persistence/database/nosql/reliable_write_store_path_test.go`：实例目录解析和非法服务名测试。
- `pkg/persistence/database/nosql/sharedbadger_operations.go`：有序 Save/Delete 本地事务原语和磁盘大小。
- `pkg/persistence/database/nosql/sharedbadger_operations_test.go`：upsert、tombstone、同 key 顺序和物理清除测试。
- `pkg/persistence/database/nosql/sharedbadger_force_sync.go`：带 context 和 limit 的有界同步入口。
- `pkg/persistence/database/nosql/sharedbadger_force_sync_test.go`：上限、部分确认、取消和无进展测试。
- `pkg/persistence/database/nosql/reliable_write_batcher.go`：跨请求 Group Commit。
- `pkg/persistence/database/nosql/reliable_write_batcher_test.go`：聚合、顺序、关闭和 panic 测试。
- `pkg/persistence/database/nosql/reliable_write_admission.go`：并发、pending 和磁盘背压。
- `pkg/persistence/database/nosql/reliable_write_admission_test.go`：三类拒绝和持续积压测试。
- `pkg/persistence/database/nosql/reliable_write_metrics.go`：统一只读指标快照。
- `pkg/persistence/database/nosql/reliable_write_store.go`：业务可见可靠 store 门面。
- `pkg/persistence/database/nosql/reliable_write_store_test.go`：公开 API、关闭和恢复契约测试。
- `pkg/persistence/database/nosql/reliable_write_store_admin.go`：独立运维物理清除 handle。

示例迁移文件：

- 04：保留 `models/order_persistence.go`，重写 `models/order_write_store.go` 为领域适配器；新增 `models/order_write_runtime.go`；删除业务重复的 `order_batcher.go`、`order_write_guard.go` 及其测试；调整 `business/order.go`、四个订单 API 和 `service.go` 为注入式访问。
- 07：保留 `models/transaction/order_persistence.go`、`order_query.go` 和业务 `OrderWriteBehindTarget`；重写 `order_write_store.go` 为领域适配器；新增 `order_write_runtime.go`；删除 `order_batcher.go`、`order_write_guard.go`、`order_write_store_lifecycle.go`；调整 `LocalOrderWriter`、`RemoteOrderSyncer`、订单查询、Public API 和 `Service` 为实例级注入。

## API 锁定

后续任务统一使用以下签名，不在示例中再造同义 API：

```go
type ServiceIdentity struct {
	ServiceName  string
	DataCenterID int64
	MachineID    int64
}

type BatchWriteResult struct {
	Committed int
}

type LocalScanOptions struct {
	Prefix string
	Limit  int
}

func NewReliableWriteStore[T types.IModel](
	identity ServiceIdentity,
	config ReliableWriteStoreConfig,
) (*ReliableWriteStore[T], *ReliableWriteStoreAdmin[T], error)

func (s *ReliableWriteStore[T]) Save(context.Context, *T) error
func (s *ReliableWriteStore[T]) SaveBatch(context.Context, []*T) (BatchWriteResult, error)
func (s *ReliableWriteStore[T]) Delete(context.Context, *T) error
func (s *ReliableWriteStore[T]) DeleteBatch(context.Context, []*T) (BatchWriteResult, error)
func (s *ReliableWriteStore[T]) Add(context.Context, *T) error
func (s *ReliableWriteStore[T]) AddBatch(context.Context, []*T) (BatchWriteResult, error)
func (s *ReliableWriteStore[T]) GetLocal(context.Context, string) (*T, error)
func (s *ReliableWriteStore[T]) ScanLocal(context.Context, LocalScanOptions) ([]*T, error)
func (s *ReliableWriteStore[T]) UseWriteBehind(WriteBehindTarget[T]) error
func (s *ReliableWriteStore[T]) ForceSyncBatch(context.Context, int) (ForceSyncResult, error)
func (s *ReliableWriteStore[T]) ForceSyncAll(context.Context) (ForceSyncResult, error)
func (s *ReliableWriteStore[T]) Metrics() ReliableWriteMetrics
func (s *ReliableWriteStore[T]) Close(context.Context) error

func (a *ReliableWriteStoreAdmin[T]) PurgeLocal(context.Context, *T) error
```

### Task 1: ServiceContext 通用资源生命周期

**Files:**
- Create: `pkg/server/router/resource_manager.go`
- Create: `pkg/server/router/resource_manager_test.go`
- Modify: `pkg/server/router/servicecontext.go`
- Test: `pkg/server/router/servicecontext_lifecycle_test.go`

- [ ] **Step 1: 写资源顺序和注册边界失败测试**

```go
func TestResourceManagerClosesInReverseOrderAndJoinsErrors(t *testing.T) {
	manager := newResourceManager()
	var order []string
	errA := errors.New("close-a")
	require.NoError(t, manager.Use("a", resourceFunc(func(context.Context) error {
		order = append(order, "a")
		return errA
	})))
	require.NoError(t, manager.Use("b", resourceFunc(func(context.Context) error {
		order = append(order, "b")
		return nil
	})))

	err := manager.Close(context.Background())
	require.ErrorIs(t, err, errA)
	require.Equal(t, []string{"b", "a"}, order)
	require.ErrorIs(t, manager.Use("late", resourceFunc(func(context.Context) error { return nil })), ErrResourceManagerClosed)
	require.ErrorIs(t, manager.Use("a", resourceFunc(func(context.Context) error { return nil })), ErrResourceManagerClosed)
	require.ErrorIs(t, manager.Close(context.Background()), errA)
}
```

同文件加入明确断言：

```go
func TestResourceManagerRejectsDuplicateName(t *testing.T) {
	manager := newResourceManager()
	require.NoError(t, manager.Use("orders", resourceFunc(func(context.Context) error { return nil })))
	require.ErrorIs(t, manager.Use("orders", resourceFunc(func(context.Context) error { return nil })), ErrResourceAlreadyRegistered)
}

func TestServiceContextResourcesAreInstanceScoped(t *testing.T) {
	first := &ServiceContext{resources: newResourceManager()}
	second := &ServiceContext{resources: newResourceManager()}
	require.NoError(t, first.UseResource("orders", resourceFunc(func(context.Context) error { return nil })))
	require.NoError(t, second.UseResource("orders", resourceFunc(func(context.Context) error { return nil })))
}
```

- [ ] **Step 2: 运行测试并确认失败原因是 API 尚不存在**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/router -run 'TestResourceManager|TestServiceContextResource' -count=1
```

Expected: FAIL，提示 `newResourceManager`、`ManagedResource` 或 `UseResource` 未定义。

- [ ] **Step 3: 实现资源管理器**

```go
var (
	ErrResourceManagerClosed      = errors.New("服务资源管理器已关闭")
	ErrResourceAlreadyRegistered = errors.New("服务资源名称已注册")
)

type ManagedResource interface {
	Close(context.Context) error
}

type managedResourceEntry struct {
	name     string
	resource ManagedResource
}

type resourceManager struct {
	mu      sync.Mutex
	entries []managedResourceEntry
	names   map[string]struct{}
	closed  bool
	once    sync.Once
	err     error
}

func (m *resourceManager) Use(name string, resource ManagedResource) error
func (m *resourceManager) Close(ctx context.Context) error
```

`Use` 对空 name、nil resource、重复 name 和 closed 状态返回稳定错误。`Close` 在锁内复制 entries 并标记 closed，在锁外把同一个 context 传给每个资源并逆序逐一关闭；即使 context 已取消或某个资源失败，也继续调用剩余资源，最后以 `errors.Join` 保存稳定结果。

- [ ] **Step 4: 接入 ServiceContext 初始化和停止路径**

在 `ServiceContext` 增加：

```go
resources *resourceManager

func (own *ServiceContext) UseResource(name string, resource ManagedResource) error {
	if own == nil || own.resources == nil {
		return ErrResourceManagerClosed
	}
	return own.resources.Use(name, resource)
}
```

两个构造路径在 `initServiceContextPost` 前初始化 `resources`。`SetRunState(false)` 在路由停止接收新请求、业务 `Service.Stop` 已取消 ticker 后，使用 `own.lifecycleDuration()` 创建 context 并关闭资源；关闭错误传给 `recordShutdownError`，之后继续关闭 WebSocket、EventBridge、MQ 和集群资源。

- [ ] **Step 5: 运行生命周期测试与 race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/router -run 'TestResourceManager|TestServiceContextResource|TestServiceContext.*Shutdown' -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/router -run 'TestResourceManager|TestServiceContextResource' -count=1
```

Expected: PASS，race 无报告。

- [ ] **Step 6: 提交资源生命周期**

```bash
rtk git add pkg/server/router/resource_manager.go pkg/server/router/resource_manager_test.go pkg/server/router/servicecontext.go pkg/server/router/servicecontext_lifecycle_test.go
rtk git commit -m "feat: add service context resource lifecycle"
```

### Task 2: ReliableWriteStore 配置与实例路径

**Files:**
- Create: `pkg/persistence/database/nosql/reliable_write_store_config.go`
- Create: `pkg/persistence/database/nosql/reliable_write_store_path_test.go`

- [ ] **Step 1: 写路径解析失败测试**

```go
func TestResolveReliableWritePathUsesServiceAndMachineIdentity(t *testing.T) {
	path, err := resolveReliableWritePath("/data/pending", ServiceIdentity{
		ServiceName: "Shop-Order", DataCenterID: 2, MachineID: 7,
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/data/pending", "shop-order", "dc-2", "machine-7"), path)
}

func TestResolveReliableWritePathRejectsUnsafeIdentity(t *testing.T) {
	for _, identity := range []ServiceIdentity{
		{ServiceName: "", DataCenterID: 1, MachineID: 1},
		{ServiceName: "../order", DataCenterID: 1, MachineID: 1},
		{ServiceName: "order/a", DataCenterID: 1, MachineID: 1},
		{ServiceName: "order", DataCenterID: -1, MachineID: 1},
		{ServiceName: "order", DataCenterID: 1, MachineID: -1},
	} {
		_, err := resolveReliableWritePath(t.TempDir(), identity)
		require.Error(t, err)
	}
}
```

- [ ] **Step 2: 运行路径测试确认失败**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run TestResolveReliableWritePath -count=1
```

Expected: FAIL，提示路径配置类型未定义。

- [ ] **Step 3: 实现完整配置和校验**

```go
type BatchCommitConfig struct {
	MaxBatch      int
	CollectWindow time.Duration
	QueueCapacity int
}

type WriteAdmissionConfig struct {
	MaxConcurrent      int
	AcquireTimeout     time.Duration
	SoftPending        int
	HardPending        int
	MaxBacklogDuration time.Duration
	HardDiskBytes      int64
}

type ReliableWriteStoreConfig struct {
	BasePath     string
	Badger       BadgerDBConfig
	Batch        BatchCommitConfig
	Admission    WriteAdmissionConfig
	CloseTimeout time.Duration
}
```

`resolveReliableWritePath` 先 `TrimSpace`、转小写，再仅接受 `[a-z0-9][a-z0-9._-]*`。`Validate` 要求 `BasePath` 非空，填充 batch、队列、关闭时限默认值；模型 prefix 继续由 `NewSharedBadgerDB[T]` 根据 T 生成。Badger 的最终 `Path` 必须被解析结果覆盖，不能信任调用方另传的 `Badger.Path`。

- [ ] **Step 4: 补默认值和路径隔离测试并运行**

```go
func TestResolveReliableWritePathSeparatesServices(t *testing.T) {
	base := t.TempDir()
	first, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order-a", DataCenterID: 1, MachineID: 1})
	require.NoError(t, err)
	second, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order-b", DataCenterID: 1, MachineID: 1})
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestResolveReliableWritePathSeparatesMachines(t *testing.T) {
	base := t.TempDir()
	first, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order", DataCenterID: 1, MachineID: 3})
	require.NoError(t, err)
	second, err := resolveReliableWritePath(base, ServiceIdentity{ServiceName: "order", DataCenterID: 1, MachineID: 4})
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}
```

运行：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestResolveReliableWritePath|TestReliableWriteStoreConfig' -count=1
```

Expected: PASS。

- [ ] **Step 5: 提交配置和路径**

```bash
rtk git add pkg/persistence/database/nosql/reliable_write_store_config.go pkg/persistence/database/nosql/reliable_write_store_path_test.go
rtk git commit -m "feat: define reliable write store identity and config"
```

### Task 3: Badger 有序 Save/Delete 原语与磁盘指标

**Files:**
- Create: `pkg/persistence/database/nosql/reliable_write_test_helpers_test.go`
- Create: `pkg/persistence/database/nosql/sharedbadger_operations.go`
- Create: `pkg/persistence/database/nosql/sharedbadger_operations_test.go`
- Modify: `pkg/persistence/database/nosql/sharedbadgermanager.go`
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`

- [ ] **Step 1: 写本地操作语义失败测试**

```go
func TestApplyWriteOperationsPreservesSameKeyOrder(t *testing.T) {
	db := newReliableOperationsTestDB(t)
	item := newFund("user-41", "HK", 10)
	updated := newFund("user-41", "HK", 20)

	result, err := db.ApplyWriteOperations([]WriteOperation[testFund]{
		{Type: WriteOperationSave, Item: item},
		{Type: WriteOperationSave, Item: updated},
		{Type: WriteOperationDelete, Item: updated},
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.Committed)
	_, err = db.Get(updated.GetHash())
	require.ErrorIs(t, err, badger.ErrKeyNotFound)
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
}
```

测试 helper 使用现有 `testFund`，统一创建关闭清理：

```go
func newReliableOperationsTestDB(t *testing.T) *PrefixedBadgerDB[testFund] {
	t.Helper()
	config := DefaultProductionConfig(t.TempDir())
	config.AutoSync = false
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehind(newRecordingWriteBehindTarget[testFund]()))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestReliableDeleteIsIdempotent(t *testing.T) {
	db := newReliableOperationsTestDB(t)
	item := newFund("missing", "HK", 1)
	result, err := db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationDelete, Item: item}})
	require.NoError(t, err)
	require.Equal(t, 1, result.Committed)
	require.Equal(t, 0, db.GetCachedPendingSyncCount())
}

func TestSaveDeletedReturnsConflict(t *testing.T) {
	db := newReliableOperationsTestDB(t)
	item := newFund("deleted", "HK", 1)
	_, err := db.ApplyWriteOperations([]WriteOperation[testFund]{
		{Type: WriteOperationSave, Item: item},
		{Type: WriteOperationDelete, Item: item},
	})
	require.NoError(t, err)
	_, err = db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: item}})
	require.ErrorIs(t, err, ErrWriteConflictDeleted)
}
```

在 `reliable_write_test_helpers_test.go` 同时定义后续任务共用的 bounded target 和 store 配置：

```go
type boundedSyncTarget struct {
	confirm        int
	returnErr      error
	waitForContext bool
	calls          atomic.Int64
	received       atomic.Int64
}

func (t *boundedSyncTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
	t.calls.Add(1)
	t.received.Add(int64(len(items)))
	if t.waitForContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	count := min(t.confirm, len(items))
	keys := make([]string, 0, count)
	for _, item := range items[:count] {
		keys = append(keys, item.Key)
	}
	return &WriteBehindResult{ConfirmedKeys: keys}, t.returnErr
}

func (t *boundedSyncTarget) Calls() int { return int(t.calls.Load()) }

func testReliableIdentity() ServiceIdentity {
	return ServiceIdentity{ServiceName: "test-order", DataCenterID: 1, MachineID: 2}
}

func testReliableConfig(t *testing.T) ReliableWriteStoreConfig {
	t.Helper()
	badgerConfig := DefaultProductionConfig(t.TempDir())
	badgerConfig.AutoSync = false
	return ReliableWriteStoreConfig{
		BasePath: t.TempDir(),
		Badger: badgerConfig,
		Batch: BatchCommitConfig{MaxBatch: 8, CollectWindow: time.Millisecond, QueueCapacity: 64},
		Admission: WriteAdmissionConfig{MaxConcurrent: 8, AcquireTimeout: time.Second},
		CloseTimeout: time.Second,
	}
}

func newBoundedSyncTestDB(t *testing.T, target *boundedSyncTarget, count int) *PrefixedBadgerDB[testFund] {
	t.Helper()
	config := DefaultProductionConfig(t.TempDir())
	config.AutoSync = false
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehind(target))
	for index := range count {
		require.NoError(t, db.Set(newFund(fmt.Sprintf("user-%d", index), "HK", 1), 0))
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
```

加入 insert/update 判定测试：

```go
func TestReliableSaveChoosesInsertThenUpdate(t *testing.T) {
	db := newReliableOperationsTestDB(t)
	first := newFund("upsert", "HK", 1)
	_, err := db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: first}})
	require.NoError(t, err)
	wrapper, err := db.getWrapper(db.generateKey(first))
	require.NoError(t, err)
	require.Equal(t, OpInsert, wrapper.Op)

	updated := newFund("upsert", "HK", 2)
	_, err = db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: updated}})
	require.NoError(t, err)
	wrapper, err = db.getWrapper(db.generateKey(updated))
	require.NoError(t, err)
	require.Equal(t, OpUpdate, wrapper.Op)
	require.Equal(t, float64(2), wrapper.Item.Balance)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestApplyWriteOperations|TestReliableDelete|TestSaveDeleted' -count=1
```

Expected: FAIL，提示 `ApplyWriteOperations` 与操作类型未定义。

- [ ] **Step 3: 实现顺序事务原语**

```go
type WriteOperationType uint8

const (
	WriteOperationSave WriteOperationType = iota + 1
	WriteOperationDelete
)

var ErrWriteConflictDeleted = errors.New("可靠写入不能复活已删除数据")

type WriteOperation[T types.IModel] struct {
	Type WriteOperationType
	Item *T
}

func (p *PrefixedBadgerDB[T]) ApplyWriteOperations(operations []WriteOperation[T]) (BatchWriteResult, error)
```

实现时在同一个 `badger.Txn` 中按切片顺序读取并改写 wrapper，确保后一个同 key 操作能看到前一个操作。每个 data key 只创建一个 sync queue key，事务成功后按唯一新 queue key 数增加 pending。`Delete` 不存在或已删除时不新增 pending；Save tombstone 立即返回 `ErrWriteConflictDeleted`。不要调用现有会另开 View/Update 的 `setItem` 或 `delete`。

- [ ] **Step 4: 实现有序拆批和部分提交结果**

`ApplyWriteOperations` 先按配置的最大操作数形成有序子批；遇到 `badger.ErrTxnTooBig` 时对当前子批做二分并依次提交。前缀子批成功、后续失败时返回：

```go
return BatchWriteResult{Committed: committedPrefix}, fmt.Errorf(
	"可靠本地批次提交失败（已成功 %d/%d）: %w",
	committedPrefix, len(operations), err,
)
```

不得在失败后继续提交后面的操作，否则单个调用无法确定可重试边界。

- [ ] **Step 5: 暴露 Badger 原生磁盘大小**

在 `SharedBadgerManager` 增加：

```go
type BadgerSize struct {
	LSMBytes  int64
	VLogBytes int64
}

func (m *SharedBadgerManager) Size() BadgerSize {
	lsm, vlog := m.db.Size()
	return BadgerSize{LSMBytes: lsm, VLogBytes: vlog}
}
```

在 `PrefixedBadgerDB` 增加只读转发 `StorageSize() BadgerSize`，并测试总量等于 `LSMBytes+VLogBytes` 且无需 `filepath.WalkDir`。

- [ ] **Step 6: 运行 operations 全套测试和 race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestApplyWriteOperations|TestReliableDelete|TestSaveDeleted|TestBadgerStorageSize' -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/persistence/database/nosql -run 'TestApplyWriteOperations|TestReliableDelete' -count=1
```

Expected: PASS，race 无报告。

- [ ] **Step 7: 提交 Badger 操作原语**

```bash
rtk git add pkg/persistence/database/nosql/sharedbadger_operations.go pkg/persistence/database/nosql/sharedbadger_operations_test.go pkg/persistence/database/nosql/sharedbadgermanager.go pkg/persistence/database/nosql/sharedbadger.go
rtk git commit -m "feat: add ordered reliable badger operations"
```

### Task 4: 带 context 的有界 WriteBehind 同步

**Files:**
- Create: `pkg/persistence/database/nosql/sharedbadger_force_sync.go`
- Create: `pkg/persistence/database/nosql/sharedbadger_force_sync_test.go`
- Modify: `pkg/persistence/database/nosql/sharedbadger.go`
- Modify: `pkg/persistence/database/nosql/writebehind_target.go`

- [ ] **Step 1: 写 limit、部分确认和取消失败测试**

```go
func TestForceSyncBatchHonorsLimitAndPartialConfirmation(t *testing.T) {
	target := &boundedSyncTarget{confirm: 2, returnErr: errRemote}
	db := newBoundedSyncTestDB(t, target, 5)

	result, err := db.ForceSyncBatch(context.Background(), 3)
	require.ErrorIs(t, err, errRemote)
	require.Equal(t, ForceSyncResult{Confirmed: 2, Remaining: 3}, result)
	require.Equal(t, int64(3), target.received.Load())
}
```

加入以下边界断言：

```go
func TestForceSyncBatchRejectsInvalidLimit(t *testing.T) {
	db := newBoundedSyncTestDB(t, &boundedSyncTarget{}, 1)
	_, err := db.ForceSyncBatch(context.Background(), 0)
	require.ErrorIs(t, err, ErrInvalidSyncLimit)
}

func TestForceSyncBatchPropagatesCancellation(t *testing.T) {
	target := &boundedSyncTarget{waitForContext: true}
	db := newBoundedSyncTestDB(t, target, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.ForceSyncBatch(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, target.Calls())
}

func TestForceSyncAllContextStopsWithoutProgress(t *testing.T) {
	db := newBoundedSyncTestDB(t, &boundedSyncTarget{confirm: 0}, 1)
	_, err := db.ForceSyncAllContext(context.Background())
	require.ErrorIs(t, err, ErrWriteBehindNoProgress)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestForceSyncBatch|TestForceSyncAllContext' -count=1
```

Expected: FAIL，提示有界同步 API 未定义。

- [ ] **Step 3: 将 context 贯穿同步调用**

把内部签名改为：

```go
func (p *PrefixedBadgerDB[T]) processSyncQueueContext(ctx context.Context, limit int) (int, error)
func (p *PrefixedBadgerDB[T]) syncBatchContext(ctx context.Context, items []*SyncQueueItem[T]) ([]string, error)
func (p *PrefixedBadgerDB[T]) syncBatchWithTarget(ctx context.Context, items []*SyncQueueItem[T], target WriteBehindTarget[T]) ([]string, error)
```

旧的 worker 和 `ForceSyncAll()` 用 `context.Background()` 委托新实现以保持兼容。target 必须收到调用方 context；旧 `IDataAction` 分组在每组前检查 `ctx.Err()`，因为该接口本身没有 context 参数。

- [ ] **Step 4: 实现公开有界结果**

```go
type ForceSyncResult struct {
	Confirmed int
	Remaining int
}

func (p *PrefixedBadgerDB[T]) ForceSyncBatch(ctx context.Context, limit int) (ForceSyncResult, error)
func (p *PrefixedBadgerDB[T]) ForceSyncAllContext(ctx context.Context) (ForceSyncResult, error)
```

`ForceSyncBatch` 在 `syncExecMu` 边界内最多读取 limit 条，先 ACK target 返回的 `ConfirmedKeys`，再返回 target error。`ForceSyncAllContext` 循环调用配置批次大小；任一轮 `Confirmed==0 && Remaining>0` 立即返回 `ErrWriteBehindNoProgress`，不使用固定 `maxIterations=100`。

- [ ] **Step 5: 保持旧 API 编译兼容并运行测试**

```go
func (p *PrefixedBadgerDB[T]) ForceSyncAll() error {
	_, err := p.ForceSyncAllContext(context.Background())
	return err
}
```

运行：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestForceSync|TestWriteBehindTarget|TestCloseWithPending' -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/persistence/database/nosql -run 'TestForceSyncBatch|TestForceSyncAllContext' -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交有界同步**

```bash
rtk git add pkg/persistence/database/nosql/sharedbadger_force_sync.go pkg/persistence/database/nosql/sharedbadger_force_sync_test.go pkg/persistence/database/nosql/sharedbadger.go pkg/persistence/database/nosql/writebehind_target.go
rtk git commit -m "feat: add bounded context-aware write behind sync"
```

### Task 5: 通用 BatchCommitter、背压和指标

**Files:**
- Create: `pkg/persistence/database/nosql/reliable_write_batcher.go`
- Create: `pkg/persistence/database/nosql/reliable_write_batcher_test.go`
- Create: `pkg/persistence/database/nosql/reliable_write_admission.go`
- Create: `pkg/persistence/database/nosql/reliable_write_admission_test.go`
- Create: `pkg/persistence/database/nosql/reliable_write_metrics.go`

- [ ] **Step 1: 写 Group Commit 失败测试**

```go
func TestBatchCommitterAggregatesAndPreservesOrder(t *testing.T) {
	var mu sync.Mutex
	var committed []WriteOperation[testFund]
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int64
	committer := newBatchCommitter[testFund](BatchCommitConfig{
		MaxBatch: 8, CollectWindow: time.Millisecond, QueueCapacity: 32,
	}, func(ops []WriteOperation[testFund]) (BatchWriteResult, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		mu.Lock()
		committed = append(committed, ops...)
		mu.Unlock()
		return BatchWriteResult{Committed: len(ops)}, nil
	})
	t.Cleanup(func() { require.NoError(t, committer.Close(context.Background())) })

	item := newFund("ordered", "HK", 1)
	results := make(chan error, 3)
	go func() { results <- committer.Submit(context.Background(), WriteOperation[testFund]{Type: WriteOperationSave, Item: item}) }()
	<-firstStarted
	go func() { results <- committer.Submit(context.Background(), WriteOperation[testFund]{Type: WriteOperationDelete, Item: item}) }()
	go func() { results <- committer.Submit(context.Background(), WriteOperation[testFund]{Type: WriteOperationSave, Item: item}) }()
	close(releaseFirst)
	for range 3 { require.NoError(t, <-results) }
	require.Equal(t, []WriteOperationType{WriteOperationSave, WriteOperationDelete, WriteOperationSave}, []WriteOperationType{
		committed[0].Type, committed[1].Type, committed[2].Type,
	})
}
```

同文件实现并断言以下独立测试，不合并成一个大测试：

```go
func TestBatchCommitterConvertsCommitPanic(t *testing.T)       // Submit 返回包含 panic 文本的 error
func TestBatchCommitterCloseDrainsAcceptedRequests(t *testing.T) // 已入队请求成功，后续 Submit 返回 ErrWriteStoreClosed
func TestBatchCommitterSubmitHonorsContext(t *testing.T)       // 满队列等待时返回 context.Canceled
func TestBatchCommitterRoutesPartialPrefixResult(t *testing.T) // Committed=1 时首请求 nil，其余收到 commit error
```

- [ ] **Step 2: 写背压失败测试**

```go
func TestWriteAdmissionRejectsHardLimits(t *testing.T) {
	controller := newWriteAdmissionController(WriteAdmissionConfig{
		MaxConcurrent: 1, AcquireTimeout: time.Millisecond,
		HardPending: 2, HardDiskBytes: 100,
	})
	_, err := controller.Acquire(context.Background(), 2, 0, time.Now())
	require.ErrorIs(t, err, ErrWriteRejectedPending)
	_, err = controller.Acquire(context.Background(), 0, 100, time.Now())
	require.ErrorIs(t, err, ErrWriteRejectedDisk)
}
```

补齐 soft pending 持续超时、恢复后计时清零、并发 timeout 和 release 幂等测试。

- [ ] **Step 3: 运行测试确认失败**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestBatchCommitter|TestWriteAdmission' -count=1
```

Expected: FAIL，提示通用组件未定义。

- [ ] **Step 4: 实现 BatchCommitter**

```go
type batchCommitRequest[T types.IModel] struct {
	sequence uint64
	op       WriteOperation[T]
	result   chan error
}

type BatchCommitter[T types.IModel] struct {
	config    BatchCommitConfig
	commit    func([]WriteOperation[T]) (BatchWriteResult, error)
	requests  chan batchCommitRequest[T]
	closing   chan struct{}
	done      chan struct{}
	sequence  atomic.Uint64
	metrics   batchCommitMetrics
	stateMu   sync.RWMutex
	closed    bool
	submitters sync.WaitGroup
	once      sync.Once
	closeErrMu sync.Mutex
	closeErr  error
}
```

worker 按 sequence 排序后提交；commit 返回 `Committed=N` 时前 N 个请求成功，其余收到同一个 error。若返回 nil error 但 `Committed != len(batch)`，转换为框架错误，防止静默丢结果。

- [ ] **Step 5: 实现 WriteAdmissionController 和 typed error**

```go
var (
	ErrWriteStoreClosed        = errors.New("可靠写入存储已关闭")
	ErrWriteRejectedConcurrency = errors.New("可靠写入并发已达上限")
	ErrWriteRejectedPending     = errors.New("可靠写入积压已达上限")
	ErrWriteRejectedDisk        = errors.New("可靠写入磁盘已达上限")
)

func (c *WriteAdmissionController) Acquire(
	ctx context.Context,
	pending int,
	diskBytes int64,
	now time.Time,
) (release func(), err error)
```

并发限制继续复用 `syncx.TimeoutLimit`；调用方 context 取消与配置 acquire timeout 取先到者。错误可用 `errors.Is` 判定，示例层负责映射为业务文案。

- [ ] **Step 6: 实现统一指标结构**

```go
type ReliableWriteMetrics struct {
	StartedAt      time.Time
	Pending        int
	BadgerLSMBytes int64
	BadgerVLogBytes int64
	Batch          BatchCommitMetrics
	Admission      WriteAdmissionMetrics
	Sync           SyncMetrics
}
```

所有字段来自原子计数或 Badger O(1) 快照，不启动磁盘扫描 goroutine。

- [ ] **Step 7: 运行测试和 race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run 'TestBatchCommitter|TestWriteAdmission' -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/persistence/database/nosql -run 'TestBatchCommitter|TestWriteAdmission' -count=1
```

Expected: PASS。

- [ ] **Step 8: 提交通用并发组件**

```bash
rtk git add pkg/persistence/database/nosql/reliable_write_batcher.go pkg/persistence/database/nosql/reliable_write_batcher_test.go pkg/persistence/database/nosql/reliable_write_admission.go pkg/persistence/database/nosql/reliable_write_admission_test.go pkg/persistence/database/nosql/reliable_write_metrics.go
rtk git commit -m "feat: add reliable write batching and admission"
```

### Task 6: ReliableWriteStore 门面与独立 Admin handle

**Files:**
- Create: `pkg/persistence/database/nosql/reliable_write_store.go`
- Create: `pkg/persistence/database/nosql/reliable_write_store_admin.go`
- Create: `pkg/persistence/database/nosql/reliable_write_store_test.go`

- [ ] **Step 1: 写公开 API 和关闭失败测试**

```go
func TestReliableWriteStoreSaveDeleteAndClose(t *testing.T) {
	store, admin, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	require.NotNil(t, admin)
	require.NoError(t, store.UseWriteBehind(&boundedSyncTarget{}))

	item := newFund("store-user", "HK", 1)
	require.NoError(t, store.Save(context.Background(), item))
	require.NoError(t, store.Delete(context.Background(), item))
	_, err = store.GetLocal(context.Background(), item.GetHash())
	require.ErrorIs(t, err, badger.ErrKeyNotFound)

	err = store.Close(context.Background())
	var pendingErr *PendingSyncError
	require.ErrorAs(t, err, &pendingErr)
	require.ErrorIs(t, store.Save(context.Background(), newFund("closed", "HK", 1)), ErrWriteStoreClosed)
	require.ErrorIs(t, store.Close(context.Background()), err)
}
```

同文件加入以下独立测试，并使用 `newFund` 与 `testReliableConfig(t)` helper：

```go
func TestReliableWriteStoreRejectsWriteBeforeTargetBinding(t *testing.T)
func TestReliableWriteStoreAddDelegatesToSave(t *testing.T)
func TestReliableWriteStoreBatchReturnsCommittedPrefix(t *testing.T)
func TestReliableWriteStoreScanLocalHidesTombstones(t *testing.T)
func TestReliableWriteStoreAdminPurgeRemovesPendingIndex(t *testing.T)
func TestReliableWriteStoreRejectsSecondTargetBinding(t *testing.T)
```

每个测试分别断言 `ErrWriteBehindNotBound`、`GetLocal` 可见、`BatchWriteResult.Committed`、Scan 结果为空、pending 归零和 `errors.Is(err, ErrWriteBehindAlreadyBound)`。

- [ ] **Step 2: 运行测试确认失败**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -run TestReliableWriteStore -count=1
```

Expected: FAIL，提示构造器和门面 API 未定义。

- [ ] **Step 3: 实现构造器和写入数据流**

```go
type ReliableWriteStore[T types.IModel] struct {
	db        *PrefixedBadgerDB[T]
	batcher   *BatchCommitter[T]
	admission *WriteAdmissionController
	startedAt time.Time
	closeOnce sync.Once
	closeErr  error
	closing   atomic.Bool
	bound     atomic.Bool
}

func (s *ReliableWriteStore[T]) submit(ctx context.Context, op WriteOperation[T]) error {
	if s == nil || s.closing.Load() {
		return ErrWriteStoreClosed
	}
	if !s.bound.Load() {
		return ErrWriteBehindNotBound
	}
	size := s.db.StorageSize()
	release, err := s.admission.Acquire(ctx, s.db.GetCachedPendingSyncCount(), size.LSMBytes+size.VLogBytes, time.Now())
	if err != nil {
		return err
	}
	defer release()
	return s.batcher.Submit(ctx, op)
}
```

`UseWriteBehind` 只有在底层绑定成功后才设置 `bound=true`。构造失败时必须关闭已经打开的 Badger；返回的 admin handle 只保存私有 db 引用，不能反向暴露 store。

- [ ] **Step 4: 实现读、同步、指标和关闭**

`GetLocal`/`ScanLocal` 先检查 context；`UseWriteBehind` 原样返回底层绑定错误；`ForceSyncBatch/All` 委托 Task 4；`Metrics` 聚合 Task 3/5 快照。`Close(ctx)` 顺序固定为：CAS 标记 closing、关闭 batcher 排空已接受写、按 context 剩余 deadline 调用 `CloseWithTimeout`、保留 pending 并稳定返回 `PendingSyncError`；不得在 Close 中强制访问远端。

- [ ] **Step 5: 运行门面测试、原有 nosql 测试和 race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/persistence/database/nosql -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/persistence/database/nosql -run 'TestReliableWriteStore|TestBatchCommitter|TestForceSyncBatch' -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交 ReliableWriteStore**

```bash
rtk git add pkg/persistence/database/nosql/reliable_write_store.go pkg/persistence/database/nosql/reliable_write_store_admin.go pkg/persistence/database/nosql/reliable_write_store_test.go
rtk git commit -m "feat: add reliable write store facade"
```

### Task 7: 示例 04 迁移到实例级 ReliableWriteStore

**Files:**
- Create: `examples/04-shop-performance/models/order_write_runtime.go`
- Modify: `examples/04-shop-performance/models/order_write_store.go`
- Modify: `examples/04-shop-performance/models/order_write_store_test.go`
- Modify: `examples/04-shop-performance/models/order_persistence.go`
- Modify: `examples/04-shop-performance/business/order.go`
- Modify: `examples/04-shop-performance/business/payment.go`
- Modify: `examples/04-shop-performance/business/product.go`
- Modify: `examples/04-shop-performance/api/private/addorder.go`
- Modify: `examples/04-shop-performance/api/private/getorders.go`
- Modify: `examples/04-shop-performance/api/private/deleteorder.go`
- Modify: `examples/04-shop-performance/api/private/cancelorder.go`
- Modify: `examples/04-shop-performance/api/private/createpayment.go`
- Modify: `examples/04-shop-performance/api/manage/productmanage.go`
- Modify: `examples/04-shop-performance/api/manage/paymentrecord_commands.go`
- Modify: `examples/04-shop-performance/service.go`
- Delete: `examples/04-shop-performance/models/order_batcher.go`
- Delete: `examples/04-shop-performance/models/order_batcher_test.go`
- Delete: `examples/04-shop-performance/models/order_write_guard.go`
- Delete: `examples/04-shop-performance/models/order_write_guard_test.go`

- [ ] **Step 1: 把现有 store 契约测试改成实例注入并确认失败**

```go
runtime := NewOrderWriteRuntime()
store, err := NewOrderWriteStore(scIdentity, action, config)
require.NoError(t, err)
require.NoError(t, runtime.Bind(store))
t.Cleanup(runtime.Unbind)

require.NoError(t, runtime.Save(context.Background(), order))
visible, err := runtime.QueryVisibleOrders(context.Background(), userID)
```

删除测试对 `StartOrderWriteStore`、`StopOrderWriteStore` 和全局 getter 的依赖；新增两个 runtime 实例互不共享 store 的测试。把原 `TestRemoveLocalWaitsForInflightSyncThenPurgesPending` 改为 `TestOrderWriteStoreDeleteKeepsTombstoneUntilSQLiteConfirms`：阻塞 target 时 `Delete` 已在本地不可见且 pending 保留，释放 target 后 SQLite 删除并 ACK。把 `TestOrderDeleteDoesNotResurrectAfterLocalCleared` 改为注入 runtime，禁止再写全局测试状态。

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/models -run 'TestOrderWriteStore|TestOrderWriteRuntime' -count=1
```

Expected: FAIL，提示新 runtime/构造签名未定义。

- [ ] **Step 2: 将 OrderWriteStore 收缩为领域适配器**

```go
type OrderWriteStore struct {
	reliable *nosql.ReliableWriteStore[Order]
}

func NewOrderWriteStore(
	identity nosql.ServiceIdentity,
	action persistencetypes.IDataAction,
	config nosql.ReliableWriteStoreConfig,
) (*OrderWriteStore, error) {
	store, _, err := nosql.NewReliableWriteStore[Order](identity, config)
	if err != nil { return nil, err }
	if err := store.UseWriteBehind(nosql.NewModelListWriteBehindTarget(entity.NewModelList[Order](action))); err != nil {
		_ = store.Close(context.Background())
		return nil, err
	}
	return &OrderWriteStore{reliable: store}, nil
}
```

领域适配器只保留订单校验、用户前缀查询、SQLite 可见数据合并和必须的 Flush。`business.OrderService.CreateOrder` 负责 `prepareForInsert` 后调用注入 access 的 `Save`；`DeleteUnpaidOrder` 调用注入 access 的可靠 `Delete` 后执行有界 `ForceSyncAll`，确保成功响应时 SQLite 已删除。`order_persistence.go` 删除依赖全局 store 的 `Order.Insert`/`Order.Delete`；事务内 `UpdateWith(action)` 继续直接使用传入 action，保持支付/撤销的多模型原子性。性能快照直接映射框架 Metrics；删除 `pendingCount`、batcher、guard、目录扫描和全局状态，并在废弃登记中记录两个移除的方法。

`OrderWriteStore.Close(ctx)` 只委托 `reliable.Close(ctx)`，以结构化方式满足 `router.ManagedResource`，models 包不导入 router 包。

- [ ] **Step 3: 新增实例级 typed runtime 并注入业务**

```go
type OrderWriteRuntime struct {
	mu    sync.RWMutex
	store *OrderWriteStore
}

func (r *OrderWriteRuntime) Bind(store *OrderWriteStore) error
func (r *OrderWriteRuntime) Unbind()
func (r *OrderWriteRuntime) Save(ctx context.Context, order *Order) error
```

`OrderWriteRuntime` 还实现 `DeleteAndSync`、`QueryVisibleOrders`、`FlushPendingOrder`、`FlushOrders` 和 `Metrics`，未 Bind 时统一返回 `ErrOrderWriteStoreUnavailable`。

`business.OrderService`、`PaymentService`、`ProductService` 改为保存最小 `OrderWriteAccess` 接口，构造器必须显式接收 access。订单四个 Private API 和 `CreatePayment` 增加构造函数；`ProductManage`、payment record commands 由各自 Manage 实例持有注入的业务服务。`ShopService.Routers()` 创建同一个实例级 runtime，并传给所有需要保存、查询、Flush 或引用完整性检查的业务入口，不从包全局查找。只读商品 Public API 可构造不含订单依赖的 query service，不能为了兼容无参 `NewProductService()` 恢复隐式全局访问。

- [ ] **Step 4: 由 ShopService 注册资源**

`ShopService.Start()` 从 `router.GetContext(contract.ServiceName)` 读取已 claim 的 `DataCenterID/MachineID`，构造：

```go
identity := nosql.ServiceIdentity{
	ServiceName: sc.Service.Name,
	DataCenterID: int64(sc.Config.DataCenterID),
	MachineID: int64(sc.Config.MachineID),
}
```

base path 使用现有数据根目录，最终实例路径由框架追加。创建 store、Bind runtime，然后 `sc.UseResource("order-write-store", store)`。`Stop()` 只先 Unbind 和停止 reference cache，不再直接关闭 Badger。

- [ ] **Step 5: 删除重复实现并运行 04 测试/race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/... ./examples/integration/04-shop-performance -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/04-shop-performance/models ./examples/04-shop-performance/business -count=1
```

Expected: PASS；`rg 'globalOrderWriteStore|orderBatcher|orderWriteGuard|WalkDir' examples/04-shop-performance` 无生产代码命中。

- [ ] **Step 6: 提交 04 迁移**

```bash
rtk git add examples/04-shop-performance
rtk git commit -m "refactor: migrate performance orders to reliable store"
```

### Task 8: 示例 07 迁移并修复 DrainOnce(limit)

**Files:**
- Create: `examples/07-shop-order-scale/order-service/models/transaction/order_write_runtime.go`
- Modify: `examples/07-shop-order-scale/order-service/models/transaction/order_write_store.go`
- Modify: `examples/07-shop-order-scale/order-service/models/models.go`
- Modify: `examples/07-shop-order-scale/order-service/business/local_order_writer.go`
- Modify: `examples/07-shop-order-scale/order-service/business/remote_order_syncer.go`
- Modify: `examples/07-shop-order-scale/order-service/business/order_query.go`
- Modify: `examples/07-shop-order-scale/order-service/business/order_syncer_test.go`
- Modify: `examples/07-shop-order-scale/order-service/business/order_writebehind_target_test.go`
- Modify: `examples/07-shop-order-scale/order-service/api/public/create_order.go`
- Modify: `examples/07-shop-order-scale/order-service/api/public/get_orders.go`
- Modify: `examples/07-shop-order-scale/order-service/service.go`
- Delete: `examples/07-shop-order-scale/order-service/models/transaction/order_batcher.go`
- Delete: `examples/07-shop-order-scale/order-service/models/transaction/order_write_guard.go`
- Delete: `examples/07-shop-order-scale/order-service/models/transaction/order_write_store_lifecycle.go`

- [ ] **Step 1: 写实例隔离和 bounded drain 失败测试**

```go
func TestRemoteOrderSyncerDrainOnceHonorsLimit(t *testing.T) {
	store := &fakeOrderSyncStore{result: nosql.ForceSyncResult{Confirmed: 2, Remaining: 3}}
	syncer := RemoteOrderSyncer{Store: store}
	result, err := syncer.DrainOnce(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, store.limit)
	require.Equal(t, 2, result.Confirmed)
}
```

同批增加并分别断言：

```go
func TestOrderWriteRuntimeInstancesAreIsolated(t *testing.T) // runtime A/B 各自只读到自己的 requestID
func TestOrderWriteStorePendingFollowsFrameworkAck(t *testing.T) // Save 后 Pending=1，确认同步后 Pending=0
func TestRemoteOrderSyncerDoesNotRebindTarget(t *testing.T) // 连续 DrainOnce 两次只调用 ForceSyncBatch
```

- [ ] **Step 2: 运行定向测试确认失败**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/transaction -run 'TestRemoteOrderSyncer|TestOrderWriteRuntime|TestPending' -count=1
```

Expected: FAIL，提示新 store 接口、runtime 或 DrainOnce 返回值未定义。

- [ ] **Step 3: 收缩 07 OrderWriteStore**

```go
type OrderWriteStore struct {
	reliable *nosql.ReliableWriteStore[Order]
}

type OrderWriteAccess interface {
	Save(context.Context, *Order) error
	FindLocalByRequest(context.Context, uint, string) (*Order, error)
	PendingByUser(context.Context, uint) ([]*Order, error)
	ForceSyncBatch(context.Context, int) (nosql.ForceSyncResult, error)
	Metrics() nosql.ReliableWriteMetrics
}
```

订单 `Save` 前保留 validate 和 `prepareForLocalInsert`；pending 数只读 `reliable.Metrics().Pending`。删除业务 `pendingCount`、batcher、guard、磁盘监控和 `RemoveLocalOrder` 门面；同步后本地清理由 `IsSyncAfterDelete` 与框架 ACK 完成。

`OrderWriteStore.Close(ctx)` 委托 `reliable.Close(ctx)`，因此 `ServiceContext` 能直接拥有该领域适配器的生命周期而不形成 import cycle。

- [ ] **Step 4: 注入 LocalOrderWriter、查询和 RemoteOrderSyncer**

```go
type LocalOrderWriter struct { Store OrderWriteAccess }
type RemoteOrderSyncer struct { Store OrderWriteAccess }

func (s RemoteOrderSyncer) DrainOnce(ctx context.Context, limit int) (nosql.ForceSyncResult, error) {
	if s.Store == nil { return nosql.ForceSyncResult{}, ErrOrderWriteStoreUnavailable }
	return s.Store.ForceSyncBatch(ctx, limit)
}
```

`LocalOrderWriter.Accept` 使用注入 store 查询本地幂等键并 Save。`ListOrders` 接收 `OrderWriteAccess` 或 runtime provider，以便合并本副本 pending；MySQL 权威查询仍留在 `order_query.go`。

- [ ] **Step 5: Service 持有 runtime 并注册资源**

`Service` 增加实例字段 `runtime *transaction.OrderWriteRuntime` 和 `ensureRuntime()`；`Routers()` 通过构造函数把同一个 runtime 注入 `CreateOrder`、`GetOrders`。`Start()`：

1. 获取当前 `ServiceContext` 和已 claim 的 identity。
2. 创建 `ReliableWriteStore[Order]`，只绑定一次 `business.OrderWriteBehindTarget{}`。
3. Bind runtime。
4. `sc.UseResource("order-write-store", store)`。
5. 启动 pending ticker 和 Outbox。

`Stop()` 先取消 ticker、等待 done、Unbind runtime；不再调用全局 Stop。`runPendingSyncLoop` 使用注入的 `RemoteOrderSyncer`，每次 `DrainOnce(ctx, 100)` 后仅在 `Confirmed>0` 时 `NotifyOutbox()`。

- [ ] **Step 6: 更新 facade 与测试调用点**

从根 `models/models.go` 删除 `StartOrderWriteStore`、`StopOrderWriteStore`、`AddOrder`、`UseOrderWriteBehind`、`SyncLocalOrders`、`RemoveLocalOrder` 等全局别名。测试直接创建 runtime/store；确需保留的外部导出符号先在 `DEPRECATION_REGISTER.md` 登记并返回 `ErrOrderWriteStoreUnavailable`，不得恢复 global singleton。

- [ ] **Step 7: 运行 07 单元测试、race 和单进程集成**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/order-service/... -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/transaction -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale -count=1 -v
```

Expected: 单元测试 PASS；有 MySQL 时全部 UAT PASS，无凭证时仅明确的 MySQL UAT skip。

- [ ] **Step 8: 检查旧实现已移除并提交**

```bash
rtk rg -n 'globalOrderWriteStore|activeOrderWriteStore|pendingCount|orderBatcher|orderWriteGuard|WalkDir|_ = limit' examples/07-shop-order-scale/order-service
```

Expected: 无生产代码命中。

```bash
rtk git add examples/07-shop-order-scale/order-service
rtk git commit -m "refactor: migrate scaled orders to reliable store"
```

### Task 9: 文档、兼容门禁与 Docker UAT

**Files:**
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/DEPRECATION_REGISTER.md`
- Modify: `examples/04-shop-performance/README.md`
- Modify: `examples/07-shop-order-scale/README.md`
- Modify: `examples/07-shop-order-scale/deploy/README.md`

- [ ] **Step 1: 更新框架使用文档**

加入可直接采用的标准示例：

```go
identity := nosql.ServiceIdentity{
	ServiceName: sc.Service.Name,
	DataCenterID: int64(sc.Config.DataCenterID),
	MachineID: int64(sc.Config.MachineID),
}
store, _, err := nosql.NewReliableWriteStore[Order](identity, config)
if err != nil { return err }
if err := store.UseWriteBehind(target); err != nil { return err }
if err := sc.UseResource("order-write-store", store); err != nil { return err }
```

明确：Save 是 insert/update；Delete 是可靠 tombstone；`PurgeLocal` 只用于运维修复；AutoMachineID 变化不会自动接管其他目录；MySQL 不可达时 07 的远程幂等探测仍 fail closed。

- [ ] **Step 2: 更新配置矩阵和废弃登记**

配置矩阵说明 `ReliableWriteStoreConfig` 每个字段的运行时读取位置；废弃表登记被移除的 04/07 全局门面、替代 API 和兼容期限。不得把 `BadgerDBConfig.AutoSync` 描述为业务手动 ticker 的开关。

- [ ] **Step 3: 运行格式化、全套定向测试和静态门禁**

```bash
rtk gofmt -w pkg/server/router pkg/persistence/database/nosql examples/04-shop-performance examples/07-shop-order-scale/order-service
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/router ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/order-service/... -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/router ./pkg/persistence/database/nosql ./examples/04-shop-performance/models ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/transaction -count=1
rtk proxy ./scripts/check-logging.sh
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

Expected: 全部 PASS；日志检查无 payload、SQL、token 或对象 dump 新违规；release contract 无未登记破坏。

- [ ] **Step 4: 运行 07 四组 Docker UAT**

```bash
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATBuyerRoleFlow$' -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATSupplierRoleFlow$' -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATAdminRoleFlow$' -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerComposeOrderScaleUAT$' -count=1 -v
```

Expected: buyer、supplier、admin、compose 全部 PASS；测试结束后 `docker ps` 不保留本轮项目容器。

- [ ] **Step 5: 最终残余搜索**

```bash
rtk rg -n 'globalOrderWriteStore|activeOrderWriteStore|orderBatcher|orderWriteGuard|filepath\.WalkDir|_ = limit' examples/04-shop-performance examples/07-shop-order-scale/order-service
rtk rg -n 'ReliableWriteStore|PurgeLocal|ForceSyncBatch|UseResource' docs/codex examples/04-shop-performance/README.md examples/07-shop-order-scale/README.md
```

Expected: 第一条无生产实现命中；第二条覆盖框架入口、可靠删除、bounded sync 和生命周期说明。

- [ ] **Step 6: 提交文档与验收更新**

```bash
rtk git add docs/codex/FRAMEWORK_USAGE_GUIDE.md docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md docs/codex/DEPRECATION_REGISTER.md examples/04-shop-performance/README.md examples/07-shop-order-scale/README.md examples/07-shop-order-scale/deploy/README.md
rtk git commit -m "docs: document reliable write store lifecycle"
```

## 完成标准

1. 04/07 生产代码不再包含业务自建 batcher、guard、磁盘目录扫描、pending 副本计数或全局 store registry。
2. `ReliableWriteStore` 的 Save/Update/Delete、Group Commit、背压、指标、bounded sync 和 Close 均有框架单测与 race 覆盖。
3. 同一 key 的 Save/Delete 顺序可证明，Delete tombstone 在远端确认前不丢失，`PurgeLocal` 不进入业务接口。
4. 目录严格包含 service、DataCenterID 和 MachineID；不同 `ServiceContext` 不共享生命周期和 typed handle。
5. 07 `DrainOnce(limit)` 实际限制处理数量，target 只绑定一次，框架 ACK 后 pending 指标自然下降。
6. 定向测试、race、日志门禁、release contract、真实 MySQL 集成和四组 Docker UAT 全部通过；环境不可用时必须报告具体缺口，不得宣称端到端完成。
