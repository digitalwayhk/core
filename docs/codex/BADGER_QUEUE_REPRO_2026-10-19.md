# Badger队列核对与删除复现记录

本轮依据[修复优化计划](../superpowers/plans/2026-10-19-order-queue-repair-and-throughput.md)执行。已完成独立核对、公共API与内部缺陷对照、迁移回退，并在隔离开发分支适配v4.9.6；**未证明07现场异常的根因，未调整运行参数，未通过容量目标。** 后续代码、真实恢复及性能进度见[执行记录](ORDER_QUEUE_EXECUTION_2026-10-19.md)，下文保留各阶段的原始结果与当时边界。

## 离线只读核对

新增工具`examples/07-shop-order-scale/tools/inspect-order-pending`，只导入标准库和Badger，必须指定数据格式，默认且仅允许只读打开。输出不包含订单值、用户身份或凭证。缺失、损坏或身份不匹配时退出非零，不执行修复。

```bash
rtk proxy go run ./examples/07-shop-order-scale/tools/inspect-order-pending \
  -path <已关闭数据库的副本目录> -format native

rtk proxy go run ./examples/07-shop-order-scale/tools/inspect-order-pending \
  -path <已关闭数据库的副本目录> -format core -prefix Order:
```

原生格式检查p/q双向关联及事实身份；Core格式区分已同步缓存、未同步事实和删除墓碑，不要求整个模型前缀物理为空。此工具只检查物理状态，不读取运行期缓存depth，也不替代远端逐键事实核验。

已确认上一轮任务容器停止，两份原始Badger目录没有打开文件。从`comparison-v1/{version}-long/native-pending`重新复制；未复用先前被可写扫描器打开的副本。

| 原生版本检查点 | p事实 | q索引 | 核对结果 | 原件/副本扫描前后SHA256 |
| --- | ---: | ---: | --- | --- |
| go-zero v1.10.2长窗 | 0 | 0 | PASS | 均不变 |
| go-zero v1.10.3长窗 | 1785 | 0 | FAIL：1785项fact_without_queue | 均不变 |

因此1785项缺索引的物理状态在全新只读路径仍可观察。远端已匹配的既有结果保留，不将其当作本地队列一致。负数内存depth仍来自原窗口采样，不能由离线扫描重新证明。

证据位于`.temp/order-queue-repair-20261019/`：`readonly-verification.json`、两份`*-readonly-manifest.json`及`verify_closed_copies.py`。原长窗证据、原始目录及旧可写扫描副本均保留。

## 上游内部删除复现

