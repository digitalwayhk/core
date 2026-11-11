# 同事务新快照复用：实现与验证

2026-11-11。发布候选为兼容PATCH v1.11.1；精确main/tag提交和官方Go SUM由正式Release的交付回执绑定，不能从候选测试推导消费方已接入。

## 根因与最小修改

v1.11.0精确提交bb1eba23f9cf3c2c638b07c659ab989f38f94591：前台applyWriteOperationBatch通过pkg/json(jsoniter兼容配置)编码/Set，随后ensureSyncQueueEntry转ensureDispositionEntry，以encoding/json读回同一新wrapper、解码再编码计算FP/ID。发生在本地FIFO/同prefix事务中，不是持锁等MySQL。R392/R409/R410是消费方提供的重叠采样/壁钟证据，不能把CPU占比换算为单笔独占毫秒。

本次保留前台编码、原前序读取、规范编码及责任写入；只对可证明往返语义的类型准备独立快照，规范字节与本次写入逐字一致才复用。不能证明时回退原读取/解码。指纹/责任ID没有改成任意前台字节的hash。类型计划在绑定时构造，无跨事务对象缓存、业务特例或新增队列。

生产文件：sharedbadger_operations.go（传递当前实际事务的新快照）；sharedbadger.go（保留旧ensureSyncQueueEntry兼容入口）；writebehind_disposition_store.go（复用准备快照/规范字节或原读取）；writebehind_disposition_runtime.go（绑定固定类型复制计划、包内局部诊断）；writebehind_snapshot_copy.go（独立复制及逐字门禁）。

## 已固定的RED与边界

真实ReliableWriteStore/临时Badger、SyncWrites/DetectConflicts=true：两项SaveBatch有2次新wrapper完整回读，期望0的断言真实失败(exit1)；旧字节/FP/ID断言通过。候选须将该计数变0，不能只编译通过。

新测试验证自定义codec仍经原解码、浮点表示等字节不一致回退、忽略字段/omitempty/null/UTF-8/time往返、可变载荷及同批同key改组快照隔离。匿名指针全省略和指向nil切片/多级指针的候选失败原件保留；不以改写身份期望掩盖。既有TooBig/部分前缀/事务回滚/未知结果/旧确认/关闭/本地清理/进程恢复继续回归。

本地证据根：/private/tmp/core-txn-reuse-20261111；原日志与command.json保留命令/退出码。最终普通/race、兼容、门禁与固定二进制性能结果如下；更晚的干净提交及合main正式门禁以发布回执为准。早期性能窗口存在本轮编译/测试干扰且候选非最终版，仅作探索；不能混入正式对照。

## 接入及边界

无需业务改动或新增参数；原StoreID/policy、卷格式、attempt/组屏障、提交后成功响应及IsSyncAfterDelete保持。当前进程绑定后自动判断，旧/自定义入口仍按原读取。v1.9.x及之后同代格式停稳后往返验证不替代更早卷单向格式迁移；不得删mode、pending或历史。

NOT_RUN：Bitzoom/Full-E4的接入、部署、业务UAT和容量；真实业务SQL事实核验；任意断电及Badger全部硬杀窗口；未采集的OS fsync/msync次数、硬CPU限流与I/O独占时长。不修改消费方配置，正式SyncWrites=true默认保持，false实验不作为可靠性通过。

## 实际验证与失败原件

所有shell按RTK代理执行；原command.json含精确argv、cwd、退出码和墙钟。测试/构建墙钟不是函数性能。

| 检查 | 实际命令（省略公共rtk proxy前缀） | 退出码 | 墙钟秒 |
|---|---|---:|---:|
| red | `go test ./pkg/persistence/database/nosql -run ^TestTransactionSnapshotReuse -v -count=1 -timeout 60s` | 1 | 7.219 |
| baseline-reviewed-red | `/private/tmp/core-txn-reuse-20261111/baseline-final.test -test.run=^TestTransactionSnapshotReuseAvoidsReadback$ -test.v -test.count=1 -test.timeout=30s` | 1 | 0.837 |
| green-final | `go test ./pkg/persistence/database/nosql -run ^TestTransactionSnapshotReuse -v -count=3 -timeout 90s` | 0 | 6.232 |
| snapshot-reviewed-race | `go test -race ./pkg/persistence/database/nosql -run ^TestTransactionSnapshotReuse -v -count=3 -timeout 90s` | 0 | 12.383 |
| nosql-reviewed | `go test ./pkg/persistence/database/nosql -v -count=1 -timeout 300s` | 0 | 33.642 |
| nosql-reviewed-race | `go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout 300s` | 0 | 53.592 |
| examples | `go test ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s` | 0 | 18.473 |
| examples-race | `go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 300s` | 0 | 21.318 |
| persistence-unit | `./scripts/test.sh persistence-unit` | 0 | 30.932 |
| config-contract | `./scripts/test.sh config-contract` | 0 | 9.104 |
| performance-contract | `./scripts/test.sh performance-contract` | 0 | 6.886 |
| release-contract | `./scripts/test.sh release-contract` | 0 | 55.529 |
| vet-fixed | `go vet ./pkg/persistence/database/nosql` | 0 | 0.859 |
| skill | `./scripts/check-ai-skill.sh` | 0 | 0.999 |
| logging | `./scripts/check-logging.sh` | 0 | 0.310 |

