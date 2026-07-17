# gRPC 默认内部传输与 Socket 删除实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 优先复用 go-zero zrpc 与 grpc-go 成熟能力，把 gRPC 建成 Core 默认同步内部传输，完成安全传输、独立生命周期、服务发现和示例 06 验证后，一次性删除自定义 Socket。

**Architecture:** Core 的 `ClusterProvider + ServiceResolver` 继续作为唯一节点发现权威，并为每次调用生成协议专属端点。客户端按 endpoint 缓存 `zrpc.Client`；服务端因 go-zero v1.10.2 不支持单个 zrpc listener 独立停止而保留薄 `grpc.Server` 生命周期适配，复用标准 credentials 与 `grpc_health_v1`。HTTP 只在发送前健康检查失败时作为显式备用，业务调用开始发送后不得跨协议重试。

**Tech Stack:** Go 1.26、go-zero v1.10.2 `zrpc`、grpc-go v1.80.0、protobuf、Redis ClusterProvider、`crypto/tls`、`crypto/x509`、Testify、Go race detector。

**设计规格:** `docs/superpowers/specs/2026-07-17-grpc-default-socket-removal-design.md`

## 完成记录

任务 1-10 已按顺序实现、测试并提交。代码实现 tip 为 `3d6b888`；任务 10 仅补验收证据与外审交接。关键提交如下：

| 任务 | 提交 |
| --- | --- |
| 1 默认配置 | `3020f99`、`54e02e3`、`94ae985`、`f55f963` |
| 2 zrpc Client | `d335448`、`5bd5eac`、`3e1e0cc` |
| 3 gRPC Server 生命周期 | `b542374`、`6642327` |
| 4 GRPCPort 发现 | `5b6c886`、`84f56a3` |
| 5 TLS/mTLS/mesh | `c4695ac`、`2c32fd0`、`71011ee`、`cbd1563`、`66d7963`、`34c6dad`、`eaea857`、`e450154`、`d24e293` |
| 6 协议选择与统计 | `a3caf9d`、`80f8425` |
| 7 示例 06 | `9e25697`、`496da71`、`3552b62`、`12e8ca6` |
| 8 删除 Socket | `1327726`、`d473573`、`758483d`、`016a816`、`12ca575` |
| 9 发布治理 | `25bab39`、`dc1904e`、`028eed1`、`3d6b888` |
| 10 总验收 | Core 门禁通过；futures 源码 smoke 通过，真实旧配置因既有 Jaeger 值保持正式发布阻断 |

进程级集成 suite 必须使用 `go test -p 1` 串行运行，避免两个真实多进程环境争用本机启动预算；不得通过放宽 WebSocket/UAT 断言掩盖并发资源争用。

---

## 权威实现依据

- go-zero zrpc 能力边界：`https://go-zero.dev/concepts/glossary/#zrpc`
- go-zero gRPC Client direct endpoint：`https://go-zero.dev/guides/grpc/client/`
- 锁定版本 zrpc Server 源码：`https://github.com/zeromicro/go-zero/blob/v1.10.2/zrpc/server.go`
- 锁定版本 zrpc Client 源码：`https://github.com/zeromicro/go-zero/blob/v1.10.2/zrpc/client.go`
- gRPC TLS 与双向认证：`https://grpc.io/docs/guides/auth/`

实现判断以 go.mod 锁定版本源码为准；官网展示的新版本配置字段只能作为概念参考。

---

## 执行约束

- 当前工作区包含大量与本计划无关的用户改动。每次只暂存任务步骤明确列出的文件路径，禁止 `git add .`，禁止还原其他改动。
- 每个任务遵循 RED -> GREEN -> 定向 race/vet -> 内部审查 -> 独立提交；上一任务未通过不得进入下一任务。
- Socket 只能在任务 7 已证明示例 06 真实使用 gRPC 后删除。
- 不导入或复制 `github.com/zeromicro/go-zero/**/internal`；不引入第二套发现权威。
- 所有日志使用稳定英文事件名和 `logx` 字段，不记录 payload、token、claims、证书内容或业务响应。
- Go API、配置、protobuf 和命令行变更均按破坏性变更登记；不得用兼容适配器偷偷保留 Socket。

## 文件结构

### 新建

- `pkg/server/transport/endpoints.go`：协议专属端点与选择结果。
- `pkg/server/transport/stats.go`：ServiceContext 级协议选择、调用结果和备用次数计数。
- `pkg/server/transport/grpc/security.go`：`insecure/tls/mtls/mesh` 的客户端和服务端 credentials 构造。
- `pkg/server/transport/grpc/server_lifecycle_test.go`：独立启动、健康状态和关闭契约。
- `pkg/server/transport/grpc/security_test.go`：TLS/mTLS/mesh 安全契约。
- `pkg/server/transport/selector_retry_test.go`：仅发送前重试，发送后不回退。
- `examples/integration/grpc_tls.go`：通用测试 CA 与服务证书生成器。
- `examples/06-shop-microservices/deploy/certs/README.md`：生产证书/服务网格注入说明，不存放私钥。
- `docs/codex/GRPC_TRANSPORT_MIGRATION.md`：消费方迁移表与运维指南。
- `docs/codex/GRPC_SOCKET_REMOVAL_REVIEW_PROMPT.md`：最终外部只读审查提示词。

### 重点修改

