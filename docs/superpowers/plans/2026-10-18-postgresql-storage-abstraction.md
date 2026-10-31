# PostgreSQL 支持与存储边界收拢实施计划

> **执行方式：** 使用 `superpowers:executing-plans`，在主线程按任务顺序实施；实现后由独立审查上下文验收。用户已授权实施与本地 Docker 测试，未授权提交或推送。

**目标：** 同一套业务 Model、Manage 和统计定义，仅调整 models 持久化组合根的存储配置，即可使用 SQLite、MySQL 或 PostgreSQL。

**架构：** models 组合根选择数据访问器，框架 persistence 封装驱动、连接池、SQL 方言、结构初始化、事务和错误。Manage 使用无参数 `models.NewManageModelList[T]()`；统计层提交与数据库无关的聚合描述，不识别数据库类型。Core 控制面通过相同的持久化工厂装配存储。

**技术栈：** 当前 Go/GORM、`gorm.io/driver/postgres` 及其 pgx 驱动、现有 `IDataAction`、ModelList、可靠写和真实进程集成测试。

**分析基线：** 分支 `feat/postgres-support`，HEAD `9f5d7d0372c985aac3b1b7abfd11cda60e3a85f4`；分析开始时工作区干净。实施与测试结果见 [验收记录](../../codex/POSTGRESQL_ACCEPTANCE_2026-10-18.md)。

---

## 一、约束与完成标准

- 数据库选择只进入 models 的持久化组合根或 Core 控制面的内部存储组合根；不进入业务编排、Manage Hook、统计任务或报表 API。
- 不要求每个业务 Model 自己实现 SQL，也不在上层增加 `switch mysql/postgres`。
- 不给现有 `IDataAction` 增加必选方法，避免破坏消费方自定义实现。
- 不改变默认 SQLite、旧配置的 `models` 库兼容行为、HTTP 路径和 DTO。
- 新增 PostgreSQL 初始化能力必须遵守启动前 `EnsureStorage()` 屏障；仅自动建库、建表、补列与补索引，不自动删列、改类型或回填数据。
- 同一模型绑定的存储负责所有标准查询与聚合；未知驱动或不支持的能力返回明确错误，不假冒 MySQL。
- 数据库类型切换不等于历史数据搬迁。本次不实现 MySQL→PostgreSQL 数据迁移、CDC、分区或 PostgreSQL 专项性能优化。
- ClickHouse 的既有引擎选择与公开接口保持兼容；本次收拢三种 OLTP 的差异，不扩建分析库或事实入库链路。

最终验收须同时包含：三库 CRUD/事务契约、Manage/RBAC、相同统计定义、可靠写恢复、真实 HTTP/进程和兼容门禁。仅驱动连通或 CRUD 成功不能标记完整支持。

## 二、文件与职责

以下新增文件名为计划目标；执行时允许按实际代码拆分小型私有辅助文件，但不得扩大层间依赖。

| 文件或目录 | 责任 |
| --- | --- |
| 新增 `pkg/persistence/types/storage_config.go` | 数据库连接配置、驱动默认值和校验，不依赖 server 或业务包 |
| 新增 `pkg/persistence/database/oltp/factory.go` | 根据存储配置构造 SQLite/MySQL/PostgreSQL action |
| 新增 `pkg/persistence/types/aggregate.go` | 不含 SQL/GORM/驱动的聚合请求、结果与可选能力契约 |
| 新增 `pkg/persistence/database/oltp/aggregate.go`、`aggregate_schema.go`、`dialect.go` | 聚合执行、GORM 字段映射及三种数据库方言 |
| 新增 `pkg/persistence/database/oltp/postgres.go`、`postgres_config.go`、`postgres_schema.go` | PostgreSQL action、配置与受限结构初始化 |
| 修改 `pkg/persistence/database/oltp/{gorm,connection,error}.go` | 复用通用 CRUD，修正引用方式、缓存身份与错误归一化 |
| 修改 `pkg/persistence/entity/stats/{exec,dialect,schema,engine_oltp}.go` | 将数据库执行细节下沉，保留统计契约与兼容入口 |
| 修改 `pkg/server/config/managestore.go`、`pkg/server/manageauth/controlplane.go` | 保留旧配置外形，委托统一配置校验与工厂 |
| 修改示例 07 的 models 存储组合根及集成夹具 | 可配置共享权威库，业务和 Manage 不增加驱动判断 |
| 修改 `docker-compose.integration.yml`、`scripts/test.sh` 及脚本契约测试 | PostgreSQL 隔离测试环境、运行入口和证据 |
| 修改 `docs/ai/core-skill/` 与兼容、配置、发布文档 | 记录现行能力与消费方使用方式 |

