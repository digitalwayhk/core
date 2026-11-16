# 可靠扫描重复工作修复与升级说明（2026-11-16）

本次仅修复已有 write-behind、普通 SharedKeys 和 Owned reader 的重复扫描。没有合入独立组锁专项 WIP，没有新增公共 API、配置、依赖、提交器或持久格式。生产实现为 `8a350a8b`、`f4d27cde`、`10c752a9`，加上发布前分类来源校验 `f47ab497`。

## 根因与最小修改

### write-behind：实际 target 在途组

`selectDispositionRound → 原组queue Seek → 原件读取` 原先对进程内已经占用的 target 组重复执行；下一轮不能准入该组，却仍支付查找/解码成本。现在在原扫描预算与 cursor 内识别 `activeGroups=false` 的实际 target，跳过无用 queue Seek。`writebehind_group_execution.go` 的登记值与 `activeIDs` 一致。失败 handler 使用 true，仍读取原件、持久化到期暂停，不能一概跳过所有 active 组。

保留原 syncExecMu/事务/提交器、版本、指纹、attempt、BatchID、同组屏障和真实回调退出边界。没有将 SQL 或失败回调放入本地事务，也没有把锁移除作为优化。默认旧整批模式不变；本优化主要作用于已经显式启用 IndependentGroups 的进程内 target。

### 普通 SharedKeys：头完成丢失尾提示

原 session 将尾消息 ID 提示归属于头 task，头完成时连同尾提示释放，后续 PEL 轮询又重取相同正文。现在 `runReliableKeyedOwner` 用最多原 effective window 条 ID→ordering key 元数据，成功只删除头 ID；每次原 XPENDING 完整身份页用于清除已消失的提示，游标仍按原页边界推进。

`claimReliableKeyedPage` 复用原 Redis 响应解码，只将已知项返回纯 ID 占位，未准入正文不缓存；某 key 的最早实际可执行项由 `pendingBody` 重取原 Stream 正文，再经原 fenced claim、handler、成功 ACK。缺正文报错且保留 PEL，同 key 后项不能越过。非 SharedKeys 仍走原路径。

### Owned：纯观测重复反序列化全部 PEL

固定/动态 Owned reader 原先每次 Observe/Step 都重取整个原 PEL 页的正文并执行 Key 分类。现在每 subject 最多保存原 PendingWindow 条 ID→ordering/scope 分类，不保存 payload、完成事实或目录注册布尔值。动态 key 注册仍以当轮目录为准；提示不能将注册前未知永久冻结。

每次仍取完整原 PEL 身份页，移除消失 ID；原 W/U、未读核验和冻结 scope 不变。`hydratePending` 只为实际选中项重取原正文、核对大小和应用 Key，然后保留 Ready credential、fenced claim、业务 handler、成功 ACK。已知项正文缺失会在实际准入或提示失效重分类时发现，不能承诺纯 Observe 立即发现人为删除。

发布前另补真实 Redis RED：Broker 附加保留名字符串字段不能冒充 scope 分类。`pendingScope` 只信任 Go 进程内私有 `redisOwnedPendingHint` 类型，Redis 字符串不具备该类型。该问题只出现在尚未发布的本轮候选，不能归为正式 v1.13.2 的既有漏洞。

### 有界资源与可靠性

- 普通提示每 session ≤effective window；Owned 每 subject ≤PendingWindow。ordering key 超过4096字节不缓存，沿原读取；scope key沿原512字节约束。元数据内存随原有限窗口增长，不保存正文副本。
- 不扩 worker、prefetch、连接池、PEL或保留容量；不提前 ACK、不清 Stream/group/pending、不重置游标，不忽略原件缺失。
- 提示重启后消失，按原消息/PEL恢复。租约到期不证明旧回调/SQL已退出，SQL fence和应用幂等仍必需；不承诺 exactly-once。
- `reclaim_read` 表示实际取回正文行数，不是唯一消息数、业务重投或完成量；纯 Observe 不全部计入。Redis Lua父子耗时和并发累计等待不能相加冒充请求墙钟。

