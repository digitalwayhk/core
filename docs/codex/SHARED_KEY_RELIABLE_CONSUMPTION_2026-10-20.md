# Redis 可靠消费跨实例分担：根因、实现与验收

基线为 v1.4.3 / `d26004683e5f3366bae54ccf627d52828dd1b4ce`，保留 v1.4.2 已修复的跨读取窗口等待。此次不操作 Bitzoom、不部署、不发压；正式版本按用户确认使用 v1.4.4。

## 根因与实际调用链

`ServiceContext.SubscribeEvent → ServiceEventBridge.SubscribeEvent → subscribeExternalControlWithOptions → MQBridge.SubscribeReliableWithOptions → MQManager.SubscribeReliable → RedisStreamProvider.runReliableKeyedSubscriber → runReliableKeyedOwner → processReliableMessage → MQBridge callback → Stream.PublishControl → 已注册业务 handler`。

原 `ownerLockKey` 按 subject+group 建立一个整主题 owner。其他副本即使健康且可承接同步请求，也只会等待该 owner。`KeyConcurrency` 控制 owner 内不同 key 的固定 worker 数；它从未表示多个进程分担。`VerifyKeyedReliableConcurrency` 不能证明跨进程分担。直接删除 owner 锁会破坏 pending 最早消息选择和 ACK fencing，不能作为修复。

## 通用层与 Provider 的职责

| 层 | 责任 |
| --- | --- |
| EventBridge | `Subscription.SharedKeys` 显式请求跨实例分担；`Prefetch` 透传组级预算；拒绝非可靠、无 Subject、Broadcast、无外部适配器以及同 Subject 选项冲突 |
| MQManager | 校验 `SharedKeyReliableMQProvider`、合法能力声明及 keyed/ordered 一致性；建立共享要求后拒绝在线 `SetCurrent/BeginSwitch`；已在切换时也拒绝建立共享要求 |
| Redis Provider | 同一现有组的有限读取/PEL 扫描、按 key 执行租约、续约、接管、fenced ACK/DLQ；Lua/Stream key 不成为业务合同 |
| NATS Provider | 仅声明其实际普通可靠与生命周期能力；不声明 ordered/keyed/shared，显式请求 SharedKeys 或多 key 并发会拒绝 |
| 业务 | 稳定 key、幂等/版本约束、事务语义及调用超时；保留 at-least-once，不宣称 exactly-once |

启动示例：

```go
stop, err := sc.SubscribeEvent(event.Subscription{
    Subject: "jobs.v1", Reliable: true,
    SharedKeys: true, KeyConcurrency: 4, Prefetch: 32,
    Handler: handle,
})
```

`KeyConcurrency=N` 是**每个订阅实例**的 worker 上限；`Prefetch` 对应 MQ `Count`。零值取 `4*N`，小于 N 按 N 归一化，若生命周期 `MaxAckPending` 更小，则再取更小值。该有效 W 是整个 subject/group 的新预取 PEL 预算，所有副本必须一致，并冻结到 Broker 模式标记。若要用不同 N 扩容，必须显式设置一致且足够大的 Prefetch。它不是配置文件的新默认值。

## 最小 Redis 实现及资源边界

保留原 Stream、group、消息、游标、payload 和业务 EventID。原 owner 槽在显式升级时变为持久的 `shared-key-v1:<W>` 模式标记；旧整体 owner 不能再获得它。旧模式准入与标记激活原子互斥，避免先检查后启动的竞态。已有整体 owner 时 Shared 启动明确冲突。

每个实例复用持续调度循环和固定 N 个 worker。`XPENDING` 排他分页 + `XRANGE` 只观察 PEL；真正获得执行槽时，Lua 以 key 的 SHA256 建立独立执行租约，并原子 `XCLAIM`。待调度消息不提前占租约，因此其他实例有空闲 worker 时能处理独立 key。一个 key 只准入本地最早未完成消息；完成或复核已 ACK 后回到 PEL 起点，防止恢复时新消息越过旧消息。

共享读取 Lua 将 PEL 数检查与 `XREADGROUP` 放在同一个原子命令，多个实例不能分别突破 W。每轮 PEL 和新消息各最多一页，有限轮询，无整批完成等待；失败只保留本 key 的 retryAt，Broker 租约也保存短退避，其他实例不能绕过 Backoff。旧 PEL 已超过 W 时分页恢复，不继续扩大。

