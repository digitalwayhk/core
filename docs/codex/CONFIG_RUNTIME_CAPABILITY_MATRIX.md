# 配置到运行时能力矩阵

状态只有三种：`supported` 表示已有运行时消费方和行为测试；`rejected` 表示非默认启用或自定义值会在校验/构造阶段明确失败；`upstream` 表示由 go-zero 直接拥有。`Cluster.Mode=off` 和 `MQ.Mode=off` 只校验 Mode 本身，其余字段不做语义校验且不会进入运行时；这用于旧 JSON 禁用后迁移。未启用能力的零值或固定默认值可继续解析，但不代表该能力已实现。

## Server 与认证

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| `ServerConfig.RestConf` | go-zero 默认值和校验 | go-zero rest server | upstream |
| ID、访问控制和运行时地址字段 | 持久配置只保存稳定输入；实例地址由 ServiceContext 捕获，`Cluster.AdvertiseAddress` 可显式覆盖 | ServiceContext、router、REST request | supported |
| 三组 Auth 的 JWT、CasDoor 字段 | JWT 直接消费；CasDoor 仅在 Enable=true 时加载，false 为 inactive 默认 | auth middleware；外部认证配置由进程持有 | supported |
| `MelodyConfigPath` | 空值 inactive；非空时 `ReloadExternalConfigs` 加载 | Melody 全局配置 | supported |

## Cluster

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| Mode、Provider、HeartbeatInterval、MachineIDMax、Etcd Endpoints/Prefix/TTL、Consul Address | 由 Validate、factory、claim 和 membership 消费；Etcd Prefix 为空时默认 `/core/cluster`，支持自定义 keyspace；未知 provider 是构造错误，仅已知外部 provider 在 `Mode=auto` 连接失败时回退 local | ServiceContext 创建并终止 membership、broker 和 provider | supported |
| AdvertiseAddress、自动 MachineID claim | 显式广播地址覆盖 ServiceContext 捕获的本机地址；自动 MachineID 在 Snowflake 初始化前通过 Provider 申请 lease | membership、ServiceContext、ClusterProvider | supported |
| NodeName、自动 DataCenterID claim、额外 conflict policy、discovery、shard、services | 非零/非默认配置明确返回 not implemented | 无 | rejected |
| HeartbeatTimeout、SuspectTimeout、InstanceReuseCooldown、DataCenterIDMax、Consul Prefix/TTL | 仅固定默认值可通过；自定义值返回 not configurable。固定值用于兼容现有配置，当前 LocalProvider 仍按固定构造值工作 | 固定 LocalProvider 行为 | rejected |
| `Mode=off` 下的旧字段 | 不创建 cluster runtime，保留旧配置解析兼容 | inactive，无生命周期对象 | rejected |

## Transport

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| Internal/Fallback 的 grpc、http；MaxRetries/RetryDelay；gRPC message size | selector、ServiceContext retry、gRPC transport 消费；自定义 MaxRecv/MaxSend 经 ApplyDefaults 保持不变 | ServiceContext/TransportSelector | supported |
| Internal/Fallback 的 quic、mq；HTTP Enable；QUIC 配置 | 非默认启用时 Validate 明确失败；false/空值只是 inactive 默认 | 无 | rejected |
| GRPC Port | 0 时按 HTTP 端口派生，显式值必须在 1..65535；节点通过 `NodeInfo.GRPCPort` 发布并由 Resolver 组装端点 | gRPC server 生命周期、ServiceResolver、TransportSelector | supported |

## MQ 与 Event

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| Mode、event-stream Usage、Redis Stream、NATS JetStream | factory 创建 provider，ServiceContext 创建 EventStream/EventBridge | ServiceContext 终止型关闭 MQManager；Health/Publish/Subscribe 的 provider 调用受 Manager 读写门禁保护，Close 等待已进入调用并阻止新调用后再按稳定 registry key/name 顺序关闭去重实例；不支持关闭后复用 | supported |
| 自定义 Provider | 通过 `RegisterProviderFactory` 注册后由 factory 创建；未注册名称是硬配置错误；测试注册必须用 `t.Cleanup` 调用 `UnregisterProviderFactory` 隔离全局状态 | MQManager/已注册 factory | supported |
| kafka、rabbitmq、rocketmq 内建 provider | 未注册同名自定义 factory 时 BuildManager 返回 not implemented | 无内建 owner | rejected |
| transport/websocket/delayed-task Usage、request/reply、retry、dead-letter、dynamic switch | 启用时 Validate 明确失败；Enable=false 和预填默认参数只是 inactive/旧配置兼容 | 无 | rejected |
| `Mode=off` 下的旧 MQ 字段 | 不创建 manager/provider/event bridge，保留旧配置解析兼容 | inactive，无生命周期对象 | rejected |

