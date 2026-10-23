# Write-behind 部分进展调度修复与验收

日期：2026-10-23。诊断基线为已发布 `v1.4.7` / `81c472c2f8a1d1bea93684f8f83435c8779e2a0e`，修复分支 `fix/writebehind-partial-progress`，补丁版本为 `v1.4.8`，正式发布状态以该 tag 与发布页为准。只修改 Core；既有 scaffold-vue 未提交内容保持原样，未修改 Bitzoom、连接 Full-E4、部署或清理现场数据。

## 根因与最小修改

真实调用链为 `syncAdaptive → processSyncQueue → processSyncQueueContext → syncBatchContext → syncBatchWithTargetContext → confirmSyncSuccess → batchConfirmSyncedStatus`。同步仍受 `syncExecMu` 互斥；target 的成功列表经原本地事务、快照时间及版本校验后，才形成返回给调度器的 confirmed 数，原 target/确认错误继续返回。

两个独立缺陷：

1. `adaptiveSyncSchedule.completed` 用 `err != nil || confirmed == 0` 决定失败退避，使已实际确认 120 条、另 8 条失败的轮次仍把整个 store 退避，最高到 10 秒。
2. `recordSyncMetrics` 在增加 `Failures` 后立即 return，同轮的真实确认数没有进入 `SyncedItems`。

生产代码只改三个函数：

| 文件 / 函数 | 修改 |
| --- | --- |
| `adaptive_sync_schedule.go` / `completed` | 仅零确认进入原指数退避；有确认进展重置退避并按现有 `SyncBacklogDrainDelay` 排空，错误参数仍沿调用链保留 |
| `sharedbadger.go` / `recordSyncMetrics` | Attempts、Failures、SyncedItems 独立累计；LastSuccessAt 仅无错误且有确认时更新，不抹掉历史成功时间 |
| `sharedbadger_adaptive.go` / `syncAdaptive` | 复用原 worker 轮次边界记录 confirmed、has_error、next_wait_ms、progress/no_progress；有错保留原 Error 事件，无错使用 Debug 事件 |

没有新增配置、公开 API、线程、依赖或 lane；`sharedbadger_force_sync.go`、target 确认、同步索引及版本校验未修改。ForceSync 遇错仍返回原错误，失败 pending 无 TTL，关闭有 pending 仍明确报错。

## RED 与 GREEN

先只加入测试，生产仍为 v1.4.7，再运行定向命令得到真实 RED。旧 `TestAdaptiveSchedule` 的「部分确认 + error 等 200ms」原件另行保留，新断言与新用例先在旧实现上失败，未通过吞错或削弱断言得到 GREEN。

| 场景 | 原实现 | 修复后 |
| --- | --- | --- |
| 先退避到 10s，再确认 120 条且有错，drain 为 0 / 25ms | FAIL，实际仍 10s | PASS，按 0 / 25ms 调度且退避复位 |
| 真实临时 Badger、UseWriteBehind、自动 worker，两批各 120 健康 + 8 持续失败 | FAIL，首轮确认后 1s 内下一批未开始，原最小退避 2s | PASS，两批自动推进；8 条持久 pending 与缓存计数一致 |
| 健康输入耗尽、100 次新通知及原失败 key 更新 | 原有零进展测试保留 | PASS，零确认尾部仍等至少 2s；解除故障后原 8 key 自动确认，最终确认累计 248、失败轮次 3 |
| 全失败、nil error 但零确认、非零 drain | 原有回归保留 | PASS，零进展退避且通知不绕过等待 |
| 本地确认事务失败、重复 key、并发写入新版本 | 本地确认错误与新版本原契约保持 | PASS，确认失败/新版本实际 0，重复 key 实际仅 1；pending 与恢复事实保留 |
| 部分成功后关闭和同目录重启 | 原数据语义保持 | PASS，Close 报 PendingSyncError(8)，重新打开同一卷仍有 8 条，自动恢复到 0 |
| 同轮确认 120 且 error | FAIL，SyncedItems 遗漏 | PASS，Attempts+1、Failures+1、SyncedItems+120，LastSuccessAt 不前移 |
| 8 个失败项占满 batch=8，后面有健康项 | 边界测试 PASS，健康项未被选中 | 同样 PASS，仍复现该限制；解除前缀故障后自动收敛 |

自动推进验收没有使用手动 ForceSync。测试中的 1s 等待用于区分受控的 2s 失败退避与 drain，不是新增业务端到端 SLO。target 是返回确定成功/失败集合的测试实现，本地队列、确认事务、worker、关闭和恢复均使用真实临时 Badger；没有将它表述为真实 MySQL 验收。

首次扩展 RED 中还出现夹具清理错误：直接关闭底层 Badger 后，manager 清理再次 Sync 导致 panic。已改为正式 `CloseSharedManager` 注入本地确认失败，最终 RED 不再含该 panic。原 `red-extended` 日志保留，此夹具错误没有计作 Core 业务缺陷。

## 实际命令与结果

环境：Go 1.26.6，darwin/arm64。所有 shell 以 `rtk` 开头。以下命令均由 `rtk proxy` 执行，并经本轮命令记录器保留原始输出、退出码及壁钟时间；未设置外部集成开关或专用数据库地址。

定向 RED / GREEN 使用相同命令：

