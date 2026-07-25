# go-zero 能力复用审计

锁定依赖：`github.com/zeromicro/go-zero v1.10.2`。本审计以当前源码、测试和本机 module cache 为证据，不因上游“存在某包”就推断项目已经使用或适合替换。

## 实际使用面

| go-zero 能力 | 当前调用点 | 状态 |
| --- | --- | --- |
| `core/conf` | `pkg/server/config/serverconfig.go` | 已使用，承担配置加载；Digitalway 继续拥有迁移、默认值和能力校验 |
| `rest`、`rest/httpx` | REST server、请求/响应、OpenAPI/HTML/Fiber 适配 | 已使用，Digitalway 封装路由、认证和响应契约 |
| `core/logx` | server、persistence、manage 多包 | 已使用，但日志词汇与敏感字段仍由任务 8 治理 |
| `core/proc` | WebServer、REST、WebSocket 关闭 | 已使用，关闭 ownership 由任务 12 的生命周期测试约束 |
| `core/service.ServiceGroup` | `pkg/server/run`、`types.Service` | 已使用，负责服务组启动/停止 |
| `stores/cache`、`stores/redis` | 无生产调用 | 上游可用，尚未接入 |
| `core/discov` | 无生产调用 | 上游可用；当前 etcd Provider 有额外 MachineID/claim/Watch 语义 |
| `core/mr`、`core/fx`、`core/threading`、`core/syncx` | 无生产调用 | 上游可用；不得在契约不匹配时机械替换 |
| `zrpc` | `pkg/server/transport/grpc/client.go` | 已复用 direct endpoint Client、连接池、客户端中间件和 credentials；节点发现仍由 Core Resolver 拥有 |

## 决策矩阵

| 领域 | 当前实现与证据 | 成熟候选 | 决策 | 后续门禁 |
| --- | --- | --- | --- | --- |
| 配置 | `serverconfig.go` 已用 `conf`; config-contract 锁定迁移/default/validate | `core/conf`、`core/configcenter` | **keep**：继续标准化现有 conf；配置中心仅在出现真实动态配置需求时立项 | `./scripts/test.sh config-contract` |
| 日志与恢复 | 广泛使用 `logx`，仍有控制台、装饰性和敏感日志债务 | `core/logx`、`core/rescue`、`core/threading` | **replace-by-policy**：任务 8 统一 logx 与结构字段；恢复 helper 按 panic 语义逐点评估 | logging contract + panic 行为测试 |
| HTTP 运行时 | `trans/rest` 封装 go-zero rest；Fiber 仅兼容/静态入口 | `rest`、`zrpc` | **keep-domain**：保留公共 RouterInfo、认证、OpenAPI、响应契约；不并行引入第二套 RPC API | server/api/public compatibility |
| 内部同步 RPC | 默认 gRPC；客户端按 endpoint 复用 `zrpc.Client`，服务端薄封装 grpc-go listener/health/有界关闭 | `zrpc`、grpc-go | **selective-reuse**：客户端复用 zrpc；v1.10.2 的 `RpcServer.Stop()` 依赖进程级 proc，不能满足 ServiceContext 独立停止和同名重建，因此服务端暂不接管 | gRPC 生命周期、health、mTLS、resolver 与 race 测试 |
| 通用 Redis KV | `nosql.Redis.GetRedis()` 每次新建并 Ping go-redis client；当前无生产调用 | `core/stores/redis` | **replace**：先确认公共 API/调用方，再用共享 go-zero Redis 适配器；禁止每操作建连 | Redis Docker ICache 契约、TTL/关闭测试 |
| Cache-aside | `CacheAdapter.getCacheDB()` 固定返回 nil，调用会 panic；无生产调用 | `core/stores/cache`、`stores/redis` | **remove-or-replace**：任务 7 先 fail-fast 并登记废弃；有真实 cache-aside 调用方时再接 stores/cache | 调用方清单、命中/未命中/TTL |
| SQL 持久化 | GORM ModelList/manage 形成公共领域契约 | `stores/sqlx/sqlc` | **keep-domain**：不运行双 ORM；只有 profile 证明瓶颈且迁移契约完备时再立项 | persistence/manage/API 兼容门禁 |
| etcd 发现 | Provider 包含 MachineID slot、claim、状态过滤、Watch、切换对账 | `core/discov` | **keep-domain**：可在 Provider 内复用客户端生命周期，但不能丢失领域语义 | etcd Docker + switcher/membership race |
| Consul 发现 | 自定义 Provider 与 etcd 共享领域接口 | 锁定 go-zero 无 Consul 等价实现 | **keep-domain** | Consul Docker Provider 契约 |
| MQ/Broker | MQProvider、Manager、切换、EventBridge、Redis Streams、NATS | 成熟 Broker 客户端；go-zero queue 生态 | **keep-domain**：Provider 内使用成熟客户端，进程内 queue 不替代 Broker ack/health/switch | MQ/Event 契约与外部集成 |
| Kafka | 无内建 provider，自定义 factory 可扩展 | 受维护 Kafka client | **reject-built-in-now**：没有调用方与契约前不新增；Compose profile 不代表产品支持 | factory fail-fast 测试 |
| 进程内队列 | WebSocket job channel、event Stream | `core/queue` | **keep-domain**：只有背压/关闭契约等价时才替换 | queue depth、关闭和 race |
| 并发辅助 | `ConcurrencyTasks` 保序、结果数组、panic 转 error | `core/mr/fx/threading` | **keep-until-contract**：先补 context 取消和并发上限契约；不因代码短而替换 | utils race/cancel/panic 测试 |
| 生命周期 | 已使用 `proc`、`ServiceGroup`，领域 Provider/Manager 有 Close | `core/proc/service/fx` | **keep-and-standardize**：新资源必须挂 owner；不复制 shutdown registry | concurrency/lifecycle 门禁 |
| 重试/超时 | transport/switcher/persistence 各有领域退避 | `core/fx`、breaker、timex | **migrate-slice-only**：每次只迁一个 owner，先证明错误分类、取消和预算等价 | 确定性错误/取消/关闭测试 |

## 已接受的后续切片

1. **任务 7：CacheAdapter/Redis 清理。** 无调用方的空适配器先改为明确错误并登记废弃；不直接删除导出 API。若后续确有 KV 调用方，以 `stores/redis` 共享客户端实现独立迁移。
2. **任务 8：日志统一。** 复用 `logx`，移除生产控制台/Fatal/装饰性输出；不得借日志重构改变错误语义。
3. **任务 17：指标与资源预算。** 优先复用 go-zero metric/prometheus/stat/trace 能力，Digitalway 只定义领域低基数标签和 SLO。

以下不接受为当前迁移：GORM→sqlx、Provider→裸 discov、MQProvider→进程内 queue、REST 公共契约→新 zrpc。它们会削弱已锁定的 Digitalway API 或领域语义，且当前没有测量收益证据。

升级 go-zero 后，只有上游 `RpcServer` 提供单 listener 独立 `GracefulStop/Stop`，且通过 ServiceContext 关闭、同名重建、health 切换和 race 契约，才重新评估服务端迁移。不得为了“统一使用 zrpc”牺牲现有资源所有权。

## 验证命令

```bash
go test ./pkg/persistence/... ./pkg/server/... ./service/manage/... -count=1
go test -race ./pkg/utils ./pkg/server/cluster ./pkg/server/mq -count=1
```

本审计本身不实施 replace；每个 replace 必须独立 TDD、独立提交并更新本矩阵证据。
