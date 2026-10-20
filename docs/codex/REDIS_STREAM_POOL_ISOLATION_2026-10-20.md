# Redis Streams 连接池饥饿修复与验收

## 根因与范围

现场消费方为 Core v1.4.2、go-redis v9.21.0；本轮修改基于已发布的 v1.4.4 / `668e4917afa74aaa6790d455af04f23d5b14c882`。只处理单实例连接资源管理，不改变跨实例 owner、SharedKeys、NATS 或业务接入。没有修改 Bitzoom、操作 Full-E4、清理现场数据或扩大主题容量。

普通订阅的 `XREADGROUP BLOCK 2s`、串行可靠订阅的 `BLOCK 200ms` 原来与 owner 校验/续租、ACK、发布、PEL 和生命周期操作共用一个 client。多个空闲主题长轮询持有连接，控制命令先在 go-redis `ConnPool.Get → waitTurn → FastSemaphore.Acquire` 排队，尚未发到 Redis。当前 keyed 调度的无 BLOCK Lua 也使用该 client，会被其他订阅的阻塞读挤占；不能把它误写成 keyed 自身使用 BLOCK。

读取链为 `ServiceContext.SubscribeEvent → ServiceEventBridge → MQBridge.SubscribeReliableWithOptions → MQManager → RedisStreamProvider.SubscribeReliableWithOptions`；串行读取进入 `runReliableSubscriber`，keyed 进入既有持续调度。业务成功后 `processReliableMessage` 才执行 fenced ACK。本轮只隔离两处阻塞 XREADGROUP 的连接，保留 v1.4.2 的跨窗口调度。

已只读复核现场两份原始 trace，可解析且均有上述等待栈。90.2% / 94.9% 是单协程各约三秒窗口内累计池等待比例，不是单条消息耗时；renew/stillOwner 是其子项，不重复相加。现场没有直接读取初始化 Options/PoolStats，不能认定池上限为10，也不能用进程13条 TCP 连接推断该池大小。该证据支持连接饥饿，不证明 Redis 物理内存耗尽或 Lua 执行数百毫秒。

## 最小实现与连接预算

每个 Provider 只建立两个 go-redis Client，连接按需创建：

| 池 | 默认预算 | 操作 |
| --- | ---: | --- |
| command | 8 | owner 校验/续租/释放、ACK、发布、PEL/reclaim、生命周期、keyed 无 BLOCK Lua |
| blocking | 16 | 普通与串行可靠订阅的阻塞 XREADGROUP |

配置在原 MQ 的 `RedisStream` 下增加 `CommandPoolSize`、`BlockingReadPoolSize`，0 分别采用8/16，负数及超过客户端 int32 范围拒绝。工厂和直接构造路径都校验；auto 模式也不能把错误预算当作 Broker 不可用静默降级。MQ 缺省仍 off，不会因这两个字段启用 Redis。

```json
{
  "Mode": "on",
  "Provider": "redis-stream",
  "Usage": ["event-stream"],
  "RedisStream": {
    "Addr": "127.0.0.1:6379",
    "CommandPoolSize": 8,
    "BlockingReadPoolSize": 16
  }
}
```

上述对象放在应用配置的 MQ 节点。旧 `NewRedisStreamProvider(addr, prefix, db)` 保留；新增 `NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig)`。每池同时设置 `PoolSize=MaxActiveConns=预算`，不依赖 CPU，不为每个订阅建立新池。单 Provider 的受控活跃连接上限为 `C+B`；go-redis 可能在连接重建期间短暂关闭/重连 socket，不能把该预算当 OS 全进程 TCP 精确瞬时上限。多个 Provider、发现、缓存及其他 Redis client 必须另计。

选择 B 时统计潜在同时执行阻塞读取的订阅：普通订阅及串行可靠订阅，含可能激活的 standby。预算应覆盖部署需要的读取并发；B 小于该数会在读池内背压。C 按发布、控制、生命周期和 keyed 调度的实际并发及等待观测选择。默认8/16是有界起点，不是所有项目的最优值或吞吐承诺。`KeyConcurrency` 不等于 Provider 总订阅数或连接预算。

## 生命周期与兼容边界

