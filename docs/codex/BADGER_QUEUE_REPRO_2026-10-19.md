# Badger队列核对与删除复现记录

本轮依据[修复优化计划](../superpowers/plans/2026-10-19-order-queue-repair-and-throughput.md)执行。当前只完成离线核对工具、基本契约回归和上游内部缺陷验证；**未证明07现场异常的根因，未修改Badger版本或运行参数，未通过容量目标。**

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