## 三、执行任务

### 任务 1：先锁定现有行为与分层契约

涉及：`internal/compat/manage_model_list_boundary_test.go`、新增 `internal/compat/storage_boundary_test.go`，以及现有 stats、config、controlplane 测试。

- [x] 运行现有基线并记录退出码、失败用例和跳过项，不把既有失败算作本次通过。
- [x] 增加边界测试：业务/Manage 不引入具体驱动；统计正常刷新不要求传入数据库类型。
- [x] 增加统计能力桩：只提供聚合结果，不提供 GORM 连接；用它验证刷新、快照和报表结果。
- [x] 固定自定义表名、列名、零值更新、hash 唯一冲突、旧配置与公开返回字段的回归样本。

基线命令：

```bash
rtk go test ./pkg/persistence/... ./service/manage/... ./pkg/server/config/... ./pkg/server/manageauth/... ./internal/compat/... -count=1 -timeout=10m
```

预期：明确得到现有基线；新增隔离能力测试在实现前失败，失败原因指向尚未收拢的数据库依赖。

### 任务 2：统一存储配置与构造入口

涉及：新增 `types/storage_config.go`、`oltp/factory.go` 及对应测试；修改 `server/config/managestore.go`、`manageauth/controlplane.go`。

- [x] 定义统一连接配置：Driver、Database、Host、Port、Username、Password、连接池、连接超时，以及 PostgreSQL TLS/时区参数；DSN 使用驱动解析能力，避免手工字符串拆接。
- [x] 新配置按驱动补默认值，PostgreSQL 默认端口 5432；既有 MySQL 与旧 SQLite 配置的有效行为不变。
- [x] 提供 `oltp.NewDataAction(config) (types.IDataAction, error)` 工厂；保留 `NewMySQL`、`NewSqlite`、`NewFixedSqlite` 等现有公开构造器。
- [x] 保持 `ManageStoreConfig` 现有 Go 类型与 JSON 字段；它只转换、委托统一配置，不维护第二套支持列表。`NewControlPlaneAction` 只委托统一工厂。
- [x] 校验未知驱动、缺字段、无效连接池、TLS 配置和口令脱敏；配置校验不建立网络连接。
- [x] PostgreSQL 分支在任务 5 接通前明确返回尚不支持，不能生成可用假象；不提前对外宣称支持。

验证：

```bash
rtk go test ./pkg/persistence/types ./pkg/persistence/database/oltp ./pkg/server/config ./pkg/server/manageauth -count=1
rtk proxy bash scripts/test.sh config-contract
```

### 任务 3：把聚合执行从统计层下沉

涉及：新增 `types/aggregate.go`、`oltp/aggregate.go`、`aggregate_schema.go`、`dialect.go`；修改 stats 执行与兼容文件。

- [x] 定义中性 `AggregateQuery`：事实模型、时间字段、时间粒度、时间范围、维度、基础资料展示字段、指标和返回上限。请求使用 Go 模型字段，不接收 SQL 片段或驱动名称。
- [x] 定义中性 `AggregateRow`：时间桶、维度 ID、展示字段、事实快照字段、十进制字符串指标；不依赖 `entity/stats`，避免循环导入。
- [x] 定义可选 `IAggregateAction.Aggregate(context.Context, AggregateQuery)` 能力；不修改 `IDataAction` 的必选方法。
- [x] 提供 `oltp.Aggregate(ctx, action, query)`：优先调用 action 的聚合能力；旧自定义 action 若通过 `GetModelDB` 暴露 GORM，则由持久化层执行兼容适配；两者均无则明确拒绝。
- [x] 将当前 `compileOLTP`、实际列映射、标识符引用、执行、扫描与基础资料展示查询移到持久化层。展示模型按其数据访问契约解析；不能假定与事实模型在同一个库。
- [x] stats 只规范化/校验统计语义、转换聚合请求、映射 `StatRow`，不导入 PostgreSQL/MySQL 驱动，不探测 `Dialector.Name()`，不拼 OLTP SQL。
- [x] 保留公开 `StatSpec`、`StatRow`、`Exec`、`ExecOptions` 和既有方言符号。旧 `ExecOptions.Dialect` 仅为兼容提示，真实连接为权威；不一致时明确报错并登记行为修正，正常调用无需填写。
- [x] 保留 ClickHouse 编译/刷新依赖的共享辅助能力；GORM 相关实现委托持久化层，不因移动辅助函数破坏现有 ClickHouse 测试。
- [x] 明确 UTC、左闭右开时间窗、日/月/季/年与 ISO 周语义。为跨年周建立样本；现有 SQLite `%W` 与声明的 ISO 周不符时按缺陷修正，记录可见结果变化。

