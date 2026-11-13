# 生命周期 owner 容量刷新：修复与验收

2026-11-13。基线正式 v1.11.2（e9791d1cc35631c1f344d6d2b5b7e6897313df2c），兼容补丁候选 v1.11.3。精确main/tag、官方Go代理/SUM和最终干净提交门禁由正式Release交付回执绑定。仅Core；未操作Bitzoom/Full-E4。

## 已确认根因与证据边界

`MQManager.Publish → lifecycleController.allowPublish` 在调用Broker前使用容量门禁。`runOnce` 的non-owner只取轻量容量并保存独立capacity；owner完整Inspect超时却直接返回、不刷新容量。旧完整证据超过2×Interval后，owner发布会因容量证据陈旧返回unknown，即使同一Broker轻量容量仍可取得；standby此时可刷新。不是已证明硬容量已满，也不能把整轮deadline计数和每次unknown直接一一对应。

当前正式源码及Core自有夹具绑定在原件source-binding.json；没有从消费方私有文件复制测试或恢复业务实现。先前只读阶段的报告8651bytes/SHA256 ef86744f96285dca8d3893890855c900248914b6f563295b5433b68006b21013保留，随后收到正式续修授权才修改生产路径。

原RED固定Interval2s/整轮500ms/完整检查350ms/Hard256MiB，旧完整快照显式设5秒前。真实Redis的租约、TYPE/fingerprint/XLEN/MEMORY管线及MQManager.Publish真实运行；仅完整Inspect受控耗尽子预算。owner约350.343ms后轻量调用0、实际Publish被unknown阻断；standby约0.588ms刷新后实际发布成功，Stream仅728bytes、指纹不变、Reclaim0。controller和真实Redis均exit1；race无DATA RACE但相同断言FAIL，不能记race整体通过。

这证明一个Core路径缺口。R427/R428现场没有逐Subject完整拒绝理由/快照来源与年龄，不能声称每一次unknown、后续全部异步积压或350ms内部命令成本均已归因。本轮未复跑现场负载，预算是否足够仍需对真实操作成本测量。

## 最小实现及职责

生产文件共六个：

- `pkg/server/mq/lifecycle_controller.go`：内置owner完整检查可恢复deadline、父runCtx尚有余量且未取消时调用一次补读；完整错误继续返回并计数，不进入Reclaim。
- `lifecycle_capacity_refresh.go`：复用已有私有worker.inspectLifecycleCapacity，直接继承原runCtx截止时间，不另开Background或500ms；只在声明硬容量时工作。核对entry/policy、Subject/fingerprint、时间/状态、必需计数；提交前再核取消。成功仅写独立capacity，失败标记不可用，不能再拿旧容量冒充补读成功。原控制器完整snapshot/前沿/组证据保持；不重定义原Inspect自身已有的Provider操作。
- `lifecycle_worker.go`：语义/fence/元数据/取消优先于同时发生的deadline，避免先覆盖为超时再降级。legacy自定义Provider不补容量，保留原完整检查/phase优先级；固定内部失败分类按安全优先级记录。
- `lifecycle_capacity_diagnostics.go`：原entry内固定字段和6种拒绝计数，状态变化限频日志；不按消息或错误建立map，不在每次发布拒绝时访问Redis。
- `lifecycle_metrics.go`：加性固定原因和补读计数，独立容量失效时省略容量字段；完整pending/lag仍按自身ObservedAt判断。
- `pkg/server/observability/provider.go`：仅将上述9项固定名加入既有Collector白名单，不增加动态标签。

Provider的Redis/NATS实现、公共接口、ServiceContext/EventBridge订阅入口、消息身份和业务事务均未改；只复用内置Provider已有能力，没有新worker、队列或API。正常owner完整成功不额外读容量；non-owner仍只轻量读。整轮原预算不增加，完整检查失败后剩余回收预算可用于一次容量补读，仍不执行回收。

## 可靠性、兼容和资源边界

