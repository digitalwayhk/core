# 有界独立组确认控制锁与FIFO合批验收（2026-11-16）

## 结论、范围和未消除的问题

本轮修复显式IndependentGroups的确认/归还控制锁串行等待；沿原BatchCommitter FIFO有限合并相邻独立结果，不新建提交器、队列或worker。32组受控RED转GREEN，原事务身份、未知结果、组屏障、实际确认、取消/关闭与新PID恢复保持；确认事务数显著下降。Save P99仍有差轮和回退，不能宣称前台长尾、消费方13秒队列或全链路容量已解决。

基线正式v1.13.4/`2ff7ad5904cfbcca4d26dc54d5c6c20360f4137e`；现场交接为v1.13.3，四个相关文件与v1.13.4逐字相同，绑定见published-baseline-source-binding.json。普通v1.13.4确认优化保留，不切换旧模式、不改参数/compaction/Prefix/yield/依赖或其他工作区WIP。本轮不操作Bitzoom/Full-E4。实施前已先写本文件中的中文记录，原件保存在implementation-before-final-report.zh-CN.md。

额外04真实进程race首轮捕获恢复请求500和另一个子进程的Start/Stop桥指针竞态；未修复且不将两现象强归同因。后续单例/完整复核结果逐项保留，不能用重跑PASS消除首次FAIL。详见后文已知边界。

## 根因与实际调用链

`syncWithIndependentGroups → enqueueIndependentGroups → 原持久选批/预留 → 固定worker → executeDispositionBatch → 原target → syncExecMu → commitDispositionResults → 原FIFO → storageMu/Badger.Update → worker再次syncExecMu → 删除activeIDs/activeGroups/归还槽 → triggerSync`。

旧确认在等待FIFO和实际可靠提交时持syncExecMu，阻止其它已返回target的结果进入同FIFO，并拖住选批。已确认worker又取同锁清理内存登记，进一步延迟原组后继；这段归还等待发生在recordSyncMetrics之后，不在Sync.TotalDuration内。target/SQL/失败回调不跨storageMu，不能写成Core持本地锁等MySQL。现场多协程累计35.89s/27.80s是交接证据，不是单条延迟或本轮本机测量。

## 最小实现与保护

- `writebehind_group_execution.go`：syncExecMu保留选批/预留/历史维护串行；组/ID登记和实际回调退出后的归还复用执行器既有短内存mu，不跨FIFO或I/O。登记仍在选择事务提交后、另一次选批之前；归还仍在结果处理和实际回调退出后，不能把target返回当本地确认成功。
- `writebehind_disposition_schedule.go`：显式独立组结果不持控制锁等FIFO；实际提交再复核ctx/store关闭，不将取消/迟到成功当业务完成。默认整轮模式继续原锁/target合同。
- `reliable_write_batcher.go`、`reliable_write_local_commit.go`、`reliable_write_confirmation_commit.go`：只在原requests chan增加私有确认请求类型。仅非阻塞收集已相邻入队、同store的结果，最多原8项/约1MiB；遇前台保存、元数据屏障、不同store或预算立即停止，不跨越重排，不添加收集窗口。边界单项carry沿原FIFO继续，不是另一队列。前台原1ms/准入/接受后等待真实结果保持。
- `writebehind_confirmation_command.go`和原confirmation_batch：复用同prefix storageMu与实际Badger事务，逐项核对ID/key/UpdatedAt/指纹/state/attempt/BatchID，再处理当前原件和索引。语义过期只拒绝该项，真实本地事务失败整块不发布结果；TooBig只重建本地写集并分别保留成功/失败子块，不重调target。事务内组核验仍在最终写集上去重。
- 每个独立target仍一组首、各自BatchID/cause/结果；实际事务成功后才由原worker扣真实pending、累计Sync和安排原IsSyncAfterDelete/失败通知，这些回调不在唯一提交worker。取消/失败/未知保持原有限处置和屏障；原nil终态Item、失败历史和保留政策不变。
- 预算编码准备在入队前，并复用commitDispositionResults原来已经计算的同快照预算；直接私有块入口保守补算。第一版把重复编码放提交worker、第二版虽前移仍重复编码，都保留原件，不以其数据冒充最终第三版。

