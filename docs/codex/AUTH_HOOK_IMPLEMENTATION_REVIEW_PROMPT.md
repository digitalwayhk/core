# AuthHook 与 Public API 限流整项实现外部审查提示词

请对 AuthHook、双令牌、Casdoor 身份交换和系统 Public API 本地限流进行一次**只读终审**，不要修改代码、文档、Git 索引或提交历史。

## 审查范围

```bash
cd /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex
git diff 4d736da..85560f9
```

- 基线：`4d736da`
- 审查目标：`85560f9`
- 设计规格：`docs/copilot/AUTH_HOOK_DESIGN.md`
- 实施计划与提交证据：`docs/superpowers/plans/2026-07-15-auth-hook-public-rate-limit.md`
- 不要把范围外工作区脏文件算作本实现缺陷；若它们影响测试，请明确标注“范围外环境影响”。

## 必查契约

1. `IAuthHookProvider` 是否由 `ServiceContext` 自动发现并持有；Hook 是否只在 Callback、Refresh、TestToken 的签名前调用一次。
2. Hook 是否至少收到可信 `uid`、认证类型、来源、签发和过期时间；Hook 拒绝时是否停止签名且不泄露内部原因。
3. Access Token 是否包含 `token_use=access`、`auth_type` 和 Hook 注入字段；Refresh Token 是否仅保留刷新白名单 Claims，不携带 Hook 自定义 Claims。
4. auth/manage 是否分别使用对应的 AccessSecret、RefreshSecret 和过期时间；servermanage 是否只颁发 Access Token。
5. Refresh Token 是否严格限制 HS256、用途、认证类型、uid、iat、exp，并拒绝错误密钥、未来签发时间、Access Token 冒充和 auth/manage 混用。
6. 历史配置迁移是否只生成一次 RefreshSecret、写回原配置、权限为 `0600`，且不会因已有其他迁移字段而跳过。
7. Casdoor 是否只负责外部身份交换；交换后 Private/Manage 是否必须使用框架内置 JWT，旧的 `context["user"]` 绕过路径是否彻底移除。
8. TestToken 是否继续受 `ServerArgs` 的本机或 `RemoteAccessManageAPI` 访问控制约束，且明确不执行限流；它是否仍调用 AuthHook 并携带默认 uid、签发和过期参数。
9. `IpWhiteList` 是否保留 `ServerArgs` 访问控制，不能因为新增限流而变成任意外部可调用。
10. RouterInfo 限流元数据是否只在注册期配置并冻结；限流器是否由每个 `ServiceContext` 独立持有和关闭，不是进程级可变单例。
11. 限流键是否至少隔离服务、路由和可信客户端 IP；无法安全解析 IP 时是否进入 `unknown` 桶而不是绕过。
12. 只有无转发头的真实 loopback 直连可以跳过限流；带 XFF/X-Real-IP 的 loopback 反代请求不得被当成本机直连。
13. REST 中间件顺序是否为安全响应头覆盖限流响应，外部限流发生在昂贵认证之前，认证仍发生在业务 RouteHandler 之前。
14. 429 是否返回稳定的类型化公开错误、保留安全响应头，并只记录脱敏 IP；L2/L3 写入、认证失败和服务关闭后的 fail-closed 语义是否被削弱。
15. 系统 Public API 的默认额度是否合理、是否遗漏可外部调用的路由；TestToken 是否确实没有限流策略。
16. 真实示例是否证明 TestToken 结构化响应、AuthHook Access Claim、Refresh Claim 隔离；集成 helper 是否兼容旧字符串响应且不持有业务 DTO。
17. 检查新增公共 Go API、JSON 字段和配置字段的兼容性；确认发布契约基线没有被实现代码擅自更新。
18. 全局搜索是否仍存在直接签发旧 JWT、Casdoor 原始身份直通、绕开 Hook 的 Callback/TestToken/Refresh 入口，或其他直接创建进程级限流器的路径。

## 建议验证命令

```bash
bash -n scripts/test.sh scripts/release-check.sh

GOCACHE=/private/tmp/core-codex-review-cache \
  go test ./pkg/server/safe ./pkg/server/config ./pkg/server/router \
  ./pkg/server/api/public ./pkg/server/ratelimit ./pkg/server/trans/rest -count=1

GOCACHE=/private/tmp/core-codex-review-cache \
  go test -race ./pkg/server/safe ./pkg/server/router ./pkg/server/api/public \
  ./pkg/server/ratelimit ./pkg/server/trans/rest -count=1

GOCACHE=/private/tmp/core-codex-review-cache \
  go test -race ./examples/integration/01-simple-shop -count=1 -timeout=15m

GOCACHE=/private/tmp/core-codex-review-cache \
  go vet ./pkg/server/... ./examples/integration/...

./scripts/check-logging.sh
GOCACHE=/private/tmp/core-codex-review-cache ./scripts/test.sh release-contract
```

涉及 `httptest`、REST 生命周期和真实示例进程的测试需要允许监听本地端口。不得因沙箱禁止 bind 而把代码裁定为失败，也不得跳过后宣称通过。

## 必须返回的反馈

1. `Findings`：按 P0、P1、P2 排序；每项给出文件和行号、确定触发场景、实际影响、修复建议。
2. 对上面 18 条契约逐项给出“满足 / 不满足 / 证据不足”，不要只给概括结论。
3. 单独说明 Hook 调用时序、Access/Refresh 密钥与用途隔离、Casdoor 绕过关闭情况。
4. 单独说明 TestToken 与 IpWhiteList ACL、限流的服务/路由/IP/生命周期隔离、429 安全响应。
5. 列出实际执行的命令、退出码和关键结果；区分代码失败、沙箱限制与范围外脏树影响。
6. 给出测试真实性评价和仍缺失的确定性测试，不接受仅靠 `time.Sleep` 或弱断言刷绿。
7. 给出公共 API、配置、HTTP/JSON 和运行时行为的兼容性评估。
8. 最终裁定只能是 `APPROVED` 或 `CHANGES_REQUIRED`。
9. 明确回答：是否允许关闭本计划；若不允许，列出必须先关闭的 P0/P1。

