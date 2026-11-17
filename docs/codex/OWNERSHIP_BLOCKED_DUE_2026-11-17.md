# 持久SQL任务的业务阻挡与归属：2026-11-17验收

## 根因与正式入口

基线为正式v1.13.6，提交`d2cd416884c204bff70a323be00160a16ea96548`。公开`MQManager.OwnershipStore → ownership.StartRuntime(Activate/Restore/Step/Observe)`可以用于应用持久SQL任务，不要求BindOwnership，也不会自动读取或ACK原消息。需要有效支持Provider、稳定Manifest、启动初始化、应用同事务SQL fence及生命周期关闭。

旧Observation只有总Due及其他来源，无法区分“可执行任务无进展”和“已到期后继被应用明确PAUSED前序阻挡”。因此Due=1、51秒无真实确认后，旧45秒Stall撤销符合旧合同；本轮修复的是显式分类缺口，不宣称旧实现丢失业务数据或解释全部业务延迟。target/SQL不在Core本地提交器中，本轮也没有修改write-behind。

应用先前的固定64桶、每实例MaxOwned=64及另设12个SQL许可符合现有容量职责，MaxOwned不能误当SQL许可。候选分配不代替Store凭据、Ready和同事务SQL fence。启动初始化不能在元数据丢失时盲目重建scope序列。

## 最小实现

- `ownership/observation.go`：追加`BlockedDue int64`，零值`omitempty`。非负且不超过Due；只有全来源已知、新鲜证明、Due>0且全部被阻挡、其他三计数为0、WorkVisible/CapacityBlocked均false时才豁免可执行停滞。
- `ownership/runtime.go`：保留实际业务确认时钟，增加watcher内资格起点；解除阻挡或新健康任务出现后给原Stall窗口。记录Step实际错误状态，持续错误不能借分类无限保主；Execution、未知、renew、真实回调退出保护不变。
- `ownership/runtime_observation.go`：非法子集失败关闭，沿原未知观察预算撤销，不将错误伪装为0。
- `ownership/options.go`：明确Step无可执行项返回false,nil，真实错误保留；Observe完整覆盖输入。

没有新Broker/队列/worker框架、业务状态机或配置；不改原消息、ACK、SQL任务格式、fence、Manifest及其Fingerprint。Due>0仍HasWork=true，轮询/阻挡不写LastConfirmed；只有Step真实无错进展才确认。低基数原状态返回Due和可选BlockedDue，不输出业务身份、token或payload。

## RED与GREEN原件

证据根为`/private/tmp/core-ownership-blocked-due-20261117/`，runner将每条实际argv、退出码、墙钟、log bytes与SHA256分别写入同名json，不覆盖旧轮。MySQL为本轮独占容器，Redis为本轮独占native进程；均无消费方数据。

| 原件 | 结果 | 秒 | log bytes | SHA256 |
| --- | --- | ---: | ---: | --- |
| sql-blocked-red | FAIL/1 | 57.874 | 200865 | 2d46aa2fe2e22e9363a4078b674e7cc991167c9ebf8613e722b0f45417e00a5d |
| sql-blocked-green | PASS/0，早期候选 | 56.357 | 1240 | 63eefc5e7e73387cd4b28b4bba8d382db1ead1a0c7e89631e7505c1d4f6091e9 |
| blocked-unit-race-v2 | PASS/0 | 9.150 | 2953 | 8aced908765a869f23d2aa6b5919f66e984b56f63d3780091effac838c5517fe |
| blocked-callback-race | PASS/0 | 7.196 | 494 | f7177a2c07708589b5fdea36e4f19b1886a951006c5d2b5a323e44ae1f50a2db |
| blocked-step-error-runtime-race | PASS/0 | 6.266 | 196 | 1471406587958cd3cd57a0ad60db7f6e9acd78fcd3d7aa5a15aaf53d52caedb1 |
| sql-process-recovery-race | PASS/0 | 24.349 | 1357 | c5b0a6f4408bc65384f16a53363339e30ada9680beb0785f4876bcf9eb49c8f2 |
| ownership-mq-normal | PASS/0，最终五包 | 181.511 | 304 | 98deb37d87c8e7fd4467274053ba17f9da7b25019726a87afd3263aa074b67da |
| ownership-mq-race | PASS/0，最终五包 | 200.512 | 304 | 623934dc60c3d0ae524ebf23faa41cbc7e35324dcc941555080ff959293c95d2 |

