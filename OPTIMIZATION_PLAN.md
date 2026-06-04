# core 项目优化计划

本文基于当前已分析的有效目录：`docs`、`examples`、`pkg`、`service`、`web`。其中 `pkg` 下当前重点是 `json`、`persistence`、`server`、`utils`；`dec`、`fileserver`、`localization` 暂不作为核心功能继续投入。

目标不是把 core 改成普通 Web 框架，而是强化它作为“标准商业业务开发框架”的能力：业务逻辑、持久化、服务提供、管理后台、接口文档、内部通信、水平扩展都保持统一约束，并允许按配置切换实现。

## 优化目标

1. 目录职责清晰，历史/暂停模块不干扰主线。
2. 业务 API、管理 API、WebSocket、OpenAPI 的接口语义更稳定。
3. `server` 层支持自动水平扩展，默认不依赖外部服务。
4. 水平扩展默认使用 core 自研注册发现，也可通过配置接入 etcd、Consul，并支持动态切换。
5. 内部传输支持 HTTP、Socket、QUIC、gRPC、Message Queue，并通过统一接口选择。
6. `persistence` 层继续保持“业务只决定保存时机，技术层决定保存方式”。
7. `service/manage` 和 `web/admin/WayPlus` 的前后端契约更清楚、更少代码完成管理功能。

## 目录结构优化

建议保留主线目录：

```text
docs/              项目文档、架构图、优化计划
examples/          官方示例，优先维护 demo
pkg/
  json/            JSON 序列化统一入口
  persistence/     数据实体、查询、持久化抽象
  server/          服务、路由、传输、鉴权、OpenAPI、集群
  utils/           工具函数
service/
  manage/          管理后台后端自动生成能力
web/
  admin/           Ant Design Pro 管理前端 submodule
```

建议新增或调整：

```text
pkg/server/cluster/       节点发现、成员管理、服务注册、水平扩展
pkg/server/transport/     统一内部传输抽象，承载 HTTP/gRPC/socket/QUIC/MQ
docs/architecture/        架构设计说明
docs/optimization/        优化计划、迁移计划、验证记录
legacy/                   暂停维护但暂不删除的历史模块
```

`pkg/dec`、`pkg/fileserver`、`pkg/localization` 可先不移动代码，但要在文档中明确“暂不维护”。后续若确认长期不用，再迁入 `legacy/` 或删除。

## server 层水平扩展方案

### 设计原则

server 层水平扩展采用“内置优先，可配置外部协调”的策略：

1. 默认模式不依赖任何外部服务。
2. 服务注册发现默认使用 core 自研 provider。
3. 通过配置可切换到 etcd、Consul 等外部注册/协调后端。
4. Redis 不作为服务注册发现 provider；如启用 Redis，只用于 MQ/event-stream/cache 等传输或缓存场景。
5. 切换不应影响业务路由、`ServiceContext.CallService`、`req.CallService` 的使用方式。
6. 运行期支持动态切换 discovery/provider，但需要有安全迁移状态。
7. 服务实例应能动态加入、离开、故障剔除和恢复。

### 建议新增 cluster 包

```text
pkg/server/cluster/
  node.go             节点身份、状态、权重、版本、标签
  service.go          服务实例、路由摘要、健康信息
  registry.go         服务注册表接口
  discovery.go        服务发现接口
  membership.go       成员管理、心跳、故障检测
  provider.go         provider 抽象和动态切换
  provider_local.go   自研 P2P/local provider
  provider_etcd.go    etcd provider
  provider_consul.go  Consul provider
  balancer.go         负载均衡
  event.go            跨节点事件和 WebSocket 通知转发
  config.go           集群配置
```

### 核心接口

```go
type NodeInfo struct {
    NodeID      string
    ServiceName string
    Address     string
    Port        int
    SocketPort  int
    GRPCPort    int
    Version     string
    Weight      int
    Tags        map[string]string
    Shards      map[string][]string
    RoutesHash  string
    UpdatedAt   time.Time
}

type DiscoveryProvider interface {
    Name() string
    Start(ctx context.Context, self NodeInfo) error
    Stop(ctx context.Context) error
    Register(ctx context.Context, self NodeInfo) error
    Unregister(ctx context.Context, self NodeInfo) error
    List(ctx context.Context, serviceName string) ([]NodeInfo, error)
    Watch(ctx context.Context, fn func(RegistryEvent)) error
    Healthy(ctx context.Context) error
}

type ProviderSwitcher interface {
    Current() DiscoveryProvider
    Switch(ctx context.Context, target string) error
}
```

其中 `Tags` 适合描述环境、版本、机房、能力标签；`Shards` 适合描述服务拆分后的业务筛选标记，例如 `userid`、`accountTypeid`、`tenantId`、`symbol`。

### 默认自研模式

无外部服务依赖时，建议采用：

- 本机多进程：本地文件/UDP loopback/端口扫描发现。
- 局域网：UDP multicast 或 mDNS 发现。
- 跨网段：配置 seed 节点列表，使用 P2P gossip 同步成员表。

配置示例：

```yaml
cluster:
  mode: auto
  provider: local
  nodeName: ""
  advertiseAddress: ""
  heartbeatInterval: 3s
  heartbeatTimeout: 10s
  seeds:
    - "10.0.0.10:18080"
    - "10.0.0.11:18080"
```

注意：完全不依赖外部服务时，跨网段自动发现无法凭空完成，必须依赖 seed 或网络广播能力。

### 服务实例身份与 MachineID 自动扩展

水平扩展模式下，服务实例身份应以 `types.IService.ServiceName()` 返回值为服务名，并结合配置中的 `DataCenterID + MachineID` 确定同一服务下的一个运行实例。

建议实例逻辑身份：

```go
type InstanceID struct {
    ServiceName  string
    DataCenterID uint
    MachineID    uint
}
```

registry key 建议：

```text
/services/{serviceName}/instances/{dataCenterID}/{machineID}
```

节点注册信息：

```go
type ServiceInstance struct {
    ServiceName    string
    DataCenterID   uint
    MachineID      uint
    NodeID         string
    Address        string
    Port           int
    GRPCPort       int
    SocketPort     int
    Status         string
    StartTime      time.Time
    LastHeartbeat  time.Time
    LeaseExpiresAt time.Time
}
```

启动注册规则：

1. 读取服务配置，得到 `ServiceName`、`DataCenterID`、`MachineID`。
2. 在初始化 Snowflake worker 前，先向 cluster registry 认领实例身份。
3. 如果 `ServiceName + DataCenterID + MachineID` 不存在，直接认领。
4. 如果存在但已过期、不可达或明确下线，允许接管。
5. 如果存在且活跃运行，则自动分配同 `ServiceName + DataCenterID` 下新的 `MachineID`。
6. 如果当前 `DataCenterID` 下 `MachineID` 已用尽，再尝试扩展 `DataCenterID`；仍失败则启动失败并给出明确错误。
7. 认领成功后，把最终 `DataCenterID + MachineID` 写入运行时配置，再初始化 Snowflake。
8. 节点启动成功后开始 heartbeat，维持租约。

这个规则可以支持 Docker 自动扩容：多个容器即使从同一份配置启动，初始 `MachineID` 相同，也会在注册阶段自动错开实例 ID，避免 Snowflake ID 冲突和服务实例冲突。

重要约束：实例身份认领必须发生在 `ServiceContext` 初始化 Snowflake 之前。当前初始化逻辑中 `sc.snow = utils.NewAlgorithmSnowFlake(con.MachineID, con.DataCenterID)` 应在未来调整到 cluster claim 之后。

建议启动顺序：

```text
ReadConfig
  -> cluster claim ServiceName/DataCenterID/MachineID
  -> update runtime config
  -> init Snowflake
  -> init routes
  -> register service node
  -> start heartbeat
  -> start transports
```

### 实例下线处理

实例下线需要同时支持优雅下线和异常失联。

#### 优雅下线

服务收到停止信号或 `WebServer.Stop()` 时，应执行：

