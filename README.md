# Digitalway Core

Digitalway Core 是面向 AI Agent 协作开发的 Go 业务服务框架。它不打算做一个通用技术框架，而是把商业服务的开发方式**标准化**：业务代码只表达「做什么、何时做」，「怎么做」交给框架决定，并且可以独立替换升级而不动已实现的业务逻辑。

这套约束同时服务两类读者。对 Agent，固定的路由、模型和管理操作约定意味着可预测的代码生成路径，规范正文是一份机器可读的权威源。对人类，生成结果保持极小体量——一个含商品与订单模型、管理后台、公开查询、用户鉴权、持久化和 WebSocket 通知的完整商城是 **789 行 / 16 个文件**——可以逐行审计。

## 环境

- Go 版本以 [go.mod](./go.mod) 为准。
- 核心 HTTP、配置、日志和生命周期复用 go-zero；SQL 模型契约使用 GORM。
- 框架只保留 Digitalway 领域契约，不重复实现通用客户端、日志或并发原语。
- 外部依赖测试默认跳过，通过显式环境变量或 Docker Compose 模式启用。

```bash
go get github.com/digitalwayhk/core@latest
```

## 为什么适合 Agent 开发

**规范只有一份，并随代码演进。** 全部开发规范集中在 [docs/ai/core-skill](./docs/ai/core-skill/SKILL.md)：一个索引加 12 个主题分片。Codex、Claude、Cursor、GitHub Copilot 各自的 skills 目录下只有指向它的指针文件，不存在会互相漂移的副本。`./scripts/ci.sh required/ai-skill` 在 CI 中校验索引完整、分片不超体积上限、指针有效，防止全文副本回流。

**约定消除自由发挥的空间。** Agent 最容易出错的地方是「同一件事有多种写法」，框架把这些位置固定下来：

- 包路径决定认证域，且不出现在 URL 里。`api/public` 匿名访问，`api/private` 自动要求认证且身份只能来自 `req.GetUser()`，不接受请求体自报。
- 建库、建表和补列由框架在**首次数据访问时**自动完成。业务不写迁移脚本，也不调用 GORM `AutoMigrate`；删列、改类型等破坏性变更不会自动执行。
- 一次 `manage.NewManageService[T](own)` 生成 View、Search、Add、Edit、Remove、Submit、Release 七个标准操作，并直接驱动内嵌管理后台的界面渲染。

**产出可自检。** `GET /api/openapi` 输出业务契约，`GET /api/servermanage/testtoken` 签发普通用户、管理、服务管理三类认证域的测试 Token。Agent 写完即可自行发起真实请求验证，不必等人工联调。

## AI 助手与 Skill（消费方）

`go get` **只安装 Go 模块**，不会把 Agent skill 装进业务仓库。依赖本框架开发或审查后端 API 时，消费方（及其 AI）需要先安装 skill，再按规范写代码。

当消费方 `go.mod` 依赖 `github.com/digitalwayhk/core`，或任务要求「按 Core 规范」开发时，Agent **应先检测再安装**，不要因找不到 skill 就凭记忆编码：

```bash
# 1) 是否已安装
test -f .codex/skills/use-digitalway-core/SKILL.md && echo ready || echo missing

# 2) 缺失则安装（优先本机 core 源码）
export DIGITALWAY_CORE_PATH=/path/to/digitalway.hk/core   # 与 go.mod replace 一致时最稳
"$DIGITALWAY_CORE_PATH/scripts/link-consumer-skill.sh" --target . --write-agents

# 或仅有模块依赖时
CORE="$(go list -m -f '{{.Dir}}' github.com/digitalwayhk/core)"
"$CORE/scripts/link-consumer-skill.sh" --target . --write-agents
```

安装后 Agent **必须**阅读：