## RED → GREEN

全部使用 Core 隔离临时 Badger / 专属真实 Redis，未接触消费方数据。原始日志与逐命令 argv、退出码、字节及SHA256在 `/private/tmp/core-reliable-scan-fix-20261116/`，对照旧扫描原件在 `/private/tmp/core-mq-scan-analysis-20261116/`。

| 验证 | RED | GREEN |
| --- | --- | --- |
| `TestIndependentGroupsSkipActiveTargetScan` | 16个真实在途target，20轮重复queue Seek=320，exit 1 | 同窗口Seek=0；新健康组读取真实组首并确认，16个原pending释放后排空 |
| `TestSharedPendingRollingProgressKeepsHints` | 128条、4键、A受控阻塞，单ID正文最多重取16次，exit 1 | 每ID最多发现/准入2次；B等键真实前进，同键仍串行 |
| `TestOwnedPendingClassificationHints` | W32、20次Observe，XRANGE=660，exit 1 | 固定/动态均20，PEL32、ACK0保持 |
| `TestOwnedPendingClassificationCannotComeFromBrokerFields` | 候选信任保留名字符串，误选A，exit 1 | 真实Key仍为B，不误选、不增加A的Unread/WorkVisible |
| `TestOwnedPendingHintsFetchOriginalBeforeHandler` | 独立边界回归 | 准入重取first/second；ACK后不重放；缺正文handler0、ACK0、原PEL2 |

初次 Shared 缺正文观测 hook 未覆盖新读取错误，保留 `mq-shared-scan-green` 的失败。修复 Redis 端缺正文错误路径后 `mq-shared-scan-green-v2` 通过，原断言没有放宽。主候选第一次合并运行 MQ 普通/race与真实进程验收时，全局Redis命令计数受并行测试流量干扰（预期16、实测25）；该两份FAIL原件保留，最终必须独占Redis串行验证，不能将并行失败改写为PASS。04示例第一次命令误写目录，保留退出1；实际 `04-shop-performance` 普通/race另行运行。

