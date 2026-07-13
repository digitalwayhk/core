# PrefixedBadgerDB 可靠写回修复计划

> **面向智能体开发者：** 必须按 TDD 逐节实施：先写失败测试并确认失败原因，再写最小实现。每节独立验证、独立提交；NATS JetStream 仅在本计划完成后编写接入指南，不在本任务开发。

**目标：** 保留 `PrefixedBadgerDB` 的本地低延迟与批量同步价值，同时消除损坏重建、TTL、非持久模式、重复 pending 计数和关闭时静默积压造成的数据丢失风险。

**架构：** 明确区分普通缓存与 write-behind。默认损坏策略 fail closed；只有显式纯缓存配置可丢弃重建。新增返回错误的 write-behind 绑定入口，旧 `SetSyncDB` 保持编译兼容但把绑定错误保存到实例，后续写入和关闭均可观察。

**技术栈：** Go 1.26、BadgerDB v3、现有 `ModelList`/`IDataAction`、go-zero `logx`、Go table-driven/race tests。

## 公共兼容边界

- 保留 `NewSharedBadgerDB`、`SetSyncDB`、`Set`、`BatchInsert`、`Close` 的现有签名。
- 新增 `EnableWriteBehind(*entity.ModelList[T]) error`，新代码只使用该入口。
- `SetSyncDB(list)` 调用相同验证；失败时不启用同步，并保存稳定错误，使后续 `Set`、`BatchInsert`、同步和 `Close` 返回该错误，避免日志后继续运行。
- `SetSyncDB(nil)` 继续用于停止接受新的待同步写入；已有队列不会被删除。
- `SyncQueueItem` 只允许新增带 `omitempty` 的兼容字段，不删除或重命名现有 JSON 字段。
- write-behind 语义为 at-least-once；不宣称 exactly-once。

## 18.1 损坏恢复策略

**文件：**

- 修改：`pkg/persistence/database/nosql/badgerdbconfig.go`
- 修改：`pkg/persistence/database/nosql/sharedbadgermanager.go`
- 创建：`pkg/persistence/database/nosql/sharedbadger_corruption_test.go`

- [x] 添加稳定策略值：

```go
type CorruptionPolicy string

const (
    CorruptionPolicyFail       CorruptionPolicy = "fail"
    CorruptionPolicyResetCache CorruptionPolicy = "reset_cache"
)
```

- [x] `BadgerDBConfig.CorruptionPolicy` 默认使用 `fail`；`Validate` 只接受闭集值。
- [x] 将损坏处理提取为可测试决策函数：`shouldResetCorruptedCache(config BadgerDBConfig) bool`。
- [x] `SharedBadgerManager` 仅在 `reset_cache` 时调用 `clearBadgerData`；默认返回保留原始目录的错误。
- [x] 测试默认策略、非法策略、显式 cache reset；断言默认路径不调用删除函数。

**完成记录：** 默认 production/fast 配置均为 `fail`，普通与共享 Badger 构造路径只在显式 `reset_cache` 时清理损坏目录。聚焦测试通过。

**RED/GREEN：**

```bash
go test ./pkg/persistence/database/nosql -run 'Test.*CorruptionPolicy' -count=1
```

## 18.2 安全启用 write-behind

**文件：**

- 修改：`pkg/persistence/database/nosql/sharedbadger.go`
- 创建：`pkg/persistence/database/nosql/sharedbadger_writebehind_test.go`

- [x] 新增稳定错误：

```go
var (
    ErrUnsafeWriteBehindConfig = errors.New("unsafe write-behind config")
    ErrWriteBehindTTL          = errors.New("write-behind entries cannot use ttl")
)
```

- [x] `EnableWriteBehind` 在绑定前要求 `SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicy=fail`；不满足时使用 `%w` 返回 `ErrUnsafeWriteBehindConfig`。
- [x] 保留 `SetSyncDB`，内部委托安全入口并保存绑定结果；不得在验证失败后启动 goroutine。
- [x] `Set` 在 write-behind 已启用时拒绝 `ttl>0`，返回 `ErrWriteBehindTTL`。
- [x] `BatchLoad` 仍表示远端数据写入本地，不进入同步队列。
- [x] 测试 production 配置可启用、fast/reset-cache 配置被拒绝、旧入口错误可观察、pending TTL 被拒绝、纯缓存 TTL 保持兼容。

**完成记录：** 新增安全绑定入口；旧入口保持编译兼容并让错误在后续写入可见。聚焦测试及 nosql 全包通过。

