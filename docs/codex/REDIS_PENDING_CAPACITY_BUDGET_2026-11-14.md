# Redis已知PEL正文与容量剩余预算（2026-11-14）

## 基线、调查与根因

基线正式 `v1.12.0` / `2c592a362a15885c26fc41e9199f33943905f5e1`，官方zip `ebb30fa5c2b9aaa62f66e83063bdd81af23fb0d0e48b79f726163e1f3596a3c0`。从干净既有工作树创建 `fix/redis-pending-capacity-budget`，主Agent本人开发/测试/审查，没有新agent/Task/终端；其他工作树和WIP保持。

只读取用户指定的既有调查文档/聚合JSON：R442所选Trade/hot-c指标stale与publish_rejected均+132、hard+0、refresh三项+0，输入55433bytes，SHA256 `49dcd6480060a5decf7f5822630c6b19e91cac8520fa087b06bcdbe5e50b7c88`。没有读取凭据、私有业务库或连接现场，未导出聚合原件进公开证据。消费方称当时Trade1.12.0、Users1.11.1、其他多角色1.11.0，不能用根go.mod当全部实例版本。不同窗口/事件重叠不相加，CPU与命令嵌套不相加。

确认两个独立SDK缺口，不把全部现场stale或异步P99归于它们：

1. `runReliableKeyedOwner → claimReliableKeyed → redisSharedPendingScript`。旧脚本XPENDING每页后对全部行XRANGE正文；本地已有相同key的task时，Go才丢弃这些重复结果。慢/失败/其他实例持租约时，头尾正文可在每轮被重新读取与解码。任务已有原件，不必为等待同key前序反复取正文。
2. `lifecycleController.runOnce`的70%观测子预算同时含acquire和standby轻量读。acquire/standby可恢复超时直接返回，只有owner完整Inspect分支会复用原runCtx余量补一次容量。合法配置Interval2s/TimeBudget500ms/新鲜度4s不代表每轮必成功；但当350ms观测失败而原轮仍有150ms、可信容量可取时，前两条路径遗漏了同样可用的有限补读。

配置本身合法，无证据判为漏注册或必须调大额度。350ms受控耗尽不是证明Redis服务端单命令执行350ms；现场客户端池、服务端、Go调度/GC及原轮次耗尽仍需消费方逐段核对。

## 最小实现与资源边界

生产仅四文件：

- `provider_redis_keyed.go`：原task附带已见ID集合，所有task合计最多有效window个，仅ID，不留尾项payload。私有`claimReliableKeyedSkipping`承接原入口；当前task存在时记录已取回的同key头尾ID，向Lua传有限提示；完成或原头项已无PEL时释放、原cursor回绕。worker/队列/并行数不变，没有新增业务队列。
- `provider_redis_shared.go`：仍分页原XPENDING，next根据原页末而不是返回正文数；仅跳过已知提示的XRANGE，未见正文仍取，缺正文仍明确错误。原mode检查、fenced XCLAIM、lease/retry/ACK都未改变。
- `lifecycle_controller.go`：acquire、standby与owner可恢复超时共用有限补读资格；原完整/观测错误仍返回和计数，补读成功不触发回收。standby正常返回提交前也核对ctx/关闭，拒绝取消后的迟到成功。
- `lifecycle_capacity_refresh.go`：复用原内置轻量读取和既有证据校验，沿同一个runCtx余量，不另建500ms、不创建Background预算。语义/fence/取消优先，无剩余预算不补读；只更新独立capacity，不改完整组/lag/pending/安全前沿。保留真实hard背压、stale和缺证据拒绝。

ID提示不是持久队列/副本，不增加PEL预取；重启无提示、从原消息恢复。有效W满时仍背压，慢key占满W仍可能阻止新读取，本修复不提供无限预取或全局公平保证。已知尾项的缺正文检查延后到前序退出、重新取原件；该时点错误仍阻断同key后项，不能承诺前序挂起期间即时发现人为正文损坏。正在执行handler的租约到期仍不能证明任意业务代码已停止，原SQL fence/幂等不可删。

原`reclaim_read`继续统计实际返回正文，少读不代表业务完成。没有新指标标签/日志payload、token、DSN或动态业务ID。policy fingerprint、scope/sequence、Stream、group、PEL、ID/正文、cursor、SQL fence及磁盘状态均未更改。

