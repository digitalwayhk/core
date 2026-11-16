# 普通 write-behind 冗余确认修复验收（2026-11-16）

## 结论与范围

本轮仅减少普通 `UseWriteBehind` 的重复完整业务编解码及分配。组件正确性、真实普通目标、恢复与兼容门禁通过；后台开启时分配量稳定下降，Save P99仍有差轮，不能宣称长尾、压实竞争或消费方SQL/端到端时延已解决。版本发布回执另按正式tag/Proxy/SumDB核验，不凭本报告假称发布。

实施前已在本文件记录设计与RED顺序：基线正式v1.13.3，提交 `18ca197cb22ea96ab0a708ffc1ade86e200777fe`；真实工作区 `internal-menu-snapshot`，独立分支 `fix/plain-writebehind-confirmation-metadata`。不夹带36b9锁WIP、Prefix、Gosched或compaction改动，不升级依赖，不操作Bitzoom/Full-E4及现场数据。

## 根因与实际调用链

普通调用链为 `syncAdaptive → processSyncQueueContext → getUnsyncedBatch → syncBatchContext → currentSyncItems → syncBatchWithTargetContext → target.SyncBatch → confirmSyncSuccess → batchConfirmSyncedStatus → Badger.Update`。首次选批已完整解码T；只需确认技术状态和版本的后两阶段再次完整解码，确认还重编码业务T。普通路径原本已经按批确认，并非逐条事务；也没有接入有界失败模式的共享FIFO/storageMu，不能误写为Core持锁等MySQL。

改后首次解析不变；同批原件未变时复用已完整解码的快照，确认仅更新原JSON封套的is_synced/synced_at，直接复制其余原值。target/SQL不进入本地提交器事务。仍写回整个Badger value并可靠提交，事务、I/O、压缩/GC和运行时竞争不会因此消失。

## 最小实现与安全收口

- `sharedbadger.go/getUnsyncedBatch`：完整codec成功、过滤已同步/接管状态后，创建私有本批读证明，不持久化、不跨轮保留正文。
- `sharedbadger_sync_read_proof.go/syncQueueReadProof.matches`：绑定同Badger DB指针、完整物理key、非零已提交MVCC版本、原UpdatedAt；复核与确认仍读取实际字节，必须匹配SHA256。仅MVCC不读字节的中间版未采用。
- `currentSyncItems`：全部匹配才省重复业务解码；变化、缺证明、坏原件仍原codec/版本检查，首次target始终得到实际T。
- `syncBatchWithTargetContext/confirmSyncSuccess/batchConfirmSyncedStatus`：target前固定原版本证明，原事务重核后复用现有json-iterator遍历复制；不是字符串全局替换或另一套JSON解析器。标准路径保留Item原编码和未来未知字段；非object、重复字段、已知字段非规范大小写别名或非标准封套回到原codec。
- Badger v4.9.6事务待写项的Item.Version是readTs，可能碰巧等于旧版本。每次实际确认事务维护已写key集合；重复key禁用快速证明，继续原IsSynced/pending处理。事务重试重新建集合，不跨回滚泄漏。
- 只有原本地事务提交成功才扣实际pending、累计SyncedItems、安排原IsSyncAfterDelete；重复/旧确认、部分未知、关库失败、tombstone和版本变化不伪造进展。自定义codec首次读取及回退继续执行，不能依赖技术确认重复触发其业务副作用。
- 生产仅上述两个文件；测试扩展现有性能fixture和四个普通确认测试文件，没有新提交器、调度器、线程、API、配置或卷格式。私有证明和map仅占本批有界空间。

## RED、失败尝试与GREEN

原RED是实际断言失败：完整业务codec重复调用及未知字段被丢失；不是编译失败。日志见下表。第一次RawMessage候选功能过但分配由约112MB增至132MB，否决；流式正文副本候选分配下降但持续立即target P99回退，未发布；最终为紧凑身份加实际编码SHA256，不用这些中间版性能替代最终版。

保留夹具失败：直接关闭底层db后fixture再次SyncWAL触发panic，改用现有CloseSharedManager注入真实关库；新PID恢复fixture曾将启动前pending缓存0误当排空，改为必须同时观察本进程SyncedItems=1，不放宽阈值。04/07首次真实集成中04通过、07 WS因未提供Redis尝试空闲6379失败，补本轮专属Redis后重跑，不改产品或断言。失败原件均保留，race断言FAIL不能写整体PASS。

最终普通与race全包覆盖原codec/CAS/坏原件/未知字段/重复别名/跨库同版本不同key/同字节重写/重复确认/pending/tombstone/普通及有界hook/关闭。定向实际编码证明错误必须回到完整codec。真实SIGKILL由新PID打开原Core+应用Badger，三个窗口为target持久成功但本地确认前、本地确认后清理前、Save可靠提交但结果未知；不发新Save即可恢复，应用事实幂等一次，仍是at-least-once而非exactly-once。