```bash
rtk proxy go test -v ./pkg/persistence/database/nosql \
  -run 'TestAdaptive(Schedule|PartialProgress.*|FailedPrefixWindowBoundary)$|TestWriteBehind(PartialRoundCountsAndErrors|ProgressUsesLocalConfirmation)$|TestSyncMetricsRecordConvergence' \
  -count=1 -timeout=60s
```

| 命令 | 退出码 | 壁钟秒数 | 结论 |
| --- | --- | --- | --- |
| 上述定向测试，修复前 `red-final` | 1 | 8.100 | RED：10s 退避、自动 worker 跨轮等待、部分确认指标遗漏 |
| 上述定向测试，最小修复后 `green` | 0 | 9.779 | GREEN |
| `go test ./pkg/persistence/database/nosql -count=1 -timeout 300s` | 0 | 16.683 | 普通回归 PASS，外部依赖跳过另列 |
| `go test -race ./pkg/persistence/database/nosql -count=1 -timeout 300s` | 0 | 26.206 | race PASS，外部依赖跳过另列 |
| `./scripts/test.sh persistence-unit` | 0 | 18.091 | 持久化回归 PASS，外部依赖跳过另列 |
| `./scripts/test.sh config-contract` | 0 | 10.455 | 配置契约 PASS，不代表外部 Broker 集成通过 |
| `./scripts/test.sh performance-contract` | 0 | 7.073 | SQLite 配置测试及相关包编译 PASS；该入口不运行吞吐 benchmark |
| `./scripts/test.sh release-contract` | 0 | 51.248 | API、公共签名、配置及安全等候选发布契约 PASS |
| `go test -json ./pkg/persistence/... -count=1 -timeout=300s` | 0 | 18.065 | 为列明跳过项补采：393 条测试/子测试 PASS 记录，39 条 SKIP |

完整命令数组、各轮结果和原始日志见[证据目录](evidence/writebehind-partial-progress-20261023/commands.json)。日志以 gzip 无损封存，`commands.json` 中保存解压后原始 SHA256；[manifest](evidence/writebehind-partial-progress-20261023/manifest.json) 校验封存文件，`source-binding.json` 绑定最终六个源码/测试文件。首轮 RED、扩展夹具失败、最终 RED 均保留，不以最终 GREEN 覆盖原件。首次定向 RED/GREEN 后仅调整测试 import 分组，普通/race/持久化清单均已实际执行。

## 观测结果和兼容边界

[轮次观测原件](evidence/writebehind-partial-progress-20261023/observations.json)来自 GREEN 日志：

- `confirmed=120, has_error=true, reason=progress`：`next_wait_ms` 分别为 0、25，原失败 Error 日志仍存在。
- `confirmed=0, has_error=true, reason=no_progress`：`next_wait_ms=2000`，只剩失败项重新退避。
- 恢复后确认原 8 条，`has_error=false`；空队列的 next_wait 为原空闲唤醒间隔，不是积压等待。

等待字段是轮次结束时快照，不等于实际下一轮或业务端到端延迟。未启用 Debug 的环境不会采到无错轮次，不能把缺失当零。新观测复用原 store prefix，不使用用户、订单、payload、SQL 或凭据作字段，不新增采样线程。

三项自适应配置全零仍走旧模式；只启用了原自适应调度的 store 改变部分成功后的等待策略。指标修复适用于所有进入原同步记录函数的轮次。存储格式、原始 key、版本确认、pending 可靠保存、执行互斥及手动 ForceSync 错误契约不变，无需清库、迁移 pending 或重建业务数据。

**剩余队头边界：** `getUnsyncedBatch` 仍只选索引前缀窗口。失败项占满窗口时，后面的健康项仍等待，零确认仍退避。本轮没有证明所有失败 key 公平隔离，也不保证应用 target 内同用户 chunk 的业务排序。

## 自审与 NOT_RUN

当前主 Agent 按正确性、并发/确认安全、兼容、观测和最小改动自审，遵守仓库禁止子 Agent 约束，不宣称独立第三方审计。审查确认生产 diff 仅三个指定函数，保留错误链、原同步互斥、关闭和版本校验；没有待修的本轮范围内阻断问题。

- 39 条持久化外部依赖 SKIP 全部记为 **NOT_RUN**，逐项见 [skip 清单](evidence/writebehind-partial-progress-20261023/skip-inventory.json)，包括真实 MySQL 自适应对照、MySQL 同步/并发/路由、连接池恢复和消息 Inbox 集成。包退出 0 不代表这些场景通过。
- Redis/MQ、WS、持续归属等真实外部集成本轮 **NOT_RUN**；相关生产源码未修改，配置/发布契约中的局部测试不替代其真实集成。
- 现场原始 R88 日志和 145.659 秒窗口未重新核验；现场数字来自用户提供计划。本轮独立复现的是框架机制，不宣称重现相同业务窗口或量化现场改善比例。
- Bitzoom 升级、真实业务路由/MySQL、历史成交、业务 chunk 排序、重启 UAT、Full-E4 和容量均 **NOT_RUN**。Core 验证通过不代表消费方通过。

消费方接入应锁定正式补丁版本、读取更新后的 [skill](../ai/core-skill/write-path-and-performance.md) 和 [同步指南](WRITE_BEHIND_SYNC_GUIDE.md)，继续使用现有 `ReliableWriteStore + UseWriteBehind`，分别验收自动 drain、失败尾部退避、原 pending 恢复和业务顺序。不得通过返回 nil、删除失败 pending、跳过未确认前序或清数据来制造收敛。
