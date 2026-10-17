# 稳定实例身份实施审计与框架回归记录

## 结论与范围

基线为 `09f8ae3`，实施分支为 `feat/stable-instance-discovery-recovery`。本轮按用户授权复审计划、修复设计遗漏、实施、再次审查结果并运行隔离回归。使用 ce-doc-review、ce-code-review 的正确性、可靠性、兼容性、数据完整性与对抗场景维度；依项目 AGENTS.md 在主线程顺序审查，没有独立子代理，因此不称为独立终审。

实现采用显式 stable，默认 legacy 保留。**消费方不会仅升级依赖就自动得到稳定身份与自愈保证**；必须按 [迁移指南](STABLE_INSTANCE_IDENTITY_GUIDE.md) 初始化身份、编号权威并验收。没有修改或重启生产消费方，没有部署或推送。

实现审计和运行证据不等于全框架无缺陷证明。候选发布脚本本身通过，但独立的 workflow 静态门禁因源码基线缺少文件而失败，两者覆盖不同，不能互相替代。实际测试结果按下表登记，保留所有中间失败，不将被跳过用例写成通过。

## 计划复审修订

| 编号 | 问题 | 修订 |
| --- | --- | --- |
| R2-01 | TTL 后旧 Boot 可能重新认领空节点 | 不带 TTL 的持久启动世代和 owner；续约只允许同一 Boot |
| R2-02 | 从响应时刻起算 TTL 会虚增准入期限 | 从请求发起时刻算，扣余量，并考虑墙钟/单调时间与休眠 |
| R2-03 | 计划的只读阶段表述与新实施授权矛盾 | 保留原历史状态，追加本轮真实实施证据 |
| R2-04 | tools 是独立 Go 模块，工具路径不受根模块回归 | 工具放根模块 cmd/instance-identity |
| R2-05 | 两个身份/生成器文件容易部分提交 | 合并 identity.json.generator；旁路锁和一次原子替换 |
| R2-06 | 崩溃后的未释放 Boot 缺少实际恢复入口 | 显式 recover，核对停机/隔离证明、文件锁与精确远端世代 |

## 实现审计发现及处置

以下均是本轮定位并修复的问题，包含审查初版实现时发现的遗漏。严重度表示修复前影响，不表示当前仍未修复。