没有新增公开API/配置、StoreID/policy、持久字段或格式。ReliableWriteStore已显式启用IndependentGroups时自动采用；直接PrefixedBadgerDB没有facade FIFO时继续原本地互斥，不新建提交器。普通UseWriteBehind与旧有界零值入口不转模式。

## 真实RED、GREEN与恢复

真实Badger和正式可靠store，32不同组各连续两项；target与FIFO分别受控。旧版手动/自动已返回target不能全部进入原FIFO，32项仍32次确认；已提交组再次等控制锁归还也真实FAIL。旧源码race RED同样因断言FAIL，不可因无DATA RACE写整体PASS。候选保留32份实际入队断言，真实确认前pending不变，提交后组后继与attempt准确；相邻确认最多8项，前台/屏障不能被跨越。

新回归覆盖入队后取消保留Unknown、坏生命周期导致实际事务整块回滚、旧指纹/跨DB隔离、真实小memtable下确认TooBig拆块、同key改组/条带碰撞、原cause/BatchID隔离、失败handler未退出、关闭超时和普通/有界hook。不能将接管/暂停/Release当业务成功。首轮构建unused import是构建FAIL，单列保留，不能当产品RED。

真实SIGKILL复用原进程协议：32个应用事实持久完成且32份确认实际入原FIFO后、确认已提交但原worker尚未交付结果时分别硬杀；新PID打开原Core与应用Badger，不发新Save，核32原事实各一次、原attempt=1和终态。原A未知/B健康不重放场景仍保留。另固定旧v1.13.4/候选二进制原卷双向新PID恢复通过，不改变更早终态格式回退门禁。

## 实际命令与结果

RTK入口调用run.py，逐项保存完整argv、exit、日志bytes/SHA。原件根为`/private/tmp/core-independent-confirmation-convoy-20261116/`。所有失败/SKIP保留，未配置的真实MySQL/Redis/WS本轮不冒充通过。

