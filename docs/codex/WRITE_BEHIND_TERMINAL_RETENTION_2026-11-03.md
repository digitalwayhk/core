# 可靠写终态快照回收验收记录

发布版本：v1.9.0（加性API按MINOR）。独立修复提交：`42895fef467643946a77ca50783fd157c07af086`。生产实现、普通/race、真实旧卷独立进程恢复及候选门禁完成；main/tag与默认Go代理/SumDB的发布后精确回执由正式Release交付，不将Core通过当消费方已升级。

基线为正式v1.8.1，提交eb9fc7912ecff69490f7e4919fe09ad5ef9534f1；独立修复分支fix/writebehind-terminal-retention，既有工作区起始clean，Pebble和其他工作区未改。

任务原件`/tmp/bitzoom-core-terminal-snapshot-retention-request-20261103/Core修复任务.zh-CN.md`，7054bytes，SHA256 fdfcdb9c4051d2ccfb14c2d7a957e101d09e5abe6dd157b32f268fc7223c7edc。消费方旧副本约98%可见值为完整终态快照的统计只是此前隔离副本结果，不代表当前16GiB现场构成；未重扫或操作现场。

## 根因与实现

原ensureDispositionEntry每版本复制完整SyncQueueItem，saveDisposition在终态只删除同步/组/target索引，始终保存r:ID；原业务IsSyncAfterDelete只删除已确认模型。历史b:BatchID也永久存在。Badger不能将仍存活的key当垃圾回收，pending=0不代表历史或物理文件已排空。

当前saveDisposition经terminalDispositionForStorage只瘦身持久副本，保留提交后hook原快照；显式完整审计由持久条数/字节预算准入。前序引用与历次批次成员引用原子保存；安全回收将后继接回更老责任，批次全终态且原回调退出才退休、逐项归还引用。新版本压缩已解除前序但不跳过更老未解决责任；旧回执不能确认新值。

initializeTerminalHistory/initializeTerminalBatches在绑定前分页导入旧卷引用并持久检查点，旧policy指纹不变、格式门禁单向转换。CompactWriteBehindHistory复用原worker/提交器，块最多8项并TooBig二分；业务原件和未解决记录不回收。ReclaimStorage仅复用Badger原生有限GC。完整预算/API/迁移与回退边界见[指南](WRITE_BEHIND_TERMINAL_RETENTION_GUIDE.md)。

## 已保留的RED与失败

- red.log：真实可靠store，成功后pending=0但终态Item仍存在，旧默认两个分支均FAIL。
- performance-before.log：首轮夹具单并发准入导致大量拒绝，FAIL，不作性能收益依据。
- green-initial.log：包内duration min遮蔽造成编译失败，属于SETUP_FAIL，不算RED/GREEN。
- nosql-initial.log：旧测试继续解引用终态完整Item，与本次新合同冲突；改为核验原指纹/版本，未解决原件断言不改。
- nosql-r2.log：终态写集变小后不再触发原TooBig夹具；显式有限审计恢复原大写集，真实ErrTxnTooBig及拆分断言仍保留。
- unrelated-handler-red.log→unrelated-handler-green.log：候选全store活跃回调保护阻塞健康组回收的真实RED→按关联引用保护GREEN。
- native-gc-red.log→native-gc-green.log：缺少显式有界物理GC入口的能力RED→真实Badger待同步原件保全GREEN。

## 验证记录

以下命令均由rtk proxy执行，退出码为0；完整输出、PID及命令在原件中，不以零测试或编译替代行为验收。

```bash
CORE_TERMINAL_OLD_BINARY="$PWD/.temp/writebehind-terminal-retention-20261103/core-v181-nosql.test" go test ./pkg/persistence/database/nosql -count=1 -timeout 300s
CORE_TERMINAL_OLD_BINARY="$PWD/.temp/writebehind-terminal-retention-20261103/core-v181-nosql.test" go test -race ./pkg/persistence/database/nosql -count=1 -timeout 300s
go test ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s
go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s
./scripts/test.sh persistence-unit
./scripts/test.sh config-contract
./scripts/test.sh performance-contract
./scripts/test.sh release-contract
./scripts/check-ai-skill.sh
./scripts/check-web-dist-sync.sh
./scripts/check-logging.sh
go vet ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/...
```

