# RouterInfo 运行时解耦实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 RouterInfo 收敛为服务内路由元数据和执行编排对象，完成请求对象池、ServiceContext 生命周期、服务级 EventBridge、WebSocket Hub 与 L1/L2/L3 路由缓存。

**Architecture:** ServiceContext 是全部运行组件的唯一所有者，RouterInfo 只通过小接口委托。IRouter 使用每路由有界池；观察事件 best-effort，控制事件可靠；WebSocket 按完整 hash 隔离；缓存依次使用 go-zero L1、纯 Badger L2 和 Redis L3。

**Tech Stack:** Go 1.24、go-zero v1.10.2、`collection.Cache`、`syncx.SingleFlight`、Badger v3、go-zero Redis、现有 MQManager/Event Envelope。

**规格：** `docs/superpowers/specs/2026-07-13-routerinfo-cache-event-websocket-design.md`

---

## 文件结构

- `pkg/server/types/routerinfo.go`：RouterInfo 元数据、执行编排和兼容门面。
- `pkg/server/types/channelpool.go`：每 RouterInfo 唯一的有界 IRouter 池。
- `pkg/server/types/route_runtime.go`：RouterInfo 对缓存、事件和 WebSocket 运行组件的小接口。
- `pkg/server/router/configfingerprint.go`：ServerConfig 规范化指纹。
- `pkg/server/router/servicecontext.go`：服务级组件所有权、初始化和关闭注销。
- `pkg/server/event/servicebridge.go`：服务内观察/控制事件总线。
- `pkg/server/types/route_websocket_*.go`：WebSocket Hub、分片、投递、生命周期和统计。
- `pkg/server/routecache/manager.go`：分层缓存编排和状态机。
- `pkg/server/routecache/l1.go`：go-zero 内存缓存。
- `pkg/server/routecache/l2_badger.go`：无 write-behind 的 Badger 缓存。
- `pkg/server/routecache/l3_redis.go`：Redis 共享缓存。
- `pkg/server/config/routecache.go`：缓存配置、默认值和 fail-closed 校验。

## Task 1：请求执行、事件快照与对象池闭环（实现 `2cb693c`，验收 `2e2b40f`）

验收：go test ./pkg/server/types -count=1 与 go test -race ./pkg/server/types -count=1 均通过。

