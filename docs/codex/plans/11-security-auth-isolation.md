# 安全与认证隔离实施计划

> **面向智能体开发者：** 必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 子技能，按任务逐项实施本计划。步骤使用复选框（`- [ ]`）语法跟踪。

**目标：** 在不替换 go-zero 现有 HTTP 限制和弹性中间件的前提下，使配置存储、CORS、Logto 认证、客户端 IP 解析和公共错误响应默认安全。

**架构：** 保持 go-zero `rest.RestConf` 作为 `MaxBytes`、`MaxConns`、breaker、shedding、timeout 和 recovery 的 owner。仅添加 Digitalway 特定策略：每 handler 不可变 Logto 配置、显式 CORS origin、感知受信代理的客户端 IP 解析、最小权限配置文件、通用公共认证错误和稳定安全响应头。在可行时保留现有导出入口，并通过现有 WebServer 启动边界传递新启动错误。

**技术栈：** Go 1.26、go-zero v1.10.2 `rest`/`logx`、`net/http`、`net/netip`、`httptest`、`keyfunc/v2`、JWT v5。

---

## 归属与非目标

- 任务 11 负责安全行为和定向测试。
- 任务 8 后续统一所有剩余运行时日志；任务 11 仅删除该边界所需的认证密钥、fatal 进程退出和不安全客户端细节。
- 任务 15 后续定义完整的类型化公共错误契约；任务 11 在删除内部原因的同时保留当前状态码。
- 不添加第二个 body limiter、连接 limiter、circuit breaker 或 load shedder。应验证 go-zero `RestConf.MaxBytes`、`MaxConns` 和中间件默认值。
- 不仅为限速而添加 Redis。分布式限速取决于任务 14 中经批准的 go-zero `core/limit` Provider；在此之前，现有最大连接数、最大 body、breaker、shedding 和认证拒绝控制仍是受支持基线。

## 任务 11.1：最小权限配置文件

**状态：** 已在 `804a2de` 完成。

**文件：**
- 创建：`pkg/server/config/serverconfig_security_test.go`
- 修改：`pkg/server/config/serverconfig.go`

- [x] **步骤 1：编写失败的权限测试**

在 `config` 包中为包内 `writeConfigFile` 辅助程序和 `migrateConfig` 添加测试。`ServerConfig.Save` 在 `go test` 下刻意提前返回，因此测试共享写入边界可避免禁用现有隔离。断言新文件和现有 `0666` 文件最终均为 `mode.Perm() == 0o600`。

```go
func TestWriteConfigFileUsesPrivateMode(t *testing.T) {
	file := filepath.Join(t.TempDir(), "service.json")
	require.NoError(t, writeConfigFile(file, []byte(`{"Name":"secure"}`)))
	info, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
```

- [x] **步骤 2：验证 RED**

运行：`go test ./pkg/server/config -run 'TestWriteConfigFileUsesPrivateMode|TestWriteConfigFileTightensExistingMode|TestMigrateConfigTightensFileMode' -count=1`

预期：失败，因为当前写入使用 `0o777` 和 `0o666`。

- [x] **步骤 3：实现最小权限处理**

使用 `os.WriteFile(..., 0o600)` 后跟 `os.Chmod(file, 0o600)` 实现 `writeConfigFile(file string, data []byte) error`。`Save` 和 `migrateConfig` 均使用它，使现有过宽文件也会收紧，即使单独 `os.WriteFile` 不会改变其模式。

- [x] **步骤 4：验证 GREEN 与回归**

运行：

```bash
go test ./pkg/server/config -count=1
go test ./pkg/server/... -count=1
```

- [x] **步骤 5：提交**

```bash
git add pkg/server/config/serverconfig.go pkg/server/config/serverconfig_security_test.go
git commit -m "fix: protect server configuration files"
```

## 任务 11.2：显式 CORS Origin

**状态：** 已在 `937d381` 完成。

**文件：**
- 创建：`pkg/server/trans/rest/server_security_test.go`
- 修改：`pkg/server/trans/rest/server.go`

- [x] **步骤 1：编写失败的选项测试**

抽取一个校验 CORS 输入并返回 go-zero run option 的包内辅助程序。测试禁用 CORS 时不返回 option，启用 CORS 但无 origin 时返回错误，显式 origin 被保留。仅当调用方显式选择时包含 `"*"`。

