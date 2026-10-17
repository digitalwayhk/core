# 07 订单写入、Outbox 与消费积压压测

## 测量边界

基线提交 `5278d471099ce5728fd86fe4b950eff51b70889f`，分支 `feat/stable-instance-discovery-recovery`。本次仅增加压测入口、复用既有进程夹具及记录结论，不修改业务同步策略、Core 发号/发现、Outbox/MQ 实现或安全上限。

宿主机为 Apple M3 Max、64 GiB 内存，Go 1.26.6、darwin/arm64、16 个逻辑 CPU；Docker Desktop 为 8 CPU、约 31.3 GiB 内存。应用在宿主机启动真实 user/supplier/order 三进程，内部使用 mTLS，order 单副本显式 stable，身份与 pending 独占，远程订单与 Outbox 使用专用 MySQL 8.0，事件使用专用 Redis 7.4 Streams。Redis 使用 AOF everysec；不以本次正常运行压测承诺断电持久性。宿主机仍有其他项目运行，属于同机开发环境的有界观测，不是生产容量认证。

采用 performance-optimization 与 ponytail：先测量，不调大批量、并发或连接池来制造通过。压测不用 race；功能/race 检查另行执行，不能将 race 开销作为性能结果。

## 负载与正确性口径

- 正式计时前创建真实供应商、商品与买家 Token，并用真实下单/查询预热。
- `POST /api/shop-user/addorder`，同一买家/商品，每个请求独立 requestID，数量为 2；保留商品查询、认证、内部调用、远程幂等查询及本地可靠提交。
- 开放发送速率：500 次/秒持续 10 秒，再以 2000 次/秒持续 30 秒；每轮共 65000 笔，另有预热订单 1 笔。
- 发送端最多 512 个在途请求，5 秒超时；无自动写重试。记录计划数、实际发送数、发送器丢弃、调度延迟、逐请求结果及延迟。报告“计划窗口内完成”和“含末尾在途请求完成”的口径，不能将目标 RPS 当实际吞吐。
- 每秒采样真实 pending depth、SQL 订单数、Outbox 总数/已发布数、进程 CPU/RSS/goroutine 与本地磁盘指标。缺指标记错误，不填零。
- 第二轮另采样订单流 `shop-user` 消费组的 pending、lag 和 entries-read，等待数据库、Outbox 与 ACK 全部排空，最多观察停载后 360 秒。
- 逐笔核对所有成功响应的 requestID/OrderID 与 MySQL 持久事实。失败但实际落库单独登记，不能当作已确认成功或静默丢失。

## 第一轮结果：持久化与发布

| 阶段 | 请求数 | 成功/失败 | 含在途收尾的成功吞吐 | 计划窗口内实际完成吞吐 | P50 / P95 / P99 |
| --- | --- | --- | --- | --- | --- |
| 500 次/秒，10 秒 | 5000 | 5000 / 0 | 499.26 笔/秒 | 499.6 笔/秒 | 5.65 / 11.44 / 76.83 ms |
| 2000 次/秒，30 秒 | 60000 | 60000 / 0 | 1999.36 笔/秒 | 1999.1 笔/秒 | 13.97 / 101.06 / 218.73 ms |

两档发送器丢弃均为 0，最大调度滞后分别约 7.45ms、7.90ms。最后约 27 笔高档请求在 30 秒窗口结束后完成，所有请求收尾共约 30.010 秒。

- 65000 笔压测订单全部逐键匹配，确认丢失/主键不符为 0，失败但落库为 0。
- pending **采样峰值 40881**，本地磁盘指标峰值约 22.94 MiB；只表示每秒样本中的最大值，不是连续时间精确峰值。
- Outbox 未发布采样峰值 **11100**。
- 停载后 pending 约 **69 秒**清空；订单与 Outbox 全部完成约 **79.09 秒**。约 69 秒来自秒级采样，不能解释为精确到毫秒的排空测量。
- 最终 SQL 为 65001 条订单、65001 条 Outbox 且全部 published；含预热 1 条。
- 120 份周期样本，无采样错误。order 进程 RSS 采样峰值约 538.58 MiB，goroutine 采样峰值 320；user 进程分别约 133.97 MiB、775，supplier 约 75.89 MiB、74。
- 第一轮用例退出码 0；这只证明其持久化/发布断言通过。退出后 Redis 流仍保留 65001 条事件，消费组 entries-read=26125、pending=0、lag=38876，**不能称为消费排空**。此原始状态已另存 Redis RDB，不用第二轮覆盖。

