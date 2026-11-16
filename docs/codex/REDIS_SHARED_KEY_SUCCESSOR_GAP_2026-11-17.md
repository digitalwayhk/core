# SharedKeys 同key后继间隙与已知候选正文往返（2026-11-17）

## 结论与边界

本轮基于正式v1.13.5（9b7f7cda537fbff356664357219ccc2b8779575a），证实并最小修复普通SharedKeys调度器在原PEL页中逐条读取已知候选正文的N+1网络往返。受控8候选旧8次、候选1次往返为真实RED→GREEN，候选仍取8份实际原正文、16次实际成功ACK，不是删除退避或缩小handler。

未证明现场约38ms或发布标记到handler的6882ms全部由本项造成。现场idle PING三角色P50约0.09–0.10ms，只是负载结束后的同连接PING，不能替代主窗Lua/连接池排队；本机1ms回包代理是明确注入的诊断条件，不是现场RTT。消费方主窗7500全闭环，10025=2500预热+7500主窗+25排空，3条drain NEW等待对手不算丢失，不能把这些业务事实当Core组件测量。没有操作Bitzoom/Full-E4，没有新Agent/Task/终端。

## 实际调用链与根因

`SubscribeReliable → runReliableKeyedSubscriber → runReliableKeyedOwner → claimReliableKeyedPage → 原XPENDING页/有限ID提示 → 各key最早可准入候选 → pendingBody逐条同步EVAL/XRANGE → 保存tasks → 下一轮派发 → processSharedReliableMessage → fenced XCLAIM → processReliableMessage → handler → renewOwner → fenced ACK → release → results → 下一轮PEL/派发`。

旧版单个调度协程在同一页内逐条调用pendingBody，每个候选都等一次响应，所有候选准备完成后才准入并进入下一轮派发；结果消费也只在轮次边界进行。网络/排队成本会乘以候选数量，同实例也可出现间隙，不能只归跨实例交接。Scan/Prefetch有界并不等于一次往返有界。

50ms仅是未取得执行租约/消息已完成或默认失败的重试，成功路径release delay=0；waitRedisKeyed最多100ms但结果通道可立即唤醒。看到这些常量不能认定它们等于现场每条gap；本轮均未修改。

## 最小生产修改与保护

- `provider_redis_keyed.go/runReliableKeyedOwner`：保留原页、原顺序与每key最早候选筛选。收集该页真正待准入的提示ID及原位置；同一pipeline取回后逐项比对ID和ordering key，再进入原调度。任一缺正文、取消、身份冲突或读取错误时该页不准入，不跳过错误项。
- `provider_redis_pending_hints.go/pendingBodies`：复用go-redis成熟Pipelined与原只读Lua及XAutoClaim解码。命令数/结果数最多当前原页候选数、受原W约束；减少串行网络往返，不宣称Redis内部XRANGE命令数减少或工作量为零。不另建队列、线程、正文缓存、业务消息副本或调度器。
- 原Owned reader使用的单项pendingBody保持行为；只抽出相同只读脚本常量，不改变scope/fence/恢复。普通零值及非SharedKeys不切模式。
- 保留同key最早未解决项、原任务/结果通道、有限N/W、50ms重试/原backoff、fence、真实handler退出、成功后ACK及生命周期安全回收。target/SQL仍不在本地提交器。本轮不涉及write-behind或SQL业务。

无公开API、配置、依赖、持久格式、Group/W标记、Stream、PEL、cursor、消息ID或业务payload变化。

## 真实RED、失败与GREEN

先用真实Reliable Provider和Redis发布8组各两项，控制首轮8个handler均在途、原PEL页暂停；释放首轮并等待真实ACK/租约归还后恢复读取。旧页8次往返断言FAIL；候选一次且实际正文8/ACK16。缺正文/取消/返回顺序和读取不能代替ACK另外覆盖。既有丢ACK回复、旧owner迟到、失败/同key屏障、跨窗口、有界预取和Owned回归继续运行。

完整普通首轮FAIL在`TestSharedPendingKnownTailMissingAfterBarrier`：原测试hook只看ProcessHook，pipeline不会走它，缺失错误已真实返回但未被原计数器观察。已仅让原观察器同时读取ProcessPipelineHook的每个实际响应；原missing>0、calls=1、缺项和后继仍在PEL等全部断言保持，没有延长等待或吞错。此首轮FAIL永久保留。

新硬杀夹具首轮仅等待25秒而源码原生lease至少120秒，因预算错误FAIL；不是产品恢复缺陷或产品RED。已按ownerLeaseTTL原值等待、子进程护栏180秒，不缩TTL、不删锁。实际SIGKILL旧PID，新两个PID等待原lease自然到期恢复原消息ID并顺序处理后继，独立健康key先完成；原生恢复race通过。未知业务副作用仍须应用幂等，组件没有宣称exactly-once或强杀远端SQL。

