# 示例 07 订单水平扩展 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `examples/07-shop-order-scale`，演示订单服务在业务增长后通过多实例水平扩展、本地可靠写、异步同步远程权威库、标准事件投递和本地投影提升吞吐并保持多角色业务一致。

**Architecture:** 07 继承 06 的 `shop-user`、`shop-supplier`、`shop-order` 服务边界，并把 `shop-order` 扩展为可多实例部署。每个 order 实例先把订单事实可靠落到本地 pending store，再异步批量同步到同一个 order 远程权威库；最终数据库按业务域拆分，不按技术实例拆分。事件发布仍使用 `sc.UseOutbox(models.OutboxStore{})`，订阅使用 `sc.SubscribeEvent(event.Subscription{...})`，缓存只放在外部入口 facade。

**Tech Stack:** Go、Digitalway Core RouterInfo/ServiceContext/ServiceResolver、go-zero、SQLite 示例远程库、Badger 本地可靠写、可配置 ClusterProvider、Redis Streams、gRPC/mTLS、TestToken、真实进程集成测试。

---

## 一、设计结论

### 0. 自动水平扩展是 07 的硬性验收

07 不只是业务示例，也必须补齐并验证框架层所有会影响自动水平扩展的能力。凡是会导致 `docker compose --scale shop-order=N` 或编排系统增加副本后不可用、不一致、冲突或无法恢复的问题，都必须在本示例中修复、测试和写入能力文档。

必须覆盖的自动扩展能力包括：

```text
AutoMachineID 跨实例自动租约
ServiceInstanceID 自动生成并可观测
ClusterProvider 可配置注册发现
ServiceResolver 发现多副本并负载均衡
本地 pending 目录按副本隔离
order 副本不暴露固定宿主机业务端口
共享远程 order 权威库
Outbox/Inbox 和事件消费组在多副本下幂等
规则/缓存变更能广播到所有副本
缩容或重启后 pending 能恢复或安全 drain
```

### 1. 07 的核心边界

07 不演示“订单库技术分片”。水平扩展的是服务实例与本地写入能力，远程权威库仍按业务域保留为统一 order 数据库：

```text
shop-user
  -> shop-order 实例 A/B/C
       -> 本地可靠 pending store
       -> 异步同步到远程 order 权威库
       -> 标准 Outbox 发布事件
  -> shop-order 共享远程权威库查询订单

shop-supplier
  -> 消费订单事件形成 SupplierOrder 投影
```

### 2. 直接套用 06 的问题

- 06 的 `shop-order` 是单实例权威，事务锁和 SQLite 都是本地进程能力。
- 直接把 06 的本地库模式复制成多个 order 实例，并把每个实例本地库都当成最终事实库，会造成技术分片：幂等、查询和管理语义都会分裂。
- 正确的水平扩展中，多个 order 实例必须共享同一个远程 order 权威库；管理 API 是一个逻辑管理面，不是每个实例各自管理一份本地事实。
- 07 必须把“本地可靠写”与“远程权威库”分开：本地只负责高 TPS 接收、恢复和重试；远程库负责最终唯一约束和业务事实收敛。

### 3. 直接套用 04 的问题

- 04 的 Badger + Group Commit 是单服务性能优化，后台同步目标仍是本服务本地 SQLite。
- 07 要把同步目标抽象成 `RemoteOrderStore`，并让远程库具备幂等 upsert 能力。
- Badger pending 是未同步业务事实，不是缓存；请求只有本地可靠持久成功后才能返回。

### 4. 运行时实例信息

每个服务的基础模型继续包含 `TraceID`。07 增加运行时戳模型，用于本地 pending、Outbox、Inbox、同步状态和投影排查：

```go
type ServiceBaseModel struct {
    TraceID     string
    ServiceName string
}

type RuntimeStampedModel struct {
    *ServiceBaseModel
    ServiceInstanceID string
    ServiceInstanceIP string
}
```

核心业务事实以 `TraceID` 为主；`ServiceInstanceID` 可作为审计字段保存，`ServiceInstanceIP` 不参与业务判断。

---

## 二、文件结构

### 新增示例目录

```text
examples/07-shop-order-scale/
  README.md
  contract/
    service_names.go
    event_names.go
    errors.go
  dto/
    event/
    order/
    supplier/
    user/
  user-service/
    service.go
    api/manage/
    api/private/
    api/public/
    business/
    models/
  supplier-service/
    service.go
    api/manage/
    api/public/
    business/
    models/
  order-service/
    service.go
    api/manage/
    api/public/
    business/
    models/
  main/
    all-in-one/
    user/
    supplier/
    order/
  deploy/
    docker-compose.yml
    README.md
```

### 每个服务的模型目录

```text
models/
  models.go
  common/
    service_base_model.go
    runtime_stamped_model.go
    database_names.go
  basedata/
  transaction/
  projection/
  internal/store/
  schema/
```

根 `models.go` 只做兼容门面，不放具体模型和持久化实现。

### 每个服务的 Manage 目录

```text
api/manage/
  manage.go
  common/
    service_manage.go
  basedata/
    base_data_manage.go
  transaction/
    transaction_manage.go
  projection/
    projection_manage.go
```

服务级权限、owner 限域、分页、审计和日志只写在 `common.ServiceManage[T]` 或更靠近根部的抽象基座；具体 Manage 不重复这些横切逻辑。

### 水平扩展后的 Manage 语义

水平扩展不会把 Manage 数据拆成多份。`shop-order` 多实例部署时，Manage 只能暴露一个逻辑管理面：

```text
方案 A：只让一个 order 管理入口暴露 Manage，但它查询/修改的是共享远程 order 权威库。
方案 B：多个 order 实例都注册同一套 Manage，但任何实例处理请求时都访问同一个远程 order 权威库，因此管理结果一致。
```

07 第一版推荐 `方案 B`：多个 `shop-order` 实例都可以注册同一套 Manage，但管理员查询、支付类型、订单规则和订单状态命令都必须落到共享远程 order 权威库，不能只改某个实例的本地 pending store。

---

## 三、实施任务

### Task 1: 固化 07 设计文档与 06 缓存文档清理