- 订阅登记与 WaitGroup.Add 同锁，关闭后拒绝新登记；取消删除登记及观测对象，重复同 consumer 登记明确拒绝，避免覆盖取消函数而泄漏。关闭对象不能重新 Connect，应创建新 Provider。
- Close 幂等：取消订阅，关闭读池打断阻塞 I/O，等待 worker/真实 handler，最后关闭命令池，让 owner 释放仍可使用原连接。重复 Close 不返回旧版重复关闭 client 的 ErrClosed。
- go-redis9.21 已发出的网络读取不承诺被 context.Cancel 立即中断：健康情况下等原 BLOCK 返回，网络黑洞受原 socket 超时限制。Close 会关闭读池打断读取。没有延长 PoolTimeout/BLOCK，没有调整 GOMAXPROCS 充当修复。
- Close 仍可能被不合作 handler 延迟；取消/租约过期不证明在途业务调用停止。没有强杀 handler、提前 ACK、吞错误或无限增加 worker。
- 消息身份、payload、group、cursor、PEL、同 key 顺序、最早失败屏障、at-least-once、容量硬限、必需组及安全回收不变；不需要清理或重置历史数据。业务事务和跨副本幂等继续由应用负责。
- 本基线已包含 v1.4.4 的显式 SharedKeys；本轮没有修改或启用它，消费方不能把池隔离升级等同于跨实例模式迁移。

## 观测与时钟

Runtime 快照/Prometheus 白名单按 `command`、`blocking` 两个固定角色区分：

- gauge：`redis_{role}_pool_limit/total/idle/in_use`；`redis_blocking_subscriptions` 是已登记的潜在阻塞订阅数，包含 standby，不是正在等待网络的精确数量。取消删除登记后，原 I/O 可能尚在退出。
- counter：`redis_{role}_pool_wait_total/wait_seconds_total/timeouts_total`，直接来自该 client 的真实 PoolStats。未连接、已关闭或未采集时无该值，不能展示成0。
- go-redis9.21 的 WaitCount/WaitDurationNs 只累计等待后成功取得连接的路径，不包括所有 context 取消等待；Timeouts 是 PoolTimeout 次数，不是所有业务/网络超时。不得用它宣称完整等待总量。
- 保留发布、Broker读取/PEL、key调度、handler开始/结束及ACK的既有 OTel 关联；本轮给 serial 新读取补上读取 span 和 publish-to-read 观测。历史 PEL 首读未知不伪造。消息ID只作 span 关联，不作指标标签；不记录正文、凭据或业务标识高基数指标。

## 修改位置

| 文件 | 函数或责任 |
| --- | --- |
| pkg/server/config/mqconfig.go | RedisStreamMQConfig.Validate、MQConfig.ApplyDefaults/Validate；预算默认及校验 |
| pkg/server/mq/factory.go | buildProvider；配置贯通与配置错误分类 |
| pkg/server/mq/provider_redis_pool.go | NewRedisStreamProviderWithConfig、connectRedisPools、closeRedisPools、registerRedisSubscription、redisPoolMetrics |
| pkg/server/mq/provider_redis.go | Connect/Close委托；Subscribe/runReliableSubscriber阻塞读路由；SubscribeReliable退出清理；RuntimeMetricSnapshot池观测 |
| pkg/server/observability/provider.go | 固定池指标白名单 |
| 各包新增 *_pool*_test.go | 真实资源/测量、配置、断连/PEL、ServiceContext正式链回归 |

未修改 keyed调度、owner Lua、retry、生命周期回收实现或业务payload。

## RED → GREEN 证据

原始文件在本轮 `.temp/redis-pool-20261020/`，没有用重跑覆盖 RED。`baseline.json` 绑定旧生产 commit；`baseline-binary.json` 绑定旧二进制和相同测量主体哈希；`field-trace-verification.json` 绑定现场 trace。测量初版仅 cleanup 不接受旧重复 Close 的 ErrClosed，单独保留为夹具失败，不算生产 RED。

| 验证 | 实际结果 |
| --- | --- |
| 真实 Redis，CPU1/2，普通阻塞读持有全部旧共享池名额，受控并发 renew/stillOwner/publish/fenced ACK | 旧版 RED；隔离后在长轮询释放前完成 GREEN。500ms仅夹具退出护栏，小于原2s阻塞窗口，不是业务SLO |
| 配置贯通、负预算拒绝、真实两角色指标 | RED → GREEN |
| Close打断阻塞读取、取消清登记 | RED → GREEN；审查另补父context取消三种订阅的原始RED与最终GREEN，工作者退出自动清登记 |
| serial/keyed慢handler取消、失败不得ACK、断网/恢复、旧consumer PEL接管后新消息顺序 | GREEN，真实Redis及测试专用TCP断连代理 |
| 三轮各64订阅启停、并发登记/Close | GREEN；工作者归零，两个池最终连接0，采样goroutine由6到5 |
| 真实 ServiceContext→EventBridge→Provider，8空闲串行订阅+B4，active keyed2，同key20条，首条失败重试 | GREEN；正式配置实际C2/B4，最终顺序与ACK正确 |
| 默认8/16，CPU1，12空闲主题+1活跃串行主题，同key64条 | GREEN；注册潜在阻塞订阅13，保持顺序推进 |
| mq/event/router/config/observability五包完整普通及race | 各5包PASS，各664个测试/子测试通过事件、30个skip事件；不是664项独立场景 |
| 五包vet | PASS |
| 示例06三个独立进程、三角色订单可见性race | PASS，37.852秒（含构建）；真实订阅/Outbox与默认池调用方回归，不代表容量压测 |