## 固定二进制三轮对照

真实独占本机Redis，3独立消费者，每进程GOMAXPROCS=2（不是硬CPU限额）、command32/blocking16、N32/W128，50合成key每key10/s输入10秒共5000条，90ms成功handler。发布者直连，消费者经明确1ms回包代理；旧正式两生产文件逐字overlay，候选与测试二进制SHA保留。编译与性能窗口分开。没有SQL、业务数据或新的业务SLO门槛。

|版本/轮|同实例gap均值ms|P50|P95|P99|全gap最大ms|完成/ACK|总墙钟s|
|---|---:|---:|---:|---:|---:|---:|---:|
|baseline/1|68.186|65.537|109.663|158.314|246.304|5000|16.181|
|baseline/2|67.033|64.278|114.365|124.845|167.941|5000|16.070|
|baseline/3|66.485|65.400|102.212|128.905|162.038|5000|15.904|
|candidate/1|34.298|33.186|54.808|67.301|160.806|5000|12.901|
|candidate/2|28.752|25.018|49.799|60.820|146.785|5000|12.358|
|candidate/3|30.440|26.368|52.986|65.478|142.427|5000|12.566|

原直连旧版：5000条全部处理，同实例gap P50=6.012ms/P99=18.974ms，不能凭此否定现场。代理旧版三轮总墙钟约16秒，新约12–13秒；每轮均5000完成、各3PID实际有量、同key无重叠且按sequence执行、原PEL排空。新版本仍追不上每key10/s，不能宣称容量或端到端已经达标。

只读调度overlay另跑，和固定主对照分开：旧同实例采样release→结果消费P50 4.332ms，完成消费→下一个准入21.074ms，准入→acquire28.395ms。单个真实中位样本在约63.53ms间隙中出现17次正文读取。各阶段P50不能相加当某条请求，累计/并发时间不当墙钟。

新诊断（独立轮）：全部4950对handler结束→下次开始P50=25.916ms，handler→ACK 3.659ms、ACK→release 1.587ms、release→acquire 17.625ms、acquire 2.119ms、acquire→handler 0.010ms。仍有竞争重试、逐项pending确认和本地轮次/资源等待，未在本轮继续改调度算法。不是无成本移交或固定15ms保证。

原件根：`/private/tmp/core-shared-key-next-gap-20261117/`。测试命令、exit、时间、日志bytes/SHA逐件见同名JSON；性能原始事件、每PID时钟、对照binary-binding.json、固定旧源码和overlay保留，调试overlay不进入正式代码。

## 实际命令和结果原件

