# 示例 06：Redis 多服务商城设计

## 1. 文档状态

- 状态：设计已确认，待开发
- 日期：2026-07-16
- 基线能力：示例 05 的用户、供应商、商品、订单和支付业务
- 示例目录：`examples/06-shop-microservices`
- 集成测试目录：`examples/integration/06-shop-microservices`
- 外部最低依赖：Redis

## 2. 目标

示例 06 是第一个多服务示例，重点演示 Digitalway Core 中多个服务的边界、调用、发现、事件协同和部署方式，不重复示例 05 的 Casdoor 身份生命周期。

本示例必须完成：

1. 将商城拆为 User、Supplier、Order 三个独立服务，每个服务拥有独立 ServiceContext、SQLite 和业务模型。
2. 外部身份全部使用框架内建 TestToken，权限只保留用户隔离、供应商隔离和平台管理员三项必要规则。
3. 调用方直接构造目标服务 API，通过 `req.CallService` 完成类型化调用，不增加额外 client 包。
4. 使用 Redis ClusterProvider 完成服务注册、发现、心跳、Watch 和健康节点选择，新代码不依赖 `AttachServices`。
5. 使用 Redis Streams 连接各 ServiceContext 的 EventBridge，完成订单、商品、缓存和 WebSocket 协同。
6. 同时提供同进程和三进程两种部署；同进程模式只用于本地断点调试，生产部署使用独立进程。
7. 使用真实 Redis、HTTP、内部传输和 WebSocket 集成测试证明完整链路，不以直接调用 Handler 代替多服务验收。

## 3. 非目标

- 不使用 Casdoor、Logto、NATS JetStream、Kafka、etcd 或 Consul。
- 不实现复杂 RBAC、权限管理 UI、多租户或供应商审批流程。
- 不让 Redis 直接保存订单、商品或用户的权威业务模型。
- 不实现跨服务数据库事务或两阶段提交。
- 不把 WebSocket 用作服务间通信。
- 不宣称内部 socket 已具备公网零信任能力；跨进程内部端口只允许部署在受控私网。
- 不删除现有公共 `AttachServices` 字段；先完成兼容废弃登记。

## 4. 已确认的关键决策

| 项目 | 决策 |
| --- | --- |
| 服务数量 | User、Supplier、Order 三个业务服务 |
| 身份 | TestToken；普通用户、供应商、平台管理员 |
| 同进程模式 | 仅用于本地调试和快速集成，不作为生产部署建议 |
| 跨进程模式 | 三个独立进程，内部调用由 Redis 发现后交给 TransportSelector |
| 最低外部依赖 | 一个 Redis 实例 |
| 服务发现 | 新增 Redis ClusterProvider，替代新代码中的 AttachServices |
| 事件 | Redis Streams + 每个 ServiceContext 独占的 ServiceEventBridge |
| 查询一致性 | User/Supplier facade 同步查询 Order/Supplier 事实服务 |
| DTO | 提升到示例根目录，跨服务共享同一序列化契约 |
| 服务调用 | 目标 API 天然作为调用对象，直接使用 `req.CallService` |
| 支付 | 保留示例 05 的支付类型、支付流水和支付状态，归属 Order Service |
| 供应商订单 | 供应商可查询并订阅自己商品对应的订单 |

## 5. 总体架构

```text
                              Redis
                    +-----------------------+
                    | Discovery + Streams   |
                    +-----------+-----------+
                                |
          +---------------------+---------------------+
          |                     |                     |
  +-------v-------+     +-------v---------+   +-------v-------+
  | User Service  |     | Supplier Service|   | Order Service |
  | User/Address  |     | Supplier/Product|   | Order/Payment |
  +-------+-------+     +---------+--------+   +-------+-------+
          |                       ^                    |
          | GetProducts           | ProductSnapshot    |
          +-----------------------+                    |
          |                                            |
          | Create/Get/Pay/Cancel Order                |
          +--------------------------------------------+

外部买家 ------> User Service
外部供应商 ----> Supplier Service
平台管理员 ----> Supplier Service / Order Service Manage

OrderChanged --EventBridge--> User/Supplier --WebSocket--> 外部订阅者
ProductChanged --EventBridge--> User cache invalidation
```