|原件名|exit|秒|日志bytes|SHA256|
|---|---:|---:|---:|---|
|convoy-red|1|10.684|9130|32e9e8f5f57c17b19954f30a8c0ec25024ac0e4fa4a10cfe04778ab0879978b1|
|convoy-baseline-red-race|1|15.241|10367|d83cc6c19d4cc0ade830a44de9a64687047ecf55edf49e163732dce11b2a8831|
|convoy-green|1|0.585|313|bb5010db228b55014c43ed22c1499d6c4427d4a81744668514355684421cdb96|
|convoy-green-v2|0|9.654|63300|4b850c2fdf62c84e5961645db18181d8c29932461e0a604019e609e0656daccc|
|convoy-boundaries-race|0|17.150|98842|9cbfe9c5457d71cbf7db1d2de14c5715c3639b347f3840490e15bfbc70f9c584|
|convoy-recovery-race|0|19.533|21903|cfc360102eb3159c1736469f59a8d087f1d9ef585c4f9a83a427304a69b1beef|
|convoy-v3-targeted-race|0|21.257|79313|8c06f75132c5672cb9114b7fd720e471524c101aadda4538e883f79a403c5bc7|
|full-nosql-v3-normal|0|37.790|1308459|3f870fe8ad2c059e600e7305f362177caa2b181ca9d895681100e5d580c18b12|
|full-nosql-v3-race|0|65.510|1263346|3480e3efa73b7d9bf99b23c5409112f431b630283f11eaab45f4ac6e74be99cb|
|compatibility-v3-process|0|0.590|500|0e66f48c0c843c1d229d27b31c8e11602bde81ca16e35ee7c5cb9f6c5b588547|
|compatibility-process|0|0.756|481|b6df27c6f1348478cfc6afd4a3683974bb06455bd874ee03dab30421d501ec79|
|example04-process-race|1|40.327|126371|d2ba8e94f52152a6402555e354ab3e5ed3da2102fd858cb5ba5a5db1524a9ee3|
|example04-baseline-race|1|62.600|200713|dfe443826370116e9557c8a1679747649ee4470ec4904587c70898a5141edfc2|
|example04-baseline-fresh-race|0|41.471|1413|5908ba97a8c65a5472ab1984897f2f19229d6104730edc135b7ec79b4dd59988|
|example04-candidate-fresh-race|0|41.254|1412|ba6c06f33fd2dcb6ab7a55bc9caeeae0df43957b306432eb2409036bc3b55c2a|
|example04-final-full-race|0|48.414|4090|eb539a6fb531c2e7007afb494007e9fe4dc32ae815c0cc43368f361b54372c43|
|example04-v3-process|0|52.348|4091|41f843b71cdc4007d84173553ed47d68f9accbfe364d3274b9f9a6ba830dd70d|
|example04-v3-process-race|0|41.797|4091|c23fa27652beec670ace92cdf15470f56037f0f2b2b04b433deba6edf37103af|
|examples-v3-04-07-normal|0|14.350|5645|3774e9602863c48ceac701cd9da8581f6dc3588b8fb8a12438d19f4edb2ad9ec|
|examples-v3-04-07-race|0|13.666|5645|17fd54a76a0ef3766f7cfdcd5ea53d1a18775a5d520a74ef5d25314019125250|
|gate-v3-persistence|0|33.765|1215|d36a5866df2d8461f443550ff0b6185f3b1cc1ab8bbb2059537445f57e9a0736|
|gate-v3-config-contract|0|8.224|353|4266dcf3573eaf69374128c6408e99d56d4912046a088d89b8368e699da0107b|
|gate-v3-performance-contract|0|7.286|326|d31e284835a7b94365fa5c6e065a4252711d576b13af1726472e9e8de3bfe58e|
|gate-v3-api-compat|0|4.453|113|327fc4f37d245013a7466565214c5e907377cf9bc3322b5748483f2244276653|
|gate-v3-public-api|0|11.357|712|9b4df99ab9513bee02ffc13ac0fdb356080017ba26f3c0cbb2d07221685cc42d|
|gate-v3-security|0|24.606|568|d2bae13f61250a4ec7a7657eb7150ba1690b423851f8f4ef20e1f95f8adb257d|
|gate-v3-web-dist-sync|0|4.664|176|cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b|
|gate-v3-release-contract|0|45.361|1861|583e59b55bc86c28c614721bafe0eae0ed3b6897afab78cd0f7ba42ba87a570b|
|gate-v3-logging|0|0.190|0|e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|

核心实际命令：

```bash
rtk proxy python3 /private/tmp/core-independent-confirmation-convoy-20261116/run.py convoy-red go test ./pkg/persistence/database/nosql -run '^TestIndependentConfirmation' -v -count=1 -timeout=60s
rtk proxy go test -race -overlay /private/tmp/core-independent-confirmation-convoy-20261116/baseline-red-overlay.json ./pkg/persistence/database/nosql -run '^TestIndependentConfirmation' -v -count=1 -timeout=90s
rtk proxy go test ./pkg/persistence/database/nosql -v -count=1 -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout=300s
```

全量原命令与环境意图分别见commands-final-summary.json、固定二进制binding及performance-series.py。旧/新二进制分别封存，独立源码overlay不改模块缓存。04/07源码普通/race与04真实进程另取证；Core组件恢复不代表消费方实际SQL或Full-E4恢复。

## 最终同机多轮性能与代价

