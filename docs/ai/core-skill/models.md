# 模型层与持久化边界

模型层是需求落地的第一层。设计 API 前先完成数据生命周期分类（判定表见 [SKILL.md](SKILL.md)），再选择框架结构体，最后决定访问路径。

## 先按数据生命周期分类

| 分类 | 业务判断 | 典型例子 | 数据与管理特性 |
| --- | --- | --- | --- |
| 基础资料 Model（主数据/原数据） | 相对稳定；集合规模、增长速度可预期；被其他业务长期引用 | 商品、商户/供应商、订单类型、支付类型 | 稳定 ID/Code、启用/禁用、引用保护；被引用后通常不允许物理删除 |
| 业务事实 Model | 必须依赖一个或多个基础资料 ID；由用户业务事件持续产生；数量无上限、增长频率不可预知 | 订单、支付流水、库存流水、结算记录 | 幂等创建、状态机、不可任意编辑/删除；需要分页上限、索引、归档/分区及按需高吞吐写 |

"基础"描述的是数据生命周期，不是继承层级，也不等于 Go 类型名中出现 `Base`。推荐继承树：

```text
entity.Model
└── ServiceModel               # 数据库名、TraceID、租户、DataAction 等公共能力
    ├── BaseDataModel          # 基础资料支路
    │   ├── Product
    │   ├── Supplier
    │   └── OrderType
    └── BusinessModel          # 业务事实支路
        ├── Order              # ProductID/SupplierID/OrderTypeID
        └── PaymentRecord      # OrderID/PaymentTypeID
```

两条支路必须分开：具体模型不能跨支路继承，`BusinessModel` 不能继承 `BaseDataModel`。相同的数据库名、TraceID 等放在中性的 `ServiceModel`，不要把它称为基础资料。基础资料 Manage 可提供 CRUD、启停和引用删除保护；业务 Manage 默认只提供 View/Search，新增来自业务 API，状态变化通过受控命令，禁止通用 Edit/Remove 绕过状态机。

业务事实必须保存关联基础资料的 ID；为保证历史可审计，还应按业务需要保存名称、编码、成交价等快照。基础资料后续改名不能改变历史订单。若 Product 同时承担无限增长的库存或价格历史，应拆成 `Product` 基础资料与 `InventoryRecord`/`PriceRecord` 业务事实。

Outbox、Inbox、审计日志属于基础设施/技术记录，可使用独立 store/audit 支路；它们不是第三种业务主数据，也不能用来取消上述业务域两分法。

## 再选择框架结构体

`entity.Model` 是中性、最小持久化根，不代表"业务 Model"。新业务应把它包装进服务公共基座，再建立两条语义支路；不要让 Product 和 Order 都直接嵌入框架根而失去分类：

```go
type ServiceModel struct {
	*entity.Model
}

type BaseDataModel struct {
	*ServiceModel
}

type Product struct {
	*BaseDataModel
	Name  string
	Price decimal.Decimal
}

func NewProduct() *Product {
	return &Product{BaseDataModel: &BaseDataModel{
		ServiceModel: &ServiceModel{Model: entity.NewModel()},
	}}
}

func (own *Product) NewModel() {
	if own.BaseDataModel == nil || own.ServiceModel == nil || own.Model == nil {
		fresh := NewProduct()
		own.BaseDataModel = fresh.BaseDataModel
	}
}
```

示例 01 的直接嵌入只用于展示最小框架机械能力；同时具有基础资料与业务事实的新项目，以示例 03 的分支结构为准。

具有稳定唯一 `Code`、`Name` 和资料状态语义的基础资料可以使用 `entity.BaseModel`；`BaseModel.GetHash()` 基于 Code。业务事实不要为了复用 State/Code 字段而继承 `entity.BaseModel`：单据可选 `entity.BaseOrderModel`，只追加记录可选 `entity.BaseRecordModel`，其他业务事实从项目 `BusinessModel`/`entity.Model` 建模。

