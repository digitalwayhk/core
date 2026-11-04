# v1.10.0 同进程延后失败处置原错误链

本能力用于 `ReliableWriteStore` / `PrefixedBadgerDB` 的显式有界 write-behind。基线为 v1.9.1（`73db92eec6c7cc8aadb64eb01d1d67bef0870d80`），不涉及 Redis Provider、持续归属、业务 SQL 或消费方应用。正式 main/tag 和官方模块验真结果由同版本 release-receipt.json 绑定；本文件不以组件通过宣称消费方已接入。

## 原因与修改

原调用链：target 返回 error → `commitDispositionResults` 保存 Unknown → `dispatchDisposition`。固定 worker 和等容量 jobs 满时，后者直接返回；后续只选出旧 Unknown 的轮次没有原 target error，worker只能从 ErrorMessage 创建普通错误。进程没有重启也可能丢类型。旧指南只保证及时准入可保留，消费方把所有同进程回调都视为保留原链，是尚未闭合的接入假设。

此次增强仍走原 worker、jobs、选批和可靠确认，增加一个进程内有限引用表：

- `writebehind_deferred_cause.go`：完整尝试身份、保留/转交/失效/关闭和有限点读核验。
- `writebehind_disposition_schedule.go/processDispositionRound`：只有本轮已提交的 Unknown 通知与原选批身份完全吻合，才保留原 target error；旧 Unknown 不继承本轮另一批 error，本地未提交不发布引用。
- `writebehind_disposition_runtime.go/dispatchDisposition`：重读真实持久状态后，按 ID/版本/指纹/target Attempts/BatchID 精确取出，转给原 job；不能让旧通知给后次尝试提供旧证明。
- `writebehind_disposition_decision.go/applyDispositionDecision`：真实处置提交后按原身份释放引用，旧失效通知不清除新尝试的引用。
- `sharedbadger.go/CloseWithTimeout`：关闭未准入引用保留；原handler实际退出、关闭超时和pending错误合同保持。
- `writebehind_failure.go`、`writebehind_disposition_store.go`：现有 disposition 查询新增三个观测字段。

原 32 组失败回归移入 `writebehind_deferred_cause_test.go`；边界见同前缀 boundaries_test.go、真实进程恢复见 process_test.go。不新增业务模型、提交器、网络入口、后台线程或恢复框架。

## 预算和失效

未准入引用上限复用原 `ScanLimit`，有效范围 1–65536；每轮至多核对这些引用。不新增配置或冻结 policy 字段，不改变扫描上限、同步批次、重试次数或期限。原 N 个 worker 与容量 N 的 jobs 仍固定，已准入/在途至多另有 2N 个 job 引用。

满额拒绝新保留，不驱逐其他有效原链、不扩容 worker、不阻止无关健康 target。原件继续 Unknown，后续只有文本说明时应用必须 Resolve/Pause。`CauseRetained` 是未准入引用数，`CauseCapacity` 是引用上限，`CauseOverflow` 是本进程保留拒绝累计次数；不是 payload bytes、业务结果、错误消息总数或 pending 数量。任意 error 可能引用应用对象，Core 不序列化它、也不声称能给出精确内存字节上限；应用在返回后不得并发修改其错误链。

本地 Unknown 保存失败、无原 error、新 attempt/批次/版本失配、重启均不能从文字反造原链。真实处置、预算暂停和下一轮持久核验释放失效引用；Close清未准入表并禁止重新保留，已经交给原job的引用仍遵守其实际退出生命周期。原超时不代表旧业务停止，不能为了拿到槽位重投尚未退出的工作。

`Cause` 是原应用 error，不是Core对SQL回滚的证明。应用仍须验证完整身份、当前尝试和真实事务结果；一次 not found、旧回滚、错误字符串或枚举 `NotCommitted` 本身都不构成证据。批内多个原件可拿到同一原子批错误链，并不代表Core替应用做了逐项金融分类。

## 接入与兼容

已有 `UseWriteBehindWithFailureHandler(target, handler)` 自动获得此增强，无需加参数或应用缓存；`UseWriteBehind`、零配置、API方法签名和默认 legacy 行为不变。应用只在真正证据充分时返回 `WriteBehindRetry + WriteBehindNotCommitted`，不得因为拿到某个error类型就省略身份和attempt核验。示例07 README补充上述接入边界；本例的独立持久接管仍不是APPLIED。

```bash
rtk proxy go get github.com/digitalwayhk/core@v1.10.0
rtk proxy bash scripts/link-consumer-skill.sh --target /你的消费方仓库
```

第二条应在Core源码检出中执行，消费方先按[安装指南](CONSUMER_AI_SKILL_SETUP.md)核对参数。`go get` 不自动同步skill。权威变更在 `write-path-and-performance.md` 与 `testing-and-release.md`，正式失败处置合同同时更新。

本次无存储格式、StoreID、policy fingerprint 或业务记录变化。同代卷停稳后可回用 v1.9.1，但其原错误保留缺口也会恢复；不能并行打开同一物理库。v1.9.0引入的终态格式边界仍在：更早旧卷需按既有指南受控单向迁移，禁止旧二进制回开新格式。尤其不能用本次新版本直接打开消费方 v1.7.1 在用卷；本轮没有执行这种迁移。

