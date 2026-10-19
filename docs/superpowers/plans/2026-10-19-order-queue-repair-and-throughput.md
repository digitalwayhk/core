# 07 队列正确性修复与吞吐优化实施计划

> 执行要求：由当前主 Agent 使用 `superpowers:executing-plans` 逐项执行并自审；遵守 `CLAUDE.md`，禁止派发子 Agent。复现、修复、依赖升级和性能优化分别提交。本文是计划，不代表已修复或容量已通过。

**目标：** 查清负数 pending 与无索引本地残留，验证 Badger 升级必要性，在不降低可靠性、认证或验收标准的前提下推进 2000/s 持续600秒。

**架构：** 保留本地可靠接单、异步汇合共享 MySQL、订单与 Outbox 同事务、按订单分键发布及成功后 ACK。先建立独立事实核对和最小复现，再决定库升级或应用修复；正确性通过后，逐项测量数据库和发布链路，最后独立验证 go-zero 维护升级。

**技术栈：** Go 1.26.6、当前 Core、原生 go-zero 对照、Badger v3/v4 隔离实验、GORM/MySQL、Redis Streams；不新增 Broker、业务表或框架封装。

## 一、基线、范围与明确不做的事

代码基线为 `a1d71c6`，分支为 `feat/transactional-messaging-throughput`。实施时重新封存实际 SHA、dirty 状态、依赖图和二进制版本；代码有变化时重新建立基线，不把后续工作树混入旧测量。

证据来源：

- [Core/原生下单对照](../../codex/GO_ZERO_ORDER_COMPARISON_2026-10-18.md)。
- [go-zero 新旧版本八轮对照](../../codex/GO_ZERO_VERSION_COMPARISON_2026-10-18.md)。
- `.temp/gozero-version-20261018/comparison-v1/` 的原始请求、采样、日志、检查点、`analysis.json`、`physical-checks.json`、`retained-fact-verification.json`。

已确认：原生两版本最终 pending 分别为 -7032/-5643；旧版物理队列与事实均0，新版物理队列0、事实1785。1785份事实均已在只读来源的 MySQL 物理克隆中匹配订单和已发布 Outbox。两版全部确认请求匹配远端；容量均未通过，旧版另有2项观测错误。现象不能直接归因于 go-zero、Badger 或 Core。

本轮实施范围按下述关卡逐步展开。保留07现有订单、支付、取消、通知及持久化语义；不设计业务状态、消费幂等依据或消息目标，不迁移Outbox/Inbox至另一个存储。不改前端，不修改其他项目资源，不降低 fsync、事务、错误率、顺序或 ACK 约束。

保护 `docs/codex/SCAFFOLD_VUE_CORE_INTEGRATION_STUDY.md`、`web/scaffold-vue/` 和已有 `.temp/` 证据；不覆盖旧结果。原 Badger 目录不能交给新版写入，所有诊断在关闭后的副本或正确快照上进行。复制运行中的 Badger 普通目录不能视为一致快照。每个测试依赖独占并带所有权标签，结束后停止，保留失败卷。

后续执行范围更新：用户两次明确要求清理本机Docker，旧测试SQL/Redis原卷包括首个最终Core长窗已按所有权核对回收；原始请求、结果、日志、Badger核对副本及既有归档仍保留，其他项目与用户WIP未清理。不能宣称所有旧原卷均经过归档恢复或仍可原位恢复。空间解除后续跑的五轮新失败原卷保留。清理明细及最终状态见[执行记录](../../codex/ORDER_QUEUE_EXECUTION_2026-10-19.md)。

## 二、Badger 版本判断：优先验证，不预设根因

2026-10-19现场核验：

