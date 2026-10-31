# PostgreSQL 存储支持验收记录

日期：2026-10-18。工作区：`/Users/vincent/orca/workspaces/core/postgres-support`；分支：`feat/postgres-support`；基线：`9f5d7d0372c985aac3b1b7abfd11cda60e3a85f4`。本次包含工作区修改和未跟踪的新文件，尚未提交或推送。

对应 [实施计划](../superpowers/plans/2026-10-18-postgresql-storage-abstraction.md) 与 [使用指南](POSTGRESQL_STORAGE_GUIDE.md)。

## 实现边界

数据库选择集中在 models 持久化组合根与 Core 控制面的内部组合根，通过 `oltp.NewDataAction(types.StorageConfig)` 装配。Manage 与业务不增加驱动判断；统计提交中性聚合请求，实际 SQL、字段映射和方言执行下沉至 persistence。既有 IDataAction 必选方法、业务 DTO、HTTP 路径和默认 SQLite 保持兼容。

PostgreSQL 使用官方 GORM 驱动，支持固定库、IDBName 模型分库、连接池共享与隔离事务、精确 decimal、加性初始化、关联表、跨进程建库及结构互斥。事务写入保存点恢复唯一冲突，普通 Insert 仍返回 409。写入和 Commit 不自动重放；批次按关联模型最大列数限制绑定参数。混合分库批次与跨库事务明确拒绝。

## 验收矩阵

| 验收范围 | 结果 | 直接证据 |
| --- | --- | --- |
| 持久化全包单元测试 | PASS | `/private/tmp/core-postgres-accepted-unit.log` |
| OLTP、stats、ManageAuth、Manage race | PASS | `/private/tmp/core-postgres-accepted-race.log` |
| PostgreSQL 全部真实驱动与首管理员 race | PASS | `/private/tmp/core-postgres-accepted-live-race.log` |
| SQLite/MySQL/PostgreSQL 同一模型 CRUD、筛选、排序分页、NULL、IN、BETWEEN、SUM、回滚、同一 StatSpec 日/ISO周/月/季/年与精确指标 | PASS | `/private/tmp/core-postgres-accepted-storage.log` |
| PostgreSQL 完整验收入口：驱动/统计/首管理员、07 单进程业务与 WebSocket、07 多副本硬崩溃恢复、09 HTTP/RBAC | PASS | `/private/tmp/core-postgres-accepted-e2e.log` |
| 配置、API、公开 Go API、发布契约 | PASS | `/private/tmp/core-postgres-accepted-{config,api,public-api,release}.log` |
| 日志、Compose 和隔离脚本生命周期契约 | PASS | `/private/tmp/core-postgres-accepted-{logging,compose-contract,script-contract}.log` |

真实 PostgreSQL 用例包括：无 CREATEDB 角色访问已建库与缺库权限失败；已有列类型及旧列保留、补列补索引、失败唯一索引不作为成功缓存；多进程首次建库；动态分库；has-many、many2many 与 14,000 个五列关联对象；14,000 行平面批量；跨 action 初始化缓存传播；未加数据库 tag 的 decimal 精度；跨数据库基础资料展示、事实快照和维度分组。

连接故障测试实际关闭连接池并终止 PostgreSQL 活动事务的 backend，断言写失败、数据未落库、事务未换连接。07 的提交未知用例在真实远程事务成功之后注入“提交回执丢失”，观察 pending 保留，再次同步只保留原订单 ID 与一份业务事实。这是受控故障注入，不宣称已覆盖真实网络在 Commit 响应字节中断的每个时序。

07 多进程验收先制造真实远程库阻塞，保留待同步事实后硬终止进程，再用原 pending 路径重启，核对原 ID、远程订单、Outbox 与 pending 清空；不是用健康状态或 HTTP 200 代替恢复验收。

## 独立审查与修复

使用 `code-review-and-quality` 进行三个独立只读审查，并以 `superpowers:verification-before-completion` 核验最终证据。审查覆盖存储正确性、查询/聚合及公开兼容、测试脚本/文档/计划完成度。未发送到外部审查服务。

审查发现并修复：关联子表遗漏、多对多中间表关系空指针、可驱逐迁移缓存被误当作事务就绪证明、跨 action 关联就绪传播、关联批量参数上限、无 ID 投影的默认排序、MySQL DSN 默认值改变连接身份、Compose 继承外部 PostgreSQL 地址，以及 09 夹具硬编码连接参数。独立复验未保留阻断实现缺陷；对应回归均实际执行。

保留失败证据：`core-postgres-review-association-red.log`、`core-postgres-search-red.log`，以及独立查询探针 `/private/tmp/core-accept-boundary-probe.go`。早期 `core-postgres-final-e2e.log` 曾在 WebSocket 超时后中止，不计作通过；以表中新的完整入口日志为准。

## 可复现命令

```bash
rtk proxy bash scripts/test.sh persistence-unit
rtk go test -race ./pkg/persistence/database/oltp ./pkg/persistence/entity/stats ./pkg/server/manageauth ./service/manage -count=1
CORE_TEST_POSTGRES=1 rtk go test -race -tags=integration ./pkg/persistence/database/oltp ./pkg/server/manageauth -run TestPostgresIntegration -count=1 -v -timeout=5m

CORE_TEST_PERSISTENCE_PROJECT_NAME=core-pg-support-acceptance-20261018 \
CORE_TEST_MYSQL_PORT=15307 CORE_TEST_POSTGRES_PORT=15433 \
CORE_TEST_MONGODB_PORT=27019 CORE_TEST_CLICKHOUSE_PORT=19001 \
CORE_TEST_CLICKHOUSE_HTTP_PORT=18124 \
  rtk proxy bash scripts/test.sh integration-persistence

CORE_TEST_REDIS_ADDR=127.0.0.1:16395 \
  rtk proxy bash scripts/test.sh integration-postgres-e2e

rtk proxy bash scripts/test.sh config-contract
rtk proxy bash scripts/test.sh api-compat
rtk proxy bash scripts/test.sh public-api
rtk proxy bash scripts/test.sh release-contract
rtk proxy bash scripts/check-logging.sh
rtk proxy bash scripts/test-compose-contract.sh
rtk proxy bash scripts/test_integration_persistence_test.sh
```

完整入口需要专用 PostgreSQL 和 Redis，缺少 Redis 地址即失败，不能把多进程用例跳过后标成完整通过。独立 PostgreSQL 入口中的 MySQL parity 可以跳过，三库结论只来自启用三库的隔离 persistence 门禁。

## 环境与结论边界

用户授权本机 Docker 测试。保留任务专用 PostgreSQL `core-postgres-support-pg-20261018`（16.6-alpine，回环 15432）与 Redis `core-postgres-support-redis-20261018`（7.4-alpine，回环 16395）供复现。隔离 Compose 项目运行结束后按项目名清理；未清理其他项目资源。

NOT_RUN：生产 TLS 双向证书握手、生产容量/性能、历史 MySQL→PostgreSQL 数据搬迁。它们不属于本次功能通过结论；数据库切换不会搬迁旧数据。PostgreSQL 的 bigint 不支持整个 uint64 范围，已验证 Snowflake 范围内的大 ID。原生 SearchSQL/Raw/Exec 中调用方自己书写的厂商 SQL 不自动翻译。