全nosql普通与race各253个顶层PASS；外部MySQL用例及显式性能/跨格式专用夹具默认SKIP，不能算真实外部DB验证。显式同代卷兼容及性能在本轮另行执行；子进程helper在默认包运行中SKIP，但父测试确实启动并检查真实进程。

受控硬杀覆盖target成功未确认、确认后未清理、Save结果未知三个窗口：普通日志PID23269→23270、23275→23276、23279→23297，race为24241→24245、24251→24253、24300→24301；均新进程重开原合成目录、无新Save继续恢复。交接前/后硬杀及target屏障恢复也通过。该结果不覆盖Badger任意硬杀/断电的既有风险。

同代卷双向兼容复用TestDispositionGroupVolumeCompatibility：旧路径seed→候选update→旧路径verify，以及候选seed→旧路径update→候选verify六条独立进程命令均exit0，保留32业务副本、原pending/改组及80条轻量历史；不重新生成消费方数据。原StoreID/policy不变，不构成v1.7.1旧卷迁移许可。

保留的FAIL包括：原路径两次回读RED；初候选过度排除*time.Time导致未复用；匿名指针全省略与null指针链的真实语义RED；字节保护后测试夹具仍误要求所有浮点表示均复用的FAIL（现明确不等字节必须回退）；go vet发现故意重复tag静态夹具（改为动态同等冲突类型后通过）。早期未导出匿名类型未命中预期路径的PASS不计作RED。没有删除原失败或降低原指纹/ID断言。

## 同机成本与持续窗口

Apple M3 Max/macOS darwin arm64，Go1.26.6、Badger4.9.6；真实磁盘临时库，GOMAXPROCS=2，进程可见16逻辑CPU，没有2CPU专用硬配额/1GiB内存硬限，不等同Full宿主。各次独立库，SyncWrites/DetectConflicts=true、原Production Options、128/1ms/CollectBacklog1/Queue1024不变。旧路径以精确v1.11.0四个生产文件overlay绑定，仅添加局部计数字段/hook以编译同一夹具；文件原hash/overlay及二进制hash一并封存。候选使用固定最终二进制，正式对照期间未并行本轮Go编译/回归。

批次成本基准：BenchmarkTransactionSnapshotReuse，128项×2028字节，100次/轮、三轮。旧路径5.978/5.937/6.278ms每128项批次，候选4.549/4.356/4.485ms；这是ApplyWriteOperations批次成本，不是逐笔Save P99。旧30392/30397/32444 allocs每批，候选30526/31336/31540，分配未稳定下降；B/op也存在Badger后台活动波动，不宣称内存收益。

性能复用既有TestConfirmationPerformanceComparison：每场景三轮×2000项短窗，另每模式40000操作/约20秒、2000个可合并状态key、50并发。稳定分组、交替改组、相邻组多版删除历史分别测三种模式。最终72组共828000次Save，错误0；每组计量外逐件匹配2000个业务载荷/组/同步标志和持久pending。无后台两模式仍保留2000 pending，不能写为排空成功。有后台全部最终pending=0，允许中间可合并版本合并，不等于40000次状态都逐条调用target。

### 有后台20秒持续输入（旧→新，单位ms，排空为s）

| 场景 | 平均 | P50 | P95 | P99 | 最大 | 排空s | 写入停止时pending |
|---|---:|---:|---:|---:|---:|---:|---:|
| 稳定分组 | 6.088→5.182 | 5.273→4.627 | 11.931→10.365 | 21.019→18.585 | 41.991→42.231 | 0.0193→0.0642 | 68→150 |
| 同key改组 | 5.707→5.177 | 5.257→4.738 | 10.684→9.545 | 17.523→15.883 | 33.657→33.010 | 0.0070→0.0171 | 50→49 |
| 保留删除历史 | 7.812→6.286 | 7.071→6.121 | 11.339→10.835 | 27.173→16.330 | 225.064→47.426 | 1.8941→3.4961 | 1529→1625 |