| 框架结构体 | 含义 | 常见业务分类 |
| --- | --- | --- |
| `entity.Model` | 中性最小持久化能力 | 两类的公共根或简单模型 |
| `entity.BaseModel` | Code/Name/State 的资料能力 | 基础资料 Model |
| `entity.BaseOrderModel` | UserID/TraceID、单据不可删除 | 业务事实 Model（单据） |
| `entity.BaseRecordModel` | 只写、不可修改删除 | 业务事实或技术审计记录 |

各基类的校验语义以当前实现为准，不要按名字猜：

| 结构体 | `AddValid` | `UpdateValid` | `RemoveValid` |
| --- | --- | --- | --- |
| `entity.Model` | 恒返回 nil（无校验） | 恒返回 nil | 恒返回 nil |
| `entity.BaseModel` | 要求 ID 非零且 Code 非空 | 要求 Code 非空 | 拒绝 `State > 0` |
| `entity.BaseOrderModel` | 要求 TraceID 与 UserID 非空 | 同 Add | 恒拒绝（单据不能删除） |
| `entity.BaseRecordModel` | 要求 TraceID 非空 | 恒拒绝（数据不能修改） | 恒拒绝（数据不能删除） |

所以"框架会帮我校验 Code"只在 `BaseModel` 及其子类成立；直接嵌 `entity.Model` 的模型必须自己实现 `AddValid`/`UpdateValid`。

先做业务分类，再选结构体；不得从结构体名称反推分类。嵌入指针必须在显式构造器和 `NewModel()` 中初始化：前者供业务代码使用，后者供 `ModelList` 反射创建实例。

## 哈希表达业务唯一性

`GetHash` 不是随机值，应表达真实的业务唯一约束：

- 商品以规范化后的名称生成哈希，因此名称不能重复。
- 订单以 `UserID + ProductID + CreatedAt(UTC 秒)` 生成哈希，因此同一用户同一商品每秒只能创建一次订单。
- 时间参与哈希时，保存值和哈希值必须使用同一精度，不能一个保留纳秒、一个截断到秒。

数据库唯一约束是并发下的最终防线，`AddValid`/`UpdateValid` 仍应提前返回清晰的公开业务错误：

```go
func (own *Product) AddValid() error {
	return own.validate(true)
}

func (own *Product) UpdateValid(_ interface{}) error {
	return own.validate(true)
}
```

校验应同时覆盖字段格式、数值范围和业务唯一性。公开错误使用框架的类型化公开错误，不直接暴露数据库错误文本。

## 建库、建表与字段迁移由框架自动完成

**业务代码不需要、也不允许自建库表。** 这是框架能力，不是留给业务实现的空位。

### SQLite

`Sqlite` 的每个 CRUD 入口都先 `ensureTable`，它转调 `HasTable`：查 `sqlite_master` 判断表是否存在，不存在则 `safeAutoMigrate`（`db.AutoMigrate` 加最多 3 次连接重建重试），结果进 `tableCache` 避免重复检查。错误处理路径还会在遇到 `no such table`、`no such column`、`datatype mismatch` 等错误时再次 `safeAutoMigrate` 并重试一次。嵌套表不递归预建，首次访问时各自触发。

默认库文件路径由 `local.GetDbPath` 决定，形如 `<工作目录>/db/<库名>/<库名>.ldb`。SQLite 默认 mmap 预算为 256 MiB/实例，可通过 `Sqlite.MmapSize` 覆盖，负值关闭；不得恢复机器级 30 GB 默认。

### MySQL

建立连接时 `ensureDatabase` 先查 `INFORMATION_SCHEMA.SCHEMATA`，库不存在则执行 `CREATE DATABASE IF NOT EXISTS`，然后 `USE`。`HasTable` 查 `information_schema.tables`：表不存在则 `Migrator().CreateTable`（并识别并发场景的 `already exists` / `Error 1050`），表已存在则 `safeAutoMigrate` 逐字段补缺失列与索引，随后按最大深度 2 处理嵌套表。`DB_FORCE_MIGRATE=true` 可跳过表缓存强制重新检查结构。

所以切换到 MySQL 不需要 DBA 预建库，也不需要迁移脚本；连上去就会自己建。

