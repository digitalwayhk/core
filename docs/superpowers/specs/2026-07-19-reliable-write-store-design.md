# ReliableWriteStore 框架收敛设计

## 1. 背景

`PrefixedBadgerDB.UseWriteBehind(WriteBehindTarget)` 已把 pending、ACK、重试、关闭恢复和同步指标收敛到框架，但示例 04/07 仍各自维护 Group Commit、背压、磁盘扫描、写入指标和 store 生命周期。07 还保留一份不会随框架 ACK 递减的 `pendingCount`，可能在真实积压已经归零后继续触发错误背压。

本设计把可复用的本地可靠写能力继续收敛为框架级 `ReliableWriteStore[T]`，让业务只保留模型校验、策略配置和远端领域事务。

## 2. 目标

1. 框架统一管理可靠本地提交、跨请求 Group Commit、pending/磁盘/并发背压、有界同步、指标和关闭恢复。
2. `ServiceContext` 成为 store 的唯一生命周期 owner，不再使用进程级 store registry 或业务包全局 singleton。
3. 04/07 删除重复 batcher、guard、磁盘扫描和 pending 状态。
4. 07 的订单与 Outbox 同 MySQL 事务语义继续由 `OrderWriteBehindTarget` 负责。
5. 保持 Badger 成功持久化后才向 API 返回 accepted，远端失败时 pending 不丢失。

## 3. 非目标

1. 不把订单支付、取消、查询、`UserID+RequestID` 幂等或 Outbox 事务下沉到通用 persistence 框架。
2. 不让不同 `ServiceContext` 自动共享或接管彼此的可靠 pending。
3. 不在关闭时无限等待 MySQL，也不承诺 exactly-once；远端仍必须幂等。
4. 不把 Manage/ModelList 重新引入高吞吐业务写路径。

## 4. 总体架构

```text
ServiceContext
└── ResourceManager
    └── ReliableWriteStore[T]
        ├── PrefixedBadgerDB[T]
        ├── BatchCommitter[T]
        ├── WriteAdmissionController
        ├── StorageMetrics
        └── WriteBehindTarget[T]
```

### 4.1 ServiceContext

`ServiceContext` 提供实例级资源注册和逆序关闭，不提供进程全局 store 查找。资源所有权跟随当前 `ServiceContext`，同进程的多个服务实例互不覆盖。

```go
type ManagedResource interface {
	Close(context.Context) error
}

func (sc *ServiceContext) UseResource(name string, resource ManagedResource) error
```

重复名称、nil 资源或已经开始关闭后的注册必须返回错误。关闭时使用有界 context，按注册逆序调用 `Close`，并通过 `errors.Join` 汇总错误。

### 4.2 ReliableWriteStore

`ReliableWriteStore[T]` 是业务高吞吐本地可靠写入口。它组合 `PrefixedBadgerDB[T]`，但不理解订单、MySQL 或 Outbox。

```go
type ReliableWriteStore[T types.IModel] struct {
	db        *PrefixedBadgerDB[T]
	batcher   *BatchCommitter[T]
	admission *WriteAdmissionController
}
```

主要能力：

- `Add(ctx, item)`：通过背压后进入 Group Commit，Badger 持久成功才返回。
- `AddBatch(ctx, items)`：显式批量可靠提交。
- `UseWriteBehind(target)`：一次性绑定远端 target。
- `ForceSyncBatch(ctx, limit)`：最多处理指定数量 pending。
- `ForceSyncAll(ctx)`：有界循环同步全部当前 pending。
- `Metrics()`：返回本地提交、batch、pending、磁盘、背压和远端同步指标。
- `Close(ctx)`：拒绝新写、排空已接受本地写、停止 worker 并保留未同步 pending。

### 4.3 BatchCommitter

`BatchCommitter[T]` 把多个并发 `Add` 聚合为一次 `PrefixedBadgerDB.BatchInsert`。它是通用并发组件，不保存业务状态。

配置包括最大 batch、收集窗口和队列容量。每个调用等待自己所在批次的 Badger 提交结果；同一事务失败时该批所有调用收到相同错误。关闭时停止接收新请求并等待已接受请求完成。

### 4.4 WriteAdmissionController

背压机制进入框架，阈值仍由服务配置：

- 最大并发写入数和获取超时；
- pending soft/hard limit；
- soft limit 持续时间；
- Badger 磁盘硬上限。

pending 必须读取 `PrefixedBadgerDB.GetCachedPendingSyncCount()`，不得维护第二份计数。磁盘用量通过 Badger LSM/vlog size 暴露，不再周期性 `filepath.WalkDir`。

统一可识别错误：

```go
ErrWriteStoreClosed
ErrWriteRejectedConcurrency
ErrWriteRejectedPending
ErrWriteRejectedDisk
ErrWriteBehindNotBound
```

### 4.5 WriteBehindTarget

`WriteBehindTarget[T]` 保持远端领域边界。target 返回错误时仍可携带 `ConfirmedKeys`；框架先 ACK 已确认 key，再传播错误。

07 的 `OrderWriteBehindTarget` 继续在一个 MySQL 事务中完成订单幂等 upsert 和 Outbox 写入。订单查询、支付和取消仍留在订单领域包。

