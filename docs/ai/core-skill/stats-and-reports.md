# 业务统计、经营分析与服务报表


框架包 `pkg/persistence/entity/stats` 把“事实聚合”“分析看板”“服务报表”分成三层。标准模板是 `examples/07-shop-order-scale/order-service`。

| 层 | Core 能力 | 业务服务必须完成 |
| --- | --- | --- |
| 统计快照 | `StatSpec`、`StatsEngine`、`RefreshWithEngine`、`Store` | 声明事实/时间/维度/指标，安装数据引擎，启动定时刷新 |
| 经营分析 | `stats.Dashboard` 标准响应契约 | 把本服务快照组装为 Dashboard，注册 Manage `analysis` API |
| 服务报表 | `ReportDef`、`BuildReportView`、报表菜单生成 | 注册 ReportDef，注册 Manage `reports` 目录与 `reports/view` API |

这不是 Manage CRUD 的替代品，也不是零接线自动生成器。Manage 列表继续使用 `ModelList`；统计任务聚合权威事实表并覆盖写快照；analysis/reports API 只读快照，不在请求路径临时扫描事实表。

### 1. 声明并注册统计 Spec

每份 `StatSpec.Code` 必须在进程内全局唯一。当前支持 `year|quarter|month|week|day` 粒度及 `count|sum|avg` 指标；指标值在 JSON 中使用 decimal 字符串，避免精度丢失。

```go
package orderstats

import (
	"github.com/acme/shop/order/models/transaction"
	corestats "github.com/digitalwayhk/core/pkg/persistence/entity/stats"
)

var OrderByDay = corestats.StatSpec{
	Code:      "order.by_day",
	Fact:      &transaction.Order{},
	TimeField: "CreatedAt",
	Grain:     corestats.GrainDay,
	Title:     "订单按天汇总",
	Metrics: []corestats.StatMetric{
		{Kind: corestats.MetricCount, Alias: "row_count"},
		{Kind: corestats.MetricSum, Field: "TotalAmount", Alias: "amount_sum"},
	},
}

func init() {
	corestats.Register(OrderByDay)
}
```

维度有两种展示来源：

- 同库基础资料：设置 `BaseModel`；`DisplayFields` 为空时默认读取 `Name`。
- 跨服务快照字段：设置 `DisplayFromFact`，并用 `NoDisplay=true` 禁止查询不存在于本库的基础 model。

不要让统计任务跨服务同步查维度名称。跨服务展示字段应在写入事实时保存快照，或通过可靠事件维护本地投影。

### 2. 安装引擎并刷新服务专属 Store

OLTP 默认支持当前 Gorm 连接的 MySQL/SQLite 聚合。服务启动时注入能取得权威库 `IDataAction` 的工厂，并让 Runner 周期调用 `RefreshWithEngine`：

```go
var OrderStatsStore = corestats.NewStore()

func init() {
	oltp := corestats.NewOLTPEngineFunc(models.RemoteDataAction)
	corestats.SetEngineConfig(corestats.DefaultEngineConfig())
	corestats.SetEngine(oltp, oltp)
}

func refresh(ctx context.Context) {
	opt := corestats.ExecOptions{Range: corestats.QueryRange{
		From: time.Now().UTC().Add(-90 * 24 * time.Hour),
		To:   time.Now().UTC().Add(time.Second),
	}}
	for _, spec := range corestats.All() {
		if !strings.HasPrefix(spec.Code, "order.") {
			continue
		}
		_, _ = corestats.RefreshWithEngine(
			ctx, OrderStatsStore, corestats.CurrentEngine(), spec, opt,
		)
	}
}
```

注册表和默认引擎是进程级全局对象；同一进程承载多个服务时，`Code` 必须带稳定业务前缀，Runner 必须筛选本服务的 Spec，每个服务使用独立 `Store`。不要让一个服务的 API 返回另一个服务的快照。

环境变量：

| 变量 | 值 | 默认 |
| --- | --- | --- |
| `CORE_STATS_ENGINE` | `oltp` / `clickhouse` | `oltp` |
| `CORE_STATS_FALLBACK_OLTP` | ClickHouse 失败是否回退 OLTP | `true` |
| `CORE_STATS_AUTO_ENSURE` | 刷新前是否 Ensure | `true` |