1. 将实例状态改为 `draining`，停止接收新请求或从负载均衡中摘除。
2. 等待正在执行的请求完成，或到达最大等待时间。
3. 主动关闭 WebSocket 订阅或向客户端发送重连提示。
4. 注销当前实例 registry 记录，或写入 `offline` 状态。
5. 停止 heartbeat。
6. 停止 transport server。
7. 释放本地资源。

状态流转：

```text
starting -> running -> draining -> offline
```

#### 异常失联

如果实例没有主动下线，registry/provider 通过租约和 heartbeat 判断：

```text
running -> suspect -> expired -> offline
```

建议参数：

```yaml
cluster:
  heartbeatInterval: 3s
  heartbeatTimeout: 10s
  suspectTimeout: 15s
  instanceReuseCooldown: 30s
```

处理规则：

- 超过 `heartbeatTimeout` 未续约，标记为 `suspect`，负载均衡不再优先选择。
- 超过 `suspectTimeout` 仍未恢复，标记为 `expired/offline`。
- 进入 `offline` 后，`DataCenterID + MachineID` 不应立即复用，应等待 `instanceReuseCooldown`。
- 冷却期用于避免网络抖动导致两个活跃实例短暂使用同一 Snowflake ID 空间。

#### MachineID 回收

`MachineID` 可以回收，但必须满足：

1. 原实例已明确 `offline`，或租约已过期。
2. 已超过 `instanceReuseCooldown`。
3. 新实例 claim 时再次检查原实例不可达。

如果 provider 是 etcd/Consul，应优先利用它们的 lease/session/health check 能力；如果 provider 是自研 local/P2P，应由 membership 模块维护租约和故障检测。

#### WebSocket 下线

实例下线时，本地 WebSocket 客户端会断开。为了让集群内通知不丢失：

- 下线前广播订阅摘要删除事件。
- 其他节点清理该实例的订阅索引。
- 客户端重连到任意健康节点后重新订阅。
- 如果使用 gRPC stream 或 Redis pub/sub 转发事件，下线节点必须停止消费和发布。

#### 正在执行请求

优雅下线阶段应支持 draining：

- 新请求不再路由到该实例。
- 已进入本实例的请求继续执行。
- 超过 drain timeout 后强制关闭。
- 对幂等请求可由调用方重试；非幂等请求需要依赖业务侧 trace/idempotency 机制。

### provider 配置

默认 provider 使用 core 自研方案；当配置为 etcd 或 Consul 时，只替换注册发现和租约存储后端，服务实例身份、MachineID 自动扩展、服务分片筛选标记、负载均衡、transport selector 都继承同一套 core 规则。

建议配置：

```yaml
cluster:
  mode: auto
  provider: local
  nodeName: ""
  advertiseAddress: ""
  heartbeatInterval: 3s
  heartbeatTimeout: 10s
  suspectTimeout: 15s
  instanceReuseCooldown: 30s
  claim:
    autoMachineID: true
    autoDataCenterID: true
    machineIDMax: 31
    dataCenterIDMax: 31
    conflictPolicy: expand-machine-id
  discovery:
    seeds:
      - "10.0.0.10:18080"
      - "10.0.0.11:18080"
    multicast: false
    mdns: false
  shard:
    missingKeyPolicy: error
    emptyCandidatePolicy: error
    keyPriority:
      - userid
      - accountTypeid
      - tenantId
  services:
    funds:
      shardKeys:
        userid:
          mode: hash
          required: true
        accountTypeid:
          mode: group
          required: false
      instances:
        - machineID: 1
          shards:
            accountTypeid: ["spot"]
        - machineID: 2
          shards:
            accountTypeid: ["spot"]
        - machineID: 3
          shards:
            accountTypeid: ["contract"]
        - machineID: 4
          shards:
            accountTypeid: ["contract"]
  providers:
    etcd:
      endpoints:
        - "127.0.0.1:2379"
      prefix: "/digitalway/core"
      ttl: 10s
    consul:
      address: "127.0.0.1:8500"
      prefix: "digitalway-core"
      ttl: 10s
```

provider 职责：

- 注册当前节点。
- 周期续租/心跳。
- 发现同服务节点。
- 监听节点变更。
- 失效节点剔除。

provider 选择规则：

- `provider: local`：默认值，使用 core 自研 local/P2P 注册发现，不依赖 Redis、etcd、Consul。
- `provider: etcd`：使用 etcd 保存实例、租约、分片元数据，并通过 watch 同步变更。
- `provider: consul`：使用 Consul service catalog/session/health check 保存实例、租约、分片元数据。
- Redis 不参与该 provider 选择；如果同时配置 Redis MQ，也只影响 `mq`。

建议在 `pkg/server/config` 中按现有标准配置方式新增结构化配置，而不是继续把集群、传输、MQ 配置塞进 `CustomerDataList`。`CustomerDataList` 可作为迁移兼容读取，但新项目应使用 `ServerConfig` 的结构化字段。

建议文件拆分：

```text
pkg/server/config/
  serverconfig.go       保留 ServerConfig、ReadConfig、Save、公共默认值入口
  clusterconfig.go      ClusterConfig、provider、shard、claim 默认值和校验
  transportconfig.go    TransportConfig 默认值和校验
  mqconfig.go           MQConfig、provider、retry、deadletter、switch 默认值和校验
```

当前配置实现需要兼容：

- `ServerConfig` 嵌入 `rest.RestConf`，现有字段包括 `DataCenterID`、`MachineID`、`Auth`、`ManageAuth`、`ServerManageAuth`、`RunIp`、`ParentServerIP`、`SocketPort`、`AttachServices`、`CustomerDataList`、`MelodyConfigPath` 等。
- `ReadConfig(servicename)` 通过 `conf.MustLoad(file, con)` 直接读取 `etc/{service}.json`，然后加载 Casdoor 和 Melody 配置。
- `NewServiceDefaultConfig(servicename, port)` 只在配置文件不存在时创建默认配置。
- `Save()` 会把配置写回文件，并手动修正 `time.Duration` 字段的 JSON 表达。
- `QueryConfig` 会返回当前 `ServerConfig`，`ModifyConfig` 会绑定完整 `ServerConfig` 后保存。
- `ServerOption.Trans`、`ServerOption.Quic` 当前是运行时启动选项，不是持久化配置；未来可继续兼容，但集群和内部传输的规范配置应进入 `ServerConfig`。

因此新增配置必须遵守兼容规则：

1. 旧配置文件中没有的新字段，读取后必须自动补默认值。
2. 默认值不能只依赖 Go 零值，因为零值经常和“禁用/未配置/非法值”混在一起。
3. `ReadConfig` 读取后必须调用 `ApplyDefaults()` 和 `Validate()`。
4. `NewServiceDefaultConfig` 创建新配置时也必须调用同一套默认值函数，避免新旧配置行为不一致。
5. `Save()` 写入前应调用 `ApplyDefaults()`，保证管理端修改配置后不会把默认项写坏。
6. `ModifyConfig` 保存用户提交配置前，应对缺失字段补默认值，对非法 provider、负数超时、空 map 做校验。
7. `CustomerDataList` 只作为历史兼容读取入口，不应继续承载新配置。

建议处理流程：

```go
func NewServiceDefaultConfig(servicename string, port int) *ServerConfig {
    var con ServerConfig
    // 保留现有 rest.RestConf 初始化、Auth 初始化、RunIp、SocketPort 等逻辑。
    // ...
    con.ApplyDefaults()
    if err := con.Validate(); err != nil {
        panic(err)
    }
    return &con
}

func ReadConfig(servicename string) *ServerConfig {
    file := CONFIGDIRPATH + servicename + ".json"
    if !utils.IsExista(file) {
        return nil
    }
    con := &ServerConfig{}
    conf.MustLoad(file, con)
    con.ApplyDefaults()
    if err := con.Validate(); err != nil {
        panic(err)
    }
    con.ReloadExternalConfigs()
    return con
}

func (own *ServerConfig) Save() error {
    own.ApplyDefaults()
    if err := own.Validate(); err != nil {
        return err
    }
    // 保留现有 duration 字段 JSON 修正逻辑。
    // ...
}
```

