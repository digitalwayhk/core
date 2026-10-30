# 有界 write-behind 本地确认批提交实施与验收

本任务由主 Agent 直接执行，不调用子 Agent。基线 v1.6.1 / 46b7551；原有 WIP 保留；不操作消费方或远端。

- [x] 核对前台保存、选批、target、确认、决定、清理全部写方。以 Badger MaxVersion 固定逐条可靠提交放大 RED。
- [x] 增加 Core 自身可重复性能场景：相同机器/APFS、SyncWrites=true、50并发、2028字节合成Gob、三模式、三轮2000条及持续窗口。修复前先运行并保存原件。
- [x] processDispositionRound 的结果落地使用有界块（ReliableWriteStore固定8项，直接PrefixedBadgerDB最多32项，编码软预算1MiB）；Badger实际 ErrTxnTooBig 再二分。逐项语义失效隔离，真实事务失败整块不计数/清理，已提交块独立计数。
- [x] 复用原 prefix 元数据锁，SQL/handler 在锁外。增加既有 SyncMetrics 的低基数前台/确认/其他事务与等待计数，区分target时间；不增加业务队列或线程。
- [x] 回归部分成功/未知、重复/旧版本/指纹、事务失败/过大与部分块、前后台公平与停流排空、全部既有清理边界。扩展原进程测试，在确认前、确认后清理前、提交结果未返回时硬杀并由新进程恢复。
- [x] 同档性能复跑；若仍有明显争用，继续在现有提交面实现公平预算，不把等待转到无限chan。诚实记录未测同步系统调用/硬限流指标，不填零。
- [x] nosql与04/07普通/race；persistence/config/performance/release/日志门禁；更新正式写路径、有界处置、清理指南、示例及skill；精确提交白名单。
- [x] 发布流程准备：main合并、干净精确检出门禁、新补丁标签不覆盖、核对远端main/tag/官方Go代理包及封存证据；实际发布SHA/退出码以后续Release附件为准。

确认成功、物理清理、业务成功是不同边界；不关闭可靠刷盘、不清历史、不修改存储格式或业务协议。Full-E4同档复测与业务UAT/容量始终NOT_RUN。

性能对照与已知限制已交付：持续P99仍60–85ms，不能将执行完成等同全部性能目标通过。正式合并/发布的实际状态以Release附件为准；Full-E4始终NOT_RUN。
