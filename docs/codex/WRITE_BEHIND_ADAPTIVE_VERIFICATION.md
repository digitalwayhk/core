# 自适应 write-behind 验证记录

日期：2026-09-11。基线为 v1.1.2 / `b5b15a83c9319407957a2669c9b08d81f0fe2ddc`。执行环境：macOS arm64、Go 1.26.6。

## RED → GREEN

- 通过 JSON 输入新增配置，旧实现忽略字段，7 个非法组合均错误通过 Validate；新增完整组合校验后通过。
- 6 条 pending、每批 2 条、旧延迟 1 秒：原 worker 在 300 ms 内无法排空；新策略完成三批 `[2 2 2]`，不重复旧等待。
- 纯调度测试验证期限、阈值、排空间隔、零确认、部分失败、退避上限及空闲周期切换。
- 收尾审查发现解绑后再次绑定没有新写入时缺少唤醒，确定性回归先失败；在绑定入口唤醒已有 worker 后通过。失败日志 `rebind-red.log` 保留，未把它当作通过。
- 真实 Badger 覆盖同 key 合并、到期触发、阈值提前、新空闲周期、失败期间信号、并发新版本/手动同步、零进展、关闭中断、重启恢复。
- `TestAdaptiveLegacyPendingUpgrade` 使用旧零配置生成 pending，关闭后在原目录启用新配置，验证三条历史记录直接排空；不清数据、不更改序列化格式。不是另行编译 v1.1.2 二进制的跨版本进程测试。

## 真实 MySQL 对照

专用容器 `core-adaptive-mysql-20260911`，MySQL 8.0.46，镜像 `sha256:b7e118f56c5963e079252e0d6e2978c9c010eb7fc7aaaec100e367796b5dd08f`，仅绑定 `127.0.0.1:50029`。`innodb_flush_log_at_trx_commit=1`、`sync_binlog=1`。没有修改、部署或重启 Bitzoom。

通过 `ModelListWriteBehindTarget` 写入真实 MySQL，框架自动建库建表及预热在统计前执行；该 target 每条模型仍走适配器，不代表 Trades 的批量 SQL 性能。三种策略均使用 `SyncBatchSize=512`：旧 100 ms、固定 10 ms、自适应 32/20 ms/0。每组重复三轮，每轮核验远端行数（含一条预热记录）、确认条数、Failures=0 和 pending=0。

| 负载 | 旧 100 ms | 固定 10 ms | 自适应 |
| --- | --- | --- | --- |
| 8 条、间隔 40 ms | 3 次 commit，批次 `[3 3 2]` | 8 次 commit，每批 1 条 | 8 次 commit，每批 1 条 |
| 一次提交 256 条本地 pending | 1 次 commit、256 条 | 1 次 commit、256 条 | 1 次 commit、256 条 |
| 预置 1536 条积压 | 3 次 commit、每批 512 条 | 3 次 commit、每批 512 条 | 3 次 commit、每批 512 条 |

低流量每轮 p50 的中位数分别为 **66.182 ms / 15.511 ms / 25.767 ms**。延迟取模型 pending UpdatedAt 到 target 返回成功，包含本地处理与远端事务，不冒充纯收集等待；8 个样本的 p95/p99 只作原始小样本诊断，不作生产尾延迟认证。

积压排空耗时分别为：

- 旧 100 ms：1.123 / 1.102 / 1.396 秒。
- 固定 10 ms：0.944 / 0.958 / 1.152 秒。
- 自适应：0.843 / 1.091 / 1.032 秒。

自适应消除了重复主动等待且保留完整批次，但宿主仍有其他容器负载，耗时有波动，不据此给出稳定的吞吐提升百分比。

采集 `Com_commit`、`Innodb_os_log_written`、`Innodb_data_fsyncs`、`Innodb_os_log_fsyncs` 的全局差值。fsync 和 redo **没有稳定下降**：例如积压的 data fsync 旧模式为 433/479/616，自适应为 465/567/546。专用数据库排除了其他业务 SQL，但不能排除 InnoDB 后台刷盘和跨窗口影响；这些差值不能严格归因到某一业务 commit。

结论：低流量下用更多小事务换取更短延迟是实测存在的取舍；本次优化不承诺同时降低所有负载下的延迟和写放大。Trades/R60 应维持相同业务链路、输入速率和持久化参数重新验收。

## 验证命令与范围

执行日志目录：`/tmp/core-adaptive-evidence-Upjl2K/`。

- `go test -race ./pkg/persistence/database/nosql -count=3`：通过，最终实现 36.084 秒，`race-final.log`。默认受保护的外部测试仍跳过。
- `go test -race ./pkg/persistence/database/nosql -run TestAdaptiveLegacyPendingUpgrade -count=3`：通过，`upgrade.log`。
- `CORE_ADAPTIVE_MYSQL_ADDR=127.0.0.1:50029 go test ./pkg/persistence/database/nosql -run TestAdaptiveMySQLComparison -count=3 -v`：通过，27 个真实对照子场景，`mysql.log`。
- 同一真实 MySQL 对照另外执行 `-race -count=1`：通过，9.681 秒，`mysql-race.log`；race 耗时不混入上面的无 race 性能数据。
- `persistence-unit`、`performance-contract`：通过；`release-contract` 包含公共 API、配置和安全门禁，执行结果见 `release-final.log`。
- `go vet ./pkg/persistence/database/nosql`、`gofmt`、`git diff --check`、日志检查、AI skill 权威源检查：通过。

全仓 `go test ./...`、Bitzoom R60 同拓扑长时间压力验收：**NOT RUN**。本任务没有推送、合并或发布版本。专用 MySQL 测试库保留，不删除业务数据。
