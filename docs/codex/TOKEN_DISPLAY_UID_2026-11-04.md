# 普通用户 JWT 展示 UID

## 契约

业务的短编号只由业务权威分配。`OnAuth` 使用 `args.Claims.(types.ITokenUIDMutator).SetTokenUID(publicID)` 设置 JWT `uid`。原 `Claims.Uid` 与 `AuthHookArgs.UID/Identity.UID` 不变，原 `IClaimsMutator` 接口不变。

当短编号与认证 UID 不同，Access 和 Refresh 都携带保留的 `auth_identity_uid`。验签后的 `UID/Identity.UID`、HTTP `req.GetUser()`、Casdoor 刷新在线核验、撤销键、WebSocket 身份和秘密 Claim 加解密继续使用认证 UID；原始 `Claims["uid"]` 是短编号。刷新重新执行 Hook，按业务权威重新取得短编号。

未设置展示编号时签发格式不变。旧令牌可以验证并通过刷新升级。非普通用户域不能设置展示编号；空值、空白、超长值、控制字符、普通/秘密 Claim 注入保留字段均拒绝。旧 `GetToken` 不能承载此能力，设置后必须通过 `IssueTokenPair` 签发。

## 升级和回退

先升级所有验证端，再启用业务签发 Hook。旧版本不识别新保留字段，禁止新签发端与旧验证端混用；回退须先停止新格式签发并处置新格式会话。无需修改 Casdoor 原始 ID、业务主键或撤销存储，不迁移用户数据。

## 验证记录

- RED：`go test ./pkg/server/safe -run '^TestTokenDisplayUIDPreservesVerifiedIdentity$' -count=1` 因缺少展示 UID 入口失败。
- GREEN：`go test ./pkg/server/safe ./pkg/server/api/public -count=1`，150 项通过（含原有用例）。
- PASS：`go test -race ./pkg/server/safe ./pkg/server/api/public ./pkg/server/router -count=1 -timeout=3m`，327 项通过。
- PASS：`go test -race ./examples/integration/casdoor-auth-lifecycle -run 'TestCasdoor(AuthLifecycleREST|WebhookClosesOldWebSocketAndAllowsRelogin|DisplayUIDRESTWebSocketAndLogout)$' -count=1 -timeout=2m`，3 项通过。使用临时本地 Badger 与 fake Casdoor，真实 HTTP/WS 端口；不访问远端环境。
- 真实集成首次发现 `req.GetUser()` 仍读取展示值；已修正原始身份映射，保持原始 Claims 的 `uid` 为展示值。同一真实集成重新通过，未放宽断言。
- PASS：`./scripts/test.sh security`、`./scripts/test.sh config-contract`、`./scripts/test.sh api-compat`、`./scripts/test.sh public-api`、`./scripts/check-logging.sh`。
- PASS：`./scripts/release-check.sh --candidate`；脚本明确未创建 tag、未 push、未发布。API 基线由 `./scripts/update-public-api.sh` 显式更新，兼容检查通过。
- PASS：独立只读安全审查未发现安全缺陷；确认旧框架缺少此能力，不能在消费方通过覆盖保留字段解决。
- NOT_RUN：shared Redis 故障/跨副本压测、消费方已发布新版本下的运行验收、线上部署；候选测试不代表已发布或部署。

## 消费方接入状态

Bitzoom `fix/token-public-user-id-20261104` 已补建户与刷新快照的 `PublicUserID` 传递及非法值/框架不支持的公开拒绝。当前 main 仍锁 Core v1.6.0；新的签名回归在该正式旧版上 RED，不能合入 main 或性能分支。发布负责人按 `docs/RELEASE_POLICY.md` 发布包含本候选的 MINOR 版本后，消费方才能更新 `go.mod` 并进行签名及真实运行验证；没有使用 go.work、replace、vendor 或模块缓存修改绕过。
