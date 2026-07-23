# 07 订单可靠写装配可读性设计

## 背景

`examples/07-shop-order-scale/order-service/service.go` 已经使用框架 `ReliableWriteStore` 承担 Group Commit、背压、pending ACK、磁盘指标和有界同步，但 `Service.Start` 仍把路径解析、Badger 配置、store 创建、target 绑定、runtime 绑定、资源注册、Outbox 注册和同步循环启动放在同一个方法中。读者需要同时理解多个生命周期边界，才能判断一行配置或绑定的作用。

`models/transaction/order_write_store.go` 和 `order_write_runtime.go` 已经收敛为较薄的领域适配层，但当前注释主要描述“做什么”，对“本地可靠确认与远程 MySQL 汇合的区别”、“runtime 与 store 的生命周期分工”和“为什么不暴露 Admin 物理删除能力”说明不足。

## 目标

1. 让 `Service.Start` 只展示订单服务启动的五个高层步骤。
2. 把 store 创建、绑定/注册、Outbox/同步设施启动拆成三个单一职责方法。
3. 用逐逻辑步骤的中文注释说明配置意图、数据语义、并发保护和失败回滚。
4. 保持现有写入、查询、同步、关闭和错误语义不变。

## 非目标

- 不修改框架 `ReliableWriteStore` 公共 API。
- 不同步重构 `examples/04-shop-performance`。
- 不引入新的全局 store registry 或第二套生命周期管理。
- 不将注释扩大为对每个赋值、`return` 或 nil 判断的字面翻译。
- 不改变 MySQL 不可达时远程幂等探测的 fail-closed 策略。

## Service 装配设计

### `Service.Start`

`Start` 仅保留以下顺序：

1. 取得订单服务 `ServiceContext`。
2. 确保 MySQL 权威库 schema 已就绪。
3. 调用 `newOrderWriteStore(sc)` 创建当前副本的本地可靠 store。
4. 调用 `bindOrderWriteStore(sc, store)` 完成 target、runtime 和资源生命周期绑定。
5. 调用 `startOrderInfrastructure(sc)` 注册 Outbox 并启动 bounded pending 同步循环。

`Start` 仍使用现有 panic 策略拒绝半启动服务，不将启动错误降级为日志后继续运行。

### `newOrderWriteStore`

签名：

```go
func (s *Service) newOrderWriteStore(sc *router.ServiceContext) (*transaction.OrderWriteStore, error)
```

职责：

- 通过 `orderPendingBasePath` 取得挂载根目录，实际实例目录仍由 `ReliableWriteStore` 追加 `<service>/dc-N/machine-N`。
- 使用 `DefaultProductionConfig` 建立 Badger 配置，禁用示例噪声日志和框架自动同步。
- 从 `ServiceContext` 生成 `ServiceIdentity`，保证水平副本不共用同一 Badger 目录。
- 集中设置 Group Commit、准入控制、pending 阈值、磁盘上限和关闭超时。
- 返回未绑定 target、未注册到 runtime 的 `OrderWriteStore`。

该方法不启动 goroutine，也不取得 store Admin handle。

### `bindOrderWriteStore`

签名：

```go
func (s *Service) bindOrderWriteStore(
	sc *router.ServiceContext,
	store *transaction.OrderWriteStore,
) error
```

绑定顺序：

1. `store.UseWriteBehind(business.OrderWriteBehindTarget{})` 绑定唯一 MySQL 汇合 target。
2. `s.ensureRuntime().Bind(store)` 让路由和 business 访问当前实例 store。
3. `sc.UseResource("order-write-store", store)` 将关闭职责交给 `ServiceContext`。

失败回滚：

- target 绑定失败：关闭新建 store。
- runtime 绑定失败：关闭 store，不覆盖已存在的 store。
- 资源注册失败：先 `Unbind`，再关闭 store，避免业务继续取得一个不受托管或已关闭的资源。

`Unbind` 只断开业务访问，`Close` 才停止本地提交并关闭 Badger prefix；两者不得混为同一职责。

### `startOrderInfrastructure`

签名：

```go
func (s *Service) startOrderInfrastructure(sc *router.ServiceContext) error
```

职责：

