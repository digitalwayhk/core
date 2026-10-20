# Redis 可靠订阅跨窗口等待修复与验收

日期：2026-10-20。基线：`v1.4.1` / `60791a7c0907acf55fc8fa3b3216c43ff929dcac`。本地分支：`fix/redis-reliable-cross-window`。本次未发布版本、未修改消费方依赖、未修改或运行 Bitzoom 业务代码。

## 根因与归因边界

当前基线确实保留了跨批屏障。`runReliableKeyedSubscriber` 的失败重试、`XAUTOCLAIM` 分页和 `XREADGROUP` 新读取三处调用 `runReliableKeyedBatch`；该函数虽然允许不同 key 并行，但末尾 `wg.Wait()` 要等本批所有 lane 完成，读取循环才继续。因此 A1 未完成时，即使 B1 已成功且有空闲执行能力，后续读取窗口的 B2 也无法推进。

本仓库初始 `keyed_scheduler.go` 与用户隔离探针的逐字副本 SHA-256 均为 `3913623ddb2e5510e4414d69a8d2265370ce363badaf862a014ad20eb0184a91`。本次先在未修复源码运行真实 Redis 用例，得到新读取和 PEL 后页两个 RED，之后才修改生产代码。最后还用 `git show 60791a7:<path>` 的逐字源码 overlay 复核基线，并增加正式桥接链的同断言 RED/GREEN，没有修改模块缓存。

实际链路已核对：Redis provider → `processReliableMessage` → `MQBridge.SubscribeReliableWithOptions` 的反序列化回调 → `Stream.PublishControl` → 注册的控制 handler。桥接层透传显式并发度，控制 handler 的 error 会回传 provider；未发现需要由消费方改业务接线来规避的问题。

现场 `published_at → Users handler` 的 11,333.538ms 是发布成功后的数据库标记与业务入口之间的观测间隔。`published_at` 不是精确 XADD 时钟。本次证明并修复了跨窗口屏障，**没有证明整段 11.33 秒全部由该屏障造成，也没有对现场修复后的延迟作容量认证**。

## 实现与可靠性

- worker 属于 owner 会话，不再属于一次读取批次。固定 `KeyConcurrency` 个 worker，通过无缓冲 job channel 和容量为并发度的结果 channel 交接；主循环不等待整批完成。
- 本地每 key 只保留最早未完成 work，后续消息留在 Redis PEL。失败 work 留住本 key 的屏障并单独退避；其他 key 不等待它的 handler。就绪 work 按准入顺序分配 worker。
- PEL 每轮最多处理一页，新读取也最多一页；新消息先进入 PEL，再按 Redis 消息顺序准入，不能绕过旧 pending。work 成功或复核已 ACK 后将扫描 cursor 回到起点，防止此前因同 key 正在执行而跳过的旧消息被后页越过。
- 会话内续约不重新抢占租约。owner 检查与 `XAUTOCLAIM`、`XREADGROUP` 分别在同一 Lua 命令执行；普通 ACK、带重试状态清理的 ACK、DLQ 原子转移也在 Lua 中校验 owner。失主、取消、失败均不得被当作 handler 成功确认。
- ACK 响应丢失时，重试前精确查询该消息的 PEL。已不在 PEL 的工作释放屏障，避免本地保存的失败 work 异常重放已 ACK 消息；仍在 PEL 时沿用 at-least-once 重试和原幂等契约。
- Redis 分 key 模式中，超时/取消后等待原 handler 真正退出，再重试或退出会话，避免遗留 handler 与下一次同 key 重试并行。已有重试次数、退避和显式 DLQ 策略保留；没有降低重试上限或用 DLQ 掩盖阻塞。
- 移除已无生产调用的批次 scheduler/helper 及其孤立测试，改由真实 provider 的顺序、失败和并发测试守住行为。

## 有界性和公平性边界

设并发为 N，有效窗口 W=`max(Count,N)`；`Count<=0` 时订阅入口补为 `4*N`。