验证：

```bash
rtk go test ./pkg/persistence/entity/stats ./pkg/persistence/database/oltp ./internal/compat -count=1
```

验收：原 SQLite/MySQL 统计与 ClickHouse 回归通过；统计能力桩完全不提供 GORM 仍可完成刷新。未知数据库不再按 MySQL 静默执行。

### 任务 4：收拢通用 SQL 与连接身份

涉及：`oltp/gorm.go`、`types/searchitem.go`、`oltp/connection.go`、新旧 adapter 的缓存键及测试。

- [x] 为 `sqlload` 的自定义查询路径增加测试，替换写死反引号的表名替换逻辑，使用 GORM 子查询/方言引用，保持值参数绑定。
- [x] 对 SearchItem 的筛选、排序、分页、分组、SUM、空值、IN、BETWEEN 和自定义表列名建立同一组契约。
- [x] 新增统一内部连接身份：驱动、端点、数据库、用户及影响连接语义的 TLS/时区参数；内部键不得泄漏原始口令。
- [x] 表缓存按真实存储身份和表名隔离；不能仅以数据库名判断已建表，覆盖 MySQL/PostgreSQL 同名库、不同端点和不同身份。
- [x] 同一存储身份的首次建池合并；Clone 共享池而独立持有语句与事务状态；旧故障句柄不能驱逐新池。
- [x] 保持既有公开存储身份方法的兼容边界；若修正其返回值，补充控制面重复绑定与同步限流的回归及兼容说明。

验证：

```bash
rtk go test ./pkg/persistence/types ./pkg/persistence/database/oltp ./pkg/persistence/database/nosql ./pkg/server/manageauth ./pkg/server/smodels -count=1
rtk go test -race ./pkg/persistence/database/oltp ./pkg/server/manageauth -count=1
```

### 任务 5：实现 PostgreSQL 存储适配器

涉及：新增 PostgreSQL 配置、action、结构初始化、单元/集成测试；修改 go.mod/go.sum、统一工厂与错误分类。

- [x] 使用与当前 GORM 版本兼容的官方 `gorm.io/driver/postgres`，锁定实际依赖版本；不复制整份 MySQL 实现。
- [x] 实现现有 IDataAction/IDataBase 能力及 Clone、GetMaxOpenConns、GetSyncPoolKey；底层复用通用 CRUD、聚合与连接管理。
- [x] 固定库优先；配置 Database 为空时沿用 IDBName 路由，按目标数据库取连接池，不用 session 切库冒充独立数据库；事务固定在首次绑定的数据库。
- [x] 启动时先连接目标库；仅目标库不存在时通过维护库在事务外创建，再建立目标连接。支持已建库但无 CREATEDB 权限的账号；缺库且无权限时启动失败。
- [x] 处理多进程并发建库、建表、补列和补索引；只接受能够确认结构已就绪的并发结果，不吞掉索引失败或权限错误。
- [x] 使用受限 Migrator 操作完成加性初始化，不直接套用可能修改已有类型/约束的完整 AutoMigrate。
- [x] 错误分类使用 pgx 驱动类型/SQLSTATE：唯一冲突归一为现有 409，其余未分类错误保持安全 500，保留 cause；不得字符串包含判断。
- [x] 保持只读连接故障最多重试一次、写入不自动重放、活动事务不换连接的契约。
- [x] 验证 uint/Snowflake 主键的可存范围、decimal 精度、时间精度、bool、NULL、自定义表名、批量参数数量和回读；不宣称 PostgreSQL 支持整个 uint64 范围。

验证：增加 `postgres_test.go`、`postgres_integration_test.go` 与错误契约用例；真实数据库用例使用 integration 构建标记。连接故障、权限与并发 DDL 必须由真实 PostgreSQL 验证。

### 任务 6：验证上层透明与可靠写闭环

涉及：示例 07 models 存储组合根、已有订单/Outbox 持久化路径、07/09 集成测试及新增三库共享夹具。

