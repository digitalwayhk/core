# Badger空memtable回收中断恢复修复

## 为什么此前没有修复

此前只验证旧版同样出现硬杀重开错误，便将其排除在组调度和选批修复之外；这能说明它不是那次新增回归，不能作为恢复缺陷长期不处理的理由。本专项独立定位并修复，不清消费方数据、不改其环境，也不把旧FAIL覆盖为PASS。

## 根因与准确范围

固定依赖Badger v4.9.6 / Ristretto v2.2.0。`z.MmapFile.Delete`在memtable已经flush后依次Munmap、Fd.Truncate(0)、Fd.Close、os.Remove。进程在truncate与unlink之间SIGKILL会遗留0字节.mem。`z.OpenMmapFileUsing`将已有0字节文件扩容并返回z.NewFile，Badger `openMemTable`返回该状态，而`openMemTables`把任何非nil当错误，产生`while opening memtables ... while opening fid: 1 err: Create a new file`。不是MySQL、业务handler或Core拿锁等待SQL，也没有证据将所有历史崩溃归此单一窗口。

来源为固定官方源码：[Badger memtable.go](https://github.com/dgraph-io/badger/blob/v4.9.6/memtable.go)、[Ristretto z/file.go](https://github.com/dgraph-io/ristretto/blob/v2.2.0/z/file.go)。模块缓存只读核对，未修改。

## 最小实现及安全边界

- `internal/badgeropen.Open`复用原Badger Options与打开入口；启动预检不在Save热路径。
- `recoverEmptyMemtables`先锁目录本身（与固定Badger Unix实现相同flock），只处理规范数字fid命名、普通单链接且Fstat确认0字节的.mem；openat不跟随链接，unlinkat局限在已锁目录，移除后同步目录。非空、非规范命名、符号/硬链接及其它文件不处理。
- 不删LOCK，不BypassLockGuard。预检锁释放后由Badger重新取得自己的正常锁，若其它owner取得则正常失败，不允许两个写方。
- 共享Badger、独立Badger、authstate、Badger L2只替换单一打开调用；无API/配置/格式/依赖变化。业务pending、原件、指纹、attempt、BatchID、组屏障、hook和确认原子性均保留。
- 只读、显式绕锁、内存库不预处理。未验证平台使用原Badger行为，不套用其它系统锁。修复不适用于应用直接第三方badger.Open，更不修复已被截断的非空业务WAL。

## RED与证据保留方法

真实ReliableWriteStore保存模型和有界pending、关闭留存原件，固定0字节memtable残留，首次重新打开旧源码失败。首轮夹具错误地要求带pending的Close返回nil，exit1保留但不是产品RED；按既有PendingSyncError合同修正后`deterministic-red-fixture-fixed`真实RED，生产最小修复后`deterministic-green`通过，精确核验原记录、pending=1、Ready/Attempts=0。

真实删除窗口使用**仅验收期**私有modfile与完整官方Ristretto源码副本，只在真实Truncate(0)之后、Close/Remove之前写断点并阻塞，父进程SIGKILL，新的独立PID第一次打开原目录。原卷打开前副本保留`before-reopen`，不是第二次打开恢复或同进程对象重建。活owner仍持锁时另一个进程的恢复入口必须拒绝、空文件不变。基线覆盖四个Core打开文件为v1.13.1原字节，不改模块缓存或正式go.mod，`controlled-crash-red-private-mod`真实失败于新PID首次重开。

Go1.26拒绝模块缓存文件overlay的`controlled-crash-red`为工具失败，不算语义RED。首轮`controlled-crash-green20`实际重开已成功，但夹具把SyncBatchResult结构体与整数比较，exit1保留，不声称20轮GREEN；修正为精确Confirmed=1、Remaining=0后另名重跑。所有错误/失败原件保持。

证据目录：`/private/tmp/core-badger-empty-wal-recovery-20261116/`。生产未插桩、未新增全局测试钩子；特殊子进程测试无显式环境默认SKIP，必须显式执行才算验收。

## 实际验证结果

下列命令均在Core隔离分支执行，原日志/退出码分别保存，不以重复通过覆盖失败：

| 证据名 | 实际命令 | 结果 |
|---|---|---|
| `deterministic-red-fixture-fixed` | `env CORE_EMPTY_WAL_EVIDENCE=/private/tmp/core-badger-empty-wal-recovery-20261116 go test ./pkg/persistence/database/nosql -run ^TestReliableStoreReopensZeroByteMemtable$ -v -count=1 -timeout=30s` | exit1，7.50s；{'PASS': 0, 'FAIL': 1, 'SKIP': 0} |
| `deterministic-green` | `env CORE_EMPTY_WAL_EVIDENCE=/private/tmp/core-badger-empty-wal-recovery-20261116 go test ./pkg/persistence/database/nosql -run ^TestReliableStoreReopensZeroByteMemtable$ -v -count=1 -timeout=30s` | exit0，7.10s；{'PASS': 1, 'FAIL': 0, 'SKIP': 0} |
| `controlled-crash-red-private-mod` | `env CORE_TEST_ZERO_WAL_CONTROLLED=1 CORE_EMPTY_WAL_EVIDENCE=/private/tmp/core-badger-empty-wal-recovery-20261116 go test -modfile /private/tmp/core-badger-empty-wal-recovery-20261116/checkpoint.mod -overlay /private/tmp/core-badger-empty-wal-recovery-20261116/baseline-checkpoint-overlay.json ./pkg/persistence/database/nosql -run ^TestZeroWALHardCrashRecovery$ -v -count=1 -timeout=60s` | exit1，7.53s；{'PASS': 0, 'FAIL': 1, 'SKIP': 0} |
| `controlled-crash-green20-result-fixed` | `env CORE_TEST_ZERO_WAL_CONTROLLED=1 CORE_EMPTY_WAL_EVIDENCE=/private/tmp/core-badger-empty-wal-recovery-20261116 go test -modfile /private/tmp/core-badger-empty-wal-recovery-20261116/checkpoint.mod ./pkg/persistence/database/nosql -run ^TestZeroWALHardCrashRecovery$ -v -count=20 -timeout=120s` | exit0，10.15s；{'PASS': 20, 'FAIL': 0, 'SKIP': 0} |
| `controlled-crash-race20` | `env CORE_TEST_ZERO_WAL_CONTROLLED=1 CORE_EMPTY_WAL_EVIDENCE=/private/tmp/core-badger-empty-wal-recovery-20261116 go test -race -modfile /private/tmp/core-badger-empty-wal-recovery-20261116/checkpoint.mod ./pkg/persistence/database/nosql -run ^TestZeroWALHardCrashRecovery$ -v -count=20 -timeout=180s` | exit0，40.39s；{'PASS': 20, 'FAIL': 0, 'SKIP': 0} |
| `native-legacy-handoff20` | `go test ./pkg/persistence/database/nosql -run ^TestCrashBeforeAndAfterHandoffConfirmation$ -v -count=20 -timeout=120s` | exit0，14.92s；{'PASS': 20, 'FAIL': 0, 'SKIP': 0} |
| `ordinary-all` | `go test ./internal/badgeropen ./pkg/persistence/database/nosql ./pkg/server/authstate ./pkg/server/routecache -v -count=1 -timeout=300s` | exit0，32.78s；{'PASS': 353, 'FAIL': 0, 'SKIP': 56} |
| `race-all` | `go test -race ./internal/badgeropen ./pkg/persistence/database/nosql ./pkg/server/authstate ./pkg/server/routecache -v -count=1 -timeout=300s` | exit0，64.55s；{'PASS': 353, 'FAIL': 0, 'SKIP': 56} |
| `examples-race` | `go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout=300s` | exit0，20.44s；{'PASS': 0, 'FAIL': 0, 'SKIP': 0} |
| `release-contract` | `bash scripts/test.sh release-contract` | exit0，68.63s；{'PASS': 0, 'FAIL': 0, 'SKIP': 0} |
| `linux-open-safety` | `docker run --rm --name core-badger-empty-wal-safety-20261116 --network none --cpus 1 --memory 1g --read-only --tmpfs /tmp:rw,nosuid,nodev --mount type=bind,source=/private/tmp/core-badger-empty-wal-recovery-20261116,target=/evidence,readonly --workdir /tmp --entrypoint /evidence/badgeropen-linux.test redis:7.4.12-alpine -test.v -test.count=5 -test.timeout=60s` | exit0，0.55s；{'PASS': 15, 'FAIL': 0, 'SKIP': 0} |
| `linux-controlled-crash20` | `docker run --rm --name core-badger-empty-wal-process-20261116 --network none --cpus 1 --memory 1g --read-only --tmpfs /tmp:rw,nosuid,nodev --mount type=bind,source=/private/tmp/core-badger-empty-wal-recovery-20261116,target=/evidence --workdir /tmp --env CORE_TEST_ZERO_WAL_CONTROLLED=1 --env CORE_EMPTY_WAL_EVIDENCE=/evidence/linux-volumes --entrypoint /evidence/nosql-checkpoint-linux-final.test redis:7.4.12-alpine -test.run ^TestZeroWALHardCrashRecovery$ -test.v -test.count=20 -test.timeout=120s` | exit0，2.42s；{'PASS': 20, 'FAIL': 0, 'SKIP': 0} |

Mac原生普通/race组件回归各353 PASS/56 SKIP；特殊进程入口、专用外部数据库/Redis等SKIP保留原因，不当作通过。真实删除窗口普通20轮、race20轮均新PID第一次打开原卷成功，无新Save，确认数/预算/原模型逐项核对；原无干预交接硬杀20轮通过。测试的未知提交、版本核验与实际回调退出仍沿既有全包回归，不宣称所有损坏/物理断电已经覆盖。

Linux真实运行使用本地已有镜像`redis:7.4.12-alpine`（仅作Linux二进制执行壳，没有启动Redis），镜像ID`sha256:bbce1773ddd486b4dd950e26333908b03d53ca6aad358cff728292fed58846a2`，linux/arm64。独立`--rm`容器、network none、1CPU/1GiB、只读根、仅挂本专项合成证据目录，不访问任何现有容器/业务库。原生未插断点的锁/文件保护15项通过；真实删除窗口20轮新PID恢复通过，使用上述显式断点副本，只证明对应窗口的功能正确，不是容量测试。测试容器退出已自动移除，无新镜像下载。

## 自审、兼容及未运行边界

当前主Agent直接审查，不派子Agent。重点核对：规范fid、真实目录锁而非LOCK文本、O_NOFOLLOW/单链接普通零文件、目录同步、预检与正式打开之间仍可拒绝竞争、只读/内存/绕锁不预处理、非空WAL/LSM/业务状态不修改。没有新增针对该错误的重试循环（原锁重试保留）、依赖升级、关闭SyncWrites/DetectConflicts或删除pending。只增加稳定事件`badger_empty_memtable_recovered`及count，不记录payload/凭据或动态业务身份。

API/配置/StoreID/冻结policy/终态nil Item及卷格式未变，Core标准入口默认生效，不要求业务改target、handler或新开关。回退不需要卷转换；停稳实际回调与库owner、保留卷副本后方可替换二进制。更早有界/终态格式既有门禁不变。直接第三方badger.Open、只读/绕锁及Windows/AIX/Plan9/JS/WASI等原行为未覆盖；其它BSD虽采用相同Unix实现，本轮真实运行NOT_RUN。

NOT_RUN：Windows等上述平台真实运行、物理断电/非空WAL损坏自动修复、外部MySQL/Redis/NATS/WS业务全链、Bitzoom/Full-E4升级/部署/压力/UAT/容量。Core通过不等于消费方已经接入。原历史20轮1失败/3失败事实不删除，本轮只闭环已稳定复现的零字节回收窗口。