业务调用使用 TransportSelector；异步通知和控制事件使用 EventBridge；外部实时订阅使用 WebSocket。三条链路不得混用。

## 6. 目录结构

```text
examples/06-shop-microservices/
├── README.md
├── contract/
│   ├── service.go
│   ├── event.go
│   └── error.go
├── dto/
│   ├── user/
│   ├── supplier/
│   ├── order/
│   └── event/
├── user-service/
│   ├── models/
│   ├── business/
│   ├── api/public/
│   ├── api/private/
│   └── service.go
├── supplier-service/
│   ├── models/
│   ├── business/
│   ├── api/private/
│   ├── api/manage/
│   └── service.go
├── order-service/
│   ├── models/
│   ├── business/
│   ├── api/private/
│   ├── api/manage/
│   └── service.go
├── main/
│   ├── all-in-one/
│   ├── user/
│   ├── supplier/
│   └── order/
└── deploy/
    └── docker-compose.yml
```

约束：

- `contract` 只保存服务名、事件名、版本和稳定错误码，不依赖其他示例包。
- `dto` 只保存传输结构和 JSON tag，不依赖 Model、business、数据库、ServiceContext 或 RouterInfo。
- 跨服务返回和接收使用同一个 DTO；不得在消费方复制镜像结构。
- DTO 发生变化时仍需考虑滚动部署的新旧进程共存，优先增加可选字段，不直接删除或改变既有字段语义。
- Model 只属于自己的服务，不提升到公共目录，不跨服务 import。
- API 调用 business，business 调用 models；反向依赖禁止。

## 7. 框架前置改造

### 7.1 Redis DiscoveryProvider

在现有 `cluster.DiscoveryProvider` 抽象下新增 Redis 实现，不另建一套服务发现接口。

配置增加：

```json
{
  "Cluster": {
    "Mode": "on",
    "Provider": "redis",
    "AdvertiseAddress": "order-service",
    "Providers": {
      "Redis": {
        "Addr": "redis:6379",
        "DB": 0,
        "Prefix": "core:discovery",
        "TTL": "10s"
      }
    }
  }
}
```

实现契约：

- 注册键按 Prefix、ServiceName 和 NodeID 隔离，并设置 TTL。
- Register、Heartbeat、Deregister、MachineID 冲突检查必须使用 Redis 原子操作。
- Watch 使用发现事件 Stream 加周期 reconcile，不依赖 Redis Keyspace Notification。
- 异常退出依靠 TTL 移除；正常关闭立即注销并发出节点变化事件。
- `Cluster.Mode=on` 时连接、注册或初始 Watch 失败必须阻止服务启动。
- `AdvertiseAddress` 从 rejected 改为 supported，并真正写入 NodeInfo；监听地址和广播地址不得混为一谈。
- Redis key、Stream 和 consumer group 使用稳定前缀，禁止与 EventBridge、缓存或认证数据冲突。

### 7.2 ServiceResolver

每个 ServiceContext 持有一个 Resolver：

1. 先按目标 RouterInfo.ServiceName 查询当前进程的 ServiceContext registry。
2. 同进程命中时，使用目标 ServiceContext 已注册的 RouterInfo 执行本地分发，不能直接执行调用方携带的 API 原型。
3. 同进程未命中时，从 ClusterProvider 的健康快照选择节点。
4. 默认使用现有 RoundRobinBalancer；候选只包含 `NodeStatusRunning` 且租约有效的节点。
5. 解析出的 Address、Port、SocketPort 和目标服务名写入 TargetInfo，再交给 TransportSelector。
6. 无健康节点、Redis 不可用或节点字段不完整时 fail closed，返回稳定的目标服务不可用错误。
7. Resolver 在首次依赖目标服务时建立 Watch，并在 ServiceContext 关闭时取消。

同进程分发必须保留 JSON 边界或等价快照语义，避免调用方和目标 Router 共享可变指针。`utils.IsTest()` 不得继续直接执行调用方传入的 Router；测试与生产必须解析目标服务中的注册路由。

### 7.3 AttachServices 迁移