```go
func restRunOptions(isCors bool, origins []string) ([]rest.RunOption, error)
```

- [x] **步骤 2：验证 RED**

运行：`go test ./pkg/server/trans/rest -run TestRestRunOptions -count=1`

预期：失败，因为辅助程序尚不存在，且 `origin` 被忽略。

- [x] **步骤 3：实现 fail-closed CORS 构造**

仅当 `isCors` 为 true 且提供至少一个非空 origin 时调用 `rest.WithCors(origins...)`。使 `NewServer` 返回 `(*Server, error)`，通过 `run.(*WebServer).newWebServer` 传播错误，并让 `initServer` 通过现有边界错误处理停止启动。

- [x] **步骤 4：验证 GREEN**

运行：

```bash
go test ./pkg/server/trans/rest ./pkg/server/run -count=1
go test ./pkg/server/... -count=1
```

- [x] **步骤 5：提交**

```bash
git add pkg/server/trans/rest/server.go pkg/server/trans/rest/server_security_test.go pkg/server/run/server.go
git commit -m "fix: require explicit CORS origins"
```

## 任务 11.3：每 Handler 不可变 Logto 策略

**状态：** 已在 `daa2c57` 完成。

**文件：**
- 创建：`pkg/server/safe/logto/authmiddleware_test.go`
- 修改：`pkg/server/safe/logto/authmiddleware.go`
- 修改：`pkg/server/trans/rest/server.go`

- [x] **步骤 1：编写失败的并发策略测试**

引入不可变策略类型，并使中间件校验使用它，而非包全局变量：

```go
type AuthConfig struct {
	Issuer           string
	ExpectedAudience string
}

func AuthMiddleware(jwks *keyfunc.JWKS, next http.Handler, cfg AuthConfig) http.Handler
func NewAuthHandler(next http.HandlerFunc, cfg AuthConfig) (http.Handler, error)
```

使用两个本地 JWKS 测试 Server，并以不同 issuer/audience 组合并发运行 handler。在 `go test -race` 下，每个匹配 token 必须仅能在自己的 handler 上成功。

- [x] **步骤 2：验证 RED**

运行：`go test -race ./pkg/server/safe/logto -run TestAuthHandlersKeepIndependentPolicy -count=1`

预期：失败，因为当前策略存在于可变包全局变量中。

- [x] **步骤 3：实现本地策略与启动错误**

只规范化一次 issuer 尾部斜杠，从本地配置派生 `jwksURL` 和可接受 issuer，并返回 JWKS 初始化错误，而非调用 `log.Fatal`。更新 REST 路由注册，使每个 Logto handler 只构造一次，并将构造错误传播给 `NewServer`。

- [x] **步骤 4：有意保留兼容性**

仅当仓库消费方需要时，将旧导出 `AuthHandler(next, issuer, audience) http.Handler` 保留为已废弃封装。该封装绝不得终止进程；内部启动代码必须使用 `NewAuthHandler` 并处理其错误。

- [x] **步骤 5：验证 GREEN**

运行：

```bash
go test -race ./pkg/server/safe/logto ./pkg/server/trans/rest -count=1
go test ./pkg/server/... -count=1
```

- [x] **步骤 6：提交**

```bash
git add pkg/server/safe/logto pkg/server/trans/rest/server.go
git commit -m "fix: isolate Logto authentication policy"
```

## 任务 11.4：通用认证与框架错误响应

**状态：** 已在 `5e4bcd8` 完成。

**文件：**
- 修改：`pkg/server/safe/logto/authmiddleware_test.go`
- 创建：`pkg/server/trans/rest/error_security_test.go`
- 修改：`pkg/server/safe/logto/authmiddleware.go`
- 修改：`pkg/server/trans/rest/error.go`

- [x] **步骤 1：编写失败的信息泄漏测试**

断言格式错误、过期、audience 错误、issuer 错误、JWKS refresh、白名单和内部错误保留预期 HTTP 状态，但响应体不包含 token parser 文本、预期 claim、issuer URL、堆栈/错误字符串或提供的密钥 fixture。

- [x] **步骤 2：验证 RED**

运行：`go test ./pkg/server/safe/logto ./pkg/server/trans/rest -run 'TestAuthResponseDoesNotDiscloseCause|TestWriteErrorResponseDoesNotDiscloseCause' -count=1`

