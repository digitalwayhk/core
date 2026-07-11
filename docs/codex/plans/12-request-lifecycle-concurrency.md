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

**文件：**
- 修改：`service/manage/manageservice.go`
- 修改：`service/manage/view.go` 及 CRUD 路由文件
- 修改：`pkg/server/api/manage/menumanage.go`
- 创建或修改：对应测试文件

- [ ] **步骤 1：编写请求隔离失败测试**

并发执行两个带不同 `NewID()` 结果的请求，断言默认菜单项只使用各自请求的 ID，且一个请求不能覆盖另一个。新增请求感知接口，同时证明旧接口仍能兼容回退：

```go
type IGetDefaultItemsWithRequest[T pt.IModel] interface {
    GetDefaultItemsWithRequest(req st.IRequest) []*T
}
```

- [ ] **步骤 2：验证 RED**

```bash
go test -race ./service/manage ./pkg/server/api/manage -run 'Test.*RequestIsolation|Test.*DefaultItems' -count=1
```

- [ ] **步骤 3：用显式参数替换共享存储**

`SearchAfter` 优先调用请求感知接口，再回退旧 `IGetDefaultItems`。`MenuManage` 将 `req` 显式传入默认项生成和递归更新逻辑。删除框架内部所有 `SetReq` 调用、`ManageService.Req` 字段和默认写入实现。

兼容规则：保留已导出的 `IRequestSet` 类型一个发布周期并标记废弃，但框架不再调用它；业务扩展应直接使用现有 hook 的 `req` 参数。

- [ ] **步骤 4：验证 GREEN 并提交**

```bash
go test -race ./service/manage ./pkg/server/api/manage -count=1
go test ./pkg/server/... -count=1
git add service/manage pkg/server/api/manage
git commit -m "fix: isolate manage requests"
```

## 任务 12.2：封装 ServiceContext 与测试结果注册表

**优先级：** P0

**文件：**
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/router/servicecontext_test.go`
- 修改：`pkg/server/api/public/openapi.go`
- 修改：`pkg/server/run/openapi.go`

- [ ] **步骤 1：编写并发与快照失败测试**

覆盖并发创建/读取不同服务上下文、同名服务只产生一个实例、并发写读 OpenAPI 测试结果，以及修改 `GetContexts()` 返回值不会改变内部注册表。

- [ ] **步骤 2：验证 RED**

```bash
go test -race ./pkg/server/router ./pkg/server/api/public ./pkg/server/run -run 'Test.*Context.*Concurrent|Test.*Context.*Snapshot|Test.*Result.*Concurrent' -count=1
```

- [ ] **步骤 3：建立唯一同步入口**

以包内 `sync.RWMutex` 保护上下文和测试结果。构造同名 `ServiceContext` 时串行化启动期初始化，避免重复 MachineID claim、MQ 或 Provider 副作用。`GetContexts` 返回新 map；新增 `SetTestResult` 与 `GetTestResult`，内部调用方不得直接访问 map。

- [ ] **步骤 4：验证 GREEN 并提交**

```bash
go test -race ./pkg/server/router ./pkg/server/api/public ./pkg/server/run -count=1
git add pkg/server/router pkg/server/api/public/openapi.go pkg/server/run/openapi.go
git commit -m "fix: synchronize service registries"
```

## 任务 12.3：收敛 WebServer 与内部服务注册表

**优先级：** P0

**文件：**
- 修改：`pkg/server/run/server.go`
- 创建：`pkg/server/run/server_concurrency_test.go`
- 按需修改：`pkg/server/run/htmlserver.go`

- [ ] **步骤 1：编写实例隔离和快照测试**

证明两个 `WebServer` 的启动回调互不影响；并发增加上下文、设置选项和读取不会竞态；`GetServerOptions` 返回快照；并发设置/获取内部服务安全且类型不匹配不会 panic。

- [ ] **步骤 2：实现并验证**

把进程级 `once` 移入 `WebServer`；为实例 map 使用同一把 `RWMutex`，外部回调在锁外运行。getter 返回快照或单值。为 `typemap` 添加独立 `RWMutex` 并使用安全类型断言。

```bash
go test -race ./pkg/server/run -count=1
git add pkg/server/run
git commit -m "fix: isolate web server state"
```

## 任务 12.4：修正异步 WebSocket 测试契约

**优先级：** P0 测试门禁

**文件：**
- 修改：`pkg/server/types/websocketshard_test.go`

- [ ] **步骤 1：保留当前竞态证据**

```bash
go test -race ./pkg/server/types -run 'TestUnRegisterWebSocketHash_DoubleUnregisterFiresOnce|TestUnRegisterWebSocketHash_UnknownClientDoesNotChangeCount' -count=1
```

当前测试在异步回调写 slice 时直接执行 `len(capture.subs)`，race detector 报警。

- [ ] **步骤 2：仅修正同步方式**

为 capture 添加持锁的 `subscriptionCount()`，或由回调关闭 channel。保留生产异步契约，不为迎合测试改成同步通知。

- [ ] **步骤 3：验证并提交**

```bash
go test -race ./pkg/server/types -count=1
git add pkg/server/types/websocketshard_test.go
git commit -m "test: synchronize websocket callbacks"
```

## 任务 12.5：按服务隔离跨节点转发器

**优先级：** P1

**文件：**
- 修改：`pkg/server/types/crossnode.go`
- 修改：`pkg/server/types/websocketshard.go`
- 修改：`pkg/server/api/manage/noticerelay.go`
- 修改：`pkg/server/router/servicecontext.go`
- 修改：相关测试

- [ ] **步骤 1：编写多服务隔离失败测试**

注册两个服务的 forwarder，断言各自路由的订阅和通知只到达同名服务；停止其中一个不会清空另一个。覆盖旧全局 API 的兼容回退。

- [ ] **步骤 2：实现服务作用域注册表**

新增 `SetCrossNodeForwarderForService`、`GetCrossNodeForwarderForService` 和 `ClearCrossNodeForwarderForService`。`RouterInfo` 使用自身 `ServiceName` 查询；旧全局 API 保留为废弃兼容入口。清理必须比较实例，防止旧 owner 删除新 owner。

- [ ] **步骤 3：验证并提交**

```bash
go test -race ./pkg/server/types ./pkg/server/router ./pkg/server/api/manage -count=1
git add pkg/server/types pkg/server/router pkg/server/api/manage
git commit -m "fix: scope cross-node forwarders by service"
```

## 任务 12.6：建立幂等、可等待的服务生命周期

**优先级：** P1

**文件：**
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/cluster/membership.go`
- 修改：`pkg/server/run/server.go`
- 修改：`pkg/server/run/fiberserver.go`
- 修改：相关测试