`ReloadExternalConfigs()` 可把当前 `ReadConfig` 中 Casdoor、MelodyConfigPath 的加载逻辑集中起来，避免 `ReadConfig` 越来越长。

建议配置结构：

```go
type ServerConfig struct {
    rest.RestConf
    DataCenterID uint
    MachineID    uint
    Cluster      ClusterConfig `json:",optional"`
    Transport    TransportConfig `json:",optional"`
    MQ           MQConfig `json:",optional"`
}

type ClusterConfig struct {
    Mode                  string // off, auto, on
    Provider              string // local, etcd, consul
    NodeName              string
    AdvertiseAddress      string
    HeartbeatInterval     time.Duration
    HeartbeatTimeout      time.Duration
    SuspectTimeout        time.Duration
    InstanceReuseCooldown time.Duration
    Claim                 ClusterClaimConfig
    Discovery             ClusterDiscoveryConfig
    Shard                 ClusterShardConfig
    Services              map[string]ClusterServiceConfig
    Providers             ClusterProviderConfig
}

type ClusterClaimConfig struct {
    AutoMachineID  bool
    AutoDataCenterID bool
    MachineIDMax  uint
    DataCenterIDMax uint
    ConflictPolicy string // expand-machine-id, expand-data-center-id, fail
}

type ClusterDiscoveryConfig struct {
    Seeds     []string
    Multicast bool
    MDNS      bool
}

type ClusterShardConfig struct {
    MissingKeyPolicy     string // error, average
    EmptyCandidatePolicy string // error, average, readonly-fallback
    KeyPriority          []string
}

type ClusterServiceConfig struct {
    ShardKeys map[string]ClusterShardKeyConfig
    Instances []ClusterInstanceShardConfig
}

type ClusterShardKeyConfig struct {
    Mode     string // exact, group, hash, optional
    Required bool
}

type ClusterInstanceShardConfig struct {
    MachineID uint
    Shards    map[string][]string
}

type ClusterProviderConfig struct {
    Etcd   EtcdProviderConfig
    Consul ConsulProviderConfig
}

type TransportConfig struct {
    Internal string
    Fallback []string
    HTTP     HTTPTransportConfig
    Socket   SocketTransportConfig
    QUIC     QUICTransportConfig
    GRPC     GRPCTransportConfig
}

type HTTPTransportConfig struct {
    Enable bool
}

type SocketTransportConfig struct {
    Enable bool
}

type QUICTransportConfig struct {
    Enable   bool
    CertFile string
    KeyFile  string
}

type GRPCTransportConfig struct {
    Enable         bool
    Port           int
    MaxRecvMsgSize int
    MaxSendMsgSize int
}

type MQConfig struct {
    Mode          string // off, auto, on
    Provider      string // redis-stream, nats-jetstream, kafka, rabbitmq, rocketmq
    Usage         []string // event-stream, transport, websocket, delayed-task
    RequestReply  MQRequestReplyConfig
    Retry         MQRetryConfig
    DeadLetter    MQDeadLetterConfig
    Switch        MQSwitchConfig
    RedisStream   RedisStreamMQConfig
    NATSJetStream NATSJetStreamMQConfig
    Kafka         KafkaMQConfig
    RabbitMQ      RabbitMQConfig
    RocketMQ      RocketMQConfig
}

type MQRequestReplyConfig struct {
    Enable  bool
    Timeout time.Duration
}

type MQRetryConfig struct {
    Enable       bool
    RetryCount   int
    InitialDelay time.Duration
    MaxDelay     time.Duration
}

type MQDeadLetterConfig struct {
    Enable bool
    Topic  string
}

type MQSwitchConfig struct {
    AllowDynamicSwitch bool
    Strategy           string // drain, dual-write, maintenance
    TargetProvider     string
    DualWriteDuration  time.Duration
    RollbackOnFailure  bool // default true when switch is enabled
}
```

建议默认值：

```go
func (con *ServerConfig) ApplyDefaults() {
    if con.AttachServices == nil {
        con.AttachServices = make(map[string]*AttachAddress)
    }
    if con.CustomerDataList == nil {
        con.CustomerDataList = make([]*CustomerData, 0)
    }
    if con.WhiteList == nil {
        con.WhiteList = make([]string, 0)
    }
    con.Cluster.ApplyDefaults()
    con.Transport.ApplyDefaults()
    con.MQ.ApplyDefaults()
}

func (c *ClusterConfig) ApplyDefaults() {
    if c.Mode == "" {
        c.Mode = "auto"
    }
    if c.Provider == "" {
        c.Provider = "local"
    }
    if c.HeartbeatInterval == 0 {
        c.HeartbeatInterval = 3 * time.Second
    }
    if c.HeartbeatTimeout == 0 {
        c.HeartbeatTimeout = 10 * time.Second
    }
    if c.SuspectTimeout == 0 {
        c.SuspectTimeout = 15 * time.Second
    }
    if c.InstanceReuseCooldown == 0 {
        c.InstanceReuseCooldown = 30 * time.Second
    }
    if c.Claim.ConflictPolicy == "" {
        c.Claim.ConflictPolicy = "expand-machine-id"
    }
    if c.Claim.MachineIDMax == 0 {
        c.Claim.MachineIDMax = 31
    }
    if c.Claim.DataCenterIDMax == 0 {
        c.Claim.DataCenterIDMax = 31
    }
    if c.Shard.MissingKeyPolicy == "" {
        c.Shard.MissingKeyPolicy = "error"
    }
    if c.Shard.EmptyCandidatePolicy == "" {
        c.Shard.EmptyCandidatePolicy = "error"
    }
    if c.Services == nil {
        c.Services = make(map[string]ClusterServiceConfig)
    }
}

func (t *TransportConfig) ApplyDefaults() {
    if t.Internal == "" {
        t.Internal = "grpc"
    }
    if len(t.Fallback) == 0 {
        t.Fallback = []string{"grpc", "http", "socket"}
    }
    if t.GRPC.Port == 0 {
        t.GRPC.Port = 19090
    }
}

func (m *MQConfig) ApplyDefaults() {
    if m.Mode == "" {
        m.Mode = "auto"
    }
    if m.Provider == "" {
        m.Provider = "redis-stream"
    }
    if len(m.Usage) == 0 {
        m.Usage = []string{"event-stream"}
    }
    if m.RequestReply.Timeout == 0 {
        m.RequestReply.Timeout = 5 * time.Second
    }
    if m.Retry.RetryCount == 0 {
        m.Retry.RetryCount = 3
    }
    if m.Retry.InitialDelay == 0 {
        m.Retry.InitialDelay = 100 * time.Millisecond
    }
    if m.Retry.MaxDelay == 0 {
        m.Retry.MaxDelay = 5 * time.Second
    }
    if m.DeadLetter.Topic == "" {
        m.DeadLetter.Topic = "digitalway.core.deadletter"
    }
    if m.Switch.Strategy == "" {
        m.Switch.Strategy = "dual-write"
    }
    if m.Switch.DualWriteDuration == 0 {
        m.Switch.DualWriteDuration = 30 * time.Second
    }
}
```

说明：Go 的普通 `bool` 无法区分“未配置”和“显式 false”。如果需要在配置层准确表达 `rollbackOnFailure` 的默认值，建议实现时使用 `*bool`、自定义 `BoolOption`，或在 `MQSwitchConfig` 中增加 `RollbackOnFailureSet` 标记；文档示例中将其语义定义为“开启动态切换时默认 true”。

建议校验规则：