复跑命令（必须使用独占 Redis；全局命令计数不可与其他包并行）：

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:57873 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 GOWORK=off \
  go test -p 1 ./pkg/server/mq ./pkg/server/event ./pkg/server/router \
    ./pkg/server/config ./pkg/server/observability -count=1 -json -timeout=4m
# 同一命令增加 -race；本轮普通38.955秒、race45.137秒，均退出0。
go vet ./pkg/server/mq ./pkg/server/event ./pkg/server/router \
  ./pkg/server/config ./pkg/server/observability
CORE_TEST_REDIS_ADDR=127.0.0.1:57873 SHOP_REDIS_ADDR=127.0.0.1:57873 GOWORK=off \
  go test -p 1 -race ./examples/integration/06-shop-microservices-three-process \
    -run '^TestThreeProcessUATThreeRolesOrderVisibility$' -count=1 -v -timeout=5m
CORE_TEST_REDIS_ADDR=127.0.0.1:57873 CORE_TEST_POOL_MEASURE=1 GOWORK=off \
  go test -p 1 ./pkg/server/mq -run '^TestRedisPoolFixedWorkloadMeasurement$' -count=1 -v
```

端口57873仅是本轮专属无盘 Redis8.0.3；消费方应启动自己的隔离实例，不连现场复跑。

## 相同资源对照与限制

旧生产源码668e491与新实现，测量主体相同，固定201条（warm1+有效200）、handler每次2ms、3秒观测窗口。总池预算保持10或20，新实现划C2，剩余给B。两组均使用真实Provider/Broker及相同订阅数，不调整主题生命周期策略。

| CPU | KeyConcurrency | 空闲主题 | 总预算 | 3秒处理数 旧→新 | ACK平均ms 旧→新 | renew平均ms 旧→新 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 12 | 10 | 7→14 | 15.635→1.572 | 98.982→1.484 |
| 1 | 2 | 12 | 10 | 19→200 | 94.009→0.124 | 77.646→0.149 |
| 2 | 1 | 22 | 20 | 6→18 | 16.166→1.913 | 141.287→1.937 |
| 2 | 2 | 22 | 20 | 25→200 | 38.482→0.133 | 60.046→0.155 |

旧 shared 池累计等待分别9.145/12.318/8.818/11.760秒；新 command 池0.515/0.535/2.429/2.022秒，blocking池13.911/11.322/13.400/10.605秒。它们是所有协程的累计值，可大于窗口长度；旧 shared 与新 command 口径不同，不能直接计算单消费者等待下降百分比。原始测量还分别保留 BrokerID年龄、读取至handler、handler和ACK时钟，不能把 publish-to-read 全归因于池等待。owner检查统计覆盖该Provider全部主题，发布仅3个样本，不报P99。

这些数据证明控制资源隔离和keyed推进改善。B故意小于潜在串行订阅数，串行读取仍受读池背压；没有据此宣称修复现场全部积压或达到业务吞吐目标。固定200条/3秒不是稳态容量压测；原现场消费方升级后的性能与积压排空均 **NOT_RUN**。

## 未验证项

Bitzoom/Full-E4部署、现场容量或256MiB积压排空、生产Redis持久化/HA/Cluster/ACL/TLS拓扑、长时间稳态与网络黑洞专项均 **NOT_RUN**。NATS/etcd/MySQL依赖专项、跨实例原生长租约故障专项不作为本轮验收；相关未配置用例skip如实保留。当前任务没有改变这些能力，不能用mock或静态检查冒充真实生产验证。

## 依赖依据

采用现有 go-redis9.21.0；[固定版本Options](https://github.com/redis/go-redis/blob/v9.21.0/options.go)、[固定版本池实现](https://github.com/redis/go-redis/blob/v9.21.0/internal/pool/pool.go)与[官方生产连接池指南](https://redis.io/docs/latest/develop/clients/go/produsage/)共同核对。官方现行指南涉及较新版本的pipeline池能力，不能套用到9.21；本轮不升级依赖或引入该能力。