额外以v1.13.3原生产源码overlay构建旧二进制，复用同一进程入口完成旧写→新恢复、新写→旧恢复各三个硬杀窗口，六例均exit0且PID不同。仅代表本补丁普通卷双向兼容，不放宽更早有界终态格式门禁。

## 实际命令与结果

全部shell由RTK入口执行，`run.py`逐项保存argv、退出码、时长、日志bytes/SHA256。下表为真实原件名；不得用子进程入口默认SKIP替代父测试实际运行。

|原件名|退出码|秒|日志bytes|SHA256|
|---|---:|---:|---:|---|
|plain-confirmation-red|1|6.634|8014|6029d8a389987ddd02617614cd3e674f6e6d6169f84b4a76aba6cf2e58d711d9|
|plain-confirmation-green|0|7.762|6164|44378f20dd29c9f6129706e7a4287e7e29747d8e9ce8fe1142e93547c20dbc49|
|plain-boundaries-normal|1|7.043|16674|b42d45eeb3a9f5af4b69fdd9d4e4928a0e84e51801ca8340e0d8891480c38bec|
|plain-boundaries-normal-v2|0|10.937|79854|578974fddbdde491d7099d7dec774aaac73dbae4514165ec5663cb49c8602364|
|ordinary-package-normal|1|47.794|1280503|df8d483519d7e09251afa954520d63670421310db39bc6d563a47318353d21cf|
|ordinary-package-race|1|86.442|1239006|4f2253c20f80300bef9a5584c6c106eacc11f8c2eae8dc337de7c49245c1c0d5|
|plain-process-recovery-v2|0|18.912|1104|4bccc45882d55e04e5501d1e70d6055eda6e2005cbb34104488c5e98b1b74d9b|
|plain-hash-boundaries|0|19.352|29786|f82d1ca121b7e56af4ebec81fa67a0e9d15d1becba2b8712521d7486e41a62eb|
|final-nosql-normal|0|39.832|1306551|5c94ac5286178f116e5fbcf1626a5a0b1d1116ff6cbb27c4cb059f7d30c2e92e|
|final-nosql-race|0|71.613|1235964|a4de6d6230c03dfad8a341c4625386ce8eed2ef2dbfd8a1a93be445dd7ec2747|
|compatibility-process|0|2.735|3471|3fecaf3257d9ef7a81c8f52037c5f481c0261a86afb4d2af5b56257049a15f87|
|plain-real-mysql|0|10.547|61981|8f36c8bb6f5662c4504fb9ae6290bdc93f703aa3c49f41fa09b2566daa59a88a|
|plain-real-mysql-race|0|13.923|61984|b409959778f0ee1612a335fb7822a7093a2a598534afa51736a713a805cd9dbb|
|examples-04-07-normal|0|20.561|5645|b28103da754c98de4e352b0b8baa52dad73a1b5b48ac96cf302446b4ef90c1ec|
|examples-04-07-race|0|26.788|5645|bda87f1a4c8de98d577df680583db039cee3d8a2a57f0fcaf27b8d5e4faf665d|
|examples-04-07-integration|1|52.178|19839|68417a3145062a9c586cfaf4c797635a741675b24cbbacd19d443f5e12525e55|
|examples-04-07-integration-v2|0|65.797|16689|3503fefa0637d971135b75c05ac4fc43138ea2a695a97750bd5fc710e2b18d30|
|examples-04-07-integration-race|0|71.059|16677|284c4150504be4cfd03b533f8f11995f45291323451dbace340110326bc7eab6|
|gate-persistence|0|33.169|1215|e4a11df7bc0ac36f39c0a97f2f450be8048491b9f19a97aafb82f48da3742dc5|
|gate-config-contract|0|8.444|353|8307eb54ce7a1df21b93cdb2a58f4e4ea6263d9fe6199948e47d203e4cb559b1|
|gate-performance-contract|0|6.928|326|b38d75a98d1e878f88ff25982949155d8892791ad47902b02126fd5c2d619701|
|gate-api-compat|0|4.504|113|132eb8a8dafef4a35bfb610d6f920b71daf74d8efafc2e4bd4c637873fbbfd4a|
|gate-public-api|0|13.297|712|9b4df99ab9513bee02ffc13ac0fdb356080017ba26f3c0cbb2d07221685cc42d|
|gate-security|0|24.722|568|fd9be183c7ee5decc819fa2ab765b0ebfb50791216804c1f513513c5267ba016|
|gate-web-dist-sync|0|6.164|176|cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b|
|gate-release-contract|0|61.271|1861|887ba6a7262c76e3794e015b848a24afcc7687c2e61e2f273ea1c3e2845340de|
|gate-logging|0|0.262|0|e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|