**Files:**
- Create: `examples/07-shop-order-scale/README.md`
- Modify: `examples/06-shop-microservices/README.md`
- Modify: `.codex/skills/use-digitalway-core/SKILL.md`

- [ ] **Step 1: 写 07 README 初稿**

写入 `examples/07-shop-order-scale/README.md`，内容必须包含：

```markdown
# 示例 07：订单服务水平扩展

本示例演示商城订单量增长后，`shop-order` 通过多实例水平扩展、本地可靠写、异步同步远程 order 权威库和标准 EventBridge 事件保持吞吐与一致性。

最终数据库按业务域拆分，不按技术实例拆分。多个 order 实例共享同一个远程 order 权威库；每个实例拥有自己的本地 pending store，用于可靠接收、崩溃恢复、批量同步和故障重试。
```

- [ ] **Step 2: 清理 06 README 内部权威服务缓存表述**

把 `examples/06-shop-microservices/README.md` 的缓存表改成入口 facade 缓存模型：

```text
User 供应商/商品 facade：30 秒，SupplierChanged/ProductChanged 主动失效
User 支付类型 facade：30 秒，PaymentTypeChanged 主动失效
User 本人订单 Private：10 秒，Order/Payment 事件按 UserID 失效
Supplier/Order 内部权威 Public：不重复 UseCache
```

- [ ] **Step 3: 更新能力文件**

在 `.codex/skills/use-digitalway-core/SKILL.md` 增加 07 规则：

```markdown
- 示例 07 这类水平扩展示例必须区分服务水平扩展、业务拆库和技术分片。默认不按服务实例拆最终业务库；多实例先写本地可靠 pending，再异步同步到同一个业务域远程权威库。
- 多实例服务的 pending、Outbox、Inbox、同步状态和投影必须记录 TraceID、ServiceName、ServiceInstanceID；ServiceInstanceIP 只用于诊断，不参与业务判断。
```

- [ ] **Step 4: 验证文档没有旧方向残留**

Run:

```bash
rg -n "技术分片|每个 order.*独立.*权威|内部权威.*UseCache|api/call" examples/07-shop-order-scale examples/06-shop-microservices .codex/skills/use-digitalway-core
```

Expected: 不出现“内部权威 Public 重复缓存”或“api/call”方向的新增描述；如果出现，只保留用于解释反例的明确禁止语句。

- [ ] **Step 5: Commit**

```bash
git add examples/07-shop-order-scale/README.md examples/06-shop-microservices/README.md .codex/skills/use-digitalway-core/SKILL.md
git commit -m "docs(example): 规划 07 订单水平扩展"
```

### Task 2: 建立 07 contract 与 DTO

**Files:**
- Create: `examples/07-shop-order-scale/contract/service_names.go`
- Create: `examples/07-shop-order-scale/contract/event_names.go`
- Create: `examples/07-shop-order-scale/contract/errors.go`
- Create: `examples/07-shop-order-scale/dto/event/metadata.go`
- Create: `examples/07-shop-order-scale/dto/order/order.go`
- Create: `examples/07-shop-order-scale/dto/order/order_changed.go`
- Create: `examples/07-shop-order-scale/dto/supplier/product_snapshot.go`
- Create: `examples/07-shop-order-scale/dto/user/address_snapshot.go`

- [ ] **Step 1: 写 contract 服务名**

`service_names.go` 必须定义稳定服务名：

```go
package contract

const (
    UserServiceName       = "shop-user"
    SupplierServiceName   = "shop-supplier"
    OrderServiceName      = "shop-order"
)
```

- [ ] **Step 2: 写事件名**

`event_names.go` 必须定义：

```go
package contract

const EventSchemaVersion = 1

const (
    SubjectOrderChanged      = "shop.order.changed"
    SubjectOrderRuleChanged  = "shop.order_rule.changed"
    SubjectSupplierChanged   = "shop.supplier.changed"
    SubjectProductChanged    = "shop.product.changed"
    SubjectPaymentTypeChanged = "shop.payment_type.changed"
)

const (
    EventOrderAccepted      = "OrderAccepted"
    EventOrderSynced        = "OrderSynced"
    EventOrderCreated       = "OrderCreated"
    EventOrderStatusChanged = "OrderStatusChanged"
    EventPaymentChanged     = "PaymentChanged"
    EventOrderRuleChanged   = "OrderRuleChanged"
    EventSupplierChanged    = "SupplierChanged"
    EventProductChanged     = "ProductChanged"
    EventPaymentTypeChanged = "PaymentTypeChanged"
)
```

- [ ] **Step 3: 写 DTO**

DTO 只表达跨服务 JSON 契约，不嵌入持久化模型。`order.Order` 至少包含：

```go
type Order struct {
    OrderID       uint   `json:"orderID"`
    UserID        uint   `json:"userID"`
    SupplierID    uint   `json:"supplierID"`
    ProductID     uint   `json:"productID"`
    Quantity      int    `json:"quantity"`
    OrderStatus   string `json:"orderStatus"`
    PaymentStatus string `json:"paymentStatus"`
    TraceID       string `json:"traceID"`
}
```

- [ ] **Step 4: 跑编译检查**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk go test ./examples/07-shop-order-scale/contract ./examples/07-shop-order-scale/dto/... -run '^$'
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add examples/07-shop-order-scale/contract examples/07-shop-order-scale/dto
git commit -m "feat(example): 添加 07 服务契约和 DTO"
```

### Task 3: 建立 order 服务基础模型、远程权威库和本地 pending store

**Files:**
- Create: `examples/07-shop-order-scale/order-service/models/models.go`
- Create: `examples/07-shop-order-scale/order-service/models/common/service_base_model.go`
- Create: `examples/07-shop-order-scale/order-service/models/common/runtime_stamped_model.go`
- Create: `examples/07-shop-order-scale/order-service/models/common/database_names.go`
- Create: `examples/07-shop-order-scale/order-service/models/basedata/order_rule.go`
- Create: `examples/07-shop-order-scale/order-service/models/transaction/order.go`
- Create: `examples/07-shop-order-scale/order-service/models/transaction/local_pending_order.go`
- Create: `examples/07-shop-order-scale/order-service/models/transaction/outbox_record.go`
- Create: `examples/07-shop-order-scale/order-service/models/internal/store/local_store.go`
- Create: `examples/07-shop-order-scale/order-service/models/internal/store/remote_store.go`
- Create: `examples/07-shop-order-scale/order-service/models/schema/schema.go`

- [ ] **Step 1: 写基础模型**

`common.ServiceBaseModel` 提供库名和 TraceID：

```go
type ServiceBaseModel struct {
    *types.Model
    TraceID     string
    ServiceName string
}

