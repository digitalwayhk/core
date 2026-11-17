# Ownership指标导出白名单缺口登记（2026-11-17）

状态：**OPEN／已确认，尚未修复**。本次只核对与登记，不修改生产实现、不发新版本，不阻塞消费方已自行决定继续的本轮验证。

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

NOT_RUN：本轮没有生产修复，未运行修复后的普通/race、真实owner新场景、HTTP scrape、Full-E4、部署或压测。此前v1.14.0功能回归结论保持其原证据边界，不因本登记升级为新的现场结论。