- 新 Resolver 和示例 06 完全不读取 `AttachServices`。
- 保留字段和旧 Manage API，避免一次提交破坏公共 API。
- 旧调用链仅在明确使用旧配置时保持兼容，不得成为 Redis Resolver 的静默 fallback。
- 更新配置能力矩阵、废弃登记、兼容表和迁移说明。
- 后续移除版本必须单独审批，不在示例 06 开发中直接删除。

### 7.4 调用与重试

- 查询调用允许在连接建立失败且尚未发送业务请求时重新解析一次节点。
- 写调用默认不做不透明重试。
- User Service 为 CreateOrder 生成稳定 IdempotencyKey；Order Service 使用唯一约束收敛重复请求。
- 当前 Transport `MaxRetries` 不得让非幂等写入产生重复订单；相关行为必须有回归测试。
- 跨进程示例默认使用 socket 内部传输，不回退到面向外部用户的 HTTP 路由。
- socket 只绑定和暴露在 Docker 私网；Order Service 的内部端口不映射到宿主公网。

## 8. Redis EventBridge 与可靠控制事件

Redis 使用两个独立命名空间：

```text
core:discovery:*  服务发现、租约和 Watch
core:event:*      Redis Streams EventBridge
```

现有 MQBridge 在本地 Handler 失败后仍 ACK，不能满足控制事件可靠处理。本示例开发前必须补齐以下兼容能力：

- 保留现有无返回值观察 Handler；新增可返回 error 的控制事件 Handler，不破坏公共调用方。
- Redis consumer group 包含逻辑订阅服务名；同一服务多实例竞争消费，不同服务各自收到一份事件。
- 控制 Handler 成功后 ACK；失败保留 pending，并支持超时消息重新认领。
- 生产事务同时写业务事实和 Outbox；后台 worker 发布成功后标记完成。
- 消费方按 EventID 写 Inbox 或等价幂等记录，重复投递不得重复执行副作用。
- 观察事件在无订阅者时直接丢弃；WebSocket 客户端不在线不积压用户通知。
- User/Supplier 查询仍同步访问事实服务，不能把事件副本当成订单或商品权威。

控制事件：

| 事件 | 发布者 | 消费者 | 用途 |
| --- | --- | --- | --- |
| `ProductChanged` | Supplier | User | 清理商品查询缓存 |
| `SupplierChanged` | Supplier | User/Order | 清理供应商有效性相关缓存 |
| `OrderChanged` | Order | User/Supplier | 清理订单缓存并产生本地 WebSocket 观察通知 |
| `PaymentChanged` | Order | User/Supplier | 更新订单展示并产生本地 WebSocket 观察通知 |

事件载荷不得包含 Token、Claims、完整用户资料或不必要的地址信息。

## 9. 公共 DTO

### 9.1 User DTO

- `User`：ID、Name。
- `Address`：ID、收件人、电话、地区和详细地址。
- `AddressSnapshot`：订单保存所需的不可变地址快照。

### 9.2 Supplier DTO

- `Supplier`：ID、Name、Code、Enabled。
- `Product`：ID、SupplierID、Name、Code、Price、Enabled。
- `ProductSnapshot`：下单时的商品、供应商、价格和有效性快照。

### 9.3 Order DTO

- `Order`：订单 ID、用户 ID、商品快照、地址快照、数量、总价、支付状态、订单状态和时间。
- `SupplierOrder`：供应商视角的必要订单字段，不暴露无关用户信息。
- `PaymentType`：ID、Name、Code、Enabled。
- `PaymentRecord`：流水 ID、订单 ID、金额、状态和时间。

### 9.4 Event DTO

所有控制事件包含：

```text
EventID, Version, EventType, OccurredAt, SourceService, AggregateID
```

订单事件额外包含 UserID、SupplierID、Action 和安全订单摘要；商品事件包含 SupplierID、ProductID 和 Action。

## 10. User Service

### 10.1 模型

- `User`：ID 使用 TestToken UID，Name 允许后续签发时更新。
- `Address`：归属唯一 UserID，所有 CRUD 必须校验可信用户所有权。

### 10.2 认证 Hook

`OnAuth` 仅处理 TestToken：UID 为空时拒绝签发；Auth 域签发时幂等创建或更新 User。写入失败拒绝 Token，避免产生有 Token 但无业务用户的状态。