```go
func (c *ClusterConfig) Validate() error {
    if c.Mode != "off" && c.Mode != "auto" && c.Mode != "on" {
        return errors.New("cluster.mode must be off, auto, or on")
    }
    if c.Provider != "local" && c.Provider != "etcd" && c.Provider != "consul" {
        return errors.New("cluster.provider must be local, etcd, or consul")
    }
    if c.Mode == "on" && c.Provider == "etcd" && len(c.Providers.Etcd.Endpoints) == 0 {
        return errors.New("cluster.providers.etcd.endpoints is required")
    }
    if c.Mode == "on" && c.Provider == "consul" && c.Providers.Consul.Address == "" {
        return errors.New("cluster.providers.consul.address is required")
    }
    return nil
}

func (m *MQConfig) Validate() error {
    if m.Mode != "off" && m.Mode != "auto" && m.Mode != "on" {
        return errors.New("mq.mode must be off, auto, or on")
    }
    switch m.Provider {
    case "nats-jetstream", "redis-stream", "kafka", "rabbitmq", "rocketmq":
    default:
        return errors.New("mq.provider is invalid")
    }
    if m.Mode == "on" && len(m.Usage) == 0 {
        return errors.New("mq.usage is required when mq.mode is on")
    }
    if m.RequestReply.Enable && m.RequestReply.Timeout <= 0 {
        return errors.New("mq.requestReply.timeout must be greater than zero")
    }
    return nil
}
```

兼容旧配置的关键点：

- 旧配置不包含 `Cluster` 时，补齐为 `Mode=auto`、`Provider=local`，但不会无条件进入集群流程。
- `Mode=off` 时，服务按现有单机/AttachServices 方式启动，不执行 cluster claim、服务注册发现、分片筛选和动态 provider。
- `Mode=on` 时，必须进入 cluster claim、服务注册发现、分片筛选和动态 provider；初始化失败应启动失败并给出明确错误。
- `Mode=auto` 时，根据运行环境自动判断：检测到 Docker/Kubernetes、多副本环境变量、显式 `CORE_CLUSTER_MODE=auto/on`、或启动参数声明集群时，进入 cluster；否则保持旧单机模式。
- 旧配置中的 `DataCenterID`、`MachineID` 继续有效；开启 cluster 后才允许自动扩展并写入运行时最终值。
- 旧配置中的 `AttachServices` 继续作为手动依赖服务地址；开启 cluster 后优先 registry/discovery，`AttachServices` 可作为静态 fallback。
- `ServerOption.Trans` 和 `ServerOption.Quic` 继续兼容当前启动方式；新增持久化 `Transport` 配置后，优先级建议为：显式 `ServerOption` > `ServerConfig.Transport` > 默认值。
- 旧配置不包含 `MQ` 时，补齐为 `Mode=auto`、`Provider=redis-stream`、`Usage=["event-stream"]`；只有检测到 MQ 配置或服务间通讯需要 MQ 时，才把 MQ 加入可选 transport。

配置文件示例：

下面示例对应现有 `etc/{service}.json` 的使用方式，只展示新增配置和部分已有关键字段。旧字段如 `Name`、`Host`、`Port`、`Auth`、`ManageAuth`、`AttachServices` 继续保留。

```json
{
  "Name": "funds",
  "Host": "0.0.0.0",
  "Port": 8080,
  "DataCenterID": 1,
  "MachineID": 1,
  "SocketPort": 18080,
  "AttachServices": {},
  "Cluster": {
    "Mode": "auto",
    "Provider": "local",
    "NodeName": "",
    "AdvertiseAddress": "",
    "HeartbeatInterval": "3s",
    "HeartbeatTimeout": "10s",
    "SuspectTimeout": "15s",
    "InstanceReuseCooldown": "30s",
    "Claim": {
      "AutoMachineID": true,
      "AutoDataCenterID": true,
      "MachineIDMax": 31,
      "DataCenterIDMax": 31,
      "ConflictPolicy": "expand-machine-id"
    },
    "Discovery": {
      "Seeds": [
        "10.0.0.10:18080",
        "10.0.0.11:18080"
      ],
      "Multicast": false,
      "MDNS": false
    },
    "Shard": {
      "MissingKeyPolicy": "error",
      "EmptyCandidatePolicy": "error",
      "KeyPriority": [
        "userid",
        "accountTypeid",
        "tenantId"
      ]
    },
    "Services": {
      "funds": {
        "ShardKeys": {
          "userid": {
            "Mode": "hash",
            "Required": true
          },
          "accountTypeid": {
            "Mode": "group",
            "Required": false
          }
        },
        "Instances": [
          {
            "MachineID": 1,
            "Shards": {
              "accountTypeid": ["spot"]
            }
          },
          {
            "MachineID": 2,
            "Shards": {
              "accountTypeid": ["spot"]
            }
          },
          {
            "MachineID": 3,
            "Shards": {
              "accountTypeid": ["contract"]
            }
          }
        ]
      }
    },
    "Providers": {
      "Etcd": {
        "Endpoints": ["127.0.0.1:2379"],
        "Prefix": "/digitalway/core",
        "TTL": "10s"
      },
      "Consul": {
        "Address": "127.0.0.1:8500",
        "Prefix": "digitalway-core",
        "TTL": "10s"
      }
    }
  },
  "Transport": {
    "Internal": "grpc",
    "Fallback": ["grpc", "http", "socket", "mq"],
    "HTTP": {
      "Enable": true
    },
    "Socket": {
      "Enable": true
    },
    "QUIC": {
      "Enable": false,
      "CertFile": "",
      "KeyFile": ""
    },
    "GRPC": {
      "Enable": true,
      "Port": 19090,
      "MaxRecvMsgSize": 4194304,
      "MaxSendMsgSize": 4194304
    }
  },
  "MQ": {
    "Mode": "auto",
    "Provider": "redis-stream",
    "Usage": ["event-stream", "websocket"],
    "RequestReply": {
      "Enable": false,
      "Timeout": "5s"
    },
    "Retry": {
      "Enable": true,
      "RetryCount": 3,
      "InitialDelay": "100ms",
      "MaxDelay": "5s"
    },
    "DeadLetter": {
      "Enable": true,
      "Topic": "digitalway.core.deadletter"
    },
    "Switch": {
      "AllowDynamicSwitch": true,
      "Strategy": "dual-write",
      "TargetProvider": "",
      "DualWriteDuration": "30s",
      "RollbackOnFailure": true
    },
    "RedisStream": {
      "Addr": "127.0.0.1:6379",
      "DB": 0,
      "Prefix": "digitalway-core"
    },
    "NATSJetStream": {
      "URL": "nats://127.0.0.1:4222",
      "StreamPrefix": "digitalway-core",
      "DurablePrefix": "core"
    },
    "Kafka": {
      "Brokers": ["127.0.0.1:9092"],
      "Prefix": "digitalway-core"
    },
    "RabbitMQ": {
      "URL": "amqp://guest:guest@127.0.0.1:5672/",
      "Exchange": "digitalway.core"
    },
    "RocketMQ": {
      "NameServers": ["127.0.0.1:9876"],
      "Group": "digitalway-core"
    }
  }
}
```

使用场景示例：

- 本地开发或旧项目：不写 `Cluster`、`Transport`、`MQ`，读取后自动补默认值；MQ provider 默认为 `redis-stream`，降低开发阶段依赖成本。
- Docker 自动扩展：设置 `Cluster.Mode=auto` 或环境变量 `CORE_CLUSTER_MODE=auto`，默认 `Provider=local`，启动时自动执行实例 claim。
- 强制集群模式：设置 `Cluster.Mode=on`，provider 为 `local`、`etcd` 或 `consul`；启动失败时直接报错。
- 服务间通讯使用 MQ：在 `MQ.Usage` 中加入 `transport`，并开启 `RequestReply.Enable=true`。
- 从 Redis Streams 切到 NATS JetStream：设置 `MQ.Switch.TargetProvider=nats-jetstream`，切换流程由 MQ manager 执行双写、追平、切读和回滚。

### 动态切换

动态切换 provider 应采用“双写、预热、切流、停止旧 provider”的步骤：