每实例 tasks、readTimes 各不超过 W；worker 为 N，续约 goroutine 至多 N，结果通道 N。活跃执行租约到期清理，模式标记每组一条。总实例内存随部署副本数线性增加，业务 handler 自己分配的内存不由 MQ 限制。Redis 历史 consumer 元数据沿用原机制，不承诺无限次崩溃重启后其基数自动归零。

公平性受资源预算约束：已准入 key 按准入顺序争 worker，PEL 恢复每轮与新读取交替；若 W 条全是同一阻塞 key，W 之后的新 key 仍需等预算释放。超限旧 PEL 必须先恢复；不能声称能越过任意长热点找到后方 key。

## 顺序、失败、取消与 fencing

顺序作用域是**同 subject + 同 group + 同 OrderingKey**，顺序为 Broker 接收顺序，不是业务序号、HTTP 接单时间或不同 subject 的全局用户顺序。没有固定的“用户归原 HTTP 实例”映射。

每次真正执行生成独立随机 token。原 ACK、retry ACK、终态 DLQ+ACK Lua 全部校验当前 key 租约 token；旧 owner 迟到完成不能 ACK，不能移走新 owner 的租约。处理错误、超时或取消不靠提前 ACK 消除 pending；显式有限重试的终态 DLQ 仍遵循原生命周期合同。ACK 已执行但客户端未知时，重试前复核 PEL，已 ACK 的工作不再次调度。

续约失败取消内部执行上下文，原 worker 仍等待实际 handler 结束，不不断遗弃 goroutine。默认租约至少两分钟，硬崩溃接管存在该等待。**租约过期不证明旧 handler、数据库调用或远端 HTTP 已停止**；新 owner 可能对同一未完成事件合法重复执行。Broker fencing 只约束 ACK/DLQ，不能跨业务数据库强制事务 fencing。业务必须保留跨副本幂等与约束。现有 Event callback 的 context 传递没有在本轮改签名；业务不能假定内部租约取消一定传到任意旧 handler，必须自己设置数据库/远端调用超时。

## 存量升级与回退

1. 先部署兼容新二进制但保持 `SharedKeys=false`，旧默认语义继续存在。
2. 对目标 subject/group 停止旧消费，确认旧进程/实际 handler 已结束；等待原租约释放或到期。暂停消费不要求暂停发布，但积压仍受原容量政策限制。
3. 所有新副本一致启用 `SharedKeys=true`、相同有效 W。首次激活不删除 Stream/PEL/组，不改游标，不吞掉已有积压；之后先恢复已有 PEL。
4. 模式和 W 持久冻结：再启动旧默认、旧版本或不同 W 的进程会拒绝/无法取得 owner。只换回旧 tag 不能自动消费共享组。在线改 W、跨 Broker 切换、Shared→legacy 原地回退均未提供安全工具，必须另行制定停服维护方案；不得删标记/组/消息或重置游标来绕过。

没有要求所有旧消费方自动 opt-in。旧行为保持；新字段加性、零值兼容。自定义 Provider 必须显式声明并通过行为测试，不能只接受字段后忽略。Redis Cluster 本轮 NOT_RUN，未新增强制 Cluster 要求。

## 原始 RED 和测试证据

证据原件在本地 `.temp/shared-keys-20261020/`，文件名保留失败与成功，不覆盖原始 RED。基线源码/测试绑定见 `red-binding.json`；最终源码哈希及命令/状态见验收 manifest。

| 证据 | 实际结论 |
| --- | --- |
| `two-process-RED.log` | 基线真实 Broker、两个子进程：A1 阻塞后，B 进程 ready 但 B1 三秒内不能完成，FAIL |
| `contract-RED.log` | keyed-only Provider、缺外桥、非可靠/Broadcast 共享要求未拒绝，FAIL |
| `mode-race-RED.log` | 初版旧模式准入后 shared 可激活，FAIL；原子预占后 GREEN |
| `shared-metrics-RED.log` | 新 lease/ACK 指标缺失，FAIL→GREEN |
| `prefetch-switch-RED.log` | Prefetch 丢失及共享要求后能切换 Provider，FAIL→GREEN |
| `initial-GREEN.log` | 真实双进程分别处理，慢 A 期间 B1/B2 完成 |
| `full-lease-crash-race.log` | 原生两分钟租约，硬杀 PID 40291 后 PID 40306 接管；恢复约 2m0.036s，A1 后才 A2，其他 key 继续 |
| `final-*.log/.json` | 最终代码普通/race、原生租约恢复、三服务 HTTP 示例与 vet 的命令及退出码 |