### 10.3 外部 API

Public：

- 查询可售商品，可按 ID、Code、Name、SupplierID 和 SupplierCode 筛选。
- 查询启用支付类型。

Private：

- 查询本人资料。
- 新增、编辑、删除、查询本人地址。
- 下单。
- 查询本人订单列表和详情。
- 删除本人未支付订单。
- 撤销本人已支付订单。
- 发起支付。
- 订阅本人订单变化 WebSocket。

User Service 是买家的唯一外部入口。商品、支付和订单 API 均为 facade，内部调用 Supplier 或 Order Service；不得在 User 数据库复制权威订单。

## 11. Supplier Service

### 11.1 模型

- `Supplier`：ID 使用 TestToken UID 或稳定映射，包含 Name、Code、Enabled。
- `Product`：归属 SupplierID，包含 Name、Code、Price 和 Enabled。

### 11.2 认证 Hook

- Manage TestToken 的普通 UID 幂等创建或更新 Supplier。
- 固定测试管理员 UID 映射为 `platform_admin`，不创建 Supplier。
- 角色只由服务端规则决定，不读取客户端提交的 Role。

### 11.3 供应商 API

- 查询和编辑自己的供应商资料。
- 新增、编辑、改价、上下架和查询自己的商品。
- 查询自己商品对应的订单。
- 订阅自己的订单变化 WebSocket。

SupplierID 只能从已验证身份映射取得，不接受 Body、Query 或 WebSocket payload 指定。

### 11.4 平台管理员 API

- 查询全部供应商和商品。
- 启用或禁用供应商。
- 查看基础数据，不绕过 Product/Supplier business 规则。

### 11.5 内部 API

- 查询可售商品。
- 按 ProductID 获取 ProductSnapshot。
- 商品不存在、商品禁用或供应商禁用时 fail closed。

## 12. Order Service

### 12.1 模型

- `Order`：保存 UserID、SupplierID、ProductID、商品名称、供应商名称、单价、数量、总价和地址快照。
- `PaymentType`：平台级支付方式，可启用和禁用。
- `PaymentRecord`：记录支付和撤销状态变化。
- `Outbox`：保存待发布控制事件。
- `Inbox`：保存已消费 EventID 或等价幂等事实。

历史订单使用下单快照；商品改名、改价、禁用或供应商资料变化不得修改历史订单。

### 12.2 内部 API

- 创建订单。
- 按 UserID 查询订单列表和详情。
- 删除未支付订单。
- 撤销已支付订单。
- 发起支付。
- 查询启用支付类型。
- 按 SupplierID 查询供应商订单。

内部 API 的 UserID/SupplierID 由入口服务从 TestToken 提取后传递；Order Service 仍需校验调用来源和参数完整性。内部 API 不对宿主公网暴露。

### 12.3 Manage API

- 支付类型 CRUD、启用和禁用。
- 全局订单和支付流水查询。
- 确认支付及既有受控状态命令。

## 13. 关键业务流程

### 13.1 下单

1. User Service 从 TestToken 读取 UID。
2. User Service 查询本人 Address 并冻结 AddressSnapshot。
3. User Service 生成 IdempotencyKey，调用 Order Service CreateOrder API。
4. Order Service 调用 Supplier Service 获取 ProductSnapshot。
5. Order Service 校验数量，以商品快照价格计算总价。
6. Order 与 Outbox 在同一 SQLite 事务提交。
7. Outbox worker 通过 EventBridge 发布 `OrderChanged(created)`。
8. User/Supplier 消费事件并清理缓存，再发布各自 ServiceContext 内的 WebSocket 观察通知。

重复 IdempotencyKey 必须返回同一订单，不得重复扣写或生成第二条订单。

### 13.2 商品变更

1. Supplier Service 校验当前供应商所有权。
2. 商品修改和 Outbox 在同一事务提交。
3. `ProductChanged` 经 Redis Streams 投递 User Service。
4. User Service 清理商品 facade 缓存；TTL 仅作兜底。

### 13.3 订单查询

