# Digitalway Core 后端开发参考

本参考以当前代码和发布契约为准。完整场景矩阵见 `docs/codex/FRAMEWORK_USAGE_GUIDE.md`。

## 路由

所有 API 实现：

```go
type IRouter interface {
	Parse(req types.IRequest) error
	Validation(req types.IRequest) error
	Do(req types.IRequest) (interface{}, error)
	RouterInfo() *types.RouterInfo
}
```

职责：

- `Parse`：绑定 JSON/query。
- `Validation`：校验和默认值，不做副作用。
- `Do`：业务副作用。
- `RouterInfo`：普通路由返回 `router.DefaultRouterInfo(own)`。

路径：

```text
public/private: /api/{service}/{structLower}
manage:         /api/manage/{service}/{manageLower}/{operationLower}
server manage:  /api/servermanage/{structLower}
```

`api/private` 自动认证。用户身份使用：

```go
userID, userName := req.GetUser()
```

禁止从 body/query 的 user id 推断认证身份。

## 模型与 ModelList

普通记录：

```go
type Order struct {
	*entity.Model
	UserID string
}

func (own *Order) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
```

只有具有稳定唯一 `Code`、`Name` 和资料状态语义时才使用 `BaseModel`。`BaseModel.GetHash()` 基于 Code；没有 Code 时使用 `Model`。

```go
list := entity.NewModelList[models.Order](nil)
item := list.NewItem()
_ = list.Add(item)
_ = list.Save()

rows, total, err := list.SearchAll(page, size)
one, err := list.SearchId(id)
rows, err = list.SearchWhere("UserID", userID)
```

`SearchWhere` 未显式改 size 时最多 500 条；分页接口使用 `SearchAll`。

SQLite 默认 mmap 预算为 256MiB/实例，可通过 `Sqlite.MmapSize` 覆盖；负值关闭。不得恢复机器级 30GB 默认。

### PrefixedBadgerDB

- 纯缓存默认损坏策略为 `CorruptionPolicyFail`；只有确认数据可从远端完整重建时才显式使用 `CorruptionPolicyResetCache`。
- 可靠写回使用 `EnableWriteBehind`，配置必须满足 `SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicyFail`。
- `SetSyncDB` 已废弃，仅保留编译兼容；其绑定错误会在后续写入和关闭时返回。
- 待同步记录禁止 TTL。`Close` 返回 `PendingSyncError` 表示本地仍是临时事实源，不能把目录当缓存删除。
- 语义为 at-least-once，远端操作必须幂等。同 key 写入会合并状态，不适用于资金流水或审计事件；不可合并事件使用唯一事件 ID 的 JetStream/outbox。

## Manage CRUD

```go
type ProductManage struct {
	*manage.ManageService[models.Product]
}

func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = manage.NewManageService[models.Product](own)
	return own
}
```

必须把真实 owner 传给 `NewManageService`，否则 `ViewModel`、Parse/Validation/Do 和 Search hooks 不会落到自定义类型。

自定义操作以值嵌入 `manage.Operation[T]`，不要嵌入指针。

## 服务与启动

```go
type OrderService struct{}

func (*OrderService) ServiceName() string { return "orders" }
func (*OrderService) Routers() []types.IRouter {
	return []types.IRouter{&public.Ping{}, &private.AddOrder{}}
}
func (*OrderService) SubscribeRouters() []*types.ObserveArgs { return nil }
```

```go
server := run.NewWebServer()
server.AddIService(&OrderService{}, &types.ServerOption{
	IsCors:     true,
	OriginCors: []string{"http://localhost:8000"},
})
server.Start()
```

CORS fail closed：`IsCors=true` 必须显式 origin；`*` 只能由调用方主动选择。

反向代理必须配置 `ServerConfig.TrustedProxies` 的 IP/CIDR。默认空表示忽略 XFF/X-Real-IP；本地/private peer 携带 forwarding header 且没有信任策略时 fail closed。

## Cluster、Transport、MQ 与事件

- Local cluster：`Stable`。
- etcd/Consul：`Conditional`，需要显式配置和外部依赖。
- 内部传输：http/grpc/socket 按能力矩阵使用。
- QUIC 和 MQ transport：`Unsupported`，配置校验拒绝。
- MQ/EventBridge：Redis Streams、NATS JetStream 为 `Conditional`。
- JetStream 可靠数据库写路径先阅读 `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md`；当前 Provider 已有 publish ACK、消息 ID 去重和显式 ACK，但重试、死信、pull consumer 与生产 stream 参数尚未实现。
- Kafka/RabbitMQ/RocketMQ：无内建 Provider；应用可在 `MQProvider` 后注册自定义 `ProviderFactory`。

go-zero `core/queue` 只用于进程内队列，不能替代 Broker。

## WebSocket 与跨节点通知

订阅使用真实 `RouterInfo().Path`。跨节点模式要求 ClusterProvider 和 CrossNodeNoticeBroker 已由 ServiceContext 启动。forwarder 按服务名隔离；IPv6 地址通过 `net.JoinHostPort`，非 2xx 转发视为错误。

worker 生命周期由通知系统持有；队列满、filter timeout、panic 和 shutdown timeout 是 error，worker 启停是 debug。不得记录消息体。

## 日志与错误

- `logx.Infow`：生命周期、切换、成功降级。
- `logx.Debugw`：重试、路由注册、worker 和高频细节。
- `logx.Errorw`：最终失败、数据风险、panic、关闭失败。
- `logx.Sloww`：测量超阈值。

请求/跨服务失败携带 `trace_id`、service、route/target、operation 和 error。错误由拥有重试、降级、响应或终止决策的边界记录一次。

禁止记录凭据、token、cookie、TOTP、完整 payload/body/response、DSN、SQL、参数和对象 dump。

## 测试与发布

```bash
./scripts/test.sh quick
./scripts/test.sh security
./scripts/test.sh config-contract
./scripts/test.sh persistence-unit
./scripts/test.sh performance-contract
./scripts/test.sh release-contract
```

外部依赖默认 skip：

```bash
./scripts/test.sh integration-external-docker
./scripts/test.sh integration-persistence
```

发布前不得自动创建 tag。开发消费方可临时引用分支或精确 commit：

```bash
go get github.com/digitalwayhk/core@codex/optimize-code-cleanup
go get github.com/digitalwayhk/core@<commit>
```

分支会移动并解析为伪版本；生产必须使用已发布 tag 或精确 commit。执行 `release-contract`，并遵循 `docs/RELEASE_POLICY.md` 与废弃登记。

## 常见错误

- URL 加入 `/public`、`/private`。
- private API 使用客户端 user id。
- `NewModel()` 未初始化嵌入指针。
- 无稳定 Code 的模型使用 BaseModel。
- ManageService 传入内嵌实例而非真实 owner。
- 绕过 ModelList/ServiceContext 或自行实现成熟基础设施能力。
- 仅因配置字段存在就声明能力稳定。
- 单元测试隐式依赖 Docker/本机数据库。
