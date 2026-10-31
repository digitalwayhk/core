# PostgreSQL 与统一 OLTP 存储

数据库类型只在 models 持久化组合根选择。Manage 继续调用无参数 `models.NewManageModelList[T]()`，业务统计继续声明同一份 `StatSpec`；它们不导入驱动，也不选择 SQL 方言。

## 模型组合根

```go
action, err := oltp.NewDataAction(types.StorageConfig{
    Driver:   "postgres",
    Host:     "127.0.0.1",
    Port:     15432,
    Username: "core_test",
    Password: os.Getenv("APP_DATABASE_PASSWORD"),
    Database: "orders",
    SSLMode:  "disable", // 仅用于显式选择的本机测试连接
})
```

生产连接默认 `SSLMode=require`，需要验证服务器身份时配置 `verify-full` 与 `SSLRootCert`；可选 `SSLCert`/`SSLKey` 是客户端证书和私钥的文件路径。时区默认 UTC。配置校验不连接网络，实际初始化错误由启动入口处理。

SQLite、MySQL 分别使用 `Driver=sqlite|mysql`；原有构造器继续兼容。`StorageConfig.Validate(true)` 要求固定数据库，Core 控制面使用该模式；业务模型显式使用空 Database 时，网络数据库可以按 `IDBName` 解析数据库。动态模型仍须对缺少分库键 fail closed，不跨库执行一个事务。

## Core 控制面

在标准 `etc/server.json` 配置：

```json
{
  "ManageStore": {
    "Driver": "postgres",
    "Host": "127.0.0.1",
    "Port": 15432,
    "Database": "core_manage",
    "Username": "core_test",
    "Password": "由部署配置注入",
    "SSLMode": "disable"
  }
}
```

这是已有 server.json 的配置片段，不是完整配置文件。控制面复用持久化工厂；系统 Manage、管理员模型及 RBAC 不增加 PostgreSQL 专用逻辑。旧配置仍保持默认 SQLite 与历史库名兼容行为。

## 初始化与事务

- 接收请求前在 models 组合根执行 `EnsureStorage()`。连接目标库不存在时，框架通过维护库在业务事务外建库，再连接目标库。目标库已存在时不要求 CREATEDB 权限；缺库且无建库权限会阻止启动。
- PostgreSQL 使用同库事务级 advisory lock 协调根模型及关联表结构初始化；仅创建缺失的表、列和索引。失败索引或权限错误不得作为成功缓存；不自动改列类型、删列或回填数据。
- 未显式指定数据库类型的 `decimal.Decimal` 在 PostgreSQL 新建列时映射为精确 numeric，已有文本列不会自动改型。显式 `gorm:"type:..."` 保留调用方声明。
- Clone 共享连接池而不继承活动事务。唯一冲突保持 HTTP 409 与原始 cause；业务事务中的写入通过保存点恢复失败语句，以支持已有“冲突后重查”的模型幂等路径，之前的事务写入仍保留。
- 普通 Insert 不自动忽略重复、不变成 upsert。连接故障的非事务标准只读查询最多重试一次；写入和 Commit 结果不确定时不自动重放，可靠写仍保留 pending。
- 连接池、表缓存与同步并发标识按真实存储身份隔离，身份包含驱动、端点、库、凭证与连接语义。公开同步键是不可解析的内部身份，不保证文本格式；不包含明文口令。

## 统计与原生扩展

统计 SQL 编译、字段映射、时间桶与执行位于 persistence/oltp。可选 `types.IAggregateAction` 允许存储只暴露中性聚合能力；不增加 `IDataAction` 必选方法。旧 action 暴露 GORM 的兼容路径仍可使用。

`ExecOptions.Dialect` 保留为旧调用兼容提示；正常调用应留空，显式提示与实际连接不一致时返回错误。未知驱动不再当作 MySQL。三种 OLTP 的周桶采用 ISO 周年，例如 2021-01-01 属于 `2020-W53`；SQLite 旧 `%W` 结果随此修正。时间窗口为左闭右开，推荐所有业务时间写入 UTC。

标准 `SearchItem` 与框架统计负责数据库差异。原生 `IDBSQL.SearchSQL`、`Raw`、`Exec` 或自定义 GORM scope 中的 SQL 正文仍是调用方的显式扩展，不承诺自动翻译任意厂商 SQL；框架只保证查询包装、标识符与绑定参数正确。标准批量写入按关联模型最大列数限制批次，避免超过 PostgreSQL 65,535 个绑定参数。

## 示例 07

只在 models 存储组合根配置：

```bash
export SHOP_ORDER_REMOTE_DRIVER=postgres
export SHOP_ORDER_REMOTE_HOST=127.0.0.1
export SHOP_ORDER_REMOTE_PORT=15432
export SHOP_ORDER_REMOTE_USER=core_test
export SHOP_ORDER_REMOTE_PASSWORD=core_pg_test
export SHOP_ORDER_REMOTE_DATABASE=shop_order_scale_remote
export SHOP_ORDER_REMOTE_SSL_MODE=disable
```

未声明 `SHOP_ORDER_REMOTE_DRIVER` 时继续使用既有 `SHOP_ORDER_REMOTE_MYSQL_*` 配置。业务、Manage、Outbox 和统计定义不因切库增加驱动分支；数据库类型切换不搬迁旧库数据。

## 验证入口

`docker-compose.integration.yml` 的 persistence profile 包含固定版本 PostgreSQL、MySQL、MongoDB 和 ClickHouse，端口均绑定回环地址。端口可用 `CORE_TEST_*_PORT` 覆盖，以避开其他项目。

```bash
# 自动启动隔离 Compose 并执行外部存储契约和三库统计一致性
rtk proxy bash scripts/test.sh integration-persistence

# 已有专用 PostgreSQL 时，只运行 PostgreSQL 真实契约
CORE_TEST_POSTGRES_PORT=15432 rtk proxy bash scripts/test.sh integration-postgres

# 专用 PostgreSQL 与 Redis 已启动：完整 07/09 HTTP、WebSocket、恢复验收
CORE_TEST_POSTGRES_PORT=15432 CORE_TEST_REDIS_ADDR=127.0.0.1:16395 \
  rtk proxy bash scripts/test.sh integration-postgres-e2e
```

测试角色、库名和待清理资源必须属于该测试项目。Go 单元回归、真实进程、race 与发布门禁的实际结果由本次验收报告记录；本指南本身不代表验收通过。