预期：失败，因为当前 handler 返回 `err.Error()` 和预期 claim 值。

- [x] **步骤 3：实现安全公共消息**

返回 `authentication failed` 等稳定通用消息，并从框架生成响应中省略 `ErrorDetail.Details["error"]`。封装并将内部原因返回给 owner 边界；仅在任务 11 拥有终止认证决策时使用结构化 `logx`，且不包含 token 或 claim。

- [x] **步骤 4：验证 GREEN**

运行：`go test ./pkg/server/safe/logto ./pkg/server/trans/rest -count=1`

- [x] **步骤 5：提交**

```bash
git add pkg/server/safe/logto pkg/server/trans/rest/error.go pkg/server/trans/rest/error_security_test.go
git commit -m "fix: hide authentication error details"
```

## 任务 11.5：受信代理客户端 IP 解析

**状态：** 已在 `503a01d` 完成。

**文件：**
- 创建：`pkg/utils/ip_test.go`
- 修改：`pkg/utils/ip.go`
- 修改：`pkg/server/config/serverconfig.go`
- 修改：`pkg/server/router/request.go`
- 修改：`pkg/server/trans/rest/server.go`
- 修改：`pkg/server/run/htmlserver.go`

- [x] **步骤 1：编写失败的信任测试**

定义 `ClientPublicIP(r *http.Request, trustedProxies ...string) string`。测试对不受信 `RemoteAddr` 忽略转发头，对受信精确 IP 或 CIDR 采纳转发头，跳过格式错误项，并正确返回直连 IPv4/IPv6 地址。对转发链从右向左遍历，移除受信代理跳，并返回第一个不受信地址，使客户端可控的最左值无法覆盖受信代理追加的地址。

- [x] **步骤 2：验证 RED**

运行：`go test ./pkg/utils -run TestClientPublicIP -count=1`

预期：失败，因为当前无条件信任转发头。

- [x] **步骤 3：实现基于 netip 的信任评估**

使用 `net.SplitHostPort` 和 `netip.ParseAddr` 解析 `RemoteAddr`；将每个已配置代理解析为地址或 prefix。仅当直连 peer 匹配已配置受信代理时，才检查 `X-Forwarded-For` 和 `X-Real-IP`。从右向左遍历 `X-Forwarded-For`，返回第一个不在受信集合内的地址。未配置受信代理时，返回直连 peer。

- [x] **步骤 4：添加并校验配置**

向 `ServerConfig` 添加 `TrustedProxies []string`，默认为空切片，将每个值校验为 IP 或 CIDR，并在每个请求/白名单边界传递它。无效代理配置必须在提供流量前失败。

- [x] **步骤 5：验证 GREEN 与竞态安全**

运行：

```bash
go test ./pkg/utils ./pkg/server/config ./pkg/server/router ./pkg/server/trans/rest ./pkg/server/run -count=1
go test -race ./pkg/utils ./pkg/server/router -count=1
```

- [x] **步骤 6：提交**

```bash
git add pkg/utils/ip.go pkg/utils/ip_test.go pkg/server/config/serverconfig.go pkg/server/router/request.go pkg/server/trans/rest/server.go pkg/server/run/htmlserver.go
git commit -m "fix: trust forwarding headers only from configured proxies"
```

## 任务 11.6：安全响应头与现有 go-zero 限制

**状态：** 已在 `0bc1a14` 完成。

**文件：**
- 修改：`pkg/server/trans/rest/server_security_test.go`
- 修改：`pkg/server/trans/rest/server.go`
- 修改：`docs/codex/plans/11-security-auth-isolation.md`，记录任务 14 所需的最终能力证据

- [x] **步骤 1：编写失败的响应头与限制测试**

测试一个包内中间件：添加 `X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer` 和 `X-Frame-Options: DENY`，但不覆盖调用方更严格的值。添加配置测试，证明调用 `ServerConfig.ApplyDefaults` 后，go-zero `RestConf.MaxBytes`、`MaxConns` 和它们的中间件 flag 仍已启用。

- [x] **步骤 2：验证 RED**

运行：`go test ./pkg/server/trans/rest ./pkg/server/config -run 'TestSecurityHeaders|TestServerConfigPreservesGoZeroLimits' -count=1`