- policy/metadata/owner/fence/前沿回退及显式取消不触发降级；即使它们与deadline同时发生，原安全错误仍优先。补读本身失败、缺证据、过期或被取消仍unknown；真实hard reached仍ErrLifecycleBackpressure。
- 补读成功不代表完整组状态安全，不刷新SafeFrontier/Groups/pending/lag的时间，也不授权删除Broker消息。完整deadline计数仍可能增加，这是如实保留扫描故障。
- 一个失败owner轮次最多一次轻量补读；只有硬容量声明且整轮有余量时执行。500ms/70%检查配置下通常至多余约150ms，不另起超时预算。上下文deadline不是严格实时墙钟保证；调度、锁等待及不合作回调仍可能推迟实际退出，Core不强杀任意代码。
- 原2×Interval容量新鲜度、阈值、policy指纹、generation、组、原消息/PEL/游标、序号和幂等均不变。无迁移、不清消息、不修改业务参数；回退不涉及格式变化，但恢复旧owner容量缺口。
- 未实现内置私有协调的自定义Provider保持原读取路径。NATS共用此controller但本轮没有真实NATS Broker验收，必须单列NOT_RUN，不能用Redis推导全部Provider通过。

## 逐Subject诊断与计数口径

日志 `mq_lifecycle_publish_blocked` 仅记录登记Subject、固定reason、实际full/capacity来源、已知时才有snapshot_age_ms、last_round_role和full_read_error/capacity_read_error。角色表示最近轮次，不伪装当前租约仍有效。未观测年龄不输出0。日志同状态一次，状态变化也至少相隔原Interval；不输出payload、凭据、业务ID或原错误标签。

Prom仍只用既有稳定服务/组件/固定名：

- `publish_rejected_reason_{not_observed|stale|state|messages_missing|bytes_missing|hard_reached}_total`：发布容量门禁的固定原因，不能与包含Provider错误的publish_rejected_total或整轮reclaim_fail逐条等同。
- `capacity_refresh_{attempt|success|fail}_total`：补读尝试/成功/失败；完整扫描deadline和补读成功可以同轮并存。

诊断只在已有entry内用固定字段保存，不随业务消息或每次错误增长。没有新增公开诊断API、后台采样器、动态Subject/UserID/OrderID标签，实际Collector导出与动态名称过滤有独立回归。

## 实际RED→GREEN与安全回归

证据根 `/private/tmp/core-lifecycle-capacity-diagnosis-20261113`，command.json含实际argv/cwd/退出码/墙钟，原日志不覆盖。原只读overlay和生产源绑定、controller/真实Redis/race RED保持；正式回归移入Core自身测试后再次RED。6个观测原因在生产修改前也得到实际断言RED，不以编译失败或零观察量当RED。

候选原controller和真实Redis断言GREEN；真实Redis三轮owner约350.8–352.1ms后补一次容量并能实际发布，完整deadline仍返回、Reclaim0，standby照常。旧完整时间/前沿保持，pending/lag未被伪造新鲜。Stream容量在发布后增长，不能把补读时的728bytes当成持续最新值。

边界矩阵包括：真实hard bytes/messages、缺字段/负数/旧时间/错身份、补读错误、语义错误与deadline竞态、显式取消、迟到成功与实际回调退出、entry/policy替换、legacy、正常完整成功与无硬限不加读、固定原因及日志节流。首候选在取消+deadline组合中误触发补读，真实FAIL原件保留；修正取消优先后原断言GREEN，没有降低保护。

原500ms预算测试在完整ctx耗尽350ms后，轻量ctx截止仅比完整ctx迟最多150ms，不从调用时再得500ms。真实Redis在完整检查超时后暂停300ms，补读进入时约149.132ms余量，实际整轮约500.936ms，仍unknown，原错误保留、零回收。

多Subject真实Redis：8Subject/2controller逐轮受控完整超时，owner_light8/standby_light8/full8/reclaim0，TYPE/XLEN/MEMORY USAGE增量各16，证明每失败owner只补一组原4命令管线；HGET亦为同管线的策略证据。最大owner补读余量149.916ms，最慢owner352.381ms，总窗口2.816s。正常13×44既有命令放大测试另行回归；不将此故障功能窗口当稳态吞吐或业务P99认证。真实Redis独占、前后相同策略/预算；没有硬CPU/I/O独占限额或现场同档机器对照，未观测的CPU限流/IO/逐请求分位数不填0。