| 项目 | 当前事实 | 对计划的影响 |
| --- | --- | --- |
| 当前依赖 | `github.com/dgraph-io/badger/v3 v3.2103.5`，模块时间2022-12-15 | v3 `@latest`仍为该版本，同主版本没有更新可试 |
| 候选 | `github.com/dgraph-io/badger/v4 v4.9.6`，模块时间2026-08-05 | 单独做主版本适配和数据兼容验证 |
| 压缩后删除复现 | [上游修正 #2278](https://github.com/dgraph-io/badger/pull/2278) | 检查旧v3实现是否存在同类路径，不能仅据症状认定命中 |
| value-log GC 后删除复现 | [上游修正 #2310](https://github.com/dgraph-io/badger/pull/2310) | 独立覆盖GC场景，不与压缩场景合并归因 |
| 后续性能修正 | [v4.9.6发布 #2330](https://github.com/dgraph-io/badger/releases/tag/v4.9.6) | 不停留在v4.9.4；验证修复正确性及写入放大/停顿 |

[v4.0.0记录](https://github.com/dgraph-io/badger/blob/v4.9.6/CHANGELOG.md#400---2023-02-27)说明当时未改变磁盘格式；这不能推导v3至v4.9.6的完整双向兼容。[官方备份说明](https://github.com/dgraph-io/badger/blob/v4.9.6/docs/quickstart.md#database-backup)提供Backup/Load路径。当前没有执行Badger升级或兼容测试，不宣称新版已解决本项目异常。

## 三、阶段与关卡

| 阶段 | 交付 | 通过后才能做什么 |
| --- | --- | --- |
| G0 证据与事实核对 | 原件哈希、独立核对器、诊断基线 | 在副本复现 |
| G1 根因与存储候选 | 原生/纯Badger复现、v3/v4矩阵、迁移判定 | 选择最小修复，不预设双重修复 |
| G2 正确性 | 原生修复及Core对应路径核验、崩溃恢复 | 建立可用于性能比较的基线 |
| G3 测量可信度 | 独立发送器、分阶段观测、稳定错误分类 | 单变量性能实验 |
| G4 性能 | 连接/SQL/规则/发布逐项结果及取舍 | 最终长窗容量验收 |
| G5 升级与交付 | 独立go-zero回归、完整验收和文档 | 提出合并/发布候选，不自动发布 |

某项未复现不能标为已修复；P0仍不通过时可继续只读诊断和测试工具准备，不发布吞吐提升结论。阶段完成不等于2000/s目标完成。

## 四、实施任务

### 任务1：封存证据并增加独立状态核对（G0）

**文件：** 新增 `examples/07-shop-order-scale/go-zero-reference/internal/reference/pending_invariant_test.go`；新增只读工具 `examples/07-shop-order-scale/tools/inspect-order-pending/main.go`；修改 `examples/integration/07-shop-order-scale-multi-process/order_load_test.go`、`state_retention_test.go`；新增证据根 `.temp/order-queue-repair-20261019/`。

- [ ] 保存工作区、实际SHA、Go/模块图、配置、容器标签/镜像ID和原件文件哈希。执行环境中不打印密码、Token、完整订单或身份密钥。
- [ ] 核对器默认只读，输入关闭的副本；同时统计事实、索引、双向关联及损坏项。遇到负数、解码错误或缺失返回非零，不清零、不重建、不删除。
- [ ] 旧的`inspect-badger`曾以可写方式打开副本，可能触发恢复/压缩；先从原关闭检查点重新制作副本，用ReadOnly选项复核。旧“物理残留”数字只代表旧扫描路径的观察，不先认定等于关闭瞬间原件视图。
- [ ] 原生在写入/确认均暂停的检查点核对 `depth == p事实数 == 合法q索引数`；每个q值须指向正确p，p中的OrderID须对应q主键。运行期一致快照必须使用既有写入锁，不能跨两个独立事务读出假不一致。
- [ ] Core分别核对缓存pending、同步索引和未同步版本/删除墓碑；保留 `IsSynced=true` 的合法缓存不是待同步，不要求整个业务prefix物理为空。不把 `GetPendingSyncCount()`只扫描索引的结果冒充业务事实扫描。
- [ ] 将“远端已匹配”“本地物理已收敛”“内存指标一致”作为独立结果。最终只读SQL仍逐键核对全部确认请求，失败后落库单列，Broker lag/PEL和Outbox published另计。

基本契约测试可直接复用现有 `testOrder` 和包内方法，拟增加以下完整测试；它可能在当前版本通过，仅是核对器基线，不能冒充异常的RED复现：

```go
// TestPendingQuiescentInvariant 验证暂停写入后的计数及双向索引。
func TestPendingQuiescentInvariant(t *testing.T) {
    s, err := openPending(t.TempDir())
    require.NoError(t, err)
    defer func() { require.NoError(t, s.close()) }()
    item, err := s.accept(context.Background(), testOrder(100, "invariant"))
    require.NoError(t, err)
    s.mu.Lock()
    depth := s.depth.Load()
    facts, queues := int64(0), int64(0)
    err = s.db.View(func(tx *badger.Txn) error {
        it := tx.NewIterator(badger.DefaultIteratorOptions)
        defer it.Close()
        for it.Rewind(); it.Valid(); it.Next() {
            key := it.Item().KeyCopy(nil)
            if bytes.HasPrefix(key, []byte("p:")) { facts++ }
            if !bytes.HasPrefix(key, []byte("q:")) { continue }
            queues++
            dataKey, e := it.Item().ValueCopy(nil)
            if e != nil { return e }
            value, e := tx.Get(dataKey)
            if e != nil { return e }
            var o Order
            if e = value.Value(func(v []byte) error {
                return json.Unmarshal(v, &o)
            }); e != nil { return e }
            if !bytes.Equal(key, queueKey(&o)) ||
                !bytes.Equal(dataKey, localKey(&o)) {
                return errors.New("队列索引与事实身份不一致")
            }
        }
        return nil
    })
    s.mu.Unlock()
    require.NoError(t, err)
    require.GreaterOrEqual(t, depth, int64(0))
    require.Equal(t, facts, queues)
    require.Equal(t, facts, depth)
    require.NoError(t, s.confirm([]*Order{item}))
    require.NoError(t, s.confirm([]*Order{item}))
    require.Zero(t, s.depth.Load())
}
```

该测试文件使用 `bytes/context/encoding/json/errors/testing`、Badger和testify/require导入。补充两份畸形夹具：p无q、q无p，核对器必须分别失败；即使远端已匹配也不能伪装成物理空队列。

验证命令（新增测试和工具后执行）：

```bash
rtk proxy go test ./examples/07-shop-order-scale/go-zero-reference/internal/reference -run '^TestPendingQuiescentInvariant$' -count=1
rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run 'Test.*(Load|Retention|Pending)' -count=1
```

外部开关未启用的测试会SKIP，不能计入真实恢复通过。G0通过条件是核对器能发现已知畸形状态、原件哈希不变、旧证据结果被准确重现。

### 任务2：最小复现与Badger v3/v4隔离实验（G1）

**文件：** 新增 `examples/07-shop-order-scale/go-zero-reference/internal/reference/pending_compaction_test.go`；新增独立实验模块 `examples/07-shop-order-scale/go-zero-reference/experiments/badger-repro/v3/{go.mod,main.go}` 与 `v4/{go.mod,main.go}`；新增 `docs/codex/BADGER_QUEUE_REPRO_2026-10-19.md`。

- [ ] 纯Badger程序只导入标准库及对应Badger，不导入Core、go-zero、GORM或SQL。两份程序除import/API适配外使用相同操作序列、随机种子、键值大小和有效持久化参数；单独二进制运行，避免同时导入两个主版本造成注册或依赖干扰。
- [ ] 建立六组场景：正常写/删；并发接单/确认及重复确认；超过memtable并经历自然压缩；显式压缩；value-log GC（`ErrNoRewrite`不作为成功发生GC的证据）；确认后关闭/重开与硬杀重启。分开记录写入、删除提交、压缩、GC、重开前后的可见键摘要。
- [ ] 导出两侧实际Options和GC/关闭策略。原生当前没有value-log GC循环，Core manager有GC且显式CompactL0OnClose=true；以现场可达的自然压缩为主控制组，显式GC/Flatten仅作单独诊断，不能当相同运行条件。
- [ ] 按上游 #2278/#2310 的复现条件检查v3实现与当前配置是否可达；引入测试时保留来源/许可。固定正确的删除可见性断言，不能让v4专用私有测试通过就宣称v3受影响。
- [ ] 对现有异常检查点只做副本实验；原始采样、复制前后哈希、首次失败操作序号和线程边界全部保留。记录故障是活跃进程中出现，还是仅在关闭/重开后出现。
- [ ] 原生层以同一操作序列记录“事务成功新增/实际移除的唯一键集合”，与物理变化和depth增减比较；区分重复计数、索引操作、读取生命周期、版本覆盖和库内部删除复现，不先写周期扫描补丁。
- [ ] 输出判断矩阵：纯库是否复现、应用是否复现、v4是否消除、参数是否匹配、失败序列能否重放。现有版本若没有失败，标NOT_REPRODUCED；不得换成必失败但与现场无关的断言制造RED。

实验命令：在两个独立模块目录分别运行 `rtk proxy go run .`，固定v3.2103.5与v4.9.6；结果写到各自新目录。每次明确SyncWrites=true、DetectConflicts=true、压缩/GC实际执行情况，记录全部有效选项及模块图，不假设跨版本默认值相同。

**决策：** 纯库命中且v4通过时优先评估升级；应用独立命中时修应用；两者均命中则分两次提交与复验。仍无法定位时保留G1未通过，并给出下一项能区分假设的实验，不进入正式性能结论。

### 任务3：Badger升级的兼容与回退关卡（G1→G2，条件任务）

**涉及文件：** `go.mod/go.sum`；`pkg/persistence/database/nosql/{badgerdb.go,sharedbadgermanager.go,sharedbadger.go,sharedbadger_operations.go}`；`pkg/server/authstate/badger.go`、`pkg/server/routecache/l2_badger.go`；原生`pending.go`及相关测试。实际清单用 `rg 'github.com/dgraph-io/badger/v3'`重新生成，包括测试，禁止无差别替换用户文件。

- [ ] 先在隔离分支/实验副本适配v4.9.6，保持go-zero、SQL、业务及调度不变。v3/v4是不同import路径，不能靠普通`-modfile`更新require冒充已切换；禁止用跨主版本replace掩盖类型/依赖差异。
- [ ] 检查Options、事务/迭代器/ValueCopy生命周期、备份接口、错误身份及公开返回类型；若导出面暴露Badger类型，先登记兼容性影响，不能当成无公开变更的补丁发布。
- [ ] 在副本验证两条读取路径：v4打开v3物理副本；v3 Backup→v4 Load至新目录。逐键校验值、版本、TTL、删除结果及重开/压缩后可见性。路径不兼容时保留原件并报错，禁止truncate、reset或自动清库。
- [ ] 同时覆盖可靠pending、普通本地库、路由L2和authstate。认证撤销/会话状态不可当可重建缓存清除；路由缓存即使允许重建，也必须单独声明，不能套用到业务pending。
- [ ] 选择最小受支持路径并写出切换顺序：停准入、等待在途/确认或保留待恢复事实、完整快照、单一新版本进程接管、核对后恢复准入。同一路径不允许旧新写进程并存。
- [ ] 回退验证必须说明新版本接单后新增事实如何保全：不能直接切回旧快照丢掉新接单；不能默认v3可读取v4写过的目录。无可验证回退/迁移路径，则不合并升级，只保留实验结论。

通过条件：功能与数据兼容真实通过、异常复现结果可解释、没有未登记的公开破坏性变更。是否升级由此关卡决定，不以“能编译”或“版本较新”代替。

### 任务4：修复原生状态路径，并验证Core同类边界（G2）

**文件：** 原生`internal/reference/{pending.go,orphan_recovery.go,application.go,business.go,pending_test.go,pending_compaction_test.go}`；Core检查点为 `pkg/persistence/database/nosql/{sharedbadger.go,sharedbadger_operations.go,sharedbadger_force_sync.go,sharedbadger_adaptive.go,reliable_write_batcher.go}`，扩展现有 `sharedbadger_syncqueue_test.go`、`sharedbadger_close_pending_test.go`；集成 `stable_pending_crash_test.go`、`state_retention_test.go`。

- [ ] 先将任务2最小失败序列加入回归，保存旧实现RED；如果根因在库，使用选定旧/新库作RED/GREEN，不额外改应用造出另一条因果。
- [ ] 应用修复以真实提交的键状态变化为准，新增、确认、重复确认及失败事务不重复计数；只有身份/版本匹配且远端持久确认后才能清理。原生不得再留下不可达p；缺少证明时保留并显式失败。
- [ ] 不用 `max(depth,0)`、高频全扫重算、无证明清理、忽略KeyNotFound或禁用compaction/GC替代根因修复。首次检测到状态异常应停止新的接单、保留诊断和恢复能力，不能继续依赖失真的计数决定安全准入。
- [ ] `orders()`不再把未经验证的depth直接当slice容量/读取上限：覆盖负数、0与物理非空、合法大量积压，返回有界安全结果或明确错误，不panic，不泄露其他用户订单。
- [ ] Core只在同类失败复现后修改。保留同key版本CAS、删除墓碑、重复ACK、部分确认、失败事务、关闭排空和正常自动同步不逐批重建的既有契约。原生问题不能自动证明Core有同缺陷。
- [ ] 真实MySQL/Redis验证：本地持久确认后硬杀、SQL提交前失败、SQL提交后/本地确认前硬杀、发布后/标记前硬杀、消费成功后/ACK前中断。原路径恢复，保留ID和幂等键，匹配订单与Outbox，允许可追踪重复而非重复业务生效。

验证：

```bash
rtk proxy go test -race ./examples/07-shop-order-scale/go-zero-reference/internal/reference -count=1
rtk proxy go test -race ./pkg/persistence/database/nosql -run 'Test.*(Pending|SyncQueue|Confirmation|BatchCommitter|Close)' -count=1
rtk proxy go test -p 1 ./examples/07-shop-order-scale/... -count=1
```

上述命令之外，必须显式运行有专用依赖的事务、ACK和硬杀恢复测试，并检查没有SKIP。G2要求失败序列回归通过、身份/数据不变、重开后状态一致；仅重算计数或只查远端订单不构成通过。

### 任务5：独立发送器与可信采样（G3）

**文件：** 从现有`order_load_test.go:runOrderLoadStage`提取纯发送逻辑到 `examples/internal/orderload/{runner.go,request.go,result.go,stage.go,runner_test.go}`；新增 `examples/07-shop-order-scale/tools/order-load/main.go`；修改原集成负载入口及`order_load_guard_test.go`。复用现有负载算法，不新写第二种发送口径。

- [ ] 公共发送包和独立main仅用标准库与该包；不导入Core持久化、测试Suite、DTO、go-zero或GORM。独立进程发压，使Core导入触发的全局统计GC不再干扰发送器；构建依赖检查必须证明确实隔离。
- [ ] 固定原请求体、JWT用户域、500/s预热、512在途、HTTP5秒、无自动写重试、1MiB响应限制和分位数算法。requestID增加稳定的run/stage命名空间，防止500/s预热与500/s主档或重复轮次撞键；同一次请求重试仍用原键，遵守现有长度校验。此变化单独测试并重建基线。凭证由权限0600配置文件传入，不放命令行、日志或提交文件；HTTP凭证不落入公开请求证据。
- [ ] 使用冻结HTTP响应夹具对照旧新发送器：成功200但无OrderID、业务失败、503非JSON、超时、取消、响应截断分别得到相同分类；模拟时钟/受控服务验证计划量=已发送+运行时丢弃+守护后未执行。
- [ ] 采样改为分阶段独立预算和耗时，字段缺失为null并保留错误；negative pending立即标INVALID_STATE。轻量每秒采集现有内存指标、池Stats与Broker状态；全量COUNT只在计时前后执行，正式窗口的非轻量扫描在控制轮单独量化开销。
- [ ] 原守护的pending40000及连续3次采样失败保留；采样失败按轮次而非错误字段个数计算。不能把取消/变更昂贵采样当作已修好原观测错误，保留旧采样控制实验和误差边界。
- [ ] 分别记录最后计划发送、全部在途结束及全链路收敛的时间。为保持现行口径，5秒排空以现有`loadEnd`（全部在途结束）为起点，同时额外报告最后计划发送至收敛的总时长；不能混用两者或通过延后起点制造通过。
- [ ] 排空阶段用有界、索引支持的EXISTS/轻量指标探测，再以全量事实核对确认有效性；记录首次满足的采样时间和观测误差，不用最终全表扫描耗时冒充后台真实排空耗时。最终核对失败仍将该轮判FAIL。
- [ ] 最终必须全量逐键读事实、核对失败后落库、Outbox发布、Broker lag/PEL，以及关闭后物理副本。别用累计计数、entries-read或Prometheus采样代替。

验证命令：`rtk proxy go test -race ./examples/internal/orderload -count=1`；原/新发送器对同受控服务契约通过后，再分别重建Core与原生的**新测量基线**。旧报告保留，不与新发送器结果合并计算提升率。

### 任务6：分段观测与连接错误定位（G3）

**文件：** `pkg/persistence/database/nosql/reliable_write_metrics.go`、`pkg/server/observability/reliable_write_provider.go`；`pkg/server/event/outbox.go`及现有Outbox测试；`pkg/persistence/database/oltp/{connection.go,mysql.go}`；07 `order_write_runtime.go`、`order_writebehind_target.go`、`outbox_store.go`；原生`{metrics.go,database.go,events.go}`。

- [ ] 优先复用现有指标，缺少时才增加有界维度的阶段计时：本地准入/队列等待/提交，SQL池等待/事务，Outbox取批/发布/确认。订单ID、requestID、EventID不得作指标标签。
- [ ] 记录池WaitCount/WaitDuration增量、InUse/Idle、MaxIdleClosed、MaxIdleTimeClosed、MaxLifetimeClosed；同步保存MySQL错误日志、连接/中止计数与语句类别统计。不把累计等待总和当壁钟，不把所有invalid connection都判为池太小。
- [ ] 将客户端超时、HTTP保护性503、RPC失败、SQL事务失败、提交结果未知分开。保留真实cause但对用户仍使用安全错误，禁止按错误字符串自动重放写事务。
- [ ] 对`connection.go`的统计强制GC单独做控制实验。先测取消统计入口GC的最小改动，检查内存/清理效果；全局后台生命周期重构若需要公开接口，另写设计并走兼容门，不把它混进SQL调参。
- [ ] 取一个有代表性的短窗采集CPU/阻塞/互斥profile并记录采样开销；最终容量窗口不同时开启重型profile、额外全表扫描或其他压力任务。

交付是可比较的阶段耗时/池等待/错误分类表，而不是笼统“数据库慢”。指标新增或生命周期行为改变时同步权威skill对应分片及公开契约。

### 任务7：数据库热路径单变量实验（G4）

**文件：** `examples/07-shop-order-scale/order-service/models/internal/store/remote_store.go`、`models/basedata/order_rule_persistence.go`、`business/{order_rule.go,order_rule_cache.go}`及测试；原生`{database.go,business.go}`；仅证据表明通用层需要时修改`pkg/persistence/database/oltp/mysql.go`。

| 实验 | 基线 | 唯一变化 | 附加验证 |
| --- | --- | --- | --- |
| D1连接复用 | open20/idle5 | open仍20，idle20 | 等待/建连/关闭增量、连接故障恢复、并发上限 |
| D2语句复用 | PrepareStmt=false | 有界生命周期的预编译复用 | 缓存上限、事务/DDL变化、连接替换与关闭 |
| D3驱动插值 | 原DSN | 安全的driver参数插值，PrepareStmt保持false | utf8mb4、引号/反斜线、二进制、NULL、金额、事务 |
| D4默认规则 | 规则缺失回退 | 启动夹具显式写入相同默认规则 | 业务结果相同；新增/修改/禁用/恢复生效语义 |

- [ ] D1～D4先分别与同一正确性基线对照，不能连续累计修改后比较首末。D2与D3互斥实验，不先决定两者都启用，不手工拼接SQL。
- [ ] D4先作为业务配置场景，不擅自负缓存“规则不存在”。原生若加入缓存失效，也须覆盖与Core同样的配置变更语义；更改禁用规则的业务含义不在本计划内。
- [ ] 每个候选运行三组交替短窗，比较成功率、p99、实际输入、SQL协议次数、池等待、CPU/内存及排空；差异与背景波动相当则标未证实，不修改全局默认。
- [ ] invalid connection先按任务6证据修复具体路径；保留现行“只读可有界恢复、写入不自动重放、事务不换连接”的契约。

各项达到明确收益且正确性回归通过后才单独保留提交；无收益的实验仅保留结果。不得用清理事实数据或改变订单/Outbox事务降低写入成本。

### 任务8：发布前积压优化（G4，按分段结果选择）

**文件：** Core `pkg/server/event/outbox.go`、`outbox_{batch_mark,barrier,keyed_concurrency}_test.go`；07 `models/transaction/{outbox_store.go,outbox_persistence.go,outbox_batch_validation_test.go,outbox_batch_mysql_test.go}`；原生`{events.go,database.go}`及相应测试。

- [ ] 先量化LoadPending、XADD/发布等待、MarkPublishedBatch及池等待。只有SQL取批占比高才检查执行计划/索引；只有发布往返占比高才评估provider批处理；只有确认占比高才优化批量确认SQL。不把“unpublished高”直接等同于某个单点。
- [ ] 保持现有earliest-first、同key顺序、失败屏障和跨key公平；只确认真实发布成功项。发布后/标记前崩溃允许重投，EventID幂等仍生效；不把发布确认当消费ACK。
- [ ] 不先提高消费并发8或发布并发4。并发/批量参数若有必要，各自与固定基线单变量比较，并验证hot key、冷key、错误key和owner/reclaim。
- [ ] Store/框架的零值兼容、可选批量接口回退及空批次契约不变。SQL索引若需要新增，走现有模型/EnsureStorage方式与发布验证，不新增手写迁移体系。
- [ ] 跑真实Broker与SQL的部分失败、标记回滚、重复投递和崩溃恢复，再运行性能候选短窗；失败率改善不能以放宽ACK或事件丢弃换取。

测试：`rtk proxy go test -race ./pkg/server/event -run 'Test.*Outbox' -count=1`，另显式运行07真实MySQL批确认与Broker集成，确认没有SKIP。

### 任务9：最终容量、维护升级和交付（G5）

**文件：** `order_load_test.go`、`order_load_guard_test.go`、`go_zero_reference_test.go`；07及原生README；两份既有对照报告追加新证据链接；权威 `docs/ai/core-skill/{write-path-and-performance.md,multiservice-and-observability.md}`；有公开变更时更新 `docs/codex/API_COMPATIBILITY_SURFACE.md`。

- [x] 先以500/1000/1500/2000每档60秒找拐点，正式比较每轮用新状态/依赖；阶梯诊断共享状态时必须明确标记为诊断，不与独立窗口混用。先固定正确性及测量基线，再评估各项候选。8个独立最终阶梯已执行，存在FAIL，见[逐轮结果](../../codex/ORDER_QUEUE_FINAL_CAPACITY_2026-10-19.md)。
- [x] Core与原生同机交替各执行三次2000/s×600秒目标；每轮记录CPU/内存、磁盘空间和同机负载。均因原守护停发，容量各0/3通过；这是共享本机条件下的FAIL，不称生产容量认证。
- [x] 按实测单轮约3.4GiB的MySQL卷及本地副本/日志预算空间，8GiB启动、3GiB运行停发线保持。首轮后空间不足未创建下轮依赖；后续用户明确清理本机Docker解除阻塞，续跑预检约100.92GiB，未触发空间停发；新失败卷保留。
- [x] go-zero v1.10.2→v1.10.3单独维护提交和依赖文件，固定已选Badger与应用代码；不与Badger迁移、SQL或队列算法同轮改变。运行07与Core受影响包，再跑API/发布/消费方契约；未测消费方单列NOT_RUN，不自动更新其依赖。独立提交`b669adb`，最终代码`258b45a`全框架及发布门禁通过。
- [x] 每轮封存源码/依赖/配置/真实二进制指纹，保留所有轮次结果和失败。1428个Go/依赖文件与最终回归一致，已生成[机器可读容量结果](../../codex/ORDER_QUEUE_CAPACITY_EVIDENCE_2026-10-19.json)；退出0不替代完整门槛。原卷清理按前述用户后续授权说明，不将归档字节比较冒充恢复验收。

最终容量通过必须同时满足：

1. 主档完整600秒，实际发送1200000请求，发送器丢弃0、守护后未执行0、无窗口中断。
2. 成功率至少99.9%；全部确认请求逐键匹配，失败后实际落库独立列出，客户端重试仍用同一幂等键。
3. 正式采样错误0，pending无负值/漂移；停止发送后5秒内远端汇合、Outbox published及Broker lag/PEL收敛。
4. 关闭后副本核对符合存储契约：原生无不可达或未确认残留；Core未同步索引/版本收敛，允许有契约内已同步缓存。关闭、压缩、GC、重开后删除事实不重现。
5. 内存、磁盘、连接和goroutine不持续无界增长；保留fsync、同事务、认证、顺序和成功后ACK。

资源审核区分永久业务事实和技术积压：订单/永久事件存储随确认请求增长属预期，报告单位事实开销及空间预算；pending、未发布Outbox、lag/PEL的收敛和空间守护仍按原门槛判定，不能用业务增长解释未收敛。有限窗口只证明该窗口内的资源趋势，不推导无限时间无泄漏。

每个待宣称通过的实现须三轮均满足以上条件，单轮失败不能用平均结果覆盖；对照方失败也应单独保留，不混成另一方的通过或失败。

若仍未达标，交付已验证的正确性修复、逐项性能结果和下一瓶颈证据，容量标FAIL，不继续盲目加并发或降低门槛。无需为本计划执行推送、合并、打tag或生产部署。

## 五、回归、提交和回退

每个实际修复先RED→最小实现→GREEN→race→提交；只有新的失败或变更才扩大/重复测试。以下按触及范围执行，不把全部SKIP包测试当外部验收：

```bash
rtk proxy go test -p 1 ./pkg/persistence/database/nosql ./pkg/persistence/database/oltp ./pkg/server/event ./pkg/server/mq -count=1
rtk proxy go test -race ./pkg/server/authstate ./pkg/server/routecache -count=1
rtk proxy go test -p 1 ./examples/06-shop-microservices/... ./examples/07-shop-order-scale/... -count=1
rtk proxy bash scripts/check-logging.sh
rtk proxy bash scripts/test.sh api-compat
rtk proxy bash scripts/test.sh release-contract
```

Badger升级须增加全部使用点和普通/加密/TTL/备份恢复测试；涉及通用层时，串行全框架及受影响真实多进程回归是交付前门槛，不以定向测试替代。真实Redis/MySQL、事务回滚、硬杀原卷恢复分别记录；NATS/etcd等未启用能力保持NOT_RUN，不据此扩大发布保证。

提交顺序：事实核对与复现；Badger适配/迁移（若选中）；独立应用修复（若需要）；Core复现与最小修复（若命中）；发送器/观测；每项性能候选；独立go-zero升级；结果与skill。只暂存命名文件，不碰用户WIP。性能参数可回退到正确性基线，数据目录不得通过代码回退自动降级；运行后新增事实必须先安全汇合或迁移。

## 六、计划自审与当前状态

- [x] 已核对源码、现有skill和两轮对照证据，区分原生异常与未证实的Core/依赖缺陷。
- [x] 已查询Badger v3/v4并核对上游相关修正；未把症状相似当成根因命中，也未把v4.0格式说明扩大至最新版本。
- [x] 已将Badger主版本兼容、公开类型、可靠pending/authstate和接单后回退列为独立关卡。
- [x] 已区分Core同步索引/墓碑/缓存与原生p/q；无统一“所有本地数据必须删除”的错误断言。
- [x] 已把发送器变更、采样变化、库升级和性能变量分开，保留旧基线，未降低容量标准。
- [x] G0～G3实现与专项、独立v4适配和迁移/回退、原生历史状态恢复、两个真实硬杀窗口、发送进程隔离和阶段观测已完成验证。现场负数/不可达p因果仍为NOT_REPRODUCED，不能写作根因已修复。
- [x] G4完成三组单变量对照，独立保留D1/D3及通用池隔离修复，不保留无稳定收益或业务失效未接线的D2/D4/B1。最终`258b45a`全框架、真实依赖、发布门禁、认证/缓存race和日志格式通过。
- [x] G5三组交替600秒目标测试、资源审核和结果文档已完成；Core/原生各0/3通过，容量FAIL。3560946个确认请求全部逐键匹配，7795个失败请求实际落库保持结果未知；两侧facade集合长期无界、业务停顿归因等未关闭，不将测量完成写成目标达标。最终结论与建议见[容量验收](../../codex/ORDER_QUEUE_FINAL_CAPACITY_2026-10-19.md)和[候选自审](../../codex/ORDER_QUEUE_CANDIDATE_REVIEW_2026-10-19.md)。

本计划写作完成只表示实施路径可审查；异常根因、升级收益与最终目标仍须执行上述验证后判定。
