# Outbox 持续健康流下的失败重试修复

2026-11-12。基线为正式 v1.11.1 提交 `7e8ba482455beb3d7511aebbd4e33a193bc5a090`；兼容补丁候选 v1.11.2。精确 main/tag、官方 Go SUM 和最终门禁由正式 Release 的交付回执绑定，不从候选测试推导消费方已接入。

## 根因与实际调用链

`ServiceContext.UseOutboxWithOptions → ServiceEventBridge.UseOutbox → outboxPublisher.run → drain → LoadPending[Skipping] → publishBatch → bridge.Publish → MarkPublished[Batch]`。旧 drain 在入口创建 blocked 集合；健康批次满批且有进展时一直 continue，失败 key 在整个 drain 内持续被跳过。健康输入不停，临时失败恢复也要等健康流耗尽才有重试机会。这与 SQL、Redis 连接池等待或业务数据是否合法不是同一问题。

v1.11.0/v1.11.1 的 outbox.go 原字节 SHA256 为 `2e9c2d9d988c445eb3d5e0ffe0f7515221535f6c063fe458929760a61200fe9d`，本轮从当前正式 git 对象重新核对。消费方所述发布标记时间差不是某函数时长，也不是精确首次 Broker 交付时间；本修复不声称解释全部现场延迟。

## 最小生产修改

只有两个生产文件：

- `pkg/server/event/outbox.go`：run 复用一个重试 timer；drain 每次取批前检查窗口到期和取消，失败状态属于发布器窗口而非可无限延长的健康 drain。保留原取批、发布、分 key worker、确认和指标实现。
- `pkg/server/event/outbox_retry.go`：`advanceOutboxRetry` 管理内存资格；到期清除技术屏障以重新读取 earliest-first 原件。Notify 同窗口最多提前恢复一次，不重置截止时间，通知风暴不能无限提前重试。

沿既有 Interval，非正值仍默认 100ms。失败批次返回后设共享截止时间；到期在下一次取批前重新检查。不是每条失败都有完整独立间隔，也不是固定端到端 SLO。一个未返回的批次仍可延迟检查。没有新增 worker、持久重试表、业务队列、公开配置或 API。

取批前 blocked 达到两批 key 数即返回原 worker 等待资格；一次最多新增 BatchSize 个，所以每批遵守 limit 时，集合峰值至多 `3*BatchSize-1`。该限制是条数，应用 key 的字符串长度仍由应用约束。它使屏障内存及 skip 参数不随无限健康流增长，饱和时有明确背压。最小批次 1 的单失败 key 仍允许健康 key 扫描；没有通过放大配置获取结果。

## 可靠性、升级和剩余边界

失败原件仍保存在原 store。到期只重试最早未发布记录，不确认、不跳过同 key 前序。发布失败和本地确认失败均保持；批量确认仍单事务全成/全败，结果未知可重放整个已发布前缀。EventID/ShardKey/幂等身份不变；发布成功不是消费 ACK，at-least-once 不等于 exactly-once。

- 原 UseOutbox/UseOutboxWithOptions、StoreID、消息、组、PEL、游标、SQL/Badger 格式保持；不迁数据、不清 pending、不重建消费组，无新开关。
- Notify 的失败资格提前恢复限每窗口一次；依赖无限连续 Notify 立即重试的调用方应接受此背压，不能自行补发布循环。
- 无 SkipBlocked 且失败前缀占满 LoadPending 窗口，后方健康 key 仍不可见；无限失败 key 达到技术预算也可背压健康扫描。本轮不增加分页或宣称全局无限公平。
- 原 runKeyedLanes 的同批等待保留；不合作 handler/网络调用不能被强杀。Close 仍等待实际发布退出；取消/超时不等于业务已停止。
- 内存退避不跨重启持久化；重启仍从原持久 earliest-first 原件重试。回退上一版不改变格式，但会恢复旧饥饿行为。

```bash
go get github.com/digitalwayhk/core@v1.11.2
```

继续原接线即可。消费方 AI 应按 README 和 CONSUMER_AI_SKILL_SETUP 重新链接精确版本的权威 skill；go get 不自动安装 skill。

## RED、失败原件与真实存储验证