ClickHouse 只负责聚合视图与读取；事实表从 OLTP 进入 ClickHouse 仍需 CDC 或批量 Ingest。可先用 `CompileAllClickHouse` 离线预览计划，再注入 `ClickHouseEngine`。

### 3. 注册经营分析 Manage API

每个业务服务提供：

```text
POST /api/manage/{service}/analysis
```

路由必须是 `ManageType`、`WithAuth(true)`，并按本服务管理员规则校验；响应返回非 nil 的 `stats.Dashboard`。Dashboard 的标题、卡片、趋势、排名、分类和 `Layout` 都由服务端动态下发，Admin 不应写死某个业务的指标名。

推荐请求体支持 `refresh` 与时间/粒度条件。`refresh=true` 只触发 Runner 的刷新入口并设置短超时；真正聚合仍在任务层。没有快照时也返回结构完整的空 Dashboard，不返回伪造指标。

原始快照诊断接口不是前端必需，但服务可按示例提供：

```text
POST /api/manage/{service}/bizstats/query
```

该接口只读服务专属 Store；`code` 为空返回本服务快照，指定 code 返回 ready/snapshot。它同样属于 Manage 认证域。

### 4. 注册服务报表和 API

`ReportDef.Code` 在服务内唯一，`SpecCode` 必须指向已注册且会写入同一 Store 的 `StatSpec.Code`：

```go
func init() {
	corestats.RegisterReports(corestats.ReportDef{
		Code:          "order-daily",
		Service:       contract.OrderServiceName,
		Title:         "订单日趋势",
		MenuTitle:     "日趋势报表",
		Kind:          corestats.ReportKindLine,
		SpecCode:      "order.by_day",
		MetricAliases: []string{"row_count", "amount_sum"},
		MetricTitles:  map[string]string{"row_count": "订单笔数", "amount_sum": "金额"},
		Sort:          10,
	})
}
```

支持 `bar|line|pie|table|mixed`。`DimAlias` 绑定维度；`MetricAlias`/`MetricAliases` 绑定指标；可用 `DrillReportCode` 下钻同服务报表，用 `LinkManagePath` 跳回 Manage 列表。

业务服务还必须注册两个 Manage API：

| 路径 | 标准实现 |
| --- | --- |
| `POST /api/manage/{service}/reports` | 返回 `stats.ListReportMenus(service)` |
| `POST /api/manage/{service}/reports/view` | 校验 code 后返回 `stats.BuildReportView(serviceStore, service, code)` |

`RegisterReports` 不会自动创建 API、刷新任务或事实数据。`BuildReportView` 只转换 Store 中已有快照；快照缺失时返回 `Empty=true` 与提示信息。

菜单同步会读取已注册 `ReportDef`，在对应服务目录下为每张报表生成一项，前端路径固定为 `/report/{service}/{code}`，并复用 reports 两个 Manage API 的权限。因此：

1. 报表定义包必须在服务启动和菜单同步前被 import。
2. reports 两个 Router 必须加入该服务路由。
3. 执行菜单同步/更新后再检查侧栏；不要手工维护同名报表菜单。
4. 后端 Core 与嵌入的 `web/admin` 必须来自包含同一报表契约的版本。

### 5. 最低验收

- `StatSpec`：缺字段、重复 Code、维度展示、MySQL/SQLite 编译与聚合结果。
- Runner：只刷新本服务前缀、首次立即刷新、停止、刷新失败保留旧成功快照。
- analysis：无快照、已有快照、手动刷新超时、Manage 权限。
- reports：目录排序、未知 code、空快照、图/表/排名映射、钻取和 Manage 跳转。
- 菜单：一份 `ReportDef` 只生成一行，旧 reports API 伪菜单会被清理。
- 消费仓先确认 `go.mod` 所用 Core 版本确实包含 `pkg/persistence/entity/stats` 和匹配 Admin；若稳定 tag 尚未发布，不得把仅分支可用描述成已发布能力，也不要用复制框架代码或 `replace` 伪装升级。

