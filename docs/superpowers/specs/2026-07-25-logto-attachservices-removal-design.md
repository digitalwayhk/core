# Logto 与 AttachServices 配置移除设计

## 目标

从 Core 框架中彻底移除 Logto 认证能力和旧静态服务地址配置，减少认证实现、后台 JWKS 生命周期、公开配置面和重复服务发现路径。跨服务地址只由 `ClusterProvider + ServiceResolver` 解析；认证路由继续使用框架 Access Token，Casdoor 继续负责既有身份生命周期。

本变更按用户批准的方案作为 MAJOR 破坏性变更实施，不保留废弃壳或运行时兼容开关。

## 删除范围

### Logto

- 删除 `AuthSecret.Logto`、`LogtoConfig` 和默认配置中的 Logto 项。
- 删除 `pkg/server/safe/logto`、REST 的 Logto middleware、`authModeLogto`、Logto 身份上下文分支和 `AuthProviderLogto`。
- REST 受保护路由统一先验证对应 User、Manage 或 ServerManage 域的框架 Access Token，再执行现有认证 Hook 和 Casdoor 撤销检查。
- 删除仅由 Logto 使用的 `github.com/MicahParks/keyfunc/v2` 与 `github.com/golang-jwt/jwt/v5` 依赖。

### AttachServices 配置

- 删除 `ServerConfig.AttachServices`、`AttachAddress` 和 `SetAttachService`。
- 删除基于静态地址配置的初始化、同进程地址回填、配置保存、Manage 编辑和 Private `SetServiceAddress` 路由。
- `WebServer` 不再从配置链接依赖服务；同进程调用直接使用已注册的 `ServiceContext`，跨进程调用使用 `ClusterProvider + ServiceResolver`，发现失败时保持 fail closed。
- 保留运行时 `Service.AttachService`、`ServiceAttach`、调用路由和订阅关系；这些对象描述服务依赖关系，不再充当配置地址表。
- 保留显式 `PayLoad.TargetAddress` 的底层兼容发送能力，本次不扩大为传输 API 清理。

## 配置与错误行为

- 新生成配置不再包含 `Logto` 或 `AttachServices`。
- go-zero 当前配置解析器会忽略未知字段，因此旧 JSON 中的这两个键可以继续被读取，但不再生效；升级说明仍要求部署时删除对应键，避免产生仍受支持的误解。
- 配置了旧静态地址但没有可用服务发现节点时，调用返回现有 `ErrTargetServiceUnavailable`，不得回退旧地址。
- 使用旧 Logto Token 访问受保护路由时，按框架 Access Token 验证失败处理，返回稳定认证失败响应，不泄露内部原因。

## 公开契约与迁移

这是明确的公共 Go API 和配置破坏：

- Logto 消费方迁移到框架 Access Token；需要外部身份生命周期时使用 Casdoor。
- 静态 `AttachServices` 消费方迁移到已支持的 `ClusterProvider + ServiceResolver`。
- 从路由发布列表移除 `SetServiceAddress`，并更新公开 API 基线、`BREAKING_CHANGE_APPROVAL.md`、`DEPRECATION_REGISTER.md`、配置能力矩阵、现行使用指南和 `CHANGELOG.md`。
- 历史计划、历史审查和旧设计保留为历史证据，不回写成当前能力；现行指南不得继续宣称支持 Logto 或静态地址配置。

## 测试设计

先增加会在当前实现上失败的删除契约，再实施最小删除：

1. 反射检查 `ServerConfig` 不存在 `AttachServices`，`AuthSecret` 不存在 `Logto`，默认配置 JSON 不输出两个键。
2. REST 测试锁定受保护路由只接受正确认证域的框架 Access Token，删除 Logto 成功路径与构造器测试。
3. 服务解析测试锁定同进程调用和 `ClusterProvider + ServiceResolver` 路径，并确认无节点时不会回退静态地址。
4. 路由契约检查 `SetServiceAddress` 不再发布。
5. 仓库契约检查现行代码、配置、依赖和现行文档不存在 Logto 或 `ServerConfig.AttachServices` 支持声明。
6. 运行 `gofmt`、定向测试、REST/router/config race、日志检查、公共 API 检查和 `release-contract`。

## 非目标

- 不删除 Casdoor、框架 JWT、认证 Hook 或三种认证域。
- 不删除运行时 `Service.AttachService`、Observe 关系或公开的通用服务依赖模型。
- 不重构 ClusterProvider、ServiceResolver、传输选择器或 EventBridge。
- 不顺带清理其他已废弃 API。
