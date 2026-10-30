# 有界 write-behind 成功确认后的本地回收修复（2026-10-30）

## 结果与范围

修复 Core v1.6.0 在启用 `WriteBehindFailurePolicy` 后成功确认遗漏 `IsSyncAfterDelete` 的问题。基线为 `827a9f840f57f5b68c6481a380c8973f6c8d1467`，独立分支 `fix/writebehind-disposition-local-cleanup`。本报告与修复同一提交；精确提交可用 `git log -1 --format=%H -- pkg/persistence/database/nosql/writebehind_disposition_schedule.go` 查询。

只改 Core，不修改消费方、不部署、不发压、不发布版本。原有前端及文档 WIP 的 189 个文件以 SHA256 核对保留，未暂存进本修复。测试只使用本轮临时 Badger、现有隔离 SQLite 和 Core 测试进程；未改模块缓存中的依赖源码，禁用依赖下载和 go.mod 自动修改。

## 根因与最小改动

普通路径已经是：

`syncBatchWithTargetContext → collectSyncAfterDeleteCandidates → confirmSyncSuccess → 本地确认事务提交 → scheduleSyncAfterDelete → IsSyncAfterDelete → deleteSyncedKeyIfUnchanged`。

有界 target 路径为 `processDispositionRound → completeDisposition(..., WriteBehindCompleted)`；应用显式 Confirm 路径为 `applyDispositionDecision → completeDisposition(..., WriteBehindCompleted)`。两者原本都在本地事务中核对版本/指纹、标记同步并移除索引，外层成功后扣 pending，却没有调度清理。因此可以出现 `pending=0`、本地模型仍存在且 hook 调用为 0。这不构成业务数据丢失或 SQL 性能结论。

生产代码仅修改两个函数：

- `writebehind_disposition_schedule.go / processDispositionRound`：仅在确认事务成功且实际移除 pending 后，从原确认快照构造候选，交给已有 `scheduleSyncAfterDelete`。
- `writebehind_disposition_decision.go / applyDispositionDecision`：保存原成功确认快照；仅在事务提交成功且 `applied=true` 时调度。接管即使移除技术 pending，也不会走这一成功分支。

`completeDisposition`、确认事务、版本/指纹校验、pending 计数、失败历史、重试预算、顺序屏障、普通模式和实际删除算法均不改。未新增公开 API、配置、后台线程或持久队列。

## RED → GREEN

消费方提供的 `d1baafb1543dd42d010daaa05a03c39cf3340819` 及两个文件、两份原始日志仅作只读参考；未在 Bitzoom 执行命令或改写证据。Core 自身测试 `writebehind_local_cleanup_test.go` 不带诊断环境开关、不会默认 SKIP：使用真实 `ReliableWriteStore`、临时 Badger、原子 hook 计数和明确的 target/应用确认。

首次生产修复前运行 `TestWriteBehindLocalCleanupContract`：

| 模式 | 手动/自动 × hook=false/true | RED | GREEN |
| --- | --- | --- | --- |
| 普通 target | 4 个组合 | 全通过；hook=1 | 全通过；hook=1 |
| 有界 target 成功 | 4 个组合 | 全失败；pending=0、hook=0 | 全通过；hook=1 |
| 有界应用 Confirm 回调 | 4 个组合 | 全失败；pending=0、hook=0 | 全通过；hook=1 |

原始 RED 命令退出码 1。日志包含 8 个失败子用例及失败父用例；不能把父用例再算成第 9 个场景。修复后同一文件、相同一秒等待和断言的 12 个组合全部通过；没有延长等待掩盖遗漏。

原始 RED：`.temp/writebehind-local-cleanup-20261030/red.log`，SHA256：`cad9ba79ac8a3c544da1f56ce90278b7f90794768d817be54e52d0205e4e1256`。最终 nosql race：`.temp/writebehind-local-cleanup-20261030/nosql-race.log`，SHA256：`4379f0ca8f3e3b3a0920f4fcf66f6e9bbfcfd8777bc7e1d02f51d39f61a06e35`。

