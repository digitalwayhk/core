# Security and Authentication Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make configuration storage, CORS, Logto authentication, client IP resolution, and public error responses secure by default without replacing go-zero's existing HTTP limits and resilience middleware.

**Architecture:** Keep go-zero `rest.RestConf` as the owner of `MaxBytes`, `MaxConns`, breaker, shedding, timeout, and recovery. Add only Digitalway-specific policy: immutable per-handler Logto configuration, explicit CORS origins, trusted-proxy-aware client IP resolution, least-permission config files, generic public authentication errors, and stable security headers. Preserve existing exported entrypoints where feasible and route new startup errors through the existing WebServer startup boundary.

**Tech Stack:** Go 1.26, go-zero v1.10.2 `rest`/`logx`, `net/http`, `net/netip`, `httptest`, `keyfunc/v2`, JWT v5.

---

## Ownership and Non-Goals

- Task 11 owns security behavior and focused tests.
- Task 8 later normalizes all remaining runtime logs; Task 11 removes only authentication secrets, fatal process exits, and unsafe client details needed for this boundary.
- Task 15 later defines the complete typed public error contract; Task 11 preserves current status codes while removing internal causes.
- Do not add a second body limiter, connection limiter, circuit breaker, or load shedder. Verify go-zero `RestConf.MaxBytes`, `MaxConns`, and middleware defaults instead.
- Do not add Redis solely for rate limiting. Distributed rate limiting is conditional on an approved go-zero `core/limit` provider in Task 14; until then existing max-connections, max-body, breaker, shedding, and authentication rejection controls remain the supported baseline.

## Task 11.1: Least-Permission Configuration Files

**Files:**
- Create: `pkg/server/config/serverconfig_security_test.go`
- Modify: `pkg/server/config/serverconfig.go`

- [ ] **Step 1: Write failing permission tests**

Add tests in package `config` that temporarily replace `CONFIGDIRPATH`, call `ServerConfig.Save`, and invoke `migrateConfig` on a legacy JSON file. Assert both resulting files have `mode.Perm() == 0o600`; preserve and restore `CONFIGDIRPATH` with `t.Cleanup`.

```go
func TestServerConfigSaveWritesPrivateFile(t *testing.T) {
	dir := t.TempDir() + string(os.PathSeparator)
	old := CONFIGDIRPATH
	CONFIGDIRPATH = dir
	t.Cleanup(func() { CONFIGDIRPATH = old })

	cfg := NewServiceDefaultConfig("secure-save", 18080)
	require.NoError(t, cfg.Save())
	info, err := os.Stat(filepath.Join(dir, "secure-save.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./pkg/server/config -run 'TestServerConfigSaveWritesPrivateFile|TestMigrateConfigPreservesPrivateFileMode' -count=1`

Expected: FAIL because current writes use `0o777` and `0o666`.

- [ ] **Step 3: Implement minimal permission handling**

Use `0o600` for both writes. After a migration rewrite, call `os.Chmod(file, 0o600)` so an existing permissive file is tightened even though `os.WriteFile` does not change the mode of an existing file.

- [ ] **Step 4: Verify GREEN and regression**

Run:

```bash
go test ./pkg/server/config -count=1
go test ./pkg/server/... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add pkg/server/config/serverconfig.go pkg/server/config/serverconfig_security_test.go
git commit -m "fix: protect server configuration files"
```

## Task 11.2: Explicit CORS Origins

**Files:**
- Create: `pkg/server/trans/rest/server_security_test.go`
- Modify: `pkg/server/trans/rest/server.go`

- [ ] **Step 1: Write failing option tests**

Extract a package-private helper that validates CORS input and returns go-zero run options. Test that disabled CORS returns no option, enabled CORS with no origin returns an error, and explicit origins are retained. Include `"*"` only as an explicit caller choice.

```go
func restRunOptions(isCors bool, origins []string) ([]rest.RunOption, error)
```

- [ ] **Step 2: Verify RED**

Run: `go test ./pkg/server/trans/rest -run TestRestRunOptions -count=1`

Expected: FAIL because no helper exists and `origin` is ignored.

- [ ] **Step 3: Implement fail-closed CORS construction**

Call `rest.WithCors(origins...)` only when `isCors` is true and at least one non-empty origin is supplied. Make `NewServer` return `(*Server, error)`, propagate the error through `run.(*WebServer).newWebServer`, and let `initServer` stop startup with the existing boundary error handling.