1. `Start(newProvider)`。
2. 把当前节点注册到新 provider。
3. 从新 provider 拉取服务列表并建立本地 registry。
4. 一段时间内同时监听新旧 provider。
5. 切换 `CurrentProvider` 到新 provider。
6. 停止旧 provider 或保持降级备用。

建议 API：

```text
POST /api/servermanage/cluster/switchprovider
POST /api/servermanage/cluster/status
POST /api/servermanage/cluster/nodes
```

安全要求：

- provider 切换必须记录审计日志。
- 切换失败必须回滚到旧 provider。
- 同一时间只允许一个切换任务。
- 本地 registry 应保留最近一次可用快照，provider 短暂失败时不中断服务调用。

## 内部传输优化

### 统一 Transport 抽象

当前内部传输已有 HTTP、socket、QUIC 相关实现。建议统一放入 `pkg/server/transport`，新增 gRPC，并把可配置的消息队列也视为内部传输的一种实现。

这里的原则是：业务侧只调用 `req.CallService(router)` 或 `ServiceContext.CallService(payload)`，不感知底层协议；server 层根据目标节点、路由语义、配置、健康状态选择 HTTP、Socket、QUIC、gRPC 或 MQ。

```go
type Transport interface {
    Name() string
    Start(ctx context.Context, sc *router.ServiceContext) error
    Stop(ctx context.Context) error
    Supports(ctx context.Context, payload *types.PayLoad, target NodeInfo) bool
    Send(ctx context.Context, payload *types.PayLoad) ([]byte, error)
    Health(ctx context.Context, target *types.TargetInfo) error
}

type TransportSelector interface {
    Select(ctx context.Context, payload *types.PayLoad, target NodeInfo) (Transport, error)
}
```

建议目录：

```text
pkg/server/transport/
  transport.go        统一接口、能力声明、错误类型
  selector.go         选择策略、fallback、健康检查
  payload_codec.go    PayLoad 编解码
  http/
  socket/
  quic/
  grpc/
  mq/
```

传输优先级建议：

1. 本地同进程：直接调用 `RouterInfo.ExecDo` 或本地 `ServiceContext`。
2. gRPC：内部服务默认推荐。
3. HTTP REST：兼容外部调用和已有路径。
4. socket：保留已有内部 socket 能力。
5. QUIC：作为高性能/实验传输保留。
6. MQ：当配置了消息队列并且目标调用适合异步、事件、广播、削峰或 request/reply 时启用。

### Message Queue 作为平台级配置

Message Queue 不应挂在 `transport.mq` 下，而应作为 `ServerConfig.MQ` 的独立配置。原因是 MQ 不只服务于内部传输，还会服务于 WebSocket 跨节点通知、事件流、配置变更、延迟任务、削峰和失败补偿。

内部服务通讯需要使用 MQ 时，应统一从 `ServerConfig.MQ` 获取当前 MQ provider；如果 MQ 存在且健康，transport selector 才把 MQ 作为一种可选 transport。

当前项目里 `pkg/dec/eventbus` 是较旧的内存事件总线实现，且 `pkg/dec` 已在本优化计划中标记为暂不作为核心功能继续投入。后续不建议继续沿用 `event-bus` 作为新概念，应升级为 `event-stream` / `event-router`：

- `event-stream`：事件的持久化、投递、重放、消费者组、死信和延迟处理。
- `event-router`：根据事件类型、服务名、业务 key 把事件路由给本地 observe、WebSocket notice、MQ provider 或内部 transport。
- `event-envelope`：事件元数据标准，建议兼容 CloudEvents。

MQ 适合以下场景：

- 跨节点事件通知，例如 WebSocket notice、配置变更、路由摘要同步。
- 高并发削峰，例如异步处理、延迟消费、重试。
- request/reply 模式的内部调用，例如带 `TraceID`、`ReplyTo`、超时时间和 correlation id 的服务调用。
- 广播或多订阅者消费，例如集群状态变更。

但 MQ 传输和 gRPC/HTTP 的语义不同，需要明确能力边界：

- 同步强一致请求默认不走 MQ，除非配置启用 request/reply 且路由声明可通过 MQ 调用。
- 非幂等请求通过 MQ 重试前，需要业务侧提供幂等键或 trace 去重。
- MQ 传输必须支持超时、重试、死信或失败回调。
- 请求和响应仍使用 `types.PayLoad` 语义，避免业务代码感知 MQ 消息格式。

建议 MQ transport 接口：

```go
type MessageQueueTransport interface {
    Transport
    Publish(ctx context.Context, topic string, payload *types.PayLoad) error
    Subscribe(ctx context.Context, topic string, handler func(*types.PayLoad) error) error
    Request(ctx context.Context, topic string, payload *types.PayLoad, timeout time.Duration) ([]byte, error)
}
```

建议支持的 provider：

```text
pkg/server/mq/
  mq.go
  manager.go             MQ 管理器、当前 provider、动态切换
  switcher.go            双写、切读、回滚、健康检查
  provider_nats.go       NATS Core request/reply + JetStream event-stream
  provider_redis.go      Redis Streams
  provider_kafka.go      Kafka topic
  provider_rabbitmq.go   RabbitMQ exchange/queue
  provider_rocketmq.go   RocketMQ topic
```

建议新增事件抽象目录：

```text
pkg/server/event/
  envelope.go       事件信封，兼容 CloudEvents 字段
  router.go         事件路由：observe/websocket/mq/transport
  stream.go         事件流接口
  local.go          本地进程内实现，仅用于开发和无依赖模式
  codec.go          事件序列化、版本、schema
```

建议事件信封：

```go
type EventEnvelope struct {
    ID              string
    Source          string
    SpecVersion     string
    Type            string
    Subject         string
    Time            time.Time
    DataContentType string
    TraceID         string
    IdempotencyKey  string
    ShardKey        string
    Data            []byte
}
```

`EventEnvelope` 字段应尽量对应 CloudEvents 的 `id/source/specversion/type/subject/time/datacontenttype/data`，并扩展 core 自己需要的 `TraceID`、`IdempotencyKey`、`ShardKey`。

Redis 只作为 MQ transport、event-stream 或 cache 的可选实现，不作为服务注册发现 provider。开发阶段默认使用 Redis Streams，上线后推荐通过配置切换到 NATS JetStream：

- `mq.provider=redis-stream` 作为开发默认和轻量部署选项。
- `mq.provider=nats-jetstream` 作为上线/生产推荐方案，负责内部消息传输、事件、request/reply。
- `cluster.provider` 只能选择 `local`、`etcd`、`consul`。

### Event 方案替换建议

现有 `pkg/dec/eventbus` 不建议继续扩展为新核心能力。它是进程内注册 subscriber、同步/并发 notify 的模式，缺少跨节点持久化、ack、consumer group、dead letter、重放、动态 provider 切换和统一事件元数据。建议处理方式：

1. `pkg/dec` 保持暂不维护，后续迁入 `legacy/` 或只作为兼容包。
2. 新增 `pkg/server/event`，作为 server 层的事件抽象入口。
3. 使用 `ServerConfig.MQ` 决定事件底层 provider。
4. 业务侧继续使用 `SubscribeRouters()`、`ObserveArgs`、`NoticeWebSocket` 等现有 API，底层由 event router 负责桥接。
5. 事件元数据采用 CloudEvents 兼容格式，避免自定义事件结构难以和外部系统互通。

可选开源方案：

- **CloudEvents**：建议作为事件信封规范，不是 MQ；用于统一事件字段和跨系统互操作。CNCF 已将 CloudEvents 毕业，适合作为长期标准。
- **Watermill**：Go pub/sub 抽象库，可以直接参考或引入。优点是支持多种 pub/sub 后端、路由器、中间件、重试和 handler 模型；缺点是需要评估和 core 当前 `IRouter/ObserveArgs` 的适配成本。
- **NATS JetStream**：作为上线/生产推荐 provider。NATS Core 负责低延迟 pub/sub 和 request/reply，JetStream 负责持久化 event-stream、consumer、ack、重放和消费者组；比普通 event bus 更适合服务间通讯和事件流统一承载。
- **Kafka**：适合高吞吐、长期保留、审计、数据同步，但 request/reply 不是它最自然的强项。
- **RabbitMQ**：适合复杂 routing、可靠队列、死信交换和补偿任务。
- **Temporal**：不建议作为普通 event-stream 替代；它适合长事务、工作流、补偿和人工参与流程，可作为业务流程编排的独立能力。

