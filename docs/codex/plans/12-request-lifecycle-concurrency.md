# 请求隔离、全局状态与生命周期实施计划

> **面向智能体开发者：** 必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans`，按任务逐项实施。每个小节都要先写失败测试，再做最小实现，并在更新状态前由独立测试/审查角色验收。

**目标：** 消除请求对象在共享服务实例之间串扰的风险，为进程级注册表建立唯一同步策略，并把集群成员、跨节点通知和服务器关闭收敛到可取消、可等待、幂等的生命周期边界。

**架构：** 请求状态只通过方法参数或请求上下文传递；进程级注册表由包内 owner 封装并只返回快照；`ServiceContext` 负责自身集群 worker，`WebServer` 负责服务组及业务启动/停止回调；跨节点转发器按服务名注册；Provider 迁移期间以 Watch 驱动全量快照对账。保留现有导出 API 的兼容入口，但禁止兼容入口继续保存请求级可变状态。

**技术栈：** Go 1.26、go-zero v1.10.2、`context`、`sync.RWMutex`、`sync.Once`、`sync.WaitGroup`、`go test -race`。

---

## 范围与约束

- 本任务只处理请求隔离、可变注册表、启动/关闭和 Provider 切换并发正确性。
- 日志全局规范属于任务 8；本任务只修改新增或受影响的生命周期日志。
- 公共 API 的系统化兼容治理属于任务 15；本任务新增 API 时必须保留现有入口并记录迁移方式。
- 不重新实现 go-zero 的 `ServiceGroup`、HTTP Server、MQ 客户端、熔断或限流；框架只负责组装和明确 owner。
- 不以 `time.Sleep` 作为并发正确性的主要断言。异步测试使用 channel、wait group 或带锁查询辅助程序。
- 不在持有内部互斥锁时执行 Provider、网络、磁盘或用户回调。

## 已确认的风险

| 分类 | 当前证据 | 目标归属 |
|------|----------|----------|
| 请求状态 | `ManageService.Req` 被 CRUD 路由反复写入，`MenuManage` 随后读取 | 方法参数 `req` |
| 上下文注册表 | `scontext`、`TestResult` 是无锁全局 map，`GetContexts` 返回原 map | `router` 包内 owner |
| WebServer 状态 | 服务、选项、子服务 map 访问不一致；启动 `once` 为进程全局 | 每个 `WebServer` 实例 |
| 内部服务注册表 | `run.typemap` 无锁读写 | `run` 包内 owner |
| 跨节点转发 | 单个全局 forwarder 会被不同服务互相覆盖 | 按 `ServiceName` 注册 |
| 集群生命周期 | `SetRunState` 可并发改写 worker；heartbeat 无等待退出 | `ServiceContext` 与 `MembershipManager` |
| Provider 迁移 | `Begin` 只复制一次，迁移窗口的节点变化不会对账 | `clusterSwitcher` Watch worker |
| 服务关闭 | 业务 `Stop` 异步触发且不等待；`FiberServer.Stop` 为空 | `WebServer` 有序关闭 |
| 测试同步 | WebSocket 测试直接读取异步回调写入的 slice | 带锁查询辅助程序 |

## 任务 12.1：移除共享请求状态

**优先级：** P0

**状态：** 已在 `60b6e3a` 完成。定向 `-race`、服务端全量回归、API 兼容审查和正确性审查均通过。

**文件：**
- 修改：`service/manage/manageservice.go`
- 修改：`service/manage/view.go` 及 CRUD 路由文件
- 修改：`pkg/server/api/manage/menumanage.go`
- 创建或修改：对应测试文件

- [x] **步骤 1：编写请求隔离失败测试**

并发执行两个带不同 `NewID()` 结果的请求，断言默认菜单项只使用各自请求的 ID，且一个请求不能覆盖另一个。新增请求感知接口，同时证明旧接口仍能兼容回退：

```go
type IGetDefaultItemsWithRequest[T pt.IModel] interface {
    GetDefaultItemsWithRequest(req st.IRequest) []*T
}
```

- [x] **步骤 2：验证 RED**

```bash
go test -race ./service/manage ./pkg/server/api/manage -run 'Test.*RequestIsolation|Test.*DefaultItems' -count=1
```

- [x] **步骤 3：用显式参数替换共享存储**

`SearchAfter` 优先调用请求感知接口，再回退旧 `IGetDefaultItems`。`MenuManage` 将 `req` 显式传入默认项生成和递归更新逻辑。删除框架内部所有 `SetReq` 调用，使生产请求不再写共享字段。

兼容规则：`ManageService.Req`、`SetReq` 和 `IRequestSet` 均为导出 API，本任务保留它们并添加中文 `Deprecated` 注释，以维持源码兼容；框架不再调用 `SetReq`。依赖该副作用的业务属于行为迁移，必须改用 hook 的 `req` 参数或 `GetDefaultItemsWithRequest`。真正删除放到任务 15 或下一个主版本。

- [x] **步骤 4：验证 GREEN 并提交**

```bash
go test -race ./service/manage ./pkg/server/api/manage -count=1
go test ./pkg/server/... -count=1
git add service/manage pkg/server/api/manage
git commit -m "fix: isolate manage requests"
```

## 任务 12.2：封装 ServiceContext 与测试结果注册表

**优先级：** P0

**状态：** 已在 `fc42ae7` 完成。同名初始化、不同名并行、panic 重试、默认序号回收、快照隔离和测试结果并发访问均有回归覆盖。

**文件：**
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/router/servicecontext_test.go`
- 修改：`pkg/server/api/public/openapi.go`
- 修改：`pkg/server/run/openapi.go`

