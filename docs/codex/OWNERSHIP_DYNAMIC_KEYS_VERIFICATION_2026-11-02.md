# 持续归属动态key修复与v1.8.0交付记录

基线为远端main与正式v1.7.1 `4f88a3d9d7da8ef8f2682e6103e0a5dc5e180396`。独立工作区`/Users/vincent/.codex/worktrees/36b9/core-codex`，分支`fix/ownership-dynamic-keys`。本轮未操作Bitzoom、Full-E4、Pebble实验、消费方数据或现存Docker卷。执行者为当前主Agent，未启动子Agent。

## 根因、修复及职责

旧Manifest冻结未来Keys，Runtime.run只遍历初始Keys；redisOwnedReader.mapMessages把合法未登记key当整个当前key未知，watchKey随之撤销。修复通过显式DynamicKeys及可选KeyRegistry追加技术目录；应用确认不透明key合法性，Core不解析市场等业务意义。默认固定声明JSON/Fingerprint有精确字节兼容回归。

新增文件/函数：ownership/key_registry.go、Manifest.Normalize/Validate/PendingWindow、Runtime.run；Provider的RegisterOwnershipKey/RegisterKey/ListKeys、MigrateOwnershipKeys和dynamicSubjectMessages；MQManager统一动态能力校验和正式ServiceContext.RegisterOwnedKey。固定Provider接口未扩方法，不支持者明确拒绝。

原窗口W被未登记PEL占满后，若禁止ACK、跳游标及无限预取，就不可能继续读取后方健康key。显式动态合同因此冻结有限U，原W与U在Lua核对PEL/原游标/权威目录后分别计费，总PEL硬限W+U；原OrderingKey前序和原消息/ID保留。容量用尽仍背压，CapacityBlocked区分有效有限观察和真正元数据故障；sequence/目录损坏仍拒绝。预算与内存估算、迁移/回退详见[接入指南](OWNERSHIP_DYNAMIC_KEYS_GUIDE.md)。

真实硬退出另外暴露CrossNodeNoticeBroker只向当时peer推摘要，新进程拿不到已有订阅索引。event_replay.go的ReplayLocalSubscriptions复用归属Gateway既有3s心跳、当前会话索引和正式中继，每轮最多128条、受ctx约束；同peer标识/地址也能重建，退订与重放读锁串行，不增加线程/协议/会话账。owner Ready、应用SQL提交和异步中继索引收敛不同，Notice仍是原有尽力交付。

## RED和失败原件

- 原始RED：`CORE_TEST_REDIS_ADDR=127.0.0.1:56389 go test ./pkg/server/mq -run 'TestRedisOwnership(UnregisteredKeyCannotPoisonKnownObservation|DynamicManifestCanStartWithoutFutureKeys)$' -v -count=1`，退出1，分别因ErrUnknown、ErrCapacity失败，非编译失败。
- 动态能力拒绝RED：`TestManagerDynamicOwnershipRequiresExplicitCapability`在旧Manager返回nil时失败；第一次夹具编译错误单列SETUP_FAIL，不算RED。
- 首轮真实进程FAIL为无剩余接管槽位；测试在B/C已占两个槽位时明确预留第三槽位后继续，未改框架默认/时限。
- 第二轮硬杀FAIL：实际SQL A=3且epoch上升，原WS无第3条；未用SQL成功掩盖推送失败。
- 重放首轮race FAIL为测试在异步通知中更换Sender，修正夹具后race通过。W/U测试预期调整的FAIL同样保留，不覆盖成PASS。

## 实际验证

所有shell经rtk执行，以下为核心命令内容；环境使用本任务独占Redis56389/56391、独立MySQL56390容器及0600本地配置文件，不在本文写凭据。

| 命令/证据 | 结果 |
| --- | --- |
| `go test -p 1 ./pkg/server/ownership ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/cluster ./pkg/server/trans/websocket/... -count=1 -timeout 300s`，显式Redis/MySQL环境 | PASS，完整普通回归 |
| 同上加`-race` | PASS，完整race，无DATA RACE |
| `go test -race ./pkg/server/mq -run 'TestRedis(OwnershipDirectory|DynamicOwnedAckResponseLost)' -v -count=1 -timeout 90s` | PASS，两个独立PID并发登记、目录响应中断及ACK回执丢失 |
| `go test -race ./pkg/server/mq -run 'TestRedisOwnership(DirectoryTCP|DynamicOversized)' -v -count=1 -timeout 30s` | PASS，确实关闭Provider真实TCP后恢复，原凭据/目录保留，大正文不提前读/ACK |
| `go test -race ./pkg/server/cluster -run TestCrossNodeReplay -v -count=1` | PASS，同标识重复恢复、退订竞争、128条上限/取消 |
| `go test ./examples/integration/11-scoped-event-ownership ./examples/integration/ws-relay -v -count=1 -timeout 600s`，显式Redis/MySQL | PASS，固定/动态/固定迁移293.472s；普通/归属WS共存44.834s |
| 动态真实进程追加恢复计时用例 | PASS，114.563s |
| `go vet`相关组件 | PASS |
| `scripts/test.sh public-api/config-contract/security/persistence-unit/performance-contract/release-contract`各自实际执行 | PASS；外部专用测试另以真实环境实跑，不将默认SKIP算外部通过 |
| `scripts/check-ai-skill.sh`、`scripts/check-web-dist-sync.sh` | PASS |