证据根 `/private/tmp/core-outbox-retry-20261112`；每条 command.json 保存实际 argv、cwd、退出码和墙钟，原日志不覆盖。测试基于 Core 自有 barrierStore 独立构造，没有复制或修改消费方私有测试源文件。自动审批未允许该跨项目复制，故未执行；不能将消费方提供的 SHA 当作本轮已复跑证明。

旧正式源码的公开 API RED：持续健康输入仍开启，640 个满批、10238 条健康发布，A 前序尝试 1、后继 0，断言 OUTBOX_RETRY_STARVATION 真实失败 exit1。候选原断言保持，健康生产仍开时前序第二次成功、后继随后成功。首轮候选采用一批屏障预算，BatchSize=1 的健康进度为 0，真实 RED 已保留；改为两批预算后通过，没有放弱断言。

真实 Redis 使用本轮独占 native Redis 8.0.3、127.0.0.1 随机端口；正式 ServiceContext→EventBridge→Redis provider。独立故障 key 从未存消息，放合成字符串造成原生 WRONGTYPE；健康主题持续成功。故障恢复仅 RENAME 该合成字符串以保留原件，没有删除 Stream/PEL 或重置游标。候选在健康生产持续时按原 ID 收到 a1/a2，Stream 长度 2、PEL=0，原故障字符串保留。首次测试因短命启动脚本结束 Redis 而 connection refused，属夹具 FAIL，已保留并用持续运行的独占服务纠正；不能当成生产 RED。

普通全包已实际覆盖真实 Redis、两个独立消费者进程和真实 SQLite Inbox 事务，发布者在 Broker 接受后、Badger 本地确认前硬杀，再由新进程打开原 SyncWrites=true 的临时 Outbox；原身份允许重投，事务事实幂等。原生租约硬杀接管必须显式启用 CORE_TEST_SHARED_FULL_LEASE=1，结果单列，不以加速夹具代替。

NOT_RUN：Bitzoom/Full-E4 接入、部署、业务功能/UAT/容量；真实业务 MySQL；任意物理断电；未配置的 NATS/Kafka/RabbitMQ/etcd 外部验收；长稳态吞吐和全业务 P99。纯内存 RED、真实 Broker、持久存储/进程恢复、正式发布与消费方验收分别判断，不互相替代。

## 实际命令及结果

所有命令通过 `rtk proxy` 执行，表中测试时间为整条命令墙钟（含编译/工具），不是 handler 时延。下列 Redis 均是本轮独占地址 127.0.0.1:58129。旧源码 overlay 只把 outbox.go 还原为正式对象字节，并屏蔽候选专有私有状态 helper/单元测试以编译相同公开 API/真实 Broker 用例；当前工作树未回退，未修改模块缓存。

| 证据名称 | 命令（省略公共代理前缀） | 退出码 | 墙钟秒 |
|---|---|---:|---:|
| red-counted | `go test ./pkg/server/event -run ^TestOutboxRetryDuringContinuousDrain$ -v -count=1 -timeout 15s` | 1 | 13.453 |
| red-small-budget | `go test ./pkg/server/event -run ^TestOutboxRetrySmallBatchDoesNotBlockHealthy$ -v -count=1 -timeout 15s` | 1 | 10.784 |
| redis-targeted | `env CORE_TEST_REDIS_ADDR=127.0.0.1:56636 go test -p 1 ./pkg/server/event -run ^TestOutboxRetry -v -count=1 -timeout 90s` | 1 | 15.190 |
| redis-live-targeted | `env CORE_TEST_REDIS_ADDR=127.0.0.1:58129 go test -p 1 ./pkg/server/event -run ^TestOutboxRetry -v -count=1 -timeout 90s` | 0 | 13.102 |
| packages-normal | `env CORE_TEST_REDIS_ADDR=127.0.0.1:58129 go test -p 1 ./pkg/server/event ./pkg/server/router ./pkg/server/mq -v -count=1 -timeout 600s` | 0 | 78.942 |
| packages-race-native | `env CORE_TEST_REDIS_ADDR=127.0.0.1:58129 CORE_TEST_SHARED_FULL_LEASE=1 go test -race -p 1 ./pkg/server/event ./pkg/server/router ./pkg/server/mq -v -count=1 -timeout 600s` | 0 | 412.503 |
| baseline-build | `go test -overlay=<证据根>/baseline-overlay.json -c -o <证据根>/baseline.test ./pkg/server/event` | 0 | 15.979 |
| redis-baseline-red | `env CORE_TEST_REDIS_ADDR=127.0.0.1:58129 <证据根>/baseline.test -test.run=^TestOutboxRetryRedisWrongTypeWhileHealthy$ -test.v -test.count=1 -test.timeout=30s` | 1 | 7.111 |
| green-reviewed | `env CORE_TEST_REDIS_ADDR=127.0.0.1:58129 go test -p 1 ./pkg/server/event -run ^TestOutboxRetry -v -count=3 -timeout 90s` | 0 | 14.912 |
| close-normal | `go test ./pkg/server/event -run ^TestOutboxRetryCloseWaitsForActualPublisher$ -v -count=3 -timeout 30s` | 0 | 8.572 |
| examples | `go test ./examples/06-shop-microservices/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s` | 0 | 62.318 |
| config-contract | `./scripts/test.sh config-contract` | 0 | 14.171 |
| performance-contract | `./scripts/test.sh performance-contract` | 0 | 13.628 |
| release-contract | `./scripts/test.sh release-contract` | 0 | 122.762 |
| vet | `go vet ./pkg/server/event` | 0 | 27.310 |
| skill | `./scripts/check-ai-skill.sh` | 0 | 1.365 |
| logging | `./scripts/check-logging.sh` | 0 | 0.410 |

