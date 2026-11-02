# v1.9.1首次迁移空组重复核验修复

## 结论与根因

有界失败模式的终态首次导入每8条提交并保存检查点。`initializeTerminalRecordBlock`在每个实际事务内收集受影响组，`saveDisposition`把终态的组加入集合，最后调用`deleteEmptyDispositionGroups`。集合不能跨事务复用，所以同一应用组的大量历史会跨块重复进入核验。旧函数即使`g:<group>`早已不存在，也建立组队列迭代器、Seek并在空组时再次写删除标记。

这不是持锁等待SQL，也不是应用必须重新定义Group。已有精确Prefix限制范围，但Badger的`PrefetchValues=false`仍需解析可见性：合并各层、跳过组内旧版本和删除标记，直到找到可见项或结束。它不意味着每次完整扫描全库，也不保证空组查询为常数成本。消费方CPU证据支持该窗口的主要CPU热点，不能把包含占比相加或换成整个迁移墙钟，更不能据此把全部现场时长归为fsync。

唯一生产改动位于`pkg/persistence/database/nosql/writebehind_disposition_store.go`的`deleteEmptyDispositionGroups`：在当前事务最终写集内先`txn.Get(g:<group>)`，仅`ErrKeyNotFound`免除无用Seek和重复删除；其他错误原样返回。标记存在时仍新建精确Prefix迭代器，只有实际空组才删标记。没有跨事务缓存、组计数、新提交器、业务分组特例或配置调整。

## 正确性与兼容

- 版本、指纹、原件、前序、组首、attempt、批次引用、未解决pending和删除hook不变。
- 当前事务点读能看到pendingWrites中的删除和重加；不能复用旧迭代器或把不存在缓存到下一事务。
- Badger快照缺失点读会登记读依赖，并发已提交的新组标记导致真实ErrConflict。命中本事务已有Delete的Get提前返回，不额外登记读依赖；原storageMu及BatchCommitter仍须保留，不允许绕过它们改技术索引。
- 缺标记而队列尚存时，原函数也不补标记；补丁不借此推断业务成功或伪称修复损坏，原bootstrap恢复/拒绝边界保持。
- 太大二分及回滚没有跨事务判定；检查点仍与实际记录块同事务提交，不提前推进。
- 无公开API、配置、StoreID/policy或卷格式变化。`terminal-history-v1:`、原8条边界、记录和批次迁移检查点保持。v1.9.0部分迁移卷可正常独占重开后续跑，不重建卷/组或清cursor。
- 已验证原v1.9.0也能复开候选后的同代格式，但原版仍有性能问题。v1.8.1拒绝新格式的单向迁移边界不变，不能删mode回退。没有操作消费方或替其升级。

## RED→GREEN

基线为正式v1.9.0，提交`80fad643c571ea08688cc4acb378874e13d990f7`。既有修复分支工作树在开始时干净；没有新开代理、终端、分支或worktree，其他工作保持。

真实临时Badger、正式Save/target确认，生成48条持久非空应用组的多版本终态，再跨6个8条事务块重核：

| 场景 | 原版RED | 候选GREEN |
| --- | ---: | ---: |
| 组标记原本不存在，实际Iterator/Seek | 6，期望0，FAIL | 0，PASS |
| 首块清掉旧组标记，实际Iterator/Seek | 6，期望1，FAIL | 1，PASS |
| 快照缺g后另一事务提交新g | 原事务成功，期望冲突，FAIL | ErrConflict，PASS |

RED为真实断言失败，非编译失败或零测试。原件`red.log`和退出码1保留，未覆盖为PASS。GREEN还覆盖有标记+活跃/暂停/未知同组原件、attempt不变、同事务删除重加、缺g但q存在不被删除、真实事务回滚及Get错误传播。没有降低断言。

实际命令（均经RTK执行；环境中的二进制由对应精确正式tag源码构建，测试夹具另行注入，不修改模块缓存）：

