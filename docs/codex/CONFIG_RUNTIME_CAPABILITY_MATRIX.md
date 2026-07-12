# 配置到运行时能力矩阵

状态只有三种：`supported` 表示已有运行时消费方和行为测试；`rejected` 表示非默认启用或自定义值会在校验/构造阶段明确失败；`upstream` 表示由 go-zero 直接拥有。`Mode=off` 保留旧配置、以及未启用能力的零值/固定默认值可以继续解析，只是兼容 inactive 配置，不代表该能力已实现。

## Server 与认证

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| `ServerConfig.RestConf` | go-zero 默认值和校验 | go-zero rest server | upstream |
| ID、地址、attach、访问控制和 customer data 字段 | 默认值由构造器或 `ApplyDefaults` 补齐 | ServiceContext、router、REST request、调用方 | supported |
| 三组 Auth 的 JWT、Logto、CasDoor 字段 | JWT 直接消费；Logto/CasDoor 仅在 Enable=true 时加载，false 为 inactive 默认 | auth middleware；外部认证配置由进程持有 | supported |
| `MelodyConfigPath` | 空值 inactive；非空时 `ReloadExternalConfigs` 加载 | Melody 全局配置 | supported |

## Cluster

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| Mode、Provider、HeartbeatInterval、MachineIDMax、Etcd Endpoints/TTL、Consul Address | 由 Validate、factory、claim 和 membership 消费 | ServiceContext 创建并终止 membership、broker 和 provider | supported |
| NodeName、AdvertiseAddress、自动 claim、额外 conflict policy、discovery、shard、services | 非零/非默认配置明确返回 not implemented | 无 | rejected |
| HeartbeatTimeout、SuspectTimeout、InstanceReuseCooldown、DataCenterIDMax、provider Prefix/Consul TTL | 仅固定默认值可通过；自定义值返回 not configurable。固定值用于兼容现有配置，当前 LocalProvider 仍按固定构造值工作 | 固定 LocalProvider 行为 | rejected |
| `Mode=off` 下的旧字段 | 不创建 cluster runtime，保留旧配置解析兼容 | inactive，无生命周期对象 | rejected |

## Transport

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| Internal/Fallback 的 grpc、http、socket；MaxRetries/RetryDelay；gRPC message size | selector、ServiceContext retry、gRPC transport 消费；自定义 MaxRecv/MaxSend 经 ApplyDefaults 保持不变 | ServiceContext/TransportSelector | supported |
| Internal/Fallback 的 quic、mq；HTTP/Socket/GRPC Enable；QUIC 配置 | 非默认启用时 Validate 明确失败；false/空值只是 inactive 默认 | 无 | rejected |
| GRPC Port | 仅 0 或固定默认 19090 可通过，自定义端口返回 not configurable；19090 是旧配置兼容默认，selector 不消费 | 无可配置端口 owner | rejected |

## MQ 与 Event

| 字段 | 当前契约 | 运行时消费方/生命周期 | 状态 |
| --- | --- | --- | --- |
| Mode、event-stream Usage、Redis Stream、NATS JetStream | factory 创建 provider，ServiceContext 创建 EventStream/EventBridge | ServiceContext 终止型关闭 MQManager；MQManager 关闭其拥有的 provider，不支持关闭后复用 | supported |
| 自定义 Provider | 通过 `RegisterProviderFactory` 注册后由 factory 创建；未注册名称是硬配置错误 | MQManager/已注册 factory | supported |
| kafka、rabbitmq、rocketmq 内建 provider | 未注册同名自定义 factory 时 BuildManager 返回 not implemented | 无内建 owner | rejected |
| transport/websocket/delayed-task Usage、request/reply、retry、dead-letter、dynamic switch | 启用时 Validate 明确失败；Enable=false 和预填默认参数只是 inactive/旧配置兼容 | 无 | rejected |
| `Mode=off` 下的旧 MQ 字段 | 不创建 manager/provider/event bridge，保留旧配置解析兼容 | inactive，无生命周期对象 | rejected |