RED使用保存的旧正式生产文件和可在旧版编译的同一真实SQL fixture，缺字段不会skip、改Due或伪造进展。默认lease15秒/stall45秒/poll250毫秒不变，观察51秒：A被撤销、B空闲仍Ready。原件保留了fixture默认GORM记录的合成SQL；后续fixture静音，不改生产全局日志、不覆盖原RED。

最终五包普通/race重新覆盖最终证明时效、实际Status分类读取与Step错误保护，不能用早期GREEN替代最终版本结果。真实SQL验证同epoch保持、Due=1不变、LastConfirmed为空；同桶另一健康组实际提交后才有真实LastConfirmed；原暂停/后继无越序写，明确应用解除前序后才处理后继。原MQ哨兵的PEL消息ID及LastDeliveredID不变，未Bind、未ACK、未重建组。

真实进程恢复由同一测试可执行文件起多个PID：正常B退出后新B接管，A真实SIGKILL后等原15秒租约，新A等待受控旧SQL行锁实际提交再Ready；迟到旧epoch/错误token的SQL、Renew、Release拒绝。旧A PID22470、新A22502，旧B22471、新B22501。受控未退出SQL事务由父进程显式持锁，不能描述成被杀进程自己的SQL连接仍活着。原任务attempt2/3、状态与Writes0保持，新A同桶健康组真实写入。

## 实际命令

均使用rtk代理。以下环境指向本轮专用Redis及私有MySQL配置文件，未在日志输出凭据：

```bash
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:65314 CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=/private/tmp/core-ownership-blocked-due-20261117/mysql-config.private.json go test ./pkg/server/ownership -run '^TestSQLBlockedDuePreservesOwnerAndHealthyTask$' -v -count=1 -timeout=120s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:65314 CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=/private/tmp/core-ownership-blocked-due-20261117/mysql-config.private.json CORE_SQL_TASK_PROCESS_ROOT=/private/tmp/core-ownership-blocked-due-20261117/native-process go test -race ./pkg/server/ownership -run '^TestSQLTaskOwnerProcesses$' -v -count=1 -timeout=120s
rtk proxy go test -race ./pkg/server/ownership -run '^TestBlockedDue' -v -count=1 -timeout=30s
rtk proxy env CORE_TEST_REDIS_ADDR=127.0.0.1:65314 go test -race ./pkg/server/mq -run '^TestRedisOwnershipBlockedDuePersistentStepErrorStalls$' -v -count=1 -timeout=30s
```

最终五包命令如下，普通/race均显式设置上述专用Redis/MySQL与不同CORE_SQL_TASK_PROCESS_ROOT：

```bash
rtk proxy go test -p 1 ./pkg/server/ownership ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -count=1 -timeout=300s
rtk proxy go test -race -p 1 ./pkg/server/ownership ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -count=1 -timeout=300s
```

完整重放应以证据json实际argv及fixture环境清单为准。没有设置真实外部依赖时的SKIP不等于真实验证。

## 兼容、启用与剩余边界

v1.14.0是公开字段加性的MINOR。旧零值Observe、默认JSON字节形状、原业务payload、API入口、配置、Store/SQL格式、Manifest/Fingerprint保持。无存量卷迁移和新初始化要求；不Bind/Unbind原MQ，不清Stream/PEL/cursor。相关owner升级并由应用同一持久快照填写真实BlockedDue才启用；旧实例仍会旧Stall撤销，混跑不认证新能力，严格JSON客户端须识别正值新字段。

应用仍负责合法key与顺序组、真实业务前序、PAUSED分类、SQL事务/幂等、有限重试和人工解除。普通前序、未来重试、执行中、未知结果及数据库错误不能算BlockedDue。回退须先停止准入、等实际回调退出，同时回退应用新增字段接线，保留任务/attempt/序列/消息；旧版恢复原停滞行为。

NOT_RUN：Bitzoom/Full-E4任何接入、部署、金融功能、UAT、500ms–1s目标、容量及长稳态性能。本轮没有压消费方，也未把Core合成SQL任务当真实金融验收；未声称消除了消费方全部延迟。go get不安装skill，升级后按link-consumer-skill流程重新同步权威规范。

## 示例11与发布门禁原件