预期：因中间件不存在，header 测试失败；limit 测试记录当前 go-zero 行为。

- [x] **步骤 3：添加范围受限的响应头中间件**

使用安全响应头中间件封装已注册 HTTP 路由。在 TLS 终止归属明确前不添加 HSTS，也不重复 go-zero body/连接限制。

- [x] **步骤 4：如实记录限速支持**

在本计划的完成证据中，将服务全局最大连接数、body 限制、breaker 和 shedding 记录为 `Stable`。在任务 14 批准 go-zero `core/limit` Redis 支撑配置和行为测试前，将分布式 auth/API 限速记录为 `Unsupported`；拒绝任何缺少运行时 Provider 的未来限速配置。任务 14 必须将此证据复制到能力矩阵。

- [x] **步骤 5：整体验证任务 11**

运行：

```bash
go vet ./pkg/server/... ./pkg/utils
go test ./pkg/server/... ./pkg/utils -count=1
go test -race ./pkg/server/safe/logto ./pkg/server/router ./pkg/utils -count=1
```

- [x] **步骤 6：提交**

```bash
git add pkg/server/trans/rest pkg/server/config docs/codex/plans/11-security-auth-isolation.md
git commit -m "fix: establish HTTP security baseline"
```

## 完成证据

| 能力 | 状态 | 证据 |
| --- | --- | --- |
| 配置文件权限 | 稳定 | `serverconfig_security_test.go` 验证新文件和已迁移文件均为 `0600` |
| 显式 CORS 允许列表 | 稳定 | 启用 CORS 时拒绝空 origin，并将显式值传给 go-zero `WithCors` |
| Logto 策略隔离 | 稳定 | 每 handler issuer/audience 并发测试在 `-race` 下通过 |
| 安全认证/框架响应 | 稳定 | 捕获的响应体中不包含带密钥的错误 fixture |
| 受信转发代理 | 稳定 | 直连、精确 IP、CIDR、IPv6、格式错误和从右向左链测试通过 |
| Body 与连接限制 | 稳定 | 已断言 go-zero 1 MiB `MaxBytes`、10000 `MaxConns`、breaker 和 shedding 默认值 |
| HTTP 安全响应头 | 稳定 | REST 路由和 WebSocket 握手设置经测试不覆盖的响应头 |
| 分布式 auth/API 限速 | 不支持 | 不接受任何配置；添加前，任务 14 必须批准 go-zero `core/limit` Provider 和行为契约 |

任务 11 已通过实施提交 `804a2de`、`937d381`、`daa2c57`、`5e4bcd8`、`503a01d` 和 `0bc1a14` 完成。最终验证已通过 server/utils vet、Logto/Router/Utils 竞态测试、完整 server/utils 测试套件和静态安全断言。不再存在认证策略全局变量或 `log.Fatal`，CORS 无法静默变为通配符，转发头需要受信代理，配置文件为 `0600`，且公共认证/框架错误体不包含内部原因。

## 审查后修复

| 项目 | 状态 | 提交 / 证据 |
| --- | --- | --- |
| A. 本地转发地址 fail closed | 已完成 | `3f4f506`；未配置本地 peer、受信不安全候选、直连 loopback 和 RFC1918 客户端的 Utils vet 与竞态测试通过 |
| B. Logto 身份绑定与 nil Request 保护 | 已完成 | `e320017`；uid/sub 和 username 回退上下文测试、缺少身份拒绝、RouteHandler nil 守卫、vet 与竞态测试通过 |
| C. CORS 示例与文档 | 已完成 | `6dd5f89`；所有已记录且可执行的 `IsCors: true` 示例均声明本地 origin，已变更 main 包可编译，运行时 fail-closed 行为不变 |
| D. 共享 JWKS 生命周期 | 已完成 | `307f44e`；每个 REST Server 按 AuthConfig 复用一个 JWKS，应用五分钟 unknown-KID refresh 限制，停止/注册失败时关闭后台 refresh，且复用与竞态测试通过 |
| E. TrustedProxies 文档与 security 测试模式 | 已完成 | `219da16`；两份 skill 参考、server/配置示例文档和已跟踪 JSON 示例定义代理信任；`./scripts/test.sh security` 通过全部四个安全包 |
