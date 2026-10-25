# EventBridge 显式仅Broker可靠消费：接入与验收

## 状态与范围

本轮以 v1.5.0 / `d73f9af45d21740ae48514d969cd01b31321ee0a` 为诊断基线。新增选项在本修复分支提供，尚未发布正式版本，不能描述为 v1.5.0 已支持。实现可审提交：`897c66c1ab2b78b2abab156350aa8de271907af7`；本分支另有文档/原件提交。不自动合并、打 tag、部署或升级消费方。

只解除显式订阅的发布前同步依赖。不修改 Redis Provider、消息生命周期、Outbox 的 `runKeyedLanes` 或业务模型；不操作 Bitzoom/Full-E4。记录生成至发布标记约35/41秒的现场差值不是函数计时，本轮没有对该现场计时，不宣称解释或解决全部延迟。SQL名额、业务投影、跨批Outbox等待不在范围内。

## 根因与实际调用链

修改前，同一可靠业务订阅同时注册本地控制handler和外部可靠订阅：

```text
UseOutbox → outboxPublisher.publish
 → ServiceEventBridge.Publish(ControlDelivery, External=true)
 → deliver → Stream.Publish（观察handler）
           → Stream.PublishControl（同步可靠handler）
           → MQBridge.Publish → MQManager.Publish → Broker
 → MarkPublished / MarkPublishedBatch
```

若本服务订阅自己发布的投影，本地handler须完成才会外发。原Outbox各lane还需本批全部完成，本轮保留该调度。外部入站也使用 `MQBridge.SubscribeReliableWithOptions → Stream.PublishControl`，因此单纯“不注册本地handler”会使真正MQ消息也找不到消费者。

修改后仍注册原Stream中的可靠handler，增加仅保存在Core内的交付入口标记：

```text
本地发布 → deliver → Stream.Publish
                  → Stream.PublishControl（只跳过BrokerOnly的handler）
                  → MQBridge.Publish → Broker → Outbox确认

可靠Broker入站 → MQBridge.SubscribeReliableWithOptions
 → Stream.publishBrokerControl（Core内部入口，含实际订阅Subject/Group）
 → 全部匹配的可靠handler（包含BrokerOnly）完成/返回聚合错误
 → 原Provider成功后ACK；错误/取消继续原pending与重投合同
```

不比较Source，不改Envelope、EventID、ShardKey、IdempotencyKey或payload，不让业务字段声明入口。本服务Source从Broker返回也处理。新模式匹配实际订阅Subject/Group，Broadcast组不会误触发逻辑组的新模式handler。旧默认handler仍保留原分发行为。

## 公开选项与非法组合

新增 `event.Subscription.BrokerOnly bool`，零值false保留旧本地控制交付及外部可靠交付；旧订阅不需改动。true只允许外部可靠MQ入站执行该handler，普通Stream.Publish/PublishControl和本地发布不能触发它。其他默认控制/观察handler仍可能同步阻塞发布，不全局颠倒deliver顺序。

true要求：

- `Reliable=true`、非空且无首尾空白的Subject、非nil Handler。
- 有稳定逻辑服务SubscriberID并实际建立可靠外部订阅；MQ off、未装配、无可靠Provider、健康连接失败均明确返回错误。
- 外部适配器显式实现加性接口 `BrokerOnlyReliableExternalSubscriber`，其方法 `EnsureBrokerOnlyReliable(context.Context, *Stream) error` 验证可靠入站能力。正式MQBridge同时检查其Stream就是注册handler的Stream；接错Stream不能假报成功。旧自定义适配器未声明该能力返回 `ErrBrokerOnlyReliableUnsupported`。优先复用正式MQBridge，不自写第二套转发。
- 原KeyConcurrency、SharedKeys、Prefetch、Broadcast约束仍生效：SharedKeys需要可靠Subject且不能Broadcast；Prefetch只能随SharedKeys使用，负值拒绝；同subject同组配置冲突拒绝；Broadcast仍需要稳定InstanceSubscriberID。

同subject/group多handler、新旧模式混用只创建一个实际Broker订阅；引用计数释放最后一项才取消外部订阅。EventType空值匹配该Subject所有类型，指定值只匹配对应类型。所有匹配可靠handler成功后才能向Provider返回成功；一个失败不吞掉，也不跳过其他匹配handler的等待。

## 消费方最小接入

只对不应进入发布同步路径的派生投影订阅增加字段，在原Initialize组合根传播错误：

```go
stop, err := sc.SubscribeEvent(event.Subscription{
    Subject: projectionSubject,
    EventType: projectionEventType, // 空字符串仍表示该Subject全部类型
    Reliable: true,
    BrokerOnly: true, // 唯一新增的业务接线选项
    SharedKeys: true, // 以下沿用应用原来已经声明的合同
    KeyConcurrency: existingConcurrency,
    Prefetch: existingPrefetch,
    Handler: func(ctx context.Context, env *event.Envelope) error {
        return existingInboxAndProjectionTransaction(ctx, env)
    },
})
if err != nil {
    return err // 不吞错，不声称已启动消费
}
// 按原服务生命周期保存/调用stop，不另写worker或本地+MQ双套订阅。
```