首次`go test ./examples/integration/11-scoped-event-ownership -v -count=1 -timeout=300s`合并运行保留为FAIL/1：固定87.81s与动态103.29s各自PASS，迁移已到最终业务/恢复断言，但全包300s预算在`Suite.StopProcess`清理期间耗尽。该日志8977字节，SHA256 `63430c5b30d23af81935c883cb6e95f33d4db1223237eaac24a56f45950daa2d`；不能写为整包通过，race也没有因该脚本失败而运行。只读进程核对未发现遗留demo11子进程。后续按单用例重新执行迁移普通及三项race，保留原断言和单项300s预算，不通过总超时放宽来掩盖功能断言。

以下门禁均已实际退出0；这些契约不替代真实依赖测试：

| 门禁 | 秒 | log bytes | SHA256 |
| --- | ---: | ---: | --- |
| logging | 0.251 | 0 | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| web-dist-sync | 5.646 | 176 | cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b |
| performance-contract | 11.214 | 326 | 259721674bcb43bbfd792effe754838d73154b0815ab1689e5036e676652e0d3 |
| release-contract | 56.734 | 1913 | 2049cbc84fbcae46b9411294c539253013584c8cd39b9b4d37570743283905d9 |

实际命令为`rtk proxy ./scripts/check-logging.sh`、`rtk proxy ./scripts/test.sh web-dist-sync`、`rtk proxy ./scripts/test.sh performance-contract`、`rtk proxy ./scripts/test.sh release-contract`。候选release-contract包含api-compat、public-api、config-contract及security；公开工具确认Observation.BlockedDue为compatible addition。performance-contract仅编译/契约验证，不是业务容量或本轮吞吐性能认证。

拆项迁移普通实际PASS：`example11-migration-normal`耗时118.076秒，741字节，SHA256 `10ef9e750b2f04260071d2f70fe22c9e5cf34e2c29373ef1926012b904ef75bb`。真实处理PID59541/59857，正常release到Ready239.151ms，硬杀到Ready与中继恢复17289.536ms（包含替代进程构建/启动，不是纯租约恢复成本）；原生预算和全部消息/隔离/恢复断言保持。`StartProcess`默认给所有真实子进程加`-race`，普通父测试也检查真实进程退出及DATA RACE日志。README全三项命令总预算统一为已有说明的600s，未改变任何单项业务/恢复断言时限。

三项父进程race均实际PASS/0，未跳过真实子进程、mTLS、Redis、MySQL、WebSocket或退出断言：

| 原件 | 秒 | log bytes | SHA256 |
| --- | ---: | ---: | --- |
| example11-fixed-race | 78.152 | 619 | b8b438bf775717ad397aca641883d24869e779ba454c962ab84bfb51a9ce35a0 |
| example11-dynamic-race | 106.494 | 745 | b90cbf2a3030f912b043942a1ea5aa1e014f8e320ed3b4cfd8752230f411fb24 |
| example11-migration-race | 111.168 | 741 | 2f68ea55c3db20c54599dfc9e5421fb2b18de75b943c22e898a5365e30e9d1e5 |

实际命令共同设置上述专用Redis/MySQL环境，分别为：

```bash
rtk proxy go test -race ./examples/integration/11-scoped-event-ownership -run '^TestScopedOwnershipRealGatewaysSQLAndWebSockets$' -v -count=1 -timeout=300s
rtk proxy go test -race ./examples/integration/11-scoped-event-ownership -run '^TestScopedDynamicOwnershipRealGatewaysSQLAndWebSockets$' -v -count=1 -timeout=300s
rtk proxy go test -race ./examples/integration/11-scoped-event-ownership -run '^TestScopedFixedToDynamicMigrationRealSQLAndGateways$' -v -count=1 -timeout=300s
```

三项分别检查真实消息、用户隔离、WS引用/退订/连接关闭、归属接管与SQL fence；动态和固定迁移含硬杀后原连接恢复。最后迁移race的正常release到Ready136.596ms、硬退出到Ready与中继17395.576ms，包含进程构建/启动，不是消费方500ms–1s SLO结论。原固定→动态迁移API和原Scope消息/PEL/序列未改。

发布前已进行主Agent正确性、安全、生命周期、兼容、测试五轴审查；没有启动新Agent，也不宣称外部Agent审计。仅本轮19个文件进入独立提交，默认工作区及未发布锁专项的tracked diff与status指纹保持。正式clean release检查、远端main/tag、官方Proxy/SumDB冷验包和测试资源归档由发布回执提供；本报告不预填其成功。外部futures smoke未配置专用源，本轮NOT_RUN，不接触消费方仓库替代。