- 买家查询：User -> Order，Order 强制 UserID 过滤。
- 供应商查询：Supplier -> Order，Order 强制 SupplierID 过滤。
- 不在 User 或 Supplier 数据库维护权威订单读模型。
- Redis 事件丢失或延迟不能改变同步查询结果。

### 13.4 支付和撤销

- 支付类型必须启用。
- 发起支付创建 PaymentRecord，并使订单进入支付处理中状态。
- 管理员确认支付后订单进入已支付状态。
- 未支付订单可物理删除；已支付订单只能撤销。
- 状态变化和 Outbox 必须在同一事务提交。

## 14. 缓存与 WebSocket

- User Public 商品和支付类型 facade 可使用 RouterInfo Cache。
- User 本人订单和 Supplier 本人订单缓存键必须包含服务端注入身份的稳定摘要。
- ProductChanged、OrderChanged 和 PaymentChanged 成功消费后主动失效；TTL 只是兜底。
- WebSocket 只面向最终外部用户。
- User WebSocket 仅推送当前 UID 的订单。
- Supplier WebSocket 仅推送当前 SupplierID 的订单。
- 订阅参数不得覆盖 UID 或 SupplierID。
- 无客户端订阅时本地观察通知直接丢弃，不建立离线消息队列。

## 15. 部署方式

### 15.1 同进程模式

`main/all-in-one` 在一个 WebServer 中装载三个 ServiceContext：

- 仅用于本地断点调试、快速阅读和集成测试。
- Resolver 优先命中同进程 ServiceContext，但仍通过目标注册 Router 执行 JSON 边界。
- Redis 仍用于服务注册和跨 ServiceContext EventBridge，保证事件路径与三进程一致。
- README 必须明确说明同进程多服务不提供进程隔离、独立扩缩容或故障隔离。

### 15.2 三进程模式

- User、Supplier、Order 使用独立 main、端口、SocketPort、DataCenterID、MachineID 和 SQLite。
- 三个进程注册同一 Redis DiscoveryProvider。
- Docker Compose 只映射 User/Supplier 必要外部 HTTP 端口；Order 内部端口留在私网。
- Redis 不可用时服务启动 fail closed。
- 节点运行期退出后由 Deregister 或 TTL 从发现快照移除。

## 16. 配置原则

- 示例不提交运行后生成的真实配置和数据库文件。
- README 提供 Redis、Cluster、MQ、端口和 MachineID 的完整配置片段。
- 集成测试通过临时目录和 `NewServiceContextWithConfig` 管理独立配置，不污染仓库 `etc`。
- Redis Discovery 与 Redis Streams 可使用同一地址，但必须使用不同 Prefix。
- 所有服务显式启用 `MQ.Usage=["event-stream"]`。
- 示例不配置 NATS、etcd、Consul 或 AttachServices。

## 17. 日志与可观测性

结构化日志至少包含：

- `service_discovery_register_failed`
- `service_discovery_watch_failed`
- `service_resolve_failed`
- `service_call_failed`
- `event_outbox_publish_failed`
- `event_control_handler_failed`
- `event_pending_reclaimed`

字段使用 service、target_service、route、node_id、event_type、event_id、trace_id、duration 和 error。禁止记录 Token、Claims、完整 Payload、地址详情、订单完整对象或数据库 SQL。

指标至少覆盖：

- 当前已发现节点数。
- Resolve 成功、失败和无节点次数。
- 各目标节点调用次数和失败次数。
- Outbox 待发布数量和最老延迟。
- Redis pending 数量、重领次数和 Handler 失败次数。
- EventBridge observer dropped 与 control queue timeout。

## 18. 测试规划

### 18.1 框架单元测试

- Redis Provider 原子注册、心跳、List、Watch、Deregister 和 Close。
- 同服务 MachineID 冲突 fail closed。
- TTL 到期后 reconcile 移除异常节点。
- Watch 断线重连并恢复完整快照。
- Resolver 同进程优先、RoundRobin、无节点和关闭清理。
- 同进程调用执行目标注册 Router，不执行调用方原型。
- 新调用链不读取 AttachServices。
- 非幂等写调用不透明重试保护。
- Redis 控制事件成功 ACK、失败 pending、重新认领和服务级 consumer group 隔离。

