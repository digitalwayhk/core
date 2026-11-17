# Ownership指标导出白名单缺口登记（2026-11-17）

状态：**修复及组件回归完成，发布目标v1.14.1**。以下初始OPEN登记及失败原件保留；后续正式修复、验证及发布边界见文末。本轮不要求消费方中断既有对照或立即重部署。

## 已核实范围

基线为正式Core v1.14.0，提交`246c8c19e5ef95b944f6a5858148c381a3610115`。消费方消息`msg_18ad3a05fc00`反馈：其两节点已启动该版本且未发压，实际scrape缺少directory_keys等指标。此部署及现场状态是消费方报告，本轮Core没有连接、检查或操作Bitzoom/Full-E4。

Core自身源码及公开Collector本地核验均确认：

```text
ownership.Runtime.RuntimeMetricSnapshot
→ observability.ComponentCollector.Collect
→ filterGauges / filterCounters
→ allowedGaugeNames / allowedCounterNames
→ core_component_gauge / core_component_counter
```

`pkg/server/ownership/runtime_metrics.go`提供4个归属gauge及11个counter，但`pkg/server/observability/provider.go`白名单仅保留其中3个gauge和3个counter。`collector.go`在创建Prometheus样本前过滤，其余9项被丢弃。不是Collector未注册，也不能把缺样本当作已知0。

| 类型 | 被过滤的固定指标名 |
| --- | --- |
| gauge | ownership_directory_keys |
| counter | ownership_directory_errors_total |
| counter | ownership_renew_failures_total |
| counter | ownership_execution_revoked_total |
| counter | ownership_observation_revoked_total |
| counter | ownership_stalled_revoked_total |
| counter | ownership_activation_failures_total |
| counter | ownership_restore_failures_total |
| counter | ownership_restore_seconds_total |

实际保留的是`ownership_active/ready/inflight`和`ownership_acquired_total/revoked_total/unknown_total`。这些名称是`core_component_gauge`或`core_component_counter`的固定`name`标签值，不是各自独立的Prometheus指标族。

这9项并非新增BlockedDue字段：v1.14.0的BlockedDue提交没有修改runtime_metrics.go或provider.go；本轮没有追溯缺口最早出现版本。BlockedDue仍通过Observation/KeyStatus表达真实子集，不能把它扩成每key指标或据此宣称存在新的聚合度量。

## 本地实际核验

命令（由rtk代理执行）：

```bash
rtk proxy go run /private/tmp/core-ownership-metrics-gap-20261117/main.go
```

该程序使用真实`ownership.Runtime`零状态快照、公开`NewComponentCollector`以及独立`prometheus.Registry.Gather`，比较源名称与实际导出名称；没有Fake Provider或手工复制源名称。未启动owner，所以只证明名称过滤链，不验证现场指标数值或活动归属。

- 退出码：0；耗时2.994秒。
- 输出：1137 bytes，SHA256 `6de6501323c5f6dad250eabfe205a81df9fab6459fe8fbe2b34a0d0279800b2a`。
- 结果：源4 gauge/11 counter，导出3 gauge/3 counter，精确缺9项。
- 原件：`/private/tmp/core-ownership-metrics-gap-20261117/collector.stdout.json`、`collector.stderr.log`、`collector-receipt.json`及`main.go`。
- 该命令退出0表示核实了现有缺口，不是修复GREEN，也不是普通/race或现场验收PASS。

## 后续最小修复与验收要求

1. 在现有两类白名单中显式允许上述9个固定低基数名称；不开放任意名称，不增加业务key/UserID/OrderID等标签，不另建观测线程。
2. 先写失败回归，再使用真实Runtime快照与正式Collector验证名称完整、gauge/counter类别、非零值及已知0；保留未知动态名称被拒绝的回归。
3. 检查现有Runtime/组件注册及Prometheus抓取链，补对应普通/race；不修改归属算法、租期、SQL fence、消息/PEL、业务状态或容量门禁。
4. 发布前同步权威skill与指标兼容说明。缺失字段保持缺失/null和正确state，不补0；恢复时间counter是累计秒，不是分位数或单次恢复SLO。
5. 作为后续观测补全独立推进，不以修复指标代替消费方功能、UAT、排空或容量验证。

消费方目前用真实64行SQL fence及已导出的active/ready交叉核对，是其自己的现场取证选择；Core本轮没有替它执行或认证该核验，也不要求为此重部署、清数据或发压。

初始登记时NOT_RUN：当时没有生产修复，也未运行修复后的普通/race、真实owner新场景、HTTP scrape、Full-E4、部署或压测。之后新增的本地验证以文末实际结果为准；此前v1.14.0现场边界不自动升级。

