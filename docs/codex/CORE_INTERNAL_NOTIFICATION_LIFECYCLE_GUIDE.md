# Core 内部通知生命周期标准

本文约束 Core 自行产生的通知，作为 [业务 MQ 生命周期标准](MQ_MESSAGE_LIFECYCLE_GUIDE.md) 的配套长期设计。不能把本规范的瞬时通知机制用于成交、资金、订单或审计事实。

## 分类与发布点清单

| 消息/Provider | 发布及存储路径 | 回收/恢复归属 |
| --- | --- | --- |
| Redis 服务发现 | `cluster/provider_redis.go` 两段 Lua，Heartbeat 复用 Register | `XADD MAXLEN ~ 10000`；节点 TTL 键、索引、service set 是当前状态，Watch 每 `ttl/2` 快照对账。裁剪旧通知不代表删除节点 |
| Local 服务发现 | `provider_local.go` 当前节点 map、进程内 watcher 回调 | 不产生 Broker 消息历史；节点超时清理与当前快照保持原机制 |
| etcd 服务发现 | `provider_etcd.go` 租约节点键与原生 Watch | Core 不另建消息日志。etcd MVCC 历史/压缩属于 Broker 运维策略；节点 TTL 不等于历史 revision 已压缩，不擅自执行全库 compact |
| Consul 服务发现 | `provider_consul.go` TTL 注册与阻塞查询当前健康服务 | 无 Core 追加消息流；Consul 服务状态与底层 Raft 存储由原生机制管理 |
| shared 路由缓存 | `routecache/invalidation.go` → router 私有通知桥 | Redis Pub/Sub 或 Core NATS 广播，不创建消息历史；Redis L3/generation 权威加周期本地失效补偿 |
| shared Casdoor 身份变更 | `authstate.Manager.ProcessEvent` → router 私有通知桥 | 先提交 Redis 权威，再广播；HTTP 查权威，存量 Casdoor WebSocket 周期验证。通知不是撤销事实 |
| 认证权威不可用、WebSocket 订阅/通知 | `authstate/manager.go`、`types/route_websocket_*` → 本地 EventBridge | `External:false`，既有有界本地队列、关闭和超时；无 Broker 历史 |
| Casdoor 业务 Hook | `authstate` 的 Badger pending hook | 成功 `AckHook` 删除；失败保留并重试。这是未完成业务，不按通知 TTL 删除 |
| 应用事件、Outbox、可靠重试、DLQ | `event`、`mq`、应用 `OutboxStore` | 继续遵守业务生命周期；Outbox `MarkPublished` 交给 Store，未发布项不能随通知裁剪。DLQ 和 Redis DLQ 去重证据不自动清理，须保持既有策略与防重复边界 |

盘点应从 Core 发布点与追加操作开始，而不是给每种服务发现 Provider 强行增加删除 API。当前内置 MQ 仅 Redis Streams/NATS JetStream；Kafka/RabbitMQ/RocketMQ 没有内置实现。

## 专用原生机制

只有组合根明确注入的 `routecache.invalidate` 与 `auth.casdoor.identity.changed` 使用内部广播；普通应用 `ServiceEventBridge.Publish`、相似 Subject、`RequireMessageLifecycle`、必需组、ACK、pending、重试、DLQ 和 v1.1.0 的 `NoRequiredGroups` 不变。

| 能力 | Redis 原生 Pub/Sub | Core NATS 普通订阅 |
| --- | --- | --- |
| 每个在线副本独立接收 | 是，不用消费组 | 是，不用 queue group |
| 离线重放、ACK、持久化确认 | 不支持 | 不支持 |
| 发布确认 | Redis 接受命令；不证明各副本完成 | Publish 后 Flush；不证明各副本完成 |
| 历史资源 | 不创建 Stream/消费组 | 不创建 Stream/durable；启动与发布前拒绝 JetStream 捕获 |
| 有界保护 | 64 KiB 帧；3 秒读/写预算，1 秒心跳；停止读取超过预算失效 | 64 KiB 帧；pending 256 条/1 MiB；禁用自动重连/重连缓存，原生慢消费者与异步错误关闭连接 |
| 恢复 | 上层新连接、新代次，不能掩盖透明重订阅造成的间隙 | 上层新连接、新代次，不能把丢失后的续收当作连续 |

命名使用 `_core.notify.v1`，环境前缀、服务、类型经编码；Redis 名称显式包含 DB（Pub/Sub 本身不按 DB 隔离）。不新增公共配置字段或通用瞬时 MQ API。配置中的 MQ 必须实际为对应内置 Provider；同名自定义工厂不视为已实现。shared 认证初始化失败即拒绝启动；缓存只允许既有显式 `RouteCache.Redis.OnUnavailable=bypass` 完全旁路，默认仍失败。

NATS 的 `StreamNameBySubject` 查询和发布不是原子事务。部署权限必须允许隔离校验，并禁止管理方并发创建捕获 `_core.notify.v1.>` 的 Stream；权限不足/隔离不可证明时拒绝相关 shared 能力。Redis/NATS 访问控制必须隔离应用与内部控制通道。内部编码不是认证或加密。

## 补偿、并发与关闭