- 通过 `sc.UseOutbox(models.OutboxStore{})` 注册标准 MySQL Outbox 发布能力。
- 使用已绑定 runtime 启动 `startPendingSync`。
- 保证同步 goroutine 不会早于 store 资源注册启动。

## Models 说明设计

### `order_write_store.go`

- 文件级注释明确它是订单领域到框架 `ReliableWriteStore` 的适配层，不实现第二套 batcher、背压或 ACK。
- `OrderWriteAccess` 的每个方法注释标明本地或远程语义，并说明该最小接口为何适合 API/business 注入。
- `NewOrderWriteStore` 说明 Admin handle 被有意丢弃：普通业务不得使用 `PurgeLocal` 跳过远程删除语义。
- `Save` 按“nil/ID 校验 -> 领域校验 -> 本地元数据准备 -> Badger 可靠提交”注释。返回 nil 只代表本地可恢复，不代表 MySQL 已可见。
- `PendingByUser` 说明前缀扫描是第一层缩小范围，`UserID` 再校验是防御性边界，倒序排列保持最新订单优先。
- `ForceSyncBatch` 说明 `limit` 是当轮最大 pending 数，不是全量 drain 暗示。
- `Close` 说明只关闭本地 prefix，不在优雅关闭时隐式访问 MySQL。
- `prepareForLocalInsert` 按时间戳来源、精度归一、hash 准备和 `AcceptedAt` 保留解释业务意图。

### `order_write_runtime.go`

- 文件级注释说明 runtime 是 `Service` 与预先构造的 API/business 之间的实例级稳定引用。
- `Bind` 说明拒绝静默 rebind，避免已构造路由在运行中切换到不明 store。
- `Unbind` 说明它用于先阻断新业务访问，不抢占 `ServiceContext` 的资源关闭职责。
- 委托方法说明未绑定时的稳定错误或空指标语义。
- `withStore` 说明读锁覆盖完整调用是为了保证调用期间不被 `Unbind`，不是为了保护 store 内部数据。

### 根 `models` 门面

`models/models.go` 只补充 `OrderWriteAccess` 与 `OrderWriteRuntime` 别名的边界注释。门面不新增 store 创建、绑定或关闭方法，避免恢复已删除的全局生命周期 API。

## 注释风格

- 每个配置组、绑定步骤、失败回滚和数据变换块都必须有中文注释。
- 注释优先回答“为什么”和“返回成功代表什么”，不重复 Go 语法。
- 关键数字需解释单位和保护对象，例如 `128` 是单个 Group Commit 批次上限，`1 << 30` 是当前副本 Badger 的 1 GiB 硬上限。
- 注释不承诺代码未实现的事务、自动接管或远程 drain 能力。

## 测试设计

1. 新增 Service 包内测试，先引用未实现的三个方法形成 RED，验证实例身份目录、runtime 绑定和重复绑定拒绝。
2. 实现三个私有方法后运行 Service 包测试形成 GREEN。
3. 运行 `models/transaction` 现有 runtime 隔离、pending ACK 和 business bounded sync 测试，证明注释与抽取没有改变行为。
4. 运行定向 race，确认 `Bind`/`Unbind`/`withStore` 的锁边界未受破坏。
5. 运行 buyer Docker UAT，验证真实 Service 启动、本地接单、MySQL 汇合和 Outbox 链路。

验证命令：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/order-service/... -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/07-shop-order-scale/order-service ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/transaction -count=1
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATBuyerRoleFlow$' -count=1 -v
```

本机未提供 `SHOP_ORDER_REMOTE_MYSQL_*` 时，直接依赖真实 MySQL 的少数单测仍可报 `Error 1045`；Docker UAT 使用 Compose 内凭证作为端到端验证。

## 验收标准

- `Service.Start` 不再内联完整 `ReliableWriteStoreConfig` 和三段绑定回滚逻辑。
- 三个新方法职责单一，没有恢复全局 store 门面。
- Service 和 models 的关键逻辑步骤都有解释意图的中文注释。
- 本地可靠确认、MySQL 汇合、runtime 解绑、store 关闭和 Admin 物理删除的边界在注释中没有混淆。
- 现有 order-service 可靠写回归和 buyer Docker UAT 结果不退化。