|原件|exit|秒|bytes|SHA256|
|---|---:|---:|---:|---|
|baseline-delayed-workers|0|21.215|1060|5dd44f548ea6f631059ec82e01474b080ca05d7b0c1ce0a54a109af523deb352|
|baseline-delayed|0|20.436|1056|0bcde26c8cecad8414fc40e9393bb192793e5b72aa05b7316701853ed98035b4|
|baseline-fixed-1|0|17.070|1003|69c56e35cc6874c6829585a80b963f85491ad5ee6ff0b490b281722ec8d564f5|
|baseline-fixed-2|0|16.217|1004|a66c5a98ef9c98416eef7f38d0cedb326521831bb4fe98a8aa5342220d0e852d|
|baseline-fixed-3|0|16.004|1000|defcf22f3801b6c3ac762f170059b679bb3c607422369d728f1deca3bb6cdc27|
|baseline-instrumented|0|22.878|1056|d337cdd57a905efd6ebf04ecd65775ea136171ac3d5364822cf8bad314a1372f|
|candidate-delayed-workers|0|15.844|1059|49366d3c0caf93f0d259361cd4a009e30749b127d30ddf718e8604b9ffe7cc1b|
|candidate-fixed-1|0|13.652|1003|1bf7f2e39c8a6db5a9443e8283090e9828ad8637d5426b5de5f8c91c65ab406f|
|candidate-fixed-2|0|12.456|1003|e9aa2fd1494137b5ea528cf745df1570b3549ec74b5e8b8b4508b7d5b300d0cb|
|candidate-fixed-3|0|12.697|1002|82b5c405391d53254f1680a28cbc2a6cde40dae4d2536db5dc47d6c23408a61d|
|candidate-instrumented|0|15.996|1058|98e1ed0222968512cd13ab51338cd92cbcf5af9cda50c8b9a215d9b176dd3198|
|example10-real-process|0|55.384|90|fc28fa560db0366a4b3d6b6bbe51956c324b171e8ba5723b8d8ec26826325fe5|
|gate-api-compat|0|9.485|113|10bc75857487f394d90c183deabb9ff570311604b3889b860bb2ef08ebb5e01c|
|gate-config-contract|0|7.750|353|587e2a9eefc20fc93dd2893f1e0b070caae15ca605a4b94a92a8a0f16df0e8a7|
|gate-logging|0|0.214|0|e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|
|gate-performance-contract|0|9.317|326|8b41d5112f0a59d8d6447ef294f20033a7c4d6519b071be1c6cea7547b6b3709|
|gate-public-api|0|12.349|712|9b4df99ab9513bee02ffc13ac0fdb356080017ba26f3c0cbb2d07221685cc42d|
|gate-release-contract|0|48.946|1861|85173fc6d931e73a67a5be086e4cbf339d8f670092161c2acfb043db6743a2cb|
|gate-security|0|24.153|568|4718d533380a093e01e07d06c03c3e486c57f9b0fc93abdc8e06f8cfa884eee9|
|gate-web-dist-sync|0|4.988|176|cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b|
|hint-batch-green|0|3.776|307|26966a6d02de94967121dc74adb37998357921956d1b2ac1b85d4cfc67377888|
|hint-batch-red|1|4.412|631|16c5573277d2cb8d1533f83321939c19cb141598d13e5d377ce2aa825b61ecf4|
|hint-boundaries-recovery|1|29.073|853|8fe1358458c89578c9f849b869e6a244d968b88840f1d4e7b37c564a60db23e7|
|missing-tail-hook-green|0|4.832|1392|dedeb6506415557730a65740e1daeace2e79b05d7a097b1404be46396d54e6d8|
|mq-event-router-normal-v2|0|80.249|241|73a4b3c622d307599dbeb58c7da667886100b6f1b4b7b9e093d61bba93a8501c|
|mq-event-router-normal|1|89.042|19962|7cb0356fdc0c66dc70732e4292bc2f917bb9e876293a8c2f9766d01be9a94014|
|mq-event-router-race-v2|0|101.885|241|8e6a1d151b3d558d692100ed813e7d938ddc909413ed82ad9233085bbc6c09af|
|native-recovery-race|0|128.016|747|6c2ad441d08321098663c3548c4cbb9f952cd85600f4686697346b7d18c31f01|
|three-process-stage-race|0|10.451|1047|d21eef6f7c1dedeb9472c926367c5e0de96ed2d4395e8a521944976f15ed9fbd|

原始命令由`run.py`完整保存；关键入口为`go test ./pkg/server/mq -run '^TestRedisSharedHintCandidatesUseOneBoundedRoundTrip$'`、`go test -race ./pkg/server/mq -run '^TestRedisSharedHintProcessCrashRecovery$' -timeout=180s`，以及四包真实Redis普通/race与示例10真实三进程。外部依赖/诊断入口默认SKIP不算通过。

## 升级与未验证

只需升级消费者Core，不改变Subscription、KeyConcurrency、SharedKeys、Prefetch、Group或payload；发布者没有因此改变发布时点。旧/新消费者可以在原冻结模式/W一致的前提下沿原滚动生命周期替换，旧副本仍有旧读取成本；不需清PEL、换组或重置游标。正常取消并等待实际handler退出后回退同代旧包，原数据保持。不替应用决定key或未知副作用处置。

go get不安装skill，按README/link-consumer-skill.sh重新同步权威skill。无额外开关。未采集CPU硬限流、Redis主线程CPU、I/O、内存/GC和连接池分位不填零；主对照只报告实际采到的时钟、计数、顺序与排空。

NOT_RUN：消费方实际主窗RTT/逐命令根因闭合、Bitzoom升级/功能/UAT/Full-E4/容量、真实SQL、长期稳态、其他平台/物理断电。NATS/Owned性能不是本项验收对象。没有部署消费方、清原消息或变更现场参数。本轮功能通过不能宣称全部现场38ms或6882ms已经解决。

本轮下一合法补丁为v1.13.6，正式可取得性以发布回执为准。完成后使用`go get github.com/digitalwayhk/core@v1.13.6`；没有新参数，需按既有流程同步skill。额外三进程race的1500条合成消息已逐条绑定首次Broker读取、调度准入、成功租约、handler、ACK与release时钟，见three-process-stage-race/message-stage-clock-binding.json。这是本机合成消息的关联，不是消费方现场复跑。主Agent按正确性、FIFO/资源边界、可读性、安全和性能逐项审查；无外部审查Agent、无其他专项混入。
