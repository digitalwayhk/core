# gRPC 默认内部传输与 Socket 删除设计

## 状态

- 日期：2026-07-17
- 状态：已批准设计，待实施计划
- 范围：仅修改 `github.com/digitalwayhk/core` 当前工作区；`futures` 只读核验，不修改消费方代码
- 变更性质：有意的破坏性变更

## 背景

框架当前同时保留 HTTP、自定义 TCP Socket 和 gRPC 三条内部调用路径，但三者并未形成同等完整的运行时能力：

1. 自定义 Socket 每次请求建立一次 TCP 连接，使用 JSON 与自定义长度帧，连接复用、健康检查、TLS、标准生态工具和可观测性均弱于 gRPC。
2. gRPC 客户端已经复用连接，但 Server 尚未接入 `WebServer` 生命周期，服务发现也没有把已有的 `NodeInfo.GRPCPort` 解析为调用目标。
3. 当前选择器把 HTTP 端口作为所有协议的健康检查目标。Socket 可能检查错误端口后退回 HTTP，因此“调用成功”不能证明实际使用了 Socket。
4. 当前 gRPC 使用明文凭证和自定义 Health RPC，不满足生产服务间通信的身份校验与标准探针要求。
5. 示例 06 强制配置 Socket，无法演示计划中的生产默认 gRPC 路径。

继续维护自定义 Socket 会让框架承担重复协议、重复生命周期和重复测试成本，而它没有提供 gRPC 或 HTTP 不具备的独特能力。因此本设计补齐 gRPC 后直接删除 Socket，不设置废弃期。

## 目标

1. gRPC 成为生产和测试中的默认同步内部传输。
2. 补齐 gRPC Server 启停、服务发现、连接池关闭、TLS、标准健康检查和协议级集成测试。
3. HTTP 保留为显式配置的同步备用协议。
4. EventBridge/MQ 继续承担异步事件，不伪装成同步 RPC 的透明备用通道。
5. 从 Core 的代码、配置、协议、测试和文档中完整删除自定义 Socket。
6. 示例 06 同进程和三进程部署都能证明请求实际经过 gRPC，而不是因回退而“假绿”。

## 非目标

- 不修改 `futures` 仓库。
- 不引入 NATS JetStream 作为本次同步调用传输。
- 不把业务 API 改造成逐路由 protobuf 服务；本次继续使用统一的 Core gRPC envelope。
- 不同时重构 QUIC、WebSocket、EventBridge 或业务路由协议。
- 不为已确认无独特价值的 Socket 增加适配器、兼容层或废弃登记期。

## 决策

### 1. 传输职责

| 通道 | 角色 | 默认行为 |
| --- | --- | --- |
| gRPC | 服务间同步调用 | 默认首选 |
| HTTP | 同步备用和诊断路径 | 仅显式配置后参与选择 |
| EventBridge/MQ | 异步事件、控制通知、可靠消息 | 不参与同步 RPC 自动回退 |
| WebSocket | 最终外部用户订阅 | 不用于内部服务通信 |
| Socket | 无 | 完整删除 |

默认配置为 `Internal: grpc`。`Fallback` 默认不再包含 Socket，也不隐式加入 HTTP；需要 HTTP 备用时必须显式写入 `Fallback: [http]`。这样配置能准确表达部署能力，避免调用方误以为所有服务都具备安全可用的备用协议。

### 2. 协议专属端点

`ServiceResolver` 返回同一服务实例的 HTTP 与 gRPC 端点。传输不得再共享一个 `host:port` 字符串：

- gRPC 使用 `NodeInfo.Address + NodeInfo.GRPCPort`。
- HTTP 使用 `NodeInfo.Address + NodeInfo.Port`。
- `GRPCPort <= 0` 表示该节点不提供 gRPC，不能拿 HTTP 端口代替。

选择器按候选协议取得各自端点，再执行 `Supports` 和健康检查。目标端口缺失、协议未配置或健康检查失败时，才可在发送前选择下一个显式备用协议。

### 3. 回退与重试语义

自动回退只发生在请求尚未交给远端处理之前：

1. 端点不存在；
2. 本地配置不支持该协议；
3. 发送前标准健康检查明确失败。

一旦 gRPC `Call` 已开始发送，无论返回超时、连接中断还是未知状态，都不得把同一请求自动改走 HTTP。框架无法证明远端是否已经执行，透明回退会让下单、支付等非幂等操作重复提交。

同理，`MaxRetries` 只允许用于明确的发送前连接建立失败，或由业务显式声明可重试的幂等调用；本次实施不得保留“所有网络错误统一重试”的模糊语义。业务级幂等键仍是写操作抵御调用方重试的最后保障。

### 4. gRPC Server 生命周期

每个 `ServiceContext` 拥有自己的 gRPC Server，不使用进程级可变单例：