## 5. 路径规则

store 必须在 `ServiceContext` 完成 MachineID claim 后创建。有效目录由框架解析为：

```text
<basePath>/<serviceName>/dc-<DataCenterID>/machine-<MachineID>
```

例如：

```text
/data/pending/shop-order/dc-1/machine-3
/data/pending/shop-order/dc-1/machine-4
```

`serviceName` 必须经过安全目录片段规范化；空名称、越界路径片段和负数标识直接拒绝。

本设计明确接受以下边界：AutoMachineID 重启后若取得不同 MachineID，将进入新目录。Docker 中每个副本仍使用独立持久卷保证物理隔离和重启恢复；框架不会自动接管其他 MachineID 目录，以免消费仍存活副本的 pending。

## 6. 有界同步

```go
type ForceSyncResult struct {
	Confirmed int
	Remaining int
}

func (s *ReliableWriteStore[T]) ForceSyncBatch(
	ctx context.Context,
	limit int,
) (ForceSyncResult, error)
```

- `limit <= 0` 返回配置错误。
- 一次最多读取和处理 `limit` 条 pending。
- 已经提交的 target 事务不因 context 随后取消而回滚。
- 部分成功时 ACK `ConfirmedKeys`，结果返回实际确认数和剩余数，同时保留 target 错误。
- `ForceSyncAll` 重复调用有界批次；无进展、context 取消或 target 错误时立即停止。
- 07 的 `DrainOnce(limit)` 必须调用该接口，不再忽略 limit。

## 7. 生命周期与关闭顺序

服务停止顺序：

1. `Service.Stop` 停止业务 ticker，不再触发新同步。
2. `ReliableWriteStore` 标记 closing，拒绝新写入。
3. `BatchCommitter` 完成已经接受的本地 Badger 提交。
4. `PrefixedBadgerDB` 停止自动 worker，等待当前同步批次在 context 内结束。
5. 未同步 pending 保留在磁盘；关闭返回 `PendingSyncError`。
6. `ServiceContext` 继续关闭 Outbox、EventBridge、MQ、gRPC 和其他资源。

关闭不主动要求清空远端 pending，不以 MySQL 可用作为本地安全停止的前提。重复关闭必须幂等并返回稳定结果。

## 8. 依赖注入

业务不得通过全局 registry 获取 store。服务保存 typed handle，并通过小接口传给 Router/Business：

```go
type OrderWriteStore interface {
	Add(context.Context, *models.Order) error
	ForceSyncBatch(context.Context, int) (nosql.ForceSyncResult, error)
}
```

路由在构造时获得 provider 或业务 runtime 引用；Service 启动后绑定 typed store。`ServiceContext` 只负责资源生命周期，不承担业务依赖定位。

## 9. 04/07 迁移

### 9.1 示例 04

- 删除本地 `orderBatcher`、`orderWriteGuard`、磁盘扫描和 store 重复指标。
- 使用 `ReliableWriteStore[Order]` 和 `ModelListWriteBehindTarget`。
- 保留 04 的 SQLite 兼容演示、订单本地查询和性能基准语义。

### 9.2 示例 07

- 删除 `globalOrderWriteStore`、`activeOrderWriteStore` 和一行式全局门面。
- 删除业务层 `pendingCount`、batcher、guard 和磁盘扫描。
- 使用 `ReliableWriteStore[Order]` 与 `OrderWriteBehindTarget`。
- `RemoteOrderSyncer.DrainOnce(limit)` 调用 `ForceSyncBatch`，成功后唤醒标准 Outbox。
- `order_persistence.go` 和 `order_query.go` 保留领域实现，可按职责合并但不进入框架。

仓库内调用全部迁移为注入式访问。确有外部兼容价值的导出入口先登记废弃并返回明确错误，不允许退回隐藏全局状态。

## 10. 测试与验收

### 10.1 框架单元测试

- Group Commit 聚合、批次失败传播、关闭并发和 panic 转换。
- pending soft/hard limit、持续积压、并发超时和磁盘硬上限。
- 路径包含 service、DataCenterID 和 MachineID，非法片段 fail closed。
- `ForceSyncBatch(limit)` 不超量，部分成功正确 ACK，context 可取消。
- `ServiceContext` 资源隔离、重复注册、逆序关闭、关闭错误汇总和幂等。

### 10.2 示例与集成测试

- 04/07 本地可靠提交、同步失败保留、指标和关闭恢复。
- `go test -race` 覆盖 persistence、ServiceContext 和订单业务。
- 07 单进程 MySQL 集成。
- `SHOP_RUN_DOCKER_UAT=1` buyer、supplier、admin 和完整 compose UAT。
- 发布契约、日志规范和文档关键字检查。

## 11. 兼容性

新增框架 API 采用 additive 方式。删除或废弃示例导出入口前更新 `DEPRECATION_REGISTER.md`，运行 public API/release contract。`PrefixedBadgerDB` 现有 `UseWriteBehind`、`EnableWriteBehind` 和 `SetSyncDB` 保持编译兼容；新业务默认入口切换到 `ReliableWriteStore[T]`。