## 第二轮结果：含消费 ACK

第二轮使用全新 Redis/MySQL，保留第一轮原数据；同样的业务实现和负载，增加消费组观测及排空条件。

| 阶段 | 请求数 | 成功/失败 | 含在途收尾的成功吞吐 | 计划窗口内实际完成吞吐 | P50 / P95 / P99 |
| --- | --- | --- | --- | --- | --- |
| 500 次/秒，10 秒 | 5000 | 5000 / 0 | 499.54 笔/秒 | 499.0 笔/秒 | 5.45 / 8.54 / 14.83 ms |
| 2000 次/秒，30 秒 | 60000 | 60000 / 0 | 1999.37 笔/秒 | 1999.27 笔/秒 | 11.48 / 40.30 / 87.59 ms |

- 两档发送器均无丢弃，最大调度滞后分别约 3.64ms、17.78ms；高档最后 22 笔在窗口结束后完成，全部收尾约 30.010 秒。
- pending 采样峰值 **39832**，Outbox 未发布峰值 **11600**，消费组未读 lag 峰值 **39638**，已领取未 ACK 的 pending 峰值为 1；三者不可混用。
- 停载后 pending 约 **66 秒**清空，数据库/Outbox 约 **78.07 秒**全部完成，消费 ACK 全部完成约 **175.51 秒**。前一项为秒级采样，后二项由排空检查记录。
- 最终 65001 条订单、65001 条 Outbox 且全部 published，Broker entries-read=65001、lag=0、pending=0；含预热事件 1 条。65000 笔成功响应逐键核对全部一致，确认丢失/主键不符=0，失败但落库=0。
- 216 份周期样本，采样错误=0。order RSS 采样峰值约 532.14 MiB、goroutine 172；user 约 121.41 MiB、329；supplier 约 74.08 MiB、72。pending 磁盘指标峰值约 25.17 MiB。
- 测试退出码 0。此通过只证明有界负载下最终恢复和正确性，没有证明后台具备持续 2000 条/秒稳态容量；实际队列增长已说明当前单副本配置跟不上输入。

两轮高档延迟存在明显波动，不能将第二轮较低的 P99 解释为优化收益：业务实现没有改变，开发机上仍有其他进程与共享物理资源。

本次结论是：**短时接单达到目标，后台稳态处理达不到 2000 条/秒；停止输入后可以自动排空，已确认订单未观察到丢失。** 后续优化应同时覆盖同步调度、索引扫描、发布与消费，不能只放大 Outbox 的批量。

## 发现与建议

| 编号 | 证据与影响 | 建议与验证边界 |
| --- | --- | --- |
| L-01 / P2 | 07 `order-service/service.go` 的 100ms ticker 每轮仅 `DrainOnce(...,100)`；理论最多约 1000 条/秒。实际真实接单约 2000 笔/秒时 pending 增至约 4.1 万条，短窗口成功不代表持续稳态可承载 | 保留有界时间/条数预算，有进展且仍积压时及时续跑，错误退避并支持取消；优先核对框架已有自适应 write-behind 入口，避免再加一套业务无界循环。必须重新测持续输入、排空和背压，不能只把 100 改成更大值 |
| L-02 / P2 | `pkg/persistence/database/nosql/sharedbadger_force_sync.go:ForceSyncBatch` 每次传 `rebuild=true`；`sharedbadger.go:rebuildSyncQueue` 遍历整个模型前缀、反序列化值并检查队列索引，之后才按 limit 同步。limit 约束同步条数，没有约束此前全扫描工作 | 把旧格式补索引限制在初始化/明确修复边界；正常运行使用持久队列索引有界读取。保留崩溃恢复、旧数据升级、并发写入与 ACK 一致性。当前代码扫描路径已确认，但未采 CPU profile 定量归因，不能把所有延迟归到这一处 |
| L-03 / 性能约束 | 07 `user-service/service.go:userEventSubscriptions` 没显式 KeyConcurrency；Redis `runReliableSubscriber` 默认整 subject 串行、每次读 1 条，并执行 owner 校验、pending/reclaim 和 ACK。第一轮数据库/Outbox 排空时仍有 38876 条未读取消息 | 高吞吐场景需明确 ordering key 与重复处理幂等后，再评估已有分 key 并发、批量读和 ACK 路径；保持同 key 顺序、旧 owner 隔离与失败屏障。不能跳过 owner/reclaim 或粗暴全局并发。零值串行是现有兼容契约，本次不将其误报为消息丢失缺陷 |
| L-04 / 验收口径 | Redis 消费组 pending=0 只表示已领取未 ACK 集合为空；lag 仍可很大。Outbox published 只证明发布，不能证明消费成功 | 验收分开观测 Badger pending、SQL unpublished、消费组 lag 和 pending，并在观测缺失时明确 Partial。持久事件已发布后不得因消费者慢而直接裁剪流或删除恢复数据 |