## 后续真实RED发现与最小生产修复

收到`msg_23376354eed3`正式修复授权后，在独立分支`fix/ownership-metrics-export`继续；不操作消费方对照或环境。生产只改两处：

1. `pkg/server/observability/provider.go`：将表中1 gauge+8 counter加入既有固定白名单，不放宽动态未知名、不改Collector和标签。
2. `pkg/server/ownership/runtime.go`的`Runtime.run`：复制原Manifest.Keys后，用已有atomic计数记录其真实长度；动态初始仍空目录，后续仍由原分页更新。没有改Acquire、游标、并发、Ready、租约、SQL fence、BlockedDue、原消息或预算。

第二处来自额外真实接线FAIL，不是性能猜测：`TestServiceContextOwnedMetricsAggregateScopes`在隔离Redis上启动两个scope，Ready已汇总为2，但目录量仍0。准确调用链中`ownedRuntimeMetrics`会完整相加，缺口在固定模式从未执行directoryKeys.Store；该写入原来仅在DynamicKeys成功读取页之后发生。本轮保留expected2，未改弱为0。原始`servicecontext-redis-normal`退出1，日志932字节，SHA256 `130fef0af9ebb1a5af239a5018deda42ac4ca70b23a618958ce36475473429b7`。

## 新回归合同与口径

- `ownership/runtime_metrics_test.go`使用真实Runtime（按测试确定性设置已有atomic状态）及正式Collector/Registry，分别核对4 gauge/11 counter的非零和已知零值、原四标签及Counter/Gauge类型。恢复累计纳秒转换1.5秒也逐值校验。随后真实本地HTTP端口使用正式promhttp处理器抓取全部固定名称。它不是运行业务owner或远端现场验收。
- typed nil Runtime保持unavailable且无指标样本，不能将未采集强行补0。
- `observability/collector_test.go`继续拒绝动态用户/消息名称；仅该边界输入使用既有测试Provider，不以Fake Provider代替真实Runtime的字段映射验证。
- 复用`router/servicecontext_owned_test.go`真实Redis接线，两个实际Runtime经原ServiceContext实例级汇总、默认Collector到Prometheus，确认目录量2与八个counter存在；不新增汇总器或调度器。

`directory_keys`源于每个Runtime已知目录，在ServiceContext中按原方式汇总本实例多个scope；不同副本可能重复看见同一目录，不能相加当全局唯一key数。counter按进程生命周期累计；restore_seconds包括失败恢复回调耗时，是累计秒而非单次延迟或P99。每Provider至多新增9个固定样本，不引入key/用户/订单标签。没有性能或容量改善承诺。

## 实际RED/GREEN与最终回归原件

证据根：`/private/tmp/core-ownership-metrics-fix-20261117/`。每项json记录实际argv、退出码、墙钟、log bytes/SHA，旧失败和中间轮均不覆盖：

| 原件 | 退出码 | 秒 | log bytes | SHA256 |
| --- | ---: | ---: | ---: | --- |
| metrics-red | 1 | 4.283 | 2660 | f3375bca1b1f7f5095ef5333528031b8fb337b26bd8d731526a696b0484f0005 |
| metrics-green | 0 | 3.881 | 594 | 89ba94a4e365f762a3383d3c1d09402ad972d73dd61eac9fa6f52e4aa3e2aa5e |
| servicecontext-redis-normal | 1 | 8.764 | 932 | 130fef0af9ebb1a5af239a5018deda42ac4ca70b23a618958ce36475473429b7 |
| servicecontext-redis-green | 0 | 9.559 | 486 | 7fa43513ef10e032ed364355a789079137972f4eb60134cb9ab286e1b8bc84c9 |
| servicecontext-redis-race-v2 | 0 | 12.016 | 486 | e581716625a9622ef4f02204ba0c5a1ef2f76bccc8d1d09a40e5c39f610f06fa |
| dynamic-redis-normal | 0 | 4.216 | 364 | c6b6777bf99b29733323b211783f8dec1078b8c33db0cd2c53771a0e4dbc1e16 |
| dynamic-redis-race | 0 | 5.477 | 364 | 0f643b0bb924bccfab2ab1a64c622c347a97692ab84882143666a370db8e75b0 |
| final-normal | 0 | 20.957 | 86465 | c04cfb27f4148be6c2bcfba65d4fd9e56da3513edf62fa69e4c7b4b7edf7ad08 |
| final-race | 0 | 28.325 | 86466 | 5d666602ee66dc717fb28a4493ff728169e16e3282515838ad38d72434c3a840 |
| logging | 0 | 0.234 | 0 | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| web-dist-sync | 0 | 5.439 | 176 | cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b |
| release-contract | 0 | 56.418 | 1913 | 271dff4c7e3d957d76598d347192995d9b6ec3b250771e5607d17e45f81a9d44 |

