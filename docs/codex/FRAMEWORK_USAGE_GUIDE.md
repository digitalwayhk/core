# Digitalway Core 场景使用指南

## 框架定位

Digitalway Core 是 go-zero 和成熟依赖之上的轻量应用组装层。业务代码表达路由、模型、管理操作和领域事件；HTTP、配置、日志、连接管理和基础并发优先复用成熟实现。配置字段存在不代表能力可用，最终以校验、factory、启动链和行为测试为准。

成熟度定义：

- `Stable`：生产构造器和默认测试已确认。
- `Conditional`：仅在显式配置或外部依赖就绪时支持。
- `Experimental`：API 存在，但生产生命周期或兼容性证据不完整。
- `Unsupported`：运行时必须明确拒绝。

## 场景矩阵

| 场景 | 推荐 API | 最近示例 | 必需配置 | 验证命令 | 成熟度 |
| --- | --- | --- | --- | --- | --- |
| 普通 public API | `types.IRouter`、`IRouterResponse`、`api/dto` | `examples/01-simple-shop/api/public` | Server 基础配置；CORS 启用时显式 origin | `go test ./examples/integration/01-simple-shop -run Public` | Stable |
| 认证 private API | `api/private`、`req.GetUser()`、`api/dto` | `examples/01-simple-shop/api/private` | JWT 或每服务 Logto/Casdoor；代理部署配置 `TrustedProxies` | `go test ./examples/integration/01-simple-shop -run Private` | Stable |
| 模型持久化 | Manage `ModelList[T]`、模型 `IDataAction` 方法、`NewModel()` | `examples/01-simple-shop/models` | 默认 SQLite；外部数据库显式配置 | `./scripts/test.sh persistence-unit` | Stable |
| 本地可靠写回 | `NewSharedBadgerDB[T]`、`UseWriteBehind(WriteBehindTarget)` | `examples/04-shop-performance`、`examples/07-shop-order-scale` | `SyncWrites=true`、冲突检测、损坏 fail closed | nosql 单元与 race | Conditional |
| 标准管理 CRUD | `manage.NewManageService[T](owner)` | `examples/01-simple-shop/api/manage` | manage auth；模型具有正确 Model/BaseModel 语义 | `go test ./service/manage ./examples/integration/01-simple-shop -run Manage` | Stable |
| 管理 Hook 与视图 | `Parse/Validation/Do` Hooks、`ViewModel` | `service/manage` 测试 | 同管理 CRUD | `go test ./service/manage/...` | Stable |
| OpenAPI 与安全响应 | OpenAPI handler、默认 `Response` | 发布契约文档 | 公共错误契约 | `./scripts/test.sh release-contract` | Stable |
| 路由结果缓存 | `RouterInfo.UseCache`、`IRouterCacheKey` | 无独立示例 | local 可选 Badger；shared 要求 Redis + EventBridge 外部 adapter | `go test ./pkg/server/routecache` | Conditional |
| 本地 WebSocket 通知 | `RegisterWebSocketClient`、`NoticeWebSocket` | `examples/01-simple-shop/api/private/getorders.go` | WebSocket 开启 | `go test -race ./examples/integration/01-simple-shop -run WebSocket` | Stable |
| 跨节点 WebSocket | ClusterProvider、CrossNodeNoticeBroker | `ROUTERINFO_RUNTIME_GUIDE.md` | 集群 `on/auto`；节点地址可达 | `go test -race ./pkg/server/cluster ./pkg/server/types` | Conditional |
| 配置 profile | `ServerConfig.ApplyDefaults/Validate` | 配置能力矩阵 | 显式环境/profile | `./scripts/test.sh config-contract` | Stable |
| 内部传输选择 | `transport.BuildSelector` | 配置能力矩阵 | http/grpc/socket；QUIC/MQ transport 被拒绝 | `./scripts/test.sh config-contract` | Conditional |
| 受限内部 Public | `router.WithInternalCallers`、`req.CallService` | `examples/06-shop-microservices` | 同进程 ServiceContext；跨进程必须有可验证 mTLS 身份 | `./scripts/test.sh integration-shop-microservices` | Stable |
| 订单水平扩展 | `AutoMachineID=true`、`sc.UseOutbox`、本地 pending + 远程权威库 | `examples/07-shop-order-scale` | ClusterProvider 可用；每副本 pending 隔离；order 不暴露宿主业务端口 | `go test ./examples/integration/07-shop-order-scale ./examples/integration/07-shop-order-scale-multi-process` | Conditional |
| 本地集群 | LocalProvider | 配置能力矩阵 | `Cluster.Mode=on/auto`、provider=local | `go test ./pkg/server/cluster` | Stable |
| etcd/Consul 集群 | Etcd/Consul Provider | 外部依赖集成文档 | Compose 服务与显式 provider | `./scripts/test.sh integration-external-docker` | Conditional |
| MQ/EventBridge | MQManager、EventBridge、ProviderFactory | 配置能力矩阵 | Redis Streams 或 NATS JetStream；`Usage=[event-stream]` | `./scripts/test.sh integration-external-docker` | Conditional |
| Kafka/RabbitMQ/RocketMQ | 自定义 `ProviderFactory` | 无内建示例 | 应用自行注册成熟客户端适配器 | 配置校验与应用自有集成测试 | Unsupported（内建） |
| QUIC transport | 无推荐 API | 历史兼容包 | 配置层明确拒绝 | `go test ./pkg/server/config -run QUIC` | Unsupported |