- [x] **步骤 1：编写并发与快照失败测试**

覆盖并发创建/读取不同服务上下文、同名服务只产生一个实例、并发写读 OpenAPI 测试结果，以及修改 `GetContexts()` 返回值不会改变内部注册表。

- [x] **步骤 2：验证 RED**

```bash
go test -race ./pkg/server/router ./pkg/server/api/public ./pkg/server/run -run 'Test.*Context.*Concurrent|Test.*Context.*Snapshot|Test.*Result.*Concurrent' -count=1
```

- [x] **步骤 3：建立唯一同步入口**

以包内 `sync.RWMutex` 保护上下文和测试结果。注册表锁只登记按服务名区分的“初始化中”条目，条目包含 `ready` channel、结果和错误；首个调用者在锁外执行配置 I/O、MachineID claim、MQ 和 Provider 初始化，同名调用者等待 `ready`，不同服务仍可并行。panic 或失败必须清理占位并唤醒等待者。`GetContexts` 返回新 map；新增 `SetTestResult` 与 `GetTestResult`，内部调用方不得直接访问 map。

- [x] **步骤 4：验证 GREEN 并提交**

```bash
go test -race ./pkg/server/router ./pkg/server/api/public ./pkg/server/run -count=1
git add pkg/server/router pkg/server/api/public/openapi.go pkg/server/run/openapi.go
git commit -m "fix: synchronize service registries"
```

## 任务 12.3：收敛 WebServer 与内部服务注册表

**优先级：** P0

**状态：** 已在 `52ac181` 完成。WebServer 实例状态、服务器选项和内部服务注册表已同步；多实例初始化使用活动计数，定向竞态、静态检查和全量服务端回归通过。