- `pkg/server/config/transportconfig.go`、`serverconfig.go`：gRPC 端口和安全配置。
- `pkg/server/transport/{transport.go,selector.go,factory.go}`：端点选择、发送前回退、zrpc 构造。
- `pkg/server/transport/grpc/{client.go,server.go,proto/payload.proto}`：zrpc Client、薄 Server、标准 health。
- `pkg/server/router/{serviceresolver.go,servicecontext.go}`：GRPCPort 全链路与生命周期。
- `pkg/server/run/server.go`：`-grpc`、服务组启动和独立停止。
- `pkg/server/cluster/node.go` 及各 Provider：发布 GRPCPort，删除 SocketPort。
- `pkg/server/types/{payload.go,observable.go,service.go}`：删除 Socket 专属字段。
- `examples/06-shop-microservices` 与对应 integration：迁移到 gRPC mTLS。
- `CHANGELOG.md`、兼容基线、能力矩阵、go-zero 复用审计和 skill 引用。

### 删除

- `pkg/server/trans/socket/`
- `pkg/server/transport/socket/`

---

### Task 1: 固定 gRPC 端口与安全配置契约

**Files:**
- Modify: `pkg/server/config/transportconfig.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/config/clusterconfig_test.go`
- Create: `pkg/server/config/transportconfig_test.go`
- Test: `pkg/server/config/serverconfig_migration_error_test.go`

- [ ] **Step 1: 写配置失败测试**

新增以下精确契约：local/off 缺省为 `insecure`；外部 Provider 缺省为 `mtls`；`mesh` 不读取证书；`mtls` 缺任一文件路径失败；gRPC 端口默认等于 HTTP 端口加 10000；fallback 缺省为空。

```go
func TestServerConfigAppliesGRPCDefaultsForLocal(t *testing.T) {
    cfg := NewServiceDefaultConfig("orders", 8080)
    assert.Equal(t, 18080, cfg.Transport.GRPC.Port)
    assert.Equal(t, "insecure", cfg.Transport.GRPC.Security.Mode)
    assert.Empty(t, cfg.Transport.Fallback)
}

func TestServerConfigExternalDiscoveryDefaultsToMTLS(t *testing.T) {
    cfg := NewServiceDefaultConfig("orders", 8080)
    cfg.Cluster.Mode, cfg.Cluster.Provider = "on", "redis"
    cfg.Cluster.Providers.Redis.Addr = "127.0.0.1:6379"
    cfg.Transport.GRPC.Security = GRPCSecurityConfig{}
    cfg.ApplyDefaults()
    assert.Equal(t, "mtls", cfg.Transport.GRPC.Security.Mode)
    assert.ErrorContains(t, cfg.Validate(), "Transport.GRPC.Security.CAFile")
}

func TestServerConfigMeshDoesNotRequireApplicationCertificates(t *testing.T) {
    cfg := NewServiceDefaultConfig("orders", 8080)
    cfg.Cluster.Mode, cfg.Cluster.Provider = "on", "redis"
    cfg.Cluster.Providers.Redis.Addr = "127.0.0.1:6379"
    cfg.Transport.GRPC.Security.Mode = "mesh"
    assert.NoError(t, cfg.Validate())
}

func TestServerConfigRejectsRemoteInsecureGRPC(t *testing.T) {
    cfg := NewServiceDefaultConfig("orders", 8080)
    cfg.Cluster.Mode, cfg.Cluster.Provider = "on", "redis"
    cfg.Cluster.AdvertiseAddress = "10.10.0.12"
    cfg.Cluster.Providers.Redis.Addr = "redis:6379"
    cfg.Transport.GRPC.Security.Mode = "insecure"
    assert.ErrorContains(t, cfg.Validate(), "insecure grpc is limited to loopback")
}
```

- [ ] **Step 2: 运行测试并确认 RED**

Run: `rtk go test ./pkg/server/config -run 'TestServerConfig.*GRPC|TestTransportConfig.*Security' -count=1`

Expected: FAIL，提示 `GRPC.Security` 不存在、固定 19090 或默认 fallback 仍含 socket。

- [ ] **Step 3: 实现唯一配置结构**

```go
type GRPCSecurityConfig struct {
    Mode       string `json:",optional"` // insecure | tls | mtls | mesh
    CAFile     string `json:",optional"`
    CertFile   string `json:",optional"`
    KeyFile    string `json:",optional"`
    ServerName string `json:",optional"`
}

type GRPCTransportConfig struct {
    Port           int                `json:",optional"`
    MaxRecvMsgSize int                `json:",optional"`
    MaxSendMsgSize int                `json:",optional"`
    Security       GRPCSecurityConfig `json:",optional"`
}
```

保留 `TransportConfig.ApplyDefaults()` 处理协议和消息大小；新增 `ApplyServerDefaults(cluster ClusterConfig, httpPort int)` 处理端口及安全模式。`ServerConfig.ApplyDefaults()` 必须先执行 Cluster defaults，再执行这两个 Transport defaults。`ValidateForServer(cluster ClusterConfig, runIP string)` 执行跨字段校验；外部 Provider 只有在 advertise/run address 为 loopback 时才允许显式 `insecure`。

`tls` 要求 CertFile/KeyFile；`mtls` 要求 CAFile/CertFile/KeyFile；`insecure` 和 `mesh` 若配置证书字段则返回稳定错误，避免字段被静默忽略。

- [ ] **Step 4: 补配置迁移测试**

旧 JSON 中 `SocketPort` 和 `Transport.Socket` 当前阶段仍可加载，但不得影响新 gRPC 默认值；真正删除由任务 8 完成。

Run: `rtk go test ./pkg/server/config -count=1`

Expected: PASS。

- [ ] **Step 5: 内部审查并提交**

Run: `rtk git diff --check -- pkg/server/config`

Run: `rtk go test -race ./pkg/server/config -count=1`

Commit:

```bash
rtk git add pkg/server/config/transportconfig.go pkg/server/config/serverconfig.go pkg/server/config/clusterconfig_test.go pkg/server/config/transportconfig_test.go pkg/server/config/serverconfig_migration_error_test.go
rtk git commit -m "feat: define grpc transport security contract"
```

---

### Task 2: 引入协议专属端点并限制回退发生在发送前

**Files:**
- Create: `pkg/server/transport/endpoints.go`
- Create: `pkg/server/transport/stats.go`
- Modify: `pkg/server/transport/transport.go`
- Modify: `pkg/server/transport/selector.go`
- Modify: `pkg/server/router/serviceresolver.go`
- Modify: `pkg/server/types/observable.go`
- Modify: `pkg/server/cluster/event.go`
- Test: `pkg/server/cluster/event_post_test.go`
- Test: `pkg/server/transport/selector_test.go`
- Create: `pkg/server/transport/selector_retry_test.go`
- Test: `pkg/server/router/serviceresolver_test.go`

- [ ] **Step 1: 写端点和禁止发送后回退测试**

```go
func TestResolverReturnsProtocolSpecificEndpoints(t *testing.T) {
    // Node: Address=orders.internal, Port=8080, GRPCPort=19090
    resolved, err := resolver.Resolve(context.Background(), "orders")
    require.NoError(t, err)
    assert.Equal(t, "orders.internal:19090", resolved.Endpoints.GRPC)
    assert.Equal(t, "http://orders.internal:8080", resolved.Endpoints.HTTP)
}

func TestSendDoesNotFallbackAfterGRPCSendStarts(t *testing.T) {
    grpcTransport := &recordingTransport{name: "grpc", sendErr: context.DeadlineExceeded}
    httpTransport := &recordingTransport{name: "http", sendResult: []byte("unexpected")}
    selector := NewDefaultSelector(grpcTransport, httpTransport)
    _, err := Send(context.Background(), selector, &types.PayLoad{}, TransportEndpoints{
        GRPC: "orders:19090", HTTP: "http://orders:8080",
    })
    require.Error(t, err)
    assert.Equal(t, 1, grpcTransport.sendCalls.Load())
    assert.Zero(t, httpTransport.sendCalls.Load())
}

func TestCrossNodeSenderErrorDoesNotFallbackToHTTP(t *testing.T) {
    var httpCalls atomic.Int32
    broker := &CrossNodeNoticeBroker{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
        httpCalls.Add(1)
        return nil, errors.New("HTTP 不应被调用")
    })}}
    broker.SetSender(func(context.Context, *NodeInfo, []byte, string) ([]byte, error) {
        return nil, context.DeadlineExceeded
    })
    err := broker.post(&NodeInfo{ID: "peer", Address: "127.0.0.1", Port: 8080, GRPCPort: 19090}, "/api/servermanage/ws/notice", map[string]string{"id": "1"})
    require.ErrorIs(t, err, context.DeadlineExceeded)
    assert.Zero(t, httpCalls.Load())
}
```

在同一测试文件定义完整 `recordingTransport`，实现 `Transport` 全部方法；`Health` 返回配置的错误，`Send` 先 `sendCalls.Add(1)` 再返回配置结果。不要使用 sleep 制造交错。CrossNode 测试沿用 `event_post_test.go` 已有 `roundTripFunc` 和包内 `broker.post`，不为测试扩大生产 API。

- [ ] **Step 2: 运行测试并确认 RED**

Run: `rtk go test ./pkg/server/transport ./pkg/server/router -run 'ProtocolSpecific|DoesNotFallback|Resolver' -count=1`

Expected: FAIL，当前 selector 只接受单一 target，Resolver 仍返回 SocketPort。

- [ ] **Step 3: 实现端点和值对象**

```go
type TransportEndpoints struct {
    GRPC string
    HTTP string
}

func (e TransportEndpoints) For(protocol string) string {
    switch protocol {
    case "grpc": return e.GRPC
    case "http": return e.HTTP
    default: return ""
    }
}

type Selection struct {
    Transport Transport
    Endpoint  string
}
```

把 `TransportSelector.Select` 改为返回 `Selection`。Selector 对每个候选协议使用自己的 endpoint 做 `Supports/Health`；endpoint 空时跳过。`Send` 只调用一次已选 Transport，不在 Send 错误后重新 Select。

新增无动态 map 的低基数 `Stats`，使用 `atomic.Uint64` 记录 grpc/http 选择次数、发送成功/失败和 HTTP fallback 次数。Stats 由 ServiceContext 创建并注入 Selector，不使用进程级全局单例。

把 `cluster.CrossNodeSender` 的 target 从单一 `host:port` 改为 `*cluster.NodeInfo`。配置 sender 时，sender 的一次发送结果就是最终结果；失败不得再调用 broker 内置 HTTP client。只有 sender 为 nil 时，broker 才走直接 HTTP 模式。`makeCrossNodeSender` 根据 NodeInfo 同时构造 gRPC/HTTP endpoints。

- [ ] **Step 4: 把 MaxRetries 限定为健康预检重试**

在 `ServiceContext.sendPayload` 中移除“发送失败后重发及 legacy HTTP”逻辑。最多根据 `MaxRetries/RetryDelay` 重试 `Select` 健康预检；取得 Selection 后只发送一次。

```go
selection, err := transport.SelectWithRetry(ctx, own.TransportSelector, payload, endpoints, attempts, delay)
if err != nil { return nil, err }
return selection.Transport.Send(ctx, payload, selection.Endpoint)
```

- [ ] **Step 5: 完整验证并提交**

Run: `rtk go test -race ./pkg/server/transport ./pkg/server/router -count=1`

Commit:

```bash
rtk git add pkg/server/transport/endpoints.go pkg/server/transport/stats.go pkg/server/transport/transport.go pkg/server/transport/selector.go pkg/server/transport/selector_test.go pkg/server/transport/selector_retry_test.go pkg/server/router/serviceresolver.go pkg/server/router/serviceresolver_test.go pkg/server/types/observable.go pkg/server/router/servicecontext.go pkg/server/cluster/event.go pkg/server/cluster/event_post_test.go
rtk git commit -m "fix: make transport fallback preflight only"
```

---

### Task 3: 用 zrpc Client 替换自建 gRPC Client pool

**Files:**
- Modify: `pkg/server/transport/grpc/client.go`
- Create: `pkg/server/transport/grpc/security.go`
- Create: `pkg/server/transport/grpc/security_test.go`
- Modify: `pkg/server/transport/grpc/grpc_transport_test.go`
- Modify: `pkg/server/transport/factory.go`
- Modify: `pkg/server/transport/factory_test.go`

- [ ] **Step 1: 写 zrpc 复用和安全失败测试**

测试同 endpoint 并发 100 次只缓存一个 zrpc Client；不同 endpoint 各一个；Stop 后池为空且 Conn 已关闭；错误 CA/ServerName 握手失败；`mesh` 和 `insecure` 不创建 TransportCredentials。

```go
func TestGRPCTransportConcurrentCallsReuseOneZRPCClient(t *testing.T) {
    transport := New(config.GRPCTransportConfig{
        Security: config.GRPCSecurityConfig{Mode: "insecure"},
    })
    var workers sync.WaitGroup
    failures := make(chan error, 100)
    workers.Add(100)
    for range 100 {
        go func() {
            defer workers.Done()
            _, err := transport.Send(context.Background(), &types.PayLoad{TraceID: "pool"}, addr)
            failures <- err
        }()
    }
    workers.Wait()
    close(failures)
    for err := range failures { require.NoError(t, err) }
    assert.Equal(t, 1, transport.PooledConns())
    require.NoError(t, transport.Stop(context.Background()))
    assert.Zero(t, transport.PooledConns())
}
```

该测试文件沿用现有 `startTestServer` 创建随机端口服务，并在测试结束时停止，不新增未定义 helper。

- [ ] **Step 2: 运行测试并确认 RED**

Run: `rtk go test ./pkg/server/transport/grpc -run 'ZRPC|Security|ConnectionPooling' -count=1`

Expected: FAIL，当前 pool 保存原生 `*grpc.ClientConn` 并使用 `insecure.NewCredentials()`。

- [ ] **Step 3: 实现 zrpc Client adapter**

`GRPCTransport.pool` 改为 `sync.Map // endpoint -> zrpc.Client`。构造 `zrpc.RpcClientConf` 时显式开启中间件，避免直接构造 struct 时未经过 conf 默认填充：

```go
rpcConf := zrpc.RpcClientConf{
    Endpoints: []string{endpoint},
    NonBlock: true,
    Timeout: timeout.Milliseconds(),
    Middlewares: zrpc.ClientMiddlewaresConf{
        Trace: true, Duration: true, Prometheus: true, Breaker: true, Timeout: true,
    },
}
client, err := zrpc.NewClient(rpcConf, clientOptions...)
```

`tls/mtls` 通过 `zrpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))`；`insecure/mesh` 不添加该 option。消息大小使用公开 `zrpc.WithDialOption(grpc.WithDefaultCallOptions(...))`。

- [ ] **Step 4: 标准 health client**

`Health` 改用 `grpc_health_v1.NewHealthClient(client.Conn()).Check(...)`，只接受 `SERVING`。删除对私有 `CoreTransport.Health` 的调用。

- [ ] **Step 5: 验证无双连接池并提交**

Run: `rtk rg -n 'grpc.NewClient|insecure.NewCredentials|map\[string\]\*grpc.ClientConn' pkg/server/transport/grpc`

Expected: 只有被明确允许的测试辅助；生产 client 不出现。

Run: `rtk go test -race ./pkg/server/transport/grpc ./pkg/server/transport -count=1`

Commit:

```bash
rtk git add pkg/server/transport/grpc/client.go pkg/server/transport/grpc/security.go pkg/server/transport/grpc/security_test.go pkg/server/transport/grpc/grpc_transport_test.go pkg/server/transport/factory.go pkg/server/transport/factory_test.go
rtk git commit -m "feat: reuse zrpc for grpc clients"
```

---

### Task 4: 补齐独立 gRPC Server 生命周期和标准 health

**Files:**
- Modify: `pkg/server/transport/grpc/server.go`
- Create: `pkg/server/transport/grpc/server_lifecycle_test.go`
- Modify: `pkg/server/transport/grpc/security.go`
- Modify: `pkg/server/transport/grpc/security_test.go`

- [ ] **Step 1: 写独立生命周期失败测试**

覆盖：端口占用构造失败；Start 后 health=SERVING；Stop 前切 NOT_SERVING；重复 Stop 无 panic；阻塞 RPC 在超时后强制 Stop；停止一个 Server 不影响同进程另一个 Server；同端口可重建。

```go
func TestServerCanStopAndRebuildIndependently(t *testing.T) {
    probe, err := net.Listen("tcp", "127.0.0.1:0")
    require.NoError(t, err)
    address := probe.Addr().String()
    require.NoError(t, probe.Close())

    cfg := config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}}
    first, err := NewServer(address, cfg, func(context.Context, *types.PayLoad) ([]byte, error) {
        return []byte("ok"), nil
    })
    require.NoError(t, err)
    go first.Start()
    select {
    case <-first.Ready():
    case <-time.After(time.Second):
        t.Fatal("gRPC Server 未进入 ready")
    }
    first.Stop()

    second, err := NewServer(address, cfg, func(context.Context, *types.PayLoad) ([]byte, error) {
        return []byte("ok"), nil
    })
    require.NoError(t, err)
    go second.Start()
    select {
    case <-second.Ready():
    case <-time.After(time.Second):
        t.Fatal("重建的 gRPC Server 未进入 ready")
    }
    second.Stop()
}
```

