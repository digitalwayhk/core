# Casdoor 认证生命周期实施计划

> **供执行代理使用：** 必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans`，逐项实施本计划。使用复选框（`- [ ]`）跟踪步骤状态。

**目标：** 在不重复实现 JWT、Redis 客户端和事件基础设施的前提下，为 Auth/Manage 两个 Casdoor 域建立独立客户端、持续身份有效性、可靠撤销、请求前业务 Hook、Webhook 和 WebSocket 撤销闭环。

**架构：** `ServiceContext` 独占 Casdoor Clients、`AuthRevocationManager` 和三类 Hook；单节点以 Badger 为权威，共享模式以 go-zero Redis + Lua 为权威，Badger只保存已确认快照。Callback/Refresh 在线验证 Casdoor 后签发带身份域和世代的内部 Token；REST/WebSocket 在签名校验后检查语义、撤销世代并调用请求 Hook；Webhook 持久化并推进世代后，通过服务专属 EventBridge 发布可靠控制事件。

**技术栈：** Go 1.24、go-zero REST/JWT/Redis、Casdoor Go SDK v1.31、Badger v3、ServiceEventBridge、Melody WebSocket、testify、Docker Compose Redis。

**规格基线:** `docs/superpowers/specs/2026-07-16-casdoor-auth-lifecycle-design.md`，设计提交 `8e6dce0`、修订 `407550b`、SDK 字段纠偏 `a996715`。

---

## 0. 实施约束

1. 每个任务独立执行红灯测试、最小实现、绿灯测试、提交和外部只读审查；外审未给出 `APPROVED` 不进入下一任务。
2. 不使用 `casdoorsdk.InitConfig`、包级 `GetOAuthToken` 或包级 `ParseJwtToken`；只使用 `casdoorsdk.NewClient` 创建的实例方法。
3. 不自行实现 JWT 签名、Redis 连接池、Pub/Sub 或通用重试器；复用 go-zero 和现有 `ServiceEventBridge`。
4. Auth 与 Manage 的 Client、Webhook Secret、Token Secret、撤销键必须隔离；ServerManage 不接入 Casdoor Refresh/Webhook。
5. 普通错误、panic、超时和依赖错误始终脱敏；只有 `types.NewPublicError` 明确声明的消息允许返回前端。
6. 共享模式下 Redis 是唯一授权事实。Redis 不可用时不得使用 Badger 快照继续授权。
7. 不修改用户工作区中与本计划无关的脏文件；每次提交只暂存当前任务列出的文件。

## 1. 文件职责图

### 新建文件

| 文件 | 单一职责 |
| --- | --- |
| `pkg/server/config/authrevocation.go` | 撤销模式、Badger 和 Redis 配置默认值与校验 |
| `pkg/server/safe/casdoor/client.go` | Auth/Manage 独立 Casdoor Client、OAuth/Claims/User 在线验证 |
| `pkg/server/safe/casdoor/client_test.go` | 双 Client 隔离和用户状态契约 |
| `pkg/server/authstate/types.go` | 身份键、状态、标准事件、存储接口和稳定错误 |
| `pkg/server/authstate/badger.go` | 单节点权威状态、共享模式快照、事件和待处理 Hook 持久化 |
| `pkg/server/authstate/redis.go` | go-zero Redis 适配和原子事件 Lua 脚本 |
| `pkg/server/authstate/manager.go` | 授权、事件应用、EventBridge、恢复和关闭生命周期 |
| `pkg/server/authstate/*_test.go` | Badger 重启、Redis 并发、乱序、故障和 Hook 重试契约 |
| `pkg/server/trans/rest/authrequest.go` | JWT 后的语义校验、撤销检查和 `OnAuthRequest` 调用 |
| `pkg/server/trans/rest/authrequest_test.go` | Auth/Manage 隔离、公开错误、panic/超时和 Redis 故障 |
| `pkg/server/api/public/casdoorcallback.go` | `/api/casdoor/callback` 在线换发与签发 |
| `pkg/server/api/public/casdoorwebhook.go` | `/api/casdoor/webhook` 边界校验、标准化和提交 |
| `pkg/server/api/public/casdoorwebhook_test.go` | Secret、Body、域、幂等和响应时机 |
| `pkg/server/types/auth_websocket.go` | 可选关闭接口和 WebSocket 认证身份索引 |
| `examples/integration/casdoor-auth-lifecycle/*` | 假 Casdoor + 真实服务 + Redis 的端到端测试 |

### 修改文件

| 文件 | 修改内容 |
| --- | --- |
| `pkg/server/config/casdoorconfig.go` | 新增 Webhook Secret、移除全局 SDK 初始化副作用 |
| `pkg/server/config/serverconfig.go` | 接入 `AuthRevocation`，修正默认 Casdoor 域赋值，严格校验 |
| `pkg/server/types/auth.go` | 新增身份、标准 Casdoor 事件、请求 Hook、事件 Hook 和公开错误边界注释 |
| `pkg/server/safe/tokenissuer.go` | Access/Refresh 同步写入 Provider、Subject、Generation |
| `pkg/server/router/servicecontext.go` | 独占认证组件并按 EventBridge 装配顺序初始化/关闭 |
| `pkg/server/api/public/casdoor.go` | 返回新 Callback 相对路径，不泄露 Secret |
| `pkg/server/api/public/callback.go` | 删除旧实现，保留废弃 Go 类型别名 |
| `pkg/server/api/public/auth_helpers.go` | Callback/Refresh/TestToken 共用安全签发和公开错误映射 |
| `pkg/server/api/public/refresh.go` | 在线 Casdoor 验证、世代校验、再执行 `OnAuth` |
| `pkg/server/trans/rest/server.go` | 将请求 Hook 中间件放在 go-zero JWT 之后、Router 之前 |
| `pkg/server/types/route_websocket_hub.go` | 保存不可变身份索引并按世代关闭会话 |
| `pkg/server/trans/websocket/melody/sessionsubscriptions.go` | 登录保存完整身份，订阅前检查和调用 Hook |
| `pkg/server/trans/websocket/melody/client.go` | 实现可选 `IWebSocketCloser` |
| `pkg/server/api/release/routes.go` | 注册新的 Casdoor Callback/Webhook 路由 |
| `docker-compose.integration.yml` | 复用现有 Redis 服务，不新增默认依赖 |
| `scripts/test.sh` | 扩展 `security` 并增加显式 `integration-casdoor-auth` 模式 |
| `docs/codex/DEPRECATION_REGISTER.md` | 登记旧 URL、旧 Go 类型和全局 SDK 入口 |
| `docs/codex/API_COMPATIBILITY_SURFACE.md` | 更新路由、配置、Claims 和 Hook 表面 |
| `CHANGELOG.md` | 记录安全性破坏变更和四密钥轮换要求 |
| `.codex/skills/use-digitalway-core/SKILL.md` | 在实现稳定后更新 Casdoor 使用入口 |
| `.codex/skills/use-digitalway-core/references/core-backend-api.md` | 写入最终认证与集成测试用法 |

---

### 任务 1： 配置契约与双 Casdoor Client

**文件：**
- 新建： `pkg/server/config/authrevocation.go`
- 新建： `pkg/server/config/authrevocation_test.go`
- 新建： `pkg/server/safe/casdoor/client.go`
- 新建： `pkg/server/safe/casdoor/client_test.go`
- 修改： `pkg/server/config/casdoorconfig.go`
- 修改： `pkg/server/config/serverconfig.go`
- 修改： `pkg/server/config/serverconfig_auth_test.go`

- [ ] **步骤 1：写配置红灯测试**

```go
func TestAuthRevocationSharedRequiresRedis(t *testing.T) {
    cfg := NewServiceDefaultConfig("auth-test", 18080)
    cfg.Auth.CasDoor.Enable = true
    cfg.AuthRevocation.Mode = "shared"
    cfg.AuthRevocation.Redis.Addr = ""
    require.ErrorContains(t, cfg.Validate(), "authRevocation.redis.addr")
}

func TestCasdoorSecretsMustBeIndependent(t *testing.T) {
    cfg := validCasdoorServerConfig(t)
    cfg.Auth.CasDoor.WebhookSecret = cfg.Auth.CasDoor.data.Server.ClientSecret
    require.ErrorContains(t, cfg.Validate(), "WebhookSecret")
}

func TestDefaultConfigInitializesEachCasdoorDomain(t *testing.T) {
    cfg := NewServiceDefaultConfig("auth-test", 18080)
    require.False(t, cfg.Auth.CasDoor.Enable)
    require.False(t, cfg.ManageAuth.CasDoor.Enable)
    require.False(t, cfg.ServerManageAuth.CasDoor.Enable)
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/config -run 'TestAuthRevocation|TestCasdoorSecrets|TestDefaultConfigInitializesEachCasdoorDomain' -count=1`

预期： FAIL，原因是 `AuthRevocation`、`WebhookSecret` 尚不存在，且默认配置仍重复写 `con.Auth.CasDoor`。

- [ ] **步骤 3：实现配置结构和校验**

```go
type AuthRevocationConfig struct {
    Mode       string
    BadgerPath string
    Redis      AuthRevocationRedisConfig
}

type AuthRevocationRedisConfig struct {
    Addr     string
    Password string
    Prefix   string
}

func (c *AuthRevocationConfig) ApplyDefaults(service string) {
    if c.Mode == "" { c.Mode = "local" }
    if c.BadgerPath == "" { c.BadgerPath = filepath.Join("data", service, "auth-revocation") }
    if c.Redis.Prefix == "" { c.Redis.Prefix = "core:authrevocation" }
}

func (c AuthRevocationConfig) Validate(casdoorEnabled bool) error {
    if c.Mode != "local" && c.Mode != "shared" { return errors.New("authRevocation.mode must be local or shared") }
    if casdoorEnabled && strings.TrimSpace(c.BadgerPath) == "" { return errors.New("authRevocation.badgerPath is required") }
    if casdoorEnabled && c.Mode == "shared" && strings.TrimSpace(c.Redis.Addr) == "" {
        return errors.New("authRevocation.redis.addr is required in shared mode")
    }
    return nil
}
```

`CasDoorConfig.ReloadConfig` 改为只重新读取 YAML 并缓存，不调用全局 SDK；`ServerConfig.Validate` 同时检查两个 Webhook Secret 互不相同，且不等于 Client/Access/Refresh Secret。生产 Endpoint 仅接受 HTTPS，测试显式允许 loopback HTTP。

- [ ] **步骤 4：写双 Client 红灯测试**

```go
func TestClientSetKeepsAuthAndManageIsolated(t *testing.T) {
    authServer := newFakeCasdoorServer(t, "auth-org", "auth-app")
    manageServer := newFakeCasdoorServer(t, "manage-org", "manage-app")
    clients, err := NewClientSet(authServer.Config(), manageServer.Config())
    require.NoError(t, err)
    require.NotSame(t, clients.Auth(), clients.Manage())
    require.Equal(t, "auth-org", clients.Auth().OrganizationName)
    require.Equal(t, "manage-org", clients.Manage().OrganizationName)
}

func TestVerifyUserRejectsForbiddenDeletedAndMismatchedSubject(t *testing.T) {
    verifier := newFakeVerifier(&casdoorsdk.User{Owner: "org", Name: "alice", IsForbidden: true})
    require.ErrorIs(t, verifier.Verify(context.Background(), "alice"), ErrIdentityInactive)
}
```

- [ ] **步骤 5：实现实例 Client 封装**

```go
type Client interface {
    GetOAuthToken(code, state string, opts ...casdoorsdk.OAuthOption) (*oauth2.Token, error)
    ParseJwtToken(token string) (*casdoorsdk.Claims, error)
    GetUser(name string) (*casdoorsdk.User, error)
}

type ClientSet struct {
    auth   Client
    manage Client
}

func newClient(data *config.CasDoorConfigData) Client {
    s := data.Server
    return casdoorsdk.NewClient(s.Endpoint, s.ClientID, s.ClientSecret, data.Certificate, s.Organization, s.Application)
}

func VerifyActiveUser(user *casdoorsdk.User, organization, subject string) error {
    if user == nil || user.IsForbidden || user.IsDeleted || user.Owner != organization || user.Name != subject {
        return ErrIdentityInactive
    }
    return nil
}
```

- [ ] **步骤 6：运行绿灯与静态禁令**

运行： `go test ./pkg/server/config ./pkg/server/safe/casdoor -count=1`

运行： `! rg 'casdoorsdk\.(InitConfig|GetOAuthToken|ParseJwtToken)' pkg/server`

预期： 两条命令均 exit 0。

- [ ] **步骤 7：提交**

```bash
git add pkg/server/config/authrevocation.go pkg/server/config/authrevocation_test.go pkg/server/config/casdoorconfig.go pkg/server/config/serverconfig.go pkg/server/config/serverconfig_auth_test.go pkg/server/safe/casdoor/client.go pkg/server/safe/casdoor/client_test.go
git commit -m "feat: isolate Casdoor clients and revocation config"
```

- [ ] **步骤 8：外部只读审查**

任务 1 提交后立即审查 `git diff HEAD^..HEAD`，反馈必须包含：P0/P1/P2、是否仍使用全局 Casdoor SDK、Secret 交叉校验、Auth/Manage 隔离、配置兼容性、测试真实性、`APPROVED` 或 `CHANGES_REQUIRED`。

---

### 任务 2： 认证身份、三类 Hook 与 Token Claims

**文件：**
- 修改： `pkg/server/types/auth.go`
- 新建： `pkg/server/types/auth_test.go`
- 修改： `pkg/server/safe/tokenissuer.go`
- 修改： `pkg/server/safe/tokenissuer_test.go`
- 修改： `pkg/server/api/public/auth_helpers.go`
- 修改： `pkg/server/api/public/auth_test.go`

- [ ] **步骤 1：写 Claims 与 Hook 红灯测试**

```go
func TestIssueTokenPairCarriesCasdoorIdentityInBothTokens(t *testing.T) {
    identity := types.AuthIdentity{Provider: "casdoor", ProviderSubject: "alice", Generation: 7}
    pair := mustIssuePair(t, identity)
    access := mustValidateAccess(t, pair.AccessToken)
    refresh := mustValidateRefresh(t, pair.RefreshToken)
    require.Equal(t, identity, access.Identity)
    require.Equal(t, identity, refresh.Identity)
}

func TestCasdoorTokenWithoutIdentityClaimsFailsClosed(t *testing.T) {
    token := signLegacyAccessToken(t)
    _, err := ValidateAccessToken(token, testSecret, types.AuthTypeUser, time.Now())
    require.ErrorContains(t, err, "Claims")
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/types ./pkg/server/safe ./pkg/server/api/public -run 'Test.*(AuthIdentity|CasdoorIdentity|Hook|TokenPair)' -count=1`

预期： FAIL，缺少 `AuthIdentity`、请求 Hook、事件 Hook 和新 Claims。

- [ ] **步骤 3：定义不可变认证契约**

```go
type AuthIdentity struct {
    UID             string
    Username        string
    AuthType        AuthType
    Provider        string
    ProviderSubject string
    Generation      uint64
    IssuedAt        time.Time
    ExpiresAt       time.Time
}

type AuthRequestArgs struct {
    Identity    AuthIdentity
    ServiceName string
    Path        string
    Method      string
    PathType    ApiType
    ClientIP    string
    TraceID     string
    Claims      map[string]interface{}
}

type CasdoorEvent struct {
    ID              string
    ServiceName     string
    AuthType        AuthType
    Provider        string
    ProviderSubject string
    UID             string
    EventType       string
    EventOrder      int64
    Generation      uint64
    Blocked         bool
    OccurredAt      time.Time
}

type IAuthRequestHookProvider interface {
    OnAuthRequest(context.Context, AuthRequestArgs) error
}

type ICasdoorEventHookProvider interface {
    OnCasdoorEvent(context.Context, CasdoorEvent) error
}
```

`Claims` 必须在构造参数时复制；Hook 按值接收参数。`CasdoorEvent` 只含标准字段，不含 Header、Token、Secret 或原始 Payload。

- [ ] **步骤 4：扩展 Token 签发和验证**

```go
type TokenIssueRequest struct {
    // 保留现有字段
    Identity types.AuthIdentity
}

func addIdentityClaims(claims jwt.MapClaims, identity types.AuthIdentity) {
    if identity.Provider == "" { return }
    claims["auth_provider"] = identity.Provider
    claims["provider_subject"] = identity.ProviderSubject
    claims["auth_generation"] = identity.Generation
}
```

Casdoor 内部 Token 要求三个字段齐全；非 Casdoor Token 不伪造 generation。Access/Refresh 的验证结果统一携带 `AuthIdentity`，并复制 Claims 供请求 Hook 只读使用。

- [ ] **步骤 5：验证公开错误边界**

```go
func TestAuthHookPublicErrorKeepsSafeContract(t *testing.T) {
    hookErr := types.NewPublicError(types.ErrorKindForbidden, 40321, "账户已冻结", errors.New("internal account state"))
    contract := types.ResolvePublicError(callAuthHook(hookErr))
    require.Equal(t, 403, contract.HTTPStatus)
    require.Equal(t, "账户已冻结", contract.Message)
    require.NotContains(t, contract.Message, "internal")
}
```

运行： `go test -race ./pkg/server/types ./pkg/server/safe ./pkg/server/api/public -count=1`

预期： PASS。

- [ ] **步骤 6：提交与外审**

```bash
git add pkg/server/types/auth.go pkg/server/types/auth_test.go pkg/server/safe/tokenissuer.go pkg/server/safe/tokenissuer_test.go pkg/server/api/public/auth_helpers.go pkg/server/api/public/auth_test.go
git commit -m "feat: add authenticated identity and request hooks"
```

外审重点：旧非 Casdoor Token 兼容边界、Casdoor 缺 Claims 是否 fail closed、Access/Refresh 是否一致、Claims 是否复制、公开错误是否只允许类型化消息。结论必须为 `APPROVED` 才继续。

---

### 任务 3： Badger/Redis 撤销事实存储

**文件：**
- 新建： `pkg/server/authstate/types.go`
- 新建： `pkg/server/authstate/badger.go`
- 新建： `pkg/server/authstate/badger_test.go`
- 新建： `pkg/server/authstate/redis.go`
- 新建： `pkg/server/authstate/redis_test.go`
- 新建： `pkg/server/authstate/manager.go`
- 新建： `pkg/server/authstate/manager_test.go`

- [ ] **步骤 1：写 Badger 重启红灯测试**

```go
func TestBadgerStoreRestoresGenerationAndBlockState(t *testing.T) {
    path := t.TempDir()
    first := mustOpenBadgerStore(t, path)
    key := IdentityKey{Service: "shop", AuthType: types.AuthTypeUser, Provider: "casdoor", Subject: "alice"}
    require.NoError(t, first.SaveState(State{Key: key, Generation: 4, Blocked: true}))
    require.NoError(t, first.Close())
    second := mustOpenBadgerStore(t, path)
    state, err := second.LoadState(key)
    require.NoError(t, err)
    require.Equal(t, uint64(4), state.Generation)
    require.True(t, state.Blocked)
}
```

- [ ] **步骤 2：验证红灯并实现本地存储**

运行： `go test ./pkg/server/authstate -run TestBadgerStoreRestoresGenerationAndBlockState -count=1`

预期： FAIL，包和实现尚不存在。

Badger 使用 `badger/v3` 直接事务，目录权限 `0700`，记录 JSON 版本号。身份状态键、事件幂等键和待处理 Hook 键分别使用 `state/v1/`、`event/v1/`、`hook/v1/` 前缀；读取字节必须 `ValueCopy`。

```go
type Store interface {
    Current(context.Context, IdentityKey) (State, error)
    Apply(context.Context, CasdoorEvent, time.Duration) (ApplyResult, error)
    ConfirmActive(context.Context, IdentityKey, uint64) (State, error)
    SaveSnapshot(context.Context, State) error
    PendingHooks(context.Context, int) ([]PendingHook, error)
    AckHook(context.Context, string) error
    Close() error
}
```

- [ ] **步骤 3：写 Redis 原子并发红灯测试**

```go
func TestRedisApplyEventIsAtomicAndIdempotent(t *testing.T) {
    redis := newScriptedRedis(t)
    store := NewRedisStore(redis, "core:authrevocation")
    event := testEvent("evt-1", "logout")
    results := make(chan ApplyResult, 64)
    var wg sync.WaitGroup
    for i := 0; i < 64; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            result, err := store.Apply(context.Background(), event, time.Hour)
            require.NoError(t, err)
            results <- result
        }()
    }
    wg.Wait()
    close(results)
    require.Equal(t, 1, countApplied(results))
    state := mustCurrent(t, store, event.IdentityKey())
    require.Equal(t, uint64(1), state.Generation)
}
```

- [ ] **步骤 4：实现 Redis Lua 原子协议**

```lua
local eventKey = KEYS[1]
local stateKey = KEYS[2]
if redis.call('EXISTS', eventKey) == 1 then
  local generation = redis.call('HGET', stateKey, 'generation') or '0'
  return {0, generation}
end
redis.call('SET', eventKey, '1', 'EX', ARGV[1])
local generation = redis.call('HINCRBY', stateKey, 'generation', 1)
redis.call('HSET', stateKey, 'blocked', ARGV[2], 'order', ARGV[3], 'uid', ARGV[4])
return {1, generation}
```

使用 go-zero `Redis.EvalCtx`，不得先 `GET` 再 `SET`。事件保留秒数取 Auth/Manage 最大 Refresh TTL。Lua 返回值严格解析为 `applied` 和 `generation`；Redis/解析错误直接返回稳定不可用错误。事件记录同时保存 `authority_applied` 与 `control_published` 阶段：相同事件在权威状态已应用但控制事件尚未发布时，返回原 generation 供上层重试发布，绝不再次 `HINCRBY`。

- [ ] **步骤 5：实现 Manager 严格授权语义**

```go
func (m *Manager) Authorize(ctx context.Context, identity types.AuthIdentity) error {
    state, err := m.authority.Current(ctx, IdentityKeyFrom(identity, m.service))
    if err != nil { return ErrAuthorityUnavailable }
    if state.Blocked || state.Generation != identity.Generation { return ErrIdentityRevoked }
    return nil
}
```

缺失状态等价于 generation `0` 且未阻断。共享模式每次授权都读取 Redis；Badger 快照仅用于恢复观察和 WebSocket 收敛，不能在 Redis 故障时授权。事件顺序小于已确认顺序时不得回退 blocked 状态；重复事件不得重复增加 generation。

状态迁移必须按下表实现，不允许由业务 Hook 改写：

| 事件 | generation | blocked |
| --- | --- | --- |
| `login`、`signup` | 不变 | 不直接修改；由在线 Callback 的 `ConfirmActive` 处理 |
| `logout`、`sso-logout`、普通 `update-user` | `+1` | 保持当前值 |
| `delete-user`、`unlink`、`IsForbidden=true` | `+1` | `true` |

`ConfirmActive(key, expectedGeneration)` 只用于 Callback 已在线验证 `IsForbidden=false`、`IsDeleted=false` 后恢复身份。Badger 在单事务中、Redis 在 Lua 中执行“当前 generation 等于 expectedGeneration 才清除 blocked”；若并发撤销已经推进 generation，返回 `ErrGenerationChanged`，本次 Callback 必须失败，不能签发旧世代 Token。

- [ ] **步骤 6：运行完整存储测试**

运行： `go test -race ./pkg/server/authstate -count=1`

Run with Redis: `CORE_TEST_REDIS=1 CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test -race ./pkg/server/authstate -run 'TestRedis' -count=1`

预期： 默认命令不依赖 Docker并通过；第二条在显式 Redis 环境中通过。

- [ ] **步骤 7：提交与外审**

```bash
git add pkg/server/authstate
git commit -m "feat: add durable Casdoor revocation authority"
```

外审重点：Lua 原子性、幂等 TTL、乱序单调性、Badger 重启、Redis 故障严格拒绝、共享模式不得由快照授权、无自行连接池/通用队列。结论必须为 `APPROVED`。

---

### 任务 4： ServiceContext 所有权与 EventBridge 生命周期

**文件：**
- 修改： `pkg/server/router/servicecontext.go`
- 修改： `pkg/server/router/servicecontext_auth_test.go`
- 新建： `pkg/server/router/servicecontext_authstate_test.go`
- 修改： `pkg/server/authstate/manager.go`
- 修改： `pkg/server/authstate/manager_test.go`

- [ ] **步骤 1：写初始化和关闭红灯测试**

```go
func TestServiceContextOwnsAuthLifecycleComponents(t *testing.T) {
    service := &allAuthHooksService{}
    cfg := validLocalCasdoorConfig(t)
    sc := NewServiceContextWithConfig(service, cfg)
    require.NotNil(t, sc.CasdoorClients)
    require.NotNil(t, sc.AuthRevocationManager)
    require.Same(t, service, sc.AuthRequestHookProvider)
    require.Same(t, service, sc.CasdoorEventHookProvider)
    sc.SetRunState(false)
    require.Eventually(t, func() bool { return sc.AuthRevocationManager == nil }, time.Second, time.Millisecond)
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/router -run 'TestServiceContext.*Auth' -count=1`

预期： FAIL，`ServiceContext` 尚无新组件。

- [ ] **步骤 3：按所有权顺序装配**

```go
type ServiceContext struct {
    // 保留现有字段
    CasdoorClients           *casdoorauth.ClientSet
    AuthRevocationManager    *authstate.Manager
    AuthRequestHookProvider  types.IAuthRequestHookProvider
    CasdoorEventHookProvider types.ICasdoorEventHookProvider
}
```

在 `initServiceContextPost` 中先捕获三类 Hook，再创建本地 EventBridge；完成 MQ 外部适配后创建 `AuthRevocationManager` 并注册 `auth.casdoor.identity.changed` 本地/外部订阅。共享模式要求 EventBridge 外部发布和订阅均可用，否则初始化失败。

- [ ] **步骤 4：实现关闭顺序与失败清理**

关闭顺序固定为：停止新认证 -> `AuthRevocationManager.Close` 关闭认证会话并停订阅/worker -> RouteWebSocketHub -> RouteCache -> ServiceEventBridge -> MQ -> Badger。任一初始化步骤失败时只关闭本次已创建组件，不触碰其他 ServiceContext。

- [ ] **步骤 5：运行生命周期测试**

运行： `go test -race ./pkg/server/router ./pkg/server/authstate -run 'Test.*(Auth|Lifecycle|Close|Shutdown)' -count=1`

预期： PASS，无 goroutine 泄漏和数据竞争。

- [ ] **步骤 6：提交与外审**

```bash
git add pkg/server/router/servicecontext.go pkg/server/router/servicecontext_auth_test.go pkg/server/router/servicecontext_authstate_test.go pkg/server/authstate/manager.go pkg/server/authstate/manager_test.go
git commit -m "feat: own authentication lifecycle in service context"
```

外审重点：初始化顺序、失败清理、关闭顺序、同名 ServiceContext 重建、跨服务隔离、EventBridge shared 强依赖。结论必须为 `APPROVED`。

---

### 任务 5： Callback、Refresh、TestToken 与路由迁移

**文件：**
- 新建： `pkg/server/api/public/casdoorcallback.go`
- 新建： `pkg/server/api/public/casdoorcallback_test.go`
- 修改： `pkg/server/api/public/callback.go`
- 修改： `pkg/server/api/public/casdoor.go`
- 新建： `pkg/server/api/public/casdoor_test.go`
- 修改： `pkg/server/api/public/refresh.go`
- 修改： `pkg/server/api/public/auth_helpers.go`
- 修改： `pkg/server/api/public/auth_test.go`
- 修改： `pkg/server/api/public/testtoken.go`
- 修改： `pkg/server/api/release/routes.go`
- 修改： `pkg/server/api/serverargs_test.go`

- [ ] **步骤 1：写路由和在线状态红灯测试**

```go
func TestCasdoorConfigReturnsNewCallbackPath(t *testing.T) {
    response := executeCasdoorConfig(t, "auth")
    require.Equal(t, "/api/casdoor/callback", response.BackgroundCallbackURL)
    require.Nil(t, releasedRoutes().GetRouter("/api/callback"))
}

func TestCallbackRejectsForbiddenCasdoorUser(t *testing.T) {
    fake := fakeCasdoorWithUser(&casdoorsdk.User{Name: "alice", Owner: "org", IsForbidden: true})
    response := executeCallback(t, fake)
    require.Equal(t, http.StatusUnauthorized, response.StatusCode)
    require.NotContains(t, response.Body.String(), "IsForbidden")
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/api/public ./pkg/server/api -run 'Test.*(Casdoor|Callback|Refresh|TestToken)' -count=1`

预期： FAIL，旧 Callback 仍为 `/api/callback` 且使用全局 Client。

- [ ] **步骤 3：实现 Callback 和废弃别名**

```go
type CasdoorCallback struct {
    Code  string `json:"code" form:"code"`
    State string `json:"state" form:"state"`
    Type  string `json:"type"`
}

// Deprecated: 使用 CasdoorCallback。
type Callback = CasdoorCallback

func (*CasdoorCallback) RouterInfo() *types.RouterInfo {
    return router.DefaultRouterInfoWithOptions(&CasdoorCallback{},
        router.WithMethod(http.MethodGet),
        router.WithPath("/api/casdoor/callback"),
        withAuthEndpointRateLimit(),
    )
}
```

`casdoor.go` 将现行类型改为 `CasdoorConfig`，并用 `type Casdoor = CasdoorConfig` 保留旧 Go 名称。`CasdoorConfig.RouterInfo()` 继续注册 `/api/casdoor`。Callback 选择明确认证域 Client，依次执行 OAuth 换取、Claims 解析、`GetUser(claims.Name)`、SDK 用户状态/组织/Subject 校验、读取当前世代、`ConfirmActive(expectedGeneration)`、`OnAuth`、签发 Token。`ConfirmActive` 遇到并发 generation 变化时本次签发失败。任何在线验证失败均返回脱敏 `401`。

- [ ] **步骤 4：收紧 Refresh 与 TestToken**

Refresh 先校验 Refresh Token 身份域和世代，再在线 `GetUser(provider_subject)`，然后执行 `OnAuth` 并只换发 Access Token。TestToken 保持测试用途，不伪装 Casdoor Provider；仍必须传 UID、时间和认证域给 `OnAuth`。

- [ ] **步骤 5：类型化 Hook 错误回归**

```go
func TestCallbackReturnsTypedAuthHookError(t *testing.T) {
    hook := rejectingHook(types.NewPublicError(types.ErrorKindForbidden, 40321, "账户已冻结", errors.New("secret")))
    response := executeValidCallback(t, hook)
    require.Equal(t, http.StatusForbidden, response.StatusCode)
    require.Contains(t, response.Body.String(), "账户已冻结")
    require.NotContains(t, response.Body.String(), "secret")
}
```

运行： `go test -race ./pkg/server/api/public ./pkg/server/api -count=1`

预期： PASS。

- [ ] **步骤 6：提交与外审**

```bash
git add pkg/server/api/public/casdoorcallback.go pkg/server/api/public/casdoorcallback_test.go pkg/server/api/public/callback.go pkg/server/api/public/casdoor.go pkg/server/api/public/casdoor_test.go pkg/server/api/public/refresh.go pkg/server/api/public/auth_helpers.go pkg/server/api/public/auth_test.go pkg/server/api/public/testtoken.go pkg/server/api/release/routes.go pkg/server/api/serverargs_test.go
git commit -m "feat: secure Casdoor callback and refresh lifecycle"
```

外审重点：旧 URL 是否确实未注册、Go 别名兼容、Callback 路径发现、在线用户检查、Refresh 旧世代、TestToken 非 Casdoor 语义、类型化错误是否安全。结论必须为 `APPROVED`。

---

### 任务 6： REST 请求前授权 Hook

**文件：**
- 新建： `pkg/server/trans/rest/authrequest.go`
- 新建： `pkg/server/trans/rest/authrequest_test.go`
- 修改： `pkg/server/trans/rest/server.go`
- 修改： `pkg/server/trans/rest/server_security_test.go`
- 修改： `pkg/server/router/request.go`

- [ ] **步骤 1：写中间件顺序红灯测试**

```go
func TestAuthRequestHookRunsAfterJWTBeforeRouter(t *testing.T) {
    calls := []string{}
    hook := hookFunc(func(_ context.Context, args types.AuthRequestArgs) error {
        calls = append(calls, "hook")
        require.Equal(t, "alice", args.Identity.ProviderSubject)
        return nil
    })
    router := routerFunc(func(types.IRequest) { calls = append(calls, "router") })
    serveAuthenticated(t, hook, router)
    require.Equal(t, []string{"hook", "router"}, calls)
}

func TestRedisFailureRejectsProtectedRouteButNotPublic(t *testing.T) {
    authority := failingAuthority{}
    require.Equal(t, http.StatusUnauthorized, servePrivate(t, authority).Code)
    require.Equal(t, http.StatusOK, servePublic(t, authority).Code)
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/trans/rest -run 'Test.*(AuthRequest|RedisFailure|AuthType)' -count=1`

预期： FAIL，请求 Hook 中间件尚不存在。

- [ ] **步骤 3：实现语义与 Hook 中间件**

```go
func AuthRequestHandler(sc *router.ServiceContext, info *types.RouterInfo, authType types.AuthType, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        identity, err := identityFromVerifiedContext(r.Context(), authType)
        if err == nil && identity.Provider == "casdoor" {
            err = sc.AuthRevocationManager.Authorize(r.Context(), identity)
        }
        if err == nil && sc.AuthRequestHookProvider != nil {
            err = invokeAuthRequestHook(r.Context(), sc.AuthRequestHookProvider, buildAuthRequestArgs(r, info, identity))
        }
        if err != nil { writeAuthBoundaryError(w, err); return }
        next.ServeHTTP(w, r)
    })
}
```

组装顺序必须是 `go-zero Authorize(AuthRequestHandler(RouteHandler))`，使 Hook 看到经过签名验证的 context。Auth Token 只能进入 Private，Manage Token 只能进入 Manage；Logto 可以执行业务 Hook，但不访问 Casdoor generation。

- [ ] **步骤 4：验证 panic、超时和公开错误**

```go
func TestAuthRequestHookFailureContract(t *testing.T) {
    assertHookResponse(t, panicHook{}, 500, "internal server error")
    assertHookResponse(t, timeoutHook{}, 500, "internal server error")
    assertHookResponse(t, publicHook("账户已冻结"), 403, "账户已冻结")
}
```

运行： `go test -race ./pkg/server/trans/rest ./pkg/server/router -count=1`

预期： PASS，响应和日志不包含 Hook 原始错误、Claims 或 Token。

- [ ] **步骤 5：提交与外审**

```bash
git add pkg/server/trans/rest/authrequest.go pkg/server/trans/rest/authrequest_test.go pkg/server/trans/rest/server.go pkg/server/trans/rest/server_security_test.go pkg/server/router/request.go
git commit -m "feat: authorize authenticated requests before routing"
```

外审重点：中间件真实顺序、context 可信来源、Auth/Manage 串域、Logto 边界、Hook panic/超时、Public 不受 Redis 故障影响。结论必须为 `APPROVED`。

---

### 任务 7： Casdoor Webhook、可靠控制事件与业务 Hook 重试

**文件：**
- 新建： `pkg/server/api/public/casdoorwebhook.go`
- 新建： `pkg/server/api/public/casdoorwebhook_test.go`
- 修改： `pkg/server/api/release/routes.go`
- 修改： `pkg/server/authstate/types.go`
- 修改： `pkg/server/authstate/manager.go`
- 修改： `pkg/server/authstate/manager_test.go`
- 修改： `pkg/server/event/servicebridge_test.go`

- [ ] **步骤 1：写 Webhook 边界红灯测试**

```go
func TestCasdoorWebhookRejectsWrongSecretBeforeParsingBody(t *testing.T) {
    response := postWebhook(t, "auth", "wrong", strings.Repeat("{", 100))
    require.Equal(t, http.StatusUnauthorized, response.Code)
    require.NotContains(t, response.Body.String(), "parse")
}

func TestCasdoorWebhookReturnsSuccessOnlyAfterControlAccepted(t *testing.T) {
    manager := blockingControlManager(t)
    done := postWebhookAsync(t, manager, validEventBody("evt-1"))
    select {
    case <-done:
        t.Fatal("EventBridge 接受控制事件前不得返回")
    default:
    }
    manager.acceptControl()
    require.Equal(t, http.StatusOK, (<-done).Code)
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/api/public ./pkg/server/authstate -run 'Test.*Webhook' -count=1`

预期： FAIL，Webhook 尚不存在。

- [ ] **步骤 3：实现请求边界和标准事件**

```go
const maxCasdoorWebhookBody = 64 << 10

func verifyWebhookSecret(header, expected string) bool {
    const prefix = "Bearer "
    if !strings.HasPrefix(header, prefix) || expected == "" { return false }
    actual := []byte(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
    wanted := []byte(expected)
    return len(actual) == len(wanted) && subtle.ConstantTimeCompare(actual, wanted) == 1
}
```

处理顺序固定为 Body 上限、Content-Type、`type` 白名单、对应 Secret、允许字段解析、组织/应用/用户/事件校验。无事件 ID 时，对规范化域、事件类型、Subject、时间和允许字段做 SHA-256 摘要；不得保存原始 Payload。

- [ ] **步骤 4：实现提交响应时机**

`AuthRevocationManager.ApplyEvent` 必须先原子写权威状态和幂等记录，再写本地快照/待处理业务 Hook，最后调用 `ServiceEventBridge.Publish` 的 `ControlDelivery`。任一步失败返回 `503`。只有 `control_published` 已持久化的完整重复事件可以直接返回 `200`；若前次只完成 `authority_applied`，重试必须沿用原 generation 再次发布控制事件，发布成功后原子标记 `control_published`，不得重复递增世代。

```go
err := sc.ServiceEventBridge.Publish(ctx, event.PublishRequest{
    Class:    event.ControlDelivery,
    External: sc.Config.AuthRevocation.Mode == "shared",
    Subject:  authstate.ControlSubject,
    Envelope: authstate.Envelope(applied.Event),
})
```

`CasdoorWebhook.RouterInfo()` 使用现有 `withAuthEndpointRateLimit()`，因此对公网可调用的 Webhook 默认限流；不得因它属于系统端点而绕过限流。

- [ ] **步骤 5：实现持久化业务 Hook worker**

EventBridge handler 只把标准事件交给 Manager；Manager 从 Badger `hook/v1/` 读取待处理记录，在独立有界 worker 中调用 `OnCasdoorEvent`。成功 Ack；失败按 `1s, 5s, 30s, 2m, 10m` 退避并持久化下次执行时间，重启后继续。框架世代递增不在 worker 内，因此重试不重复撤销。

- [ ] **步骤 6：运行可靠性测试**

运行： `go test -race ./pkg/server/api/public ./pkg/server/authstate ./pkg/server/event -run 'Test.*(Webhook|CasdoorEvent|Control|PendingHook)' -count=1`

预期： PASS；重复/乱序、EventBridge 拒绝、Hook 重启恢复均有确定性 channel 屏障测试，不使用 sleep 刷绿。

- [ ] **步骤 7：提交与外审**

```bash
git add pkg/server/api/public/casdoorwebhook.go pkg/server/api/public/casdoorwebhook_test.go pkg/server/api/release/routes.go pkg/server/authstate/types.go pkg/server/authstate/manager.go pkg/server/authstate/manager_test.go pkg/server/event/servicebridge_test.go
git commit -m "feat: process Casdoor revocation webhooks reliably"
```

外审重点：Secret 常量时间比较、认证先于解析、Body 限制、组织/应用绑定、幂等原子性、2xx 时机、EventBridge 控制事件、业务 Hook 重试不重复推进世代、日志脱敏。结论必须为 `APPROVED`。

---

### 任务 8： WebSocket 请求授权与撤销关闭

**文件：**
- 新建： `pkg/server/types/auth_websocket.go`
- 修改： `pkg/server/types/interface.go`
- 修改： `pkg/server/types/route_websocket_hub.go`
- 修改： `pkg/server/types/route_websocket_hub_test.go`
- 修改： `pkg/server/trans/websocket/melody/client.go`
- 修改： `pkg/server/trans/websocket/melody/sessionsubscriptions.go`
- 修改： `pkg/server/trans/websocket/melody/auth_boundary_test.go`
- 修改： `pkg/server/authstate/manager.go`

- [ ] **步骤 1：写订阅与撤销红灯测试**

```go
func TestAuthenticatedSubscriptionRunsRequestHook(t *testing.T) {
    session := loggedOnSession(t, casdoorIdentity("alice", 3))
    hook := recordingRequestHook()
    require.True(t, session.HandleSubscribe(validPrivateSubscription()))
    require.Equal(t, "alice", hook.Last().Identity.ProviderSubject)
}

func TestHigherGenerationClosesOldWebSocket(t *testing.T) {
    client := newClosableTestSocket()
    hub := newHubWithIdentity(t, client, casdoorIdentity("alice", 3))
    hub.RevokeIdentity(casdoorIdentity("alice", 4))
    require.True(t, client.Closed())
    require.Zero(t, hub.ActiveClients())
}
```

- [ ] **步骤 2：验证红灯**

运行： `go test ./pkg/server/types ./pkg/server/trans/websocket/melody -run 'Test.*(AuthenticatedSubscription|HigherGeneration|Revocation)' -count=1`

预期： FAIL，Hub lease 尚无认证身份，`IWebSocket` 尚无可选关闭能力。

- [ ] **步骤 3：添加非破坏性可选关闭接口**

```go
type IWebSocketCloser interface {
    Close() error
}

type WebSocketAuthIdentity struct {
    ServiceName     string
    AuthType        AuthType
    Provider        string
    ProviderSubject string
    UID             string
    Generation      uint64
}
```

不得给现有 `IWebSocket` 增加方法。MelodyClient 实现 `Close()`；测试客户端可选择实现。Hub lease 保存身份值副本，不保存当前请求或 Token。

- [ ] **步骤 4：订阅前执行完整授权**

`SessionSubscriptions.Logon` 保存 `safe.ValidateAccessToken` 返回的完整身份。每次认证路由订阅前重新执行 Token 语义、`AuthRevocationManager.Authorize` 和 `OnAuthRequest`；失败不创建持久 Router 实例。收到更高世代或 blocked 控制事件时，Hub 删除匹配 lease，并对实现 `IWebSocketCloser` 的客户端调用 `Close`。

- [ ] **步骤 5：共享权威故障关闭已有连接**

Manager 检测 Redis 权威不可用后发布本地关闭信号，Hub 关闭所有 Casdoor 认证连接；Public WebSocket 若存在则不受影响。恢复后只允许新登录/新订阅，不能复活旧 lease。

- [ ] **步骤 6：运行 WebSocket race 测试**

运行： `go test -race ./pkg/server/types ./pkg/server/trans/websocket/melody ./pkg/server/authstate -run 'Test.*(WebSocket|Subscription|Revocation|Authority)' -count=1`

预期： PASS，无死锁、双重归还 Router 或请求级状态泄漏。

- [ ] **步骤 7：提交与外审**

```bash
git add pkg/server/types/auth_websocket.go pkg/server/types/interface.go pkg/server/types/route_websocket_hub.go pkg/server/types/route_websocket_hub_test.go pkg/server/trans/websocket/melody/client.go pkg/server/trans/websocket/melody/sessionsubscriptions.go pkg/server/trans/websocket/melody/auth_boundary_test.go pkg/server/authstate/manager.go
git commit -m "feat: revoke authenticated websocket sessions"
```

外审重点：未破坏 `IWebSocket`、身份来自 Token、每次订阅重验、旧世代关闭、Redis 故障关闭、Router 实例释放和并发锁序。结论必须为 `APPROVED`。

---

### 任务 9： 真实集成、门禁、迁移文档与能力文件

**文件：**
- 新建： `examples/integration/casdoor-auth-lifecycle/helpers_test.go`
- 新建： `examples/integration/casdoor-auth-lifecycle/rest_test.go`
- 新建： `examples/integration/casdoor-auth-lifecycle/websocket_test.go`
- 新建： `examples/integration/casdoor-auth-lifecycle/shared_test.go`
- 修改： `docker-compose.integration.yml`
- 修改： `scripts/test.sh`
- 修改： `scripts/test-ci-contract.sh`
- 修改： `docs/codex/DEPRECATION_REGISTER.md`
- 修改： `docs/codex/API_COMPATIBILITY_SURFACE.md`
- 新建： `docs/codex/BREAKING_CHANGE_APPROVAL.md`
- 修改： `CHANGELOG.md`
- 修改： `.codex/skills/use-digitalway-core/SKILL.md`
- 修改： `.codex/skills/use-digitalway-core/references/core-backend-api.md`
- 修改： `internal/compat/compat.go`

- [ ] **步骤 1：建立真实进程测试夹具**

假 Casdoor 使用 `httptest.Server` 实现 OAuth Token、JWT 和 `GetUser` 所需端点；测试服务通过 `NewServiceContextWithConfig` 启动真实 REST/Melody 端口，Badger 使用 `t.TempDir()`。不得访问生产 Casdoor，不得在默认测试中自动启动 Docker。

```go
func TestCasdoorAuthLifecycle(t *testing.T) {
    app := startLifecycleApp(t, localModeConfig(t))
    authPair := app.Callback(t, "auth", "alice")
    require.Equal(t, http.StatusOK, app.Private(t, authPair.AccessToken).StatusCode)
    app.Webhook(t, "auth", "logout", "alice")
    require.Equal(t, http.StatusUnauthorized, app.Private(t, authPair.AccessToken).StatusCode)
    require.Equal(t, http.StatusUnauthorized, app.Refresh(t, authPair.RefreshToken).StatusCode)
    nextPair := app.Callback(t, "auth", "alice")
    require.Equal(t, http.StatusOK, app.Private(t, nextPair.AccessToken).StatusCode)
}
```

- [ ] **步骤 2：覆盖 Auth/Manage、Hook 和 WebSocket**

测试必须逐项断言：Auth Token 不能访问 Manage；Manage Token 不能访问 Private；请求 Hook 类型化拒绝返回安全 `403`；普通错误返回脱敏 `500`；Webhook 后旧 WebSocket 在超时窗口内收到关闭；重新登录后新连接可订阅。

- [ ] **步骤 3：增加显式 Redis 集成入口**

```bash
integration-casdoor-auth)
  : "${CORE_TEST_REDIS_ADDR:?CORE_TEST_REDIS_ADDR is required}"
  CORE_TEST_CASDOOR_AUTH=1 go test -race ./examples/integration/casdoor-auth-lifecycle -count=1 -timeout=15m
  ;;
```

`docker-compose.integration.yml` 复用现有 `redis`，测试命令由调用方先执行 `docker compose -f docker-compose.integration.yml up -d redis`。测试停止 Redis 后断言 Private/Manage/Refresh/Callback 和新 WebSocket 失败，Public REST 保持 `200`，随后恢复 Redis 完成清理。

- [ ] **步骤 4：更新安全与 CI 契约**

`scripts/test.sh security` 增加 `./pkg/server/safe/casdoor ./pkg/server/authstate ./pkg/server/api/public ./pkg/server/trans/websocket/melody`。`scripts/test-ci-contract.sh` 断言 required 门禁不会隐式调用 Docker 或 `integration-casdoor-auth`。

运行： `./scripts/test.sh security`

预期： PASS，不访问外部服务。

- [ ] **步骤 5：更新兼容和迁移文档**

明确记录：

- `/api/callback` 删除，前端必须读取 `/api/casdoor` 返回的 `/api/casdoor/callback`
- `Callback`/`Casdoor` Go 类型为废弃别名
- 四个 Token Secret 必须轮换，全部用户和管理员重新登录
- 新 Token 的 `auth_provider`、`provider_subject`、`auth_generation` 契约
- shared 模式必须配置 Redis；故障时认证面 fail closed
- Webhook Secret 独立、HTTPS、不得记录 Header/Payload
- `IAuthHookProvider`、`IAuthRequestHookProvider`、`ICasdoorEventHookProvider` 的调用时机和类型化公开错误规则

- [ ] **步骤 6：最终质量门禁**

```bash
gofmt -w pkg/server/config pkg/server/safe pkg/server/authstate pkg/server/router pkg/server/api/public pkg/server/trans/rest pkg/server/trans/websocket/melody pkg/server/types examples/integration/casdoor-auth-lifecycle internal/compat
go test -race ./pkg/server/config ./pkg/server/safe/... ./pkg/server/authstate ./pkg/server/router ./pkg/server/api/public ./pkg/server/trans/rest ./pkg/server/trans/websocket/melody ./pkg/server/types -count=1
go vet ./pkg/server/...
./scripts/check-logging.sh
./scripts/test.sh security
./scripts/ci.sh required/contracts
```

显式外部依赖门禁：

```bash
docker compose -f docker-compose.integration.yml up -d redis
CORE_TEST_REDIS_ADDR=127.0.0.1:6379 ./scripts/test.sh integration-casdoor-auth
docker compose -f docker-compose.integration.yml stop redis
```

预期： 所有 required 命令 exit 0；集成测试明确启用时 exit 0；日志扫描无 Token、Secret、Header、Payload、Claims dump。

- [ ] **步骤 7：提交**

```bash
git add examples/integration/casdoor-auth-lifecycle docker-compose.integration.yml scripts/test.sh scripts/test-ci-contract.sh docs/codex/DEPRECATION_REGISTER.md docs/codex/API_COMPATIBILITY_SURFACE.md docs/codex/BREAKING_CHANGE_APPROVAL.md CHANGELOG.md .codex/skills/use-digitalway-core/SKILL.md .codex/skills/use-digitalway-core/references/core-backend-api.md internal/compat/compat.go
git commit -m "test: verify Casdoor authentication lifecycle"
```

- [ ] **步骤 8：整体外部终审**

审查范围为计划提交之后至任务 9 HEAD，并同时读取本计划和设计规格。使用下面的命令动态定位计划提交，反馈必须包含：

```bash
PLAN_BASE="$(git log --format=%H --grep='docs: plan Casdoor authentication lifecycle' -1)"
git diff "$PLAN_BASE"..HEAD
```

1. Findings，按 P0/P1/P2，附文件和行号
2. 设计 15 节验收项逐条覆盖表
3. Auth/Manage/ServerManage、Casdoor/Logto/TestToken 兼容性
4. 单节点 Badger、共享 Redis、EventBridge、Webhook、REST、WebSocket 故障矩阵
5. 测试是否真实制造并发、重启、Redis 断连和旧会话关闭
6. 日志、错误、Secret、Claims 和 Payload 泄露检查
7. 公共 Go API、路由、JSON、配置、JWT 和运行时行为的迁移登记
8. 最终裁定 `APPROVED` 或 `CHANGES_REQUIRED`

只有整体终审 `APPROVED` 后，才能将本计划状态改为完成并推送。

---

## 2. 完成定义

- [ ] 九个任务均有独立提交哈希和外部 `APPROVED`
- [ ] Auth/Manage 使用不同 Client、Secret、Token 和撤销键
- [ ] Callback/Refresh 在线验证 `IsForbidden`、`IsDeleted`、Owner 和 Subject
- [ ] Casdoor Access/Refresh 都携带并验证身份域和世代
- [ ] 单节点 Badger 重启不恢复旧 Token；共享 Redis 故障严格拒绝认证
- [ ] Webhook 幂等、乱序、2xx 时机和 EventBridge 控制事件通过测试
- [ ] REST 和 WebSocket 均执行请求 Hook，撤销后旧会话关闭
- [ ] 只有类型化公开错误可返回业务安全消息
- [ ] 默认门禁不依赖 Docker；Redis 集成显式开启
- [ ] 兼容表、废弃登记、破坏变更批准、CHANGELOG 和 skill 与实现一致
- [ ] 整体外部终审结论为 `APPROVED`

## 3. 计划执行后的外部终审提示词

```markdown
请只读审查 Casdoor 认证生命周期完整实现，不要修改代码。

审查范围：
PLAN_BASE="$(git log --format=%H --grep='docs: plan Casdoor authentication lifecycle' -1)"
git diff "$PLAN_BASE"..HEAD

规格：
- docs/superpowers/specs/2026-07-16-casdoor-auth-lifecycle-design.md
- docs/superpowers/plans/2026-07-16-casdoor-auth-lifecycle.md

重点检查：
1. Auth 与 Manage 是否使用独立 Casdoor Client，是否彻底停止全局 SDK 初始化。
2. Callback/Refresh 是否在线检查 IsForbidden、IsDeleted、Owner、ProviderSubject。
3. Access/Refresh 的 Provider、Subject、Generation 是否一致且严格验证。
4. 单节点 Badger 和共享 Redis 是否各自满足权威语义；Redis 故障是否禁止使用快照授权。
5. Webhook Secret、Body 上限、域绑定、幂等、乱序、原子递增和 2xx 时机是否正确。
6. EventBridge 控制事件和异步业务 Hook 是否可靠且不重复推进世代。
7. REST Hook 是否位于 JWT 后、Router 前；Auth/Manage 是否不串域。
8. WebSocket 是否使用可信 Token 身份、每次订阅重验、撤销后关闭旧会话。
9. 类型化公开错误是否安全返回；普通错误、panic、超时是否脱敏。
10. 兼容、迁移、密钥轮换、测试门禁和能力文件是否与实现一致。

请输出：
- Findings，按 P0/P1/P2 排序并提供文件/行号
- 规格覆盖矩阵
- 安全与故障矩阵
- API/配置/JWT/路由兼容性评估
- 测试真实性与缺口
- 最终裁定：APPROVED 或 CHANGES_REQUIRED
- 是否允许关闭计划并推送
```