`projectionSubject`等占位标识代表应用已有声明；不要因此新增/更换主题、分组、幂等身份或顺序键。SharedKeys原本未启用时保留原值，不为使用BrokerOnly而盲目启用或增加并发。应用仍须先完成原持久化/幂等事务再返回nil；接管、暂停、未知结果确认仍由原应用合同负责，不将接管冒充业务成功。

发布成功只表示Broker按既有可靠合同接受。投影可能仍在执行；失败不代表投递意图可删除。Broker发布后、Outbox确认前崩溃可沿原身份重复，Inbox或等价业务唯一约束负责收敛，仍是at-least-once。该选项不自动创建Inbox，不自动给无有限生命周期的旧MQ消费者增加有限预算，也不改变原有MaxDeliveries/DLQ与安全回收算法。

## 取消、关闭与资源

没有新worker、队列或消息框架。可靠控制注册采用独立对象身份，避免函数代码地址相同导致取消一个闭包误删其它handler；外部引用创建在原锁下串行化，避免并发注册短暂创建多个相同组消费者。

SubscribeEvent的在途可靠handler纳入Bridge原WaitGroup；准入与Close协调，不在应用执行期间持锁。单独取消会撤销后续分发并取消该订阅的处理上下文；已经执行的handler仍须实际退出。Close等待真实执行结束，超时返回错误；取消后迟到成功返回错误，不能以取消让Provider提前ACK。不强杀任意Go代码或远端事务；租约到期、handler超时、Close超时均不证明原业务已停止。消息在原PEL/retry/归属保护下恢复，跨副本业务幂等仍必要。

## 修改文件与函数

| 文件 | 入口 |
| --- | --- |
| `pkg/server/event/subscription.go` | BrokerOnly、validateSubscription、subscribeExternalRef |
| `pkg/server/event/servicebridge.go` | 新可选入站能力、SubscribeEvent过滤/上下文/工作准入、Close等待 |
| `pkg/server/event/mqbridge.go` | EnsureBrokerOnlyReliable、SubscribeReliableWithOptions正式入站 |
| `pkg/server/event/stream.go` | 内部controlSubscription、subscribeControl、publishBrokerControl；公开SubscribeControl/PublishControl签名不变 |
| `pkg/server/event/broker_only*_test.go` | 默认兼容、隔离/过滤/失败/取消，真实Redis/ServiceContext/独立进程/原库恢复 |
| `examples/10-reliable-multi-instance/service.go`及README | 原可靠订阅显式启用；没有新增应用消费循环 |

ServiceContext已按值透传Subscription，无须改router接线。未变更go.mod/go.sum、Provider、Outbox算法或业务数据库模型。

## RED/GREEN 与原始证据

原始命令和输出已随本分支封存在 `evidence/event-broker-only-20261026/`，包括原失败，不以重跑覆盖。初始夹具通过JSON设置BrokerOnly，让旧基线忽略未知字段并产生真实行为RED，而不是用“字段不存在”的编译失败代替。

| 记录 | 实际结果 |
| --- | --- |
| red-contract | 新选项仍执行本地handler、非法/无适配器注册被接受、Close未等待实际handler；退出1 |
| red-redis | 独占真实Redis、正式ServiceContext与Outbox；投影屏障未释放时不能确认/继续外发，退出1 |
| green-initial | 原失败断言保持，真实Broker在handler完成前已收到两条，Outbox确认；当前消费仍在PEL，退出0 |
| broadcast-red | 同主题的两个组错误触发另一组的新模式handler，退出1；broker-mode-green中相同断言通过 |
| detached-ingress-red / detached-ingress-green | MQBridge指向另一Stream仍成功注册的RED；校验同Stream后GREEN，退出1→0 |
| extended-contract-race | 32并发注册只创建一次外部订阅、取消只移除自身；全部匹配handler等待；真实连接中断不确认Outbox并恢复，退出0 |
| process-first / broker-mode-green | 真实两个消费者PID分担，同key失败屏障、正常退出，以及发布后确认前硬杀/原Badger重开＋SQLite事务Inbox幂等，普通/race均退出0 |
| final-native-race | 最终生产源码下实际等待原生租约，旧PID 24745硬杀，PID 24746约119.967秒接管；A1再A2，原消息保留，退出0，整轮130.340秒 |

原生恢复测试没有缩短TTL、删除保护标记、清Stream/pending或重建消费组。其独立进程均是Core测试程序；不是同进程两个Provider对象。持久Inbox使用框架 `message.Run/ConsumeInboxWith` 的真实SQLite事务，发布意图使用独立SyncWrites Badger；不是内存去重计数。两个接管库/SQLite的测试结论只覆盖进程硬杀与重开，不覆盖物理断电或任意远端MySQL。