func (m *ServiceBaseModel) GetLocalDBName() string {
    return LocalDatabaseName
}

func (m *ServiceBaseModel) GetRemoteDBName() string {
    return RemoteDatabaseName
}
```

- [ ] **Step 2: 写运行时戳模型**

```go
type RuntimeStampedModel struct {
    *ServiceBaseModel
    ServiceInstanceID string
    ServiceInstanceIP string
}
```

- [ ] **Step 3: 写订单事实和 pending 模型**

`transaction.Order` 保存远程权威事实；`transaction.LocalPendingOrder` 保存实例本地可靠 pending，包含 `SyncStatus`、`RetryCount`、`LastError`、`Payload`。

- [ ] **Step 4: 写 order 规则基础资料模型**

`basedata.OrderRule` 保存订单服务的共享业务规则，属于远程 order 权威库的基础资料，不属于某个 order 实例本地配置：

```go
type OrderRule struct {
    *common.ServiceBaseModel
    RuleCode       string          `json:"ruleCode"`
    RuleName       string          `json:"ruleName"`
    MinQuantity    int             `json:"minQuantity"`
    MaxQuantity    int             `json:"maxQuantity"`
    MaxOrderAmount decimal.Decimal `json:"maxOrderAmount"`
    Enabled        bool            `json:"enabled"`
    RuleRevision   int             `json:"ruleRevision"`
}
```

默认规则建议：

```text
RuleCode: default
MinQuantity: 1
MaxQuantity: 100
MaxOrderAmount: 99999
Enabled: true
```

管理员通过 Manage 修改该规则后写入共享远程 order 权威库，并写 Outbox 发布 `OrderRuleChanged`；所有 order 实例订阅该事件后失效本地规则缓存，下一次下单读取新规则。

- [ ] **Step 5: 写本地与远程 store**

本地 store 使用当前实例本地 SQLite/Badger 能力；远程 store 使用独立 `RemoteDatabaseName`，并提供：

```go
func UpsertRemoteOrderWith(action persistencetypes.IDataAction, order *transaction.Order) (*transaction.Order, error)
func FindRemoteOrderByIdempotencyWith(action persistencetypes.IDataAction, userID uint, requestID string) (*transaction.Order, error)
func GetEnabledOrderRuleWith(action persistencetypes.IDataAction, ruleCode string) (*basedata.OrderRule, error)
```

远程库必须以 `UserID + IdempotencyKey` 收敛重复请求。

- [ ] **Step 6: 写模型单测**

Create: `examples/07-shop-order-scale/order-service/models/transaction/order_idempotency_test.go`
Create: `examples/07-shop-order-scale/order-service/models/basedata/order_rule_test.go`

测试内容：

```go
func TestRemoteOrderIdempotency(t *testing.T) {
    // 同一 userID + requestID 写两次，只能得到同一订单。
}

func TestOrderRuleStoredInRemoteAuthority(t *testing.T) {
    // 修改 default 规则后，任意 order 实例读取远程规则都能得到同一份配置。
}
```

- [ ] **Step 7: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk go test ./examples/07-shop-order-scale/order-service/models/... -count=1
```

Expected: PASS。

- [ ] **Step 8: Commit**

```bash
git add examples/07-shop-order-scale/order-service/models
git commit -m "feat(example): 添加 07 订单本地与远程模型"
```

### Task 4: 实现 order 本地可靠写与远程同步器

**Files:**
- Create: `examples/07-shop-order-scale/order-service/business/order_command.go`
- Create: `examples/07-shop-order-scale/order-service/business/local_order_writer.go`
- Create: `examples/07-shop-order-scale/order-service/business/remote_order_syncer.go`
- Create: `examples/07-shop-order-scale/order-service/business/order_reference_cache.go`
- Create: `examples/07-shop-order-scale/order-service/business/order_rule_cache.go`
- Create: `examples/07-shop-order-scale/order-service/business/order_syncer_test.go`
- Create: `examples/07-shop-order-scale/order-service/business/order_rule_cache_test.go`

- [ ] **Step 1: 写失败测试**

`order_syncer_test.go` 验证：

```go
func TestOrderSyncerRetriesRemoteFailure(t *testing.T) {
    // 远程库第一次失败时 pending 保留。
    // 远程库恢复后同步成功。
    // 同步成功后 pending 标记为 synced。
}
```

- [ ] **Step 2: 写 `LocalOrderWriter`**

`LocalOrderWriter.Accept(command)` 必须：

```text
1. 校验 requestID、UserID、ProductID、Quantity、Address
2. 生成或接收 OrderID
3. 构造订单事实 payload
4. 写入本地 pending store
5. 本地 fsync/事务成功后返回 OrderID
```

- [ ] **Step 3: 写 `RemoteOrderSyncer`**

`RemoteOrderSyncer.DrainOnce(ctx, limit)` 必须：

```text
1. 读取 pending
2. 调用 RemoteOrderStore upsert
3. 写本地 OutboxRecord
4. 标记 pending synced
5. 失败时增加 RetryCount 和 LastError
```

Outbox 由业务事实同步成功后写入，再由 `sc.UseOutbox(models.OutboxStore{})` 统一发布。

- [ ] **Step 4: 写引用缓存**

迁移 04 的 `OrderReferenceCache` 思路，只缓存下单所需最小供应商/商品快照。缓存失效来自 `SupplierChanged/ProductChanged` 事件。

- [ ] **Step 5: 写订单规则缓存和下单规则校验**

`OrderRuleCache` 从远程 order 权威库读取 `OrderRule` 快照，所有 order 实例本地缓存该规则。下单进入本地 pending 前必须校验：

```text
Quantity >= MinQuantity
Quantity <= MaxQuantity
TotalAmount <= MaxOrderAmount
Enabled == true
```