- [ ] **Step 2: 运行测试并确认 RED**

Run: `rtk go test ./pkg/server/transport/grpc -run 'Server.*Lifecycle|ServerCanStop|HealthState' -count=1`

Expected: FAIL，当前 Server 在 Start 内创建 listener、依赖 context goroutine，且无标准 health 状态。

- [ ] **Step 3: 实现薄 Server**

构造阶段预绑定 listener 并加载 credentials，保证配置/端口错误在 WebServer 启动前返回。Server 保存 `grpc.Server`、`health.Server`、ready/done channel 和 `sync.Once`。

```go
type Server struct {
    listener net.Listener
    grpc     *grpc.Server
    health   *health.Server
    ready    chan struct{}
    done     chan struct{}
    stopOnce sync.Once
}
```

`Start()` 注册 CoreTransport 与标准 health，先置 SERVING 并关闭 ready，再阻塞 `Serve`。`StopContext(ctx)` 先置 NOT_SERVING，异步 GracefulStop；ctx 超时调用 Stop。无上下文 `Stop()` 使用配置的关闭预算。

- [ ] **Step 4: mTLS 服务端配置**

`tls` 使用服务端证书；`mtls` 设置 `ClientAuth: tls.RequireAndVerifyClientCert` 和 `ClientCAs`；`mesh/insecure` 不添加 `grpc.Creds`。证书解析错误不得延迟到 goroutine。

- [ ] **Step 5: race、泄漏和提交**

Run: `rtk go test -race ./pkg/server/transport/grpc -count=10`

Run: `rtk go vet ./pkg/server/transport/grpc`

Commit:

```bash
rtk git add pkg/server/transport/grpc/server.go pkg/server/transport/grpc/server_lifecycle_test.go pkg/server/transport/grpc/security.go pkg/server/transport/grpc/security_test.go
rtk git commit -m "feat: add independent grpc server lifecycle"
```

---

### Task 5: 接入 WebServer、ServiceContext 和服务发现全链路

**Files:**
- Modify: `pkg/server/run/server.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/router/servicecontext_lifecycle_test.go`
- Modify: `pkg/server/router/servicecontext_discovery_test.go`
- Modify: `pkg/server/run/server_concurrency_test.go`
- Modify: `pkg/server/cluster/node.go`
- Modify: `pkg/server/cluster/provider_redis_test.go`
- Modify: `pkg/server/types/service.go`

- [ ] **Step 1: 写启动顺序与发现测试**

扩展现有 `lifecycleProvider`：增加 `lastNode atomic.Pointer[cluster.NodeInfo]`，`Register` 保存传入节点副本。增加测试用 `readyRPCServer`，包含 `ready chan struct{}`、幂等 Stop 和 `Ready() <-chan struct{}`。

```go
func TestNodeRegistersOnlyAfterGRPCIsServing(t *testing.T) {
    provider := &lifecycleProvider{}
    rpcServer := newReadyRPCServer()
    sc := router.NewServiceContext(&fakeService{name: "orders-grpc-ready"})
    sc.Config.Transport.GRPC.Port = 19090
    sc.ClusterProvider = provider
    sc.SetGRPCServer(rpcServer)

    started := make(chan struct{})
    go func() { sc.SetRunState(true); close(started) }()
    assert.Never(t, func() bool { return provider.registerCount.Load() != 0 }, 50*time.Millisecond, 5*time.Millisecond)

    rpcServer.MarkReady()
    <-started
    require.NotNil(t, provider.lastNode.Load())
    assert.Equal(t, 19090, provider.lastNode.Load().GRPCPort)
    sc.SetRunState(false)
}
```

另加 `TestStoppingOneServiceContextReleasesOnlyItsGRPCPort`：启动两个真实随机端口 gRPC Server，停止第一个 ServiceContext 后断言第二个标准 health 仍为 SERVING，并在第一个地址成功构造新 Server。

- [ ] **Step 2: 运行并确认 RED**

Run: `rtk go test ./pkg/server/run ./pkg/server/router ./pkg/server/cluster -run 'GRPC|RegistersOnly|ReleasesOnly' -count=1`

Expected: FAIL，当前 WebServer 只构造 Socket，Node 未发布 GRPCPort。

- [ ] **Step 3: 接入 `-grpc` 和服务组**

`WebServer` 增加 `GRPCPort`，命令行新增 `-grpc`；默认由每个 ServiceConfig 的 `Transport.GRPC.Port` 决定，CLI 非零值才覆盖。`newInternalServer` 构造 gRPC Server 并交给 ServiceContext。

把 `ServiceContext.GetServers()` 返回类型改为 `[]service.Service`，内容为 REST Server 与 gRPC Server；不要强迫 gRPC Server 实现含 `Send/RegisterHandlers` 的旧 `IRunServer`。

在 `pkg/server/types/service.go` 定义并由 gRPC Server 实现：

```go
type GRPCServerLifecycle interface {
    service.Service
    Ready() <-chan struct{}
    StopContext(context.Context) error
}
```

ServiceContext 只依赖该接口，测试 fake 与生产 Server 使用同一生命周期契约。

- [ ] **Step 4: 保证 Ready -> Register 与 Stop -> Deregister 顺序**

