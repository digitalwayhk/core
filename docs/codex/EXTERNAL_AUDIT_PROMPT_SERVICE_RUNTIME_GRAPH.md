# 多服务运行图（Service Runtime Graph）外部全面审计提示词

请对 `github.com/digitalwayhk/core` 的 **`core-web-admin` 分支**做一次外部全面代码审计。审计必须以当前代码与测试为准，不要只根据 README、计划文档或“测试全绿”下结论。

## 1. 审计范围

- 分支：`core-web-admin`（主仓 `digitalwayhk/core`）
- 前端子模块：`web/admin` 的 `core-web-admin` 分支（`bitzoom-futures/futures.admin`）
- 对比基线：优先对比 `origin/main`（或合并前基线提交 `a0785b7` 一带），重点审阅本功能相关提交：
  - `7a7f43b` feat(runtime): add service graph observability backend MVP
  - `c6e9b34` feat(examples/07): wire prometheus scrape and admin runtime graph
  - `1ff335e` chore(submodule): pin web/admin to core-web-admin branch
  - 以及本轮收尾提交（废弃登记、调用边解析、Pending Provider 等）
- 前端提交：`c10933e` feat(admin): render service runtime graph from Runtime API
- 核心目录：
  - `pkg/server/observability`
  - `pkg/server/runtime`
  - `pkg/server/api/public/runtimetopology.go`
  - `pkg/server/api/release/routes.go`
  - `pkg/server/transport/grpc`（server metrics / Call 入站）
  - `pkg/server/router/servicecontext.go`（CallService 调用边、Aggregator 装配、Provider 注册）
  - `pkg/server/config/runtime_observability.go` 与能力矩阵
  - `pkg/server/types/routerstats.go`、`statsmanager.go`（废弃面）
  - `examples/07-shop-order-scale/deploy`、`bootstrap`
  - `examples/integration/07-shop-order-scale-multi-process`
  - `web/admin/src/pages/MonitorSystem`、`web/admin/src/services/runtime.ts`
  - `docs/superpowers/specs/2026-07-27-service-runtime-graph-design.md`
  - `docs/superpowers/plans/2026-07-27-service-runtime-graph.md`
  - `docs/codex/DEPRECATION_REGISTER.md`、`API_COMPATIBILITY_SURFACE.md`、`CHANGELOG.md`

## 2. 背景与目标

本轮目标是在 **不恢复旧 RouterStats** 的前提下，建设可用的多服务运行图：

1. 复用 go-zero Prometheus / zrpc 观测能力；补齐 Core gRPC Server、服务调用边与可靠写组件低基数指标。
2. **Prometheus 是唯一历史指标源**；Runtime Aggregator 只查询合并，不成为第二套内存统计。
3. 全局服务图 → 服务请求视图；MySQL/Pending/Outbox 等只进入服务内部视图。
4. 用示例 07 验证多副本、Pending、MySQL、Outbox、EventBridge 相关可观测性接入。
5. 未采集 / 过期 / 不可用必须 `null + state` 明确表达，**不得伪装为零**。
6. 旧 `RouterStats` 进入废弃登记；删除必须另开批准的破坏性版本。

请判断实现是否真正达成以上目标，并指出安全、正确性、可运维性、兼容性、测试与文档缺口。

## 3. 必查设计契约

不符合时给出：**证据路径 + 影响 + 修复建议**。

1. 浏览器只访问 ServerManage 保护的 Runtime API；不得暴露 Prometheus 地址、实例内部 `/metrics`、mTLS 材料或任意 PromQL。
2. PromQL 必须由服务端白名单模板生成；服务名/窗口必须校验，禁止字符串拼接用户输入。
3. 指标标签低基数：路由模板、稳定服务名、`result_class` 闭集；禁止用户 ID、订单 ID、TraceID、原始 URL、SQL。
4. `Mode=off` → `not_collected`；`Mode=prometheus` 查询失败 → `unavailable`；有样本但过期 → `stale`；部分缺失 → `partial`。
5. 数值 `0` 只表示权威采集结果确实为零；`null` 必须配合 state。
6. ClusterProvider 是拓扑权威；Prometheus 是指标权威；RouterInfo 是路由元数据权威。
7. `RuntimeMetricProvider` 只在本进程注册 Collector；Aggregator **不得**在 API 请求路径直连各实例 Provider。
8. gRPC 入站不能只聚合 `/CoreTransport/Call`；需要在身份/路由校验后的稳定 `route` 维度。
9. 调用边标签只使用解析后的 source/target/route/protocol，不因观测改变重试/fallback 语义。
10. 异步边只能由“发布事实 + 已注册订阅”生成，禁止猜测消费者节点。
11. 旧 Statistics 路由不得被重新注册；新链路不得 fallback 到 RouterStats。
12. 配置 `RuntimeObservability` 必须进入能力矩阵与 config-contract；QueryURL 不得进入 AdminView。
13. 前端运行图修改必须在 **admin 子模块 `core-web-admin` 分支**，不得混入 `main`/`test`。
14. 示例 07 scrape 必须可复现：`service` + `service_instance_id` 标签；order 多副本合并为逻辑服务。