**Files:**
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/types/channelpool.go`
- Modify: `pkg/server/types/observable.go`
- Create: `pkg/server/types/router_execution_test.go`
- Create: `pkg/server/types/channelpool_contract_test.go`

- [x] **Step 1: 写失败测试**

覆盖以下契约：`ExecDo` panic 返回非 nil 安全响应；观察回调看到的是快照；同一 RouterInfo 归还后能再次取到对象；`IRouterFactory` 最终实例进入同一池；`Reset` 在 Parse 前调用、`Clean` 在归还前调用。

```go
func TestRouterInfoExecDoPanicReturnsSafeResponse(t *testing.T) {}
func TestRouterInfoObserverUsesSnapshotBeforePoolReturn(t *testing.T) {}
func TestChannelPoolGetPutUsesSamePool(t *testing.T) {}
func TestChannelPoolUsesResetAndCleanContracts(t *testing.T) {}
func TestChannelPoolPoolsFactoryResult(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/types -run 'TestRouterInfoExecDoPanic|TestRouterInfoObserver|TestChannelPool' -count=1
```

Expected: panic 响应为 nil、观察对象被 Clean 修改、池对象未复用等断言失败。

- [x] **Step 3: 最小实现**

`ExecDo` 改为具名返回或移除内部 recover，让 `Exec` 成为唯一 panic 边界。通知前创建 JSON 安全快照，不再把池化 IRouter 交给 goroutine。

```go
type RouterEventSnapshot struct {
    Service string          `json:"service"`
    Route   string          `json:"route"`
    TraceID string          `json:"trace_id"`
    State   ObserveState    `json:"state"`
    Router  json.RawMessage `json:"router,omitempty"`
}
```

统一对象池：`initChannelPool` 的 factory 直接识别原型 `IRouterFactory`；`putRouter` 调用 `Clean` 后放回 `channelPool`，删除 RouterInfo 中未被读取的 `sync.Pool`。

```go
func (own *RouterInfo) putRouter(router IRouter) {
    if router == nil || own.channelPool == nil { return }
    own.cleanRouter(router)
    own.channelPool.Put(router)
}
```

- [x] **Step 4: 开发验收**

Run:

```bash
gofmt -w pkg/server/types/routerinfo.go pkg/server/types/channelpool.go pkg/server/types/observable.go pkg/server/types/*_test.go
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/types -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/types -count=1
```

Expected: PASS，race 无报告。

- [x] **Step 5: 提交**

```bash
git add pkg/server/types
git commit -m "fix: make router execution and pooling lifecycle safe"
```

## Task 2：RouterInfo 冻结与 ServiceContext 注册表生命周期（实现 `690bb1a`，验收 `ffd42cf`）

**Files:**
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/servicerouter.go`
- Modify: `pkg/server/router/servicecontext.go`
- Create: `pkg/server/router/configfingerprint.go`
- Create: `pkg/server/types/routerinfo_lifecycle_test.go`
- Modify: `pkg/server/router/servicecontext_registry_test.go`

- [x] **Step 1: 写失败测试**

```go
func TestRouterInfoFreezeRejectsMetadataMutation(t *testing.T) {}
func TestServiceContextSameNameSameConfigReusesActiveInstance(t *testing.T) {}
func TestServiceContextSameNameDifferentConfigFailsClosed(t *testing.T) {}
func TestServiceContextShutdownUnregistersExactInstance(t *testing.T) {}
func TestServiceContextCanRecreateAfterShutdown(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/types ./pkg/server/router -run 'Freeze|SameName|UnregistersExact|RecreateAfter' -count=1
```

Expected: 当前元数据可修改、不同配置静默复用、终止实例仍留在 registry。

- [x] **Step 3: 最小实现**

RouterInfo 增加所有者和冻结状态；`ServiceRouter.AddRoutes` 完成归一化后调用 `Freeze(serviceName)`。冻结后的 setter panic 并使用稳定错误文本。

```go
func (own *RouterInfo) Freeze(owner string) {
    own.lifecycleMu.Lock()
    defer own.lifecycleMu.Unlock()
    own.owner = owner
    own.frozen = true
}
```

规范化配置后对不含运行时指针的 JSON 计算 SHA-256 指纹，只保存摘要。registry entry 同时保存 context 和 fingerprint；关闭完成后使用 compare-and-delete。

```go
func (r *serviceContextRegistry) remove(name string, expected *ServiceContext) bool {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.contexts[name] != expected { return false }
    delete(r.contexts, name)
    return true
}
```

- [x] **Step 4: 测试工程师验收**

Run:

```bash
gofmt -w pkg/server/types/routerinfo.go pkg/server/router/*.go
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/router ./pkg/server/types -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/router ./pkg/server/types -count=1
```

Expected: PASS；配置冲突不打印配置正文。

验收证据：`go test ./pkg/server/router ./pkg/server/types -count=1` 与对应 `-race` 均通过；实现提交 `690bb1a`。

- [x] **Step 5: 提交**

```bash
git add pkg/server/types/routerinfo.go pkg/server/types/routerinfo_lifecycle_test.go pkg/server/router
git commit -m "fix: enforce router ownership and context registry lifecycle"
```

## Task 3：每服务 ServiceEventBridge（实现 `6c75625`，验收 `ab3a346`）

**Files:**
- Create: `pkg/server/event/servicebridge.go`
- Create: `pkg/server/event/servicebridge_test.go`
- Modify: `pkg/server/event/stream.go`
- Modify: `pkg/server/router/servicecontext.go`
- Create: `pkg/server/types/route_runtime.go`
- Modify: `pkg/server/types/routerinfo.go`

- [x] **Step 1: 写失败测试**

```go
func TestServiceEventBridgeExistsWithoutMQ(t *testing.T) {}
func TestServiceEventBridgeDropsObserverBeforePayloadBuildWhenUnused(t *testing.T) {}
func TestServiceEventBridgeObserverQueueIsBounded(t *testing.T) {}
func TestServiceEventBridgeControlEventsKeepShardOrder(t *testing.T) {}
func TestServiceEventBridgeControlPublishWithoutExternalProviderFails(t *testing.T) {}
func TestServiceEventBridgeCloseIsIdempotent(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/event ./pkg/server/router -run 'ServiceEventBridge' -count=1
```

Expected: `ServiceEventBridge` 尚不存在。

- [x] **Step 3: 最小实现**

定义明确的事件类别和发布结果。观察事件使用有界队列；控制事件按 ShardKey 进入固定数量串行 worker。外发只有显式 `External=true` 时调用 MQ adapter。

```go
type DeliveryClass uint8
const (
    ObserverDelivery DeliveryClass = iota
    ControlDelivery
)
type PublishRequest struct {
    Class DeliveryClass
    External bool
    Envelope *Envelope
    BuildData func() ([]byte, error)
}
```

`NewServiceContext*` 无条件创建本地 bridge；MQ 可用时只安装外部 adapter。RouterInfo 的兼容 Subscribe/UnSubscribe 委托 bridge。

- [x] **Step 4: 测试工程师验收**

Run:

```bash
gofmt -w pkg/server/event pkg/server/router/servicecontext.go pkg/server/types
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/event ./pkg/server/router ./pkg/server/types -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/event ./pkg/server/router ./pkg/server/types -count=1
./scripts/check-logging.sh
```

Expected: PASS；未注册观察者时 BuildData 调用次数为 0。

验收证据：`go test ./pkg/server/event ./pkg/server/router ./pkg/server/types -count=1`、对应 `-race` 与 `./scripts/check-logging.sh` 均通过；实现提交 `6c75625`。

- [x] **Step 5: 提交**

```bash
git add pkg/server/event pkg/server/router/servicecontext.go pkg/server/types
git commit -m "feat: add service scoped event bridge"
```

## Task 4：RouteWebSocketHub 精确订阅与生命周期（实现 `ed2c7d5`，验收 `a052282`）

**Files:**
- Create: `pkg/server/types/route_websocket_hub.go`
- Create: `pkg/server/types/route_websocket_shard.go`
- Create: `pkg/server/types/route_websocket_delivery.go`
- Create: `pkg/server/types/route_websocket_lifecycle.go`
- Create: `pkg/server/types/route_websocket_stats.go`
- Create: `pkg/server/types/route_websocket_hub_test.go`
- Modify: `pkg/server/types/websocketshard.go`
- Modify: `pkg/server/types/websocketnotificationsystem.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/servicecontext.go`

- [x] **Step 1: 写失败测试**

```go
func TestRouteWebSocketHubSeparatesHashesInSameShard(t *testing.T) {}
func TestRouteWebSocketHubAllowsClientOnMultipleHashes(t *testing.T) {}
func TestRouteWebSocketHubDuplicateRegisterIsIdempotent(t *testing.T) {}
func TestRouteWebSocketHubCleanupUpdatesSubscriptionState(t *testing.T) {}
func TestRouteWebSocketHubControlEventsKeepActiveInactiveOrder(t *testing.T) {}
func TestRouteWebSocketHubIsIsolatedPerService(t *testing.T) {}
func TestRouteWebSocketHubCloseWithoutInitializationIsSafe(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/types -run 'RouteWebSocketHub' -count=1
```

Expected: 同 shard 不同 hash 串消息或 Hub 尚不存在。

- [x] **Step 3: 最小实现**

分片内使用完整 hash 二级 map；订阅组保存稳定 Router 快照和客户端请求元数据。

```go
type routeWebSocketShard struct {
    mu sync.RWMutex
    subscriptions map[uint64]*routeSubscription
}
type routeSubscription struct {
    router IRouter
    clients map[IWebSocket]IRequest
}
```

同 client/hash 重复注册不增加计数；0->1 和 1->0 通过 ServiceEventBridge 发布控制事件。发送使用服务级有界队列，不为每个 client 创建 goroutine。旧 RouterInfo WebSocket 方法仅委托 Hub。删除全局 notification system 和 clearMap 的业务状态。

- [x] **Step 4: 测试工程师验收**

Run:

```bash
gofmt -w pkg/server/types/route_websocket_*.go pkg/server/types/websocket*.go pkg/server/router/servicecontext.go
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/types ./pkg/server/router -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/types ./pkg/server/router -count=1
```

Expected: PASS；碰撞 hash 只收到各自消息；关闭后 goroutine 数回落到测试容差内。

验收证据：`go test ./pkg/server/types ./pkg/server/router -count=1`、对应 `-race`、日志检查与 `release-contract` 均通过；实现提交 `ed2c7d5`。

- [x] **Step 5: 提交**

```bash
git add pkg/server/types pkg/server/router/servicecontext.go
git commit -m "fix: isolate websocket subscriptions by full hash"
```

## Task 5：RouteCacheManager 与 go-zero L1（实现 `4d16ef3`，验收 `b81d487`）

**Files:**
- Create: `pkg/server/config/routecache.go`
- Create: `pkg/server/config/routecache_test.go`
- Create: `pkg/server/routecache/manager.go`
- Create: `pkg/server/routecache/l1.go`
- Create: `pkg/server/routecache/manager_test.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/types/router.go`
- Modify: `pkg/server/types/route_runtime.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/servicecontext.go`

- [x] **Step 1: 写失败测试**

```go
func TestRouteCacheConfigDefaultsToOff(t *testing.T) {}
func TestRouteCacheKeyUsesCacheKeyBeforeHashKey(t *testing.T) {}
func TestRouteCacheFallbackEncodingHasFieldBoundaries(t *testing.T) {}
func TestRouteCacheL1ExpiresAndEvicts(t *testing.T) {}
func TestRouteCacheSingleFlightLoadsOnce(t *testing.T) {}
func TestRouterInfoUseCacheDelegatesToManager(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/config ./pkg/server/routecache ./pkg/server/types -run 'RouteCache|UseCacheDelegates' -count=1
```

Expected: routecache package、配置和 IRouterCacheKey 尚不存在。

- [x] **Step 3: 最小实现**

新增配置，默认 `Mode=off`；local 启用 L1，shared 留给 Task 7 校验。L1 使用 `collection.NewCache(ttl, collection.WithLimit(limit))`，加载用 `syncx.NewSingleFlight()`。

```go
type IRouterCacheKey interface { GetCacheKey() string }
type RouteCacheConfig struct {
    Mode string `json:",optional"`
    TTL time.Duration `json:",optional"`
    L1 RouteCacheL1Config `json:",optional"`
    L2 RouteCacheL2Config `json:",optional"`
    Redis RouteCacheRedisConfig `json:",optional"`
}
```

键顺序为 `IRouterCacheKey -> IRouterHashKey -> 带字段名/类型/长度的确定性 JSON`。RouterInfo 原有 UseCache/FailureCache 保留并委托 manager。

- [x] **Step 4: 测试工程师验收**

Run:

```bash
gofmt -w pkg/server/config pkg/server/routecache pkg/server/types pkg/server/router/servicecontext.go
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/config ./pkg/server/routecache ./pkg/server/types ./pkg/server/router -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/routecache ./pkg/server/types -count=1
./scripts/test.sh release-contract
```

Expected: PASS，公共 API 快照只新增接口和配置。

验收证据：`go test ./pkg/server/config ./pkg/server/routecache ./pkg/server/types ./pkg/server/router -count=1`、聚焦 `-race`、日志检查和 `release-contract` 均通过；实现提交 `4d16ef3`。

- [x] **Step 5: 提交**

```bash
git add pkg/server/config pkg/server/routecache pkg/server/types pkg/server/router/servicecontext.go
git commit -m "feat: add service scoped route cache l1"
```

## Task 6：纯缓存 Badger L2（实现 `b6aa0d0`，验收 `58423b9`）

**Files:**
- Create: `pkg/server/routecache/l2_badger.go`
- Create: `pkg/server/routecache/l2_badger_test.go`
- Modify: `pkg/server/routecache/manager.go`
- Modify: `pkg/server/config/routecache.go`

- [x] **Step 1: 写失败测试**

```go
func TestBadgerL2SetGetDeleteWithTTL(t *testing.T) {}
func TestBadgerL2RestartReadsUnexpiredValue(t *testing.T) {}
func TestBadgerL2HasNoWriteBehindQueue(t *testing.T) {}
func TestBadgerL2CorruptionResetRequiresExplicitPolicy(t *testing.T) {}
func TestRouteCachePromotesL2HitToL1(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/routecache -run 'BadgerL2|PromotesL2' -count=1
```

Expected: L2 adapter 尚不存在。

- [x] **Step 3: 最小实现**

直接使用 Badger v3 的 `SetEntry(...WithTTL(ttl))`、`View` 和 `Delete`，值为版本化 JSON envelope。不得导入或调用 `PrefixedBadgerDB`、SyncQueue、远程 adapter。

```go
type cacheEnvelope struct {
    Version int `json:"version"`
    Data json.RawMessage `json:"data"`
}
```

路径由 ServiceContext 名称隔离；测试使用 `t.TempDir()`。关闭时先停止 manager，再关闭 Badger。

- [x] **Step 4: 测试工程师验收**

Run:

```bash
gofmt -w pkg/server/routecache pkg/server/config/routecache.go
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/routecache -count=20
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/routecache -count=1
```

Expected: PASS，无 sleep 刷绿、无同步队列键。

验收证据：`go test ./pkg/server/routecache -count=20`、对应 `-race`、四包回归、日志检查和 `release-contract` 均通过；实现提交 `b6aa0d0`。

- [x] **Step 5: 提交**

```bash
git add pkg/server/routecache pkg/server/config/routecache.go
git commit -m "feat: add pure badger route cache l2"
```

## Task 7：Redis L3、严格共享模式与可靠失效（实现 `8eec8b6`，验收 `24722c6`）

**Files:**
- Create: `pkg/server/routecache/l3_redis.go`
- Create: `pkg/server/routecache/l3_redis_test.go`
- Create: `pkg/server/routecache/invalidation.go`
- Create: `pkg/server/routecache/invalidation_test.go`
- Modify: `pkg/server/routecache/manager.go`
- Modify: `pkg/server/config/routecache.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `docker-compose.integration.yml`
- Modify: `scripts/test.sh`

- [x] **Step 1: 写失败测试**

使用接口化 fake Redis 完成默认单元测试；Docker 集成测试仅在显式环境变量下运行。

```go
func TestSharedModeWithoutRedisFailsClosed(t *testing.T) {}
func TestSharedModeExplicitBypassDisablesAllLayers(t *testing.T) {}
func TestRedisFailureClearsAndPausesL1L2(t *testing.T) {}
func TestRedisRecoveryWaitsForInvalidationSubscription(t *testing.T) {}
func TestInvalidationClearsPeerL1L2(t *testing.T) {}
func TestInvalidationIsIdempotent(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/config ./pkg/server/routecache -run 'SharedMode|RedisFailure|Invalidation' -count=1
```

Expected: shared 模式和 Redis adapter 尚不存在。

- [x] **Step 3: 最小实现**

L3 使用 go-zero Redis `GetCtx/SetexCtx/DelCtx/PingCtx`。失效事件通过 ServiceEventBridge 的外部控制通道发布，payload 只含 service、route、key、generation。manager 状态为 enabled/bypass/degraded/closed。

```go
type ManagerState uint8
const (
    StateEnabled ManagerState = iota
    StateBypass
    StateDegraded
    StateClosed
)
```

启动时 shared+Redis 不可用默认返回错误；显式 `OnUnavailable=bypass` 时关闭全部层。运行期失败清空并暂停 L1/L2；只有 Ping 和失效订阅都恢复才重新启用。

- [x] **Step 4: 测试工程师验收**

Run:

```bash
gofmt -w pkg/server/routecache pkg/server/config/routecache.go pkg/server/router/servicecontext.go
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/config ./pkg/server/routecache ./pkg/server/router -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/routecache ./pkg/server/router -count=1
CORE_TEST_REDIS=1 ./scripts/test.sh integration
```

Expected: 默认单测 PASS；显式 Redis 集成环境 PASS；环境变量未设置时集成测试 skip。

验收证据：聚焦 RED 首先因缺少 `SubscribeExternal`、Redis 注入和失效协议而失败；实现后四包全量、`routecache/router` race、`routecache -count=20`、日志检查和 `release-contract` 均通过。`./scripts/test.sh integration` 默认明确 skip；临时启动现有 Compose Redis 后，`CORE_TEST_REDIS=1 ./scripts/test.sh integration` 真实往返通过并已停止容器。Compose 原本已包含 Redis，因此未修改或提交用户现有的 MySQL 镜像变更。实现提交 `8eec8b6`。

- [x] **Step 5: 提交**

```bash
git add pkg/server/routecache pkg/server/config/routecache.go pkg/server/router/servicecontext.go docker-compose.integration.yml scripts/test.sh
git commit -m "feat: add shared redis route cache l3"
```

## Task 8：兼容门面、全局状态清理与总验收（实现 `eb38719`）

**Files:**
- Modify: `pkg/server/types/routerinfo.go`
- Delete after migration: `pkg/server/types/websocketnotificationsystem.go`
- Delete or reduce to delegates: `pkg/server/types/websocketshard.go`
- Modify: `pkg/server/types/crossnode.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/LOGGING_AUDIT_AND_STANDARD.md`
- Create: `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`
- Modify: `docs/superpowers/plans/2026-07-13-routerinfo-runtime-refactor.md`

- [x] **Step 1: 写失败契约检查**

新增静态测试，禁止 RouterInfo/WebSocket 使用 `globalNotificationSystem`、`clearMap` 和未读取的 `sync.Pool`；确认旧公开门面仍存在。

```go
func TestRouterRuntimeHasNoProcessGlobalMutableComponents(t *testing.T) {}
func TestRouterInfoCompatibilityMethodsRemain(t *testing.T) {}
```

- [x] **Step 2: 验证 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./internal/compat ./pkg/server/types -run 'RouterRuntime|RouterInfoCompatibility' -count=1
```

Expected: 旧全局 WebSocket 系统仍被扫描到。

- [x] **Step 3: 最小清理与文档**

删除已迁移实现和大段注释代码；cross-node fallback global 标记废弃并由 service scoped bridge 替代。文档写明 RouterInfo/IRouter 生命周期、Reset/Clean、缓存模式、Redis 故障语义和 WebSocket 精确 hash。

- [x] **Step 4: 测试工程师总验收**

Run:

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/... -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/types ./pkg/server/event ./pkg/server/router ./pkg/server/routecache -count=1
./scripts/check-logging.sh
./scripts/test.sh release-contract
./scripts/ci.sh required/quick
./scripts/ci.sh required/contracts
```

Expected: 全部 PASS。Redis/Badger 外部集成只在显式环境变量下运行。

验收证据：静态契约首先因 `clearMap`、旧全局 forwarder 回退、旧通知 worker 和 RouterInfo 内缓存/WebSocket 状态而失败；清理后聚焦契约转绿。`go test ./pkg/server/... -count=1`、四运行组件 race、日志守卫、`release-contract`、`required/quick` 与 `required/contracts` 全部通过。端口测试在允许绑定本机临时端口的环境中执行；默认外部集成仍不隐式连接 Docker。实现提交 `eb38719`。

- [x] **Step 5: 更新计划证据并提交**

在本计划每个 Task 标题后记录提交 SHA 和验收结果，禁止使用“本批提交”等占位文本。

```bash
git add pkg/server docs/codex docs/superpowers/plans/2026-07-13-routerinfo-runtime-refactor.md internal/compat scripts
git commit -m "refactor: complete router runtime component isolation"
```

## 外部审查修复与复审（实现 `22ba48f`，复审 `APPROVED`）

首轮外部审查裁定为 `CHANGES_REQUIRED`：无 P0，有两项 P1。修复提交 `22ba48f` 的外部复审裁定为 `APPROVED`：原 P1-1、P1-2 均已关闭，无新增 P0/P1，允许关闭本计划的外部审查门禁。

- [x] **P1-1：Shared generation 在重启/扩容后可重用失效数据**

  Redis 中以 `service + route` 保存权威 generation；首次通过 `SETNX` 初始化，路由级删除通过 `INCR` 原子递增，`EnableRoute` 和 `Recover` 都从 Redis 加载当前世代。新增冷启节点回归测试，并覆盖 go-zero `Redis.GetCtx` 在 key 不存在时返回 `("", nil)` 的真实契约。

- [x] **P1-2：关闭窗口内 registry 复用已 terminated 实例**

  `ServiceContext` 在关闭期间公布 `shutdownDone`；同名创建遇到 terminated 实例时等待关闭、精确注销完成后再重建。`get` 和 `snapshot` 不再对外返回关闭中实例。新增可控阻塞关闭窗口的并发回归测试。

内部验证证据：

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/routecache -count=20
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/routecache ./pkg/server/router -count=1
CORE_TEST_REDIS=1 CORE_TEST_REDIS_ADDR=127.0.0.1:6379 ./scripts/test.sh integration
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/... -count=1
./scripts/check-logging.sh
./scripts/test.sh release-contract
./scripts/ci.sh required/contracts
```

上述均通过；真实 Redis 测试使用现有 Compose 服务，测试后已停止容器。

外部复审登记的后续非阻断项：

- 并发 `EnableRoute` 与 `DeleteRoute` 时，本地 generation 写回应保证单调不回退，并补最终本地值与 Redis 权威值一致的压测。
- 补充多 goroutine 冷 key `SETNX` 竞态和关闭后多 waiter 同名重建测试。
- TTL jitter、L1 值类型一致性、`TempStore`/公开元数据、WebSocket 订阅租约、控制队列超时与指标继续作为后续优化项。

## 最终关闭条件

- [x] 八个 Task 均有独立提交 SHA。
- [x] 每节定向测试和 race 验收通过。
- [x] `pkg/server/...`、日志检查、release-contract、required quick/contracts 全绿。
- [x] 外部审查无 P0/P1；P2 明确登记，不伪装关闭。
- [x] 设计规格和使用文档与实际配置、启动和降级行为一致。