`OrderRuleChanged` 事件到达后只失效规则缓存，不直接修改本地 pending。缓存 miss 时重新读取远程规则，保证任一 order 实例都能看到管理员最新设置。

- [ ] **Step 6: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk go test ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/... -count=1
```

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add examples/07-shop-order-scale/order-service/business examples/07-shop-order-scale/order-service/models
git commit -m "feat(example): 实现 07 订单本地可靠写同步"
```

### Task 5: 实现 order Public API、Manage 和服务启动

**Files:**
- Create: `examples/07-shop-order-scale/order-service/service.go`
- Create: `examples/07-shop-order-scale/order-service/api/public/create_order.go`
- Create: `examples/07-shop-order-scale/order-service/api/public/cancel_order.go`
- Create: `examples/07-shop-order-scale/order-service/api/public/create_payment.go`
- Create: `examples/07-shop-order-scale/order-service/api/public/get_payment_types.go`
- Create: `examples/07-shop-order-scale/order-service/api/manage/manage.go`
- Create: `examples/07-shop-order-scale/order-service/api/manage/common/service_manage.go`
- Create: `examples/07-shop-order-scale/order-service/api/manage/basedata/base_data_manage.go`
- Create: `examples/07-shop-order-scale/order-service/api/manage/basedata/order_rule_manage.go`
- Create: `examples/07-shop-order-scale/order-service/api/manage/transaction/transaction_manage.go`

- [ ] **Step 1: 写受限 Public**

所有 order Public 都必须：

```go
router.WithServiceName(contract.OrderServiceName)
router.WithInternalCallers(contract.UserServiceName)
```

HTTP 伪造调用方不能通过。

- [ ] **Step 2: 写 `Start`**

`service.Start(sc)` 只做标准声明：

```go
func (s *ShopOrderService) Start(sc *router.ServiceContext) {
    s.sc = sc
    sc.UseOutbox(models.OutboxStore{})
    s.startRemoteSyncer(sc)
    s.subscribeCacheInvalidation(sc)
}
```

不得手写 Outbox worker，也不得使用 `SubscribeExternalControl`。

- [ ] **Step 3: 写 Manage 基座**

`api/manage/common.ServiceManage[T]` 统一处理管理员权限、日志、分页和 TraceID 字段，不允许具体 Manage 重复通用权限。

order Manage 的数据源必须是共享远程 order 权威库，不能查询某个实例的本地 pending store 当作管理事实。本地 pending 只允许作为运维诊断只读信息展示，不参与管理员业务查询和业务命令判断。

- [ ] **Step 4: 写 OrderRule Manage**

`OrderRuleManage` 继承 `basedata.BaseDataManage[*models.OrderRule]`，只允许管理员修改。修改成功后必须写 `OrderRuleChanged` Outbox，由所有 order 实例通过标准订阅失效规则缓存。该 Manage 用于演示：在任意一个 order 管理入口修改规则后，所有水平扩展的 order 实例下单校验立即按新规则执行。

- [ ] **Step 5: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk go test ./examples/07-shop-order-scale/order-service/... -count=1
```

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add examples/07-shop-order-scale/order-service
git commit -m "feat(example): 添加 07 订单服务 API"
```

### Task 6: 复制并调整 user/supplier 服务边界

**Files:**
- Create/Modify: `examples/07-shop-order-scale/user-service/**`
- Create/Modify: `examples/07-shop-order-scale/supplier-service/**`

- [ ] **Step 1: 从 06 迁移服务结构**

迁移后必须保持：

```text
user-service:
  Manage: User/Address
  Public facade: GetSuppliers/GetProducts/GetPaymentTypes
  Private: AddOrder/GetOrders/CancelOrder/CreatePayment
  WebSocket: 只对买家订单订阅

supplier-service:
  Manage: Supplier/Product/SupplierOrder
  Public: GetSuppliers/GetProducts，仅内部调用
  Private: 无
```

- [ ] **Step 2: 去掉内部权威服务重复缓存**

只保留 user facade 的 `UseCache`。supplier/order 权威 Public 不重复缓存。

- [ ] **Step 3: supplier 投影消费 order 事件**

Supplier 通过 `sc.SubscribeEvent(event.Subscription{Subject: contract.SubjectOrderChanged, Reliable: true, Handler: ...})` 消费全部订单变化，按 `EventID` Inbox 幂等，按 `OrderID` 更新 `SupplierOrder`。

- [ ] **Step 4: user 消费 order 事件**

User 通过 `sc.SubscribeEvent` 失效本人订单缓存，并向对应买家 WebSocket 投递订单事件。

- [ ] **Step 5: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk go test ./examples/07-shop-order-scale/user-service/... ./examples/07-shop-order-scale/supplier-service/... -count=1
```

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add examples/07-shop-order-scale/user-service examples/07-shop-order-scale/supplier-service
git commit -m "feat(example): 添加 07 用户和供应商服务"
```

### Task 7: 实现 order 服务统一查询路径

**Files:**
- Create: `examples/07-shop-order-scale/order-service/api/public/get_orders.go`
- Modify: `examples/07-shop-order-scale/order-service/api/manage/transaction/order_manage.go`
- Modify: `examples/07-shop-order-scale/order-service/models/internal/store/remote_store.go`
- Modify: `examples/07-shop-order-scale/order-service/business/order_command.go`

- [ ] **Step 1: 写远程权威库订单查询**

`remote_store.go` 必须提供分页查询远程 order 权威库的能力，所有 order 实例调用同一套远程库查询，不读取本实例本地 pending 当作管理事实：

```go
func ListRemoteOrdersWith(action persistencetypes.IDataAction, filter OrderQueryFilter, page, size int) ([]*transaction.Order, int64, error)
```

- [ ] **Step 2: 写 Public GetOrders**

`GetOrders` 只允许 `shop-user` 和 `shop-supplier` 内部调用。`shop-user` 调用时必须带可信数字 `UserID`，只能返回该用户订单；`shop-supplier` 调用时必须带可信数字 `SupplierID`，只能返回该供应商订单。

- [ ] **Step 3: 写 Manage 查询**