## 4. 重点审计项

### 4.1 指标与拦截器

- `core_service_call_*` / `core_service_request_*` 标签是否真正低基数。
- CallService 出站是否记录；HandleInternalPayload 入站是否错误记成调用边。
- gRPC Server `Call` 的身份失败是否用 `rejected` + `invalid_route`，避免 payload 污染标签。
- 与 go-zero 指标命名冲突（例如 `rpc_server_*` 全局注册）是否已规避。
- `prometheus.Enabled()` 门闩未打开时是否静默丢指标，运维是否可发现。

### 4.2 Runtime Aggregator / Prom Client

- Instant query 超时、5xx、空向量、标签缺失时的状态语义。
- 同步边是否按 source/target/protocol/result_class 正确聚合错误率。
- 服务详情路由/组件是否正确；组件缺失是否 `not_collected`。
- 缓存是否可能返回跨窗口脏数据；并发查询限制是否存在。
- QueryURL/凭据是否可能进入日志、错误信息或 Admin 配置视图。

### 4.3 API 与认证

- 路径是否符合 ServerRouterInfo：`/api/servermanage/runtimetopology`、`runtimeservice`。
- PathType 是否为 ServerManage；未认证是否拒绝。
- 未知服务名是否拒绝且不进入 PromQL。
- 响应是否可能泄露内部地址、PromQL、token。

### 4.4 示例 07

- `SHOP_METRICS_*`、`SHOP_RUNTIME_PROM_URL` 是否只在入口服务开查询端。
- `prometheus.yml` 双 order 副本标签是否正确。
- compose 依赖是否合理（prometheus 依赖业务服务、业务可不依赖 prometheus 启动）。
- Pending Provider 是否在 store 绑定后注册；关闭生命周期是否安全。
- `TestComposeDefinesPrometheusScrape` / `TestRuntimeGraphUAT` 覆盖是否充分；UAT 是否会在普通 CI 静默 Skip 却宣称完成。

### 4.5 前端 Admin

- 是否只调用 Runtime API；是否仍依赖旧 statistics mock 作为默认路径。
- `formatMetric` / StateBadge 是否保证 null 不显示为 0。
- 时间窗口保持、轮询取消、visibility 暂停是否正确。
- 可访问性：状态是否仅靠颜色区分。

### 4.6 废弃与兼容

- DEPRECATION_REGISTER / API_COMPATIBILITY_SURFACE / CHANGELOG 是否一致。
- 是否存在“临时兼容适配”实际双写旧统计。
- 破坏性删除是否被错误提前执行。

## 5. 建议验证命令

```bash
# 后端核心
GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/observability ./pkg/server/runtime \
  ./pkg/server/api/public ./pkg/server/api/release ./pkg/server/transport/grpc ./pkg/server/router \
  ./pkg/server/config -count=1

GOCACHE=/private/tmp/core-codex-gocache go test -race ./pkg/server/observability ./pkg/server/runtime \
  ./pkg/server/transport/grpc ./pkg/server/router -count=1

./scripts/test.sh config-contract
./scripts/test.sh api-compat
./scripts/test.sh public-api
./scripts/test.sh release-contract

# 07 静态
GOCACHE=/private/tmp/core-codex-gocache go test ./examples/integration/07-shop-order-scale-multi-process \
  -run 'ComposeDefinesPrometheus|DockerScale|DockerOrder' -count=1

# 前端（在 web/admin）
yarn test --watchAll=false --testPathPatterns=formatMetric
```

可选真实 UAT（不得在未运行时宣称完成）：

```bash
SHOP_RUN_RUNTIME_UAT=1 \
SHOP_RUNTIME_API_BASE=http://127.0.0.1:18181 \
SHOP_RUNTIME_TOKEN=... \
go test ./examples/integration/07-shop-order-scale-multi-process -run TestRuntimeGraphUAT -count=1 -v
```

## 6. 输出格式

请按以下结构输出中文审计报告：

1. **总体结论**：通过 / 有条件通过 / 不通过（一句话）
2. **P0 阻断问题**（安全、数据错误、兼容破坏、假完成）
3. **P1 高优先级问题**
4. **P2 改进项**
5. **已验证通过的关键契约**（列证据）
6. **建议的修复顺序与最小补丁范围**
7. **是否建议合并 `core-web-admin` → 目标主干**

每条问题必须包含：

- 严重级别
- 文件路径与符号
- 复现/推理步骤
- 业务影响
- 建议修复

## 7. 明确禁止

- 不要因为单元测试通过就跳过契约审查。
- 不要建议恢复旧 Statistics 或 RouterStats 作为 fallback。
- 不要建议浏览器直连 Prometheus。
- 不要把破坏性删除 RouterStats 与本 MVP 混在同一合并里，除非已完成批准流程与消费方扫描证据。