主要实际命令（完整argv见同名JSON）：

```bash
rtk proxy python3 /private/tmp/core-plain-confirmation-fix-20261116/run.py plain-confirmation-red go test ./pkg/persistence/database/nosql -run '^TestPlainConfirmation' -v -count=1 -timeout=60s
rtk proxy python3 /private/tmp/core-plain-confirmation-fix-20261116/run.py final-nosql-normal go test ./pkg/persistence/database/nosql -v -count=1 -timeout=300s
rtk proxy python3 /private/tmp/core-plain-confirmation-fix-20261116/run.py final-nosql-race go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout=300s
rtk proxy env CORE_CONFIRM_PERF=1 CORE_CONFIRM_PERF_PLAIN=1 GOMAXPROCS=1 GOMEMLIMIT=1GiB CORE_CONFIRM_PERF_OUTPUT=<独立目录> go test ./pkg/persistence/database/nosql -run '^TestConfirmationPerformanceComparison$' -v -count=1 -timeout=300s
```

旧性能组的`-overlay baseline-overlay-mvcc.json`只恢复v1.13.3生产sharedbadger.go并屏蔽仅新字段的定向测试，未修改模块缓存或依赖。三轮持续由baseline-mvcc一轮与baseline-hash-repeat两轮组成；候选为candidate-hash-full一轮与candidate-hash-repeat两轮。各实际命令、固定源码、构建绑定和全量result.json在证据目录。

Core专属MySQL8.0.43随机58111、Redis7.4.12随机64225，均全新容器、没有业务挂载。真实MySQL原ModelList目标九种输入/调度组合普通及race通过，核远端行数与pending；这是接线回归，不作为真实消费方SQL性能。04/07源码普通/race和真实HTTP/WS/恢复另取证。本轮容器仅在测试后按精确ID清理，不操作其它项目。

## 同机最终性能：收益与差轮

固定本机APFS非tmpfs，SyncWrites=true、DetectConflicts=true；GOMAXPROCS=1/GOMEMLIMIT=1GiB，并非1CPU/1GiB硬cgroup。50并发、2028字节合成完整Gob、MaxBatch128/1ms/Queue1024，SyncBatch128/FlushThreshold32/Collect20ms/Drain0。三轮短窗各2000笔；三轮持续各20秒40000次Save、2000key固定工作集更新，不是40000个不可合并业务事实的稳态认证。编译、回归和探索候选与最终性能分开。

短窗立即target分配111.86–111.87MB→83.54–83.56MB，约25%下降；受控20ms目标112.01–112.02→83.74MB。CPU有波动，延迟短窗部分候选CPU更高，不宣称普遍CPU下降。持续分配中位数立即约15.18%、延迟约14.87%下降；无后台约持平，未混入有界路径收益。

|源码|场景|轮次目录|平均ms|P50|P95|P99|最大|分配MB|CPU秒|确认|pending|排空秒|
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
|baseline|plain_delayed_sync|3-plain_delayed_sync-3238322314|4.661|3.869|9.773|20.863|31.776|4376.59|5.537|40000|0|0.0431|
|baseline|plain_fast_sync|3-plain_fast_sync-768536520|5.466|4.722|10.438|22.742|48.245|3759.46|3.573|40000|0|0.0011|
|baseline|plain_no_sync|3-plain_no_sync-1585246716|4.069|2.621|7.834|20.768|78.554|1523.81|1.931|0|2000|0.0000|
|baseline|plain_delayed_sync|3-plain_delayed_sync-1534259436|5.180|3.881|10.760|23.154|43.946|4427.92|6.947|40000|0|0.0441|
|baseline|plain_delayed_sync|3-plain_delayed_sync-2609353130|5.022|4.015|10.269|23.054|44.233|4428.50|6.264|40000|0|0.0508|
|baseline|plain_fast_sync|3-plain_fast_sync-1460402034|8.572|8.378|13.511|21.944|33.882|3756.31|5.885|40000|0|0.0000|
|baseline|plain_fast_sync|3-plain_fast_sync-409187974|8.766|9.009|13.568|20.970|29.174|3757.00|5.796|40000|0|0.0000|
|baseline|plain_no_sync|3-plain_no_sync-591595878|5.280|4.776|8.191|22.892|57.796|1523.30|2.805|0|2000|0.0000|
|baseline|plain_no_sync|3-plain_no_sync-823809074|5.473|4.880|10.065|16.443|45.444|1523.13|2.955|0|2000|0.0000|
|candidate|plain_delayed_sync|3-plain_delayed_sync-1178080844|5.292|4.283|10.520|20.498|104.913|3769.58|5.852|40000|0|0.0251|
|candidate|plain_fast_sync|3-plain_fast_sync-1123556901|7.967|7.737|14.042|25.082|54.099|3184.54|5.073|40000|0|0.0011|
|candidate|plain_no_sync|3-plain_no_sync-101496592|4.538|4.056|7.750|18.693|33.564|1524.09|2.591|0|2000|0.0000|
|candidate|plain_delayed_sync|3-plain_delayed_sync-3204923829|4.721|3.821|10.751|16.865|33.706|3785.29|5.651|40000|0|0.0424|
|candidate|plain_delayed_sync|3-plain_delayed_sync-3365052118|4.578|3.866|9.239|17.161|45.840|3688.01|5.026|40000|0|0.0330|
|candidate|plain_fast_sync|3-plain_fast_sync-1834252599|7.057|6.607|12.011|17.975|42.992|3187.25|4.700|40000|0|0.0000|
|candidate|plain_fast_sync|3-plain_fast_sync-1866034938|7.005|6.159|12.933|21.919|28.449|3186.81|4.751|40000|0|0.0011|
|candidate|plain_no_sync|3-plain_no_sync-3732722437|3.111|2.433|6.328|13.819|38.256|1524.22|1.453|0|2000|0.0000|
|candidate|plain_no_sync|3-plain_no_sync-927914439|3.929|3.455|6.900|17.081|36.606|1523.80|2.139|0|2000|0.0000|