管理员通过 `order-service/api/manage/transaction/order_manage.go` 查询全部订单，查询来源是共享远程 order 权威库。该 Manage 是订单水平扩展后的统一管理查询面，不能按 order 实例拆成多份本地查询。

- [ ] **Step 4: 写一致性测试**

新增或补充 order 服务测试，断言任意 order 实例处理 `GetOrders` 或 Manage Search 时，都读取同一个远程 order 权威库；本地 pending 只作为 accepted 状态或运维诊断，不冒充最终订单事实。

- [ ] **Step 5: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk go test ./examples/07-shop-order-scale/order-service/... -count=1
```

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add examples/07-shop-order-scale/order-service
git commit -m "feat(example): 添加 07 订单统一查询路径"
```

### Task 8: 补齐自动水平扩展框架能力

**Files:**
- Modify: `pkg/server/config/clusterconfig.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/cluster/provider_local.go`
- Modify: `pkg/server/cluster/provider_redis.go`
- Modify: `pkg/server/cluster/provider_etcd.go`
- Modify: `pkg/server/cluster/provider_consul.go`
- Modify: `pkg/server/cluster/node.go`
- Modify: `pkg/server/router/serviceresolver.go`
- Test: `pkg/server/config/clusterconfig_test.go`
- Test: `pkg/server/router/servicecontext_registry_test.go`
- Test: `pkg/server/router/serviceresolver_test.go`
- Test: `pkg/server/cluster/provider_redis_test.go`

- [ ] **Step 1: 修改配置能力**

`Cluster.Claim.AutoMachineID=true` 必须从 rejected 改为 supported。`AutoDataCenterID` 第一版仍可保持 rejected；07 只要求同一 `DataCenterID` 下自动分配 `MachineID`。

- [ ] **Step 2: 实现启动时自动租约**

`ServiceContext` 初始化 Snowflake 之前，如果 `AutoMachineID=true`，必须通过当前配置选择的 `ClusterProvider` 为 `ServiceName + DataCenterID` 申请一个唯一 `MachineID`。申请成功后写回 `con.MachineID`，再初始化 Snowflake 和节点注册。

语义要求：

```text
同服务、同 DataCenterID 的多个实例不能拿到同一 MachineID
Provider 不支持 AutoMachineID 时必须 fail closed
MachineIDMax 限制可用槽位
实例正常 Stop 时释放租约
实例异常退出后依赖 Provider TTL 释放租约
```

- [ ] **Step 3: Provider 复用现有分配能力**

Local/Redis/Etcd/Consul Provider 已有或接近已有 `AllocateMachineID` 能力，07 应补齐统一接口或适配层。实现必须保持 Provider 可替换，不能让业务代码直接依赖 Redis。

- [ ] **Step 4: 写框架测试**

测试必须覆盖：

```text
AutoMachineID=true 时两个同名服务实例获得不同 MachineID
MachineIDMax 被占满时启动失败
Stop 后 MachineID 可复用
Provider 不支持自动分配时返回明确错误
AutoDataCenterID=true 仍按当前能力矩阵拒绝
```

- [ ] **Step 5: 补齐自动扩展相关运行时能力**

除 MachineID 外，框架层还必须确认这些能力可用于自动水平扩展：

```text
ServiceInstanceID：每个 ServiceContext 启动时生成稳定的本实例 ID，并写入 Cluster NodeInfo；同一进程/容器重启应生成新运行实例 ID。
ServiceResolver：同一 ServiceName 多节点时可发现全部健康副本，并通过既有负载均衡策略分配调用。
ClusterProvider：Provider 只由配置决定，业务代码不能读取 Redis/Etcd/Consul 地址。
NodeInfo：必须包含足够排查字段，例如 ServiceName、DataCenterID、MachineID、ServiceInstanceID、Address、Port、GRPCPort、Status。
Stop/Shutdown：服务下线时注销节点，停止接新请求，并给本地 pending drain 留出生命周期入口。
```

- [ ] **Step 6: 写自动扩展框架测试**

新增或补充测试：

```text
同一 ServiceName 三个实例注册后，ServiceResolver 能看到三个健康节点
三个实例的 ServiceInstanceID 不重复
三个实例的 DataCenterID + MachineID 不重复
停止一个实例后，ServiceResolver 不再选择该节点
重新启动一个实例后，MachineID 可通过 lease 重新分配
```

- [ ] **Step 7: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/config ./pkg/server/router ./pkg/server/cluster -run 'AutoMachineID|MachineID|ClusterConfig' -count=1
```

Expected: PASS。

- [ ] **Step 8: 更新能力文档**

更新 `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`：

```text
ServerConfig.Cluster.Claim.AutoMachineID: supported
ServerConfig.Cluster.Claim.AutoDataCenterID: rejected
```

同时更新 `.codex/skills/use-digitalway-core/SKILL.md`，写入：

```text
自动水平扩展示例必须启用 AutoMachineID=true，并验证 ClusterProvider lease、ServiceInstanceID、多副本发现、本地 pending 目录隔离、共享远程权威库和优雅下线恢复。
```

- [ ] **Step 9: Commit**

```bash
git add pkg/server/config pkg/server/router pkg/server/cluster docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md .codex/skills/use-digitalway-core
git commit -m "feat(cluster): 支持自动水平扩展基础能力"
```

### Task 9: 增加 main、配置和多实例部署

**Files:**
- Create: `examples/07-shop-order-scale/main/all-in-one/main.go`
- Create: `examples/07-shop-order-scale/main/user/main.go`
- Create: `examples/07-shop-order-scale/main/supplier/main.go`
- Create: `examples/07-shop-order-scale/main/order/main.go`
- Create: `examples/07-shop-order-scale/deploy/docker-compose.yml`
- Create: `examples/07-shop-order-scale/deploy/README.md`

- [ ] **Step 1: all-in-one 启动所有服务**

all-in-one 用于开发和同进程集成测试，必须启动 user、supplier、两个 order 实例语义配置。WebSocket 只开启 user。

- [ ] **Step 2: 三进程/多进程启动**

部署文件至少包含一组固定双实例演示：

```text
shop-user
shop-supplier
shop-order-a
shop-order-b
redis
```

`shop-order-a/b` 使用相同 `ServiceName=shop-order`，开启 `Cluster.Claim.AutoMachineID=true`，不同本地数据目录、同一个远程 order 库配置。测试和运行日志必须能证明两个实例自动获得不同 `MachineID`。

服务注册和发现不能写死为 Redis。07 的配置必须通过 Core 的 `ClusterProvider` 选择发现实现：本地 all-in-one 使用本地 Resolver，局域网/内网可以使用框架已有的注册发现机制，Docker 示例默认可使用 Redis Provider，后续也可替换为其它中间件 Provider。业务代码只能依赖 `ServiceResolver`，不能直接依赖 Redis 地址、容器名或静态服务列表。

配置必须显式打开自动水平扩展相关能力：

```text
Cluster.Claim.AutoMachineID=true
Cluster.Claim.MachineIDMax >= 3
Cluster.Provider 由配置选择
ServiceInstanceID 未配置时由运行时生成
LocalPendingDir 必须支持从实例 ID、hostname 或容器 ID 派生
```

- [ ] **Step 3: order 不暴露宿主机 HTTP 端口**

order 实例只在 Docker 内网监听，外部不映射业务 ports；管理员 Manage 通过受控入口访问任意 order 实例时，必须读取和修改共享远程 order 权威库，因此结果一致。

- [ ] **Step 4: Run compose config**

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml config
```

