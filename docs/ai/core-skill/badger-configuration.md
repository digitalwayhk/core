# Badger配置、可靠性与调参

用于可靠pending、共享本地库、路由缓存和authstate的配置审查。**官方默认、Core工厂默认、服务覆盖和实际生效Options必须分开记录；没有跨场景“最优配置”。** 版本升级与参数实验分开，最终以当前实现和实测为准。

## 可靠性先于吞吐

- 可靠pending是远端确认前的业务事实：必须`SyncWrites=true`、`DetectConflicts=true`、`CorruptionPolicyFail`，禁止TTL；本地提交成功后才确认接单。不能用PeriodicSync替代每次可靠确认，也不能用Fast配置追求容量通过。
- `DefaultSharedConfig`面向共享缓存，当前`SyncWrites=false`；不能直接作为可靠写配置。`ReliableWriteStoreConfig.normalized`在未指定Badger模式时选`DefaultProductionConfig`，`UseWriteBehind`另行校验可靠性约束。
- 同一物理目录只有一个Badger owner，prefix共享的是同一个manager/Options；首个打开者确定配置，不是每个模型可各自生效。多个实例应有独占持久路径，不能用删除LOCK、BypassLockGuard或清库解决竞争。
- authstate不能一律当可重建缓存；业务pending或安全状态损坏时保留原卷。只有明确可重建的缓存才可显式使用reset_cache。

## 当前值及来源

当前后端适配Badger v4.9.6，独立复现模块保留v3.2103.5作对照。下面列出的原生默认值在这两个固定版本中相同，并非建议所有服务照抄；升级仍须重新导出有效Options，不能从注释推定。

