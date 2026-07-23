# 持久化正确性与外部测试分离实施计划

> **面向智能体开发者：** 按小节执行 TDD。每个行为修复必须先观察失败测试，再做最小实现，每节独立验收、提交并更新本计划。

**目标：** 让默认持久化测试完全不依赖 MySQL、MongoDB 或 ClickHouse，修正 GORM 结果错误传播和 SharedBadger 同步语义，再通过显式 Docker 套件验证外部数据库契约。

**架构：** 默认层只使用 SQLite、Badger 与可控 fake；外部层同时需要 `integration` build tag 和 `CORE_TEST_*` 环境变量。数据库行为继续委托 GORM 与官方 driver，框架只组装连接、路由、同步状态与生命周期。

**当前 RED 基线（2026-07-11）：** `go test ./pkg/persistence/... -count=1 -timeout=5m` 失败；默认套件隐式连接 `127.0.0.1:3306`，并复现 `SyncBatchDelay=0`、fatal 错误后继续 Insert、pendingCount 不减少、零成功仍记录“同步成功”等问题。

---

## 范围与约束

- 不重写 GORM driver、连接池、事务或 SQL 执行器。
- 不以“本机恰好运行 MySQL”作为默认测试通过条件。
- 不将混合文件整体加 integration tag，避免丢失其中的 Badger/SQLite/fake 单元覆盖。
- 外部测试容器端口只绑定 `127.0.0.1`，镜像版本锁定并配置健康检查。
- 新增日志使用中文事件文本；零成功、部分成功和完全成功必须是不同语义。

## 任务 13.1：建立可信的默认测试层

**优先级：** P0

**文件：**
- 拆分：`pkg/persistence/database/oltp/mysql_concurrency_test.go`
- 拆分：`pkg/persistence/database/oltp/mysql_issues_test.go`
- 拆分：`pkg/persistence/database/nosql/sharedbadger_test.go`
- 拆分：`pkg/persistence/database/nosql/sharedbadger_issues_test.go`
- 拆分：`pkg/persistence/database/nosql/userinfo_test.go`
- 审查：`pkg/persistence/database/olap/*_test.go`
- 修改：`scripts/test.sh`

- [x] **13.1a 盘点并拆分混合测试**

将真实 MySQL/MongoDB/ClickHouse 用例移到 `*_integration_test.go`，添加 `//go:build integration`。纯 Badger、SQLite、fake 和配置测试保留在默认文件。

- [x] **13.1b 添加双重外部门禁**

MySQL 套件还必须显式设置 `CORE_TEST_MYSQL=1`，MongoDB 与 ClickHouse 对应 `CORE_TEST_MONGODB=1` 和 `CORE_TEST_CLICKHOUSE=1`。未设置时输出清晰 skip 原因。

- [x] **13.1c 增加脚本入口**

`./scripts/test.sh persistence-unit` 运行无外部依赖持久化套件；`integration-persistence` 使用 integration tag 和显式环境变量。

**完成记录（2026-07-11）：** 独立 MySQL 测试文件已使用 `integration` build tag；由于 SharedBadger 文件混合了 Badger、SQLite、fake 和 MySQL 用例，未将整个文件排除，而是通过编译期 `persistenceIntegrationBuild` 开关与 `CORE_TEST_MYSQL=1` 双重门禁约束真实 MySQL 入口。已新增两个脚本模式；`integration-persistence` 当前只负责编译与运行显式开启的集成测试，Docker 编排在 13.4 完成。

**验收结果：** `go test ./pkg/persistence/... -count=1 -timeout=5m` 与 `./scripts/test.sh persistence-unit` 均通过，默认套件不再连接本地 MySQL。原先包装真实 MySQL 的六个回退行为测试暂纳入双门禁，13.3 将其改为纯 fake 后恢复默认覆盖。

**验收：**
```bash
go test ./pkg/persistence/... -count=1 -timeout=5m
./scripts/test.sh persistence-unit
```

## 任务 13.2：修正 GORM 结果错误传播

**优先级：** P0

**文件：**
- 修改：`pkg/persistence/database/oltp/mysql.go`
- 修改：`pkg/persistence/database/oltp/sqlite.go`
- 创建或修改：对应结果传播测试

- [x] **13.2a 编写 Raw/Scan/Exec 失败测试**

使用 SQLite 内存库或可控 GORM dialector，验证无效 SQL、Scan 失败、context 取消和事务回滚返回本次 `Raw/Scan/Exec` 的 result `.Error`。

- [x] **13.2b 最小修正过时 handle 错误**

将 `m.db.Raw(...).Scan(...); return m.db.Error` 与 SQLite 对应实现改为返回链式结果 `.Error`；审查同类 Exec/Find/Count 路径，只修正有失败证据的位置。

**完成记录（2026-07-11）：** 新增公开 `Sqlite.Raw/Exec` 回归测试，旧实现能在 GORM 记录本次错误时稳定复现返回 `nil`。MySQL 和 SQLite 已统一返回链式 result `.Error`，聚焦测试连续 20 次通过，完整持久化套件通过。SQLite 已有事务回滚覆盖；当前 `IDataAction.Raw/Exec` 签名不接收 `context.Context`，调用方取消不在本次最小修复中虚假标记为已覆盖，后续如扩展公共接口需单独设计兼容性方案。

**验收：**
```bash
go test ./pkg/persistence/database/oltp -run 'Test.*(ResultError|Rollback|Context)' -count=20
```