## 相关回归命令与结果

本机专属Redis为 `127.0.0.1:53737`，Redis 8.0.3，随机隔离前缀。没有连接任何业务环境。以下shell均以RTK运行；耗时为该轮墙钟，不是延迟SLA。完整命令/退出码/日志保存在证据目录。

```bash
# 普通：122.350秒，退出0
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:53737 go test -json -p 1 \
  ./pkg/server/event ./pkg/server/router ./pkg/server/mq \
  ./examples/10-reliable-multi-instance/... \
  ./examples/integration/10-reliable-multi-instance -count=1 -timeout=300s

# 完整race：139.072秒，退出0
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:53737 go test -race -json -p 1 \
  ./pkg/server/event ./pkg/server/router ./pkg/server/mq \
  ./examples/10-reliable-multi-instance/... \
  ./examples/integration/10-reliable-multi-instance -count=1 -timeout=300s

# 原生租约race：130.340秒，退出0；最终集中验证另见下表
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:53737 CORE_TEST_SHARED_FULL_LEASE=1 \
  go test -race ./pkg/server/event -run '^TestBrokerOnlyHardCrashNativeLeaseRecovery$' \
  -v -count=1 -timeout=240s

# 真实取消/Close后的PEL：9.166秒，退出0
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:53737 go test -race ./pkg/server/event \
  -run '^TestBrokerOnlyRedisCancellationDoesNotAckInFlight$' -v -count=1 -timeout=30s

# 发布契约：54.523秒，退出0；含公开API、配置与安全契约，不发布版本
rtk proxy ./scripts/test.sh release-contract
# 日志规范：0.195秒，退出0
rtk proxy ./scripts/check-logging.sh
```

完整普通及race各有461条测试/子测试PASS记录、35条测试SKIP记录，不把无测试文件的包计为测试通过。35项中：26项NATS真Broker未配置，4项为父测试实际执行的子进程默认入口，1项新BrokerOnly原生租约另行真实验证通过，另外4项为旧SharedKeys原生租约入口、两个需CLIENT PAUSE开关的生命周期时限用例和连接池固定负载测量。本轮未运行这4项，不能以其它测试替代为PASS。逐项原因见 `regression-inventory.json / race-inventory.json`。

示例10的HTTP回归实际启动一个发布服务与两个独立消费服务；普通消费PID 36077/36481，race消费PID 43482/44045。它们证明Core演示接线与用户隔离，不证明Bitzoom业务功能。

## 自审、升级边界与NOT_RUN

主Agent按正确性、可读性、架构、安全和资源五轴自审；仓库禁止子Agent，本轮没有独立外部审计。生产净改动只在event包四文件；Source/业务消息身份不变，不新增依赖和应用worker。订阅创建使用原引用锁，执行handler期间不持该锁；实际工作仍受原Provider并发与预取预算限制。单key填满整个Prefetch时，无关key仍可能遭遇容量背压，不承诺无限资源下的绝对无等待。

这是待发布的加性API，不移动已发布v1.5.0标签。消费方需在审核/发布后锁定包含本提交的版本并按新skill接入；没有替消费方修改模块缓存、replace或业务代码。新字段只改变显式handler的入口选择，不改Broker持久数据格式、组、游标和历史pending，不需要清数据。滚动混用时旧节点仍有原同步本地交付，不能宣称整个服务已全部解耦；原EventID/Inbox幂等必须继续覆盖重复。

NOT_RUN：NATS新模式及其真实Broker回归；上述4项可选专项；真实远端MySQL/物理断电；Bitzoom接入、部署、业务UAT、容量和Full-E4。没有运行吞吐优化或端到端现场延迟归因。Core测试通过不代表消费方已接入或现场35/41秒差值全部消失。

## 冻结源码集中复验

在最后补强PEL查询错误断言后，生产文件未再变化，重新集中运行全部新增/默认兼容用例，**128.465秒、退出0**：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:53737 CORE_TEST_SHARED_FULL_LEASE=1 \
  go test -race ./pkg/server/event -run '^Test(BrokerOnly|DefaultControlHandler)' \
  -v -count=1 -timeout=240s
```

该轮包含真实取消未ACK、全部混合/组隔离/接线校验、Outbox原卷恢复和原生租约恢复，唯一默认SKIP是父测试实际启动的子进程夹具入口。最终原生恢复：硬杀PID 49148，PID 49166约119.9998秒接管，无保护标记/游标变化。新增取消测试及最终断言补强在完整461条普通/race汇总之后另行执行，不把不同轮次PASS数简单相加。

[命令与原输出指纹](evidence/event-broker-only-20261026/commands.json)、[最终源码绑定](evidence/event-broker-only-20261026/source-binding.json)、[文件manifest](evidence/event-broker-only-20261026/manifest.json)及逐项SKIP清单随分支提交。`*.log.gz`无损保留原字节，解压后须匹配commands中的raw_sha256；不格式化或覆盖原RED。