| 资源 | 上限或规则 |
|---|---|
| 实际业务 handler 并发 | N；配置超时时附加的 handler goroutine 也不会跨超时累加 |
| 本地 work / 失败屏障 | 最多 W 个 key，每 key 一条 |
| 首次读取时钟缓存 | 最多 W 条 |
| 单次 PEL 返回 / 临时 work 切片 | 最多 W 条 |
| job channel | 无缓冲 |
| completion channel | 最多 N 条 |
| 本 owner 新读取造成的组 PEL | 最多 W 条，还受较小的 `Retry.MaxAckPending` 限制 |
| 接管已有的超限 PEL | 只分页恢复，不加载全量 key 集合，不继续扩大预取 |

内存按消息条数有界，字节数仍取决于单条 payload 和 key 的大小，本次没有增加新的业务消息大小限制。累计指标保存在 provider 的固定 atomic 字段，取消订阅不会累积历史订阅对象，也不会使累计 counter 回退。

公平性测试保留失败的 A pending，并连续发布 32 个 B 后续消息，验证旧 pending 重试和新消息两路都有进展；另将 72 条旧 PEL 放入 4 条窗口，验证分页、回扫及同 key 顺序。空闲时最多等待 100ms 再探测 Redis，完成、取消会立即唤醒；已准入且有空闲 worker 的 work 不等待该空闲探测间隔。

**预算满时明确背压新读取**。若整个预取预算都被慢 key/失败 key 的消息占满，尚未读取的后方 key 可能等待预算释放；有限预取不能承诺越过任意多热 key 消息找到后方 key。接管超限旧 PEL 时也先恢复到预算内。这与“已准入窗口因某一 lane 未完成而整体停止”的旧屏障不同，不通过无限预取消除背压。

## 观测口径

启用消费方既有 OTel SDK/exporter 后，使用 `redis.publish`、`redis.reliable.read`、`redis.reliable.delivery`、`redis.reliable.handler` span。通过 **stream hash + Redis message ID** 关联，消费侧再带 group hash；message ID 只在 stream 内唯一，不能单独作为跨 stream 的全局身份。

| 阶段 | 时钟/观测 | 限制 |
|---|---|---|
| 发布后等待读取 | XADD span 起止、stream ID 毫秒时间、`read_source=new` 的读取 span | ID 时间与读取进程壁钟有时钟误差；不是发布者 DB 的 `published_at` |
| PEL / reclaim | `read_source=reclaim`、读取命令起止、`redis.claim`；本会话已知首次读取时记录 `redis.read` | 接管前首次读取未知，`first_read_known=false`，不伪造历史 PEL 时钟 |
| key 调度 | `scheduler.admit`、`scheduler.wait`、`delivery.start` | 重试的调度等待包含本 key 退避；与 handler 时间分别累计 |
| handler 自身 | 独立 handler span 起止；`delivery.end` 在整个处理/ACK 尝试之后 | handler span 不等于 ACK 成功；未知或失败结果仍保留可靠语义 |

新增固定指标名经 `ComponentCollector` 白名单导出为 `core_component_gauge/counter{component="mq",name="..."}`，不添加业务 ID 标签：

- gauge：`prefetch_capacity`、`prefetched_messages`、`queued_work`；已有 `pending_keys` 现在表示本地跟踪 key 数，`active_keys` 表示已派发 work 数（含短暂待收割完成），`blocked_keys` 为本地失败屏障数，不代表全量旧 PEL key 数。
- counter：`reliable_new_read_total`、`reliable_reclaim_read_total`、`reliable_publish_to_read_seconds_total`、`reliable_pel_wait_seconds_total`、`reliable_pel_wait_samples_total`、`reliable_schedule_wait_seconds_total`、`reliable_scheduled_total`、`reliable_handler_seconds_total`、`reliable_handler_calls_total`。

`reclaim_read_total` 统计实际取回的 PEL 条目，包含重复扫描仍在运行/阻断的消息，不是业务重投次数。PEL 等待总量只累计首次读取已知的样本，必须结合样本数解读。已有 `redelivered_total` 的 lifecycle 语义不变。span 不记录正文、原始 OrderingKey、业务幂等 ID、token、DSN 或业务 error 正文；message ID 和 stream/group hash 也不成为指标标签。观测测试检查时钟顺序、关联身份和敏感字段排除，并实际 Gather Prometheus 指标。

## RED / GREEN 证据