从 v1.3.0 升级需按[消费方指南](../../codex/UPGRADE_FROM_V1_3.md#badger-数据与错误兼容)核对旧目录副本和回退。Core 方法签名保持，但底层错误来自 Badger v4；直接用 v3 sentinel 比较错误的项目须调整并回归，不能只用编译通过证明行为兼容。

| 参数 | Badger DefaultOptions | Core Production / Shared | 07原生对照 |
| --- | --- | --- | --- |
| SyncWrites | false | true / false | true |
| DetectConflicts | true | true / true | true |
| MemTableSize | 64MiB | 16MiB / 16MiB | 16MiB |
| NumMemtables | 5 | 未覆盖，继承5 | 继承5 |
| NumCompactors | 4 | 2 / 2 | 2 |
| NumLevelZeroTables / Stall | 5 / 15 | 4 / 8 | 4 / 8 |
| ValueLogFileSize | 2^30-1字节 | 1MiB / 1MiB | 1MiB |
| ValueThreshold | 1MiB | 64KiB / 64KiB | 64KiB |
| BlockCacheSize | 256MiB | 未覆盖 | 未覆盖 |
| IndexCacheSize | 0（不设淘汰预算，索引保留在内存） | 未覆盖 | 未覆盖 |
| CompactL0OnClose | false | manager显式true | 继承false |
| 应用调用value-log GC | 调用方负责 | 5分钟 / 10分钟，ratio0.5，每次最多10轮 | 当前未注册此循环 |

来源：`pkg/persistence/database/nosql/{badgerdbconfig.go,sharedbadgermanager.go,reliable_write_store_config.go}`、07原生`pending.go`；[官方v3 Options](https://github.com/dgraph-io/badger/blob/v3.2103.5/options.go)、[官方v4 Options](https://github.com/dgraph-io/badger/blob/v4.9.6/options.go)。官方部分setter注释与实际DefaultOptions构造值有出入，必须读固定版本源码/导出配置，不能复制注释中的过期数字。

Core目前未暴露NumMemtables、BlockCacheSize、IndexCacheSize等字段，不能在服务JSON中虚构这些参数。确需配置时，先增加有边界校验的配置字段、Options接线和实际生效测试，再写用法；不能只增加文档或结构体字段。SyncMaxConcurrency与AutoCleanup等已有预留字段的未接线边界以`BadgerDBConfig`为准。

可靠写配置优先从`DefaultProductionConfig`构造后逐项覆盖。注意`ReliableWriteStoreConfig.normalized`在`Badger.Mode`为空时会替换整份Badger配置，单独填MemTableSize等字段并不会保留；不能把“已传入”当作“已生效”。当前可靠写参数作为对照基线保留，下面候选用于独立实验，不是已经批准替换的生产默认值。

## 参数如何选择

**先记录条件：** 每个物理库的并发、唯一键数、值大小p50/p95/p99、更新/删除比例、LSM/vlog尺寸、磁盘类型/空间、进程与容器内存、允许恢复时间、可靠性要求。吞吐数字必须同时报告失败率、p99和积压年龄；不能仅调内存参数而不核对数据正确性。

| 参数 | 取舍 | 07下一步候选，尚未验证最优 |
| --- | --- | --- |
| MemTableSize | 更大可减少flush频率，但增大内存与重开恢复工作 | 固定其他值比较16→32→64MiB |
| NumCompactors | 提高可增加压缩能力，也争用CPU与磁盘 | 比较2→4；不关闭压缩制造吞吐优势 |
| L0表/阻塞阈值 | 表积压会影响读放大与写停顿，调高只延后压力 | 先测L0增长/停顿，再单独比较4/8与5/15 |
| ValueLogFileSize | 实际有vlog流量时，小文件可能增加轮换/回收频率；大文件占用与回收粒度更大 | 有实测vlog压力才比较1→64→128MiB，不直接认定1MiB是瓶颈 |
| ValueThreshold | 小于阈值的值内联LSM，否则进入vlog；影响写放大、随机读和GC | 按真实值大小比较1/4/64KiB及1MiB；小值都内联时，调大阈值可能根本不改变路径 |
| Block/Index缓存 | 缓存减少读取/解压，但与memtable、索引及业务堆共同占内存 | 先导出实际值；保持压缩/加密时不能随意禁用所需block cache |
| GC周期/ratio | GC要有可回收的vlog文件，并与前台争用I/O | 先确认实际执行、回收字节和停顿；ErrNoRewrite不是发生了有效回收 |
| CompactL0OnClose | 影响关闭延迟和关闭后布局，是恢复测试的重要变量 | 保留原控制组，分别核对关闭前后；不能当删除复现的绕过开关 |

总内存预算应包含`MemTableSize × NumMemtables`的缓冲上界、block cache预算、索引/bloom、压缩工作区、mmap驻留和业务堆；预算不等于即时分配或精确RSS。按每个物理库/进程累计，不能因memtable仅16MiB就宣称低内存。`IndexCacheSize=0`与`BlockCacheSize=0`的语义不同，不是两个都禁用缓存。启用Badger原生加密时必须显式配置非零IndexCache，否则可在flush建表时panic；16MiB仅为已验证的迁移夹具取值，实际预算仍需按索引规模选择。

[官方使用说明](https://github.com/dgraph-io/badger/blob/v4.9.6/docs/quickstart.md)解释事务、值生命周期、备份与内存取舍。Core的512条/32阈值/20ms以及128条/1ms分别属于远端同步和本地合批，不是Badger原生参数，见[写路径](write-path-and-performance.md)，不能混成一组官方推荐值。

## 固定流程与验收

1. 先封存版本、服务覆盖值和最终Options；相同业务的对照额外列出GC、关闭压缩、数据布局和日志差异。
2. 可靠性异常先最小复现。统计计数、索引和事实分开核对；只读打开关闭后的副本，禁止核对器默认可写打开再将结果称为原件状态。
3. 一次只改变一个参数或一个依赖主版本，保留原控制组；收益与本机波动相当则不改默认。
4. 检查fsync、并发冲突、事务失败保留、重复ACK、关闭/硬杀/重开、压缩/GC后删除不复现，以及已确认事实逐键匹配。计数截零、关闭compactor/GC、清理旧数据都不是正确性修复。
5. 短窗通过后再做长窗，检查积压增长、回收与恢复耗时。参数不证明根因，能编译不证明主版本数据兼容。v3→v4只对副本做直接打开及Backup/Load实验，实际目录不能自动降级或reset。

已观察到删除复现相似症状时，检查[Badger #2278](https://github.com/dgraph-io/badger/pull/2278)、[#2310](https://github.com/dgraph-io/badger/pull/2310)与当前版本是否命中；不能仅凭上游修复就归因本项目，也不能以v4.0未改格式推导最新版本全兼容。最终参数只在有源码指纹、完整数据核对和稳定多轮证据后登记为已验证配置。