## 任务 13.3：修正 SharedBadger 同步语义

**优先级：** P0

**文件：**
- 修改：`pkg/persistence/database/nosql/sharedbadger.go`
- 修改：`pkg/persistence/database/nosql/sharedbadgermanager.go`
- 修改：已有 bench/issues/syncqueue 测试

- [x] **13.3a 修正默认批处理延迟**

新建 manager 在未配置时应将 `SyncBatchDelay` 设为 100ms，显式零值语义必须在配置构造器中明确。

- [x] **13.3b 修正成功数、pending 与 CAS**

只有真正写入远程并通过 CAS 的 key 才计入成功并从 pending 减少；并发 Set 生成的新版本必须保持未同步。

- [x] **13.3c 修正 fatal-break 与重试边界**

连接不可用、事务已回滚和 context 取消等致命错误不得继续对后续项执行 Insert/Update/Delete。重试必须可被关闭 context 取消。

- [x] **13.3d 修正同步日志语义**

当 `successCount==0` 时记录失败/未同步，部分成功记录部分结果，只有全部成功才记录同步成功。

**完成记录（2026-07-11）：** 默认延迟已由正确配置构造器测试锁定为 100ms。同步状态更新现在返回真正通过 CAS 或完成远程删除的确认 key；pending、后置删除与上报成功数均仅使用确认结果。零确认、部分完成和全部成功已分离日志语义。同时修正 `ConnectionManager.GetConnection` 在读锁下写 `LastUsed` 的竞态。

13.3c 已将六个批量/逐条回退用例从真实 MySQL 迁移到线程安全的深拷贝内存 action，恢复默认单元覆盖。事务开启、提交、主操作、冲突后二次操作和 `Exists` 六条路径都会在 context 取消、事务失效或连接级错误后立即回滚并停止，不再降级重试后续 item；实例关闭对 Insert/Update/Delete 逐条路径均有断言。`IDataAction` 暂无 `context.Context` 参数，因此无法强制中断已经进入 adapter 内部的永久阻塞调用；本任务未为此破坏公共接口兼容性。聚焦测试、nosql 全包 race 与完整持久化套件均通过。

最终跨小节审查进一步补齐了 `syncBatch` 编排层：路由、批量 helper、逐条降级和 rollback 路径遇到 fatal 后都会停止后续操作类型与数据库分组，不触发失败分组的 `OnSyncAfter`；fatal 前已经完成的 key 仍经过 CAS 确认并正确更新 pending。多操作、多数据库和降级路径已有回归覆盖。

**验收：**
```bash
go test -race ./pkg/persistence/database/nosql -run 'TestSyncConfig_DefaultBatchDelay|TestIssue_|TestSyncBatchDelay_' -count=1
```

## 任务 13.4：建立 Docker 外部持久化套件

**优先级：** P1

**文件：**
- 创建：`docker-compose.integration.yml`
- 修改：`scripts/test.sh`
- 修改：外部集成测试配置

- [x] **13.4a 锁定容器与健康检查**

加入 MySQL、MongoDB 和 ClickHouse，使用 `persistence` profile、持久化测试专用用户/密码/数据库，主机端口只绑定 `127.0.0.1`。

- [x] **13.4b 验证 driver 契约**

分别验证连接池配置、迁移、CRUD、context 取消、事务回滚和清理。

- [x] **13.4c 增加有界脚本**

脚本先等待健康状态，测试设置明确 timeout，结束时关闭容器；任一步失败都必须返回非零状态。

**完成记录（2026-07-12）：** 新增 `persistence` Compose profile，锁定 MySQL 8.4.4、MongoDB 7.0.16 和 ClickHouse 24.8.14.39，使用专用测试账号、密码与数据库，所有主机端口仅绑定 `127.0.0.1`，并为三个服务配置健康检查。外部测试继续同时要求 `integration` build tag 与对应 `CORE_TEST_*` 环境变量。

真实 driver 契约测试覆盖 MySQL 连接池、迁移、CRUD、事务回滚与清理，MongoDB CRUD、取消 context 与清理，以及 ClickHouse 建表、同步写入、查询与清理。MongoDB 适配器当前没有事务能力，本任务不虚构该契约；事务回滚由 MySQL 真实 driver 套件验证。ClickHouse `InsertSync` 会在单次写入 context 中关闭异步插入并等待服务端确认，不改变普通批量写入的异步 DSN。

`integration-persistence` 使用互斥锁避免并发套件争用固定端口，对 Compose 启动、Go 测试与 Compose 清理分别管理进程组。INT/TERM 会先停止并等待子进程，再清理容器和锁；清理默认以 30 秒为界，超时后依次发送 TERM/KILL。生命周期脚本覆盖成功、失败、信号、孤儿子进程、锁竞争、陈旧锁和清理卡死，真实 Docker 套件通过后无残留容器。

**验收：**
```bash
./scripts/test.sh integration-persistence
```

## 最终门禁

- [x] `go test ./pkg/persistence/... -count=1 -timeout=5m` 在没有 Docker/本地 MySQL 时通过。
- [x] `./scripts/test.sh persistence-unit` 通过。
- [x] `./scripts/test.sh integration-persistence` 在 Docker 环境通过。
- [x] Raw/Scan/Exec 错误、context 取消和事务回滚有回归覆盖。
- [x] 零成功不再记录“同步成功”，pending/CAS/fatal-break 语义通过。
- [x] 失败路径测试结束时无残留重试、worker、集成容器或测试锁。