1. 配置校验完成后创建 listener，端口占用、证书错误或注册失败直接阻止服务启动。
2. 注册统一 `CoreTransport` 服务和标准 `grpc_health_v1.Health` 服务。
3. 服务业务路由就绪后，把 health 状态切为 `SERVING`，随后向 `ClusterProvider` 发布 `GRPCPort`。
4. 关闭时先停止接收新调用并把 health 切为 `NOT_SERVING`，再注销发现记录，执行有超时上限的 `GracefulStop`；超时后调用 `Stop`。
5. 最后关闭客户端连接池，保证所有 `grpc.ClientConn` 被释放。

重复 `Start`、重复 `Stop` 和启动中途失败都必须幂等收口，不得泄漏 listener、goroutine 或连接。

### 5. gRPC 端口

`Transport.GRPC.Port` 改为真正可配置，不再限制为固定 `19090`。默认值按服务 HTTP 端口派生为 `ServerConfig.Port + 10000`，便于同进程多服务和本地调试；显式配置优先。

服务发现中的 `NodeInfo.GRPCPort` 成为唯一远端 gRPC 端口事实。删除 `ServerConfig.SocketPort`、`NodeInfo.SocketPort` 以及所有 `SourceSocketPort`、`TargetSocketPort` 字段后，不再通过 Socket 端口间接表达内部监听地址。

### 6. TLS 与身份校验

`Transport.GRPC` 增加 TLS 配置：

```text
TLS.Mode       = insecure | tls | mtls
TLS.CAFile     = 信任根证书
TLS.CertFile   = 当前服务证书
TLS.KeyFile    = 当前服务私钥
TLS.ServerName = 可选的服务端证书名称覆盖
```

规则如下：

- `Cluster.Mode=off` 或 `Cluster.Provider=local` 时，未配置 `TLS.Mode` 的缺省值为 `insecure`，仅用于本机开发和不跨主机的测试；也可以显式改为 `tls` 或 `mtls`。
- `Cluster.Provider=redis|etcd|consul` 时，未配置 `TLS.Mode` 的缺省值为 `mtls`，且必须提供 CA、证书和私钥，否则启动失败。
- `tls` 只验证服务端，可用于受控迁移，但不能作为生产多服务示例的最终配置。
- `mtls` 服务端校验客户端证书，客户端校验服务端证书；证书名称不匹配、过期或不受信均 fail closed。
- 不允许从 `mtls` 自动降级为 `insecure`。

示例 06 的三进程集成测试在临时目录生成测试 CA 与每个服务的证书，Docker 示例通过只读挂载注入证书。证书、私钥和测试生成物不提交到仓库。

### 7. 标准健康检查

删除 `CoreTransport.Health`、`HealthRequest` 和 `HealthResponse`，改用 `google.golang.org/grpc/health/grpc_health_v1`：

- 服务端使用官方 health server。
- 客户端选择器调用标准 `Check`。
- 启动未完成和关闭期间返回 `NOT_SERVING`。
- 健康检查使用短超时，错误日志只记录服务、协议、端点和稳定错误码，不记录 payload、token 或 claims。

这使 Docker、Kubernetes、`grpc_health_probe` 和其他标准工具无需理解 Core 私有协议。

### 8. gRPC envelope

本次保留统一 `CoreTransport.Call`，继续承载路由路径、认证上下文和 JSON 数据。同步删除 envelope 中所有 Socket 端口字段，并且不新增 `SourceGRPCPort` 或 `TargetGRPCPort`：HTTP/gRPC 端点只存在于 `ServiceResolver` 的解析结果和传输调用上下文，不进入业务 payload。

远端业务错误继续以框架公开错误契约返回，不把原始内部错误文本写入响应。传输错误使用 gRPC status code 表达；业务错误与传输错误不得混为同一个字符串字段。

### 9. 可观测性

gRPC Server 和 Client 至少提供以下稳定事件或计数，供测试与运维判断实际协议：

- 调用总数与结果：service、route、grpc code；
- 健康检查失败：service、endpoint、稳定错误码；
- 连接建立/关闭：target、结果，不记录证书内容；
- 优雅关闭超时；
- HTTP 备用协议被选中次数及原因。

协议级集成测试必须读取进程内测试计数器或受控拦截器，断言 gRPC 调用数大于零且 HTTP 备用调用数为零。不能依赖日志文本或仅断言业务响应成功。

## Socket 删除清单

实施时必须一次性清除以下表面，完成后全仓搜索不得再出现作为内部传输含义的 Socket：

1. 删除 `pkg/server/trans/socket`。
2. 删除 `pkg/server/transport/socket`。
3. 从传输工厂、选择器默认值、配置校验和测试中删除 `socket`。
4. 删除 `ServerConfig.SocketPort`、`Transport.Socket`、`NodeInfo.SocketPort`。
5. 删除 payload、观察参数、服务附加信息中的 Source/Target/Own/Receive Socket 端口字段。
6. 删除 `-socket` 命令行参数及配置生成逻辑。
7. 从 protobuf、生成代码、示例配置、Docker、README、能力矩阵和 skill 引用中删除 Socket。
8. 删除只为 Socket 存在的测试和辅助代码；与 HTTP/gRPC 共用的业务契约测试必须迁移而不是删除。