## 升级

```bash
go get github.com/digitalwayhk/core@v1.11.3
```

继续原RequireMessageLifecycle、UseOutbox、SubscribeEvent接线，没有新开关；不要因本次修复更改subject/group/fingerprint或重建消费组。AI消费方按README/CONSUMER_AI_SKILL_SETUP重新链接精确版本权威skill，go get不会自动安装。

NOT_RUN：消费方接入/部署、Bitzoom/Full-E4功能/UAT/容量、R427/R428每次unknown精确归因、真实业务MySQL、NATS等未配置外部Broker、任意物理断电、长期吞吐与全部业务延迟。Core测试通过不表示消费方已经接入。

## 实际命令、普通/race与原件

下表命令均通过 `rtk proxy` 执行；墙钟含编译/工具成本，不是函数时延。独占Redis地址63713属于本轮新fixture；只读原RED使用61288，均不连接消费方。

| 原件名称 | 实际命令（省略代理） | exit | 命令墙钟秒 |
|---|---|---:|---:|
| controller-red | `go test -overlay=<证据根>/overlay.json ./pkg/server/mq -run ^TestCapacityRefreshDiagnosticController$ -v -count=1 -timeout 30s` | 1 | 4.665 |
| redis-red | `env CORE_TEST_REDIS_ADDR=127.0.0.1:61288 go test -overlay=<证据根>/overlay.json ./pkg/server/mq -run ^TestCapacityRefreshDiagnosticRedisOwnerStandby$ -v -count=1 -timeout 30s` | 1 | 3.628 |
| diagnosis-race-red | `env CORE_TEST_REDIS_ADDR=127.0.0.1:61288 go test -race -overlay=<证据根>/overlay.json ./pkg/server/mq -run ^TestCapacityRefreshDiagnostic -v -count=1 -timeout 30s` | 1 | 6.028 |
| repository-red | `go test ./pkg/server/mq -run ^TestCapacityRefreshDiagnosticController$ -v -count=1 -timeout 30s` | 1 | 4.314 |
| diagnostics-red | `go test ./pkg/server/mq -run ^TestLifecycleCapacityRejectReasons$ -v -count=1 -timeout 30s` | 1 | 3.191 |
| first-green | `go test ./pkg/server/mq -run TestCapacityRefreshDiagnosticController|TestLifecycleCapacityRejectReasons|TestLifecycle(SlowInspect|WorkerFailureReasons|NonOwner|ReclaimHasIndependent) -v -count=1 -timeout 45s` | 0 | 4.754 |
| boundaries | `go test ./pkg/server/mq -run TestOwnerCapacityRefresh|TestLifecycleCapacity|TestCapacityRefreshDiagnosticController -v -count=1 -timeout 45s` | 1 | 7.469 |
| boundaries-fixed | `go test ./pkg/server/mq -run TestOwnerCapacityRefresh|TestLifecycleCapacity|TestCapacityRefreshDiagnosticController -v -count=1 -timeout 45s` | 0 | 6.106 |
| redis-green | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 go test ./pkg/server/mq -run ^TestCapacityRefreshDiagnosticRedisOwnerStandby$ -v -count=3 -timeout 30s` | 0 | 4.558 |
| targeted-real | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 go test -p 1 ./pkg/server/mq ./pkg/server/observability -run TestOwnerCapacityRefresh|TestLifecycleCapacity|TestCapacityRefreshDiagnostic|TestRedisOwnerCapacityRefresh|TestCollectorExportsCapacity -v -count=1 -timeout 60s` | 0 | 12.125 |
| real-budget | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 go test ./pkg/server/mq -run ^TestRedisOwnerCapacityRefreshExhaustsRemainingBudget$ -v -count=1 -timeout 30s` | 0 | 3.241 |
| packages-normal | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 go test -p 1 ./pkg/server/event ./pkg/server/router ./pkg/server/mq ./pkg/server/observability -v -count=1 -timeout 600s` | 0 | 84.651 |
| packages-race-native | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 CORE_TEST_SHARED_FULL_LEASE=1 go test -race -p 1 ./pkg/server/event ./pkg/server/router ./pkg/server/mq ./pkg/server/observability -v -count=1 -timeout 600s` | 0 | 345.995 |
| classification-lock-red | `go test ./pkg/server/mq -run ^TestLifecycleReadClassificationDoesNotHoldControllerLock$ -v -count=1 -timeout 30s` | 1 | 3.439 |
| packages-final-normal | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 go test -p 1 ./pkg/server/event ./pkg/server/router ./pkg/server/mq ./pkg/server/observability -v -count=1 -timeout 600s` | 0 | 85.625 |
| packages-final-race | `env CORE_TEST_REDIS_ADDR=127.0.0.1:63713 CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 go test -race -p 1 ./pkg/server/event ./pkg/server/router ./pkg/server/mq ./pkg/server/observability -v -count=1 -timeout 600s` | 0 | 103.825 |
| config-contract | `./scripts/test.sh config-contract` | 0 | 10.958 |
| performance-contract | `./scripts/test.sh performance-contract` | 0 | 11.056 |
| release-contract | `./scripts/test.sh release-contract` | 0 | 60.465 |
| skill | `./scripts/check-ai-skill.sh` | 0 | 1.203 |
| logging | `./scripts/check-logging.sh` | 0 | 0.373 |
| vet | `go vet ./pkg/server/mq ./pkg/server/observability` | 0 | 0.600 |