`metrics-red`在已知零/非零两种输入都因directory_keys被过滤而真实FAIL；动态未知名称拒绝原本PASS并保持。`metrics-green`只证明白名单候选，之后真实接线发现固定目录初始化遗漏，保留第二份FAIL，最终用一行真实计数初始化转GREEN。初始metrics-normal/race中间通过也保留，最终结果以final-normal/final-race为准。候选release-contract在目录补正前已通过；干净最终提交的正式release-check会重新执行API/配置/安全，结果由正式发布回执提供，不能拿候选轮替代。

实际命令如下，均由rtk代理执行；带Redis的用例明确指向本轮独占`127.0.0.1:58005`（native PID9269，save/appendonly均关闭），没有默认6379、消费方地址或现场数据：

```bash
rtk proxy go test ./pkg/server/ownership ./pkg/server/observability -run 'TestRuntimeMetricsSurviveCollector|TestCollectorRejectsDynamicOwnershipNames' -v -count=1 -timeout=60s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:58005 go test ./pkg/server/router -run '^TestServiceContextOwnedMetricsAggregateScopes$' -v -count=1 -timeout=90s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:58005 go test -race ./pkg/server/router -run '^TestServiceContextOwnedMetricsAggregateScopes$' -v -count=1 -timeout=90s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:58005 go test ./pkg/server/mq -run 'TestRedisOwnershipDynamicRuntimeDiscoversAppendWithoutRestart|TestRedisOwnershipFixedToDynamicMigrationPreservesPELAndFence' -v -count=1 -timeout=90s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:58005 go test -race ./pkg/server/mq -run 'TestRedisOwnershipDynamicRuntimeDiscoversAppendWithoutRestart|TestRedisOwnershipFixedToDynamicMigrationPreservesPELAndFence' -v -count=1 -timeout=90s
rtk proxy go test -p 1 ./pkg/server/ownership ./pkg/server/observability ./pkg/server/router -v -count=1 -timeout=180s
rtk proxy go test -race -p 1 ./pkg/server/ownership ./pkg/server/observability ./pkg/server/router -v -count=1 -timeout=180s
rtk proxy ./scripts/check-logging.sh
rtk proxy ./scripts/test.sh web-dist-sync
rtk proxy ./scripts/test.sh release-contract
```

最终包回归没有配置MySQL和外部Broker通用fixture，因此四项真实SQL/独立PID测试，以及部分Redis/NATS/关闭场景按原测试明确SKIP；日志完整保留，不把SKIP称通过。定向Redis场景则实际运行，覆盖真实服务接线、动态目录追加和保全迁移PEL/sequence/旧凭据拒绝。没有假称本轮重跑了全部真实SQL/mTLS/WS或多进程恢复；生产没有改变这些算法。

## 兼容、升级与本轮未验证项

生产仅9个固定白名单条目和一行观测初始化；没有新公开Go API/配置/依赖、StoreID、scope/fingerprint、SQL或消息/卷格式。旧指标保留，新增固定样本可被旧Prometheus正常忽略；旧副本仍缺样本或固定目录0，不能认证其新观测能力。按消费方自己的维护安排升级`github.com/digitalwayhk/core@v1.14.1`后重新同步权威skill。无需清理pending/history、重建组、回退游标、重注册scope或修改业务代码；不要求消费方中断当前对照或立即部署。

恢复计数随本地进程重启按原生命周期重新累计，Prometheus消费者仍需按counter使用；这是既有口径，无新增持久计数。回退正常停止相关进程并保留原数据，旧版回到旧观测缺口，不影响数据格式兼容；禁止移动已发布tag。

本轮NOT_RUN：新的真实MySQL、双进程/mTLS/WS、示例11全链、Full-E4、消费方接入/部署/发压、业务UAT、500/s对照及容量/性能认证、未配置源的外部futures smoke。普通/race、本地真实HTTP和定向真实Redis属于Core组件证据，不能替代这些结论。本轮没有操作Bitzoom仓库、环境或其压力，也没有更改其版本。

主Agent按正确性、可读性、架构、安全与性能五轴审查；不启动新Agent、不宣称外部Agent审计。当前原件保留两类真正RED、初始诊断及全部中间轮。正式提交/main/tag、官方Proxy/SumDB冷验包和封存校验由正式交付回执给出。