MySQL 运行期复用 `database/sql` 连接池句柄，每次 CRUD 前不再额外 `Ping`。连接有效性由真实 SQL 结果判定：

- 同一个 `host:port/database` 的首次建池和故障恢复按连接键合并，只发布一套池；并发调用不得各自创建池后互相替换。
- Clone 报告连接错误时只能驱逐自己仍引用的那套底层池；若其他 goroutine 已发布新池，旧 Clone 不得删除新池。
- 长期服务池不按 ConnectionManager 的查询时间主动关闭；物理空闲连接由 `database/sql` 的 `MaxIdleConns`、`ConnMaxIdleTime` 和 `ConnMaxLifetime` 管理。

- 非事务只读遇到连接级错误时，驱逐失效句柄、重建连接并且最多重试一次。
- 写入的提交结果可能不确定；框架只驱逐失效句柄并返回原错误，不自动重放。上层只能在稳定业务幂等键下决定是否重试。
- 活动事务已绑定专用连接，连接错误时不切换连接、不重放，由事务回滚与业务边界收敛。

不得在业务层为每次数据操作再包一次 `Ping`；这会在连接池饱和时把一条 SQL 放大成两次串行借连接。实际失败与认证口径见 `docs/codex/cases/MYSQL_PER_OPERATION_PING_POOL_AMPLIFICATION.md`。

### 触发时机

建表发生在**首次数据访问**时，不是构造 `ModelList` 时。`entity.NewModelList[T](nil)` 只是创建列表对象；真正建表在首次落到 `DefaultAdapter.getLocalDB` 绑定该库时调用 `HasTable`，以及各驱动 CRUD 入口的 `ensureTable`。框架启动期间 `config.IsServerInitializing()` 为真时会跳过建表，等正式请求再执行。

因此"新增字段后重启即自动补列"要理解为：重启后**首次访问该表**时补列。

### 边界：不会自动做的事

| 变更 | 是否自动 |
| --- | --- |
| 建库、建表 | 是 |
| 新增列、新增索引 | 是 |
| 删除列 | **否** |
| 修改列类型、长度 | **否** |
| 新增/修改约束、外键 | **否**（迁移时显式关闭了外键约束） |
| 数据回填、数据订正 | **否** |

这些破坏性或有数据风险的变更必须走发布流程单独处理，并按 `docs/RELEASE_POLICY.md` 与废弃登记评估兼容性。

### 禁止的做法

- 写 `CREATE TABLE`、`CREATE DATABASE`、`init.sql`、`schema.sql`。
- 建 `migrations/` 目录或引入 golang-migrate、goose 等版本化迁移框架。
- 在业务代码里直接调用 GORM `AutoMigrate` 或 `Migrator()`。
- 在 Docker/Compose 里用初始化 SQL 预建业务库表。

### `models/schema` 是事务前预热，不是 DDL 层

示例 05/06/07 里的 `models/schema` 包（`EnsureStorage`）经常被误读成"框架要求业务建表"。它的实现只是拿模型做一次空 `Load`，借上面的自动建表机制把表提前建好：

```go
// EnsureModel 确保模型表已创建。
func EnsureModel(model interface{}) error {
	ensureMu.Lock()
	defer ensureMu.Unlock()
	t := reflect.TypeOf(model)
	if t == nil || t.Kind() != reflect.Ptr {
		return errors.New("模型类型无效")
	}
	return Get().Load(NewSearch(model, 1), reflect.New(reflect.SliceOf(t)).Interface())
}
```

它存在的唯一理由是事务时序：事务开启后再触发 DDL 会失败，所以 `RunInTransaction(ensureStorage, operation)` 必须先预热。**只有存在这类跨模型事务时才需要 `schema` 包**；示例 01–04 没有该包，完全正常。不要为新服务无条件生成 `models/schema`。

## 模型持久化边界与双路径访问

框架支持多种数据库类型（SQLite、MySQL、PostgreSQL 等）。**SQLite 只是零配置的默认/开发选项**：本地开发与单机测试最简单，无需额外配置即可作为本地库，也可临时当作"远程"权威库；**生产与多进程共享权威库应按 MySQL 等网络库选型**，不是"只能 SQLite"。

