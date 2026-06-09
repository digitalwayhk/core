# ADR-001: Platform Architecture Optimization

**Status:** Proposed  
**Date:** 2026-06-08  
**Deciders:** Vincent / core-codex team  

---

## Context

`github.com/digitalwayhk/core` 是一个商业级 Go 服务框架，目标是标准化业务开发过程，
实现业务逻辑与技术实现分离。

当前框架处于从单机模式向水平可扩展平台演进的关键阶段。已完成了 Phase 3–6 的代码
（Cluster / Transport / MQ 子系统），但存在一个核心问题：**新子系统没有接入服务启动链路**，
导致 `ServiceContext` 始终以单机模式运行，无法利用已实现的能力。

本 ADR 聚焦五个优化维度的架构决策：**服务分层、存储策略、API 设计、并发性能、水平扩展**。

---

## 当前状态（Gap 分析）

### 已实现（代码存在）

| 模块 | 代码位置 | 状态 |
|------|---------|------|
| Cluster provider (local/etcd/consul) | `pkg/server/cluster/` | ✅ 实现，有测试 |
| Transport selector (grpc/http/socket) | `pkg/server/transport/` | ✅ 实现，有测试 |
| MQ manager + provider (redis/nats) | `pkg/server/mq/` | ✅ 实现，有测试 |
| WebSocket 全局通知系统 | `pkg/server/types/` | ✅ 实现（20 workers, 10K 缓冲） |
| OLTP/NoSQL/OLAP 多存储适配 | `pkg/persistence/` | ✅ 实现 |
| Cluster/Transport/MQ 结构化配置 | `pkg/server/config/` | ✅ 实现 |

### 关键 Gap（P0）

**`ServiceContext` 启动链路未接入新子系统。** 具体：

- `ServiceContext.ClusterProvider` 定义了字段，但启动时不根据配置创建 provider，始终为 nil
- `ServiceContext.membership` 未创建，节点不会注册、心跳、优雅下线
- `ServiceContext.TransportSelector` 不根据配置创建，`sendPayload` 仍 fallback 到旧 HTTP
- `MQManager` 不根据 `ServerConfig.MQ` 初始化，event-stream 和内部传输不可用
- `CrossNodeNoticeBroker` 依赖 `ClusterProvider != nil` 才启动，故跨节点 WebSocket 通知永远不启用

---

## Decision

### D1：服务启动链路接入（最高优先级）

**决策：** 在 `ServiceContext` 初始化阶段根据配置驱动所有子系统的生命周期。

启动顺序：

```
ReadConfig
  → ApplyDefaults / Validate
  → Cluster.Mode 判断
      off  → 跳过 cluster，保持现有单机行为
      auto → 创建 local provider；etcd/consul 配置存在且连通则切换，失败降级并记录
      on   → 强制创建配置 provider，失败则启动失败
  → cluster.Claim(ServiceName, DataCenterID, MachineID)   ← Snowflake 初始化之前
  → 初始化 Snowflake
  → 初始化 routes
  → MQ.Mode 判断（同 off/auto/on 逻辑）
  → 创建 TransportSelector（grpc/http/socket/quic/mq 按配置）
  → 注册服务节点 + 启动 heartbeat
  → 启动 transports（HTTP/gRPC/socket）
  → 若 ClusterProvider != nil，启动 CrossNodeNoticeBroker
```

**关键约束：**
- `cluster.Claim` 必须在 Snowflake 初始化之前，保证多副本 MachineID 不冲突
- `Mode=on` 时任何子系统失败都是启动失败，不允许降级

---

### D2：服务分层架构

**决策：** 确认三层分层模式，并通过接口隔离保证可替换性。

```
框架层      github.com/digitalwayhk/core
  ↓
项目共享层  internal/pkg/{models,api,services}
  ↓
服务专属层  internal/core/{serviceName}/{models,api,service}
```

**理由：** 业务开发时只修改最下层，框架升级只影响最上层接入点。
当前框架仓库本身已遵循此原则（`pkg/` 和 `service/` 分离）。

**未解决问题（需 P2 跟进）：**
- `pkg/dec/eventbus` 是进程内事件总线，不建议继续扩展为核心能力
  → **决策：** 新事件能力走 `pkg/server/event`，使用 CloudEvents 兼容信封，通过 MQ provider 实现

---

### D3：存储策略

**决策：** 维持"业务只决定保存时机，技术层决定保存方式"原则。