核心复现命令（前置设置专属Redis地址，禁止连接生产）：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=<独占Redis> go test ./pkg/server/mq -run '^Test(SharedPending|OwnedPending)' -v -count=1 -timeout=90s
rtk proxy go test ./pkg/persistence/database/nosql -run '^TestIndependentGroups' -v -count=1 -timeout=120s
rtk proxy go test ./pkg/persistence/database/nosql -v -count=1 -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout=300s
```

## 固定资源对照与剩余边界

### Redis扫描成本

同一专属Redis、原窗口及连接预算（命令4/阻塞8），三轮对照。普通负载640条/8键/Count128/并发8，一个键受控阻塞，其他560条先真实完成，最后全部640条确认、PEL0、无越序。

| 场景 | 旧版三轮 | 候选三轮 |
| --- | --- | --- |
| 普通Shared，1对象，XRANGE | 6195–6208 | 1193 / 1193 / 1193 |
| 普通Shared，3同PID对象，XRANGE | 13985–15302 | 2706 / 2689 / 2675 |
| Owned固定/动态，W128，20次Observe，XRANGE | 2580 | 20 |
| Owned固定，20次pending分类，XRANGE | 2560 | 0 |
| Owned动态，20次pending分类，XRANGE | 2580 | 20 |

三同PID对象只证明成本，不充当跨进程证据；独立PID和真实WS在下一节单列。这些是扫描成本改进，不是消费者容量认证，也不能解释消费方整段异步时延。

### write-behind扫描单变量

使用原有性能fixture的私有overlay，仅替换本次扫描生产文件，原锁/确认实现保持；每轮1920条、16组、IndependentGroups=32、target55ms定时器无SQL、GOMAXPROCS=1、本机APFS非tmpfs、SyncWrites=true、DetectConflicts=true，三轮；这是约6秒持续输入和后续排空短窗口，不是长稳态。

| 指标 | 旧版三轮 | 仅扫描候选三轮 |
| --- | --- | --- |
| Save P99（ms） | 7.639 / 6.166 / 5.997 | 9.060 / 13.527 / 6.978 |
| Save平均（ms） | 2.754 / 2.769 / 2.836 | 3.311 / 3.666 / 3.317 |
| 停输入后排空（s） | 1.151 / 1.140 / 1.165 | 1.146 / 1.221 / 1.286 |
| 最终确认/错误/pending | 每轮1920/0/0 | 每轮1920/0/0 |

候选健康组Save起点到本地确认P99为1112.514 /1179.331 /1231.340ms，包含target队列/处理和本地确认，不等于Save耗时。旧版另一独立差轮P99=14.332ms也保留。**没有证明Save P99改善、确认吞吐提升或所有长尾解决**。本次结构收益是无用queue Seek减少；锁专项WIP的性能不被接受且未发布。CPU限流与物理I/O未采集，原JSON为null；不能补零，嵌套累计提交/target时长不相加。

## 升级、兼容与责任边界

消费者升级正式tag后原代码即可获得对应角色优化，无新增参数；没有启用 IndependentGroups 的旧write-behind不会被擅自切换。producer升级不能替代 SharedKeys consumer / Owned reader 所在进程升级；旧副本仍有旧扫描成本。`Key` 回调仍须稳定、纯粹地映射不可变原事件。

本次无需重建库、重同步pending、删除标记、重建group或迁移scope；原 fingerprint/sequence/epoch/SQL fence、Stream/PEL/游标、StoreID/policy/持久字节语义保持。仍继承既有 Shared/Owned/有界处置的格式回退禁令，不能以本次兼容声明允许旧二进制打开其不识别的更早能力卷。

`go get`只更新模块，不安装AI skill。消费者按[安装指南](CONSUMER_AI_SKILL_SETUP.md)重新运行 `scripts/link-consumer-skill.sh`，阅读权威 `docs/ai/core-skill/` 对应版本。默认行为、失败/取消、真实回调退出与应用数据库幂等仍由原合同约束。

本次未操作Bitzoom仓库/Full-E4、未升级部署消费方、未发业务压测。消费方功能/UAT/容量、Redis HA/Cluster和非Redis外部Broker验收为 **NOT_RUN**。MySQL fixture的数据目录使用tmpfs仅作SQL fence功能证明，不作为落盘性能证据。

Code review: skipped (ce-code-review unavailable)

上述技能要求派生审查Agent，而项目与本轮明确禁止子Agent；由当前主Agent直接核对正确性、错误/取消、fence、预算与API边界，发现并补了分类来源RED，未宣称独立Agent审计。

## 持续输入逐轮分位与累计本地事务

以下ms为单次Save分位；QueueWait等原指标为并发累计量，不是单次Save独占时长。确认吞吐为含输入及排空的整窗确认/秒。

| 源码/轮次 | Save P50/P95/P99/最大（ms） | 确认/秒 | 前台/确认/元数据事务数 |
| --- | --- | --- | --- |
| 旧版/1 | 2.730 / 4.934 / 7.639 / 15.025 | 270.229 | 161 / 1920 / 3723 |
| 旧版/2 | 2.586 / 4.030 / 6.166 / 30.859 | 270.727 | 156 / 1920 / 3635 |
| 旧版/3 | 2.848 / 5.186 / 5.997 / 6.192 | 269.765 | 163 / 1920 / 3558 |
| 仅扫描候选/1 | 3.168 / 4.955 / 9.060 / 9.541 | 270.457 | 146 / 1920 / 3593 |
| 仅扫描候选/2 | 3.370 / 7.043 / 13.527 / 14.027 | 267.544 | 150 / 1920 / 3481 |
| 仅扫描候选/3 | 3.326 / 5.135 / 6.978 / 9.442 | 265.226 | 149 / 1920 / 3359 |

## 原始失败与收口日志指纹

| 日志（证据目录内） | 退出码 | bytes | SHA256 |
| --- | --- | --- | --- |
| wb-active-scan-red.log | 1 | 2431 | `2bff6f7bd386982114801b4762a9f0152fb645dc553d72b44fb1ba46e430867f` |
| mq-pending-scan-red.log | 1 | 1713 | `8362bc820b4117f3aa11d585a74918c10b3cd51443cab3530ef5ce9edc94b7e3` |
| mq-shared-scan-green.log | 1 | 1311 | `d594fa1b5612188996aba90c818afe21abcd417042d7df99c6c3af27dd0e69fb` |
| main-owned-classification-provenance-red.log | 1 | 608 | `67cc2f425a275d543a7f0b98468e5cd54e96a8458e7a4ca78edf9708fd9fc7c0` |
| main-owned-classification-provenance-green.log | 0 | 1125 | `bcbccc74a2d914d5378b125ce80b2074a051ae4aa2923c160cf70e24fab86af8` |
| main-persistence-normal.log | 0 | 1266364 | `87abc9768d0ea58e3b2366ba652a1c9a8f04c0fdeeef6858f3326675fe7ac5aa` |
| main-persistence-race.log | 0 | 1208394 | `8bb18f920af490685ccc7234ee7cec1a85399a029f612873c99dd38bdf835d6d` |
| main-server-normal.log | 1 | 193731 | `631c291693c4137f77b875d0640fe1e546687ac0d18acebc519dd9769bb61b2c` |
| main-server-race.log | 1 | 194173 | `dc1f338a5f303e7b86d2e64bd6428a7c4c809644f6c00105a5d892b6df25a114` |
| main-examples-normal.log | 1 | 5048 | `fdd60f595c15d46cde4b6331999bb21f2cc2e4b00603954c33b0a4abbf1fcdda` |
| main-examples-normal-v2.log | 0 | 5645 | `a067511c61cec9faf380968c3c20e234da49b9dd9c6b90346f870c151c972131` |
| main-examples-race.log | 0 | 5645 | `28a35f384feae870f2e53ac895d099538d440cf0bf5fb6858505e394e632cf82` |
| main-repository-gates.log | 0 | 3597 | `ef72d6859611612a5a1c64617a4baf7126efa6c9040674beaf5b94222bae5797` |

## 最终候选实际验收

代码绑定 `f47ab49783426b3238c0dfeba8d67532ef8c306e`，与本次最终文档提交的生产代码一致。所有下列命令均在独立main候选树执行，锁WIP没有参与。

| 证据命令名 | 退出码/秒 | 顶层PASS/SKIP | 日志bytes/SHA256 |
| --- | --- | --- | --- |
| main-persistence-normal | 0 / 37.642 | 281 / 53 | 1266364 / `87abc9768d0ea58e3b2366ba652a1c9a8f04c0fdeeef6858f3326675fe7ac5aa` |
| main-persistence-race | 0 / 66.732 | 281 / 53 | 1208394 / `8bb18f920af490685ccc7234ee7cec1a85399a029f612873c99dd38bdf835d6d` |
| main-server-serial-normal | 0 / 334.417 | 474 / 28 | 195538 / `d6b69dfb431f051688b15386eacedc71e32590dc36a1f82526241028cf0f4e3d` |
| main-server-serial-race | 0 / 350.778 | 474 / 28 | 195966 / `868d82d48376f8ab47ad3c5ba497e96e9be252b6573ca11d83680a20f038be21` |
| main-real-processes | 0 / 445.132 | 5 / 0 | 3946 / `3ae71f122d9b3c1341c9d2ed3a2eef7a8a52d853095f813e8f8d4abb68be3d47` |
| main-examples-normal-v2 | 0 / 31.486 | 0 / 0 | 5645 / `a067511c61cec9faf380968c3c20e234da49b9dd9c6b90346f870c151c972131` |
| main-examples-race | 0 / 36.237 | 0 / 0 | 5645 / `28a35f384feae870f2e53ac895d099538d440cf0bf5fb6858505e394e632cf82` |
| main-badgeropen-race | 0 / 2.991 | 3 / 0 | 598 / `15c10dbd01ced0de30095a18ca17d224ff6eb00ab2fe4e30ae23d90b708aee40` |
| main-repository-gates | 0 / 137.203 | 0 / 0 | 3597 / `ef72d6859611612a5a1c64617a4baf7126efa6c9040674beaf5b94222bae5797` |
| main-release-contract | 0 / 57.815 | 0 / 0 | 1861 / `6370982c6945d71b05e6784916c1f62b2676cd2362911c80b31d5e4436ac5c3b` |


SKIP包含供父测试启动的子进程入口、未开启的性能/故障开关及未配置外部依赖；不是它们被执行并通过。门禁使用非verbose输出时顶层PASS计数为0，退出0只代表门禁本身；不能用该数替代业务观察量。完整argv在同名JSON，日志保留实际子测试与PID。

串行真实Broker命令（普通与race各一次，前置独占隔离Redis；本轮配置允许暂停该专属Redis，不能对业务Broker使用）：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=<独占Redis> \
 CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=<0600专属MySQL配置> \
 CORE_TEST_SHARED_FULL_LEASE=1 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 \
 go test -p 1 ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/ownership -v -count=1 -timeout=300s
# 相同环境增加 -race 后串行再执行，不能与全局命令计数测试并行。
```