默认 Outbox 的 100 条是单次取数批量；满批且有进展会连续处理，不存在固定每秒 1000 条调度上限。第一轮仍观察到 Outbox 积压，说明真实发布、确认与数据库路径也有实际成本；本次没有独立 Outbox 饱和压测或剖析，不能把它宣称为已具备 2000 条/秒端到端容量。

## 建议优化顺序（方案，尚未实施）

首期以现有单 order 副本持续处理 2000 笔/秒为目标，保留真实 HTTP、mTLS、远程幂等探测、Badger 持久提交、MySQL 事务、Outbox 与消费 ACK。业务事实及 Outbox 同事务、远端成功才 ACK 本地、同 key 失败屏障、stable 准入与旧 Boot 隔离均保留。

| 路径 | 代价与适用范围 | 选择 |
| --- | --- | --- |
| 仅调大批量或缩短 ticker | 修改小，但重复扫描与串行消费仍存在，还可能增加事务/IO 压力 | 可作为对照实验，不作为完整方案 |
| 直接增加 order 副本 | 能分担本地接单/同步；共享 MySQL 与同一消费组 active owner 仍可能成为瓶颈 | 单副本问题明确后再做容量矩阵 |
| 接回已有自适应 worker，再优化发布/确认及分 key 消费 | 复用已有恢复、退避、确认及并发能力，分阶段验证每层队列 | 推荐 |

### 1. 先统一 pending 调度，复用现有框架入口

07 当前 `AutoSync=false` 并另启 100ms 业务循环。改为显式配置现有自适应 worker：`AutoSync=true`，候选起点 `SyncBatchSize=512`、`SyncFlushThreshold=32`、`SyncMaxCollectDelay=20ms`、`SyncBacklogDrainDelay=0`；这些是待测参数，不是默认值变更或吞吐承诺。

移除重复的业务 ticker，由标准 `UseWriteBehind(OrderWriteBehindTarget)`、`ServiceContext.UseResource` 统一负责调度和资源关闭。当前 adaptive 构造器已在返回前初始化/恢复索引；正常 worker 调用队列处理，不逐批走 `ForceSyncBatch` 的全前缀重建。因此第一步可以同时避开固定等待和热路径重复扫描，不必先重写通用同步器。手动 `ForceSyncBatch` 的恢复语义需另外审查所有调用方，保持兼容，不能直接删去重建逻辑。

接入时在开始远程事务前保留服务准入检查；通用存储 worker 本身不知道 stable ServiceContext，不能只改 `AutoSync` 而遗漏门禁。已有 Outbox 周期扫描负责可靠兜底，`NotifyOutbox` 只是成功同步后的低延迟唤醒，不是数据可靠性条件。失败/零进展继续退避，恢复沿用原卷，关闭仍检查未确认 pending。

### 2. 再减少 Outbox 的数据库往返

当前 `MarkPublishedBatch` 已合并事务，但 `MarkOutboxPublishedByIDs` 内部仍逐条 Update。先对待处理点查、批量确认查询设置适当的 `SkipCount=true`，并在 models 当前 MySQL 事务内实现真正的有界批量状态更新，只更新 published、发布时间和必要元数据。先核对并锁定完整 ID 集合，保持空集合无事务、已发布幂等成功、缺失 ID 报错及整批回滚，不因优化跳过模型约束、元数据或审计语义。索引是否需要补充由实际执行计划决定，通过框架启动阶段 EnsureStorage 管理。

继续保留 earliest-first 加载、发布成功前缀确认与失败屏障。独立测量发布耗时和确认 SQL 成本后，再通过已有 `UseOutboxWithOptions` 评估分 key 并发；无需为了唤醒或并发新造另一套发布器。