后台各持续轮40000次Save均完成、errors=0、本地确认40000、持久pending=0；无后台组仍2000 pending，不能称同步收敛。立即target P99旧20.970–22.742ms、新17.975–25.082ms，存在回退；20ms受控target P99旧20.863–23.154、新16.865–20.498ms，但新最大104.913ms也保留。不承诺稳定P99改善。无后台CPU与尾部本身漂移，不能把全部CPU差或单个差轮归因于优化。

原累计Admission、Batch.QueueWait、ForegroundLocal/MetadataLocal的等待/提交、目标调用/耗时、GC、峰值RSS和有效Options保存在performance-final-summary.json及原result.json。普通确认仍计入既有MetadataLocal，ConfirmationLocal/Sync.TargetDuration在本模式未单独采集，其零不能解释成未发生；性能fixture的target_duration_ns另包裹实际目标。前台/后台累计时间与嵌套阶段不能相加当单请求壁钟；纯事务体/fsync、I/O与硬CPU限流未采集仍null/NOT_RUN。确认事务没有改成逐条，提交次数也不等于fsync次数。

## 接入、回退与未验证

消费方继续原UseWriteBehind/AutoSync，升级包后自动应用，无开关、无需转有界、无需改业务target或StoreID/policy；原卷不清理、不重同步。本补丁不新增持久字段和回退门禁，普通卷已双向新PID验证；更早已启用有界终态等格式限制仍按原指南，不能让不兼容旧二进制回开。实际内存读证明不跨关闭/重启保存，启动继续从原件正常建立。

`IsSyncAfterDelete=false`保留；true仍仅原版本确认提交后异步清理。确认后清理前退出，重启不会为补删重发已确认target，旧原件可能保留；这条已有契约未改。不能调用PurgeLocal/Delete或清历史制造排空。

PASS：最终整包普通/race、真实临时Badger、三窗口硬杀/新PID及旧新双向普通卷、真实MySQL原target、04/07源码与集成、既有发布门禁。FAIL原件：初始RED、夹具失败及被否决性能候选如上；SKIP：默认性能/外部依赖及子进程入口在常规整包下的保护性SKIP不作为外部通过。NOT_RUN：消费方接入/功能/Full-E4/UAT/容量，现场3.6秒或615ms延迟因果闭环，真实消费方SQL性能、长期磁盘稳态、物理断电和其它平台、未采集I/O/fsync/硬限流。本轮不改变compaction参数，也不把解码优化称作压实修复。

主Agent直接核对私有证明范围、实际字节读取、pendingWrites重复key陷阱、事务重试与提交后计数/hook、未知字段/回退、生命周期及卷字节兼容；遵项目禁子Agent要求，没有派发独立Agent审查。Code review: skipped (ce-code-review unavailable)。这是未执行该工具流程，不是声称未进行源码审查。

## 原始证据

本轮：`/private/tmp/core-plain-confirmation-fix-20261116/`。`source-manifest-final.json`绑定最终生产/测试源码；`commands-final-summary.json`含完整argv和日志哈希；`performance-final-summary.json`保存每轮原件路径/全部分段；`compatibility-process-results.json`含六次硬杀/新PID/原目录；失败尝试未覆盖。后续正式发布回执和私有归档另由精确path/bytes/SHA256交付；未取得远端tag与Go代理前不编造版本可用性。
