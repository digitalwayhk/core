# Logto、旧服务依赖与顶层配置清理设计

## 目标

从 Core 框架中彻底移除 Logto 认证能力和旧 ServiceAttach 服务依赖系统，减少认证实现、后台 JWKS 生命周期、公开配置面、重复服务发现路径和旧 Observe/Notify 订阅链。跨服务地址只由 `ClusterProvider + ServiceResolver` 解析；认证路由继续使用框架 Access Token，Casdoor 继续负责既有身份生命周期；异步服务事件统一使用 EventBridge。

本变更按用户批准的方案作为 MAJOR 破坏性变更实施，不保留废弃壳或运行时兼容开关。

## 删除范围

### Logto

- 删除 `AuthSecret.Logto`、`LogtoConfig` 和默认配置中的 Logto 项。
- 删除 `pkg/server/safe/logto`、REST 的 Logto middleware、`authModeLogto`、Logto 身份上下文分支和 `AuthProviderLogto`。
- REST 受保护路由统一先验证对应 User、Manage 或 ServerManage 域的框架 Access Token，再执行现有认证 Hook 和 Casdoor 撤销检查。
- 删除仅由 Logto 使用的 `github.com/MicahParks/keyfunc/v2` 与 `github.com/golang-jwt/jwt/v5` 依赖。

### 旧 ServiceAttach 服务依赖系统

- 删除 `ServerConfig.AttachServices`、`AttachAddress` 和 `SetAttachService`。
- 删除运行时 `Service.AttachService`、`ServiceAttach`、`IAttachService` 和 `Service.CallService` 对该表的地址回退。
- 删除 `IService.SubscribeRouters`、`ObserveState`、`ObserveArgs`、`NotifyArgs`、`RouterInfo.Subscribe/UnSubscribe`、自动 request/response/error Observe 通知及相关生命周期状态。
- 删除系统级 `Attach`、`Observe`、`Notify`、`SetServiceAddress` 路由，以及 `ServiceManage` 中对应的依赖、调用路由和订阅展示/编辑模型。
- 删除基于静态地址或 Observe 的初始化、同进程地址回填、配置保存、后台重试和链接流程。
- `WebServer` 不再链接旧依赖服务；同进程调用直接使用已注册的 `ServiceContext`，跨进程调用使用 `ClusterProvider + ServiceResolver`，发现失败时保持 fail closed。
- 现行业务若需要跨服务异步通知，必须显式使用 `ServiceContext.SubscribeEvent`、Outbox/Inbox 和 EventBridge，不再通过 Router 执行结果的隐式观察回调。
- 保留显式 `PayLoad.TargetAddress` 的底层兼容发送能力，本次不扩大为传输 API 清理。

### 顶层配置清理

- 删除 `ServerConfig.RunIp`。该值本来就由 `utils.GetLocalIP()` 生成，不再写入配置文件；`ServiceContext` 初始化时保存一次运行时地址，`Cluster.AdvertiseAddress` 仍可显式覆盖对外广播地址。
- 删除只写不读的 `ServerConfig.ParentServerIP`。
- 删除没有 server 运行时消费方的 `ServerConfig.Debug`。
- 删除框架内无消费方的 `ServerConfig.CustomerDataList`、`CustomerData` 和 `GetCustomerData`；结构化配置继续使用明确字段，不保留通用键值逃生口。
- 保留 go-zero `RestConf.Port`；它是 HTTP 监听端口，不是可删除的 `RunPort` 遗留字段。
- 保留仍有真实消费方的 `DataCenterID`、`MachineID`、三套 Auth、访问控制、`TrustedProxies`、`RemoteAccessManageAPI`、`MelodyConfigPath`、Cluster、Transport、MQ、RouteCache 和 AuthRevocation。
- 本次不删除 Cluster、Transport、MQ 内部被标记为 `rejected` 的兼容字段；它们属于独立配置协议收敛，不与顶层遗留清理混做。