`SetRunState(true)` 等待 gRPC Ready 后才调用 provider Register；等待受启动 context/超时控制。关闭顺序固定为 NOT_SERVING -> Deregister -> StopContext -> 关闭 zrpc Client pool -> 注销 ServiceContext。

`clusterMembershipConfig()` 发布 `GRPCPort: own.Config.Transport.GRPC.Port`。Resolver 只选择具有目标协议端口的 Running node。

ServiceContext 持有自己的 `transport.Stats`。每次发送记录最终选中的协议和结果；gRPC Server 收到 Call 时记录 inbound 次数。为测试和运维提供只读 Snapshot，不暴露可变计数器。

- [ ] **Step 5: 验证生命周期并提交**

Run: `rtk go test -race ./pkg/server/run ./pkg/server/router ./pkg/server/cluster -count=1`

Commit:

```bash
rtk git add pkg/server/run/server.go pkg/server/run/server_concurrency_test.go pkg/server/router/servicecontext.go pkg/server/router/servicecontext_lifecycle_test.go pkg/server/router/servicecontext_discovery_test.go pkg/server/cluster/node.go pkg/server/cluster/provider_redis_test.go pkg/server/types/service.go
rtk git commit -m "feat: wire grpc into service lifecycle and discovery"
```

---

### Task 6: 清理 protobuf envelope 与业务/传输错误边界

**Files:**
- Modify: `pkg/server/transport/grpc/proto/payload.proto`
- Regenerate: `pkg/server/transport/grpc/proto/payload.pb.go`
- Regenerate: `pkg/server/transport/grpc/proto/payload_grpc.pb.go`
- Modify: `pkg/server/transport/grpc/client.go`
- Modify: `pkg/server/transport/grpc/server.go`
- Modify: `pkg/server/transport/grpc/grpc_transport_test.go`

- [ ] **Step 1: 写 envelope 契约测试**

测试 protobuf descriptor 不再包含 `Health` 方法和 source/target address/port/socket 字段；保留字段号必须被 reserved，防止未来误复用。handler 内部错误必须返回 `codes.Internal` 和安全公开文字，不能写入 `PayloadResponse.error` 原始文本。

- [ ] **Step 2: 修改 proto，保留历史字段号**

```proto
service CoreTransport {
  rpc Call(PayloadRequest) returns (PayloadResponse);
}

message PayloadRequest {
  reserved 2, 3, 4, 6, 7, 8;
  reserved "source_address", "source_port", "source_socket_port";
  reserved "target_address", "target_port", "target_socket_port";
  string trace_id = 1;
  string source_service = 5;
  string target_service = 9;
  // 其余既有业务字段保持原字段号，不重新编号。
}
```

删除 `HealthRequest/HealthResponse`。端点仅由 `TransportEndpoints` 传入 Send，不序列化进业务 envelope。

- [ ] **Step 3: 使用锁定工具版本重新生成**

```bash
rtk env GOBIN=/private/tmp/core-proto-tools go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
rtk env GOBIN=/private/tmp/core-proto-tools go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
rtk env PATH=/private/tmp/core-proto-tools:$PATH protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative pkg/server/transport/grpc/proto/payload.proto
```

Expected: 生成文件 source path 不变，字段号未重排。

- [ ] **Step 4: 适配映射与错误状态**

`payloadToPB/pbToPayload` 只映射业务上下文。Server handler error 使用 `status.Error(codes.Internal, "internal server error")`；业务响应仍通过序列化后的 Core Response 返回。

- [ ] **Step 5: 验证并提交**

Run: `rtk go test -race ./pkg/server/transport/grpc ./pkg/server/router -count=1`

Commit:

```bash
rtk git add pkg/server/transport/grpc/proto/payload.proto pkg/server/transport/grpc/proto/payload.pb.go pkg/server/transport/grpc/proto/payload_grpc.pb.go pkg/server/transport/grpc/client.go pkg/server/transport/grpc/server.go pkg/server/transport/grpc/grpc_transport_test.go
rtk git commit -m "refactor: narrow grpc transport envelope"
```

---

### Task 7: 迁移示例 06 并证明真实使用 gRPC

**Files:**
- Create: `examples/integration/grpc_tls.go`
- Modify: `examples/integration/helpers.go`
- Modify: `examples/06-shop-microservices/bootstrap/config.go`
- Modify: `examples/06-shop-microservices/main/{all-in-one,user,supplier,order}/main.go`
- Modify: `examples/integration/06-shop-microservices/helpers_test.go`
- Modify: `examples/integration/06-shop-microservices-three-process/three_process_test.go`
- Modify: `examples/06-shop-microservices/deploy/docker-compose.yml`
- Create: `examples/06-shop-microservices/deploy/certs/README.md`
- Modify: `examples/06-shop-microservices/README.md`

- [ ] **Step 1: 为通用进程辅助增加环境变量支持**

```go
type ProcessOptions struct {
    // 既有字段...
    Environment map[string]string
}

type Suite struct {
    // 既有字段...
    Environment map[string]string
}
```

`Restart` 基于 `os.Environ()` 合并 Suite Environment，后者同名覆盖；不得修改父测试进程全局环境。

- [ ] **Step 2: 创建纯 Go 临时 PKI helper**

`NewGRPCTestPKI(t, serviceNames...)` 使用 `crypto/x509` 生成临时 CA、服务端/客户端证书，SAN 同时包含服务名、localhost 和 127.0.0.1，文件权限私钥 0600、证书 0644，全部位于 `t.TempDir()`。

```go
type GRPCTestIdentity struct { CertFile, KeyFile, ServerName string }
type GRPCTestPKI struct { CAFile string; Services map[string]GRPCTestIdentity; Client GRPCTestIdentity }
```