字段级细节见 `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`。

RouterInfo 所有权、IRouter 对象池、缓存层级、EventBridge 和 WebSocket 生命周期见 `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`。

## 路由规则

普通路由：

```text
/api/{serviceName}/{structNameLower}
```

包目录决定安全类型，但不会出现在 URL 中：

- `api/public`：无需认证。
- `api/private`：要求认证；身份只能来自 `req.GetUser()`/claims。
- `api/manage`：优先使用 ManageService 生成标准操作。

管理路由：

```text
/api/manage/{serviceName}/{manageStructLower}/{operationLower}
```

服务管理路由：

```text
/api/servermanage/{structNameLower}
```

Public 只描述路由契约，不天然表示互联网可访问。只供其他服务调用的 Public 应声明 `router.WithInternalCallers("service-a", ...)`。同进程身份来自源 ServiceContext；远程身份必须由已验证 mTLS 客户端证书 SAN 绑定到 `SourceService`。普通 HTTP、伪造字段和未验证的远程身份会在 Parse 前失败。调用方直接构造目标服务已注册 API 并使用 `req.CallService`，不要另建地址型 client 或 `api/call` 副本。

示例 06 的 Supplier 用统一 Manage Hook 同时处理供应商本人和管理员：Search Hook 限定 owner，Do Hook 冻结归属并检查禁用状态。Order 可靠事件在 Supplier 本地形成永久 `SupplierOrder`，删除保护只查询本地引用，不在 Hook 中跨服务查询。

## 模型选择

使用 `entity.Model`：记录没有稳定、唯一的业务 `Code`，只需要 ID、时间和基础状态。

使用 `entity.BaseModel`：资料天然具有 `Code`、`Name` 和提交/发布状态。`BaseModel.GetHash()` 基于 `Code`，没有稳定 Code 时误用会造成哈希冲突和校验失败。

所有嵌入指针都必须初始化：

```go
func (own *Product) NewModel() {
	if own.BaseModel == nil {
		own.BaseModel = entity.NewBaseModel()
	}
}
```

`SearchWhere` 默认最多返回 500 条；分页 API 使用 `SearchAll(page, size)`，不要通过扩大默认上限隐藏无界查询。

## PrefixedBadgerDB 使用边界

纯缓存模式以远端数据库为事实源，可以设置 TTL；只有显式 `CorruptionPolicyResetCache` 才允许在检测到损坏时清空重建。默认策略是 `CorruptionPolicyFail`，会保留目录并返回错误。

write-behind 模式使用 `UseWriteBehind(WriteBehindTarget)`，要求 `SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicyFail`。待同步记录禁止 TTL；关闭时仍有积压会返回可通过 `errors.As` 识别的 `PendingSyncError`。旧 `EnableWriteBehind(ModelList)` 和 `SetSyncDB` 仅为 `ModelList/IDataAction` 兼容入口，新业务热路径不得使用。

同一 `PrefixedBadgerDB` 生命周期内只允许绑定一次 write-behind 目标，重复调用 `UseWriteBehind` 或 `EnableWriteBehind` 会返回 `ErrWriteBehindAlreadyBound`，不得依赖静默替换 target。`AutoSync=true` 时框架启动 ticker/trigger worker；`AutoSync=false` 时只保留 pending 和手动 `ForceSyncAll` 能力，业务必须自行提供有界同步循环或显式触发。