- [ ] **步骤 1：编写重复启动/关闭测试**

并发调用 start/stop，断言只注册和注销一次、heartbeat 在 deadline 内退出、broker 只 drain 一次。验证业务 `IStopService` 在 `WebServer.Start` 返回前完成，且 `FiberServer.Stop` 调用 Fiber shutdown。

- [ ] **步骤 2：实现单一 owner**

`ServiceContext` 使用生命周期 mutex、运行 context/cancel 和明确状态；锁内只做状态转换，锁外执行 Provider。`MembershipManager` 用 `sync.Once` 与 wait group 实现幂等退出。`WebServer` 先停止 server group，再同步或有界等待业务 `Stop`。

MQ、Transport 和数据库只调用成熟客户端已有的 `Close`/`Stop`，不得创造新的连接池或 worker。

- [ ] **步骤 3：验证并提交**

```bash
go test -race ./pkg/server/cluster ./pkg/server/router ./pkg/server/run -count=1
go test ./pkg/server/... -count=10
git add pkg/server/cluster/membership.go pkg/server/router/servicecontext.go pkg/server/run
git commit -m "fix: make service lifecycle deterministic"
```

## 任务 12.7：Provider 切换期间持续对账

**优先级：** P1

**文件：**
- 修改：`pkg/server/cluster/switcher.go`
- 修改：`pkg/server/cluster/switcher_test.go`
- 按需修改：`pkg/server/router/servicecontext.go`

- [ ] **步骤 1：编写迁移窗口失败测试**

覆盖 `Begin` 后新增节点进入 pending、节点离线/删除后的完整快照、`Complete` 和 `Rollback` 取消 watcher、重复快照幂等，以及 pending 暂时失败后可恢复。

- [ ] **步骤 2：实现 Watch 驱动的全量对账**

`Begin` 首次复制后订阅 current provider。每次回调对 pending 注册当前节点，并注销上次镜像中已不存在的节点。保存 cancel，在完成或回滚前先取消。锁只保护指针、状态和镜像元数据，Provider 调用全部在锁外。

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

- [ ] **步骤 1：增加稳定测试入口**

为 `scripts/test.sh` 增加 `concurrency` 模式，只运行本任务拥有的包。不得让文档声明但未实现的模式返回含糊的 `exit 2`。

- [ ] **步骤 2：执行最终门禁**

```bash
./scripts/test.sh concurrency
go test -race ./service/manage ./pkg/server/api/manage ./pkg/server/router ./pkg/server/run ./pkg/server/types ./pkg/server/cluster -count=1
go test ./pkg/server/... -count=1
go vet ./pkg/server/... ./service/manage/...
```

- [ ] **步骤 3：更新能力矩阵**

重复启动/关闭测试至少 20 次，以 channel/wait group 证明 worker 已退出。在总计划记录提交、命令、剩余兼容风险和未纳入项。只有全部门禁通过后才将任务 12 标为完成。

## 最终验收清单

- [ ] `ManageService` 及业务管理服务不再保存请求对象。
- [ ] 所有可变进程级 map 都有唯一 owner 和一致同步策略，getter 不返回内部 map。
- [ ] 两个服务的 CrossNode forwarder 不会互相覆盖或误清理。
- [ ] `ServiceContext`、membership 和服务器关闭可重复调用，并在 deadline 内完成。
- [ ] Provider 迁移窗口内新增、删除和离线节点都能对账到 pending。
- [ ] WebSocket 异步回调测试不直接读取未同步状态。
- [ ] 定向 `-race`、服务端回归和 `go vet` 全部通过。
- [ ] 每个小节均有独立开发提交、测试证据和审查结论。