| 编号/级别 | 触发、影响 | 位置与最小修复 | 验证 |
| --- | --- | --- | --- |
| R3-01 / P1 | stable 节点可被 legacy Register/Deregister 越过 owner 覆盖或删除 | provider_local.go、provider_redis.go、provider_etcd.go；在同一互斥/Lua/事务中拒绝 legacy 操作带 owner 的节点 | TestOwnedRegistrationLocal/Redis/Etcd 先红后绿，真实后端 + race |
| R3-02 / P1 | 暂时未就绪使生成器永久 closed，网络恢复仍不能发号 | identity/generator.go NextId；仅永久错误关闭，ErrAdmissionUnavailable 保留恢复能力 | TestGeneratorTemporaryAdmissionRecovers 先红后绿 |
| R3-03 / P1 | InitRequest 的包级备用 worker 绕过持久编号及时间保护 | router/request.go InitRequest.NewID；无服务上下文拒绝真实编号 | TestMetadataRequestCannotAllocateBusinessID 先红后绿 |
| R3-04 / P1 | etcd Get(id) 实际返回扫描到的第一个节点，注销也可能选错节点 | cluster/provider_etcd.go Get/Deregister；精确匹配 NodeInfo.ID | provider_etcd_identity_test.go 多节点夹具先红后绿；真实 etcd legacy 集成 |
| R3-05 / P1 | 损坏反向 worker 索引可把已占用编号当空闲；身份字段缺失被接受 | identity/authority.go decode 与 state.go validateState；验证格式版本、固定布局、索引双向一致、实际 worker 与持久来源 | TestAuthorityRejectsCorruptLedger、TestIdentityRejectsInvalidGeneratorState 先红后绿 |
| R3-06 / P1 | 缺少崩溃后有证据的恢复通道，或仅凭 TTL 接管旧 Boot | identity/recovery.go、CLI recover；可信外部隔离声明 + 精确 CAS，保留编号/上界 | TestRecoverStoppedBootRequiresExactProof；TestOfflineImportRecoveryAndRetirement |
| R3-07 / P2 | HTTP 请求没有填充 routerinfo，健康探针豁免失效，断网后 health 也 503 | types/routerinfo.go Exec；按实际冻结路由路径豁免探针，不信任请求自报路径 | 06 真断网用例先失败，修复后 health=200、ready=503→200 |
| R3-08 / P1 | 仅路由入口设门禁，受管理事件和 Outbox 仍能开始业务操作 | ServiceContext.SubscribeEvent 与 OutboxOptions.Admission；Handler 前、加载/发布前检查 | TestStableEventSubscriptionAdmission、TestOutboxAdmissionStopsBeforeLoadAndPublish；event 整包 race |
| R3-09 / P2 | 清理故障上下文后，遍历“仍注册上下文”的进程探针可能误报恢复 | router/instance_identity.go CheckProcessAdmission；保存 stable 终态故障，同名成功重建清除；Ready 同时检查所属服务 | TestProcessReadinessKeepsFencedFailure、TestStableAdmissionUsesRequestStartAndRecovers |
| R3-10 / P1 | Provider 迁移把“源快照缺失”当成“目标已停机”，删除目标独立 stable 实例 | cluster/switcher.go；不清理快照外 stable owner；目标由自身续约/TTL 管理 | TestMigrationDoesNotDeleteIndependentStableOwner 先红后绿 |
| R3-11 / P2 | 07 默认 mesh 配置带 ServerName，正式配置校验失败 | 07 bootstrap/config.go；只有 mTLS 配置 ServerName | TestStableIdentityEnvironment 暴露错误，修复后通过 |
| R3-12 / P2 | 旧 WebSocket 集成夹具使用全局 forwarder 且未绑定 EventBridge/Hub，三条用例没有测试现行路径 | tests/integration/websocket_cluster_test.go；使用 service-scoped forwarder 和正式内存 EventBridge/RouteWebSocketHub | 三条定向 race 与完整外部集成通过；没有改变产品为兼容旧夹具 |
| R3-13 / P2 | CLI 对不支持的 --dry-run 静默忽略，可能执行写操作 | cmd/instance-identity/main.go；按操作拒绝无效 flag | TestOfflineCreateAndAuthority 校验未写入权威 |
| R3-14 / P2 | 心跳预算可长于实际 TTL，造成准入失效和恢复振荡 | config/idgenerator.go；stable TTL 至少 1 秒、心跳低于 80%，拒绝非法编号时间预算及编号 TTL | 配置整包与正式构造器回归 |
| R3-15 / P1 | OutboxOptions 新增函数字段使公开结构从可比较变成不可比较 | event/outbox.go；改成 OutboxAdmission 接口，保持零值和可比较性 | 公共 apidiff 实际拦截，改后通过；map[OutboxOptions] 编译契约 |
| R3-16 / P1 | 权威布局/世代/索引损坏被当成普通网络错误一直重试 | identity.ErrAuthorityInvalid 与 membership 回调；损坏/缺失绑定为终态，拒绝继续并停止服务 | 损坏错误链用例先红后绿、identity/router/run 定向 race |
| R3-17 / P1 | 事件处理器关闭超时只记录日志，ShutdownError 仍为空，可能错误释放 Boot 并批准新进程 | ServiceContext.SetRunState(false) 将事件/通知/MQ/发现等关闭错误纳入 ShutdownError；未确认停止则保留 Boot | TestStableShutdownKeepsBootWhenEventDrainTimesOut 真实阻塞处理器 5 秒超时，先红后绿；router/run 完整 race |