固定APFS非tmpfs、SyncWrites=true/DetectConflicts=true、GOMAXPROCS=2、GOMEMLIMIT=1GiB（非1.75核硬cgroup）、32worker、原1ms本地合批/条数/队列/准入预算、2028字节合成Gob；立即target和5ms受控timer各3个2000次短窗及3个20秒/40000次Save持续窗。编译/回归与主性能分开。持续窗为2000key更新工作集，原合并/CAS可能令实际确认少于Save，不能称40000不可合并事实吞吐。测量后逐件核最新2000原件、载荷与持久pending=0。

|源码|目标ms|轮目录|Save均ms|P50|P95|P99|最大|真实确认|确认事务|最大确认批|同步累计/确认ms|排空s|CPU秒|
|---|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
|baseline|0|3-bounded_fast_sync-642729404|12.782|4.054|24.018|151.175|1562.051|30409|31045|1|9.679|0.1311|14.742|
|baseline|0|3-bounded_fast_sync-722338617|4.497|4.173|7.710|19.769|37.664|39999|40000|1|3.810|0.0450|17.765|
|baseline|0|3-bounded_fast_sync-737485417|4.327|4.090|6.537|16.050|57.507|40000|40000|1|3.833|0.0622|18.252|
|baseline|5|3-bounded_fast_sync-2117046363|3.814|3.405|5.894|9.713|44.965|40000|40000|1|9.849|0.0960|17.537|
|baseline|5|3-bounded_fast_sync-4156969836|4.252|3.529|7.226|15.143|125.447|40000|40000|1|10.260|0.1652|17.940|
|baseline|5|3-bounded_fast_sync-909112551|4.420|3.818|7.830|16.598|56.975|40000|40000|1|10.260|0.4471|18.592|
|candidate-v3|0|3-bounded_fast_sync-2236070269|5.216|4.571|9.649|24.050|78.064|39993|8000|8|3.528|0.1170|17.624|
|candidate-v3|0|3-bounded_fast_sync-2629829962|4.967|4.553|8.884|14.754|56.148|40000|7959|8|3.468|0.0180|17.858|
|candidate-v3|0|3-bounded_fast_sync-4145993754|5.040|4.540|8.573|20.428|63.941|40000|7952|8|3.534|0.0342|17.847|
|candidate-v3|5|3-bounded_fast_sync-1310819192|5.443|4.693|10.658|23.078|54.158|40000|8169|8|9.023|0.0752|18.028|
|candidate-v3|5|3-bounded_fast_sync-36485687|7.116|4.686|14.519|80.310|175.736|40000|7978|8|9.662|0.8012|18.193|
|candidate-v3|5|3-bounded_fast_sync-3897920428|5.568|4.488|11.055|25.839|75.325|40000|8089|8|9.164|0.2653|18.432|

确认事务约4万→0.8万，原8项量子不扩大；5ms目标真实确认各4万，同步累计/实际确认由旧9.85–10.26ms降至新9.02–9.66ms，但这是累计归一化、仍不含worker归还，不是单条分位数。前台Save P99旧9.71–16.60ms、新23.08–80.31ms，明确回退；新最大175.74ms、旧立即目标约151ms差轮均保留，不能以确认收益宣称前台全面改善。第一、二候选及最终所有48轮在performance-final-all-rounds.json，禁止挑好轮或改预算换结果。

原Admission、Batch及ForegroundLocal/MetadataLocal/ConfirmationLocal的队列/锁/事务累计、target、CPU/GC/内存/有效Options均在JSON。未采集I/O、fsync、纯提交/事务体和硬CPU限流保持null/NOT_RUN；累计与嵌套阶段不可相加当单笔壁钟。新确认队列等待增大是等待分布变化的一部分，不能只看mutex消失；下一表另报告实际归还与确认FIFO。

## 独立Go阻塞/互斥诊断

另跑20秒/4万工作诊断，-test.blockprofilerate=1、mutexprofilefraction=1，和主性能数字分开；所有Go原始profile、raw位置及二进制hash保留。下列是多协程累计毫秒，target含Go等待，不是单条函数耗时，也不是现场R512复跑。

