<div align="center">

# Digitalway Core

**面向 AI Agent 协作开发的 Go 业务服务框架**

[![Go Reference](https://pkg.go.dev/badge/github.com/digitalwayhk/core.svg)](https://pkg.go.dev/github.com/digitalwayhk/core)
[![Go](https://img.shields.io/badge/Go-1.26.6-00ADD8?logo=go&logoColor=white)](./go.mod)
[![Examples](https://img.shields.io/badge/examples-7%20个完整应用-brightgreen)](./examples)
[![Skill](https://img.shields.io/badge/AI%20skill-单一权威源-8A2BE2)](./docs/ai/core-skill/SKILL.md)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](./LICENSE)

</div>

---

Digitalway Core 不做通用技术框架，而是把商业服务的开发方式**标准化**：业务代码只表达「做什么、何时做」，「怎么做」交给框架，并且可以独立替换升级而不动已实现的业务逻辑。

一个含商品与订单模型、管理后台、公开查询、用户鉴权、持久化和 WebSocket 通知的**完整商城，是 789 行 / 16 个文件**。对 Agent，固定的约定意味着可预测的生成路径；对人类，这个体量可以逐行审计。

```bash
go get github.com/digitalwayhk/core@latest
```

## 目录

**上手**　[快速开始](#快速开始)　·　[服务骨架](#服务骨架)　·　[能力地图](#能力地图)　·　[能力阶梯](#能力阶梯)

**规范**　[核心约定](#核心约定)　·　[为什么适合 Agent 开发](#为什么适合-agent-开发)　·　[消费方接入 Skill](#消费方接入-skill)

**进阶**　[高性能写路径](#高性能写路径)　·　[多服务与水平扩展](#多服务与水平扩展)　·　[统计与报表](#统计与报表)

**运维**　[安全与配置](#安全与配置)　·　[兼容与废弃](#兼容与废弃)　·　[测试与门禁](#测试与门禁)　·　[文档地图](#文档地图)　·　[许可证](#许可证)

---

## 快速开始

首次运行会在可执行文件目录自动生成 `etc/server.json` 和 `etc/shop.json`。

> [!NOTE]
> 不需要手工准备配置，也**不需要建库建表**。建库、建表和补列由框架在首次数据访问时自动完成。

```bash
cd examples/01-simple-shop/main
go build -o simple-shop . && ./simple-shop -view 8888
```

启动后有两个入口：

| 入口 | 地址 | 说明 |
| :--- | :--- | :--- |
| 管理后台 | `http://127.0.0.1:8888` | 开发视图（HtmlServer），内嵌 `web/admin`，自动走 TestToken 登录。**先从这里用界面走通** |
| 业务 API | `http://127.0.0.1:8081` | 商城服务直连端口，脚本和集成测试打这里 |

> [!TIP]
> `-view` 指定开发管理后台的端口，**默认 `80`**（属特权端口，普通用户无法绑定，所以示例显式用 `8888`）。`-view 0` 表示不启用视图服务，**只在正式部署时使用**——关闭后没有管理后台，也没有 `/api/web/bootstrap`。

首次启动的两个常见现象：

| 现象 | 原因与处理 |
| :--- | :--- |
| 后台打不开，日志有 `service_start_failed`、`port already in use` | server 默认占 `8080`、内部 gRPC 默认占 `18080`。用 `-p` 和 `-grpc` 换端口，例如 `-view 8888 -p 8099 -grpc 18099`。**任一服务未就绪，开发视图就不会开始监听**，所以端口冲突会表现为后台打不开 |
| 刷出若干 Redis 连接失败日志 | 未部署 Redis，会自动降级为进程内事件（`mq_degraded`），不影响本示例 |

### 用管理后台走通

打开 [http://127.0.0.1:8888](http://127.0.0.1:8888)。开发模式下权威服务走 `test_token`，页面会自动签发管理令牌，不用先 curl。

| 界面 | 位置 | 能做什么 |
| :--- | :--- | :--- |
| 菜单管理 | 左侧「内部系统管理」→「菜单管理」→ 工具栏「更新菜单」 | 把当前进程里所有 Manage API 同步成侧栏菜单。本示例会立刻出现商品管理、订单管理，点进去就能增删改查，不必手写 Manage 路径 |
| OpenAPI / Swagger | 侧栏**左下角**「OpenAPI 文档」，或直接打开 [http://127.0.0.1:8888/swagger/](http://127.0.0.1:8888/swagger/) | 覆盖全部 Public 和 Private 接口。本示例可在页面上查商品、带用户令牌下单、查本人订单 |

> [!NOTE]
> Manage 路由**不进** OpenAPI，只通过菜单使用。Public / Private 才出现在 Swagger。Swagger 里调 Private 接口时，用页面上的 Authorize，令牌从 `/api/servermanage/testtoken?userid=user-a` 取 `data.access_token`（`type=0` 用户 / `1` 管理 / `2` 服务管理）。

完整接口清单与 WebSocket 订阅见 [最简商城示例](./examples/01-simple-shop)。

也可以用命令行走同一条 Public / Private 路径（Manage 仍建议走后台菜单）：

```bash
BASE=http://127.0.0.1:8081

# 1. 匿名查询商品（Public，无需令牌）
curl "$BASE/api/shop/getproducts"

# 2. 取测试令牌（type=0 用户 / 1 管理 / 2 服务管理）
TOKEN=$(curl -s "$BASE/api/servermanage/testtoken?userid=user-a" | jq -r .data.access_token)

# 3. 用令牌下单（Private，身份只来自令牌，不从请求体读）
curl -X POST "$BASE/api/shop/addorder" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"productID":1,"quantity":2}'

# 4. 查本人订单
curl "$BASE/api/shop/getorders" -H "Authorization: Bearer $TOKEN"
```

两个容易踩空的细节：令牌接口返回的 `data` 是对象，要取 `data.access_token` 而非整个字符串；路由默认是 `POST`，声明 `router.WithMethod(http.MethodGet)` 才是 `GET`。

> [!WARNING]
> `TestToken` 无需登录即可签发 JWT，仅接受本地 IP 请求。生产环境必须在网关屏蔽 `/api/servermanage/*`，并改用真实认证流程。

---

## 服务骨架

启动组合根 14 行：

```go
func main() {
	server := run.NewWebServer()
	server.AddIService(&simpleshop.ShopService{}, &types.ServerOption{IsWebSocket: true})
	server.Start()
}
```

服务只负责装配路由，管理操作由 Manage 展开：

```go
func (own *ShopService) ServiceName() string { return contract.ServiceName }

func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 11)
	routers = append(routers, manage.NewProductManage().Routers()...) // 7 个标准 CRUD 操作
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

每个 API 实现 `IRouter` 的四个方法，职责固定——`Parse` 绑定参数，`Validation` 校验与默认值，`Do` 执行业务，`RouterInfo` 声明路由：

```go
func (own *Ping) Parse(req types.IRequest) error      { return req.Bind(own) }
func (own *Ping) Validation(req types.IRequest) error { return nil }
func (own *Ping) Do(req types.IRequest) (interface{}, error) {
	return map[string]string{"message": "pong"}, nil
}
func (own *Ping) RouterInfo() *types.RouterInfo { return router.DefaultRouterInfo(own) }
```

---

## 能力地图

按任务定位入口。「模板」列的示例都是可独立运行的完整应用，直接照抄结构即可。

| 我要做的事 | 用什么 | 模板 | 怎么验证 |
| :--- | :--- | :---: | :--- |
| 开放匿名查询接口 | `api/public` 包 + `IRouter` | [01](./examples/01-simple-shop) | `go test ./examples/integration/01-simple-shop -run Public` |
| 开放登录后接口 | `api/private` 包 + `req.GetUser()` | [01](./examples/01-simple-shop) | `go test ./examples/integration/01-simple-shop -run Private` |
| 给模型配一套后台 CRUD | `manage.NewManageService[T](own)` | [01](./examples/01-simple-shop) | `go test ./service/manage` |
| 建表、加字段 | **无需操作**，首次数据访问时自动完成 | [models 分片](./docs/ai/core-skill/models.md) | 启动后查看表结构 |
| 实时推送给用户 | `RouterInfo.RegisterWebSocketClient` / `NoticeWebSocket` | [01](./examples/01-simple-shop) | `go test -race ./examples/integration/01-simple-shop -run WebSocket` |
| 跨模型事务、状态机 | `IDataAction` 事务边界 + business 层 | [02](./examples/02-shop-payment) | `go test ./examples/integration/02-shop-payment` |
| 模型与 Manage 建继承层 | `BaseDataModel` / `BusinessModel` 双支路 | [03](./examples/03-shop-inheritance) | `go test ./examples/integration/03-shop-inheritance` |
| 给接口加结果缓存 | `RouterInfo.UseCache` + `IRouterCacheKey` | [04](./examples/04-shop-performance) | `go test ./pkg/server/routecache` |
| 扛高并发写入 | `ReliableWriteStore[T]` + `UseWriteBehind` | [04](./examples/04-shop-performance) · [07](./examples/07-shop-order-scale) | `go test ./pkg/persistence/database/nosql` |
| 接第三方登录与权限 | Casdoor Auth / Manage 双认证域 | [05](./examples/05-shop-casdoor-rbac) | `go test ./examples/integration/05-shop-casdoor-rbac` |
| 拆多服务并互相调用 | `router.WithInternalCallers` + `req.CallService` | [06](./examples/06-shop-microservices) | `./scripts/test.sh integration-shop-microservices` |
| 同一服务多副本扩容 | `AutoMachineID` + 共享权威库 + `sc.UseOutbox` | [07](./examples/07-shop-order-scale) | `go test ./examples/integration/07-shop-order-scale` |
| 做经营分析与报表 | `stats.StatSpec`、`stats.RegisterReports` | [07](./examples/07-shop-order-scale/order-service) | `go test ./pkg/persistence/entity/stats` |
| 按租户 / 分库路由数据 | `IDBName` + MySQL `Database=""` | [manage 分片](./docs/ai/core-skill/manage.md) | `go test ./service/manage/...` |
| 查看 API 契约文档 | `GET /api/openapi` | — | `./scripts/test.sh release-contract` |
| 查看运行拓扑与调用量 | `POST /api/servermanage/runtimetopology` | [07](./examples/07-shop-order-scale) | 见运行观测文档 |

> [!IMPORTANT]
> **配置字段存在不代表能力可用**，以校验、factory、启动链和行为测试为准。成熟度分级（Stable / Conditional / Experimental / Unsupported）、必需配置和完整场景矩阵见 [框架场景使用指南](./docs/codex/FRAMEWORK_USAGE_GUIDE.md)。

---

## 能力阶梯

七个示例是一条递进阶梯，覆盖同一个商城业务域，因此可以直接对比「多一项能力要多写多少代码」。

| 示例 | 在前一级之上新增 | 业务代码 |
| :--- | :--- | ---: |
| [01-simple-shop](./examples/01-simple-shop) | 模型、Manage CRUD、Public/Private、JWT 鉴权、SQLite、订单 WebSocket | 789 行 / 16 文件 |
| [02-shop-payment](./examples/02-shop-payment) | API / business / models 分层，跨模型事务，支付状态机，Manage 自定义命令 | 2224 行 / 35 文件 |
| [03-shop-inheritance](./examples/03-shop-inheritance) | 模型与 Manage 双继承层，基础资料与业务事实分支，只读子表 | 2936 行 / 49 文件 |
| [04-shop-performance](./examples/04-shop-performance) | 查询分层缓存、下单事实缓存、Group Commit 可靠写、写后同步 | 3861 行 / 54 文件 |
| [05-shop-casdoor-rbac](./examples/05-shop-casdoor-rbac) | Casdoor 登录，Auth / Manage 双认证域隔离，权限矩阵 | 3727 行 / 58 文件 |
| [06-shop-microservices](./examples/06-shop-microservices) | 三服务拆分，Redis 发现，gRPC/mTLS，受限内部 Public，可靠事件，本地投影 | 6261 行 / 120 文件 |
| [07-shop-order-scale](./examples/07-shop-order-scale) | 订单服务多副本水平扩展，AutoMachineID，共享权威库，Outbox，服务报表 | 6266 行 / 106 文件 |

<sub>行数只统计业务代码，不含测试。</sub>

---

## 核心约定

### 路径由包目录决定

`public` / `private` 不出现在 URL 里：

| 类型 | 路径 | 认证 |
| :--- | :--- | :--- |
| 公开 | `/api/{service}/{structLower}` | 无，`api/public` 包 |
| 私有 | `/api/{service}/{structLower}` | 需 JWT，身份只能来自 `req.GetUser()` |
| 管理 | `/api/manage/{service}/{manageLower}/{operationLower}` | Manage 认证域 |
| 服务管理 | `/api/servermanage/{structLower}` | ServerManage 认证域，默认仅本地 IP |

### 模型先按数据生命周期分两类

两类建立平行继承支路，业务事实不得继承基础资料支路：

| 类别 | 特征 | 基类与支路 | 操作方式 |
| :--- | :--- | :--- | :--- |
| 基础资料 | 稳定、增长可预期，如商品、商户、订单类型 | `ServiceModel → BaseDataModel → Product` | 稳定 Code、启停、引用保护 |
| 业务事实 | 持续产生、规模不可预知，如订单、支付流水 | `ServiceModel → BusinessModel → Order` | 幂等、状态机、受控命令、分页归档 |

`entity.Model` 是中性持久化根；只有天然具有稳定 `Code` 语义的资料才用 `entity.BaseModel`（其 `GetHash()` 基于 `Code`，误用会哈希冲突）；业务单据按需选 `BaseOrderModel` / `BaseRecordModel`。嵌入模型指针的类型必须在 `NewModel()` 里初始化完整继承链。

### 其余固定约定

- Manage 用 `entity.NewModelList[T](nil)` 保留标准筛选、排序和分页；public/private 路由调用模型封装的 `IDataAction` 方法，不直连数据库。
- `SearchWhere` 默认最多 500 条，分页用 `SearchAll(page, size)`，不要靠放大上限掩盖无界查询。
- Manage 生成的 `ViewModel` 同时描述字段、命令和子表，内嵌管理后台 `web/admin` 据此渲染界面，无需为每个模型写前端。
- 自定义 manage 操作必须**值嵌入** `manage.Operation[T]`。

详见 [模型分片](./docs/ai/core-skill/models.md) · [Manage 分片](./docs/ai/core-skill/manage.md) · [路由与 DTO 分片](./docs/ai/core-skill/routing-and-dto.md)。

---

## 为什么适合 Agent 开发

**规范只有一份，并随代码演进。**　全部开发规范集中在 [docs/ai/core-skill](./docs/ai/core-skill/SKILL.md)：一个索引加 12 个主题分片。Codex、Claude、Cursor、GitHub Copilot 各自的 skills 目录下只有指向它的指针文件，不存在会互相漂移的副本。`./scripts/ci.sh required/ai-skill` 在 CI 中校验索引完整、分片不超体积上限、指针有效。

**约定消除自由发挥的空间。**　Agent 最容易出错的地方是「同一件事有多种写法」，[核心约定](#核心约定)里的位置全部被固定死：包路径决定认证域、建表补列自动完成、一次 `NewManageService[T]` 出七个标准操作。

**产出可自检。**　`GET /api/openapi` 输出业务契约，`GET /api/servermanage/testtoken` 签发三类认证域的测试令牌，Agent 写完即可自行发真实请求验证，不必等人工联调。

---

## 消费方接入 Skill

> [!IMPORTANT]
> `go get` **只安装 Go 模块**，不会把 Agent skill 装进业务仓库。当 `go.mod` 依赖 `github.com/digitalwayhk/core`，或任务要求「按 Core 规范」开发时，Agent **应先检测再安装**，不要因找不到 skill 就凭记忆编码。

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

1. 消费方仓库内 `.codex/skills/use-digitalway-core/SKILL.md`（指针，只给出权威源路径）
2. 权威源 `.codex/skills/core-skill/SKILL.md`，以及「主题分片索引」中按任务需要的分片
3. Core 源码里的 `docs/codex/*` 和最近 `examples/*`

Claude、Cursor、Copilot 的 skills 目录同构安装，各自指针都指向同一份权威源。脚本加 `--write-agents` 可自动向消费方 `AGENTS.md` / `Claude.md` 追加标准段落。完整说明、软链/复制、用户级安装与故障排查见 [消费方 AI Skill 安装与识别](./docs/codex/CONSUMER_AI_SKILL_SETUP.md)；本仓库自身的 Agent 约定见 [AGENTS.md](./AGENTS.md)。

---

## 高性能写路径

示例 04 与 07 的写优化都建立在「本地可靠确认 + 最终写回」上，由框架统一实现，业务只提供汇合适配器：

- **Group Commit**　下单请求最多合并 1 毫秒或 128 个并发为一次 Badger 事务，请求在所属批次完成 `SyncWrites` 之后才返回。这不是「内存入队即成功」，已确认的订单在进程异常退出后仍可恢复。
- **可靠写存储**　`ReliableWriteStore[T]` 封装本地持久写、批量提交、准入背压、pending 计数与实例目录隔离；`UseWriteBehind(WriteBehindTarget)` 绑定一次远端汇合目标，之后的 ACK、重试、磁盘指标和关闭排空都由框架处理。
- **查询侧缓存**　L1 按进程有效内存的 2% 自动解析字节预算（下限 16 MiB、上限 256 MiB），可选 Badger L2，同键冷加载用 SingleFlight 合并；数据变更后由事件主动失效，TTL 只兜底。

> [!CAUTION]
> write-behind 是 **at-least-once**，远端 insert/update/delete 必须靠稳定主键或 upsert 保证幂等。同 key 多次更新会合并为最新状态，只适用于账户快照、资料和订单当前状态；资金流水、审计记录等**不可合并事件**必须走 transactional outbox 或带唯一事件 ID 的 JetStream。

---

## 多服务与水平扩展

- **内部调用**　只供其他服务调用的 Public API 声明 `router.WithInternalCallers("shop-user", ...)`。调用方身份只能来自同进程 ServiceContext 或已验证的 mTLS 证书 SAN，不接受 HTTP Header 或请求体自报。调用方直接构造目标服务已注册的 API 并用 `req.CallService`，不另建地址型 client。
- **传输与发现**　内部同步调用默认走 gRPC，服务发现用 ServiceResolver（Redis / etcd / Consul），异步事件统一走 EventBridge。
- **扩容方式**　数据库按业务域拆分，不按技术实例拆分。同一逻辑服务的多个副本共享一个远程权威库，每个副本持有独立的本地 pending store 用于可靠接收、崩溃恢复和批量同步；`AutoMachineID=true` 在扩容时自动分配唯一实例编号。
- **运行观测**　运行拓扑与请求聚合通过 `POST /api/servermanage/runtimetopology` 和 `runtimeservice` 查询，历史指标源是 go-zero / Prometheus。

---

## 统计与报表

统一入口是 `pkg/persistence/entity/stats`，完整模板见 [07 订单服务](./examples/07-shop-order-scale/order-service)，实现契约见 [业务统计与报表分片](./docs/ai/core-skill/stats-and-reports.md)。最小接入流程：

1. 用 `stats.StatSpec` 声明事实表、时间粒度、维度和 `count|sum|avg` 指标，启动期调用 `stats.Register`。
2. 为服务创建独立 `stats.Store`，注入 OLTP 或 ClickHouse `StatsEngine`，由后台 Runner 定时调用 `stats.RefreshWithEngine` 刷新快照。
3. 注册 Manage 接口 `POST /api/manage/{service}/analysis`，返回标准 `stats.Dashboard`。
4. 用 `stats.RegisterReports` 注册服务报表，再注册 `POST /api/manage/{service}/reports` 和 `/reports/view`。
5. 执行菜单同步后报表挂在对应服务子菜单，前端路径为 `/report/{service}/{code}`。

`ReportDef` 只定义报表展示与 `StatSpec` 的绑定，不会自动创建事实数据、Runner 或 API；analysis/reports 应只读统计快照，不得在请求路径临时扫描业务表。同一进程承载多个服务时，Spec Code 必须带稳定业务前缀，每个服务使用独立 Store。这些接口属于 Manage 认证域。

> [!WARNING]
> **消费项目版本要求**：分析与报表能力当前位于 `core-web-admin` 开发分支，尚未进入稳定 tag。不能仅执行 `go get ...@latest` 后假定可用；正式消费应等待包含该能力的发布版本，或在明确评审和锁定提交的前提下使用对应开发版本。不得通过复制 Core 实现或长期 `replace` 伪装成已发布能力。

---

## 安全与配置

- CORS 开启时必须显式配置 origin；`*` 仅在调用方主动选择时允许。
- `TrustedProxies` 默认空，忽略 `X-Forwarded-For` / `X-Real-IP`；反向代理部署必须配置可信 IP/CIDR。
- 配置文件和迁移结果使用 `0600` 权限；不支持的能力在启动前 fail closed。
- 运行时日志统一用 go-zero `logx`，不得记录 token、TOTP、完整请求/响应、payload、SQL 或参数值。

当前支持范围见 [配置到运行时能力矩阵](./docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md)，日志级别、字段和错误归属规则见 [日志审计与规范](./docs/codex/LOGGING_AUDIT_AND_STANDARD.md)。

---

## 兼容与废弃

公共 API 按 additive 方式演进。已废弃但仍可编译的表面统一登记在 [废弃 API 登记](./docs/codex/DEPRECATION_REGISTER.md)，逐条给出替代入口、最早删除版本、Owner 和迁移证据。

表中「最早删除版本」是下限而非自动删除指令：删除前还需仓库内调用清零、已登记消费方完成迁移、CHANGELOG Removed 段完整。

> [!TIP]
> 升级 Core 版本前，先对照废弃登记表检查自己用到的入口。

---

## 测试与门禁

```bash
./scripts/test.sh quick                        # 日常自检
./scripts/test.sh server                       # 服务层
./scripts/test.sh release-contract             # 公共 API / OpenAPI 契约
./scripts/test.sh integration-external-docker  # 需要外部依赖
```

CI 门禁由 `scripts/ci.sh` 按名称执行：

| 门禁 | 作用 |
| :--- | :--- |
| `required/quick` | 日常快速回归 |
| `required/contracts` | 配置、公共 API 与发布契约 |
| `required/ai-skill` | 校验 AI skill 权威源结构，防止 agent 目录回流全文副本 |
| `required/server-manage` | ServerManage 与 Manage 行为 |
| `required/race` | 竞态检测 |

外部依赖测试默认跳过，通过显式环境变量或 Docker Compose 启用。

---

## 文档地图

| 想了解 | 看这里 |
| :--- | :--- |
| 完整开发规范（唯一权威源） | [docs/ai/core-skill/SKILL.md](./docs/ai/core-skill/SKILL.md) |
| 场景矩阵、成熟度、必需配置 | [FRAMEWORK_USAGE_GUIDE.md](./docs/codex/FRAMEWORK_USAGE_GUIDE.md) |
| 配置字段到运行时能力的对应 | [CONFIG_RUNTIME_CAPABILITY_MATRIX.md](./docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md) |
| MQ 消息生命周期、安全回收与 Provider 扩展 | [MQ_MESSAGE_LIFECYCLE_GUIDE.md](./docs/codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md) |
| 消费方安装 skill | [CONSUMER_AI_SKILL_SETUP.md](./docs/codex/CONSUMER_AI_SKILL_SETUP.md) |
| 废弃 API 与迁移 | [DEPRECATION_REGISTER.md](./docs/codex/DEPRECATION_REGISTER.md) |
| 日志规范 | [LOGGING_AUDIT_AND_STANDARD.md](./docs/codex/LOGGING_AUDIT_AND_STANDARD.md) |
| 版本变更 | [CHANGELOG.md](./CHANGELOG.md) |

---

## 许可证

Digitalway Core 使用 [Apache License 2.0](./LICENSE) 开源。

---

<div align="center">
<sub>技术栈：go-zero · GORM · gRPC · Badger · Redis / NATS · Casdoor · Umi + Ant Design Pro</sub>
</div>