复现命令（必须是独占 Redis，不能与全局命令计数测试并行）：

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:<专属端口> CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 GOWORK=off go test -p 1 ./pkg/server/mq ./pkg/server/event ./pkg/server/observability -count=1 -json -timeout=3m
CORE_TEST_REDIS_ADDR=127.0.0.1:<专属端口> CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 GOWORK=off go test -p 1 -race ./pkg/server/mq ./pkg/server/event ./pkg/server/observability -count=1 -json -timeout=3m
CORE_TEST_SHARED_FULL_LEASE=1 CORE_TEST_REDIS_ADDR=127.0.0.1:<专属端口> GOWORK=off go test -p 1 -race ./pkg/server/event -run '^TestSharedKeyReliableHardCrashAndFullLeaseRecovery$' -count=1 -v -timeout=4m
CORE_TEST_REDIS_ADDR=127.0.0.1:<专属端口> GOWORK=off go test -p 1 -race ./examples/10-reliable-multi-instance/... ./examples/integration/10-reliable-multi-instance -count=1 -v -timeout=4m
```

真实故障测试分别覆盖：多进程成功/失败屏障和正常退出/加入、硬杀/重启、72 条超窗旧 PEL 加新消息恢复、真实 TCP 代理断开/恢复、旧 owner 迟到成功/失败不得 ACK 或 DLQ、超时未退出占原 worker、ACK 执行后返回未知不得异常重放、组预算与并发上限。持续输入测试对两个 Provider 的实际 handler 并发、排队与共享 PEL 采样，200 条消息三轮 race 通过；它的首轮误用指标名称导致夹具失败（bounded-input-race.log），修正后结果单独保存在 bounded-input-race-GREEN.log，不算框架 RED。网络断开用例仅对失联 key 加速 TTL 为 10ms，明确是故障注入；原生两分钟证明由单独硬崩溃用例提供，不能混算。ACK 未知注入是实际 Redis 命令执行后客户端 hook 返回错误，不等于穷举所有 socket/Redis 故障。

`VerifySharedKeyReliableDistribution` 是通用双连接 helper，不能代替上面真实独立 PID 证据。第10示例第一次因夹具 DataCenterID 导致端口重叠，日志 `example10-first.log` 保留为启动 FAIL，不算调度 RED；修夹具后 `example10-second.log` 三服务通过。

## 可观测性与未验证项

复用发布 span、`redis.reliable.read` 的 new/reclaim 来源及 Broker age、delivery 的 read/claim/admit/wait/start/end、独立 `redis.reliable.handler` 与新增 `redis.reliable.ack`。Redis Stream ID 可关联同条消息；旧 pending 的首次读取时间未知时明确 `first_read_known=false`，不能伪造全链路时延。跨机器时钟未校准时 Broker age 只是估计。

新增 `shared_key_acquired_total/contended_total/owner_lost_total`、`reliable_ack_total/fail_total/seconds_total`；`shared_key_leases` 是本地已拿执行凭据且尚未退出的工作数，连接未知/过期后不能当成 Broker 实时有效租约数。既有 reliable_handler_calls_total/handler_inflight/handler_peak、queued_work、prefetch_capacity、prefetched_messages、pending_keys 等继续提供。组件指标保持固定名称，按 Prometheus scrape instance 区分进程并与 process_info 身份关联；不加 UserID/OrderID/TraceID 标签，不记录 payload/token/凭据。

本轮主 Agent 按并发、可靠性、公开合同和迁移边界自审，仓库禁止子 Agent，未宣称独立第三方审计。生产 Redis 主从故障/数据丢失、多主机网络分区、Redis Cluster、长时间容量吞吐、NATS 真 Broker 重测、Bitzoom 消费方迁移和生产部署均为 **NOT_RUN**。不能把两个连接的 conformance 当多进程，通过 Core 回归也不等于消费方已验收。

官方机制参考：[XPENDING 排他分页](https://redis.io/docs/latest/commands/xpending/)、[XCLAIM](https://redis.io/docs/latest/commands/xclaim/)、[Lua 原子执行](https://redis.io/docs/latest/develop/programmability/eval-intro/)。