建议优先级：

```text
CloudEvents 作为事件格式
  -> pkg/server/event 自研轻量抽象
  -> Watermill 作为可选实现参考或 adapter
  -> Redis Streams 开发默认 provider
  -> NATS JetStream 上线/生产推荐 provider
  -> Kafka/RabbitMQ/RocketMQ 按场景扩展
```

命名建议：

- 不再使用 `event-bus` 表达新核心能力。
- 事件持久化和分发称为 `event-stream`。
- 事件路由称为 `event-router`。
- 本地内存事件只称为 `local-event`，仅用于开发、测试或无依赖降级。

### MQ 配置和动态切换

server 启动时应检查是否配置了 MQ，如果 `mq.mode=on` 或 `mq.mode=auto` 且检测到 provider 配置，则初始化 MQ manager。初始化成功后，MQ manager 暴露给 event-stream、WebSocket notice 和 transport selector 使用。

建议检查顺序：

1. 读取标准 `mq` 配置。
2. 如果 `mq.mode=off`，不初始化 MQ。
3. 如果 `mq.mode=auto` 且没有 provider 连接信息，不初始化 MQ，但保留默认配置。
4. 初始化 MQ provider 并执行 `Health()`。
5. 健康检查通过后，把 MQ 注册到 `MQManager`，并在 `mq.usage` 包含 `transport` 时加入 transport registry。
6. 健康检查失败时，`mq.mode=on` 应启动失败；`mq.mode=auto` 可降级为无 MQ。

配置示例：

```yaml
mq:
  mode: auto
  provider: redis-stream
  usage:
    - event-stream
    - websocket
  requestReply:
    enable: false
    timeout: 5s
  retry:
    enable: true
    retryCount: 3
    initialDelay: 100ms
    maxDelay: 5s
  deadLetter:
    enable: true
    topic: "digitalway.core.deadletter"
  switch:
    allowDynamicSwitch: true
    strategy: dual-write
    targetProvider: ""
    dualWriteDuration: 30s
    rollbackOnFailure: true
  redisStream:
    addr: "127.0.0.1:6379"
    db: 0
    prefix: "digitalway-core"
```

如果需要让 MQ 参与 `CallService` 同步调用，可显式打开 request/reply：

```yaml
mq:
  mode: on
  provider: nats-jetstream
  usage:
    - event-stream
    - websocket
    - transport
  requestReply:
    enable: true
    timeout: 3s
```

动态切换建议：

```text
Start(newMQProvider)
  -> Health(newMQProvider)
  -> Create topics/streams/consumer groups
  -> Dual write old and new provider
  -> Wait consumer lag drained or timeout
  -> Switch current provider read side
  -> Keep old provider as rollback window
  -> Stop old provider
```

切换策略：

- `dual-write`：默认建议。新旧 MQ 同时写入一段时间，消费者追平后切读，适合从 Redis Streams 迁移到 NATS JetStream/Kafka/RabbitMQ/RocketMQ。
- `drain`：暂停新消息或短暂维护，等待旧 MQ 消费完后切换，简单但会影响可用性。
- `maintenance`：明确进入维护窗口，适合大版本变更或消息格式变化。

provider 选型建议：

- `redis-stream`：开发默认方案，部署简单，适合本地开发、小规模或希望复用 Redis 的阶段。
- `nats-jetstream`：上线/生产推荐方案。NATS Core 适合低延迟 pub/sub、request/reply、WebSocket notice、轻量服务间消息；JetStream 适合持久化、ack、consumer、重放和事件流。
- `kafka`：适合高吞吐事件流、审计日志、长时间保留、消费者组多的场景。
- `rabbitmq`：适合复杂 routing、可靠队列、死信交换、业务补偿和任务分发。
- `rocketmq`：适合延迟消息、顺序消息、事务消息诉求更强的业务。

建议演进路线：

```text
redis-stream       开发默认、小规模、兼容或复用 Redis
  -> nats-jetstream  上线/生产服务间消息和 event-stream
  -> kafka     平台事件流、审计、数据同步量增大
  -> rabbitmq  复杂 routing、死信、补偿任务增多
  -> rocketmq  延迟/顺序/事务消息成为核心诉求
```

切换要求：

- MQ 消息必须带 `TraceID`、`MessageID`、`IdempotencyKey`、`CreatedAt`。
- 消费端必须幂等，避免双写阶段重复消费。
- provider 切换必须记录审计日志。
- 切换失败必须回滚到旧 provider。
- 死信、重试、失败回调配置必须随 provider 切换一起迁移。
- 服务间通讯使用 MQ 时，应从 `MQManager.Current()` 获取 provider，不直接读取某个具体 MQ 客户端。

Transport selector 需要基于能力选择：

```text
local same process -> direct
route requires sync and grpc healthy -> grpc
route requires sync and grpc unavailable -> http/socket/quic fallback
route is event/notice/broadcast and mq healthy -> mq
route allows async/request-reply and mq healthy -> mq
otherwise -> fallback list
```

### gRPC 支持

新增目录：

```text
pkg/server/transport/grpc/
  proto/
    payload.proto
  server.go
  client.go
  codec.go
  stream.go
```

gRPC 传输建议承载同一个 `types.PayLoad` 语义：

```protobuf
message PayloadRequest {
  string trace_id = 1;
  string source_service = 2;
  string target_service = 3;
  string target_path = 4;
  string user_id = 5;
  string user_name = 6;
  string client_ip = 7;
  string http_method = 8;
  bytes instance_json = 9;
}

message PayloadResponse {
  bytes response_json = 1;
  int32 code = 2;
  string error = 3;
}

service CoreTransport {
  rpc Call(PayloadRequest) returns (PayloadResponse);
  rpc Health(HealthRequest) returns (HealthResponse);
}
```

gRPC 优点：

- 内部调用性能和连接复用优于 HTTP。
- 更容易做双向流，用于节点事件、配置更新、路由表同步。
- 适合与 cluster registry 结合，按节点地址建立 client pool。

### 动态传输选择

配置示例：

```yaml
transport:
  internal: grpc
  fallback:
    - grpc
    - http
    - socket
    - quic
    - mq
  grpc:
    enable: true
    port: 19090
    maxRecvMsgSize: 4194304
    maxSendMsgSize: 4194304
  socket:
    enable: true
  quic:
    enable: false
  mq:
    enable: false
    provider: ""
```

`CallService` 不直接选择具体协议，而是：

1. 从 registry 找目标节点。
2. 由 balancer 选节点。
3. 检查可用 transport，包括 MQ 配置和健康状态。
4. 由 transport selector 按调用语义选协议。
5. 失败后按 fallback 重试。

## 服务调用与负载均衡

建议把当前服务调用拆成四层：

```text
ServiceContext.CallService
  -> ClusterRegistry.Resolve(serviceName)
  -> ServiceShardRouter.Filter(nodes, payload)
  -> LoadBalancer.Pick(filteredNodes, payload)
  -> Transport.Send(payload)
```

### 服务分片筛选标记

水平拆分服务时，应允许对同一个 `ServiceName` 下的实例配置业务筛选标记。这个筛选发生在负载均衡之前：先根据 `serviceName` 找到全部实例，再根据筛选标记缩小候选实例，最后在候选实例中做平均分布或一致性选择。

建议概念：

```go
type ServiceRouteKey struct {
    ServiceName string
    Key         string
    Value       string
}

type ServiceShardRule struct {
    ServiceName string
    Key         string
    Required    bool
    Mode        string // exact, group, hash, optional
}
```

示例 1：`funds` 服务拆成 5 台，以 `userid` 为标记。