推荐在服务**公共模型/持久化组合根**（如 `models/common`、`models/data_action.go`、`models/internal/store`）集中定义明确的 `IDataAction` 获取方法，例如 `LocalDataAction()` / `RemoteDataAction()` / `ManageDataAction()`。后续切换库类型时**只改这些方法**，Manage 与 public/private 调用点保持不变。这里共享的是无请求状态的数据访问能力；模型实例、当前用户、查询条件和响应不得放入单例。

**Manage API 与 public/private 使用数据库的方式不同，不可混用：**

| 路径 | 访问方式 | 适用 |
| --- | --- | --- |
| Manage | `ModelList` + 标准 Search/View/Add… | 管理后台；框架筛选/排序/分页；管理人员配置与查询 |
| public/private | models 业务方法（内部 `IDataAction`）+ 可选 business | **所有**业务读写默认模式（见 01）；API 不直接 `NewModelList` |
| public/private 高吞吐写 | 04/07 专用 store：本地可靠写 + `UseWriteBehind` → 远程权威库 | 下单/支付等需水平扩展或极高 TPS 时再升级，不是简单业务的必选项 |

两者可以**共用同一 model 结构体**，通过服务公共持久化组合根的 DataAction 取连接。库类型（SQLite/MySQL 等）由 DataAction 决定，与「是否 ModelList」正交。

public/private 默认示例（01：语义方法 + `IDataAction`，不是 ModelList）：

```go
product, err := models.NewProduct().FindByID(productID) // 内部 getDataAction().Load(...)
orders, err := models.NewOrder().QueryByUser(userID)
order, err := models.NewOrder().FindOwned(orderID, userID)
err = order.Delete() // 内部 getDataAction().Delete(...)
```

集中 DataAction 示例。框架为 SQLite 提供了进程级共享入口 `entity.GetGlobalSqliteInstance(name)`；MySQL **没有**对应的 `GetGlobalMysqlInstance`，需要显式构造 `oltp.NewMySQL(&oltp.Config{...})`：

```go
var (
	dataActionOnce sync.Once
	dataAction     persistencetypes.IDataAction
)

// LocalDataAction / RemoteDataAction：切换 SQLite→MySQL 只改此处实现。
func LocalDataAction() persistencetypes.IDataAction {
	dataActionOnce.Do(func() {
		dataAction = entity.GetGlobalSqliteInstance(NewProduct().GetLocalDBName())
		// 生产示例（固定库）：
		// dataAction = oltp.NewMySQL(&oltp.Config{
		// 	Host: host, Port: port, User: user, Password: pass,
		// 	Database: "shop", // 固定库；动态分库时此处必须留空，见 manage.md
		// })
	})
	return dataAction
}
```

## `SearchWhere` 的默认行数上限

`ModelList.SearchWhere` 在调用方未主动设置 `Size` 时会把上限设为 500 行，并在实际总数超过 500 时以 `model_search_result_capped` 记一条 `Sloww` 日志。需要更多数据时显式设置 `SearchItem.Size`，或改用分页的 `SearchAll`，不要依赖默认值扫全表。

## 数据库名与动态路由

`types.IDBName` 只有两个方法，且**都只返回 string，没有 error**：

```go
type IDBName interface {
	GetLocalDBName() string  //获取本地数据库的名称
	GetRemoteDBName() string //获取远程数据库的名称
}
```

`entity.Model` 的默认实现让两者都返回 `"models"`，因此未重写时所有模型共用同一个 `models` 库。服务公共模型基座应重写这两个方法给出本服务库名，而不是在每个具体模型上重复声明。

MySQL 解析库名的顺序是：`config.Database` 非空则固定库；否则取 `GetRemoteDBName()`，**为空时回退 `GetLocalDBName()`**；两者都空才用缓存的 `m.Name` 兜底。这个回退行为对动态分库的 fail-closed 设计影响很大，详见 [manage.md](manage.md)。