**文件：**
- 修改：`pkg/server/run/server.go`
- 创建：`pkg/server/run/server_concurrency_test.go`
- 修改：`pkg/server/config/serverconfig.go`
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/types/server.go`
- 修改：读取初始化状态的 router 与 persistence 内部调用点

- [x] **步骤 1：编写实例隔离和快照测试**

证明两个 `WebServer` 的启动回调互不影响；并发增加上下文、设置选项和读取不会竞态；`GetServerOptions` 返回深度足够的快照，修改返回 map 或 `ServerOption` 普通字段/slice 不影响内部状态；并发设置/获取内部服务安全且类型不匹配不会 panic。

- [x] **步骤 2：实现并验证**

把进程级 `once` 移入 `WebServer`；为实例 map 使用同一把 `RWMutex`，外部回调在锁外运行。getter 返回快照或单值。为 `typemap` 添加独立 `RWMutex` 并使用安全类型断言。

```bash
go test -race ./pkg/server/run -count=1
git add pkg/server/run
git commit -m "fix: isolate web server state"
```

## 任务 12.4：修正异步 WebSocket 测试契约

**优先级：** P0 测试门禁

**状态：** 已在 `87cc800` 完成。删除不安全读取和固定等待，`go test -race ./pkg/server/types -count=20` 通过。

**文件：**
- 修改：`pkg/server/types/websocketshard_test.go`

- [x] **步骤 1：保留当前竞态证据**

```bash
go test -race ./pkg/server/types -run 'TestUnRegisterWebSocketHash_DoubleUnregisterFiresOnce|TestUnRegisterWebSocketHash_UnknownClientDoesNotChangeCount' -count=1
```

当前测试在异步回调写 slice 时直接执行 `len(capture.subs)`，race detector 报警。

- [x] **步骤 2：仅修正同步方式**

为 capture 添加持锁的 `subscriptionCount()`，或由回调关闭 channel。保留生产异步契约，不为迎合测试改成同步通知。

- [x] **步骤 3：验证并提交**

```bash
go test -race ./pkg/server/types -count=1
git add pkg/server/types/websocketshard_test.go
git commit -m "test: synchronize websocket callbacks"
```

## 任务 12.5：按服务隔离跨节点转发器

**优先级：** P1

**状态：** 已在 `b816515` 完成。服务作用域注册、兼容回退、实例安全清理、WebSocket 通知和 NoticeRelay 均已迁移并通过竞态与全量服务端回归。

**文件：**
- 修改：`pkg/server/types/crossnode.go`
- 修改：`pkg/server/types/websocketshard.go`
- 修改：`pkg/server/api/manage/noticerelay.go`
- 修改：`pkg/server/router/servicecontext.go`
- 修改：相关测试

- [x] **步骤 1：编写多服务隔离失败测试**

注册两个服务的 forwarder，断言各自路由的订阅和通知只到达同名服务；停止其中一个不会清空另一个。覆盖旧全局 API 的兼容回退。

- [x] **步骤 2：实现服务作用域注册表**

新增 `SetCrossNodeForwarderForService`、`GetCrossNodeForwarderForService` 和 `ClearCrossNodeForwarderForService`。`RouterInfo` 使用自身 `ServiceName` 查询；旧全局 API 保留为废弃兼容入口。清理必须比较实例，防止旧 owner 删除新 owner。

- [x] **步骤 3：验证并提交**

```bash
go test -race ./pkg/server/types ./pkg/server/router ./pkg/server/api/manage -count=1
git add pkg/server/types pkg/server/router pkg/server/api/manage
git commit -m "fix: scope cross-node forwarders by service"
```

## 任务 12.6：建立幂等、可等待的服务生命周期

**优先级：** P1

**状态：** 已完成。任务 12.6a 已在 `ffe27c8` 完成：`MembershipManager` 并发 Start/Stop 幂等、注销仅执行一次且 Stop 可等待 worker；`ServiceContext` 已串行化启停与 Provider 切换，相同状态不再重复注册或通知，Provider 调用期间不持有内部状态锁。任务 12.6b 已在 `f016173` 完成：REST 与 HTML listener 可有界关闭，HTMLServer 使用实例级 mux，WebServer Stop 等待 ServiceGroup、所有 Start 和业务 Stop 返回。

**文件：**
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/cluster/membership.go`
- 修改：`pkg/server/run/server.go`
- 修改：`pkg/server/run/htmlserver.go`
- 修改：`pkg/server/trans/rest/server.go`
- 按需清理：`pkg/server/run/fiberserver.go`（当前不在实际启动路径）
- 修改：相关测试