后台持续窗口P99三场景均下降，但排空均变长，历史场景1.894→3.496秒；实际target确认项/调用也随可合并版本时序改变，不能只看前台而声称整体效率都改善。历史场景target项14404→16769、用户态CPU20.362→22.288秒。排空仍在固定窗口内收敛，没有关闭流量后停摆；未为成绩调阈值、预算、FIFO或清历史。

### 三轮短窗P99中位数与持续窗口P99（旧→新，ms）

| 场景 | 模式 | 短窗P99中位数 | 持续P99 |
|---|---|---:|---:|
| 稳定分组 | plain_no_sync | 1.645→1.668 | 10.415→8.511 |
| 稳定分组 | bounded_no_sync | 4.054→3.523 | 15.017→14.728 |
| 稳定分组 | bounded_fast_sync | 5.831→5.619 | 21.019→18.585 |
| 同key改组 | plain_no_sync | 1.872→2.700 | 10.645→16.476 |
| 同key改组 | bounded_no_sync | 4.000→3.764 | 31.456→18.829 |
| 同key改组 | bounded_fast_sync | 6.300→9.689 | 17.523→15.883 |
| 保留删除历史 | plain_no_sync | 2.584→2.364 | 8.973→9.489 |
| 保留删除历史 | bounded_no_sync | 8.828→4.700 | 15.173→17.172 |
| 保留删除历史 | bounded_fast_sync | 4.740→3.829 | 27.173→16.330 |

改组有后台短窗P99中位数回退，历史无后台持续P99也回退；未优化的plain对照同样存在明显尾部差异，说明共享宿主/布局/调度噪声不能排除。没有一致性P99承诺、吞吐极限或长期稳态认证。固定输入约2000操作/s得到约1999.68–2002.24完成/s，不能据此宣称最大吞吐提高。

### 累计阶段口径

原始72份result及performance-summary.json保留Admission、ForegroundLocal/MetadataLocal/ConfirmationLocal的排队/锁/Update及批次、target、CPU/GC/RSS、实际Options与pending趋势。Foreground.QueueWait按操作累计，除以Items；CommitDuration除以Attempts仅得每事务Update均值，包含编码及Badger提交，不能除以批项当某笔独占。Metadata/Confirmation累计可能与前台等待/target执行嵌套或并行，不能相加冒充请求壁钟。

没有额外采集逐单编码/Badger提交纯分段、OS fsync/msync/独占I/O与硬限流，相关字段保留null；不将其填0。RSS为本机进程累计高水位原始单位，不宣称每case新增内存。早期候选/有本轮编译干扰的72组保留为探索证据，不纳入上述最终表。

## 升级与正式交付

```bash
go get github.com/digitalwayhk/core@v1.11.1
```

继续原UseWriteBehindWithFailureHandler及Save/SaveBatch，无需应用开关。消费方AI应重新链接精确v1.11.1权威skill，go get不会自动安装skill；见CONSUMER_AI_SKILL_SETUP.md。复杂/自定义codec类型可能全部回退而没有此项收益，但原合同保持。字节不一致的动态回退发生在复制和规范编码之后，因此还可能有额外准备成本，不是零开销；本轮不能证明所有模型都受益。正式source.diff/验证包/精确提交和官方Go代理SUM见v1.11.1 Release交付回执。未操作Bitzoom/Full-E4，消费方接入/部署/UAT/容量仍NOT_RUN。

## 证据交付

本库附[脱敏验证包](evidence/transaction-snapshot-reuse-20261111/evidence.tar.gz)，包含source.diff、源码/二进制绑定、精确命令/退出码、RED原件、最终72组完整指标、自审及普通/race/恢复摘要；source.diff为5个生产文件，SHA256 `bda7ec54bf7d9b805aa4f82ffbc6780fa0d6376674415b5e04fa8eeb5bf6d27e`。完整日志保留本地证据根，manifest给出path/bytes/SHA256；不公开其他测试对象dump，未复制消费方原始trace/凭据或业务库存。

本轮合成性能/兼容数据库在各自进程真实关闭及结果封存后回收，旧失败日志不覆盖。正式main/tag、官方Go代理/SUM与公开附件下载验证由Release的release-receipt.json提供，不从本文件自引用提交号。