- [ ] **Step 4: Verify GREEN**

Run:

```bash
go test ./pkg/server/trans/rest ./pkg/server/run -count=1
go test ./pkg/server/... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add pkg/server/trans/rest/server.go pkg/server/trans/rest/server_security_test.go pkg/server/run/server.go
git commit -m "fix: require explicit CORS origins"
```

## Task 11.3: Immutable Per-Handler Logto Policy

**Files:**
- Create: `pkg/server/safe/logto/authmiddleware_test.go`
- Modify: `pkg/server/safe/logto/authmiddleware.go`
- Modify: `pkg/server/trans/rest/server.go`

- [ ] **Step 1: Write failing concurrent policy tests**

Introduce an immutable policy type and make middleware validation use it rather than package globals:

```go
type AuthConfig struct {
	Issuer           string
	ExpectedAudience string
}

func AuthMiddleware(jwks *keyfunc.JWKS, next http.Handler, cfg AuthConfig) http.Handler
func NewAuthHandler(next http.HandlerFunc, cfg AuthConfig) (http.Handler, error)
```

Use two local JWKS test servers and run handlers concurrently with distinct issuer/audience pairs. Each matching token must succeed only against its own handler under `go test -race`.

- [ ] **Step 2: Verify RED**

Run: `go test -race ./pkg/server/safe/logto -run TestAuthHandlersKeepIndependentPolicy -count=1`

Expected: FAIL because policy currently lives in mutable package globals.

- [ ] **Step 3: Implement local policy and startup errors**

Normalize issuer trailing slashes once, derive `jwksURL` and accepted issuer from the local config, and return JWKS initialization errors instead of calling `log.Fatal`. Update REST route registration to construct each Logto handler once and propagate construction errors to `NewServer`.

- [ ] **Step 4: Preserve compatibility deliberately**

Keep the old exported `AuthHandler(next, issuer, audience) http.Handler` only as a deprecated wrapper if repository consumers require it. The wrapper must never terminate the process; internal startup code must use `NewAuthHandler` and handle its error.

- [ ] **Step 5: Verify GREEN**

Run:

```bash
go test -race ./pkg/server/safe/logto ./pkg/server/trans/rest -count=1
go test ./pkg/server/... -count=1
```

- [ ] **Step 6: Commit**

```bash
git add pkg/server/safe/logto pkg/server/trans/rest/server.go
git commit -m "fix: isolate Logto authentication policy"
```

## Task 11.4: Generic Authentication and Framework Error Responses

**Files:**
- Modify: `pkg/server/safe/logto/authmiddleware_test.go`
- Create: `pkg/server/trans/rest/error_security_test.go`
- Modify: `pkg/server/safe/logto/authmiddleware.go`
- Modify: `pkg/server/trans/rest/error.go`

- [ ] **Step 1: Write failing disclosure tests**

Assert malformed, expired, wrong-audience, wrong-issuer, JWKS-refresh, whitelist, and internal errors preserve their intended HTTP status but do not include token parser text, expected claims, issuer URLs, stack/error strings, or supplied secret fixtures in the response body.

- [ ] **Step 2: Verify RED**

Run: `go test ./pkg/server/safe/logto ./pkg/server/trans/rest -run 'TestAuthResponseDoesNotDiscloseCause|TestWriteErrorResponseDoesNotDiscloseCause' -count=1`

Expected: FAIL because current handlers return `err.Error()` and expected claim values.

- [ ] **Step 3: Implement safe public messages**

Return stable generic messages such as `authentication failed` and omit `ErrorDetail.Details["error"]` from framework-generated responses. Wrap and return internal causes to the owning boundary; use structured `logx` only where Task 11 owns a terminal authentication decision, without tokens or claims.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./pkg/server/safe/logto ./pkg/server/trans/rest -count=1`

- [ ] **Step 5: Commit**

```bash
git add pkg/server/safe/logto pkg/server/trans/rest/error.go pkg/server/trans/rest/error_security_test.go
git commit -m "fix: hide authentication error details"
```

## Task 11.5: Trusted Proxy Client IP Resolution

**Files:**
- Create: `pkg/utils/ip_test.go`
- Modify: `pkg/utils/ip.go`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/router/request.go`
- Modify: `pkg/server/trans/rest/server.go`
- Modify: `pkg/server/run/htmlserver.go`