**RED/GREEN：**

```bash
go test ./pkg/persistence/database/nosql -run 'Test.*(WriteBehind|PendingTTL)' -count=1
```

## 18.3 队列计数与损坏项处理

**文件：**

- 修改：`pkg/persistence/database/nosql/sharedbadger.go`
- 修改：`pkg/persistence/database/nosql/sharedbadger_syncqueue_test.go`

- [x] 在写事务内判断同步索引是否已存在；只有新建索引时增加 `pendingCountCache`。
- [x] `BatchInsert` 按实际新建同步索引数增加计数，不按输入项数增加。
- [x] 同一 key 在确认前重复更新时，队列长度和内存计数保持 1；确认后均归零。
- [x] `getUnsyncedBatch` 遇到无法反序列化的数据项时返回带 key 的错误，不删除数据、不删除同步索引、不静默越过。
- [x] 重启初始化继续以持久同步索引重建内存计数。

**完成记录：** 单条与批量写入均按新建持久索引计数；损坏项保留现场并阻断批次。聚焦测试 `-count=20` 和 nosql 全包通过。

**RED/GREEN：**

```bash
go test ./pkg/persistence/database/nosql -run 'Test.*(PendingCount|MalformedSync)' -count=20
go test -race ./pkg/persistence/database/nosql -run 'Test.*PendingCount' -count=1
```

## 18.4 关闭与积压可观察性

**文件：**

- 修改：`pkg/persistence/database/nosql/sharedbadger.go`
- 创建：`pkg/persistence/database/nosql/sharedbadger_close_pending_test.go`

- [x] 新增可供 `errors.As` 使用的错误：

```go
type PendingSyncError struct {
    Prefix string
    Count  int
}
```

- [x] `CloseWithTimeout` 停止 worker 后读取持久同步索引；存在积压时返回 `PendingSyncError`，重复关闭返回相同结果。
- [x] 绑定失败与 pending 错误使用 `errors.Join`，不丢失任一原因。
- [x] 关闭日志只记录 prefix、pending、timeout，不记录模型 payload。
- [x] 测试无积压关闭、积压关闭、重复关闭、绑定失败关闭和 `errors.As/errors.Is`。

**完成记录：** 关闭结果持久保存并可重复读取；pending 和绑定失败均可通过标准错误链判断。聚焦测试及 nosql 全包通过。

**RED/GREEN：**

```bash
go test ./pkg/persistence/database/nosql -run 'Test.*Close.*(Pending|WriteBehind)' -count=20
go test -race ./pkg/persistence/database/nosql -run 'Test.*Close' -count=1
```

## 18.5 总验收与文档

**文件：**

- 修改：`docs/codex/PROJECT_REVIEW_ACTION_PLAN.md`
- 修改：`docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- 修改：`.codex/skills/use-digitalway-core/references/core-backend-api.md`

- [x] 记录 cache 与 write-behind 的边界、at-least-once 和远端幂等要求。
- [x] 明确 write-behind 不适用于资金流水、审计事件等不可合并事件；这些场景使用唯一事件 ID 的 JetStream/outbox。
- [x] 运行完整门禁：

```bash
gofmt -w pkg/persistence/database/nosql/*.go
go test ./pkg/persistence/database/nosql -count=1
go test -race ./pkg/persistence/database/nosql -count=1
./scripts/test.sh persistence-unit
go vet ./pkg/persistence/database/nosql/...
./scripts/test.sh release-contract
```

**完成记录：** nosql 全包、race、`persistence-unit`、vet 与 `release-contract` 均通过；框架指南、skill 引用、CHANGELOG、废弃登记和项目总台账已同步。JetStream 接入说明已单独写入 `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md`，未修改 NATS 业务实现。

## 完成后输出的 NATS JetStream 指南

本任务不实现 NATS。代码验收完成后已在 `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md` 说明：

1. 服务端默认模式：API 等待 JetStream publish ACK，durable pull consumer 批量写远端数据库，成功后 ACK。
2. 离线节点模式：本地 write-behind 只负责断网暂存，恢复后发布 JetStream，不直接写远端数据库。
3. 主数据库事务模式：业务表与 outbox 同事务提交，再由 CDC/relay 发布 JetStream。
4. 三种模式都要求稳定事件 ID、消费者幂等、MaxDeliver/DLQ、backlog/oldest-age 指标和有界关闭。