```bash
rtk proxy go test ./pkg/persistence/database/nosql -run '^(TestTerminalMigrationSkipsMissingGroupRepeatedSeek|TestEmptyGroupSnapshotMissConflictsWithConcurrentMarker)$' -v -count=1 -timeout 60s
rtk proxy go test ./pkg/persistence/database/nosql -run '^(TestTerminalMigration|TestEmptyGroup(Marker|Snapshot))' -v -count=1 -timeout 60s
rtk proxy env CORE_TERMINAL_OLD_BINARY=<v1.8.1测试二进制> CORE_EMPTY_GROUP_BASELINE_BINARY=<v1.9.0测试二进制> go test ./pkg/persistence/database/nosql -v -count=1 -timeout 300s
rtk proxy env CORE_TERMINAL_OLD_BINARY=<v1.8.1测试二进制> CORE_EMPTY_GROUP_BASELINE_BINARY=<v1.9.0测试二进制> go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout 300s
```

定向GREEN退出0。全包普通28.651秒、race51.735秒，退出0；包内已有部分确认、有限重试、TooBig/连续提交前缀、旧结果/并发新版本、暂停/接管、清理及关闭等回归一起运行。保留预期pending导致的保护性关闭日志，不把它抹成业务排空成功。

## 真实进程恢复

新增进程测试复用已有Go测试子进程模式。精确v1.9.0源码加同一夹具构建独立原版二进制，实际建卷并保存原暂停责任；候选在第一记录块真实提交检查点之后SIGKILL，另一个新PID重开同目录续跑，再以原版新PID验证同代格式复开。候选验证原暂停项、attempt=2、pending=1、组首/原件保持，并保存无关组后真实确认前进。

| 回归 | 普通PID顺序 | race PID顺序 | 结果 |
| --- | --- | --- | --- |
| 原版建卷→候选硬杀→候选恢复→原版复开 | 75000→75044→75045→75046 | 77714→77728→77729→77795 | PASS |

已有真实v1.8.1旧卷测试也实际启用：记录导入、批次引用导入、回收提交后三处硬杀，新PID原卷恢复，旧二进制因格式门禁拒绝；普通与race都PASS。不是同进程对象重建，不把默认SKIP的夹具当已运行。

## 同机三轮迁移对照

同一APFS文件系统、Go1.26.6 darwin/arm64、GOMAXPROCS=1、SyncWrites=true、DetectConflicts=true、ValueThreshold=65536，原批提交128/CollectBacklog1/1ms/Queue1024，2048业务记录/4096终态，同一非空应用组。使用正式Save及立即成功target生成版本和q删除，准备期不计入迁移计时。固定旧只读快照使组内MVCC/删除历史在测试期间保持，避免原生压缩差异提前消除负载；这不是消费方接入要求。

先在未改生产源码时构建原版测试二进制、完成基线，再构建候选。同一夹具只在测试中观察现有groupCheckObserver，不改生产时钟或采样框架。

| 轮次 | 原版/候选Seek | 原版墙钟(s) | 候选墙钟(s) | 原版CPU累计(s) | 候选CPU累计(s) |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | 512 / 0 | 1.037193 | 0.360153 | 0.970871 | 0.297459 |
| 2 | 512 / 0 | 1.020614 | 0.370998 | 0.970735 | 0.324367 |
| 3 | 512 / 0 | 1.023527 | 0.320412 | 0.965245 | 0.280476 |

每轮两侧MetadataLocal提交都为2562次、errors=0、pending=0，业务副本2048/终态4096保持。没有减少可靠事务数或8条边界。包含512个记录块与原批次引用导入，剩余提交、编码、点读和刷盘成本仍在。

每轮原版/候选累计事务耗时分别为0.978/0.300、0.950/0.305、0.949/0.261秒；累计队列等待各约0.0010–0.0011秒，锁等待各约0.00011–0.00013秒。它们有嵌套关系，不相加冒充墙钟。

CPU原件也保留。第一轮原版profile共930ms采样，其中Iterator.Seek包含140ms（15.05%），同时Msync累计710ms（76.34%）；候选共300ms采样，主要剩余Msync230ms（76.67%）。该小fixture没有复现消费方83.26%占比，不把两者混成同一证据；原profile还覆盖计时之后的只读Usage核验尾段，不能当作纯迁移函数采样。补丁同时省掉无效删除墓碑，其写集变化也会影响本地同步成本，不能把所有墙钟收益都归为Seek CPU。未在候选top表出现的函数不宣称绝对零CPU。

命令：

```bash
rtk proxy env CORE_EMPTY_GROUP_PERF=1 CORE_EMPTY_GROUP_PROFILE_DIR=<隔离输出目录> <原版或候选测试二进制> -test.run='^TestEmptyGroupMigrationPerformance$' -test.v -test.timeout=300s
rtk proxy go tool pprof -top -cum -nodecount=20 <对应测试二进制> <对应CPU原件>
```