两处理进程及双Gateway实际处理，A/B/C在不重启整组下追加；B第一条先入原PEL后注册，原ID保留；固定A真实SQL激活后停稳、原消息留PEL迁入动态，再追加B/C，正epoch/SQL下界/原游标保留。真实WS验证用户隔离、退订/关闭、无证书/异服务/伪造来源/非法节点及未知路由拒绝；未Ready/恢复失败不发布有效owner。硬杀后原WS无需重订。

恢复计时：显式释放至新Ready为161.173ms；硬退出至新Ready且中继索引恢复为23,409.755ms，**包含替代进程构建和启动**，不能当纯Broker接管耗时或生产SLO。未给未测阶段填0。

## 固定数据组件对照

同机Darwin arm64，GOMAXPROCS=2，两个Runtime各1槽、A/B、W=8、MaxBatch=8、每条上限64KiB、每轮256条，3轮；原版从已发布v1.7.1归档源码构建。候选动态显式U=8。业务handler只记录处理，不访问SQL；真实SQL及独立进程证明在上一节，不能互换。

最终顺序r3轮在累计指标归属修正及其它门禁结束后运行；r2与更早轮次原件另保留。首轮与门禁构建有重叠，另存原件，不混为最终结果。宿主不独占，不控制其它项目；CPU限流/I/O未采集，不能作为稳态或业务容量认证。

| 组别 | 吞吐 条/s | P50 ms | P95 ms | P99 ms | 排空 ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| v1.7.1固定 | 1795.99–1889.85 | 49.87–58.11 | 87.61–101.21 | 92.07–110.19 | 99.18–110.19 |
| 候选固定 | 1783.10–1917.22 | 55.33–68.10 | 92.42–108.03 | 96.69–114.61 | 96.16–113.29 |
| 候选动态 | 1557.66–1697.68 | 56.02–67.70 | 98.62–121.29 | 103.86–126.39 | 102.19–127.18 |

表中为各轮范围，不拼接成单个总体分位数；平均/最大/heap/goroutine等原值在performance-summary.json。三组各768条，错误0，最终pending=0。动态模式波动明显，不宣称吞吐/P99更优；本轮价值为通用动态接入及一致归属恢复，长期容量仍由消费方后续实测。

## 合并自审追加回归

自审另发现未登记观察/读取推迟累计指标属于订阅条目，关闭订阅会1→0。先固定真实RED，再复用既有provider累计指标面修复；完整MQ普通17.565s、race19.809s通过，调度、消息、fence与WS控制流未变。最终r3测量及final-source-manifest.json绑定该修正，先前跨进程验收的生产控制流不变。原RED及此前归档在Git历史e7510702和本次原件中保留。

## 兼容、发布和接入

新增加性API按仓库SemVer规则交付MINOR v1.8.0，不覆盖任何已有tag；正式发布负责人在clean提交执行release-check --release后显式合入main并创建annotated tag。精确提交以v1.8.0 peeled tag及最终交付回传为准，避免把本文自身SHA循环写回。

消费方命令：`go get github.com/digitalwayhk/core@v1.8.0`，刷新skill链接，再按指南维护迁移旧scope，稳定声明后受信注册key。禁止新旧同组混跑、注册冒充Ready、清消息/PEL或删除元数据回退；存量固定模式不启用DynamicKeys无需改变。

## NOT_RUN与限制

Bitzoom接入、Full-E4部署/功能/UAT/压测/长稳态容量全部NOT_RUN；未操作这些环境。Redis HA/所有持久副本同时回退不是本合同保证；NATS/Kafka/RabbitMQ未声明动态归属，不以普通ACK冒充支持。组件测量未采集CPU限流及I/O，未把这些填0。预算耗尽仍背压，元数据/序号损坏仍失败关闭，旧handler或远端调用不能被Core强杀。

## 原件清单

归档：`docs/codex/evidence/ownership-dynamic-keys-20261102/raw-evidence.tar.gz`，99919 bytes，SHA256 `17a031b1692473fbbaaa8495b3545ee1f5b208af9369226fe292cff78340b9ef`；含52文件、原件合计407733 bytes。逐项path/bytes/SHA256见归档内raw-manifest.json；旧归档SHA及提交另记外层physical-manifest.json。密码文件、证书、数据库、旧源代码归档及消费方数据均未纳入。