这里的 “Socket” 删除仅指 Core 自定义内部 TCP 传输。标准库 socket 概念、WebSocket 和 Unix socket 等不同语义不在删除范围内，搜索结果需按语义审查。

## 示例 06 迁移

示例 06 保持现有用户、供应商、订单三服务边界和业务行为，只替换内部同步传输：

1. 同进程模式仍可优先使用 ServiceContext 本地直达，但必须增加强制跨传输用例，防止全部调用都被本地短路掩盖。
2. 三进程模式使用 Redis 服务发现发布 HTTPPort 与 GRPCPort，内部调用默认走 gRPC mTLS。
3. HTTP 服务继续承载 public/private/manage API，并作为显式备用协议测试对象；正常三进程验收中不得触发备用。
4. EventBridge Redis Streams、Outbox/Inbox、缓存失效和用户活动 UAT 保持原职责，不因同步传输切换而重写。
5. README 说明 gRPC、HTTP、Redis 和 EventBridge 的边界，并给出证书注入及端口表。

## 测试与验收

### 单元测试

- Transport 配置默认值、端口派生和跨字段 TLS 校验。
- Resolver 为每种协议返回正确端点，缺失 `GRPCPort` 时不借用 HTTP 端口。
- Selector 只在发送前失败时选择 HTTP，发送后不透明回退。
- gRPC 连接池并发复用、关闭和重复关闭。
- Server 启动失败、健康状态切换、GracefulStop 超时与强制 Stop。
- protobuf 映射不再包含 Socket 字段，业务/传输错误边界保持安全。

### 协议级集成测试

- 使用临时 CA、服务端证书和客户端证书完成一次真实 mTLS 调用。
- 无客户端证书、错误 CA、错误 ServerName、过期证书均拒绝。
- 标准 health `SERVING`/`NOT_SERVING` 状态可观察。
- 并发调用复用连接；关闭后无 listener、goroutine 和连接泄漏。
- gRPC 调用计数大于零，HTTP 备用计数为零。

### 示例与 UAT

- 示例 06 同进程集成测试。
- 示例 06 三进程 Redis + gRPC mTLS 集成测试。
- 用户注册/地址 CRUD/下单/支付/撤销/供应商商品管理/事件通知等现有 UAT 全部通过。
- 增加停掉 gRPC 或令其健康检查失败后，幂等只读调用按显式配置走 HTTP 的用例。
- 增加非幂等写调用在 gRPC 发送状态不明时不走 HTTP 的用例。

### 全仓门禁

- 定向测试、`-race`、`go vet`、日志检查。
- `release-contract`、API diff 和配置兼容检查。
- 全仓语义搜索确认无自定义 Socket 残留。
- `futures` 只读扫描与代表性配置加载测试；若旧 `SocketPort` 未知字段导致加载失败，必须在合并报告中列为消费方阻断，不得修改 `futures` 或宣称兼容。

## 兼容性与发布治理

这是明确批准的一次性破坏性删除，不走长期废弃流程，但仍必须留下可审计迁移证据：

1. 更新 API 兼容基线和配置能力矩阵。
2. 在 `CHANGELOG.md` 的 `Unreleased / Removed` 记录 Socket 删除，在 `Changed` 记录 gRPC 默认传输和 mTLS 要求。
3. 增加破坏性变更批准记录，列出删除的 Go API、配置字段、命令行参数和 protobuf 字段。
4. 提供迁移表：`SocketPort -> Transport.GRPC.Port`、`socket -> grpc`、Socket 健康检查 -> 标准 gRPC health。
5. Core 合并前只读核对 `futures` 中 13 份历史/本地 JSON 的 `SocketPort`。未知字段若被容忍，记录清理建议；若不被容忍，Core 变更不得伪装可直接升级。

## 实施边界

本设计应拆成可独立验证的小节，但删除动作必须在同一最终发布候选中闭环：

1. 先补齐 gRPC Server、TLS、健康检查和端点解析。
2. 再把示例 06 与协议测试切到 gRPC，并用计数证明没有 HTTP 假回退。
3. 最后删除 Socket 全表面，更新兼容基线和发布文档。

在 gRPC 替代路径和协议级测试尚未通过前，不允许先删 Socket；在 Socket 全仓残留尚未清零前，也不能宣称迁移完成。

## 完成定义

- gRPC 是默认同步内部传输，HTTP 仅为显式备用。
- 非本地发现模式强制 mTLS，错误证书 fail closed。
- `WebServer` 完整管理 gRPC Server 与客户端池生命周期。
- `NodeInfo.GRPCPort` 从注册、发现、解析到调用全链路生效。
- 标准 `grpc_health_v1` 替代私有 Health RPC。
- 示例 06 三进程测试证明实际走 gRPC，正常路径 HTTP 备用调用数为零。
- 非幂等调用不存在发送后的透明跨协议重试。
- Core 中自定义 Socket 代码、配置、字段、协议、测试和文档残留为零。
- 兼容基线、CHANGELOG、破坏性批准和迁移说明完整。
- `futures` 只读核验结果被如实记录，未越界修改消费方。