Expected: `shop-order-a`、`shop-order-b` 没有宿主机 `ports` 映射。

- [ ] **Step 5: 写 Docker 水平扩容约束**

`deploy/README.md` 必须说明 07 支持容器编排层水平扩容，但不内置自动创建/删除容器的 HPA 控制器。Docker Compose 第一版使用固定 `shop-order-a/b` 便于 UAT 断言；可选扩展可以提供同一 `shop-order` 服务模板配合 `docker compose up --scale shop-order=N` 的说明。

水平扩容必须满足：

```text
所有 order 副本使用相同 ServiceName=shop-order
每个副本拥有唯一 ServiceInstanceID 和 MachineID
每个副本拥有独立本地 pending 目录
所有副本共享同一个远程 order 权威库
所有副本通过配置选择的 ClusterProvider 注册发现，不在业务代码中绑定 Redis
order 副本不暴露宿主机业务端口，内部调用走 ServiceResolver + gRPC/mTLS
缩容时先停止接新请求，再尽量 drain 本地 pending；未 drain 完的 pending 必须可在实例重启后恢复
```

如果选择 Docker Compose `--scale`，必须使用 `AutoMachineID=true` 通过启动时 lease 自动获得唯一 `MachineID`；本地 pending 目录可以由环境变量、hostname、实例序号或容器 ID 派生，不能多个副本共享同一个目录。

- [ ] **Step 6: 写 Docker scale 配置**

除固定 `shop-order-a/b` UAT 外，`deploy/README.md` 和 compose 示例必须说明或提供可 scale 的 `shop-order` 服务模板：

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml up --scale shop-order=3
```

scale 模式下不得通过服务名写死 `MachineID`。`MachineID` 必须来自 `AutoMachineID=true` 的 Provider lease；本地 pending 目录必须按副本隔离。

- [ ] **Step 7: Commit**

```bash
git add examples/07-shop-order-scale/main examples/07-shop-order-scale/deploy
git commit -m "feat(example): 添加 07 多实例启动配置"
```

### Task 10: 单进程集成测试

**Files:**
- Create: `examples/integration/07-shop-order-scale/buyer_role_test.go`
- Create: `examples/integration/07-shop-order-scale/supplier_role_test.go`
- Create: `examples/integration/07-shop-order-scale/admin_role_test.go`
- Create: `examples/integration/07-shop-order-scale/websocket_test.go`
- Create: `examples/integration/07-shop-order-scale/order_sync_test.go`

- [ ] **Step 1: 买家闭环测试**

买家测试必须覆盖：

```text
注册/模拟登录
维护用户资料
新增地址
查询供应商/商品/支付类型
下单
支付
查询本人订单
其他买家不能看到该订单
```

- [ ] **Step 2: 供应商闭环测试**

供应商测试必须覆盖：

```text
维护供应商资料
新增商品
上架商品
看到自己的订单投影
看不到其他供应商订单
被使用商品不能删除，只能禁用
```

- [ ] **Step 3: 管理员闭环测试**

管理员测试必须覆盖：

```text
配置支付类型
设置订单规则，例如最小下单数量
查询全局订单
禁用供应商
管理订单状态
```

- [ ] **Step 4: WebSocket 测试**

买家 WebSocket 必须覆盖真实登录、真实 RouterInfo 路径订阅、订单事件投递、其他买家隔离、未认证订阅失败。

- [ ] **Step 5: 本地 pending 与远程同步测试**

模拟远程库失败：

```text
下单返回成功
pending 保留
远程恢复
syncer drain 成功
远程库出现订单
Outbox 发布事件
订单最终可查，supplier/user 本地投影最终收敛
```

- [ ] **Step 6: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale -count=1 -v
```

Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add examples/integration/07-shop-order-scale
git commit -m "test(example): 添加 07 单进程集成测试"
```

### Task 11: 多进程 UAT 和水平扩展验证

**Files:**
- Create: `examples/integration/07-shop-order-scale-multi-process/buyer_role_test.go`
- Create: `examples/integration/07-shop-order-scale-multi-process/supplier_role_test.go`
- Create: `examples/integration/07-shop-order-scale-multi-process/admin_role_test.go`
- Create: `examples/integration/07-shop-order-scale-multi-process/order_scale_test.go`
- Create: `examples/integration/07-shop-order-scale-multi-process/websocket_test.go`

- [ ] **Step 1: 买家多进程角色测试**

真实启动 user、supplier、order-a、order-b、redis。买家下单多次，断言请求分布到两个 order 实例，并且本人订单查询完整。

测试必须读取两个 order 实例的运行配置或注册节点信息，确认它们在 `AutoMachineID=true` 下自动获得不同 `MachineID`，而不是测试脚本显式写死两个 MachineID。

同时断言：

```text
两个 order 实例的 ServiceInstanceID 不重复
两个 order 实例的本地 pending 目录不相同
ServiceResolver 至少选择过两个不同 order 节点
```

- [ ] **Step 2: 供应商多进程角色测试**

供应商上架商品后，买家订单经过不同 order 实例创建，供应商仍能在 supplier 服务看到自己的完整订单投影。

- [ ] **Step 3: 管理员多进程角色测试**

管理员通过任一 order 实例的 Manage 查询全部订单，不受订单最初由哪个 order 实例接收影响；如果请求打到任一 order 实例的管理命令，最终也必须修改同一个远程 order 权威库。

- [ ] **Step 4: 幂等测试**

同一买家、同一 `requestID` 重复下单，远程权威库只能存在一笔最终订单；如果两个 order 实例都产生本地 pending，远程 upsert 必须收敛为同一 OrderID 或返回同一已存在订单。

- [ ] **Step 5: 订单规则跨实例生效测试**

管理员通过任意一个 order 管理入口把默认规则的 `MinQuantity` 从 `1` 改成 `3` 后，两个 order 实例都必须按新规则拒绝 `Quantity=1` 的下单，并允许 `Quantity=3` 的下单。测试必须证明不是只修改了某一台 order 实例的本地配置：

```text
修改规则前：Quantity=1 下单成功
修改规则后：连续多次下单请求分布到 order-a/order-b，Quantity=1 都失败
修改规则后：Quantity=3 下单成功
OrderRuleChanged 事件至少被两个 order 实例消费并失效本地规则缓存
```

- [ ] **Step 6: 可信内部调用测试**

断言：

```text
User -> Order 使用 gRPC
Order -> Supplier 使用 gRPC
普通 HTTP 不能调用 Order 受限 Public
错误 mTLS 证书不能调用 Order
order 不暴露宿主机端口
```

- [ ] **Step 7: Docker scale 自动 MachineID 测试**

如果当前测试环境支持 Docker Compose，增加可选但推荐的真实扩容测试：

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml up --scale shop-order=3
```