早期环境检查曾将 `GOTOOLCHAIN=local` 用于系统 Go 1.26.0，因本仓库要求 1.26.6 而未执行测试，日志为 `toolchain-local-failed.log`，不计作逻辑 RED。随后直接使用本机已有的 Go 1.26.6；没有下载工具链。新增边界夹具首次直接关闭底层 Badger 后，夹具收尾又调用 manager Sync，产生了关闭流程 panic；保留在 `green-boundaries.log`。夹具改为通过原共享 manager 关闭后重新运行，`green-boundaries-v2.log` 通过；未为此修改生产生命周期或放宽断言。

## 验证覆盖

新增 `writebehind_local_cleanup_boundaries_test.go` 覆盖：

| 合同 | 测试 |
| --- | --- |
| 同 key false→true，后续版本须重新确认；target 与人工 Confirm；重复确认不重复扣数/调度 | `TestBoundedCleanupFalseThenTrue` |
| target 在途保存新版本，旧回执不能确认或回收新值 | `TestBoundedCleanupOldTargetCannotDeleteNewVersion` |
| 旧人工决定、错误决定指纹均不能回收新版本 | `TestBoundedCleanupOldDecisionAndFingerprint` |
| 时间戳不变但当前内容不同，指纹必须拒绝确认 | `TestBoundedCleanupCurrentFingerprintConflict` |
| hook 进入后并发 Save，新版本经原卷重开仍保留 pending | `TestBoundedCleanupNewSaveDuringHook` |
| 部分成功、重复成功 key、保留原 error/失败项/历史及真实计数 | `TestBoundedCleanupPartialSuccess` |
| target/人工确认前真实本地库不可用，不扣数、不清理，原卷恢复未同步记录 | `TestBoundedCleanupLocalConfirmationFailure` |
| 本地确认闭包已执行但 Badger Commit 真实冲突；tentative 结果不是提交成功 | `TestBoundedCleanupAbortedConfirmationTransaction` |
| 暂停、独立持久应用接管、Release 都不是业务成功 | `TestBoundedCleanupPauseHandoffAndRelease` |
| 未投递旧版本被 Superseded 后不调用旧 hook | `TestBoundedCleanupSupersededVersion` |
| 超时后迟到 Committed 回调不确认、不清理 | `TestBoundedCleanupLateConfirmation` |
| 普通/有界在确认提交到清理入队之间关闭，再开原 Badger，核对当前恢复边界 | `TestWriteBehindCleanupCommitCloseRestart` |

提交冲突测试用真实 Badger 事务与冲突写入，不用 Fake Badger；确认故障测试通过共享 manager 关闭真实本地库。应用接管测试先把完整快照提交到另一临时 SyncWrites Badger，再交回绑定原身份的回执。并发、超时和关闭测试使用通道、锁与生命周期屏障，不依赖猜测某个毫秒时点。

完整 nosql 回归还实际执行原有 `TestCrashBeforeAndAfterHandoffConfirmation` 的独立进程硬终止与恢复前后场景。它证明原有接管合同未回退，不是本次新增的持久本地清理保证。

## 实际命令与结果

全部 shell 使用 RTK。下列命令通过 `.temp/writebehind-local-cleanup-20261030/run-check.py` 执行，实际参数、统一环境、退出码和耗时逐项保存于 `*-command.json`。

统一使用已安装工具链目录 `/Users/vincent/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin` 优先于 PATH；设置 `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`。完整回归前清除继承的外部数据库/集成开关，避免误连接其他项目环境。耗时为命令壁钟（含编译），不是业务性能。

| 实际检查 | 退出码 | 命令耗时 |
| --- | ---: | ---: |
| 修复前 12 组合 RED | 1 | 16.12s |
| 同一 12 组合 GREEN | 0 | 85.13s |
| 新增边界测试 | 0 | 8.42s |
| 完整 nosql 普通 | 0 | 25.08s |
| 完整 nosql race | 0 | 60.50s |
| 04/07 普通 | 0 | 33.97s |
| 04/07 race | 0 | 47.47s |
| persistence-unit | 0 | 21.66s |
| performance-contract | 0 | 9.41s |
| config-contract | 0 | 6.69s |
| quick | 0 | 11.86s |
| security | 0 | 22.44s |
| release-contract | 0 | 73.58s |
| web-dist-sync | 0 | 5.90s |
| 日志门禁 | 0 | 0.42s |