真实进程命令：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=<独占Redis> \
 CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=<0600专属MySQL配置> \
 go test -p 1 ./examples/integration/10-reliable-multi-instance \
 ./examples/integration/11-scoped-event-ownership ./examples/integration/ws-relay \
 -v -count=1 -timeout=15m
```

父集成命令使用普通Go test；现有StartProcess默认以go build -race编译真实服务子进程，关闭时核对实际退出和DATA RACE。没有将同PID多个对象冒充多进程。例10真实worker PID3164/3311。例11固定worker PID5879/6306、动态8420/8589、迁移9804/10133，每组两个Gateway、三个认证WS客户端，真实SQL提交计数2。固定→动态保持原组/消息/SQL fence，真实旧owner与恢复窗口验证仍走正式入口。正常换主到Ready为55.043 /55.218 /369.623ms；动态/迁移硬退出到Ready且relay恢复为14685.903 /23269.378ms，包含替代进程构建/启动，不能当作纯租约或生产恢复SLO。普通private/public/Owned路由共存及真实WS投递、身份隔离、退订/关闭、mTLS拒绝边界通过，Gateway PID12532/12653。

存储整包普通/race各281顶层PASS、53SKIP；真实新PID原卷恢复覆盖ConfirmationHardCrash、IndependentGroupsHardCrash、DispositionSelectionTargetReopensInNewProcess和CleanupCommitCloseRestart。原unknown/暂停/接管、有限预算、旧版本、TooBig、取消和回调实际退出边界均保留原断言。继承v1.13.2空memtable打开修复，internal/badgeropen另跑race；不把有限组件恢复测试扩写为任意非空损坏自动恢复。

执行仓库persistence-unit、performance-contract、api-compat、public-api、config-contract、security、web-dist-sync、check-logging及release-contract。04/07源码包普通/race通过；07需要远程权威库的业务MySQL UAT因未开启该业务开关仍SKIP，不能用ownership专属MySQL证明07业务UAT。

消费方升级命令：`go get github.com/digitalwayhk/core@v1.13.3`。本报告对应正式候选；正式发布和官方Proxy/SumDB冷验包结果以交付回执为准，不在tag创建前声称远端已可获取。