## RED→GREEN与真实验证

独占本机Redis `127.0.0.1:49988`，非消费方实例；不使用Fake代替Broker结论。

- 原始 `red-real-redis` exit1：acquire约351.9ms、capacity_calls=0；standby约350.3ms、calls=1，实际低容量可读仍stale拒绝。两个handler持续阻塞、6条PEL的10轮读取中，多48次重复正文。不是编译或环境失败。
- `green-real-redis` exit0，同断言acquire/standby约352ms，原deadline仍返回、原完整snapshot不变、实际Publish成功；重复正文0，PEL保持6直到handler完成。
- 新容量边界覆盖acquire/standby的policy/owner错误与deadline同现、取消、原预算耗尽、迟到轻量成功。实际fallback deadline与inspect deadline差不超过原30%余量，不只靠毫秒阈值；取消/错误/缺预算不产生可发布证据。
- PEL页全被提示时返回空正文但原cursor推进；未提示原件缺失仍错误。专用ScriptFlush用例验证XStreamSlice/XAutoClaim自定义响应，lease与ACK的go-redis原生NOSCRIPT回退。已知尾项故障在前序完成后真实报错，后两条PEL不ACK、不执行后项。
- 相关四包普通475顶层PASS/30 SKIP/0 FAIL；显式原生租约race477 PASS/28 SKIP/0 FAIL。最后增加的已知尾项损坏用例另随整个专项普通/race通过；生产逻辑未再修改，未把这次补充伪装为此前整包已执行的项。默认微基准/进程子入口和未配置外部能力SKIP原原因保留。
- 真双进程同group、失败屏障、旧pending与新消息、成员退出、网络失联/迟到ACK/重入在event实测通过。原生共享硬杀PID51298→51299接管，未加速120.00016325s；Broker-only原生硬杀48838→48846约119.9425s；Owned15s硬杀50957/50958→51217约15.87s。不是同进程两个对象冒充。
- 示例10真实发布进程+两个消费进程HTTP普通及race通过；消费PID56312/56043、56980/57178。仅证明该合成HTTP/用户隔离闭环，不是消费方吞吐SLO。
- release-contract实际包括CI/API/public-api/config/security；日志及内嵌dist门禁通过。clean-tree --release和正式引用/代理可取性由随后正式回执登记，未执行前不编造已发布。