- [x] 示例 07 的远程库选择集中在 models/internal/store，使用统一工厂；新增通用存储配置入口，既有 `SHOP_ORDER_REMOTE_MYSQL_*` 继续按原语义工作。
- [x] 同一业务 Model/Manage 和 StatSpec，在三库共享 fixture 下验证相同结果；测试参数中可选择驱动，业务代码不分支。
- [x] Core ManageStore 使用 PostgreSQL 跑真实 HTTP：目录/菜单、角色/权限、首管理员唯一性、只读用户 403；确认系统 Manage 无 PostgreSQL 专用逻辑。
- [x] 对“先查再插”的并发幂等和“Insert 失败后同事务重查”增加 PostgreSQL 竞争测试。冲突语句中止事务后，必须通过明确回滚/保存点或持久化能力恢复；不得在已失败事务里假装继续查询成功。
- [x] 幂等 SQL 差异留在持久化能力中，业务只表达幂等键和冲突策略；业务事实及 Outbox 仍同事务，失败不得只写入一部分。
- [x] 跑通本地 pending→PostgreSQL 权威库→Outbox→消费确认；远程断连或 Commit 结果不确定时保留 pending，不提前确认或盲目重放写操作。
- [x] 增加崩溃重启、重复同步、多副本共享权威库与 pending 目录隔离验证；沿用真实 WebSocket 用户投递/隔离测试。

验收：只修改 models 装配配置即可切库；测试观察实际持久化行、事务回滚、幂等记录和 pending 状态，不用 HTTP 200、健康状态或累计计数替代。

### 任务 7：纳入测试入口与发布契约

涉及：Compose、scripts、兼容测试、权威 skill 与现行配置/能力/兼容文档。

- [x] 在 persistence profile 增加固定版本 PostgreSQL、健康检查及隔离端口；测试通过环境变量接入，不隐式依赖开发机服务。
- [x] 接入现有 `integration-persistence`，同时更新 `scripts/test_integration_persistence_test.sh`、Compose/CI 契约测试；清理只操作本次测试 project 的资源。
- [x] 增加独立 PostgreSQL 集成入口，驱动、初始化、聚合、控制面与可靠写用例实际执行；依赖缺失/跳过必须在结果中明确区分。
- [x] 更新 `docs/ai/core-skill/models.md`、`manage.md`、`stats-and-reports.md` 和 `testing-and-release.md`，说明模型选择存储、统一聚合和自动初始化边界。
- [x] 更新 README、`pkg/persistence/README.md`、示例 07 README、CONFIG_RUNTIME_CAPABILITY_MATRIX、API_COMPATIBILITY_SURFACE、DEPRECATION_REGISTER、CONSUMER_COMPATIBILITY_MATRIX、CI_QUALITY_GATE_MATRIX 与 CHANGELOG 中受影响条目；不复制 skill 正文到指针目录。
- [x] 静态检查新增公开 Go API、配置默认值、旧方言兼容入口和发布说明；运行下述门禁，记录 PASS/FAIL/NOT_RUN/跳过项。

```bash
rtk proxy bash scripts/test.sh persistence-unit
rtk proxy bash scripts/test.sh integration-persistence
rtk proxy bash scripts/test.sh config-contract
rtk proxy bash scripts/test.sh api-compat
rtk proxy bash scripts/test.sh public-api
rtk proxy bash scripts/test.sh release-contract
rtk go test -race ./pkg/persistence/database/oltp ./pkg/persistence/entity/stats ./pkg/server/manageauth ./service/manage -count=1
```

真实进程补充验收：07 的单进程与多进程角色闭环、09 的 PostgreSQL 控制面 HTTP/RBAC、WebSocket。执行入口随任务 6/7 一并新增，不能拿现有 MySQL 专用环境变量驱动 PostgreSQL 测试，也不能把跳过的用例记为通过。

## 四、实施顺序与交付

顺序：任务 1 → 2 → 3 → 4 → 5 → 6 → 7。任务 2 的 PostgreSQL 构造分支在任务 5 完成；中间结果不发布为 PostgreSQL 支持版本。

每项采用：失败用例与明确 RED → 最小实现 → 针对性 GREEN → 检查边界与变更范围。确认实现后按“统一装配、聚合下沉、通用 SQL/连接身份、PostgreSQL、集成与文档”形成可审查的提交单元；实际提交/推送按用户后续授权执行，不使用 `git add -A`。

最终交付：实现差异、消费方最小切库示例、三库行为矩阵、真实环境验证结果、兼容影响与未执行项。性能容量不在此次功能完成结论中推定。

## 五、计划自检

- 已覆盖用户确认的 Model/Manage/统计边界，并区分“模型选择”与“底层方言实现”。
- 已覆盖默认配置与外部 IDataAction/统计公开接口兼容，不通过扩充必选接口制造消费方编译破坏。
- 已覆盖建库权限、并发 DDL、连接/表缓存隔离、事务失败状态、Commit 不确定与恢复链路。
- 已限定任务 3 为现有聚合能力下沉，不建立新的通用查询语言，也不扩大 ClickHouse/数据搬迁范围。
- 文中任务已实施；最终命令、证据边界及独立审查修复见验收记录。