- 消费方仓库内 `.codex/skills/use-digitalway-core/SKILL.md`（指针，只给出权威源路径）
- 权威源 `.codex/skills/core-skill/SKILL.md`，以及它「主题分片索引」中按任务需要的分片
- Claude、Cursor、Copilot 的 skills 目录同构安装（`.claude/skills/`、`.cursor/skills/`、`.github/copilot/skills/`），各自指针都指向同一份权威源，不存在多份正文
- Core 源码中的 `docs/codex/*` 与最近 `examples/*`（路径：`go list -m -f '{{.Dir}}' github.com/digitalwayhk/core` 或 `DIGITALWAY_CORE_PATH`）

建议在消费方 `AGENTS.md` / `Claude.md` 中写入上述流程（脚本加 `--write-agents` 可自动追加标准段落）。完整说明、软链/复制、用户级安装与故障排查见 [消费方 AI Skill 安装与识别](./docs/codex/CONSUMER_AI_SKILL_SETUP.md)。本仓库自身的 Agent 约定见 [AGENTS.md](./AGENTS.md)。

## 能力从简到繁

七个示例构成递进阶梯。每个都是可独立运行、独立测试的完整应用，而不是片段演示，可以直接作为对应阶段的模板：

| 示例 | 在前一级之上新增的能力 | 业务代码规模 |
| --- | --- | --- |
| [01-simple-shop](./examples/01-simple-shop) | 模型、Manage CRUD、Public/Private API、JWT 鉴权、SQLite 持久化、订单 WebSocket 通知 | 789 行 / 16 文件 |
| [02-shop-payment](./examples/02-shop-payment) | API / business / models 分层，跨模型事务，支付结果滞后的状态机，Manage 自定义命令 | 2224 行 / 35 文件 |
| [03-shop-inheritance](./examples/03-shop-inheritance) | 模型与 Manage 的双继承层，基础资料与业务事实分支，通用启停，只读子表 | 2936 行 / 49 文件 |
| [04-shop-performance](./examples/04-shop-performance) | 查询分层缓存、下单事实缓存、Group Commit 可靠写、写后同步 | 3861 行 / 54 文件 |
| [05-shop-casdoor-rbac](./examples/05-shop-casdoor-rbac) | Casdoor 登录，Auth / Manage 双认证域隔离，权限矩阵 | 3727 行 / 58 文件 |
| [06-shop-microservices](./examples/06-shop-microservices) | 拆成三服务，Redis 发现，gRPC/mTLS，受限内部 Public，可靠事件，本地永久投影 | 6261 行 / 120 文件 |
| [07-shop-order-scale](./examples/07-shop-order-scale) | 订单服务多副本水平扩展，AutoMachineID，共享远程权威库，Outbox，服务报表 | 6266 行 / 106 文件 |

行数只统计业务代码，不含测试。七个示例覆盖同一个商城业务域，因此可以直接对比「多一项能力要多写多少代码」。

## 最小服务

路由实现 `types.IRouter` 的 `Parse`、`Validation`、`Do` 和 `RouterInfo`。普通 public/private 路径为 `/api/{service}/{structLower}`：

```go
func (own *Ping) Parse(req types.IRequest) error { return req.Bind(own) }
func (own *Ping) Validation(req types.IRequest) error { return nil }
func (own *Ping) Do(req types.IRequest) (interface{}, error) {
	return map[string]string{"message": "pong"}, nil
}
func (own *Ping) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}
```

服务把路由装配起来即可启动，管理操作按 Manage 展开：

```go
func (own *ShopService) ServiceName() string { return contract.ServiceName }

func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 11)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers,
		&publicapi.GetProducts{},
		&privateapi.AddOrder{},
		&privateapi.GetOrders{},
		&privateapi.DeleteOrder{},
	)
	return routers
}
```

完整代码见 [最简商城示例](./examples/01-simple-shop)。

## 模型与管理 CRUD

