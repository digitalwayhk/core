# Digitalway Core

Digitalway Core 是构建 Go 商业服务的应用组装框架。它以 go-zero、GORM 和成熟基础设施客户端为底座，提供统一的路由、模型、管理 CRUD、服务生命周期、集群、传输、MQ、事件桥接和 WebSocket 约定。框架只保留 Digitalway 领域契约，不重复实现通用客户端、日志或并发原语。

## 环境

- Go 版本以 [go.mod](./go.mod) 为准。
- 核心 HTTP、配置、日志和生命周期复用 go-zero。
- SQL 模型契约使用 GORM。
- 外部依赖测试默认跳过，通过显式环境变量或 Docker Compose 模式启用。

```bash
go get github.com/digitalwayhk/core@latest
```

## AI 助手与 Skill（消费方）

`go get` **只安装 Go 模块**，不会把 Agent skill 装进业务仓库。依赖本框架开发或审查后端 API 时，消费方（及其 AI）需要先安装 skill，再按规范写代码。

### AI 自动识别与安装（固定流程）

当消费方 `go.mod` 依赖 `github.com/digitalwayhk/core`，或任务要求「按 Core 规范」开发时，Agent **应先检测再安装**，不要因找不到 skill 中断后凭记忆编码：

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

建议在消费方 `AGENTS.md` / `Claude.md` 中写入上述流程（脚本加 `--write-agents` 可自动追加标准段落）。完整说明、软链/复制、用户级安装与故障排查见 [消费方 AI Skill 安装与识别](./docs/codex/CONSUMER_AI_SKILL_SETUP.md)。

本仓库自身的 Agent 约定见 [AGENTS.md](./AGENTS.md)；规范正文的唯一权威源在 [docs/ai/core-skill](./docs/ai/core-skill/SKILL.md)，各 agent 目录下只有指向它的指针文件。

## 最小服务

路由实现 `types.IRouter` 的 `Parse`、`Validation`、`Do` 和 `RouterInfo`。普通 public/private 路径为：

```text
/api/{service}/{structLower}
```

`api/public` 无需认证；`api/private` 自动要求认证，并通过 `req.GetUser()` 读取身份。完整代码见 [最简商城示例](./examples/01-simple-shop)。

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

## 模型与管理 CRUD

- Model 层是业务设计的第一层。先按数据生命周期拆成两类：商品、商户、订单类型等稳定且增长可预期的数据属于基础资料 Model；订单、支付/库存流水等依赖基础资料 ID、由业务持续产生且规模不可预知的数据属于业务事实 Model。
- 两类模型必须建立平行继承支路：`ServiceModel → BaseDataModel → Product` 与 `ServiceModel → BusinessModel → Order`。服务公共基座只承载数据库名、TraceID 等能力，不是基础资料 Model；业务事实不得继承基础资料支路。
- 基础资料通常使用稳定 Code、启停和引用保护；业务事实保存基础资料 ID 与必要历史快照，使用幂等、状态机、受控命令、分页/索引/归档，不能按普通 CRUD 任意修改删除。
- `entity.Model` 是中性持久化根；只有天然具有稳定 `Code` 语义的资料模型才使用 `entity.BaseModel`，业务单据/记录按需选择 `BaseOrderModel`/`BaseRecordModel`。
- Manage 使用 `entity.NewModelList[T](nil)` 保留标准筛选、排序和分页；public/private 路由调用模型封装的 `IDataAction` 查询与操作方法。
- 嵌入框架或项目模型指针的类型必须在 `NewModel()` 中初始化完整继承链。
- 管理 CRUD 路径为 `/api/manage/{service}/{manageStructLower}/{operationLower}`。

简单模型、Manage CRUD 和私有订单接口见 [最简商城示例](./examples/01-simple-shop)；两类模型与 Manage 分支见 [模型继承示例](./examples/03-shop-inheritance) 和 [模型分片](./docs/ai/core-skill/models.md#先按数据生命周期分类)。

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

完整日志级别、字段和错误归属规则见 [日志审计与规范](./docs/codex/LOGGING_AUDIT_AND_STANDARD.md)。

## 集群、传输与 MQ

能力是否可用由配置校验、运行时 factory 和行为测试共同决定，不能仅依据配置字段存在。当前支持范围和外部依赖接入方式见 [配置到运行时能力矩阵](./docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md)。

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