- 通知桥只保存当前连接，整轮外部发布及本地交付预算 3 秒；失败使健康代次失效。恢复每 1 秒尝试一次，单次连接预算 3 秒，无历史扫描、离线队列或无界重连缓冲。
- 缓存每 1 秒读取已注册路由的权威 generation，单轮 3 秒预算。成功后推进本地记录版本，旧 L1/L2 记录逻辑失效，以补偿 key 级漏通知；物理 key 不扩展版本，不每秒扫描/删除 Badger。旧值仍按原 TTL/容量回收。热路径检查连接代次和最近对账新鲜度，5 秒未成功即旁路。对账期间再次断线不能用旧结果恢复。
- Casdoor WebSocket 从成功登录开始保护，即使没有任何路由订阅也受保护；每会话一个周期任务，不按订阅数倍增。每 1 秒调用权威 `Authorize`，单次 3 秒预算，每 Manager 最多 64 个检查在途，容量满直接失败，不排无界队列。独立 5 秒 watchdog 覆盖检查停滞。登录代次隔离旧任务，注销/重登取消旧任务；失效关闭连接，不自动恢复旧身份。
- 普通 HTTP 不依赖通知健康，仍验签、读取权威 generation/blocked；权威不可用继续 fail closed。其他认证域不因 Casdoor 通知故障被关闭。
- 这些时间是正常调度下的预算，不是进程停顿/OS 调度失效时的硬实时保证，也不构成广播线性一致性。通知刚丢失到下一轮补偿之间存在窗口。
- ServiceContext 持有连接和恢复任务，初始化失败和正常停止都关闭；缓存关闭等待其恢复任务退出。消息处理错误、解析错误或不确定状态保守失效；不记录 payload/token。

## 容量与观测

发现事件近似上限不是精确条数上限：Redis 按内部块裁剪，短期可略超过 10000；稳态规模不再与运行天数线性增长。估算内存须包含平均记录大小、索引和 Redis 结构开销，不能只用 XLEN 推断全部 RSS。

内部广播无 Broker 历史，但网络输出缓冲仍由 Broker 限制；运维应配置并监测 Redis Pub/Sub 输出缓冲、NATS pending/连接限制。流量约为 `通知速率 × 帧大小 × 在线副本数`，接收能力不足会导致显式失效和补偿，不产生无限历史。缓存对账读取量约 `副本数 × 已缓存路由数/秒`；认证查询量约 `共享 Casdoor 在线会话数/秒`，需对 Redis 和本地快照磁盘做容量预算，不能宣称此次短测证明生产峰值容量。

Runtime 固定组件 `internal-notification-cache`/`internal-notification-identity` 提供 `connections`（逻辑通道健康，不是 TCP 数）、发布成功/拒绝及连续性间隙计数。缓存另提供补偿成功/失败和最近耗时；从未执行时不提供耗时。身份周期检查数、关闭会话数、客户端队列压力、Broker RSS 此版未采集；pending/retained/ACK 对此通道不适用，均不填假零。日志只记录 service/kind 等稳定分类。

业务未消费积压、失败 Hook、Outbox、DLQ 与已消费历史必须分别观测。缺少安全回收策略时仍保守保留，不能因发现本次容量故障便为业务消息增加 MAXLEN、TTL 或 ACK 后删除。

## 升级、旧资源与 Bitzoom

1. 同环境、同逻辑服务安排维护窗口：停止旧协议实例后再启动新实例并开放流量。不支持旧 Stream 通知协议与新广播协议安全混跑；回滚同样协调停止再切换。
2. 使用既有 MQ/RouteCache/AuthRevocation 配置，检查上述原生权限和命名隔离；应用无需增加 XTRIM 或为这两个框架内部主题声明业务生命周期。
3. 验证每副本缓存恢复、Casdoor 撤销/会话保护、低基数健康指标，以及业务 MQ 的真实必需消费组。Bitzoom 成交事件仍按实际注册声明 `RequireMessageLifecycle`，不能套用内部无历史机制，也不猜测订阅服务名。
4. 旧 Redis `<MQ.RedisStream.Prefix>:<service>.routecache.invalidate`、`<Prefix>:<service>.auth.casdoor.identity.changed` 和 NATS 对应旧 Subject 的 Stream/consumer **不会自动删除**。NATS 名称含规范化和 hash，应通过精确 Subject 查询资源，不猜名字或通配删除。
5. 只有核对全部旧写入方已停止、资源仅含内部通知、无业务依赖后，由运维另行显式授权清理精确旧资源。升级阻止新增堆积，不等于释放存量内存；发现流会在后续有界 XADD 时近似裁剪。禁止用 FLUSHDB、全库 TTL 或全库 XTRIM 代替核对。

## 扩展与验收标准

新 Provider 必须分别证明业务可靠生命周期和内部瞬时通知能力；实现普通 MQProvider 或注册同名工厂都不等于内部广播已支持。选择原生广播前，必须存在可读权威、漏通知补偿、连续性检测、有界队列与失败保护；否则保留可靠业务消息并显式声明组和生命周期，不静默降级。

必测 RED → GREEN → race：超过发现上限、两副本与独立进程广播、Broker 重启、慢消费者、异步错误、错误帧、漏发布后的缓存/身份收敛、空闲 WebSocket、初始化/关闭、持续发布无新历史、旧历史保留和业务生命周期不变。结果与 NOT RUN 边界见 [验证记录](CORE_INTERNAL_NOTIFICATION_VERIFICATION.md)。