增强不会自动解除已经 paused 的32条或其他历史记录，不恢复已经丢失的内存证明，不修改业务价格条件，也不把旧成功日志重新分类为当前attempt的证明。

## 验证口径

原件保存在 `/private/tmp/core-deferred-cause-20261104/`。保留基线overlay、首轮夹具错误、真正RED、所有候选轮次和命令退出码。普通与race的RED都是类型保留断言失败；无DATA RACE不等于race整体通过。

真实ReliableWriteStore/临时Badger，SyncWrites=true/DetectConflicts=true，32独立组、2 worker/容量2队列，屏障控制首两回调在途。基线4份原类型/28份文本；候选手动与自动均32份原类型且版本/指纹逐项吻合。失败pending32、每项target/handler各1次；屏障期间健康组仍确认1项。原合成target错误仅验证技术传递，不是消费方SQL证明。

容量用例在ScanLimit=4、2 worker/2 queue下产生10失败原件：8份原链、2份溢出文本，原件与预算保持，健康项继续；旧Unknown不能继承新批error，第一次回滚不能证明第二次无原error的未知结果。Unknown本地提交失败不保留原链；真实Close超时不假装handler结束，缓存仍失效。

真实进程用例在32原件、28份未准入引用、4份已准入时SIGKILL，再由新PID打开原目录：不重调target，32原件保留，原target Attempts=1；4项handler次数恢复为2、28项为1，全部缺原类型并保守暂停。父进程记录实际PID，子进程入口默认SKIP不替代父用例；这也不证明任意断电或所有Badger崩溃窗口都安全。

实际命令使用RTK启动，完整参数/退出码/命令墙钟见同版本公开证据包的commands.json；下表时间含构建等命令开销，不是单函数耗时或性能结果。

| 命令/用例 | 结果 | 命令墙钟 |
| --- | --- | --- |
| 原32组overlay需求RED（手动/自动） | FAIL，exit1，4/32 | 见原件 |
| 原32组需求race RED | FAIL，exit1，无DATA RACE | 见原件 |
| `go test ./pkg/persistence/database/nosql -v -count=1 -timeout 300s` | PASS，246顶层用例 | 35.970秒 |
| 上述全包加`-race` | PASS，246顶层用例 | 56.055秒 |
| `go test -race ./pkg/persistence/database/nosql -run ^TestDeferredCause -v -count=3 -timeout 90s` | PASS，三轮32组/溢出/跨attempt/事务失败/Close/新PID恢复 | 见原件 |
| `go test ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -timeout 180s` | PASS | 31.472秒 |
| 上述示例加`-race` | PASS | 41.719秒 |
| `./scripts/test.sh persistence-unit` | PASS | 32.770秒 |
| `./scripts/test.sh config-contract` | PASS，外部依赖缺省跳过不作真实Broker证明 | 9.284秒 |
| `./scripts/test.sh performance-contract` | PASS，其中router/cluster/event/types为COMPILE_ONLY | 6.633秒 |
| `./scripts/test.sh release-contract` | PASS，含API/public API/config/security门禁 | 53.891秒 |
| `check-ai-skill.sh`、`check-web-dist-sync.sh`、`check-logging.sh`、nosql `go vet` | PASS | 见原件 |

本轮新增硬杀用例普通PID49846→49847、race52297→52312；三轮专项race又分别35204→35234、35297→35298、35363→35366。精确v1.9.1旧源码独立二进制按既有正式兼容fixture建卷，最终候选更新，再由v1.9.1验证完成状态、32业务副本和80份轻量历史，自审修正后再次构建最终候选，三个PID56536→56537→56553，均exit0。只用Core合成目录，没有消费方卷。

提交前自审另补“旧通知失配不能删除后次引用”的断言，候选旧实现真实RED（stale-notice-red.log，exit1）。修正为只有完整身份匹配才转交并删除；失配只拒绝，由持久状态核验释放失效项。完整普通/race重新验证此边界，原RED保留，不以最初32组GREEN代替并发边界验收。

首轮扩展夹具误解已完成终态的Item可为空，另一次按事务序号注入拦住了既有ForceSync准备事务；两轮失败原件均保留，修正为跳过轻量终态及按target真正返回屏障注入后通过，不冒充生产RED。真正保留需求RED及其断言没有改弱。

正式release gate只在干净精确HEAD上执行，结果由发布回执记录；不将candidate契约冒充实际tag已发布。包内实际SKIP列表在证据清单：外部MySQL、显式性能和精确旧版本卷fixture未默认启用；新增父测试已真实执行的子进程入口不重复算NOT_RUN。

主Agent按正确性、简洁性、架构、安全及有界成本完成自审。仓库CLAUDE禁止子Agent，本轮没有独立外部审计，不把自审写成多模型审计。公开source.diff仅含本专项生产Go差异；命令摘要和原件SHA见[公开证据包](evidence/deferred-cause-20261104/evidence.tar.gz)。

## 未验证与责任边界

消费方接入/真实SQL回滚逐项证明、Full-E4恢复/部署/UAT/容量均NOT_RUN；未连接业务Redis/MySQL、未打开或复制消费方卷、未清业务数据。未重做全依赖安全扫描，依赖版本不变。历史Badger任意硬杀打开错误不在本次范围，不能以受控进程用例抹去既有失败。新增保留表有有限本地核验成本，没有新的前台P99、稳态吞吐或金融正确性承诺。