## Persistence 边界

持久化配置由 `pkg/persistence` 各 adapter 拥有，不属于 `ServerConfig`。未来若把 persistence 字段加入 `ServerConfig`，矩阵一致性测试会要求同步更新下面的机器检查清单。

## 机器检查字段清单

以下每项与反射枚举的导出 Go 字段路径精确对应。map、slice、interface 和 pointer 是叶子；`ServerConfig.RestConf` 保留嵌入点但不递归枚举 go-zero 内部字段。

`ServerConfig.RestConf`
`ServerConfig.DataCenterID`
`ServerConfig.MachineID`
`ServerConfig.Auth`
`ServerConfig.Auth.AccessSecret`
`ServerConfig.Auth.AccessExpire`
`ServerConfig.Auth.Logto`
`ServerConfig.Auth.Logto.ExpectedAudience`
`ServerConfig.Auth.Logto.Issuer`
`ServerConfig.Auth.Logto.Enable`
`ServerConfig.Auth.CasDoor`
`ServerConfig.Auth.CasDoor.Enable`
`ServerConfig.Auth.CasDoor.YamlFilePath`
`ServerConfig.ManageAuth`
`ServerConfig.ManageAuth.AccessSecret`
`ServerConfig.ManageAuth.AccessExpire`
`ServerConfig.ManageAuth.Logto`
`ServerConfig.ManageAuth.Logto.ExpectedAudience`
`ServerConfig.ManageAuth.Logto.Issuer`
`ServerConfig.ManageAuth.Logto.Enable`
`ServerConfig.ManageAuth.CasDoor`
`ServerConfig.ManageAuth.CasDoor.Enable`
`ServerConfig.ManageAuth.CasDoor.YamlFilePath`
`ServerConfig.ServerManageAuth`
`ServerConfig.ServerManageAuth.AccessSecret`
`ServerConfig.ServerManageAuth.AccessExpire`
`ServerConfig.ServerManageAuth.Logto`
`ServerConfig.ServerManageAuth.Logto.ExpectedAudience`
`ServerConfig.ServerManageAuth.Logto.Issuer`
`ServerConfig.ServerManageAuth.Logto.Enable`
`ServerConfig.ServerManageAuth.CasDoor`
`ServerConfig.ServerManageAuth.CasDoor.Enable`
`ServerConfig.ServerManageAuth.CasDoor.YamlFilePath`
`ServerConfig.RunIp`
`ServerConfig.ParentServerIP`
`ServerConfig.SocketPort`
`ServerConfig.AttachServices`
`ServerConfig.Debug`
`ServerConfig.IsWhiteList`
`ServerConfig.WhiteList`
`ServerConfig.TrustedProxies`
`ServerConfig.CustomerDataList`
`ServerConfig.IsLoaclVisit`
`ServerConfig.RemoteAccessManageAPI`
`ServerConfig.MelodyConfigPath`
`ServerConfig.Cluster`
`ServerConfig.Cluster.Mode`
`ServerConfig.Cluster.Provider`
`ServerConfig.Cluster.NodeName`
`ServerConfig.Cluster.AdvertiseAddress`
`ServerConfig.Cluster.HeartbeatInterval`
`ServerConfig.Cluster.HeartbeatTimeout`
`ServerConfig.Cluster.SuspectTimeout`
`ServerConfig.Cluster.InstanceReuseCooldown`
`ServerConfig.Cluster.Claim`
`ServerConfig.Cluster.Claim.AutoMachineID`
`ServerConfig.Cluster.Claim.AutoDataCenterID`
`ServerConfig.Cluster.Claim.MachineIDMax`
`ServerConfig.Cluster.Claim.DataCenterIDMax`
`ServerConfig.Cluster.Claim.ConflictPolicy`
`ServerConfig.Cluster.Discovery`
`ServerConfig.Cluster.Discovery.Seeds`
`ServerConfig.Cluster.Discovery.Multicast`
`ServerConfig.Cluster.Discovery.MDNS`
`ServerConfig.Cluster.Shard`
`ServerConfig.Cluster.Shard.MissingKeyPolicy`
`ServerConfig.Cluster.Shard.EmptyCandidatePolicy`
`ServerConfig.Cluster.Shard.KeyPriority`
`ServerConfig.Cluster.Services`
`ServerConfig.Cluster.Providers`
`ServerConfig.Cluster.Providers.Etcd`
`ServerConfig.Cluster.Providers.Etcd.Endpoints`
`ServerConfig.Cluster.Providers.Etcd.Prefix`
`ServerConfig.Cluster.Providers.Etcd.TTL`
`ServerConfig.Cluster.Providers.Consul`
`ServerConfig.Cluster.Providers.Consul.Address`
`ServerConfig.Cluster.Providers.Consul.Prefix`
`ServerConfig.Cluster.Providers.Consul.TTL`
`ServerConfig.Transport`
`ServerConfig.Transport.Internal`
`ServerConfig.Transport.Fallback`
`ServerConfig.Transport.MaxRetries`
`ServerConfig.Transport.RetryDelay`
`ServerConfig.Transport.HTTP`
`ServerConfig.Transport.HTTP.Enable`
`ServerConfig.Transport.Socket`
`ServerConfig.Transport.Socket.Enable`
`ServerConfig.Transport.QUIC`
`ServerConfig.Transport.QUIC.Enable`
`ServerConfig.Transport.QUIC.CertFile`
`ServerConfig.Transport.QUIC.KeyFile`
`ServerConfig.Transport.GRPC`
`ServerConfig.Transport.GRPC.Enable`
`ServerConfig.Transport.GRPC.Port`
`ServerConfig.Transport.GRPC.MaxRecvMsgSize`
`ServerConfig.Transport.GRPC.MaxSendMsgSize`
`ServerConfig.MQ`
`ServerConfig.MQ.Mode`
`ServerConfig.MQ.Provider`
`ServerConfig.MQ.Usage`
`ServerConfig.MQ.RequestReply`
`ServerConfig.MQ.RequestReply.Enable`
`ServerConfig.MQ.RequestReply.Timeout`
`ServerConfig.MQ.Retry`
`ServerConfig.MQ.Retry.Enable`
`ServerConfig.MQ.Retry.RetryCount`
`ServerConfig.MQ.Retry.InitialDelay`
`ServerConfig.MQ.Retry.MaxDelay`
`ServerConfig.MQ.DeadLetter`
`ServerConfig.MQ.DeadLetter.Enable`
`ServerConfig.MQ.DeadLetter.Topic`
`ServerConfig.MQ.Switch`
`ServerConfig.MQ.Switch.AllowDynamicSwitch`
`ServerConfig.MQ.Switch.Strategy`
`ServerConfig.MQ.Switch.TargetProvider`
`ServerConfig.MQ.Switch.DualWriteDuration`
`ServerConfig.MQ.Switch.RollbackOnFailure`
`ServerConfig.MQ.RedisStream`
`ServerConfig.MQ.RedisStream.Addr`
`ServerConfig.MQ.RedisStream.DB`
`ServerConfig.MQ.RedisStream.Prefix`
`ServerConfig.MQ.NATSJetStream`
`ServerConfig.MQ.NATSJetStream.URL`
`ServerConfig.MQ.NATSJetStream.StreamPrefix`
`ServerConfig.MQ.NATSJetStream.DurablePrefix`
`ServerConfig.MQ.Kafka`
`ServerConfig.MQ.Kafka.Brokers`
`ServerConfig.MQ.Kafka.Prefix`
`ServerConfig.MQ.RabbitMQ`
`ServerConfig.MQ.RabbitMQ.URL`
`ServerConfig.MQ.RabbitMQ.Exchange`
`ServerConfig.MQ.RocketMQ`
`ServerConfig.MQ.RocketMQ.NameServers`
`ServerConfig.MQ.RocketMQ.Group`