Expected:

```text
3 个 shop-order 副本均注册为 ServiceName=shop-order
3 个副本的 MachineID 自动分配且互不重复
3 个副本拥有独立本地 pending 目录
shop-user 通过 ServiceResolver 能发现并调用 3 个副本
```

- [ ] **Step 8: 缩容和重启恢复测试**

模拟停止一个 order 副本：

```text
停止前：该副本已注册且可被 ServiceResolver 选择
停止后：该副本从发现列表消失，ServiceResolver 不再选择它
重启后：副本重新注册，获得合法 MachineID，pending 能恢复或 drain
```

- [ ] **Step 9: Run**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -count=1 -v
```

Expected: PASS。

- [ ] **Step 10: Race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/integration/07-shop-order-scale-multi-process -count=1 -v
```

Expected: PASS。

- [ ] **Step 11: Commit**

```bash
git add examples/integration/07-shop-order-scale-multi-process
git commit -m "test(example): 添加 07 多进程水平扩展 UAT"
```

### Task 12: 性能基准和可观测性

**Files:**
- Create: `examples/integration/07-shop-order-scale/benchmark_add_order_test.go`
- Create: `examples/integration/07-shop-order-scale/benchmark_compare_06_07_test.go`
- Create: `docs/codex/SHOP_ORDER_SCALE_BENCHMARK_REPORT.md`
- Modify: `scripts/benchmark-shop-examples.sh`

- [ ] **Step 1: 添加 AddOrder benchmark**

benchmark 对比：

```text
06 单 order
07 单 order 实例 + 本地可靠写
07 双 order 实例 + 本地可靠写
```

输出：

```text
TPS
p50/p95/p99
pending 最大值
远程库同步收敛耗时
事件投影收敛耗时
错误率
```

- [ ] **Step 2: 添加 06 与 07 同机同口径对比 benchmark**

新增 `benchmark_compare_06_07_test.go`，在 07 完成后必须可直接评估 07 相对 06 的性能提升。对比口径固定为：

```text
06：examples/06-shop-microservices，单 shop-order，同样商品、供应商、支付类型、买家和地址数据
07-single：examples/07-shop-order-scale，单 shop-order 实例，本地可靠写 + 远程权威库同步
07-scale：examples/07-shop-order-scale，两个 shop-order 实例，本地可靠写 + 同一远程权威库同步
```

benchmark 必须输出每组的原始指标和提升比例：

```text
add_order_tps
add_order_tps_improvement_vs_06_pct
p50_ms
p95_ms
p99_ms
p95_change_vs_06_pct
p99_change_vs_06_pct
remote_sync_convergence_ms
projection_convergence_ms
pending_max
error_rate_pct
```

结果判断不得只看请求返回 TPS，还必须确认：

```text
远程 order 权威库最终订单数正确
同一 UserID + requestID 没有重复订单
本地 pending 最终排空或保持在可解释阈值内
supplier / user 本地投影最终收敛
```

- [ ] **Step 3: 添加性能快照**

order 服务提供只读性能快照函数：

```go
func GetOrderScalePerformanceSnapshot() OrderScalePerformanceSnapshot
```

包含本地 pending、同步成功/失败、重试、远程 upsert 冲突、Outbox 发布数量。

- [ ] **Step 4: 写性能报告**

`docs/codex/SHOP_ORDER_SCALE_BENCHMARK_REPORT.md` 明确：

```text
结果只用于同机同次比较
必须包含 06、07-single、07-scale 三组数据
必须给出 07-single vs 06、07-scale vs 06 的 TPS 和 p95/p99 提升或退化百分比
必须解释远程同步收敛时间和投影收敛时间，不能只报告入口请求 TPS
07 不承诺跨机器固定提升倍数
本地 pending 是业务事实
远程库是最终权威
```

- [ ] **Step 5: Run**

```bash
SHOP_BENCH_CONCURRENCIES=100,500 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale -run '^$' -bench '^BenchmarkAddOrder$' -benchtime=30s -count=1 -timeout=10m
```

Expected: benchmark 完成，无 pending 无界增长。

- [ ] **Step 6: Run 06 vs 07 对比 benchmark**

```bash
SHOP_BENCH_CONCURRENCIES=100,500 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale -run '^$' -bench '^BenchmarkCompareShop06And07AddOrder$' -benchtime=30s -count=1 -timeout=15m
```

