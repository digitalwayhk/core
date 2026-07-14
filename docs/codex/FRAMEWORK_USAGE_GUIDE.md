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
| 普通 public API | `types.IRouter`、`router.DefaultRouterInfo` | `examples/01-simple-shop/api/public` | Server 基础配置；CORS 启用时显式 origin | `go test ./examples/integration -run Public` | Stable |
| 认证 private API | `api/private`、`req.GetUser()` | `examples/01-simple-shop/api/private` | JWT 或每服务 Logto/Casdoor；代理部署配置 `TrustedProxies` | `go test ./examples/integration -run Private` | Stable |
| 模型持久化 | `entity.NewModelList[T]`、`NewModel()` | `examples/01-simple-shop/models` | 默认 SQLite；外部数据库显式配置 | `./scripts/test.sh persistence-unit` | Stable |
| 本地可靠写回 | `NewSharedBadgerDB[T]`、`EnableWriteBehind` | 无独立示例 | `SyncWrites=true`、冲突检测、损坏 fail closed | nosql 单元与 race | Conditional |
| 标准管理 CRUD | `manage.NewManageService[T](owner)` | `examples/01-simple-shop/api/manage` | manage auth；模型具有正确 Model/BaseModel 语义 | `go test ./service/manage ./examples/integration -run Manage` | Stable |
| 管理 Hook 与视图 | `Parse/Validation/Do` Hooks、`ViewModel` | `service/manage` 测试 | 同管理 CRUD | `go test ./service/manage/...` | Stable |
| OpenAPI 与安全响应 | OpenAPI handler、默认 `Response` | 发布契约文档 | 公共错误契约 | `./scripts/test.sh release-contract` | Stable |
| 路由结果缓存 | `RouterInfo.UseCache`、`IRouterCacheKey` | 无独立示例 | local 可选 Badger；shared 要求 Redis + EventBridge 外部 adapter | `go test ./pkg/server/routecache` | Conditional |
| 本地 WebSocket 通知 | `RegisterWebSocketClient`、`NoticeWebSocket` | `examples/01-simple-shop/api/private/getorders.go` | WebSocket 开启 | `go test -race ./examples/integration -run WebSocket` | Stable |
| 跨节点 WebSocket | ClusterProvider、CrossNodeNoticeBroker | `ROUTERINFO_RUNTIME_GUIDE.md` | 集群 `on/auto`；节点地址可达 | `go test -race ./pkg/server/cluster ./pkg/server/types` | Conditional |
| 配置 profile | `ServerConfig.ApplyDefaults/Validate` | 配置能力矩阵 | 显式环境/profile | `./scripts/test.sh config-contract` | Stable |
| 内部传输选择 | `transport.BuildSelector` | 配置能力矩阵 | http/grpc/socket；QUIC/MQ transport 被拒绝 | `./scripts/test.sh config-contract` | Conditional |
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

write-behind 模式使用 `EnableWriteBehind`，要求 `SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicyFail`。待同步记录禁止 TTL；关闭时仍有积压会返回可通过 `errors.As` 识别的 `PendingSyncError`。旧 `SetSyncDB` 仅为编译兼容入口，新代码不得使用。

`DefaultSharedConfig` 面向共享缓存，默认 `SyncWrites=false`，不能直接启用 write-behind。可靠写回必须显式设置 `SyncWrites=true` 并通过 `EnableWriteBehind` 校验，不要依赖共享缓存默认值。

write-behind 是 at-least-once：远端成功而本地确认失败时会重试，因此远端 insert/update/delete 必须通过稳定主键、upsert 或操作 ID 保证幂等。同 key 多次更新会合并为最新状态，只适用于账户快照、资料和订单当前状态；资金流水、审计记录等不可合并事件必须使用唯一事件 ID 的 NATS JetStream 或 transactional outbox。

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
- 绕过 ModelList、ServiceContext 或本地 service wrapper 直接访问底层连接。
- 自行实现 Redis 连接池、发现循环、日志门面、重试或并发原语，而成熟依赖已提供等价能力。
- 把 write-behind 当作可随时清空的缓存，或让待同步记录使用 TTL/fast 模式。
- 接受配置字段但不消费，或在不支持时静默回退。
- 在日志中输出密钥、JWT、TOTP、请求/响应体、SQL 或业务对象。
- 让默认单元测试隐式依赖本机 Docker、MySQL、MongoDB、ClickHouse、etcd、Consul、Redis 或 NATS。