- [ ] **Step 1: Write failing trust tests**

Define `ClientPublicIP(r *http.Request, trustedProxies ...string) string`. Test that forwarding headers are ignored for an untrusted `RemoteAddr`, honored for a trusted exact IP or CIDR, the first valid forwarded IP is selected, malformed entries are skipped, and direct IPv4/IPv6 addresses are returned correctly.

- [ ] **Step 2: Verify RED**

Run: `go test ./pkg/utils -run TestClientPublicIP -count=1`

Expected: FAIL because forwarding headers are currently trusted unconditionally.

- [ ] **Step 3: Implement netip-based trust evaluation**

Parse `RemoteAddr` with `net.SplitHostPort` and `netip.ParseAddr`; parse each configured proxy as an address or prefix. Only inspect `X-Forwarded-For` and `X-Real-IP` when the direct peer matches a configured trusted proxy. With no trusted proxy configuration, return the direct peer.

- [ ] **Step 4: Add and validate configuration**

Add `TrustedProxies []string` to `ServerConfig`, default it to an empty slice, validate every value as an IP or CIDR, and pass it at each request/whitelist boundary. Invalid proxy configuration must fail before serving traffic.

- [ ] **Step 5: Verify GREEN and race safety**

Run:

```bash
go test ./pkg/utils ./pkg/server/config ./pkg/server/router ./pkg/server/trans/rest ./pkg/server/run -count=1
go test -race ./pkg/utils ./pkg/server/router -count=1
```

- [ ] **Step 6: Commit**

```bash
git add pkg/utils/ip.go pkg/utils/ip_test.go pkg/server/config/serverconfig.go pkg/server/router/request.go pkg/server/trans/rest/server.go pkg/server/run/htmlserver.go
git commit -m "fix: trust forwarding headers only from configured proxies"
```

## Task 11.6: Security Headers and Existing go-zero Limits

**Files:**
- Modify: `pkg/server/trans/rest/server_security_test.go`
- Modify: `pkg/server/trans/rest/server.go`
- Modify: `docs/codex/plans/11-security-auth-isolation.md` to record final capability evidence for Task 14

- [ ] **Step 1: Write failing header and limit tests**

Test a package-private middleware that adds `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and `X-Frame-Options: DENY` without overwriting stricter caller values. Add a configuration test proving go-zero `RestConf.MaxBytes`, `MaxConns`, and their middleware flags remain enabled after `ServerConfig.ApplyDefaults`.

- [ ] **Step 2: Verify RED**

Run: `go test ./pkg/server/trans/rest ./pkg/server/config -run 'TestSecurityHeaders|TestServerConfigPreservesGoZeroLimits' -count=1`

Expected: header test FAILS because the middleware does not exist; limit test records the current go-zero behavior.

- [ ] **Step 3: Add the narrow header middleware**

Wrap registered HTTP routes with the security-header middleware. Do not add HSTS until TLS termination ownership is explicit, and do not duplicate go-zero body/connection limits.

- [ ] **Step 4: Record rate-limit support honestly**

Record service-global max connections, body limits, breaker, and shedding as `Stable` in this plan's completion evidence. Record distributed auth/API rate limiting as `Unsupported` until Task 14 approves a go-zero `core/limit` Redis-backed configuration and behavior test; reject any future rate-limit config that lacks a runtime provider. Task 14 must copy this evidence into its capability matrix.

- [ ] **Step 5: Verify Task 11 as a whole**

Run:

```bash
go vet ./pkg/server/... ./pkg/utils
go test ./pkg/server/... ./pkg/utils -count=1
go test -race ./pkg/server/safe/logto ./pkg/server/router ./pkg/utils -count=1
```

- [ ] **Step 6: Commit**

```bash
git add pkg/server/trans/rest pkg/server/config docs/codex/plans/11-security-auth-isolation.md
git commit -m "fix: establish HTTP security baseline"
```

## Completion Evidence

Task 11 is complete only when all six commits are recorded in the master plan, the full Task 11 verification commands pass, no authentication policy globals or `log.Fatal` remain, CORS cannot silently become wildcard, forwarded headers require trusted proxies, config files are `0600`, and public authentication/framework error bodies contain no internal causes.