对应可复验命令：

```bash
rtk proxy go test ./pkg/persistence/database/nosql -run '^TestWriteBehindLocalCleanupContract$' -v -count=1 -timeout 60s
rtk proxy go test ./pkg/persistence/database/nosql -run '^(TestBoundedCleanup|TestWriteBehindCleanup|TestWriteBehindLocalCleanup)' -v -count=1 -timeout 90s
rtk proxy go test ./pkg/persistence/database/nosql -v -count=1 -timeout 300s
rtk proxy go test -race ./pkg/persistence/database/nosql -v -count=1 -timeout 300s
rtk proxy go test ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout 300s
rtk proxy go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -v -count=1 -timeout 300s
rtk proxy bash scripts/test.sh persistence-unit
rtk proxy bash scripts/test.sh performance-contract
rtk proxy bash scripts/test.sh config-contract
rtk proxy bash scripts/test.sh quick
rtk proxy bash scripts/test.sh security
rtk proxy bash scripts/test.sh release-contract
rtk proxy bash scripts/test.sh web-dist-sync
rtk proxy bash scripts/check-logging.sh
```

最终 nosql 普通/race 各记录 300 条 PASS（含子测试）、0 FAIL、33 SKIP；race 无 `WARNING: DATA RACE`。04/07 普通/race 各记录 122 条 PASS（含子测试）、0 FAIL、14 SKIP。这些统计不把 SKIP 算成通过。

## 兼容、升级与剩余边界

- 消费方继续使用原绑定入口与 `IsSyncAfterDelete`，无需新 API 或额外 worker。本修复未发布 tag，不把分支可用冒称消费方已经升级。
- 有界模式修复后开始实际执行 hook；消费方必须核对返回 true 的业务条件，不能依赖旧漏洞保留副本。false 继续保留；新版本只有自己的成功确认才可清理。
- 清理对象是本地模型副本，不是远端事实、tombstone、失败快照或完成历史。所有失败/完成历史的保留政策不变；不声称解决历史长期增长。
- **现有清理队列不持久化。** 确认提交后、清理入队或执行完成前退出，重启不自动补删，也不为此重发已经确认的 target。`pending=0`、关闭成功和物理清理完成不是同一事实。普通/有界真实原卷重开测试固定并保留这一边界；本次没有扩大为新的持久回收机制。
- v1.6.0 已经确认但因遗漏残留的存量模型不会自动回收。需另行制定应用认可的处置方案；不能通过 `PurgeLocal`、`Delete`、清 pending 或清失败历史绕过删除条件。

## NOT_RUN 与审查

- 外部 MySQL/MongoDB/Redis、真实业务环境、Bitzoom/Full-E4、UAT、容量、部署与发布均 NOT_RUN。
- nosql 的 33 个 SKIP 中，32 个需要外部 MySQL，另一个是仅供父测试启动的 `TestDispositionCrashProcess` 入口；父测试的真实子进程故障场景已执行。
- 04/07 的 14 个 SKIP 是原生对照外部数据库/崩溃入口与 MySQL 规则/幂等/Outbox 测试；完整跳过名称保留在普通及 race 日志中。没有用 mock 结果替代真实外部依赖验收。
- 本次已运行的包与门禁之外，没有执行全仓库 `go test ./...` 或全仓库 race。新增关闭窗口测试为受控关闭并重开真实存储，不宣称已穷举此窗口的所有 OS 硬崩溃时序。
- 遵守仓库禁止子 Agent 的约束，由主 Agent 直接按正确性、简洁性、架构、安全与资源五个维度复核，没有宣称独立外部审计。生产只新增两处既有队列接线；确认事务内不执行 hook，只有提交后的实际成功分支持有清理候选，非成功状态不能进入。公开 API/配置/存储格式未变化，模块依赖文件未变化。

原始命令和日志、内容 SHA256、消费方只读证据指纹、WIP 指纹位于 `.temp/writebehind-local-cleanup-20261030/verification-manifest.json` 及同目录其他文件，保留初次失败，不用重跑覆盖。Core 本轮通过不代表消费方功能、UAT 或容量通过。