从module cache复制v3.2103.5与v4.9.6源码到本轮独占目录，未修改module cache、项目go.mod/go.sum或实际业务目录。将v4.9.6的[`TestDeletedKeyReappears`](https://github.com/dgraph-io/badger/blob/v4.9.6/levels_test.go)移植到v3副本，保留来源和Apache-2.0许可；v4运行其原测试。

| 场景 | v3.2103.5 | v4.9.6 |
| --- | --- | --- |
| 原上游测试，重复3次 | 3/3 FAIL：期待ErrKeyNotFound，实际Get成功 | 3/3 PASS |
| 同序列增加SyncWrites=true、DetectConflicts=true，重复3次 | 3/3 FAIL，同一删除可见性断言 | 3/3 PASS |

这是**库内部缺陷已复现**的证据。它通过managed事务、过期填充记录、16KiB基础层/8KiB基础表、禁用自动compactor及直接调用私有压缩函数，人为制造层级布局；并非07实际16MiB memtable、2个后台compactor、Snappy、无TTL的自然运行条件。因此不能据此断言它造成了现场负数depth或1785项残留，也不能跳过升级兼容和业务恢复验证。

日志：`upstream-v3-repeat.log`、`upstream-v4-repeat.log`、`upstream-v3-synced.log`、`upstream-v4-synced.log`；初次结果另外保留。两个源码副本及来源说明也在本轮证据目录。

## 本轮验证与剩余关卡

- PASS：核对器构建、健康/畸形夹具、缺失路径不创建、扫描前后哈希不变。
- PASS：核对器及原生队列包`go test -race -count=1`，包含新增静止检查点双向索引/计数契约；该契约本来通过，不冒充现场异常的RED。
- PASS：多进程测试包的定向负载守护、卷隔离、状态保留测试。真实负载两项因外部开关未启用而SKIP，不计为真实多进程或容量通过。
- NOT_RUN：同现场参数的公共API自然压缩/GC/硬杀矩阵、v3→v4数据兼容、整框架回归和新长窗。G0运行期Core缓存计数与物理事实关联尚未补齐；G0/G1整体仍为Partial。

下一项区分实验是用纯Badger公共API重放原生p/q写入与删除，在现场相同Options下记录事务成功集合、删除前后可见键及后台压缩变化，并对v3/v4分别运行。内部缺陷复现与现场可达性必须分开；在该边界收敛前不以调高阈值或清零计数推进容量结论。

## 继续执行：公共API、原生并发与迁移

新增两个独立模块`examples/07-shop-order-scale/go-zero-reference/experiments/badger-repro/{v3,v4}`，分别固定v3.2103.5与v4.9.6。操作序列、随机种子和现场参数相同，差异仅为依赖import；预期状态只在事务成功后更新，逐键比较值哈希，不用计数相等替代事实核对。人工数据不包含用户资料或凭证。

| 场景 | v3 | v4 | 证据边界 |
| --- | --- | --- | --- |
| 自然压缩3轮，分别262144、786432、786432条事实 | PASS | PASS | 未复现现场异常，不能称根因已消除 |
| Core关闭策略CompactL0OnClose=true，262144条 | PASS | PASS | 与原生默认关闭策略分开 |
| 显式Flatten，262144条 | PASS | PASS | 诊断变量，不冒充自然压缩 |
| SIGKILL后原路径重开，32768条写入、保留4096条事实 | PASS | PASS | 对剩余事实值及已删除键逐键核对 |
| 有效vlog GC，524288条写入、2048字节随机payload | PASS，100次rewrite | PASS，100次rewrite | 诊断阈值1024；前三次零rewrite结果保留，不计为有效GC |
| 原生应用4生产者、并发确认及重复确认，131072条 | PASS | 尚未切换应用依赖 | 静止点depth、p、q均0，重开后仍0 |
| Core运行期缓存/索引/事实/墓碑三方核对 | PASS | 尚未切换项目依赖 | 保留合法已同步缓存，不改生产算法 |

证据：`public-matrix.json`、`public-*-natural-r1.jsonl`、`public-*-gc-r4.jsonl`、`application-stress-v3.log`及`core-invariant.log`。GC轮次为产生真实可回收统计单独持久化head并触发重叠SST压缩；这些条件和更低值阈值均在程序及Options输出中明确，不扩大为07现场条件。

普通及加密人工夹具均完成v3物理副本→v4直接打开、v3 Backup→v4 Load、v4新增事实后Backup→v3新目录Load及再次重开。750份最新值的版本/内容摘要一致，250份删除键不出现，过期记录不可见，活跃TTL元数据保持，升级后的新事实可回退保全。仅证明这些固定版本和夹具；不得自动将v4写过的实际目录交给v3打开。

加密首轮因未设置IndexCache在flush时panic，已保留失败记录；按官方要求设置16MiB IndexCache后在全新目录重跑通过。此为诊断夹具配置问题，未据此修改未启用加密的Core配置。证据`compat-results.json`与`compat-encrypted-r2-results.json`。

**决策边界：** 现场同参数与原生应用尚未复现负数depth/不可达p；仍为NOT_REPRODUCED。已确认v3库内部删除缺陷，v4通过其固定回归，兼容/备份回退验证允许继续在当前隔离开发分支评估v4适配。升级收益须由Core各使用点、真实恢复及新负载验证；不能将其预先称为现场异常根因修复。