额外安全约束：生成时间上界先远端、再本地持久；候选逻辑时间越界先扩展再返回。时钟回拨、持久写失败、权威缺失、错误 worker 均拒绝编号。远端 BootEpoch 低于本地文件时拒绝启动。编号客户端不与发现共享生命周期。上述逻辑均有定向测试，但不能推导为未知时钟/任意数据库故障下的完整业务 fencing。

## 整体框架检查的未解决项

| 编号/级别 | 证据、影响 | 建议与边界 |
| --- | --- | --- |
| F-01 / P2 | HEAD 09f8ae3 的 git tree 无 .github/workflows/ci.yml、ci-scheduled.yml、consumer-smoke.yml；三个独立 workflow shell 门禁明确失败，自动 CI 不能按现有发布文档执行。candidate 脚本本身通过，但没有覆盖这些检查 | 单独恢复与 scripts/ci.sh 相匹配的最小权限、Action SHA 锁定、超时、artifact/summary 的工作流，并重新运行相关门禁。本轮记录并定位，不扩展为 CI 重建或远端发布任务 |
| F-02 / P2 | 07 order-service/service.go 的 runPendingSyncLoop 每 100ms 只执行一次 DrainOnce，limit=100；理想调度上限约 1000 条/秒，实际还受事务耗时影响。持续写入 2000 条/秒会积压并最终触发背压 | 后续按每轮时间/条数预算连续推进有界批次，有积压且有进展时及时续跑；失败须退避，保留取消、ACK 与幂等契约，并以真实 MySQL/Broker 持续负载验证。本次仅补测与记录，不修改同步调度。Outbox 本身满批有进展会立即续取，不存在相同的固定 1000 条/秒调度上限 |

框架现有 legacy 身份/自动编号仍保留旧限制，这是本候选的兼容选择，不能宣传 legacy 也已自愈。实际 WorkerID 容量有限；跨命名空间不能承诺 ID 唯一。复制身份卷、同时回滚整个权威和身份卷、无隔离证明的强制接管均不由本协议保证安全。

## 回归结果

首次实施封存（提交 `fbf91bb`）回归：245 包，125 个有测试包通过，120 包无测试文件；两阶段退出码均为 0，Go 源码始末哈希一致。该运行内有 56 个 skip 测试项，包含 Provider 不适用分支、子进程 helper、显式集成开关和缺少外部依赖；不能把这 56 项全部解释为缺陷或已运行。后续补测单独记录在文末，不能把此全量结果冒称为补测变更后的再次全量运行。

| 检查 | 当前状态 | 证据 |
| --- | --- | --- |
| 根模块全部 Go 包，MQ 独占 Redis 隔离 | PASS | sealed-go-summary.json：245 包，125 pass/120 无测试文件，exit=0/0，source_unchanged=true |
| identity、CLI、cluster、router、event、public、types、Manage、run、REST、WebSocket 共 12 包 race | PASS | sealed-race.log，exit=0，含 R3-17 退出超时保护 |
| go vet ./... | PASS | sealed-vet.log，exit=0 |
| 06 稳定三进程真实恢复 | PASS，含 race | 首轮约 1.07 秒，race 约 1.58 秒恢复；final-stable-processes-race.log，冻结全量也覆盖该用例 |
| 07 稳定四进程/双 order、专用 MySQL | PASS，含 race | stable-07-native.log、final-stable-processes-race.log；真实下单、幂等、原卷重启 |
| Local/Redis/etcd owner、独立编号客户端 | PASS | TestOwnedRegistration*、TestExternalAuthoritySurvivesDiscoveryClose |
| integration 标签：Local/etcd/Consul legacy/Redis/NATS/EventStream/WebSocket | PASS | external-integration-final.log；Broker 重启阶段另测 |
| Redis/NATS 真实 Broker 重启 | PASS | broker-restart-prepare.log + broker-restart-recover.log；只重启本轮专用容器 |
| 公共 Go API 与现行 HTTP/OpenAPI 契约、candidate 发布脚本 | PASS | sealed-release-contract.log；candidate 脚本没有覆盖三个独立 workflow 静态门禁 |
| 现有管理前端 Jest | PASS | 33 个 suites、256 个 tests；web-jest.log |
| 管理前端认证 TypeScript | PASS | web-tsc-auth.log |
| 嵌入前端同步、AI skill 指针 | PASS | remaining-contracts.log、skill-check.log |
| CI/定时/消费方 workflow 契约 | FAIL（基线缺失） | contracts.log、remaining-contracts.log；F-01 |
| 消费方 futures smoke 脚本本身的 shell 契约 | PASS | remaining-contracts.log；不是实际消费方升级验收 |
| 显式 Redis 认证撤销、共享权威断连与 L3 缓存 | PASS，含 race | explicit-redis-gates.log；补跑 TestRedisIntegrationWhenExplicitlyEnabled、TestSharedRedisAuthority、TestRedisL3Integration，未回写原始 skip |