原始本地证据：`.temp/redis-window-20261020/`。最终绑定和结果索引见该目录 `verification-summary.json`、`source-manifest.json`；JSONL 保存完整测试输出，未用重试覆盖失败日志。

| 场景 | RED | GREEN |
|---|---|---|
| 新读取及 PEL 后页的 B2 | 基线两个子用例均超过 1s，A1 未释放 | `TestRedisReliableKeyedNextWindowDoesNotWaitForUnrelatedKey` |
| 正式 EventBridge 链 | 固定基线 B2 不能进入注册 handler | `TestMQBridgeRedisReliableNextWindowReachesRegisteredControlHandler` |
| 旧 handler 失去 owner 后重新抢占 ACK | 固定基线错误返回 nil、消息已被 ACK | `TestRedisReliableHandlerCannotReacquireLostOwnerToAck` |
| 原超时 helper 与持续调度组合 | 150ms 内实际进入 5 次，旧 handler 未退出 | `TestRedisReliableKeyedTimeoutWaitsForHandlerBeforeRetry`，只进入 1 次 |
| ACK 实际成功、返回错误的注入 | 持续调度初版执行 first 两次 | `TestRedisReliableKeyedLostAckReplyDoesNotReplayAlreadyAckedWork`，first 只执行一次 |
| Collector 导出 | 新 gauge 在 Gather 中为空 | `TestCollectorExportsReliableQueueAndClocks`，3 个 gauge、9 个 counter 均可导出 |

超时和 ACK 响应丢失两项 RED 是本次持续调度开发中暴露的边界，不冒充在未改基线的正式桥接链上都已独立复现。测试夹具编译错误、重复 Close、非原子完成观测的中间失败另有原始日志，均不作为根因 RED 或最终通过证据。

真实 Redis 覆盖：同 key 多页严格顺序、A 失败而 B/C 完成、并发与预取上限、持续输入、取消/Close 等待 worker、独立 owner 变更退出、失主无法读取/认领/ACK/DLQ、ACK 消息不异常重放、合法重复依靠消费方事件身份幂等、正式桥接链。真实子进程在 handler 中 `os.Exit(73)`，不运行 defer/Close；父进程确认 PEL 留存，随后仅将该崩溃 owner 的 Redis TTL 加速到 1ms，再验证新 owner 恢复，不是删除消息或重置消费组。

使用独占 `redis:7.4-alpine`，容器名 `core-redis-window-20261020`，128MiB 内存、1 CPU、64MiB tmpfs，无持久数据卷。测试地址 `127.0.0.1:56535` 仅是本次随机映射。命令中的地址替换成新独占 Redis，不得指向 Bitzoom 或其他业务实例：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:56535 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 \
  go test -p 1 ./pkg/server/mq ./pkg/server/event ./pkg/server/observability -count=1 -json

rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:56535 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 \
  go test -p 1 -race ./pkg/server/mq ./pkg/server/event ./pkg/server/observability -count=1 -json

rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:56535 \
  go test -p 1 -race ./pkg/server/mq ./pkg/server/event \
  -run 'TestRedisReliableKeyed|TestRedisReliableHandlerCannot|TestMQBridgeRedisReliableNextWindow' -count=10 -json