- [x] **步骤 1a：编写 MembershipManager 与 ServiceContext 重复启停测试**

并发调用 Start/Stop，断言只注册和注销一次、Stop 等待 heartbeat worker 退出，相同 ServiceContext 状态不重复通知。

- [x] **步骤 2a：实现集群生命周期 owner**

`ServiceContext` 使用实例级操作门串行化启停与 Provider 切换，内部 mutex 只保护状态读写，Provider 调用位于锁外。`MembershipManager` 使用 `sync.Once` 和完成通道实现幂等退出及有界等待。

- [x] **步骤 1b：编写服务器重复启动/关闭测试**

验证 `WebServer.Stop` 后 listener 关闭、所有 `Start` goroutine 返回、业务 `IStopService` 已完成且重复 Stop 不阻塞。两个 WebServer 实例不得因全局 HTTP mux 重复注册而 panic。

- [x] **步骤 2b：实现服务器单一 owner**

`WebServer` 持有实例级 `ServiceGroup`、状态和幂等 `Stop`。

go-zero v1.10.2 的 REST `StartWithOpts` 在 listener 被 `Shutdown` 后仍会等待进程级 shutdown listener。因此 REST 包装器只关闭自己的 `http.Server`，不调用 `proc.Shutdown`；顶层 WebServer 作为整个应用 owner，在所有 ServiceGroup 服务已就绪后统一触发进程级协调器。这不是可用于单个 REST 实例独立重启的 API。

复用 go-zero：通过其 REST `StartWithOpts` 获取实际 `*http.Server`，包装层 `Stop` 使用 deadline 调用 `Shutdown`；继续使用 `ServiceGroup` 的并发启动与一次性停止语义，不自建第二套 service group 或信号监听器。`HTMLServer` 改为拥有独立 `http.ServeMux` 和 `http.Server`，并实现同样的有界 Shutdown。

MQ、Transport 和数据库只调用成熟客户端已有的 `Close`/`Stop`，不得创造新的连接池或 worker。

- [x] **步骤 3：完成 12.6b 后执行全量验证并提交**

```bash
go test -race ./pkg/server/cluster ./pkg/server/router ./pkg/server/run ./pkg/server/trans/rest -count=1
go test ./pkg/server/... -count=10
git add pkg/server/cluster/membership.go pkg/server/router/servicecontext.go pkg/server/run pkg/server/trans/rest/server.go
git commit -m "fix: make service lifecycle deterministic"
```

验收记录：四个生命周期包全量 `-race` 通过，`go test ./pkg/server/... -count=1` 通过，run/rest 重复 10 次通过，`go vet ./pkg/server/...` 通过。全量 `-count=10` 仍会在 `pkg/server/api/manage` 命中既有的 ClusterSwitcher 测试状态残留（`provider migration already in progress`），纳入任务 12.8 修复，不属于本节服务器生命周期回归。

## 任务 12.7：Provider 切换期间持续对账

**优先级：** P1

**文件：**
- 修改：`pkg/server/cluster/switcher.go`
- 修改：`pkg/server/cluster/switcher_test.go`
- 按需修改：`pkg/server/router/servicecontext.go`

- [ ] **步骤 1：编写迁移窗口失败测试**

覆盖 `Begin` 后新增节点进入 pending、running 转 offline、offline 仍在快照、节点删除、乱序 Watch 回调、取消时正在 Register、`Complete` 和 `Rollback` 等待 watcher 退出、重复快照幂等，以及没有新 Watch 事件时 pending 暂时失败后仍可恢复。

- [ ] **步骤 2：实现 Watch 驱动的全量对账**