### 3. 消费端按业务 key 并发，复用已有 keyed-reliable

为同一订单的创建/状态/支付事件统一提供稳定 ordering key，例如服务名与 OrderID 的组合；生产侧 Outbox 和 Broker 载荷保持一致。消费者先核对缓存失效、WebSocket 通知和去重处理的并发安全，再显式设置 `Subscription.KeyConcurrency`，依次测试 4、8，必要时才扩至 16。当前 Redis keyed 路径已有有界窗口批量读取和按 key 调度，同订单串行、不同订单并行；保持 owner、reclaim、ACK 及失败 key 隔离，不能把 subject 简单改成无界并发。

先验证现有 keyed 路径是否达到目标；若 owner/ACK 的网络往返仍占主导，再对 Broker 调用做有界批量或 pipeline 优化，先证明所有权检查及 ACK 的原子边界不变。数据库事务、Lua 和 handler 不能被任意合并来换吞吐。单发布实例的 KeyConcurrency 也不代表多个 publisher 间已获得同 key 全局顺序，多副本须单独验证。全局默认仍保持旧串行契约，应用显式启用。

### 4. 每一步独立对照，再做稳态和故障验收

每阶段保持相同业务与持久化参数，对比原始基线和上一阶段，采集三层队列、最旧待处理时间、延迟、SQL 次数、每批条数、CPU、RSS 和磁盘；至少重复 5 次再判断稳定收益。用 CPU profile/SQL 与 Broker 指标定量归因，不把代码中的结构性开销直接当成全部耗时。

最终建议验收为持续 2000 笔/秒 10 分钟：真实发送达到目标且发送器无丢弃，成功率至少 99.9%，已确认数据零丢失/错键，三层队列在稳态无持续增长，消费 lag 和 pending 均可排空。停载后 5 秒内各层排空作为首期目标，属于拟定验收目标，并非当前实测能力。保留本地 pending/磁盘背压；观测缺失或达到既有资源硬界时终止负载并记为未达标，不能提前结束窗口后宣称稳态通过。

再验证断网、重投/重复、同 key 顺序与 revision、失败 key 对其他 key 的隔离、硬崩溃原卷恢复、旧 Boot 与关闭保护，以及相关包 race/公开契约回归；最后才做 1/2/4 副本扩展测试。实际生产消费方升级另列迁移范围，本方案不隐式授权。

## 复现与证据

新增入口 `TestStableOrderWriteLoad` 默认跳过，只有明确开关和专属依赖时运行：

```bash
CORE_TEST_ORDER_LOAD=1 \
CORE_TEST_LOAD_EVIDENCE_DIR=/private/tmp/core-order-load-new-run \
CORE_TEST_REDIS_ADDR=127.0.0.1:16390 \
CORE_TEST_STABLE_MYSQL_ADDR=127.0.0.1:13390 \
CORE_TEST_STABLE_MYSQL_PASSWORD='<专用测试库密码>' \
go test ./examples/integration/07-shop-order-scale-multi-process \
  -run '^TestStableOrderWriteLoad$' -count=1 -timeout=10m -v
```

每轮目录必须独立；Redis 中不能有其他流量或上轮遗留消费组，本轮数据库由唯一 namespace 自动创建。不要将命令地址改成生产或共享测试资源。

原始证据分别位于 `/private/tmp/core-order-load-20261018-run1/`、`/private/tmp/core-order-load-20261018-run2/`：环境配置、逐请求 JSON、秒级样本、最终 summary、真实进程日志及数据库/Broker 快照。第一轮的夹具源码也单独封存，明确它未等待消费排空；第二轮不能覆盖这一历史范围。

压力窗口结束后，受影响的 `examples/integration/07-shop-order-scale-multi-process` 整包 race 回归通过（72.766 秒），含复用夹具后的双副本、原卷重启及真实积压硬崩溃恢复；Docker/Runtime 外部开关用例仍按原契约跳过，压测默认开关也验证为 skip。`go vet`、日志检查和 skill 权威源检查通过。此次没有重新运行全框架 245 包，没有将局部回归宣传为全框架再次验收。

本次未运行长时间稳态、分 key 并发调优、横向副本容量矩阵、网络/磁盘故障注入或生产消费方压测；两轮同机结果不提供统计显著的性能提升结论。
