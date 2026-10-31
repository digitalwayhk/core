# 07 示例 MySQL / PostgreSQL 性能对照

> 后续诊断修正：MySQL 100 比 50 更慢的排名未稳定复现，不能将初筛候选视为最优；原始数值保留。详见[连接池差异诊断](MYSQL_POOL_50_100_DIAGNOSIS_2026-10-18.md)。

## 结论

2026-10-18 已完成硬件预算调优、应用连接池 20／50／100 梯度、候选配置各 5 轮真实链路复测，以及各 5 轮独立批量同步基准。

20 个连接确实出现排队。初筛中 MySQL 的 HTTP P95 为 623 ms，并因在途请求达到上限漏发 1,167 笔；PostgreSQL 的 P95 为 92 ms。扩大连接池后改善，但 MySQL 100 个连接的单轮表现比 50 个差，连接数不能无限增加。本机候选档位为 MySQL 50、PostgreSQL 100；这是有限梯度的选择，不是全局最优配置。

候选配置下，两边各确认 65,000 笔，HTTP 失败、漏发、确认丢失或错键、失败后落库、采样错误均为 0，最终 pending、Outbox 未发布、消费组 lag/pending 全部清零。两边均承受了目标 2,000 笔/秒、持续 6 秒的短时接单；完整后台链路的平均完成速率仍只有数百笔/秒，不能据此宣称可持续 2,000 笔/秒。

PostgreSQL 的独立 1,000 条批量同步更快；当前真实链路下 MySQL 的 HTTP 尾延迟更稳、持久化与发布完成更快。结果取决于路径，不能概括为某个数据库整体更快。

## 范围与机器预算

基线 HEAD 为 `9f5d7d0372c985aac3b1b7abfd11cda60e3a85f4`，分支 `feat/postgres-support`，包含尚未提交的 PostgreSQL 支持实现。真实链路复用 07 的 user/supplier/order 三进程、mTLS、HTTP 下单、远程幂等查询、本地 Badger 可靠写、订单与 Outbox 事务、Redis Streams 及消费 ACK。单 order 副本，stable 身份，每轮独立数据库和本地状态目录。

| 项目 | 实际条件 |
| --- | --- |
| 宿主机 | macOS arm64，64 GiB 内存、16 个逻辑 CPU，Go 1.26.6，不启用 race |
| Docker Desktop | 分配 8 CPU、约 31.29 GiB 内存 |
| 数据库预算 | 每容器 4 CPU 配额、6 GiB 内存上限、无额外 swap 额度；数据卷存储 |
| MySQL | 8.0.46，复用 `core-tx-load-mysql-r2-20261018`，回环 13392 |
| PostgreSQL | 16.6，复用 `core-postgres-support-pg-20261018`，回环 15432 |
| Redis | 7.4，两边复用 `core-postgres-support-redis-20261018`，回环 16395 |
| 数据缓存 | MySQL buffer pool 与 PostgreSQL shared_buffers 均为 2 GiB |
| 服务端连接上限 | 两边 `max_connections=200` |
| 应用池梯度 | MaxOpen=20／50／100，MaxIdle 与 MaxOpen 相同，存活 30 分钟 |
| 负载 | 500 笔/秒 × 2 秒，随后 2,000 笔/秒 × 6 秒；共计划 13,000 笔/轮，另有预热 1 笔 |
| 发送器 | 最多 512 个在途，HTTP 超时 5 秒，不重试写请求；达到在途上限单独计作漏发 |
| 排空与采样 | 停载后最多 360 秒；每秒采样应用与队列，另采集 Docker 资源 |

宿主机 64 GiB 不等于 Docker 可用内存。这里按 Docker 实际预算分配，给应用、Redis、系统缓存及其他项目留出空间。CPU 配额不是独占核心；仍有共享宿主机干扰，复用数据卷的历史和缓存热态也未完全统一。两边测量结束均无 OOM。

MySQL 增加 redo 容量、log buffer，并使用 O_DIRECT；PostgreSQL 调整 WAL、检查点、工作内存及规划器缓存估算。具体参数见 [MySQL 配置](bench-configs/07-mysql-4cpu-6gb.cnf)和 [PostgreSQL 配置](bench-configs/07-postgres-4cpu-6gb.conf)。实际生效值已查询核验，参数原值已备份。MySQL 保留 `innodb_flush_log_at_trx_commit=1`、`sync_binlog=1`；PostgreSQL 保留 `fsync=on`、`synchronous_commit=on`、`full_page_writes=on`。未通过关闭持久化换取性能。