## 配置与错误行为

- 新生成配置不再包含 `Logto`、`AttachServices`、`RunIp`、`ParentServerIP`、`Debug` 或 `CustomerDataList`。
- go-zero 当前配置解析器会忽略未知字段，因此旧 JSON 中的这些键可以继续被读取；配置迁移器会在首次加载时幂等删除它们并保留其他未知字段。
- 配置了旧静态地址但没有可用服务发现节点时，调用返回现有 `ErrTargetServiceUnavailable`，不得回退旧地址。
- 使用旧 Logto Token 访问受保护路由时，按框架 Access Token 验证失败处理，返回稳定认证失败响应，不泄露内部原因。

## 公开契约与迁移

这是明确的公共 Go API 和配置破坏：

- Logto 消费方迁移到框架 Access Token；需要外部身份生命周期时使用 Casdoor。
- 静态 `AttachServices` 消费方迁移到已支持的 `ClusterProvider + ServiceResolver`。
- `SubscribeRouters`、`ObserveArgs`、`NotifyArgs` 和 Router Observe 消费方迁移到 `ServiceContext.SubscribeEvent`；可靠业务事实使用 Outbox/Inbox。
- `RunIp` 消费方不再修改配置；需要广播固定地址时设置 `Cluster.AdvertiseAddress`。HTTP 监听地址和端口继续使用 `RestConf.Host/Port`。
- `CustomerDataList` 消费方改为服务自己的结构化配置，不再通过 Core 通用键值表传入业务配置。
- 从路由发布列表移除 `Attach`、`Observe`、`Notify`、`SetServiceAddress`，并更新公开 API 基线、`BREAKING_CHANGE_APPROVAL.md`、`DEPRECATION_REGISTER.md`、配置能力矩阵、现行使用指南和 `CHANGELOG.md`。
- 历史计划、历史审查和旧设计保留为历史证据，不回写成当前能力；现行指南不得继续宣称支持 Logto 或静态地址配置。

## 测试设计

先增加会在当前实现上失败的删除契约，再实施最小删除：

1. 反射检查 `ServerConfig` 不存在 `AttachServices`、`RunIp`、`ParentServerIP`、`Debug`、`CustomerDataList`，`AuthSecret` 不存在 `Logto`，默认配置 JSON 不输出这些键。
2. REST 测试锁定受保护路由只接受正确认证域的框架 Access Token，删除 Logto 成功路径与构造器测试。
3. 服务解析测试锁定同进程调用和 `ClusterProvider + ServiceResolver` 路径，并确认无节点时不会回退静态地址或 `Service.AttachService`。
4. 运行时地址测试锁定默认地址只计算一次、`Cluster.AdvertiseAddress` 优先以及配置 JSON 不再保存动态地址。
5. 接口与路由契约检查 `IService.SubscribeRouters`、Observe 类型和四个旧系统路由均已删除。
6. EventBridge 契约继续覆盖本地订阅、跨进程可靠订阅和关闭生命周期，证明替代路径未被破坏。
7. 仓库契约检查现行代码、配置、依赖和现行文档不存在 Logto、旧 ServiceAttach/Observe 或已删除顶层配置支持声明。
8. 运行 `gofmt`、定向测试、REST/router/config/types/event race、日志检查、公共 API 检查和 `release-contract`。

## 非目标

- 不删除 Casdoor、框架 JWT、认证 Hook 或三种认证域。
- 不重构 ClusterProvider、ServiceResolver、传输选择器或 EventBridge。
- 不改变 `ServiceContext.SubscribeEvent`、Outbox/Inbox、`PayLoad.TargetAddress` 或现行 EventBridge 事件契约。
- 不删除 `RestConf.Host/Port` 或仍有运行时消费方的顶层配置，也不清理 Cluster、Transport、MQ 内部 rejected 字段。
- 不顺带清理其他已废弃 API。
