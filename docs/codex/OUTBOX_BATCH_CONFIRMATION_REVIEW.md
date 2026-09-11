# Outbox 批量确认审计与 v1.2.0 验证

## 代码审查结果

范围：仅摘取 `87cfa747e3b9972398af0809d5638afb65927a0b`（16 个文件），集成基线为本地 main `1f1c6c8`。未合并整个后台分支。
目的：可选批量确认降低 Outbox 确认事务次数，保留旧 store 与可靠投递语义。
模式：交互式；按 AGENTS.md 在主线程顺序完成正确性、测试、维护性、API、性能与故障场景审查，未声称有独立子代理复核。

### 已应用并验证

| # | 文件 | 修复 | 审查方向 |
|---|---|---|---|
| 1 | `examples/07-shop-order-scale/order-service/models/transaction/outbox_persistence.go:113`，以及示例 06 Order:52 / Supplier:53 | 拒绝零主键，更新前核对完整 ID 集合，缺失即失败 | 正确性、API |

问题级别 P2，置信度 100：原代码跳过零 ID，且查询缺少记录时仍返回 nil，违反“整批成功”的可选接口契约。三个 store 的同一回归用例均先以“预期错误但得到 nil”失败，再经修复通过。已发布记录仍可重复确认，重复 ID 去重。

补充：将确认失败测试的固定 sleep 改为等待实际确认调用；销毁旧发布器并重建后，断言原 EventID 重投并最终确认。新增真实 MySQL 第二条 Update 注入失败、整批回滚、缺失 ID 回滚、重试提交及重复确认测试。

### 测试证据

2026-09-11，Go 1.26.6。原始日志：`/tmp/core-outbox-review-PUkJal/`。

| 验证 | 结果 | 证据 |
|---|---|---|
| 三个 store 缺失 ID 回归 RED→GREEN | PASS，先 3 个失败后修复 | `red.log`；`race.log` / `mysql-race.log` |
| event、示例 06 两个 store、nosql 全包 race 三轮 | PASS | `race.log`；event 5.059s，nosql 37.882s |
| 示例 07 transaction 全包 race 三轮，显式启用真实 MySQL 批量确认 | PASS | `mysql-race.log`，4.384s |
| Core pkg/internal/service 与示例 07 全部包 | PASS，55 个有测试的包 | `core-suite.log` |
| 示例 06 全部包、同进程与三进程 Redis/mTLS/SQLite 集成，race | PASS | `microservices.log`，集成分别 21.920s / 120.238s |
| release-contract（含 API、公开表面、配置、安全） | PASS | `release-contract.log` |
| gofmt、定向 go vet、logging、AI skill、web-dist-sync、diff check | PASS | 当前审计执行记录 |

真实 MySQL 使用专用容器 `core-adaptive-mysql-20260911`（8.0.46），启动后实际映射 `127.0.0.1:52774`，数据库由框架自动创建为独立 `core_outbox_*`；首轮沿用旧端口 50029 的连接检查失败不计为代码 RED，核实 Docker 新端口后重跑通过。Redis 使用专用 `core-internal-notify-redis`，测试使用独立前缀。未连接 Bitzoom 数据库。

### 兼容与发布边界

- 只新增可选 `OutboxBatchMarker`，未实现的 store 保留逐条 `MarkPublished`；不存在强制迁移。
- 批量确认仅更新 Outbox 的发布状态，不是消费 ACK，不改变 MQ 生命周期、必需组或 Broker 回收；`pkg/server/mq`、`pkg/server/cluster` 与 origin/main 无差异。
- 确认失败可能重放已发布前缀；同 key 首次发布保持串行，重复消息不承诺单调位置，消费方必须保持 EventID 幂等。
- Write-behind 自适应配置全零保持历史行为；已有 pending 无需清空。实现及真实 MySQL 性能证据见 [同步指南](WRITE_BEHIND_SYNC_GUIDE.md) 与 [验证记录](WRITE_BEHIND_ADAPTIVE_VERIFICATION.md)。
- 事务数减少并不保证 redo/fsync 一定下降；低流量短收集窗口可能增加提交次数。
- 后台子模块与内嵌 dist 未变化，用户后台工作区未修改。
- 版本按仓库加性 API/能力的 MINOR 规则选择 `v1.2.0`。
- NOT RUN：Bitzoom 部署、R60/容量复测、示例 07 Docker 全拓扑及 Runtime 浏览器 UAT、生产 Broker 故障恢复。本次未改 Broker 代码；外部依赖默认跳过的测试不计为真实 Broker 验证。
- 发布后的消费方自行锁定正式 tag，升级和业务验收不由本次 Core 测试替代。

---

> 结论：原提交存在一个已修复的确认契约缺口，当前无剩余已确认阻断项；可与 write-behind 自适应能力一起进入 main 发布。