Expected: benchmark 输出 06、07-single、07-scale 三组数据，并写入或可复制到 `docs/codex/SHOP_ORDER_SCALE_BENCHMARK_REPORT.md`；07 指标如果没有提升，报告必须保留真实结果并说明瓶颈，不允许为了好看改口径。

- [ ] **Step 7: Commit**

```bash
git add examples/integration/07-shop-order-scale/benchmark_add_order_test.go examples/integration/07-shop-order-scale/benchmark_compare_06_07_test.go docs/codex/SHOP_ORDER_SCALE_BENCHMARK_REPORT.md scripts/benchmark-shop-examples.sh
git commit -m "test(example): 添加 07 订单水平扩展基准"
```

### Task 13: 最终质量门禁

**Files:**
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Modify: `.codex/skills/use-digitalway-core/SKILL.md`

- [ ] **Step 1: 更新框架使用指南**

在 `docs/codex/FRAMEWORK_USAGE_GUIDE.md` 示例表中加入 07：

```markdown
| 订单水平扩展 | `examples/07-shop-order-scale` | AutoMachineID、多 order 实例、本地可靠写、远程权威库同步、标准 Outbox、共享规则、多进程 UAT |
```

- [ ] **Step 2: 全库旧实现扫描**

Run:

```bash
rg -n "api/call|SubscribeExternalControl|UseOutbox\\([^m]|内部权威.*UseCache|每个实例.*最终.*订单库|显式.*MachineID=1|显式.*MachineID=2|写死.*Redis" examples/07-shop-order-scale examples/integration/07-shop-order-scale examples/integration/07-shop-order-scale-multi-process docs/codex .codex/skills/use-digitalway-core
```

Expected: 无新增旧实现路径；如果命中，只允许出现在明确禁止或反例说明中。

- [ ] **Step 3: 注释检查**

Run:

```bash
./scripts/check-logging.sh
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/... ./examples/integration/07-shop-order-scale ./examples/integration/07-shop-order-scale-multi-process -count=1
```

Expected: PASS。

- [ ] **Step 4: release contract**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add docs/codex/FRAMEWORK_USAGE_GUIDE.md .codex/skills/use-digitalway-core
git commit -m "docs(example): 收录 07 订单水平扩展能力"
```

---

## 四、验收标准

- `shop-order` 可以启动至少两个实例，服务名相同，实例 ID、IP、本地数据目录不同。
- `shop-order` 多副本必须使用 `Cluster.Claim.AutoMachineID=true`，并由 ClusterProvider lease 自动获得互不重复的 `MachineID`；测试必须覆盖固定双实例和 Docker scale 场景。
- 两个 order 实例共享同一个远程 order 权威库。
- order 水平扩展后 Manage 仍是一个逻辑管理面：管理员查询和管理命令都落到共享远程 order 权威库，不能只处理某个实例的本地事实。
- order 支持共享业务规则 Manage，例如默认下单最小数量；管理员在一个管理入口修改规则后，所有 order 实例必须通过共享远程库和 `OrderRuleChanged` 事件看到新规则，并在下单校验中一致生效。
- 订单创建在本地 pending 可靠落盘成功后返回。
- 远程库短暂失败不会丢订单，恢复后 pending 能同步成功。
- 同一 `UserID + requestID` 不会产生重复最终订单。
- Outbox 由 `sc.UseOutbox(models.OutboxStore{})` 发布，业务代码不自建事件 worker。
- 订阅统一使用 `sc.SubscribeEvent(event.Subscription{...})`，`EventType` 可为空表示订阅整个 Subject。
- user facade 是供应商/商品/支付类型/本人订单缓存入口；supplier/order 内部权威 Public 不重复缓存。
- 买家 WebSocket 真实测试覆盖订阅、投递、隔离和异常。
- 多进程 UAT 按买家、供应商、管理员拆文件，每个角色可单独运行。
- 07 完成后必须运行 06 vs 07 基准性能测试，报告 06、07-single、07-scale 的同机同口径 TPS、p50/p95/p99、远程同步收敛、投影收敛和错误率。
- 性能报告必须计算 07 相对 06 的提升或退化百分比；如果 07 没有达到预期提升，保留真实结果并说明瓶颈，不能调整口径掩盖问题。
- 所有新增文件有中文文件级注释；所有导出类型、函数、方法有中文注释。

---

## 五、已知风险和处理策略

### 1. 远程库与本地返回的最终一致性窗口

请求在本地可靠持久后返回，远程库可能稍后才可见。07 必须通过状态表达清楚：

```text
Accepted -> Synced -> Created/Paid/Cancelled
```

买家查询应能看到本地 accepted 状态，最终由事件投影更新为 synced/created。

### 2. 重复 pending 的处理

round-robin 下同一 `requestID` 可能进入不同实例。远程库必须用 `UserID + requestID` 唯一约束收敛，重复同步时返回已存在订单并标记本地 pending 完成。

### 3. Outbox 发布时机

07 第一版建议远程同步成功后写 Outbox 并发布 `OrderCreated`，避免下游投影看到远程库尚未收敛的最终事实。若要发布 `OrderAccepted`，它必须明确是“本地接收成功”，不能被当作最终订单创建。

### 4. 实例 IP 的使用边界

`ServiceInstanceIP` 只用于本地 pending、Outbox、Inbox、投影和同步状态排查，不参与订单所有权、路由、权限、幂等或业务状态判断。

---

## 六、自审结果

- 需求覆盖：已覆盖 04 性能能力、06 多服务边界、07 order 水平扩展、本地可靠写、远程权威库、多角色 UAT、WebSocket 和能力文档更新。
- 性能覆盖：已明确 07 完成后必须新增 06 vs 07 对比 benchmark，并在报告中给出同机同口径提升或退化百分比。
- 旧方向排除：明确排除了按 order 实例拆最终数据库、内部权威 Public 重复缓存、自建事件 worker、`api/call`。
- 类型一致性：规划中统一使用 `TraceID`、`ServiceName`、`ServiceInstanceID`、`ServiceInstanceIP`、`UserID + requestID` 幂等键、`sc.UseOutbox` 和 `sc.SubscribeEvent`。