## 历史失败与证据解释

1. 修改前定向四包全部通过；最初整模块缺少默认 6379 Redis，06 两包失败。后续改用本轮隔离依赖，原失败日志保留。
2. 并行包共用 Redis 时，MQ 读取的全局 evalsha 计数受到其他包污染，首次为 660，阈值为 396；独占无其他流量实例重测为 220，132 次完整扫描，测试通过。不能以放宽阈值隐藏失败。
3. 开发过程中启动的中间全量回归会编译到修改中的源码，曾出现缺方法/新增接口类型尚未被另一编译单元看到的 build-fail；这些记录保留，不能作为最终源码的 PASS。末次冻结代码须单独验证。
4. 07 端口预留工具原假设连续端口全部监听，supplier 的 DC=3 实际只在偏移 0/2 监听；增加显式 HTTPServiceOffsets，仅调整测试工具假设，保留原默认契约。
5. 07 mesh 参数、探针豁免、旧 WebSocket 夹具和 OutboxOptions 可比较性分别留下原始失败，修复后以新的命令输出证明，不覆写为历史已通过。
6. 一次中间隔离回归在 Redis/NATS 的 100ms 生命周期扫描预算用例中触发 context deadline exceeded；没有放宽预算或断言，也没有把它冒称为协议缺陷已修复。最终冻结回归另用独占 Broker，原始 verified-all.jsonl 保留该失败及源码编辑造成的无效封存状态。

原始证据位于本机 `/private/tmp/core-stable-recovery-evidence/`。测试 Redis 16389/16390、etcd 12389、MySQL 13389、Consul 18589、NATS 14289 及 Broker 重启专用 Redis 16391/NATS 14290 均为本轮创建；没有停止或清理用户原有资源。

最终冻结另用独占 Redis 16392/NATS 14291；源码封存清单 SHA256 为 `e259f42a5f60ec5f9fda921ec64ef549efa120a4df7ed854470c26a1a5c41c6b`。sealed-all.jsonl 为 `eeaaa4ac290f7750ed0b35061c0959d1c1bc19e39cf470cff69732e6f8e594d4`，sealed-go-summary.json 为 `2c52d96178fe566ae6eb9f7f8d836df3d73c526542f33abf9590a33e50ed42c3`，sealed-race.log 为 `7895e30280dde5e7f5a6b85d71631cb61d72abf6a811491c11b26b6bb12df0d6`。证据文件保留；临时测试进程和容器在收尾按本轮清单清理。

## 尚未运行及发布边界