`redis-targeted` 的 exit1 为测试 Redis 已退出导致 connection refused，断言未进入实际故障恢复；`redis-baseline-red` 则是旧正式算法在真实已恢复 Broker 上无法重试的真实功能 RED，不能混为一谈。候选 green-reviewed 三轮均通过相同断言，健康生产未提前停止。

普通全包 434 个顶层 PASS、31 个 SKIP、0 FAIL；之后新增实际发布退出屏障普通重复三次 PASS。完整 race 含该新增用例及显式原生租约为 437 PASS、29 SKIP、0 FAIL，无 DATA RACE。SKIP 包括未配置的 NATS、进程入口 helper、显式独立测量和两个 Redis 网络 deadline 专项；不能算这些能力已验证。普通未开启的两个原生租约用例在 race 中已显式执行通过。

真实进程证据：普通消费者 PID34551/34553；race PID40755/40756。同组失败阻断与正常退出恢复通过。race 硬杀 PID40901→接管40902，未加速租约 120.004s；共享 key 硬杀45252→接管45257，119.842s。持续归属既有回归原生15s租约普通15.297s/race15.916s，另有明确标注900ms的网络失联注入，两者不混称。发布后未确认的真实 Badger Outbox 硬杀/新进程恢复及 SQLite Inbox 幂等普通/race均 PASS。

真实 Redis 故障恢复候选：首次独占测试 healthy255/full60；普通全包 healthy241/full60；race healthy122/full35；均在健康生产仍开启时收到原 a1/a2、StreamA=2、PEL=0。计数是不同测试窗口的进展见证，不是稳态吞吐对照。旧真实 Redis 功能 RED 在恢复故障后5秒内仍未重新发布前序；其生产源码及二进制 SHA 在 baseline-binding.json。没有用这些数字承诺现场146秒或所有长尾均消失。

Core 自有 fixture 的 SQL 是临时 SQLite，不是 MySQL；06/07 示例测试是普通包回归，未启动真实消费方多服务UAT。性能契约检查通过不等于容量或延迟目标认证。正式 main 干净提交的 --release 门禁、tag、代理/SUM和公开附件验真由发布回执另列。

## 可审证据包

[验证原件选集](evidence/outbox-retry-20261112/evidence.tar.gz)包含独立RED、真实Redis旧源码RED、候选GREEN、实际命令、原始日志哈希及主Agent自审（不是独立模型审计）。 `source.diff`：3965 bytes，SHA256 `90f5b715941bc44ae0883fa7e96a35ccc90f471c3870dab36d0264842bebe921`。 `evidence.tar.gz`：40911 bytes，SHA256 `3b425c7e110193298d57b7f07b332af030a6b9264ae651817ce3d766b2e61caa`。