- [ ] **Step 3: 拆分本地与分布式配置**

同进程 all-in-one 使用 local provider + `insecure`，保留 Redis EventBridge；三进程使用 Redis provider + `mtls`，从环境变量读取 CA/Cert/Key/ServerName。正常分布式配置 `Internal=grpc`、`Fallback=[]`。

- [ ] **Step 4: 改写三进程测试**

启动参数把 `-socket` 改为唯一 `-grpc` 端口。测试生成 PKI，为三个进程分别传入证书环境变量。新增断言：

1. 三个 gRPC 标准 health 均为 SERVING；
2. 现有远程商品、地址、下单 UAT 成功；
3. 配置中 fallback 为空；
4. Task 3/4 的受控协议测试从 ServiceContext 级 Stats 读取计数，断言 gRPC Call 大于 0、HTTP 为 0；三进程测试不得通过解析日志伪造该证据；
5. 错误 CA 的第四个客户端无法调用。

三进程业务成功 + fallback 为空 + Task 2 “发送后无 legacy HTTP” 契约共同证明远程业务调用不能假回退。

- [ ] **Step 5: Compose 与 README**

Compose 只读挂载 `/run/secrets/shop-grpc`，通过环境变量指定证书；不提交证书。README 同时说明应用 mTLS 与 `mesh` 两种生产方案，并明确纯私网明文不属于生产安全方案。

- [ ] **Step 6: 运行真实集成并提交**

Run: `rtk go test -race ./examples/06-shop-microservices/... -count=1`

Run: `rtk go test -race ./examples/integration/06-shop-microservices -count=1 -timeout=15m`

Run: `rtk go test -race ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=15m`

Commit:

```bash
rtk git add examples/integration/grpc_tls.go examples/integration/helpers.go examples/06-shop-microservices examples/integration/06-shop-microservices examples/integration/06-shop-microservices-three-process
rtk git commit -m "feat: migrate multi-service example to grpc"
```

---

### Task 8: 一次性删除自定义 Socket 全表面

**Files:**
- Delete: `pkg/server/trans/socket/`
- Delete: `pkg/server/transport/socket/`
- Modify: `pkg/server/config/{serverconfig.go,transportconfig.go,clusterconfig_test.go}`
- Modify: `pkg/server/cluster/{node.go,provider_consul.go,event.go}`
- Modify: `pkg/server/router/{request.go,servicecontext.go,serviceresolver.go}`
- Modify: `pkg/server/run/server.go`
- Modify: `pkg/server/types/{payload.go,observable.go,service.go,routerinfo.go}`
- Modify: `pkg/server/api/private/setserviceaddress.go`
- Modify: all directly affected tests under the same packages

- [ ] **Step 1: 建立残留清单测试/命令**

Run before deletion:

```bash
rtk rg -n 'SocketPort|SourceSocketPort|TargetSocketPort|OwnSocketProt|ReceiveSocketProt|transport/socket|trans/socket|"socket"' pkg/server examples/06-shop-microservices
```

保存输出到任务审查记录，不把临时文件提交。

- [ ] **Step 2: 删除代码与配置字段**

删除两个 Socket 包、`ServerConfig.SocketPort`、`Transport.Socket`、`NodeInfo.SocketPort`、payload/TargetInfo/ObserveArgs/NotifyArgs/ServiceAttach 的全部 Socket 字段、`-socket` flag、Socket server 构造与 factory builder。

`SetAttachService(name,address,port,socketport)` 改为三参数；所有内部调用同步迁移。`AttachServices` 仍按既有废弃计划保留，但不再携带 SocketPort。

- [ ] **Step 3: 删除观察通知中的端口耦合**

Observe/EventBridge 通知只携带逻辑服务名、topic、trace 和快照；不再通过 `ReceiveSocketProt` 推导内部传输。跨服务地址全部交由 ServiceResolver。

- [ ] **Step 4: 更新配置迁移**

`migrateConfig` 显式删除旧顶层 `SocketPort` 与 `Transport.Socket`，写回前保持其他用户字段不变。新增 fixture 断言迁移幂等，且未知字段不导致 exit/panic。

- [ ] **Step 5: 运行语义残留扫描**

```bash
rtk rg -n 'SocketPort|SourceSocketPort|TargetSocketPort|OwnSocketProt|ReceiveSocketProt|transport/socket|trans/socket' pkg examples docs .codex
```

Expected: 只允许 WebSocket、Unix socket 等不同语义，以及迁移/CHANGELOG 对已删除字段的历史说明；逐条人工分类。

- [ ] **Step 6: 全相关包验证并提交**

Run: `rtk go test -race ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/router ./pkg/server/run ./pkg/server/types -count=1`

Run: `rtk go vet ./pkg/server/...`

Commit:

```bash
rtk git add pkg/server/config pkg/server/cluster pkg/server/transport pkg/server/trans/socket pkg/server/router pkg/server/run pkg/server/types pkg/server/api/private/setserviceaddress.go
rtk git commit -m "refactor: remove custom socket transport"
```

---

### Task 9: 更新兼容基线、能力文档和发布治理

**Files:**
- Create: `docs/codex/GRPC_TRANSPORT_MIGRATION.md`
- Modify: `docs/codex/GO_ZERO_REUSE_AUDIT.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/DEPRECATION_REGISTER.md`
- Modify: `docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`
- Modify: `docs/codex/BREAKING_CHANGE_APPROVAL.md`
- Modify: `docs/RELEASE_POLICY.md`
- Modify: `CHANGELOG.md`
- Modify: `.codex/skills/use-digitalway-core/SKILL.md`
- Modify: `.codex/skills/use-digitalway-core/references/core-backend-api.md`
- Update: API diff baselines referenced by `scripts/test.sh release-contract`