```yaml
cluster:
  services:
    funds:
      shardKeys:
        userid:
          mode: hash
          required: true
```

调用 `funds` 服务时必须带 `userid`。selector 根据 `hash(userid)` 固定选择 5 台中的一台。这样同一个用户的资金请求总是进入同一台 `funds` 实例，有利于本地缓存、顺序处理和减少跨实例锁。

示例 2：`funds` 服务按 `accountTypeid` 指定了 2 台实例。

```yaml
cluster:
  services:
    funds:
      shardKeys:
        accountTypeid:
          mode: group
          required: false
      instances:
        - machineID: 1
          shards:
            accountTypeid: ["spot"]
        - machineID: 2
          shards:
            accountTypeid: ["spot"]
        - machineID: 3
          shards:
            accountTypeid: ["contract"]
        - machineID: 4
          shards:
            accountTypeid: ["contract"]
```

当调用方带 `accountTypeid=spot` 获取 `funds` 服务时，只在声明支持 `spot` 的两台实例中按默认平均分布选择一台；带 `accountTypeid=contract` 时，只在 contract 组内均衡。

如果调用方没有任何筛选标记，则使用默认平均分布原则，在该服务的所有健康实例中选择。

建议选择顺序：

```text
serviceName
  -> health/status filter
  -> required shard key check
  -> exact/group/hash shard filter
  -> local-first or round-robin/least-latency/weighted
  -> transport selector
```

关键建议：

- 筛选标记应属于服务配置和调用 payload 的一部分，不应散落在业务代码里硬编码。
- 对资金、订单、账户这类强状态服务，优先使用 `hash` 或 `exact`，保证同一业务主体固定进入同一组或同一台实例。
- 对通用查询、报表、管理接口，可以允许无筛选标记并走默认平均分布。
- 如果某个路由声明 `Required=true`，调用方没带标记时应直接返回明确错误，不建议静默随机选择。
- 多个筛选标记同时存在时应有优先级，例如 `userid > accountTypeid > tenantId`，避免候选集为空或规则冲突。
- 候选集为空时可按配置决定：返回错误、降级到全服务平均分布、或进入只读 fallback；默认建议返回错误，防止请求落到错误分片。

负载均衡策略：

- `local-first`：同进程/同机优先。
- `round-robin`：默认简单均衡。
- `least-latency`：基于统计选择延迟最低节点。
- `consistent-hash`：基于 `IRouterHashKey` 或 payload key。
- `weighted`：根据节点权重。

建议默认：

```yaml
cluster:
  balance: local-first
  hash:
    enable: true
    source: router-hash-key
  shard:
    missingKeyPolicy: error
    emptyCandidatePolicy: error
    keyPriority:
      - userid
      - accountTypeid
      - tenantId
```

## WebSocket 横向扩展

WebSocket 的难点是连接只存在于某个节点本地。水平扩展后，某个业务事件可能发生在 A 节点，但订阅客户端在 B/C 节点。

现有 `IWebSocketRouterNotice` 和 `IRouterHashKey` 应作为 WebSocket 横向扩展的核心契约：

```go
type IWebSocketRouterNotice interface {
    NoticeFiltersRouter(message interface{}, api IRouter) (bool, interface{})
}

type IRouterHashKey interface {
    GetHashKey() uint64
}
```

`IRouterHashKey` 决定订阅分组 hash。每个节点只需要把 `routePath + hash` 的订阅摘要同步到 event-stream，不暴露具体 client。`IWebSocketRouterNotice` 负责目标节点本地最终过滤：即使某个节点拥有相同 `routePath + hash` 的订阅，也必须再次调用 `NoticeFiltersRouter(message, api)`，由业务路由根据订阅参数 `api` 决定是否推送以及推送什么数据。

建议：

1. 客户端订阅路由时，如果路由实现 `IRouterHashKey`，使用 `GetHashKey()`；否则使用框架默认字段 hash。
2. 每个节点维护本地订阅索引：`routePath + hash -> local clients`，其中每个 hash 下仍保存订阅时的 `api IRouter`。
3. 节点向 event-stream 发布订阅摘要：`serviceName + routePath + hash + nodeID`，不暴露具体 client。
4. `NoticeWebSocket(message)` 先在本节点按 `routePath + hash` 查找本地订阅组，并调用 `NoticeFiltersRouter(message, api)` 做过滤和数据转换。
5. 如果其他节点发布过相同 `routePath + hash` 的订阅摘要，则把 notice event 转发到这些节点。
6. 目标节点收到 notice event 后，只在本地相同 `routePath + hash` 的订阅组中再次调用 `NoticeFiltersRouter(message, api)`，最终决定是否推送给本节点 client。

这样可以先通过 `routePath + hash` 判断“目标 client 是否可能在某个节点上”，再通过 `NoticeFiltersRouter` 判断“该订阅组是否真的应该收到这条消息”。前者减少跨节点广播，后者保证业务过滤正确。

订阅摘要结构：

```go
type WebSocketSubscriptionSummary struct {
    NodeID      string
    ServiceName string
    Path        string
    Hash        uint64
    Count       int
    UpdatedAt   time.Time
    ExpiresAt   time.Time
}
```

事件结构：

```go
type WebSocketNoticeEvent struct {
    SourceNodeID string
    ServiceName  string
    Path         string
    Hash         uint64
    Message      []byte
    TraceID      string
}
```

发送规则：

- 如果 notice message 实现 `IRouterHashKey`，可直接用 `GetHashKey()` 定位 hash。
- 如果 notice message 无法确定 hash，可退化为 `routePath` 级别广播到拥有该路由订阅的节点，但目标节点仍必须执行 `NoticeFiltersRouter`。
- 对资金、订单、行情等高频推送，应强制业务实现稳定的 `IRouterHashKey`，避免退化广播。
- 订阅摘要必须有 TTL；节点下线、WebSocket 断开或订阅数量归零时，应发布删除摘要。

跨节点推送不应直接写死某一种协议，应作为内部 transport 调用，由 selector 根据配置和健康状态选择：

- 默认 local/P2P provider 的 gossip/event stream。
- Message Queue transport，例如 NATS JetStream、Redis Streams、Kafka、RabbitMQ、RocketMQ。
- gRPC stream。
- cluster provider event，例如 etcd watch、Consul event。

如果配置了 MQ 且健康，WebSocket notice、订阅摘要同步、节点事件广播应优先考虑 MQ；如果没有 MQ，则回退到 gRPC stream 或 local/P2P event stream。

## OpenAPI 优化

当前 OpenAPI 只生成 public/private 非管理端 API，这是合理边界。建议增强：

1. 支持 `IRouterResponse.GetResponse()` 作为响应 schema 示例。
2. 支持 `example`、`required`、`format` tag。
3. 输出 parse/validation/do 错误响应 schema。
4. 明确 manage API 不进入 OpenAPI，管理端由 WayPlus 消费 `ViewModel`。
5. 支持 `/api/{service}/openapi.json` 和 `/swagger/`。

## persistence 优化

建议保持业务入口 `ModelList[T]` 不变，但内部逐步拆分：

```text
ModelList[T]       业务操作队列
Repository[T]      查询和保存语义
SearchBuilder      查询条件转换
DataAction         底层数据库执行
Adapter            存储选择
```

重点优化：

- `SearchItem` symbol 定义为常量，减少字符串错误。
- 为 `like`、`left`、`right`、`between`、`in` 补测试。
- 明确 `SearchWhere` 的 500 条限制。
- `ModelList` 中默认 SQLite 全局实例和 adapter 默认实例要统一，避免重复全局 map。
- 金额字段继续使用 `decimal.Decimal`。

## manage 后端优化

`service/manage` 的价值很大，建议补强标准化：

- 明确 7 个命令生命周期：`view/search/add/edit/remove/submit/release`。
- `Submit` 和 `Release` 引入状态机接口，减少类型强转。
- 批量删除：要么实现 `Remove.Ids`，要么移除该字段。
- 提供 ViewModel helper：隐藏字段、必填字段、枚举字段、外键字段、命令改名。
- hook 返回 `stop=true` 的语义写入 examples。