- 完整nosql普通25.803s、race45.700s；新增绑定前分类/真实物理字节与三处硬杀定向race6.501s。后续纯夹具/文档变化不冒充生产算法变更。
- 04/07普通15.233s、race16.801s；持久化32.771s、配置7.799s、性能契约6.375s、发布契约47.133s。performance-contract中的router/cluster/event/types部分是COMPILE_ONLY；不能当那些服务已作完整行为回归。
- 精确v1.8.1源码独立PID建卷，分别在记录导入、批次引用导入、真实终态删除提交后SIGKILL，再由第三进程重开原目录；恢复后旧v1.8.1进程绑定拒绝。共三种断点，每种四个独立PID。原件quantitative-process-race.log保留实际PID及分类字节。
- 另用三个真实进程执行当前格式seed/update/verify，原业务副本、跨组替换及轻量历史断言通过；这是同代格式，不能推导旧程序可回开。
- Core本地Git对象逐项SHA确认：v1.6.4与v1.8.1八个指定存储实现相同；不是消费方实际卷验收。原件v164-v181-source-comparison.json。

## 持续新增饥饿的第二轮RED

自审发现仅把完整终态载荷缩小还不够：顺序游标在持续新增时可永远追赶新项，身后已到期元数据无法再次核验。sweep-starvation-red.log为首轮夹具编译SETUP_FAIL；sweep-starvation-red-r2.log为真实Badger行为FAIL。最终按HistoryAt到期索引定位，未到期不扫描、受保护项同事务延期，due-index-green/due-index-safety为GREEN；不新增业务队列或worker。旧卷导入仍用可恢复分页检查点，原业务选批cursor不变。

## 存量分类与物理边界

一次记录导入中断后的真实恢复样本：

| 项目 | 重绑定前 | 绑定后 | 安全回收后 |
| --- | ---: | ---: | ---: |
| 业务副本数 | 34 | 34 | 34 |
| 业务key/value编码字节 | 17,686 | 17,686 | 17,686 |
| 未解决数/编码字节 | 2 / 2,506 | 2 / 2,506 | 2 / 2,506 |
| pending索引数 | 2 | 2 | 2 |
| 终态记录数 | 96 | 96 | 0 |
| 完整终态Item编码字节 | 20,940 | 0 | 0 |
| 终态其余元数据字节 | 96,672 | 102,216 | 0 |

此样本绑定前已有部分导入提交，不能把20,940写成全旧卷构成。原业务副本hook=false，故34个副本必须仍在；其他普通/有界手动/自动、target/应用确认以及false→true/旧版本/关闭回收由原测试同时覆盖。

这份小卷原生GC实际Attempts=1、RewrittenFiles=0、NoRewrite=true，物理文件逻辑字节36,780,666→36,780,666，**没有立即缩小**。这是同一卷的实际结果；不能用逻辑已回收、pending=0、Native NoRewrite或后述新建库对照宣称现场16GiB准入已恢复。

## 同条件性能与保留量

同机APFS、Go1.26.6 darwin/arm64、GOMAXPROCS=1；没有施加1CPU/1GiB cgroup限制。SyncWrites=true、DetectConflicts=true；相同DefaultProductionConfig、128批上限、1ms合批、队列1024、准入128/2s。模型含固定种子生成的1024个2028字节ASCII载荷池，准备在计时外；target不访问SQL，立即返回真实选择集合ConfirmedKeys。原生Options另以相同构造配置重建实测，旧新相同：MemTable16MiB×5、vlog1MiB、ValueThreshold64KiB、BlockCache256MiB、IndexCache0、compactors2、L0 4/8、compression代码1。该观测未改压测配置，也不代表消费方4KiB实验Options。

### 三模式各三轮短饱和窗口

每轮持续3秒、50个调用者；两个无后台组的pending会保留，**不视为同步收敛**。每轮完整平均/P50/P95/P99/max、吞吐、队列/本地阶段、保留量及CPU/GC见performance-summary.json，不筛掉冷轮。

| 模式 | 旧Save平均ms范围 | 新范围 | 旧P99ms范围 | 新范围 |
| --- | ---: | ---: | ---: | ---: |
| 普通可靠写、无后台 | 2.77–2.96 | 2.78–2.87 | 18.94–21.93 | 17.84–23.38 |
| 有界、无后台 | 5.21–5.39 | 5.30–6.18 | 28.30–30.80 | 30.21–41.07 |
| 有界、空target后台 | 6.13–7.80 | 6.15–8.19 | 53.23–100.53 | 51.22–115.94 |

所有短窗错误为0；有后台组最终pending=0。短窗P99有差轮，未证明所有长尾改善，不能据此承诺15ms或任何机器的业务SLO。

### 固定65000条、130秒、500/s目标、每次50条突发

两侧固定逻辑数量65000、错误0、输入末pending50、排空后0，业务副本0，未解决字节0。吞吐由既定输入控制，不能当峰值TPS提升。