```

`CORE_TEST_LIFECYCLE_PAUSE_REDIS=1` 会暂停该 Redis 200ms，只可对专用 fixture 启用。`-p 1` 保证不同包不会污染 Redis 全局命令统计。最终普通回归、race、重复回归的精确数量与耗时由结果索引记录；所有三包均通过，重复十轮通过。26 条 skip 记录中，25 条属于未配置 NATS，1 条是只由子进程执行的崩溃 fixture 入口；实际子进程崩溃恢复测试已运行，不将其列为未运行。

最终执行环境为 Go 1.26.6 / darwin-arm64、Redis 7.4.11。PASS 数量按 Go JSONL 记录计数，包含父测试和子用例，不当作互不重叠的业务场景数：

| 最终验证 | PASS / FAIL / SKIP | 包耗时（mq / event / observability） | 原始结果 |
|---|---|---|---|
| 普通三包回归 | 207 / 0 / 26 | 6.157s / 1.700s / 0.417s | `packages-accepted.jsonl` |
| 三包 race | 207 / 0 / 26 | 7.420s / 2.817s / 1.415s | `race-delivery.jsonl` |
| 分 key 及正式桥接 race ×10 | 200 / 0 / 10 | 16.741s / 1.506s / 未选取 | `repeated-delivery.jsonl` |
| 三包 `go vet` | PASS | — | `vet.log` |

各轮真实 Redis 执行时间不重叠，未共享其他项目 Redis。主 Agent 自审核对了同 key 失败屏障、成功后回扫、固定 worker/队列上限、取消后的 join、原子 owner ACK/认领、counter 生命周期及敏感数据排除。按本仓库 `CLAUDE.md` 禁止子 Agent 的约束，本轮没有启动子 Agent，也不将主 Agent 自审写成独立审计。

## 修改位置与兼容性

| 文件 | 关键修改 |
|---|---|
| `pkg/server/mq/provider_redis_keyed.go` | `runReliableKeyedSubscriber`、`runReliableKeyedOwner`、原子 read/claim 和有界 worker 生命周期 |
| `pkg/server/mq/provider_redis.go` | 续约与抢占分离；发布/handler 时钟；累计指标与订阅 gauge |
| `pkg/server/mq/provider_redis_retry.go` | `processReliableMessage` 的原子 owner ACK/DLQ；`callReliableKeyedHandler` 等待真实退出 |
| `pkg/server/mq/keyed_scheduler.go` | 仅保留 work/result 数据结构，移除批次屏障 helper |
| `pkg/server/mq/mq.go` | `Count` 的行为说明，公开签名不变 |
| `pkg/server/mq/provider_redis_keyed*_test.go` | 真实 Redis 及观测回归，替代旧 helper 孤立测试 |
| `pkg/server/event/mqbridge_redis_window_test.go` | 正式可靠桥接集成 |
| `pkg/server/observability/provider.go`、`collector_test.go` | 固定指标白名单与真实 Gather 验证 |

公开订阅 API、`IService`、MQBridge 与业务 handler 签名、消息编码、Redis stream/group 名及可靠投递身份未改变；`go.mod/go.sum` 未变。不需要迁移业务表、清空 PEL、重置 group 或改 OrderingKey。零值/1 仍用原串行入口，NATS 实现未修改。共享 ACK 路径增加失主原子校验，不能把串行入口仍在当作“允许失主重新抢占并 ACK”的兼容理由。

升级时注意 `Count` 由单次读取数量进一步收敛为预取预算，`MaxAckPending` 小值仍优先生效；满预算产生背压。可靠 handler 必须返回成功，由 provider ACK；原来手动调用 `Message.Ack` 的代码需移除提前确认，非 lifecycle 可靠路径现在明确返回错误而不执行提前 ACK。普通非可靠 `Subscribe` 的 ACK 合约不变。

同组活跃实例的 `Consumer` 仍须具有唯一实例身份；本次未重定义 owner identity 格式。升级建议先停止旧订阅并等待现有 handler 退出，再启动新版本，保留全部消息和 pending。未做新旧版本同组混跑验收，不将回退视为没有语义变化。

## 未验证项

- 未将修复部署到 Bitzoom，未重跑现场 R58/R59 或重新测量那笔 11.33 秒慢单；未认证 2000/s 或其他容量目标。
- 未测试真实生产 Redis 拓扑、Cluster、TLS/ACL、网络分区或主从故障切换；本轮 Redis 暂停测试属于既有 lifecycle deadline 回归，不能代替这些验证。
- 未运行真实 NATS，未改其调度/超时实现；未宣称完成 Core 全仓或全示例回归。
- Go 无法强杀不合作的 handler。无限阻塞会占住有限 worker，并使 `Close` 等待；不会为它无限创建替代 goroutine。owner fencing 保护 Redis ACK，不替代业务数据库提交 fencing 和 Inbox/业务幂等。
- OTel 功能已用本地 SDK recorder 验证；消费方生产 exporter、采样策略与跨机器时钟校准未验收，不能从未采样的 trace 缺失推导未处理。

本次没有修改 Bitzoom 源码、配置、运行容器或数据库/Redis；测试仅使用并回收本轮独占 Redis。工作区既有 scaffold-vue 研究文档和目录保留，不包含在本次修改中。