- [ ] **Step 1: 写迁移表**

至少包含：

| 旧表面 | 新表面 | 行为变化 |
| --- | --- | --- |
| `ServerConfig.SocketPort` | `Transport.GRPC.Port` | gRPC 端口进入发现链 |
| `Transport.Internal=socket` | `grpc` | zrpc Client + 标准 health |
| `-socket` | `-grpc` | 命令行破坏性变更 |
| Socket ping | `grpc_health_v1.Check` | 标准健康探针 |
| 明文私网 Socket | `mtls` 或 `mesh` | 生产必须有可验证加密身份层 |

- [ ] **Step 2: 修正 go-zero 复用审计**

把 zrpc 从“无生产调用/暂无迁移动机”改为：客户端已复用，服务端因 v1.10.2 独立停止契约不匹配而保留薄 grpc-go 适配；注明升级 go-zero 后的重新评估条件。

- [ ] **Step 3: 完成破坏性批准和 CHANGELOG**

`Unreleased / Removed` 列出 Socket Go API、配置、flag、proto 字段；`Changed` 列出默认 gRPC、显式 HTTP fallback、mTLS/mesh 安全要求。Socket 不进入长期 Deprecated 状态，但批准记录必须有 owner、日期、迁移文档和验证提交。

- [ ] **Step 4: 更新 skill**

能力文件说明：内部同步调用默认 gRPC；Core Resolver 唯一权威；客户端复用 zrpc；HTTP 仅显式备用；EventBridge 只做异步；WebSocket 只面向最终用户；生产安全为应用 mTLS 或 mesh。

- [ ] **Step 5: 运行文档和发布契约并提交**

Run: `rtk ./scripts/check-logging.sh`

Run: `rtk ./scripts/test.sh release-contract`

Run: `rtk ./scripts/ci.sh required/contracts`

Commit:

```bash
rtk git add docs/codex docs/RELEASE_POLICY.md CHANGELOG.md .codex/skills/use-digitalway-core internal/compat
rtk git commit -m "docs: publish grpc transport migration"
```

---

### Task 10: 全量验收、消费方只读核验和外部审查交接

**Files:**
- Create: `docs/codex/GRPC_SOCKET_REMOVAL_REVIEW_PROMPT.md`
- Modify only if evidence requires: `docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`

- [ ] **Step 1: 运行 Core 全量门禁**

```bash
rtk go test ./pkg/server/... -count=1
rtk go test -race ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/router ./pkg/server/run ./pkg/server/types -count=1
rtk go test -race ./examples/06-shop-microservices/... ./examples/integration/06-shop-microservices ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=20m
rtk go vet ./pkg/server/... ./examples/06-shop-microservices/...
rtk ./scripts/check-logging.sh
rtk ./scripts/ci.sh required/quick
rtk ./scripts/ci.sh required/contracts
rtk ./scripts/ci.sh required/race
```

Expected: 全部 exit 0。任何失败先归因，不用 retry/sleep 刷绿。

- [ ] **Step 2: 验证协议与安全负向路径**

Run: `rtk go test -race ./pkg/server/transport/grpc -run 'MTLS|WrongCA|ServerName|Health|Stop|ZRPC' -count=20`

Run: `rtk go test -race ./pkg/server/transport -run 'Fallback|Retry|Send' -count=20`

Expected: 错误证书稳定失败；发送后 HTTP 调用计数恒为 0；关闭/重建无 race。

- [ ] **Step 3: `futures` 只读核验**

仅在 `/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/futures` 执行搜索、配置加载和测试，不写文件：

```bash
rtk rg -n 'SocketPort|Transport.*socket|-socket' . --glob '*.go' --glob '*.json' --glob '*.yaml' --glob '*.yml'
rtk go test ./... -run 'Config|Service' -count=1
```

如果全仓测试预算过大，记录实际执行的最小包和未覆盖范围。发现旧字段导致加载失败时，标记消费方阻断，禁止修改 futures 或宣称可直接升级。

- [ ] **Step 4: 创建外部只读审查提示词**

提示词必须包含：设计/计划路径、起止提交、精确测试结果、Socket 语义残留扫描、zrpc 复用边界、mTLS/mesh、发送后不回退、示例 06 协议证明、futures 只读结果。要求输出 P0/P1/P2、兼容性、虚假绿色检查和 `APPROVED/CHANGES_REQUIRED`。

- [ ] **Step 5: 最终提交**

```bash
rtk git add docs/codex/GRPC_SOCKET_REMOVAL_REVIEW_PROMPT.md docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md
rtk git commit -m "test: record grpc transport acceptance"
```

完成后不得自行宣布整个计划关闭；把外部审查提示词和所需反馈格式交给用户，等待其他 Agent 的最终裁定。

---

## 最终验收摘要生成规则

```text
执行 Task 1 前记录实施基线：rtk git rev-parse HEAD
执行 Task 10 后记录实施终点：rtk git rev-parse HEAD
使用以下命令生成真实提交清单，禁止手填“本批提交”：
rtk git log --reverse --format='%h %s' 实施基线..实施终点

必填证据：
- Core 定向/race/vet/logging/release-contract
- 示例 06 同进程与三进程
- gRPC Call > 0，正常路径 HTTP fallback = 0
- mTLS 正向与错误 CA/证书名负向
- grpc_health_v1 SERVING/NOT_SERVING
- Socket 语义残留逐条分类
- futures 只读核验结果与未覆盖范围
```