## Persistence 边界

持久化配置由 `pkg/persistence` 各 adapter 拥有，不属于 `ServerConfig`。未来若把 persistence 字段加入 `ServerConfig`，矩阵一致性测试会要求同步更新下面的机器检查清单。

### ReliableWriteStoreConfig

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| `BasePath` + `ServiceIdentity` | 构造时解析为 `<base>/<service>/dc-N/machine-N`；服务名必须是安全目录片段，ID 不能为负数 | `NewReliableWriteStore`，目录在 store 生命周期内固定 | supported |
| `Badger` | 构造时覆盖 `Path` 为已解析实例目录；`SyncWrites`、冲突检测和损坏策略在绑定 target 时校验 | `SharedBadgerManager`、`PrefixedBadgerDB` | supported |
| `Badger.AutoSync` | 只控制框架内置 write-behind worker；`false` 仍保留 pending 与手动 `ForceSyncBatch/All` | `PrefixedBadgerDB.startWriteBehindWorker` | supported |
| `Badger.SyncBatchSize` | 框架 worker 和 `ForceSyncAll` 的单轮批次，同时是手动 `ForceSyncBatch(ctx, limit)` 的硬上限；调用方 limit 更大时自动截断 | `PrefixedBadgerDB.forceSyncBatch`、`ForceSyncAllContext` | supported |
| `Batch.MaxBatch`、`CollectWindow`、`QueueCapacity` | 构造时补默认值并拒绝 queue 小于 max batch | `BatchCommitter` 从接收写到本地事务完成 | supported |
| `Admission.MaxConcurrent`、`AcquireTimeout` | 每次进入 Group Commit 前读取 | `WriteAdmissionController` | supported |
| `Admission.SoftPending`、`HardPending`、`MaxBacklogDuration` | 每次写入用 O(1) pending 快照检查持续积压 | `WriteAdmissionController` | supported |
| `Admission.HardDiskBytes` | 每次写入读取 Badger 原生 LSM + VLog 大小 | `WriteAdmissionController` | supported |
| `CloseTimeout` | `Close(ctx)` 计算等待本地 drain 和 prefix 关闭的上限 | `ReliableWriteStore`，由 `ServiceContext.UseResource` 统一关闭 | supported |

业务自建 ticker 调用 `ForceSyncBatch` 与 `Badger.AutoSync` 是两个不同层次：前者是业务选择的 bounded drain 调度，后者只控制框架内置 worker，不能把 `AutoSync=false` 描述成“禁止所有同步”。

## 机器检查字段清单