- Model 层是业务设计的第一层。先按数据生命周期拆成两类：商品、商户、订单类型等稳定且增长可预期的数据属于基础资料 Model；订单、支付/库存流水等依赖基础资料 ID、由业务持续产生且规模不可预知的数据属于业务事实 Model。
- 两类模型必须建立平行继承支路：`ServiceModel → BaseDataModel → Product` 与 `ServiceModel → BusinessModel → Order`。服务公共基座只承载数据库名、TraceID 等能力，不是基础资料 Model；业务事实不得继承基础资料支路。
- 基础资料通常使用稳定 Code、启停和引用保护；业务事实保存基础资料 ID 与必要历史快照，使用幂等、状态机、受控命令、分页/索引/归档，不能按普通 CRUD 任意修改删除。
- `entity.Model` 是中性持久化根；只有天然具有稳定 `Code` 语义的资料模型才使用 `entity.BaseModel`，业务单据/记录按需选择 `BaseOrderModel`/`BaseRecordModel`。
- Manage 使用 `entity.NewModelList[T](nil)` 保留标准筛选、排序和分页；public/private 路由调用模型封装的 `IDataAction` 查询与操作方法。
- 嵌入框架或项目模型指针的类型必须在 `NewModel()` 中初始化完整继承链。
- 管理 CRUD 路径为 `/api/manage/{service}/{manageStructLower}/{operationLower}`。Manage 生成的 `ViewModel` 同时描述字段、命令和子表，内嵌管理后台 `web/admin` 据此渲染界面，无需为每个模型单独写前端。