- 真实未同步 pending 积压在崩溃时恢复：首轮 NOT_RUN；后续已补跑 PASS，见文末真实记录。
- Linux 容器中的本候选、原有 Compose stable 部署：NOT_RUN；本轮 Core 进程在 macOS arm64 运行。
- etcd 多进程断网后的真实 gRPC 调用恢复：首轮 NOT_RUN；后续已补跑 PASS，见文末真实记录。
- Bitzoom/futures 等实际升级、历史数据迁移、生产部署、真实 Casdoor 端到端：NOT_RUN。既有 Fake Casdoor、RBAC HTTP 与普通示例测试另属已运行回归。
- MongoDB/ClickHouse/Kafka 等未配置外部后端的真实协议测试不由 Go 单元通过替代；以最终 skip 清单为准。
- 本报告不授权混跑旧二进制、回填历史 ID、重建消费组、强制换主或迁移旧 pending。

## 用户指出遗漏后的补测

前两项隔离验收属于已授权计划，首轮没有环境或权限障碍，结束验收过早；NOT_RUN 是事实标记，不是完成要求的替代品。补测从 `fbf91bb` 开始，仍使用原分支，不更改生产消费方。

- Redis/etcd 三进程断网恢复：PASS，race。`followup-etcd-redis-race-final.log`：两个真实三进程用例均通过；断网超过发现 TTL 后分别约 0.96 秒、2.71 秒恢复；真实 mTLS 业务请求成功，原实例 ID/MachineID/同次 Boot 保持，正常重启改变 Boot，退出不复活。etcd 只代理发现连接，事件与独立编号权威使用专用 Redis。
- 07 真实积压硬崩溃恢复：PASS，race。`followup-pending-crash-race-sealed.log`：8 笔下单确认后，专用 MySQL 对应订单为 0；硬杀并 wait 两个 order 副本，原卷核对全部 8 条 pending，普通启动均拒绝未释放 Boot。显式 recover 后保留 InstanceID/MachineID/WorkerID、推进 BootEpoch 与时间上界，8 条订单自动汇合，9 条 Outbox 已发布（含原用例的 1 条），逐请求重试返回各自原 ID，真实查询可见；停止后原卷 pending=0。整个原生双副本用例 66.92 秒，崩溃子用例 7.15 秒，测试包退出码 0。这里证明的是进程硬杀后的原卷恢复，不是主机掉电或磁盘故障恢复。
- Outbox 调度契约：PASS，event 整包 race，`followup-outbox-race.log`。新增 `TestOutboxNotificationDrainsBeyondDefaultBatch`，定时间隔一小时、单次 Notify、默认取数 100，连续确认 2001 条/21 批；此为内存调度契约测试，不证明真实后端能达到 2000 条/秒。07 pending 调度限制单独登记 F-02。
- skill 已补齐主入口第 22 条及多服务、测试分片，避免主入口仍把 stable 编号描述为发现 lease 分配。`followup-skill-final.log` 校验 13 个权威文件/4 指针通过；`followup-logging.log`、`followup-vet-final.log` 检查通过。

中间失败原样保留：`followup-etcd-redis-race.log` 首次 etcd 夹具误将事件 Redis 连接指向 etcd 网络代理，服务明确失败；修正独立连接后重测。`followup-pending-crash-race.log` 中离线核对工具只关闭 prefix，未关闭本工具进程的共享 Badger manager，导致它自己持锁阻止服务重启；显式关闭本轮专属 manager。`followup-pending-crash-race-final.log` 又暴露调用方 gRPC 连接尚未重连时写请求立即失败；夹具改为先通过真实只读业务查询确认恢复，再发单次幂等重试，不以自动重试写操作掩盖失败。进程 ready 只表示自身准入，不能替代调用方链路的恢复断言。

本次按正确性、资源归属、兼容性和可靠性维度在主线程再次审查，只新增示例 06 显式 etcd 接线、验收测试与文档；没有修改 Core 身份/发现/发号实现和 07 同步调度。没有独立 reviewer。没有重跑首次封存的全框架 245 包，只对本次受影响入口补跑 race、配置单测、vet 和 skill/日志检查；整体框架发布仍受 F-01 与 F-02 及上述未运行范围约束。