### 18.2 服务单元测试

- TestToken OnAuth 幂等创建 User/Supplier。
- 地址跨用户 CRUD 拒绝。
- 商品跨供应商修改、改价和上下架拒绝。
- 禁用供应商的商品不可下单。
- 商品价格和地址快照固定。
- CreateOrder 幂等键重复收敛。
- 未支付删除、已支付撤销和支付状态机。
- 供应商订单只能返回自己的 SupplierID。
- Outbox 与业务事务原子；Inbox 重复事件无副作用。

### 18.3 同进程集成测试

- 启动 Redis 和一个 all-in-one 进程。
- 获取用户、供应商和管理员 TestToken。
- 覆盖 User Public/Private、Supplier Manage 和 Order Manage 全部 API。
- 验证 User -> Order -> Supplier 的真实调用链。
- 验证 User/Supplier WebSocket 身份隔离和订单通知。
- 验证商品变更主动失效 User facade 缓存。

### 18.4 三进程集成测试

- Docker Compose 启动 Redis，测试进程分别启动三个服务。
- 不设置 AttachServices，等待 Redis 注册表出现三个健康节点。
- 通过 User Service 完成查询商品、地址、下单、支付和订单查询。
- 通过 Supplier Service 完成商品管理、订单查询和 WebSocket。
- 启动第二个 Order 实例，验证 RoundRobin 和幂等写入。
- 正常关闭一个实例，验证立即摘除。
- 强制终止一个实例，验证 TTL 后摘除。
- 中断 Redis，验证新 Resolve 和控制事件 fail closed，恢复后 Watch 自动收敛。

### 18.5 安全与兼容测试

- Body、Query 和 WebSocket payload 伪造 UserID/SupplierID 无效。
- 用户不能直接访问 Supplier/Order Manage。
- 供应商不能修改其他供应商商品或查询其订单。
- Order 内部端口不映射宿主公网。
- 配置矩阵锁定 Redis Provider 与 AdvertiseAddress 的 supported 状态。
- apidiff、HTTP/JSON、配置和 release-contract 验证 AttachServices 兼容废弃而非直接破坏。

## 19. 实施与验收顺序

本示例不再建立单独的逐步骤开发计划，直接按本规格实施，但必须按以下边界逐段验收：

1. Redis DiscoveryProvider、配置契约和真实 Redis 测试。
2. ServiceResolver、本地分发、RoundRobin、失败语义和 AttachServices 废弃兼容。
3. Redis 控制事件 ACK/pending/reclaim、Outbox/Inbox 支撑。
4. 公共 contract/DTO 与三个服务模型。
5. Supplier 内部商品快照和外部供应商能力。
6. Order 内部订单/支付能力和 Manage 能力。
7. User facade、地址、订单和支付入口。
8. Product/Order/Payment 事件、缓存失效和双端 WebSocket。
9. 同进程集成测试、三进程 Docker 集成测试和 README。
10. race、vet、日志、配置、API 兼容和发布契约总验收。

每一段完成后必须运行定向测试并记录提交；前一段存在 P0/P1 时不得进入下一段。

## 20. 完成定义

- 新服务调用不需要 AttachServices，也不手写目标地址。
- 一个 Redis 同时支持服务发现和 EventBridge，且命名空间隔离。
- 同进程和三进程使用相同 API、DTO、Resolver 与事件协议。
- User、Supplier、Order 各自拥有独立数据库和 ServiceContext。
- 买家只能经 User Service 操作本人地址和订单。
- 供应商只能经 Supplier Service 管理自己的商品并查看自己的订单。
- Order Service 保存商品、价格、供应商和地址快照，幂等下单不重复。
- Redis 控制事件失败不被错误 ACK，pending 可恢复，消费副作用幂等。
- WebSocket 只面向外部用户且不会跨用户或跨供应商投递。
- Redis/目标节点不可用时 fail closed，不静默回退旧 AttachServices。
- README 清楚说明同进程模式仅用于本地调试、三进程模式才是部署示范，以及内部端口只允许私网访问。
- 框架定向测试、三个服务测试、两种集成测试、race、vet、日志检查、配置契约、API 兼容和 release-contract 全部通过。