这是组件迁移对照，无前台Save并发容量输入，不报告不存在的Save P50/P95/P99或峰值TPS。物理I/O字节、fsync调用数、RSS与cgroup限流未采集，保留null/NOT_RUN。没有1CPU/1GiB容器硬限，也没有复跑现场两百万终态或给出ETA；有标记且真正含未解决组首的范围核验仍可能昂贵。

## 消费方升级与剩余边界

```bash
rtk proxy go get github.com/digitalwayhk/core@v1.9.1
rtk proxy ./scripts/link-consumer-skill.sh --target <消费方目录>
```

第一条在消费方由其执行方运行；第二条从Core正式源码按原skill安装指南执行，按脚本实际参数使用，go get不会自动安装skill。不需要新增开关或修改Group/target/handler。已部分迁移卷保持原模型、实例目录、身份、prefix、StoreID/policy及原检查点，在正常独占重开时续跑，不能并行开卷、删标记或清数据。

现有绑定API没有context参数，迁移期间持closeMu读锁，Close也不是可依赖的中止入口；外层ctx超时或SSH结束不表示绑定进程退出。无信号关闭处理的SIGTERM不能算优雅Close；先证明实际进程已退出、卷独占，才能重开续检查点。本轮只验证指定提交后硬杀点，不认证任意native提交/压缩中断。历史选批报告中各20轮有3次打开恢复失败的证据仍保留，此补丁没有修复Badger打开恢复，不能因本轮通过宣称风险消失。

Full-E4原卷维护、物理释放、业务功能/UAT、百万规模时限及容量均为NOT_RUN，由消费方独立执行。本修复不保证任意机器Save P99、迁移完成时间或物理磁盘释放量；未提供公开绑定中进度API，不重写8条串行提交。

正式门禁结果与证据清单在交付时随下节及版本附件记录。所有公开日志副本去除机器绝对路径，原始FAIL/CPU等保留在Core本地；不公开业务原件、凭据或现场私有调用图。


## 门禁、审阅与证据

| 实际命令/检查 | 退出码 | 端到端耗时(s) |
| --- | ---: | ---: |
| `go test ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s` | 0 | 14.169 |
| `go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s` | 0 | 17.716 |
| `./scripts/test.sh persistence-unit` | 0 | 28.430 |
| `./scripts/test.sh config-contract` | 0 | 10.158 |
| `./scripts/test.sh performance-contract` | 0 | 6.478 |
| `./scripts/test.sh release-contract` | 0 | 48.488 |
| `./scripts/check-ai-skill.sh` | 0 | 0.774 |
| `./scripts/check-web-dist-sync.sh` | 0 | 0.219 |
| `./scripts/check-logging.sh` | 0 | 0.210 |
| `go vet ./pkg/persistence/database/nosql ./examples/04-shop-performance/... ./examples/07-shop-order-scale/...` | 0 | 1.351 |

`performance-contract`中部分跨模块包是仅编译契约，不当作完整业务行为通过；示例04/07为组件包普通/race，不代表真实消费方MySQL或Full-E4验收。正式干净HEAD的`release-check.sh --release`及远端tag/Go代理结果在版本发布回执核验。

按code-review-and-quality五维直接审阅：唯一生产分支只略过不存在标记的无效删除核验，存在标记的范围/顺序/事务路径不变；新增点读保留缺失快照的冲突依赖，不引入异步状态、业务含义或新资源。错误传播、回滚、真实TooBig及原卷进程恢复有实际回归。新增测试复用原store/observer/子进程模式，无新增生产观测接口；没有依赖升级或公开鉴权变化。本仓库禁止子Agent，审阅由当前主Agent完成，不声称外部独立审计。

公开证据包：[`evidence.tar.gz`](evidence/empty-group-seek-20261103/evidence.tar.gz)，96584 bytes，SHA256 `fcbb4764ef4f7422678b58eeb1da65ecf87e5dc331aaa133db731692b8ffe5f8`。包含34份脱敏副本/清单，原始39份日志/CPU的bytes及SHA256见包内raw-manifest。原始FAIL、未采集null及SKIP边界保留；环境收集初次沙盒写失败单列为SANDBOX_WRITE_DENIED，非组件FAIL。