`DefaultSharedConfig` 面向共享缓存，默认 `SyncWrites=false`，不能直接启用 write-behind。可靠写回必须显式设置 `SyncWrites=true` 并通过 `UseWriteBehind` 校验，不要依赖共享缓存默认值。

write-behind 是 at-least-once：远端成功而本地确认失败时会重试，因此远端 insert/update/delete 必须通过稳定主键、upsert 或操作 ID 保证幂等。同 key 多次更新会合并为最新状态，只适用于账户快照、资料和订单当前状态；资金流水、审计记录等不可合并事件必须使用唯一事件 ID 的 NATS JetStream 或 transactional outbox。

新业务需要“本地可靠确认 + 最终写回”时，应使用 `ReliableWriteStore[T]`，不要在业务包重复实现 batcher、背压、pending 计数、磁盘扫描或全局生命周期：

```go
identity := nosql.ServiceIdentity{
	ServiceName:  sc.Service.Name,
	DataCenterID: int64(sc.Config.DataCenterID),
	MachineID:    int64(sc.Config.MachineID),
}
store, admin, err := nosql.NewReliableWriteStore[Order](identity, config)
if err != nil {
	return err
}
if err := store.UseWriteBehind(target); err != nil {
	return err
}
if err := sc.UseResource("order-write-store", store); err != nil {
	return err
}
_ = admin // 仅交给独立运维入口，不注入业务服务。
```

`Save` 同时表示 insert/update，`Delete` 写入可靠 tombstone，二者都只有在本地 Badger 提交完成后才返回成功。`ReliableWriteStoreAdmin.PurgeLocal` 会物理删除本地事实和 pending 索引，不产生远端删除语义，只能用于明确的运维修复。`ForceSyncBatch(ctx, limit)` 是 bounded sync；`Close(ctx)` 排空已接收的本地提交并报告 `PendingSyncError`，不会为了“优雅关闭”偷偷访问远端。

目录固定为 `<BasePath>/<service>/dc-<DataCenterID>/machine-<MachineID>`。`ServiceContext` 必须持有 store 资源，业务通过同一服务实例的 typed runtime 访问，禁止恢复包级 registry。`AutoMachineID` 重新分配后会解析到新目录，不会自动接管旧 MachineID 的 pending；编排层必须为每个副本提供稳定、独立的持久卷并制定旧目录 drain/接管流程。

JetStream 数据库写路径的模式选择、当前能力边界和生产化前置条件见 `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md`。当前 Provider 只应视为基础事件流能力，不能把尚未实现的重试、死信和 pull consumer 当作已生效。

## 外部依赖

默认单元测试不依赖 Docker。外部能力必须显式运行：

```bash
cp .env.integration.example .env.integration
./scripts/test.sh integration-external-docker
```

Compose 默认提供 etcd、Consul、Redis、NATS；Kafka 和持久化数据库使用 profile。Kafka 容器只提供消费方扩展环境，不表示框架存在内建 Kafka Provider。

## 日志与错误

- 使用 `logx.Infow/Errorw/Debugw/Sloww` 和稳定 ASCII 事件名。
- 请求/跨服务错误携带 `trace_id`、service、route/target、operation 和 error。
- 下层包装并返回错误，拥有重试、降级、响应或终止决策的边界记录一次。
- 不记录 token、密码、cookie、TOTP、完整 payload/body/response、原始 SQL、参数或对象 dump。

静态守卫：

```bash
./scripts/check-logging.sh
```

## 反模式

- 把请求、身份或 trace 存进共享 Service/Manage 单例。
- Manage CRUD 绕过 ModelList，或 public/private 路由越过模型 `IDataAction` 方法直接访问具体数据库连接。
- 自行实现 Redis 连接池、发现循环、日志门面、重试或并发原语，而成熟依赖已提供等价能力。
- 把 write-behind 当作可随时清空的缓存，或让待同步记录使用 TTL/fast 模式。
- 接受配置字段但不消费，或在不支持时静默回退。
- 在日志中输出密钥、JWT、TOTP、请求/响应体、SQL 或业务对象。
- 让默认单元测试隐式依赖本机 Docker、MySQL、MongoDB、ClickHouse、etcd、Consul、Redis 或 NATS。
