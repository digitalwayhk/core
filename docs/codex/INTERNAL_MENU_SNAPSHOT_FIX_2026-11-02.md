# 跨进程菜单发现修复

## 问题与范围

Full-E4 菜单搜索空表初始化和 UpdateMenu 都通过 gRPC 调用目标服务的
`/api/servermanage/queryrouters`；入站处理器禁止普通服务访问 ServerManage，
因此在 QueryRouters.Validation 前失败。原测试直接调用 Validation，遗漏入站边界。

## 修复合同

- 使用独立的内部传输操作 `/internal/core/menu-snapshot`，不注册 HTTP 路由。
- 仅接受框架认证的可信调用方，且 SourceService 必须与认证身份一致；目标必须为当前服务。
- 仅返回当前服务已注册的 Manage 菜单与报表标题/路径快照，不读取业务数据、配置或令牌。
- 保持所有既有 ServerManage 拒绝及两个精确 WS 中继边界。
- 聚合端继续逐副本查询、校验归属与一致性，任一节点失败不落库。
- 不增加配置，不改变数据库或持久化格式；混合旧节点时明确失败，全部被发现节点完成升级后恢复菜单。

## 验证计划

1. 入站处理器 RED → GREEN，覆盖可信、自报/伪造身份、错误目标与管理路径隔离。
2. 真实独立进程 mTLS 查询，覆盖无证书、SAN/Source 不一致、HTTP 不暴露、重启恢复。
3. 菜单存储/搜索/同步、router、gRPC 普通与 race，正式发布门禁。
4. 正式版本被消费方锁定后，验证 Full-E4 两个 Exchange 与所有被发现服务，再做真实账号页面验收。

## 验证证据

基线：`c89370bcd3c5b600eab46c557307f0a5574bface`（v1.7.0）。

- RED：新增 `TestInternalMenuSnapshotInboundBoundary` 在未修复代码中失败，可信只读请求无法被处理；原ServerManage拒绝仍通过。
- GREEN：入站、旧QueryRouters菜单兼容及调用元数据共8项定向测试通过。
- PASS：`go test ./pkg/server/router ./pkg/server/api/public ./pkg/server/api/manage ./pkg/server/transport/grpc -count=1`，419项。
- PASS：上述4包完整 `-race -count=1 -timeout=4m`。
- PASS：独占临时Redis容器，`CORE_TEST_REDIS_ADDR=127.0.0.1:55002 go test -race ./examples/integration/menu-snapshot -count=1 -timeout=4m`，60.653秒。两个真实服务进程、mTLS、空表搜索自动初始化、UpdateMenu、gRPC出入站计数增长且HTTP/fallback为零、SQLite保存及重启读取；覆盖无证书、SAN/Source不一致、HTTP404和ServerManage拒绝。
- 保留夹具失败：首次真实网络测试误把按服务名字典序分配的gRPC端口当作HTTP顺序，连接了另一个未启用TLS的listener。修正夹具端口后通过；该次失败不计为产品RED。
- PASS：独立安全审查未发现阻断问题；已核查证书链、Source/Target、快照服务归属、HTTP不注册和WS隔离。
- PASS：`scripts/test.sh release-contract`，含API兼容、公共Go API、配置和安全检查。

## 当前状态

Core源码与上述验证完成。正式tag/push、Full-E4消费版本和实际账号浏览器验收尚未完成，不能据此宣称线上菜单恢复。