连接池会复用连接，20 个连接不等于每秒只能处理 20 单；是否排队取决于连接占用时长，以及前台查询、后台事务共享池的程度。应结合真实 `DB.Stats` 判断，再限制应用副本的总连接预算。[Go 连接管理](https://go.dev/doc/database/manage-connections)

上述缓存和工作内存是本机候选设置，未证明为最优；尤其 PostgreSQL 的工作内存可能随并发操作累加，不能只依据 `shared_buffers` 判断总内存。[PostgreSQL 16 资源配置](https://www.postgresql.org/docs/16/runtime-config-resource.html)、[连接配置](https://www.postgresql.org/docs/16/runtime-config-connection.html)、[MySQL 8 缓冲池配置](https://dev.mysql.com/doc/refman/8.0/en/innodb-buffer-pool-resize.html)

## 连接池初筛

按 MySQL20、PostgreSQL20、PostgreSQL50、MySQL50、MySQL100、PostgreSQL100 顺序，每档 1 轮。先按漏发与失败最少，再按 P95 较低选择候选配置。随后交替补测候选配置各 4 轮，连同初筛共各 5 个样本。

| 数据库 | 池上限 | 高档窗口内完成/秒 | HTTP P95 ms | HTTP P99 ms | 漏发 | 池等待次数 | 累计等待秒 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| MySQL | 20 | 1,721.7 | 623.2 | 874.7 | 1,167 | 21,677 | 2,706.41 |
| PostgreSQL | 20 | 1,958.8 | 92.4 | 168.4 | 0 | 10,860 | 163.84 |
| PostgreSQL | 50 | 1,994.7 | 29.3 | 61.2 | 0 | 1,127 | 7.60 |
| MySQL | 50 | 1,992.3 | 20.5 | 27.0 | 0 | 119 | 0.15 |
| MySQL | 100 | 1,993.7 | 58.0 | 142.2 | 0 | 1,269 | 24.54 |
| PostgreSQL | 100 | 1,992.7 | 11.0 | 20.2 | 0 | 0 | 0 |

六档 HTTP 失败均为 0。MySQL20 只实际发送并确认 11,833 笔，其余档位为 13,000 笔，漏发不能算作成功，也不能只比较排空秒数。

等待统计来自应用进程实际的 SQL 池，覆盖该轮完整过程，包含前后台共享负载；不是观测器自己的池。累计等待秒数是各并发请求等待之和，可大于墙钟时间。采样到的 in-use 峰值可能遗漏瞬时峰值，不能用它否定累计等待证据。PostgreSQL100 在初筛没有等待，复测仍出现等待，不能宣称该档位彻底消除了竞争。

## 候选配置各 5 轮

下表为各轮指标的中位数，括号为最小—最大值；延迟是每轮 P95/P99 的中位数，不是合并所有请求后重新计算的分位数。

| 指标 | MySQL，池 50 | PostgreSQL，池 100 |
| --- | ---: | ---: |
| 高档窗口内完成/秒 | 1,992.3（1,992.2—1,993.8） | 1,992.7（1,991.0—1,996.3） |
| HTTP P50 ms | 9.8（9.0—10.5） | 7.8（6.6—8.5） |
| HTTP P95 ms | 18.4（15.9—20.5） | 25.5（11.0—77.5） |
| HTTP P99 ms | 27.0（23.0—28.2） | 106.5（20.2—165.5） |
| 远程持久化及 Outbox 发布平均完成/秒 | 561.6（516.4—562.0） | 478.1（445.1—496.7） |
| 消费 ACK 平均完成/秒 | 307.2（268.8—307.4） | 299.8（280.1—307.0） |
| 停载后持久化及发布排空秒 | 15.13（15.12—17.15） | 19.18（18.16—21.20） |
| 停载后消费 ACK 排空秒 | 34.29（34.27—40.35） | 35.35（34.33—38.39） |
| 池累计等待次数 | 129（3—306） | 925（0—1,454） |
| 池累计等待秒 | 0.15（0.001—0.45） | 11.36（0—38.81） |

各库 5 轮共确认 65,000 笔，另有每轮 1 笔预热。每轮逐笔核对成功响应的 requestID/OrderID 与远程事实，最终订单及已发布 Outbox 均为 13,001 条；确认丢失/错键、失败后落库、发送器漏发、HTTP 失败、采样错误均为 0，消费组 lag 与 pending 均为 0。

以中位数比较，MySQL 的持久化与发布平均完成速率高约 17.4%，消费 ACK 平均速率高约 2.5%；PostgreSQL 的 HTTP P50 更低，但尾延迟波动更大。这些是当前框架路径的有限样本结果，未做显著性检验或 SQL/CPU 归因。

## 独立批量同步基准

使用现有 `Benchmark07DrainPendingOnce`，两边交替各 5 次独立进程，每次预置 1,000 条 pending，计时单次同步，实际确认必须为 1,000。建库建表、fixture 和本地入队在计时外；计时包含 pending 索引读取、真实订单与 Outbox 事务、本地 ACK，排除 Outbox 发布与消费者。连接池使用上面的候选档位。

该基准 `SyncBatchSize=1000`，真实链路沿用原有 100 条批次与 100 ms 调度，两种结果不能互相替代。

| 指标，中位数（最小—最大） | MySQL | PostgreSQL |
| --- | ---: | ---: |
| 1,000 条同步耗时 ms | 193.3（190.0—227.2） | 118.7（113.5—130.7） |
| 折算同步订单/秒 | 5,174（4,402—5,264） | 8,422（7,653—8,812） |
| Go 分配字节/订单 | 41,509（41,500—41,521） | 53,208（53,192—53,216） |
| Go 分配次数/订单 | 535（534—535） | 626（625—626） |

此批量路径 PostgreSQL 的折算速率高约 62.8%、单批耗时低约 38.6%，同时 Go 分配更多。它说明纯同步阶段具备不同表现，不能把单批折算速率当作完整链路持续 TPS，也不能仅凭该数据解释真实链路尾延迟的来源。

## 统计边界

- HTTP 成功只证明本地可靠提交；远程订单、Outbox 发布和消费者 ACK 分别核验。
- 高档“窗口内完成”由逐笔 `started_ms + duration_ms` 重算。原始 `success_rps_including_inflight` 包含窗口尾部完成，不能冒充窗口内完成。
- 持久化/发布平均完成速率 = 非预热订单数 ÷（两段实际负载时长，含尾部完成 + 持久化/发布排空时长）；ACK 平均速率同理。这是有限批次平均速率，不是纯数据库最大 TPS。
- 初筛每档仅 1 次，候选选择包含该初筛样本；结论存在选择偏差。不同档位的收益及尾延迟仍需长窗口、更多样本验证。
- 两边均在有限接单窗口积压，最终排空不等于稳态没有积压。可持续 2,000 单/秒容量：`NOT_CHARACTERIZED`。
- 队列与内存峰值为周期样本，可能低于瞬时绝对峰值。磁盘峰值采集未独立验证：`NOT_CHARACTERIZED`，不把采样字段 0 解释为无磁盘占用。
- 参数、实例、连接池及窗口多项共同变化，不能把初始阶段到调优阶段的差值归因于单一数据库参数。

## 初始诊断与资源收尾

初始阶段未设置单容器 CPU/内存上限，缓存均为 128 MiB，应用池最大 20、空闲 5，窗口为 500/s × 5 秒、2,000/s × 10 秒。执行 MySQL、PostgreSQL、PostgreSQL、MySQL、MySQL 后，按用户追加要求结束该阶段。后来发现初始 MySQL `core-tx-recovery-mysql-20261018` 上存在另一轮活跃订单压测，因此初始数据不能用于厂商性能比较或调优收益归因。原始失败、漏发及日志保留，未挑除后包装为成功结果。

调优阶段改为复用已存在、原先停止的 MySQL `core-tx-load-mysql-r2-20261018`，避免修改另一个仍有压测的实例。每轮使用唯一测试数据库；本次没有新建或删除容器，也未清理其他项目数据。结束后该 MySQL 已恢复停止，保留测试数据与调优配置；任务原有 PostgreSQL、Redis 保持运行并保留调优设置。两个数据库的 4 CPU／6 GiB 配额保留，修改前快照及参数备份保留在本机证据目录。

本次代码只增加 PostgreSQL 压测观察适配、可配置窗口、连接池环境传递及 `corebench` 构建限定的真实池指标；未更改生产默认连接数、业务同步调度、批次或消费者并发。相关 bootstrap/store 单测通过，压测构建通过；14 轮真实链路与 10 轮独立同步基准退出码均为 0。未提交、未推送。

## 证据与复现

[脱敏汇总 JSON](bench-results/07-mysql-postgres-2026-10-18.json) 保存逐轮汇总、候选统计、环境、源码及压测二进制指纹、独立同步基准。测量时压测二进制 SHA256：`b4e8aa6a868cf657cf7a70142fdcff72b34625c8943117847c36eb422124a764`；源码指纹是测量时快照，随后报告补写会改变文档哈希。

调优原始证据：`/private/tmp/core-07-tuned-db-perf-20261018/`。初始诊断证据：`/private/tmp/core-pg-mysql-07-perf-20261018/`。每轮保留测试日志、逐笔请求、周期采样、三份应用进程日志及 Docker 资源样本。完整容器环境和凭据仅保存在本机私有文件，不进入汇总。

真实链路单轮示例，凭据从本机私有环境注入：

```bash
# 先编译一次；原生子进程构建也需保留 GOFLAGS。
export GOFLAGS=-tags=corebench
rtk go test -c ./examples/integration/07-shop-order-scale-multi-process -o /tmp/order-load.test
export CORE_TEST_ORDER_LOAD=1 CORE_TEST_LOAD_POOL_METRICS=1
export CORE_TEST_REDIS_ADDR=127.0.0.1:16395
export CORE_TEST_LOAD_LOW_SECONDS=2 CORE_TEST_LOAD_HIGH_SECONDS=6

# MySQL：清除遗留的 PostgreSQL 连接环境。
# CORE_TEST_STABLE_MYSQL_PASSWORD 由本机私有环境注入。
unset SHOP_ORDER_REMOTE_DRIVER SHOP_ORDER_REMOTE_USER SHOP_ORDER_REMOTE_PASSWORD
export CORE_TEST_STABLE_MYSQL_ADDR=127.0.0.1:13392
export SHOP_ORDER_REMOTE_MYSQL_MAX_OPEN=50 SHOP_ORDER_REMOTE_MYSQL_MAX_IDLE=50
export CORE_TEST_LOAD_EVIDENCE_DIR=/absolute/path/to/mysql-evidence
rtk proxy /tmp/order-load.test -test.run='^TestStableOrderWriteLoad$' -test.count=1 -test.v -test.timeout=8m

# PostgreSQL：沿用共同负载变量，替换驱动、连接和证据目录。
# 运行前从本机私有环境设置 SHOP_ORDER_REMOTE_PASSWORD。
export SHOP_ORDER_REMOTE_DRIVER=postgres
export CORE_TEST_POSTGRES_ADDR=127.0.0.1:15432
export SHOP_ORDER_REMOTE_USER=core_test SHOP_ORDER_REMOTE_SSL_MODE=disable
export SHOP_ORDER_REMOTE_MAX_OPEN=100 SHOP_ORDER_REMOTE_MAX_IDLE=100
export CORE_TEST_LOAD_EVIDENCE_DIR=/absolute/path/to/postgres-evidence
rtk proxy /tmp/order-load.test -test.run='^TestStableOrderWriteLoad$' -test.count=1 -test.v -test.timeout=8m
```

独立同步基准的连接 host/port/user/password/database 与池参数由 `SHOP_ORDER_REMOTE_*` 注入，每次使用唯一数据库：

```bash
CORE_TEST_MYSQL=1 CORE_TEST_POSTGRES=1 rtk go test ./examples/integration/07-shop-order-scale -run '^$' -bench '^Benchmark07DrainPendingOnce$' -benchtime=1000x -count=1 -benchmem -timeout=2m
```

正式测量使用同一已编译的真实链路测试二进制，并按上文交替顺序逐轮切换驱动、连接池和唯一证据目录。重新运行前需启动所复用的测试 MySQL，并确认没有其他客户端负载。