| 层 | 存储方案 | 用途 |
|----|---------|------|
| OLTP | SQLite（开发）/ MySQL（生产） | 业务实体，自动 AutoMigrate |
| OLAP | ClickHouse / StarRocks | 分析查询（已有实现，按需启用） |
| NoSQL | BadgerDB / BoltDB | 本地轻量 KV，配置/缓存 |
| Remote KV | Redis | MQ/事件流/缓存，不作为服务注册发现 |
| Document | MongoDB | 文档型业务数据 |

**关键决策：Redis 只用于 MQ/缓存，不用于 cluster 服务注册发现。**  
服务注册发现 provider 只能选择 `local`（自研）、`etcd`、`consul`。

**现有已知问题（需 P1 修复）：**
- `entity.modellist.go` 和 `adapter/default.go` 都维护了独立的 `globalSqliteInstances` map，
  存在重复全局状态 → 需合并到单一入口

---

### D4：API 设计规范

**决策：** 维持现有三类 API 路由语义，补强以下约束：

| API 类型 | 路径模式 | 鉴权 | 备注 |
|---------|---------|------|------|
| Public | `/api/{svc}/{structNameLower}` | 无 | 目录名不出现在路径中 |
| Private | `/api/{svc}/{structNameLower}` | JWT Bearer | 同上 |
| Manage | `/api/manage/{svc}/{ctrl}/{op}` | JWT(type=1) | OpenAPI 不暴露 |

**新增要求：**
1. 每个新增 API 必须在 `api/release/routers.go` 注册，框架不做自动扫描
2. `IRouterResponse.GetResponse()` 作为 OpenAPI 响应 schema 的唯一来源，不再依赖反射推断
3. Manage API 路由不进入 OpenAPI 文档；`ViewModel` 由前端 WayPlus 独立消费

**OpenAPI 增强（P2）：** 支持 `example`、`required`、`format` tag；支持 `/api/{svc}/openapi.json` 路径。

---

### D5：并发与性能

**决策：** 分层处理并发瓶颈。

#### WebSocket 通知系统

当前实现（`WebSocketNotificationSystem`）：
- 全局单例，20 workers，10K 缓冲队列
- 已有原子操作防重复启动
- 每 5 分钟重置 droppedJobs 统计

**已知风险：**  
`IRouterHashKey` 的 hash 分组仅在单节点有效。水平扩展后，客户端在 B 节点，
事件发生在 A 节点，当前跨节点通知路径未打通（依赖 CrossNodeNoticeBroker 启动）。

**决策：** 跨节点 WebSocket 通知路径：

```
NoticeWebSocket(message)
  → 本节点：routePath + hash → 本地 clients → NoticeFiltersRouter(message, api) → push
  → 其他节点：向 event-stream 发布 WebSocketNoticeEvent（含 routePath + hash + message）
  → 目标节点：收到 event → 本地相同 routePath + hash 订阅组 → NoticeFiltersRouter → push
```

传输层优先级：MQ（若健康）→ gRPC stream → local/P2P event stream。

#### 服务分片

对有状态服务（资金、订单、账户）启用分片路由，减少跨节点锁：

```
CallService
  → ClusterRegistry.Resolve(serviceName)
  → ShardRouter.Filter(nodes, payload.shardKey)   // hash/group/exact
  → LoadBalancer.Pick(filteredNodes)              // round-robin/consistent-hash/local-first
  → Transport.Send(payload)
```

`required=true` 的 shard key 缺失时直接返回错误，不允许静默随机选择。

---

### D6：水平扩展

**决策：** 分三步实现水平扩展能力，默认不依赖外部服务。

**Step 1（当前阻塞，P0）：** 接入启动链路（见 D1）。

**Step 2（P1）：**
- 修复 `LocalProvider` MachineID slot 冲突未按 `ServiceName` 隔离的问题
  （当前 `funds` 和 `orders` 不能同时使用相同的 `DataCenterID+MachineID`）
- 修复 `TransportConfig` 配置 `quic`/`mq` 时 `BuildSelector` 静默跳过的问题

**Step 3（P2）：** 完成 etcd/consul provider 集成测试；provider 动态切换 + 回滚。

**MachineID 自动扩展规则（已设计，待实现）：**

```
同 ServiceName+DataCenterID+MachineID 存在且活跃 → 自动递增 MachineID
DataCenterID 下 MachineID 耗尽 → 尝试下一个 DataCenterID
仍失败 → 启动失败，给出明确错误
```