简单模型、Manage CRUD 和私有订单接口见 [最简商城示例](./examples/01-simple-shop)；两类模型与 Manage 分支见 [模型继承示例](./examples/03-shop-inheritance) 和 [模型分片](./docs/ai/core-skill/models.md#先按数据生命周期分类)。

## 高性能写路径

示例 04 与 07 的写优化都建立在「本地可靠确认 + 最终写回」上，由框架统一实现，业务只提供汇合适配器：

- 下单请求最多合并 1 毫秒或 128 个并发为一次 Badger 事务，请求在所属批次完成 `SyncWrites` 之后才返回。这不是「内存入队即成功」，已确认的订单在进程异常退出后仍可恢复。
- `ReliableWriteStore[T]` 封装本地持久写、批量提交、准入背压、pending 计数与实例目录隔离；`UseWriteBehind(WriteBehindTarget)` 绑定一次远端汇合目标，之后的 ACK、重试、磁盘指标和关闭排空都由框架处理。
- 查询侧 L1 缓存按进程有效内存的 2% 自动解析字节预算（下限 16 MiB、上限 256 MiB），可选 Badger L2，同键冷加载用 SingleFlight 合并；数据变更后由事件主动失效，TTL 只负责兜底。

write-behind 是 at-least-once，远端 insert/update/delete 必须靠稳定主键或 upsert 保证幂等。同 key 多次更新会合并为最新状态，只适用于账户快照、资料和订单当前状态；资金流水、审计记录等不可合并事件必须走 transactional outbox 或带唯一事件 ID 的 JetStream。完整边界见 [框架场景使用指南](./docs/codex/FRAMEWORK_USAGE_GUIDE.md)。

## 多服务与水平扩展

- 只供其他服务调用的 Public API 声明 `router.WithInternalCallers("shop-user", ...)`。调用方身份只能来自同进程 ServiceContext 或已验证的 mTLS 证书 SAN，不接受 HTTP Header 或请求体自报。调用方直接构造目标服务已注册的 API 并用 `req.CallService`，不另建地址型 client 或 `api/call` 副本。
- 内部同步调用默认走 gRPC，服务发现使用 ServiceResolver（Redis / etcd / Consul），异步事件统一使用 EventBridge。
- 数据库按业务域拆分，不按技术实例拆分。同一逻辑服务的多个副本共享一个远程权威库，每个副本持有独立的本地 pending store 用于可靠接收、崩溃恢复和批量同步；`AutoMachineID=true` 在扩容时自动分配唯一实例编号，不依赖人工配置。
- 运行拓扑与请求聚合通过 `POST /api/servermanage/runtimetopology` 和 `runtimeservice` 查询，历史指标源为 go-zero / Prometheus。

服务边界与扩容验证见 [多服务商城示例](./examples/06-shop-microservices) 和 [订单服务水平扩展示例](./examples/07-shop-order-scale)。

## 业务统计、经营分析与服务报表

Core 提供声明式业务统计、管理端经营分析和服务级报表能力，统一入口为
`pkg/persistence/entity/stats`。完整模板见
[07 订单服务](./examples/07-shop-order-scale/order-service)，实现契约见
[业务统计与报表分片](./docs/ai/core-skill/stats-and-reports.md)。

最小接入流程：

1. 用 `stats.StatSpec` 声明事实表、时间粒度、维度和 `count|sum|avg` 指标，并在启动期调用 `stats.Register`。
2. 为服务创建独立 `stats.Store`，注入 OLTP 或 ClickHouse `StatsEngine`，由后台 Runner 定时调用 `stats.RefreshWithEngine` 刷新快照。
3. 注册 Manage 接口 `POST /api/manage/{service}/analysis`，返回标准 `stats.Dashboard`。
4. 用 `stats.RegisterReports` 注册服务报表，再注册 `POST /api/manage/{service}/reports` 和 `/reports/view`。
5. 执行菜单同步后，报表会挂在对应服务子菜单，前端路径为 `/report/{service}/{code}`。

`ReportDef` 只定义报表展示与 `StatSpec` 的绑定，不会自动创建事实数据、Runner
或 API；analysis/reports API 应只读统计快照，不得在请求路径临时扫描业务表。
同一进程承载多个服务时，Spec Code 必须带稳定业务前缀，每个服务使用独立 Store。
这些接口属于 Manage 认证域。

### 消费项目版本要求

消费项目需要同时满足：

- `go.mod` 使用的 Core 版本包含 `pkg/persistence/entity/stats`；
- Core 嵌入的 `web/admin` 与分析、报表 JSON 契约匹配；
- 已按上方“AI 助手与 Skill”运行 `scripts/link-consumer-skill.sh`，让项目内 AI 读取现行接入规范。

当前分析与报表能力位于 `core-web-admin` 开发分支，尚未进入稳定 tag。因此不能仅执行
`go get github.com/digitalwayhk/core@latest` 后假定该能力可用；正式消费应等待包含该能力的
发布版本，或在明确评审和锁定提交的前提下使用对应开发版本。不得通过复制 Core 实现或
长期 `replace` 伪装成已发布能力。

## 安全与配置

- CORS 开启时必须显式配置 origin；`*` 仅在调用方主动选择时允许。
- `TrustedProxies` 默认空，忽略 `X-Forwarded-For`/`X-Real-IP`；反向代理部署必须配置可信 IP/CIDR。
- 配置文件和迁移结果使用 `0600` 权限；不支持的能力在启动前 fail closed。
- 运行时日志统一使用 go-zero `logx`，不得记录 token、TOTP、完整请求/响应、payload、SQL 或参数值。

配置字段存在不代表能力可用，最终以校验、factory、启动链和行为测试为准；当前支持范围见 [配置到运行时能力矩阵](./docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md)。完整日志级别、字段和错误归属规则见 [日志审计与规范](./docs/codex/LOGGING_AUDIT_AND_STANDARD.md)。

## 兼容与废弃

公共 API 按 additive 方式演进。已废弃但仍可编译的表面统一登记在 [废弃 API 登记](./docs/codex/DEPRECATION_REGISTER.md)，逐条给出替代入口、最早删除版本、Owner 和迁移证据。表中「最早删除版本」是下限而非自动删除指令：删除前还需仓库内调用清零、已登记消费方完成迁移、CHANGELOG Removed 段完整。升级 Core 版本前应先对照该表检查自己用到的入口。

## 验证

```bash
./scripts/test.sh quick
./scripts/test.sh server
./scripts/test.sh release-contract
./scripts/test.sh integration-external-docker
```

CI 门禁由 `scripts/ci.sh` 按名称执行，如 `./scripts/ci.sh required/quick`、`required/contracts`、`required/ai-skill`（校验 AI skill 权威源结构，防止 agent 目录回流全文副本）、`required/server-manage`、`required/race`。

更完整的场景选择、成熟度和测试命令见 [框架场景使用指南](./docs/codex/FRAMEWORK_USAGE_GUIDE.md)。