命令原件包含完整argv/退出码，关键命令：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:49988 go test ./pkg/server/mq -run '^(TestRedisCapacityRemainingBudget|TestSharedPendingHeldBodiesAreNotRefetched)$' -v -count=1 -timeout=60s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:49988 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 go test -p 1 ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -json -count=1 -timeout=600s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:49988 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 CORE_TEST_SHARED_FULL_LEASE=1 go test -race -p 1 ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -json -count=1 -timeout=900s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:49988 go test -race ./pkg/server/mq -run '^Test(RedisCapacityRemaining|SharedPending)' -v -count=1 -timeout=90s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:49988 go test -race ./examples/integration/10-reliable-multi-instance -json -count=1 -timeout=240s
rtk proxy bash scripts/test.sh release-contract
rtk proxy bash scripts/check-logging.sh
rtk proxy bash scripts/test.sh web-dist-sync
```

## 固定PEL读轮对照与EVALSHA取舍

GOMAXPROCS=1、本轮独占Redis、32原消息×4KiB、8个ordering key、200读轮/方案、三轮，原PEL=32且始终未ACK。旧脚本字节的SHA1核对为`b25e2afd1e7b36bba1016faa583069bbf4c49a75`。old_eval用该原脚本；skip_eval用候选已知ID提示；skip_evalsha只在测试中评估相同候选脚本。新旧对照是固定读轮协议微基准，不是应用handler/HTTP/经济事务或稳态容量对照。

| 轮/方案 | 返回正文行 | XRANGE | Redis入/出bytes | Redis进程CPU秒 | 调用P50/P95/P99 ms | 200轮墙钟ms |
| --- | --- | --- | --- | --- | --- | --- |
| 1 / `old_eval` | 6400 | 6400 | 152681 / 26708136 | 0.197883 | 1.155/1.820/2.259 | 244.429 |
| 1 / `skip_eval` | 0 | 0 | 323281 / 13550 | 0.040218 | 0.251/0.459/0.739 | 57.702 |
| 1 / `skip_evalsha` | 0 | 0 | 187881 / 13550 | 0.037054 | 0.229/0.353/0.396 | 47.870 |
| 2 / `old_eval` | 6400 | 6400 | 152681 / 26708150 | 0.169783 | 0.916/1.205/1.558 | 190.751 |
| 2 / `skip_eval` | 0 | 0 | 323281 / 13550 | 0.039695 | 0.235/0.367/0.482 | 49.642 |
| 2 / `skip_evalsha` | 0 | 0 | 187881 / 13550 | 0.034857 | 0.197/0.294/0.357 | 42.295 |
| 3 / `old_eval` | 6400 | 6400 | 152681 / 26708152 | 0.151993 | 0.816/1.067/2.487 | 176.689 |
| 3 / `skip_eval` | 0 | 0 | 323281 / 13553 | 0.037711 | 0.225/0.379/0.439 | 47.935 |
| 3 / `skip_evalsha` | 0 | 0 | 187881 / 13554 | 0.041286 | 0.262/0.455/0.618 | 57.137 |

进程CPU由独立INFO cpu增量获得，不把Lua内嵌GET/XPENDING/XRANGE与EVAL命令累计时间再相加；主线程专属CPU字段在本机Redis未提供，保持null/NOT_RUN，不用进程CPU冒充。INFO字节观测含同口径INFO固定开销，非纯payload。所有方案PEL=32、消息身份不变、业务handler完成数=0；不能说已排空或业务吞吐提升。

skip_eval每200轮避免6400次XRANGE/正文，Redis出站约26.7MB→13.6KB，进程CPU约0.152–0.198s→0.0377–0.0402s。输入因有限ID列表约153KB→323KB，明确不是所有资源都下降。全量旧轮与skip轮的P99见全表，不只报告平均；业务端到端P50/P95/P99、微小payload/大W长稳态均NOT_RUN。

EVALSHA输入再降至约188KB，但两组实测均有P99/CPU回退轮，不能作为稳定长尾收益。本轮生产保留原EVAL及自定义解析，不引入手工NOSCRIPT重试新分支；成熟客户端的lease/ACK既有回退实际经ScriptFlush检验。原始初轮和末轮测量均保留，不挑好轮覆盖差轮。

## 角色升级、兼容与恢复

建议正式补丁 `v1.12.1`，无开关/调参/迁移。公共controller对实现内置worker合同的Provider共用补读资格；NATSProvider源码未改，但真实NATSBroker本轮NOT_RUN，不能把Redis验证写成NATSPASS。发布完成以Release及回执为准：

```bash
go get github.com/digitalwayhk/core@v1.12.1
```

| 实际角色/入口 | 本轮效果 | 必须升级的位置 |
| --- | --- | --- |
| 经MQManager/EventBridge/Outbox发布，声明hard容量的producer | acquire/standby/owner可恢复超时的原轮次余量容量补读 | 发生capacity unknown的那个发布进程；只升consumer不能替它刷新本地controller |
| `SubscribeEvent`的普通Reliable+SharedKeys consumer | 本地key task存续时省已见PEL正文 | 所有希望减少重复成本的实际消费副本；只升producer不改变旧consumer的读取成本 |
| `SubscribeOwnedEvents`/`SubscribeOwnedBatch`专用reader | 原消息/预算/归属安全兼容；目前无普通task提示 | 不宣称该reader减少XRANGE；同进程若也是producer仍受容量修复益处 |
| legacy/non-shared可靠订阅或NATS | 未启用该ID提示 | 沿原能力合同，不冒称得到shared或有序能力 |

从1.11.0/1.11.1还会包含既有1.11.2 Outbox重试公平性、1.11.3 owner容量补读及1.12.0可选本地分页；原业务接线无需为本修复重写，但必须逐实际二进制核对，不能拿根go.mod代替运行身份。应用此前私有SyncWrites诊断既不由Core改动，也不是本次门禁通过的可靠性证据。

按应用既有托管生命周期正常停旧进程、确认实际handler退出，重开同一原volume/身份/逻辑group；硬退出沿原租约等待与SQL fence接管，不能因超时就认定旧业务停止。不要删Stream/group/PEL、重置cursor、消息ID、scope sequence、SQL fence，或覆写fingerprint。新提示仅进程内、重启自动丢弃并重新取原件，策略/卷格式不新增；旧版混跑合同不变但旧实例成本/缺口仍在。既有历史格式/回退禁令继续有效，不是许可任意旧二进制开卷。

无需改变Interval2s/TimeBudget500ms/Hard容量/N/W；冻结值仍参与fingerprint，当前没有建议消费方在线改某台预算或直接补Redis元数据。若原总预算全部耗尽、连接/调度/服务端仍不可用，继续fail closed；本补丁不承诺所有stale消失，也不将SLOWLOG<10ms当所有客户端等待均短。更新权威skill后，复制/旧模块链接消费方按README重新同步，go get不安装skill。

## 本人审查与剩余边界

主Agent按正确性、结构、安全与性能审查：已知提示只源于真实已取原件，保留task前序；ID总量≤W，释放点与原task相同，未借命令返回空正文跳过PEL页。原fenced执行/ACK及实际回调退出路径没改。容量fallback仅可恢复错误且原ctx尚活，所有最终证据经原类型/策略/entry校验，保持错误计数与原完整前沿。没有新依赖/API/队列/并发预算，也没把生命周期策略调大。

NOT_RUN：Bitzoom/Full-E4接入部署、实际132次拒绝的逐调用根因与残余数、现场异步P99/经济事实/UAT/容量；Owned专用reader性能优化；真实NATS/MySQL/WS全套业务集成（本次未改相应实现）、主线程专属CPU、生产CPU/I/O/GC/连接池逐段归因、小消息/大W/长稳态。真实Redis脚本/进程结果不等于消费方或其他Broker结论。没有操作现场、改业务payload、恢复/清理消息，或修改任何消费方配置。

## 原始证据

目录 `/private/tmp/core-redis-pending-capacity-20261114/`，原RED与两组测量均保留；公开附件不含消费方输入或凭据。表中未列的后续正式发布命令记录在最终回执。

| 日志 | exit | 墙钟秒 | bytes | SHA256 |
| --- | --- | --- | --- | --- |
| `example10-race.log` | 0 | 60.321 | 2057 | `98733b577cee356f249d8cdac034b5152fcd65cb46dcc96bbffbec817b3f2808` |
| `example10.log` | 0 | 71.002 | 2057 | `da05068c4f4f625f74564ce94d40892a457f3c38a452643cd7cc0d2ce161973c` |
| `final-targeted-race.log` | 0 | 8.627 | 6227 | `603f444c49da639e1d55ad03ad08f4659c7051bf1878c4263c791e63b8e9e976` |
| `final-targeted.log` | 0 | 7.610 | 6227 | `c15cdc2fb0ce830d232fe9ca466c1b946021f59794b5daeea56d5807f836310d` |
| `green-real-redis.log` | 0 | 6.165 | 889 | `d02dc80f325ec4c4b41ae70ea01935cbe9203237a3d1641d84d90f00a19324dc` |
| `logging.log` | 0 | 0.278 | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `packages-normal.log` | 0 | 98.525 | 779978 | `8884cf4d9448a4689abd024bd5bf42bd313ac1b1c1c8aeb0f6dd345aa2b076b6` |
| `packages-race-native.log` | 0 | 356.890 | 782521 | `27e6d1b2c336e982e357124eb8cff2d58aae799c0b16e3ac3004cda5eb7123ae` |
| `poll-measurement-final.log` | 0 | 10.720 | 3770 | `a6e22422da2309221b5caf3617314b7c9643f5e5df8d1c23d816624f3fb70927` |
| `poll-measurement.log` | 0 | 9.549 | 3346 | `0439e14fb21b34f7d21dcf752af242095816ec445bc3f252857194420b6f4ebb` |
| `red-real-redis.log` | 1 | 6.893 | 3035 | `c740a706affba04fb0fd92b80af5375776b5f807de6985ab844969ed78fb94ba` |
| `release-contract.log` | 0 | 78.977 | 1357 | `8f2d1b2c8ee142b5d8650ae26138a533f0cb938337fe338032bfa6c731979785` |
| `web-dist-sync.log` | 0 | 6.799 | 176 | `cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b` |