`Begin` 首次复制后订阅 current provider。只镜像 `NodeStatusRunning`，suspect/offline/删除节点均从 pending 注销；首次 List 与 Watch 使用同一过滤规则。Watch callback 只把最新快照提交给单一有界 reconciliation worker，由 worker 串行处理并以 migration generation 丢弃旧事件。失败时对最新快照做有界指数退避，直到成功、被新快照替代或迁移 context 取消。

`Complete`/`Rollback` 的固定顺序为：停止接收快照、取消 Watch、等待 worker 和在途 Provider 调用结束、再切换或关闭 Provider。`Begin` 的首次 List 或 Watch 失败必须返回错误并保持未迁移状态。锁只保护指针、状态和镜像元数据，Provider 调用全部在锁外。

- [ ] **步骤 3：验证并提交**

```bash
go test -race ./pkg/server/cluster ./pkg/server/router -count=1
git add pkg/server/cluster/switcher.go pkg/server/cluster/switcher_test.go pkg/server/router/servicecontext.go
git commit -m "fix: reconcile cluster provider migration"
```

## 任务 12.8：竞态、泄漏与能力验收

**优先级：** P1 门禁

**文件：**
- 修改：`scripts/test.sh`
- 修改：`docs/codex/PROJECT_REVIEW_ACTION_PLAN.md`
- 修改：本文件

**已知测试债务：** `pkg/server/api/manage` 的 ClusterSwitcher 测试使用固定服务名并保留迁移状态，`-count=2` 第二轮会命中缓存并报 `provider migration already in progress`；单次测试通过。任务 12.8 必须改为每轮唯一服务名或显式清理迁移状态。

- [ ] **步骤 1：增加稳定测试入口**

为 `scripts/test.sh` 增加 `concurrency` 模式，只运行本任务拥有的包，并包含 `pkg/server/trans/rest`。不得让文档声明但未实现的模式返回含糊的 `exit 2`。

- [ ] **步骤 2：执行最终门禁**

```bash
./scripts/test.sh concurrency
go test -race ./service/manage ./pkg/server/api/manage ./pkg/server/router ./pkg/server/run ./pkg/server/trans/rest ./pkg/server/types ./pkg/server/cluster -count=1
go test -race ./pkg/server/run ./pkg/server/router ./pkg/server/cluster ./pkg/server/trans/rest -run 'Test.*Lifecycle|Test.*Concurrent.*Start.*Stop|Test.*Shutdown' -count=20
go test ./pkg/server/... -count=1
go vet ./pkg/server/... ./service/manage/...
```

- [ ] **步骤 3：更新能力矩阵**

重复启动/关闭测试至少 20 次，以 channel/wait group 证明 worker 已退出。在总计划记录提交、命令、剩余兼容风险和未纳入项。只有全部门禁通过后才将任务 12 标为完成。

## 进程级 WebSocket Worker 归属

- [ ] 全局通知系统和周期清理 worker 归进程生命周期，不归任一 `WebServer` 实例。
- [ ] 复用 go-zero `proc.AddShutdownListener` 注册一次关闭，不重复处理系统信号。
- [ ] 周期清理接受 context/cancel 并可等待退出；测试明确单例关闭后是否允许在同一进程重启。

## 最终验收清单

- [x] 框架生产路径不再向 `ManageService` 或业务管理服务写入请求对象；旧导出字段仅保留源码兼容。
- [ ] 所有可变进程级 map 都有唯一 owner 和一致同步策略，getter 不返回内部 map。
- [ ] 两个服务的 CrossNode forwarder 不会互相覆盖或误清理。
- [ ] `ServiceContext`、membership 和服务器关闭可重复调用，并在 deadline 内完成。
- [ ] Provider 迁移窗口内新增、删除和离线节点都能对账到 pending。
- [x] WebSocket 异步回调测试不直接读取未同步状态。
- [ ] 定向 `-race`、服务端回归和 `go vet` 全部通过。
- [ ] 每个小节均有独立开发提交、测试证据和审查结论。