| 指标 | v1.8.1 | 候选 |
| --- | ---: | ---: |
| Save平均ms | 16.172682 | 14.029743 |
| P50ms | 17.077959 | 13.981583 |
| P95ms | 22.941292 | 22.980375 |
| P99ms | 50.765583 | 42.186875 |
| 最大ms | 106.607083 | 109.815000 |
| 实际输入完成/s | 500.345420 | 500.298763 |
| 输入结束后排空s | 0.006503 | 0.013206 |
| 终态记录数 | 65,000 | 30,400 |
| 保留批次数 | 1,302 | 610 |
| 终态Item编码字节 | 145,778,538 | 0 |
| 终态其余value字节 | 57,165,284 | 29,081,029 |
| 全部物理文件逻辑字节 | 266,427,131 | 121,908,268 |
| 文件系统分配字节 | 239,267,840 | 103,497,728 |
| 进程CPU累计秒（不是壁钟） | 40.675800 | 41.014589 |
| 窗口结束HeapInuse字节 | 744,071,168 | 1,085,595,648 |
| 窗口GC次数 | 49 | 47 |

- 旧65000个成功版本均保留完整载荷；新为0，轻量记录30400约为一分钟诊断窗口，已过期记录及批次持续回收。持续新增的稳定范围断言与独立进程恢复同时通过，不能只用pending=0证明这一点。
- 末尾物理分配来自两个独立新库的同输入对照，不是把同一个16GiB现场卷回收了这些字节。Badger原生Size有采样延迟，JSON同时保留Size及实际文件逻辑/分配口径，不混用。
- HeapInuse单点候选更高，不代表RSS或稳态存活堆，也不能宣称内存改善；峰值RSS/长期内存预算、cgroup限流、I/O字节及fsync系统调用数未采集，JSON对应项为null/NOT_RUN。不能相加CPU累计、本地阶段累计或嵌套时间冒充单笔Save壁钟。
- 同档ForegroundLocal/ConfirmationLocal/MetadataLocal的次数、排队/锁等待/事务累计及TargetDuration全量保留。快照取输入结束，因此确认Items为64950，剩余50条在独立排空后完成；不误报漏确认。

### 已失败的饱和持续窗口

此前无节流65秒，旧版和早期候选均未在原30秒排空预算内完成，FAIL原件before-continuous.log/after-continuous.log保留；旧版关闭时仍有176909 pending。初始夹具在失败断言处退出，完整分位数未落盘，不能补造；测试临时卷未封存，该轮不是完整恢复/容量证据。最终夹具已改为先记录指标再保留原失败断言，后续固定输入持续对照与它分开，未以降低输入覆盖FAIL。

最终候选未重复65秒无节流容量档，记NOT_RUN，不能据固定500/s窗宣称超载问题、任意输入资源有界或Full-E4容量已解决。

## 审查结论与剩余边界

本仓库禁止子Agent，由当前主Agent按正确性、简单性、架构、安全、性能五轴审阅。关键确认：未解决不进入gc索引；原指纹/版本/更老前序保留；每次批次成员引用都退休后才删；错误事务不发布计数；只复制持久化快照，原hook不被置nil；无新业务worker、消息队列、数据引擎或依赖升级。scope/目录/StoreID错误不能靠空库排空，运维须用原模型T和原配置。

- PASS：原始RED→GREEN、默认/显式审计预算及取消配置重开、批次/前序/迟到决定/未解决保全、原删除hook全部边界、自动无新Save维护、持续新增不饥饿、真实三处SIGKILL及新PID恢复、旧程序拒绝、新格式三进程兼容、相关普通/race及候选门禁。
- FAIL保留：夹具准入错误、编译SETUP_FAIL、旧永久载荷断言冲突、候选无关handler屏障/游标饥饿、旧/早期候选饱和窗口。均未覆盖为PASS。
- SKIP：辅助子进程测试默认仅在父进程驱动；真实性由显式CORE_TERMINAL_OLD_BINARY运行证明。独立可选外部测试没有以SKIP冒充通过。
- NOT_RUN：Bitzoom/Full-E4升级、真实16GiB卷/物理阈值恢复、消费方MySQL/业务幂等对账、UAT、物理断电、长期容量/内存及生产迁移；本轮使用真实Badger与既有独立进程fixture，不把空target当真实MySQL。

## 证据封存

[原始证据归档](evidence/writebehind-terminal-retention-20261103/raw-evidence.tar.gz)：228,724 bytes，SHA256 `49c6a4f9d611c076f42c941e06c34442615c76d6a0ed89904f0fa247ea950dc5`。含67份原件、6,588,065原始字节及逐文件raw-manifest，保留RED/FAIL/SKIP/实际命令、源码/二进制绑定与性能JSON。未含消费方数据、密钥、二进制或测试大卷。

源码修复提交为`42895fef467643946a77ca50783fd157c07af086`；最终main/tag及默认Go代理/SumDB回执在正式交付时核验；原件工作目录为`.temp/writebehind-terminal-retention-20261103/`。升级/API/单向兼容边界以[正式指南](WRITE_BEHIND_TERMINAL_RETENTION_GUIDE.md)为准。