| 字段路径 | 状态 | 生命周期 owner | 运行时/拒绝证据 |
| --- | --- | --- | --- |
| `ServerConfig.RestConf` | upstream | go-zero rest server | rest.MustNewServer 消费嵌入配置；门禁只跟踪嵌入点 |
| `ServerConfig.DataCenterID` | supported | ServiceContext | 服务初始化和 ID 生成消费 |
| `ServerConfig.MachineID` | supported | ServiceContext | 服务初始化和 ID 生成消费 |
| `ServerConfig.Auth` | supported | auth middleware | 认证容器由 router 装配并按子字段启用 |
| `ServerConfig.Auth.AccessSecret` | supported | JWT auth middleware | JWT 签名校验消费 |
| `ServerConfig.Auth.AccessExpire` | supported | JWT auth middleware | JWT 过期时间消费 |
| `ServerConfig.Auth.RefreshSecret` | supported | auth token issuer | Refresh Token 签名和刷新校验消费 |
| `ServerConfig.Auth.RefreshExpire` | supported | auth token issuer | Refresh Token 有效期消费 |
| `ServerConfig.Auth.CasDoor` | supported | CasDoor auth middleware | Enable 控制外部配置生命周期 |
| `ServerConfig.Auth.CasDoor.Enable` | supported | CasDoor auth middleware | ReloadExternalConfigs 和 middleware 装配消费 |
| `ServerConfig.Auth.CasDoor.YamlFilePath` | supported | CasDoor auth middleware | ReloadConfig 加载指定文件 |
| `ServerConfig.Auth.CasDoor.WebhookSecret` | supported | ServerConfig/Casdoor webhook | 配置层强制独立密钥；Webhook 运行时由认证生命周期接入 |
| `ServerConfig.ManageAuth` | supported | manage auth middleware | 管理路由认证容器装配 |
| `ServerConfig.ManageAuth.AccessSecret` | supported | manage JWT middleware | JWT 签名校验消费 |
| `ServerConfig.ManageAuth.AccessExpire` | supported | manage JWT middleware | JWT 过期时间消费 |
| `ServerConfig.ManageAuth.RefreshSecret` | supported | manage token issuer | 管理端 Refresh Token 签名和刷新校验消费 |
| `ServerConfig.ManageAuth.RefreshExpire` | supported | manage token issuer | 管理端 Refresh Token 有效期消费 |
| `ServerConfig.ManageAuth.CasDoor` | supported | manage CasDoor middleware | Enable 控制外部配置生命周期 |
| `ServerConfig.ManageAuth.CasDoor.Enable` | supported | manage CasDoor middleware | ReloadExternalConfigs 和 middleware 装配消费 |
| `ServerConfig.ManageAuth.CasDoor.YamlFilePath` | supported | manage CasDoor middleware | ReloadConfig 加载指定文件 |
| `ServerConfig.ManageAuth.CasDoor.WebhookSecret` | supported | ServerConfig/Casdoor webhook | 与 Auth、JWT、ClientSecret 密钥隔离；Webhook 运行时由认证生命周期接入 |
| `ServerConfig.ServerManageAuth` | supported | server-manage auth middleware | 服务管理路由认证容器装配 |
| `ServerConfig.ServerManageAuth.AccessSecret` | supported | server-manage JWT middleware | JWT 签名校验消费 |
| `ServerConfig.ServerManageAuth.AccessExpire` | supported | server-manage JWT middleware | JWT 过期时间消费 |
| `ServerConfig.ServerManageAuth.RefreshSecret` | rejected | server-manage token issuer | servermanage 仅颁发 Access Token，默认必须为空 |
| `ServerConfig.ServerManageAuth.RefreshExpire` | rejected | server-manage token issuer | servermanage 不支持刷新，默认必须为零 |
| `ServerConfig.ServerManageAuth.CasDoor` | supported | server-manage CasDoor middleware | Enable 控制外部配置生命周期 |
| `ServerConfig.ServerManageAuth.CasDoor.Enable` | supported | server-manage CasDoor middleware | ReloadExternalConfigs 和 middleware 装配消费 |
| `ServerConfig.ServerManageAuth.CasDoor.YamlFilePath` | supported | server-manage CasDoor middleware | ReloadConfig 加载指定文件 |
| `ServerConfig.ServerManageAuth.CasDoor.WebhookSecret` | rejected | ServerConfig.Validate | ServerManage 不接入 Casdoor Webhook，非空配置明确拒绝 |
| `ServerConfig.IsWhiteList` | supported | access-control middleware | 白名单开关消费 |
| `ServerConfig.WhiteList` | supported | access-control middleware | 白名单匹配消费 |
| `ServerConfig.TrustedProxies` | supported | REST request handling | Validate 校验 IP/CIDR，请求来源解析消费 |
| `ServerConfig.IsLoaclVisit` | supported | access-control middleware | 本地访问控制分支消费 |
| `ServerConfig.RemoteAccessManageAPI` | supported | manage access control | 远程管理 API 访问控制消费 |
| `ServerConfig.MelodyConfigPath` | supported | Melody global config | 非空时 ReloadExternalConfigs 加载 |
| `ServerConfig.Cluster` | supported | ServiceContext | ApplyDefaults/Validate 并构造 cluster runtime |
| `ServerConfig.Cluster.Mode` | supported | ServiceContext | 决定不创建、自动或强制 cluster runtime |
| `ServerConfig.Cluster.Provider` | supported | cluster factory | 选择 local/etcd/consul/redis provider，未知值拒绝 |
| `ServerConfig.Cluster.NodeName` | rejected | ClusterConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.Cluster.AdvertiseAddress` | supported | membership runtime | 非空时作为服务发现广播地址；否则使用 ServiceContext 创建时捕获的本机地址，REST 监听仍由 Host 决定 |
| `ServerConfig.Cluster.HeartbeatInterval` | supported | membership runtime | heartbeat ticker 构造消费 |
| `ServerConfig.Cluster.HeartbeatTimeout` | rejected | ClusterConfig.Validate | 仅允许固定兼容值，自定义值拒绝 |
| `ServerConfig.Cluster.SuspectTimeout` | rejected | ClusterConfig.Validate | 仅允许固定兼容值，自定义值拒绝 |
| `ServerConfig.Cluster.InstanceReuseCooldown` | rejected | ClusterConfig.Validate | 仅允许固定兼容值，自定义值拒绝 |
| `ServerConfig.Cluster.Claim` | supported | ClusterConfig.Validate、ServiceContext、ClusterProvider | 容器由子字段分别声明支持或拒绝；`AutoMachineID` 与 `MachineIDMax` 已接入 |
| `ServerConfig.Cluster.Claim.AutoMachineID` | supported | ServiceContext + ClusterProvider | true 时在 Snowflake 初始化前通过当前 Provider 申请 MachineID lease，并写回运行时配置 |
| `ServerConfig.Cluster.Claim.AutoDataCenterID` | rejected | ClusterConfig.Validate | true 返回 not implemented |
| `ServerConfig.Cluster.Claim.MachineIDMax` | supported | machine ID claim | claim 范围构造消费 |
| `ServerConfig.Cluster.Claim.DataCenterIDMax` | rejected | ClusterConfig.Validate | 仅允许固定兼容值，自定义值拒绝 |
| `ServerConfig.Cluster.Claim.ConflictPolicy` | rejected | ClusterConfig.Validate | 非固定兼容策略返回 not implemented |
| `ServerConfig.Cluster.Discovery` | rejected | ClusterConfig.Validate | 任一 discovery 能力启用都在启动前拒绝 |
| `ServerConfig.Cluster.Discovery.Seeds` | rejected | ClusterConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.Cluster.Discovery.Multicast` | rejected | ClusterConfig.Validate | true 返回 not implemented |
| `ServerConfig.Cluster.Discovery.MDNS` | rejected | ClusterConfig.Validate | true 返回 not implemented |
| `ServerConfig.Cluster.Shard` | rejected | ClusterConfig.Validate | 任一 shard 策略配置都在启动前拒绝 |
| `ServerConfig.Cluster.Shard.MissingKeyPolicy` | rejected | ClusterConfig.Validate | 非默认值返回 not implemented |
| `ServerConfig.Cluster.Shard.EmptyCandidatePolicy` | rejected | ClusterConfig.Validate | 非默认值返回 not implemented |
| `ServerConfig.Cluster.Shard.KeyPriority` | rejected | ClusterConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.Cluster.Services` | rejected | ClusterConfig.Validate | 非空 service shard map 返回 not implemented |
| `ServerConfig.Cluster.Providers` | supported | cluster factory | provider 容器按 Provider 选择并验证子配置 |
| `ServerConfig.Cluster.Providers.Etcd` | supported | EtcdProvider | factory 透传 endpoints/prefix/TTL 并关闭 client |
| `ServerConfig.Cluster.Providers.Etcd.Endpoints` | supported | EtcdProvider | clientv3 客户端构造消费，强制模式缺失时拒绝 |
| `ServerConfig.Cluster.Providers.Etcd.Prefix` | supported | EtcdProvider | factory 透传至 node/service/claim key 构造 |
| `ServerConfig.Cluster.Providers.Etcd.TTL` | supported | EtcdProvider | lease grant 和 keepalive 消费 |
| `ServerConfig.Cluster.Providers.Consul` | supported | ConsulProvider | factory 依 Provider 构造并关闭 client |
| `ServerConfig.Cluster.Providers.Consul.Address` | supported | ConsulProvider | Consul client 构造消费 |
| `ServerConfig.Cluster.Providers.Consul.Prefix` | rejected | ClusterConfig.Validate | 仅允许固定兼容值，自定义值拒绝 |
| `ServerConfig.Cluster.Providers.Consul.TTL` | rejected | ClusterConfig.Validate | 仅允许固定兼容值，自定义值拒绝 |
| `ServerConfig.Cluster.Providers.Redis` | supported | RedisProvider | factory 透传 Redis 连接、前缀和租约配置并关闭 client |
| `ServerConfig.Cluster.Providers.Redis.Addr` | supported | RedisProvider | Redis client 构造消费，强制模式缺失时拒绝 |
| `ServerConfig.Cluster.Providers.Redis.DB` | supported | RedisProvider | Redis DB 选择消费 |
| `ServerConfig.Cluster.Providers.Redis.Prefix` | supported | RedisProvider | 节点、槽位、服务索引和 Watch Stream 键前缀 |
| `ServerConfig.Cluster.Providers.Redis.TTL` | supported | RedisProvider | 节点、索引和 MachineID 槽位租约时长 |
| `ServerConfig.Transport` | supported | ServiceContext | ApplyDefaults/Validate 并构造 TransportSelector |
| `ServerConfig.Transport.Internal` | supported | TransportSelector | grpc/http 主协议选择，其他值拒绝 |
| `ServerConfig.Transport.Fallback` | supported | TransportSelector | grpc/http 降级顺序，其他值拒绝 |
| `ServerConfig.Transport.MaxRetries` | supported | ServiceContext transport retry | 请求重试计数消费 |
| `ServerConfig.Transport.RetryDelay` | supported | ServiceContext transport retry | 请求重试间隔消费 |
| `ServerConfig.Transport.HTTP` | rejected | TransportConfig.Validate | Enable 容器不是协议选择入口，启用时拒绝 |
| `ServerConfig.Transport.HTTP.Enable` | rejected | TransportConfig.Validate | true 返回 not implemented，改用 Internal/Fallback |
| `ServerConfig.Transport.QUIC` | rejected | TransportConfig.Validate | 任一 QUIC 启用或文件配置都拒绝 |
| `ServerConfig.Transport.QUIC.Enable` | rejected | TransportConfig.Validate | true 返回 not implemented |
| `ServerConfig.Transport.QUIC.CertFile` | rejected | TransportConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.Transport.QUIC.KeyFile` | rejected | TransportConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.Transport.GRPC` | supported | gRPC transport | Port、message size 和安全配置由 client/server 构造消费 |
| `ServerConfig.Transport.GRPC.Port` | supported | gRPC server、NodeInfo、ServiceResolver | 0 按 HTTP Port 派生；显式值允许 1..65535 |
| `ServerConfig.Transport.GRPC.MaxRecvMsgSize` | supported | gRPC transport | ApplyDefaults 保留自定义值并透传构造器 |
| `ServerConfig.Transport.GRPC.MaxSendMsgSize` | supported | gRPC transport | ApplyDefaults 保留自定义值并透传构造器 |
| `ServerConfig.Transport.GRPC.Security` | supported | gRPC client/server | 按 Mode 构造标准 transport credentials 或委托 mesh |
| `ServerConfig.Transport.GRPC.Security.Mode` | supported | TransportConfig.Validate、gRPC client/server | insecure/tls/mtls/mesh；外部发现默认 mtls |
| `ServerConfig.Transport.GRPC.Security.CAFile` | supported | gRPC client/server TLS | tls/mtls 加载 CA；缺失或无效时启动失败 |
| `ServerConfig.Transport.GRPC.Security.CertFile` | supported | gRPC client/server TLS | mtls 加载服务证书；缺失或无效时启动失败 |
| `ServerConfig.Transport.GRPC.Security.KeyFile` | supported | gRPC client/server TLS | mtls 加载私钥；缺失或无效时启动失败 |
| `ServerConfig.Transport.GRPC.Security.ServerName` | supported | zrpc client TLS | 固定名称或 `{service}` 动态目标服务名校验 |
| `ServerConfig.RouteCache` | supported | ServiceContext | 规范化配置并创建服务级 RouteCacheManager |
| `ServerConfig.RouteCache.Mode` | supported | RouteCacheManager | 默认 local；shared 要求 Redis 与 EventBridge 外部失效订阅同时就绪；旧 off 兼容规范化为 local |
| `ServerConfig.RouteCache.TTL` | supported | RouteCacheManager | 作为路由未显式指定时的默认 TTL |
| `ServerConfig.RouteCache.L1` | supported | RouteCacheManager | 使用成熟 LRU 管理本地序列化缓存，按条目数与字节预算双重淘汰 |
| `ServerConfig.RouteCache.L1.MaxEntries` | supported | RouteCacheManager | 0 按自动字节预算/4 KiB 解析，上限 10000；非零时显式限制条目数 |
| `ServerConfig.RouteCache.L1.MaxValueBytes` | supported | RouteCacheManager | 默认 1 MiB；超限响应不写入 L1/L2/L3 |
| `ServerConfig.RouteCache.L1.MaxBytes` | supported | RouteCacheManager | 0 按有效内存 2% 自动解析为进程级 16–256 MiB 总预算；非零时使用显式值 |
| `ServerConfig.RouteCache.L1.Limit` | supported | RouteCacheManager | 废弃兼容字段，映射到 MaxEntries；新配置不应再使用 |
| `ServerConfig.RouteCache.L2` | supported | RouteCacheManager | 可选装配服务隔离的纯 Badger TTL 缓存 |
| `ServerConfig.RouteCache.L2.Enable` | supported | RouteCacheManager | true 时创建并在服务关闭时关闭 Badger L2 |
| `ServerConfig.RouteCache.L2.Path` | supported | BadgerL2 | 作为服务哈希子目录的根路径 |
| `ServerConfig.RouteCache.L2.MaxBytes` | supported | BadgerL2 | 写入前根据 Badger LSM/vlog 大小执行容量保护 |
| `ServerConfig.RouteCache.L2.CorruptionPolicy` | supported | BadgerL2 | fail 保留现场；显式 reset 才清空并重建 |
| `ServerConfig.RouteCache.Redis` | supported | RouteCacheManager/RedisL3 | 仅 shared 模式消费，使用 go-zero Redis 客户端 |
| `ServerConfig.RouteCache.Redis.Addr` | supported | RedisL3 | shared 默认要求非空并在启动时 Ping；不可用时按 OnUnavailable 处理 |
| `ServerConfig.RouteCache.Redis.Password` | supported | RedisL3 | 透传 go-zero RedisConf.Pass，不得写入日志 |
| `ServerConfig.RouteCache.Redis.DB` | rejected | RouteCacheConfig.Validate | go-zero Redis adapter 不消费 DB；仅允许 0，防止配置静默失效 |
| `ServerConfig.RouteCache.Redis.Prefix` | supported | RedisL3 | 作为所有共享缓存键的显式前缀 |
| `ServerConfig.RouteCache.Redis.OnUnavailable` | supported | RouteCacheManager | fail 默认阻止启动；显式 bypass 关闭 L1/L2/L3 全部缓存层 |
| `ServerConfig.AuthRevocation` | supported | ServerConfig/AuthRevocationManager | 配置层规范化；Casdoor 启用后由服务级撤销管理器消费 |
| `ServerConfig.AuthRevocation.Mode` | supported | AuthRevocationConfig.Validate | 仅接受 local/shared；shared 强制要求 Redis |
| `ServerConfig.AuthRevocation.BadgerPath` | supported | AuthRevocationManager | local 权威存储或 shared 已确认快照目录 |
| `ServerConfig.AuthRevocation.Redis` | supported | AuthRevocationManager | 仅 shared 模式消费，使用 go-zero Redis 客户端 |
| `ServerConfig.AuthRevocation.Redis.Addr` | supported | AuthRevocationConfig.Validate | shared 且 Casdoor 启用时必须非空 |
| `ServerConfig.AuthRevocation.Redis.Password` | supported | AuthRevocationManager | 透传 go-zero RedisConf.Pass，不得写入日志 |
| `ServerConfig.AuthRevocation.Redis.Prefix` | supported | AuthRevocationManager | 撤销状态、幂等事件和世代键命名空间 |
| `ServerConfig.MQ` | supported | ServiceContext | ApplyDefaults/Validate 并按 Mode 创建和关闭 MQManager |
| `ServerConfig.MQ.Mode` | supported | ServiceContext | 决定不创建、自动或强制 MQ runtime |
| `ServerConfig.MQ.Provider` | supported | MQ factory | 选择内建或注册 factory，未实现 provider 拒绝 |
| `ServerConfig.MQ.Usage` | supported | MQManager/EventBridge | event-stream 装配消费，其他 usage 启用时拒绝 |
| `ServerConfig.MQ.RequestReply` | rejected | MQConfig.Validate | 容器仅保留 inactive 默认，Enable 时拒绝 |
| `ServerConfig.MQ.RequestReply.Enable` | rejected | MQConfig.Validate | true 返回 not implemented |
| `ServerConfig.MQ.RequestReply.Timeout` | rejected | MQConfig.Validate | 仅固定 inactive 兼容值，自定义值拒绝 |
| `ServerConfig.MQ.Retry` | rejected | MQConfig.Validate | 容器仅保留 inactive 默认，Enable 时拒绝 |
| `ServerConfig.MQ.Retry.Enable` | rejected | MQConfig.Validate | true 返回 not implemented |
| `ServerConfig.MQ.Retry.RetryCount` | rejected | MQConfig.Validate | 仅固定 inactive 兼容值，自定义值拒绝 |
| `ServerConfig.MQ.Retry.InitialDelay` | rejected | MQConfig.Validate | 仅固定 inactive 兼容值，自定义值拒绝 |
| `ServerConfig.MQ.Retry.MaxDelay` | rejected | MQConfig.Validate | 仅固定 inactive 兼容值，自定义值拒绝 |
| `ServerConfig.MQ.DeadLetter` | rejected | MQConfig.Validate | 容器仅保留 inactive 默认，Enable 时拒绝 |
| `ServerConfig.MQ.DeadLetter.Enable` | rejected | MQConfig.Validate | true 返回 not implemented |
| `ServerConfig.MQ.DeadLetter.Topic` | rejected | MQConfig.Validate | 仅固定 inactive 兼容值，自定义值拒绝 |
| `ServerConfig.MQ.Switch` | rejected | MQConfig.Validate | 容器仅保留 inactive 默认，动态切换配置拒绝 |
| `ServerConfig.MQ.Switch.AllowDynamicSwitch` | rejected | MQConfig.Validate | true 返回 not implemented |
| `ServerConfig.MQ.Switch.Strategy` | rejected | MQConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.MQ.Switch.TargetProvider` | rejected | MQConfig.Validate | 非空值返回 not implemented |
| `ServerConfig.MQ.Switch.DualWriteDuration` | rejected | MQConfig.Validate | 非零值返回 not implemented |
| `ServerConfig.MQ.Switch.RollbackOnFailure` | rejected | MQConfig.Validate | 显式配置指针值作为动态切换能力拒绝 |
| `ServerConfig.MQ.RedisStream` | supported | RedisStream provider | factory 透传连接参数并由 MQManager 关闭 |
| `ServerConfig.MQ.RedisStream.Addr` | supported | RedisStream provider | Redis client 构造消费 |
| `ServerConfig.MQ.RedisStream.DB` | supported | RedisStream provider | Redis DB 选择消费 |
| `ServerConfig.MQ.RedisStream.Prefix` | supported | RedisStream provider | stream key/subject 构造消费 |
| `ServerConfig.MQ.NATSJetStream` | supported | NATS JetStream provider | factory 透传连接参数并由 MQManager 关闭 |
| `ServerConfig.MQ.NATSJetStream.URL` | supported | NATS JetStream provider | NATS 连接构造消费 |
| `ServerConfig.MQ.NATSJetStream.StreamPrefix` | supported | NATS JetStream provider | stream/subject 命名消费 |
| `ServerConfig.MQ.NATSJetStream.DurablePrefix` | supported | NATS JetStream provider | durable consumer 命名消费 |
| `ServerConfig.MQ.Kafka` | rejected | MQ factory | 未注册同名自定义 factory 时返回 not implemented |
| `ServerConfig.MQ.Kafka.Brokers` | rejected | MQ factory | 内建 Kafka provider 未实现，启用时拒绝 |
| `ServerConfig.MQ.Kafka.Prefix` | rejected | MQ factory | 内建 Kafka provider 未实现，启用时拒绝 |
| `ServerConfig.MQ.RabbitMQ` | rejected | MQ factory | 未注册同名自定义 factory 时返回 not implemented |
| `ServerConfig.MQ.RabbitMQ.URL` | rejected | MQ factory | 内建 RabbitMQ provider 未实现，启用时拒绝 |
| `ServerConfig.MQ.RabbitMQ.Exchange` | rejected | MQ factory | 内建 RabbitMQ provider 未实现，启用时拒绝 |
| `ServerConfig.MQ.RocketMQ` | rejected | MQ factory | 未注册同名自定义 factory 时返回 not implemented |
| `ServerConfig.MQ.RocketMQ.NameServers` | rejected | MQ factory | 内建 RocketMQ provider 未实现，启用时拒绝 |
| `ServerConfig.MQ.RocketMQ.Group` | rejected | MQ factory | 内建 RocketMQ provider 未实现，启用时拒绝 |