|阶段|旧累计ms/次数|最终累计ms/次数|
|---|---:|---:|
|确认控制锁|199082.201936 / 41394|原调用点已移出确认路径|
|确认后归还锁|85481.872493 / 24244|0.142252 / 48（既有短内存锁）|
|确认FIFO阶段|10915.244403 / 39999|141371.947264 / 39440|
|选批控制锁|9083.379438 / 2568|0.000000 / 0（本轮未观察争用不等于永远零）|
|target调用阶段|215204.562743 / 39998|211139.954814 / 39440|

原确认锁、确认FIFO和归还锁为互不重叠的阻塞点；新路径仍有真实FIFO等待，归还短锁保留原内存登记保护。该诊断证明变化不仅是删锁，但不证明Save所有尾部、CPU/磁盘竞争或消费方13秒排队已解决；没有采到的callsite不是跨负载零等待承诺。详情见profile-final-callsite-summary.json。

## 04额外失败与未验证边界

04真实进程普通通过，首轮race中恢复请求500；输出另包含主suite子进程Start读ServiceEventBridge与SetRunState清空该指针的DATA RACE，两个源码点本轮未修改。失败恢复suite卷被原测试默认清理，本轮不能事后伪造其更细内部时钟或将主suite竞态归为该500唯一根因。旧/新独立新父进程恢复单例race均通过，后续完整race也按表实记；这些PASS不代表已修复首轮竞态。旧count=2复用TestMain产生业务名称冲突为夹具FAIL，不能用于旧版产品回归结论。本轮不扩展服务启动/退出框架，未修改断言或延长超时隐去原失败，后续应以独立专项处理。

PASS限实际表中exit0的组件/协议/门禁；初始RED、构建FAIL、04首轮与无效重复夹具FAIL永久保留。子进程入口和外部依赖默认SKIP不算真实外部通过。NOT_RUN：本轮真实MySQL/Redis/07外部HTTP/WS、消费方接入/功能/Full-E4/UAT/容量、现场全部13秒/原SQL差额归因、物理断电/其他平台/长期稳态、未采集I/O/fsync/硬限流。没有改compaction或消费方target/金融规则，没有操作Bitzoom/Full-E4。

## 升级、回退与审查

已显式IndependentGroups的ReliableWriteStore只升级版本，保持原target/handler/StoreID/policy/组定义，不加配置、不改持久格式、不清pending/历史或重建卷；未启用的不转模式。停止准入并确认旧target/handler实际退出后换包，原卷保留，同代旧v1.13.4双向恢复已验证，更早终态格式门禁仍不放松。前台P99敏感消费方应保留独立角色同档对照与回退准备，不能直接以组件确认收益认证接纳SLO。

权威write-path/testing skill、同步/独立组/API兼容指南和04/07 README同步更新；go get不安装skill，消费方需按link-consumer-skill流程重新同步。当前主Agent直接审查FIFO顺序/预算、事务回滚与实际计数、生命周期、原错误归属及原卷；依项目约束没有创建Agent/Task/终端或混入其它WIP。

正式提交、main/tag/Proxy/SumDB/归档回执在证据根另取证；未实际发布前不把下一合法版本当作已可取得。原始证据根 `/private/tmp/core-independent-confirmation-convoy-20261116/`，源码/二进制绑定、全部FAIL/日志/性能/Go profiles与兼容目录均保留，后续仅在无损归档读回后回收自有关闭合成卷。

本轮正式补丁候选为 v1.13.5，发布前仍须由干净提交运行正式门禁。最终第三候选与 v1.13.4 双向原卷新 PID 恢复的实际原件为 compatibility-v3-process.json 和 compatibility-v3-results.json，均 exit=0。五轴主 Agent 审查确认原 FIFO 条数/字节/屏障、原事务身份及实际结果返回保持；性能回退与 04 首轮未消除风险不列为改善。