最终普通和race各470个顶层PASS/29SKIP/0FAIL，无DATA RACE；包含最终错误分类锁边界修正及真实Redis余量耗尽用例。前轮显式原生长租约race为471PASS/27SKIP/0FAIL：硬杀PID32121→接管32122，119.997s；SharedKeys硬杀33772→接管33773，119.995s。该前轮在最后把Error分类移到锁外之前；锁边界修正后完整普通/race再跑，两个长租约用例未重复启用，不能把它们写成最终默认运行中没有跳过。持续归属原生15s硬杀恢复最终普通15.303s/race16.368s，真实原组/Stream保留；进程入口helper的默认SKIP不否定父测试确实启动了真实子进程。

NATS未配置真实Broker用例均SKIP；独立性能测量等显式开关未运行也不算PASS。真实Badger Outbox发布后未确认的硬杀/新进程恢复、SQLite Inbox事务幂等及Outbox持续健康重试普通/race继续通过；本轮没有真实MySQL或消费方数据验证。

最终8Subject窗口普通owner最慢352.216ms、race351.906ms；命令增量仍各16，owner补读余量分别149.904/149.834ms。真实Redis暂停300ms后的原500ms上下文整轮普通501.347ms、race502.154ms；余量149.144/147.588ms。调度和退出开销造成墙钟略超截止，不承诺严格实时500ms，更不把它换算成HTTP或业务P99。

13controller×44Subject×3轮的正常稳定期回归，完整Inspect132次，XINFO GROUPS增量132，0失败，未因本修复对成功owner增加一次容量读或让standby全组扫描。这里只证明该夹具的命令边界，不代表所有业务负载的稳态吞吐。

自审另外固定了“错误分类不能持controller全局锁执行外部error.Error”的候选RED，原断言FAIL保留；分类移到锁外后最终全包GREEN。所有本轮失败、旧只读报告/overlay、实际命令和源绑定均保留，不用最终PASS覆盖差轮。正式干净main门禁、tag与官方模块验真以发布回执绑定。

## 证据交付

[脱敏原件选集](evidence/lifecycle-owner-capacity-20261113/evidence.tar.gz)包含原始RED、候选FAIL、精确命令/源绑定、最终普通/race摘要及自审。 `source.diff` 19764 bytes，SHA256 `f0a14dcb4ae9c83247488344b61b07bec34c1281e7e847e1836ad590ba397ded`。 `evidence.tar.gz` 82925 bytes，SHA256 `e8808d5fe9f0fc08696336a58d0178342ec1de9ace5432f164ae0dcd296a75a7`。