冷却期：MachineID 进入 offline 后 30s 内不允许复用，防止 Snowflake ID 空间冲突。

---

## Options Considered

### Option A：维持现状（只修复 bug，不重构启动链路）
**Pros:** 零风险，不影响现有单机用户  
**Cons:** 新实现的 Cluster/Transport/MQ 代码成为死代码，浪费已有投资；水平扩展永远无法启用

### Option B：接入启动链路 + 向下兼容（本 ADR 选择）
**Pros:** 保留所有已有单机用户的零配置体验；Cluster.Mode=off 等于现状；新能力通过配置渐进启用  
**Cons:** 启动链路改动较大，需要全面测试

### Option C：重写 ServiceContext
**Pros:** 更干净  
**Cons:** 破坏向下兼容；风险过高；当前代码质量可以渐进演进而不必重写

---

## Trade-off Analysis

| 维度 | 现状 | 目标 |
|------|------|------|
| 单机启动 | 零配置，稳定 | **保持不变**（Mode=off/auto 降级） |
| 水平扩展 | 无（代码存在但不生效） | 配置驱动，默认 local provider |
| 内部传输 | HTTP only | grpc（默认）> http > socket > quic > mq |
| 事件总线 | 进程内 eventbus（legacy） | event-stream，MQ provider 驱动 |
| WebSocket 跨节点 | 不可用 | 通过 event-stream 转发 |
| MachineID | 手动配置，容器扩展易冲突 | 自动认领，冷却回收 |

---

## Consequences

**变得更容易：**
- Docker `docker-compose scale` 直接扩容，无需手动修改 MachineID
- 内部服务调用切换协议不改业务代码
- WebSocket 推送在多节点集群中对用户透明

**变得更难：**
- 启动链路增加了更多初始化步骤，首次调试复杂度上升
- `Mode=on` 要求外部 provider 可用，增加部署依赖
- 需要完整的集成测试覆盖 Cluster / Transport / MQ 初始化路径

**需要后续跟进：**
- `pkg/dec/eventbus` 迁移到 `legacy/` 或废弃文档
- `entity.modellist.go` 和 `adapter/default.go` 的重复全局 SQLite map 合并
- WebSocket 订阅摘要同步协议的正式定义

---

## Action Items

### P0（当前阻塞，优先处理）

- [ ] **接入 Cluster 启动链路** — `ServiceContext` 根据 `Cluster.Mode` 创建 provider，启动 membership
- [ ] **接入 Transport 启动链路** — 根据 `Transport` 配置构建 `TransportSelector`
- [ ] **接入 MQ 启动链路** — 根据 `MQ` 配置初始化 `MQManager`，注册到 event-stream
- [ ] **修复 CrossNodeNoticeBroker** — provider 初始化成功后自动启动，不需要外部手动触发

### P1（次优先）

- [ ] **修复 LocalProvider MachineID 隔离** — slot 冲突检查必须限定在同一 ServiceName 域内
- [ ] **修复 TransportConfig quic/mq 静默跳过** — 配置了但未实现的 transport 应返回明确错误
- [ ] **合并重复 globalSqliteInstances** — `entity/modellist.go` 和 `adapter/default.go` 统一入口

### P2（计划内）

- [ ] **OpenAPI 增强** — `IRouterResponse`、`example`/`required`/`format` tag、swagger 路径
- [ ] **Manage 状态机接口** — `Submit`/`Release` 引入状态机，减少 `DoBefore` 类型强转
- [ ] **event-stream 抽象** — 新增 `pkg/server/event`，CloudEvents 兼容信封
- [ ] **集成测试** — Cluster local/etcd、MQ switch、WebSocket 跨节点、配置兼容

### P3（长期）

- [ ] **暂停模块标记** — `pkg/dec`、`pkg/fileserver`、`pkg/localization` 迁入 `legacy/`
- [ ] **QUIC transport** — 完整实现后加入 `BuildSelector`
- [ ] **MQ 动态切换** — 实现 dual-write → 切读 → 回滚完整链路

---

## 参考文档

- `OPTIMIZATION_PLAN.md` — 完整优化规划，包含所有接口设计和配置结构
- `COPILOT_REVIEW_BUGS.md` / `ROUND2` / `ROUND3` — 当前已知 bug 清单
- `pkg/server/cluster/` — Cluster 子系统实现
- `pkg/server/transport/` — Transport 子系统实现  
- `pkg/server/mq/` — MQ 子系统实现
- `pkg/server/router/servicecontext.go` — 需要修改的启动链路入口