## web/admin 前端优化

`WayPlus` 应继续保持“由后端 ViewModel 驱动页面”的方向。

建议：

- 修正并兼容字段拼写：`porpfield` -> `propfield`，`isdata` -> `isdate`。
- 命令名标准化：`add/edit/remove/submit/release/import/export`。
- `request.ts` 的 `s` 参数明确用途，不用则移除。
- 大量 `console.log` 按环境变量控制。
- 外键查询、子表查询和导入/导出补充文档。

## 测试规划与验收

测试规划应写在当前优化文档中，作为每个功能的验收标准；真正开发时再按这里的规划创建测试文件。这样不会在计划阶段提前制造空测试目录，也能保证每个核心改动都有明确验证方式。

建议测试目录：

```text
pkg/server/config/
  clusterconfig_test.go       Cluster/MQ/Transport 默认值、旧配置兼容、Validate
  mqconfig_test.go            MQ provider、retry、deadletter、switch 默认值和校验
pkg/server/cluster/
  registry_test.go            注册、续约、下线、租约过期
  shard_test.go               服务分片筛选标记、required key、候选集为空策略
  balancer_test.go            local-first、round-robin、consistent-hash、weighted
pkg/server/transport/
  selector_test.go            transport 选择、fallback、MQ 可用性判断
  grpc_transport_test.go      gRPC payload 编解码、超时、错误返回
pkg/server/mq/
  manager_test.go             MQManager.Current、Health、provider 注册
  switcher_test.go            双写、追平、切读、回滚
  redis_stream_test.go        Redis Streams provider，使用测试容器或可跳过集成标签
  nats_jetstream_test.go      NATS JetStream provider，使用测试容器或可跳过集成标签
pkg/server/event/
  envelope_test.go            CloudEvents 字段映射、TraceID、IdempotencyKey、ShardKey
  router_test.go              observe/websocket/mq/transport 路由
pkg/server/types/
  websocket_hash_test.go      IRouterHashKey 与 routePath+hash 分组
  websocket_notice_test.go    IWebSocketRouterNotice 本地过滤
tests/integration/
  cluster_local_test.go       多服务实例、本地 provider、MachineID 自动扩展
  service_call_test.go        CallService -> registry -> balancer -> transport
  websocket_cluster_test.go   多节点订阅摘要、notice 转发、本地过滤
  mq_switch_test.go           Redis Streams -> NATS JetStream 动态切换
  config_compat_test.go       旧 etc/{service}.json 读取后补默认值并可启动
```

单元测试原则：

- 跟随被测包放置，文件名使用 `*_test.go`。
- 不依赖外部服务，能在 `go test ./pkg/server/...` 中稳定运行。
- 覆盖默认值、校验、接口选择、hash、过滤、序列化、状态流转。
- 对旧配置兼容必须构造“不包含新字段”的 JSON，验证 `ApplyDefaults()` 后不会 panic。

集成测试原则：

- 集中放在 `tests/integration/`，使用 build tag，例如 `//go:build integration`。
- Redis Streams、NATS JetStream、etcd、Consul 等外部依赖使用测试容器或环境变量控制。
- 没有外部依赖时应自动 skip，而不是让普通 `go test ./...` 失败。
- 集成测试验证端到端链路，不重复单元测试细节。

建议命令：

```bash
# 普通单元测试
go test ./...

# server 相关单元测试
go test ./pkg/server/...

# 集成测试
go test -tags=integration ./tests/integration/...

# 指定 MQ provider 集成测试
CORE_TEST_REDIS=1 CORE_TEST_NATS=1 go test -tags=integration ./tests/integration/...
```

核心验收点：

- 配置：旧配置不含 `Cluster`、`Transport`、`MQ` 时能读取、补默认值、保存并启动。
- Cluster：Docker 多副本同配置启动时能自动扩展 `MachineID`，不会产生 Snowflake 冲突。
- 服务分片：`userid` 必填时缺失应报错；`accountTypeid` 分组时只在候选实例内均衡；无筛选标记时全量健康实例平均分布。
- Transport：`CallService` 必须经过 registry、balancer、transport selector，fallback 可验证。
- MQ：开发默认 `redis-stream` 可用；配置切换到 `nats-jetstream` 不改业务代码；双写切换失败可回滚。
- Event：事件信封兼容 CloudEvents，具备 `TraceID`、`IdempotencyKey`、`ShardKey`。
- WebSocket：`IRouterHashKey` 能生成稳定 `routePath + hash` 订阅摘要；跨节点 notice 到达目标节点后必须再次执行 `IWebSocketRouterNotice.NoticeFiltersRouter`。
- 下线：优雅下线进入 draining，不接新请求；异常失联经过 suspect/expired/offline；`MachineID` 经过冷却后才能回收。

## 分阶段实施计划

### 阶段 1：文档与接口稳定

- 完成目录 README。
- 补齐 `IRouterResponse`、WebSocket 接口、OpenAPI 文档。
- 给 `types/router.go`、`types/service.go`、`types/payload.go` 增加注释。
- 更新 `examples/demo`，增加 `IRouterResponse` 和 WebSocket hash/filter 示例。
- 补充测试目录规划和每个核心模块的验收用例。

### 阶段 2：基础质量修复

- `SearchItem` 查询符号常量化。
- 补充 persistence 查询测试。
- 统一 `pkg/json` 使用入口。
- 清理或标记暂停模块。
- 修复前后端字段拼写兼容问题。
- 新增配置兼容、persistence 查询、WebSocket hash/filter 单元测试。

### 阶段 3：Transport 抽象、gRPC 和 MQ

- 新增 `pkg/server/transport`。
- 迁移 HTTP/socket/QUIC 到统一接口。
- 新增 gRPC proto/server/client。
- 新增独立 `pkg/server/mq` 和 `ServerConfig.MQ`。
- 新增 MQ manager、provider 检测、健康检查和动态切换。
- 首批支持 Redis Streams 和 NATS JetStream：Redis Streams 作为开发默认，NATS JetStream 作为上线/生产推荐；后续扩展 Kafka/RabbitMQ/RocketMQ。
- `ServiceContext.CallService` 改为通过 transport selector。
- 增加 fallback 和健康检查。
- 增加 transport selector、gRPC、MQ manager、MQ switcher 单元测试和集成测试。

### 阶段 4：Cluster 默认自研 provider

- 新增 `pkg/server/cluster`。
- 实现 local/P2P provider。
- 实现节点注册、心跳、故障剔除。
- 实现本地 registry 和负载均衡。
- 实现服务分片筛选标记和候选实例过滤。
- 支持 seed 配置。
- 增加 cluster local provider、MachineID claim、服务分片、负载均衡测试。

### 阶段 5：外部 provider 和动态切换

- 实现 etcd provider。
- 实现 Consul provider。
- 实现 provider switcher。
- 增加 servermanage 管理 API。
- 增加切换回滚和审计日志。
- 增加 etcd/Consul provider、provider switcher 集成测试。

### 阶段 6：WebSocket 跨节点通知

- 建立订阅摘要同步。
- 实现跨节点 notice event。
- 通过 transport selector 支持 MQ / gRPC stream / local event stream。
- 用 `IRouterHashKey` 做一致性分组。
- 增加多节点 WebSocket 订阅摘要、跨节点 notice、本地过滤集成测试。

## 优先级建议

P0：

- Transport 抽象设计。
- gRPC 内部传输。
- 独立 MQ 配置、MQ manager、动态切换和事件传输。
- cluster provider 接口。
- 默认 local/P2P provider。
- 服务分片筛选标记和候选实例过滤。

P1：

- etcd/Consul provider。
- 动态 provider 切换。
- WebSocket 跨节点事件转发。

P2：

- OpenAPI 增强。
- manage helper。
- web/admin 字段兼容和日志控制。

P3：

- 暂停模块迁移到 legacy。
- QUIC 深度优化。
- 更完整的性能压测与监控面板。
