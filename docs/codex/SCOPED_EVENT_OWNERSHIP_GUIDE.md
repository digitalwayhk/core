# 跨主题持续归属、批次与恢复指南

适用版本：v1.4.6及以后；普通WS与归属WS共存应使用[v1.4.7补丁](RELEASE_V1_4_7_2026-10-22.md)。权威接入示例与时限见 [skill分片](../ai/core-skill/scoped-ownership.md)。本指南登记通用层/Provider职责、保全迁移、运维边界及验收入口。

## 根因与实际调用链

v1.4.5的SharedKeys按subject/group/原OrderingKey取得每次执行租约，多个主题没有同一持续凭据；健康哈希也不知道单key恢复或失效，单会话订阅访问面不能恢复跨Gateway引用。这是新合同缺口，不推翻v1.4.4多实例分担和v1.4.5连接池修复。

新链路为：

```text
ServiceContext.SubscribeOwnedEvents
  → MQBridge.SubscribeOwned / MQManager.SubscribeOwnedBatch（统一能力及生命周期校验）
  → OwnedReliableMQProvider（Redis原组有限PEL/新读取）
  → ownership.Runtime（持续scope/key、有限worker、Activate→Restore→Ready）
  → 应用Handler(Credential, []*Envelope)（真实SQL fence/幂等/commit）
  → 原子验证凭据、原组模式后ACK

IRequestOwnedServiceCaller.CallServiceOwned
  → Ready租约 → ServiceResolver.ResolveInstance → 原路由权限/认证 → 返回前再次验租约

现有SessionSubscriptions
  → 已认证路由的可选归属引用 → 有界Gateway全量快照/revision/lease
  → owner Restore（无需客户端重订）→ Ready / 持续收敛
```

通用层只理解稳定scope/key、凭据、待办、回调和有限预算。Redis实现Lua原子校验、持续租约/正序列、原组PEL、原模式冻结和引用版本；应用不读私有Redis键。SQL业务fence、事务、幂等及业务任务数量由应用定义。NATS没有声明该能力，启动明确拒绝。

## 保全原shared/W=8组

1. 固定应用Selector版本、完整稳定键目录、原主题和逻辑组；保持原事件身份、原ShardKey及payload。冻结预算，先验证所有消息能映射到声明键。未知/畸形/超字节限制消息仍保留，不能跳过。
2. 停止全部原消费者，确认实际handler/SQL/已发出的远端调用退出；只等租约过期不够。持有期间仍可见的原执行锁会使Bind返回busy，但这不是替代步骤2的完整进程证明。
3. 启动组合根初始化应用SQL fence和幂等依据，读取SQL权威epoch下界。在独立维护操作中先ProvisionOwnership，再BindOwnership。Bind对全部原subject/group的PEL ID快照和原执行租约做原子复核，任何变化/超W/模式冲突拒绝；不删旧记录、不改游标。
4. 新消费者只调用SubscribeOwnedEvents。旧接口/旧版本不能消费已绑定scope模式，避免混跑；W仍为8。应用Activate事务必须先等待旧SQL的fence行锁，Restore完成才Ready。
5. 用原PEL+新消息验证顺序、重复、SQL持久结果、查询跟随和WS引用。未完成消费方验收前不能把Core自身测试当作项目上线批准。

回退：停止所有新消费者并等实际回调/SQL/远端调用退出 → 确认租约释放 → `UnbindOwnership` 恢复原shared/W标记 → 原版以原组重启。操作不会退回游标或删除PEL；应用SQL/消息schema必须仍兼容。未解除绑定时旧版失败是保护行为，禁止手工DEL模式键。

## Redis异常与SQL隔离

运行时续租、读取、claim、ACK任一未知均不能提前确认消息。缺失/损坏元数据不自动补0；正epoch仍需SQL验证。Redis恢复了旧快照时，凭据token/epoch与SQL不一致必须拒绝激活/业务写/权威查询；不能仅靠Redis单实例计数宣称防脑裂。

`RecoverOwnership` 是加性维护入口：停稳并重建全部owner运行时、核对原manifest和SQL最大epoch、确认旧事务/远端调用结束、所有scope租约缺失，再传入正floor。只恢复缺失或提升有效序列；不降低现有序列，不修复未知manifest/损坏值，不改变原模式/Stream/group/PEL。Gateway可以保留连接并从原会话重新发布快照。丢失manifest或模式等更大范围故障需恢复经过核对的Broker备份和应用权威，不能由Core猜测重建。

SQL fence不能由Core自动造业务表：`LockFence`必须按scope/key持排它行锁到真实commit/rollback，未创建行也必须在启动阶段预建或采用事务安全的唯一键策略。每个需要保护的业务事务先CheckFence，业务副作用和去重依据同事务。租约失效但旧SQL仍持锁时，新Activate等待；新epoch激活后旧凭据不能再次写入。已发出的外部调用还需外部系统自己的幂等/fence；MQ无法中断它。

## 调度、资源和可观测性

每个归属key一个持续循环，批次成功后立即继续，其他key不等它完成。读取复用v1.4.4共享组脚本；新读取后重新扫描最早PEL，不能直接准入并发读取中缺失前序的后序消息。各subject轮换首位，避免固定首主题吞掉所有批次槽位；同原OrderingKey跨归属key取最早连续前缀，不承诺跨subject总序。

资源上限和默认时限见skill。Broker的每subject组级W限制与本地MaxOwned/MaxBatch/字节预算是不同维度。每次扫描至多一个有限pending页和一个有限新读取页，观测也有字节限制；超W历史PEL不能正常绑定，需先用兼容原消费者排回预算，不可删除。不同key在W内可调度；W全被慢key占用时后方key会背压。总归属槽位不足时部分key无owner，Core不自动扩容/迁移空闲租约。

本实现不持有无限应用队列。消息正文缓冲的数量级为 `MaxOwned × (MaxBatch + 有限数×Window) × MaxMessageBytes`，再加并发观测和Envelope/JSON解码开销；不是精确RSS保证。多scope须求和，选最大允许值并不代表机器能承受。所有Redis操作复用v1.4.5命令池；没有新增每订阅连接池，也没有提高CPU/池大小或主题容量。

- 现有 `redis.publish`、`redis.reliable.read` 区分发布与Broker首次新读取；`pending_scan`表示观察旧PEL，重复扫描次数不是唯一重投数。
- `redis.owned.batch`记录每条Broker消息ID+stream hash与claim/admit时钟；`redis.owned.handler`和`redis.owned.ack`分别计时，不将ACK等待计入handler。无已知首次读取时钟的旧PEL不补造历史时间。
- Provider原有 `reliable_new_read_total`、`reliable_reclaim_read_total`、publish-to-read、schedule-wait、handler及ACK counters继续使用；本入口的reclaim_read统计pending扫描行数，可能重复，不等于handler重投次数。新批次无独立内存排队，schedule_wait是准入后到handler的本地等待，不代表完整Broker等待。
- ownership组件按实例采集active/ready/inflight和acquired/revoked/unknown；多个scope在实例内汇总，不用scope/key/UserID/OrderID/TraceID做指标标签。active含仍在退出的旧回调，ready可已为0。实例区别来自正常scrape target，不新增业务高基数标签。
- Runtime.Status按需给出单key精确有限观测；-1和nil保留未知。被顺序屏障阻挡的消息仍计待办；超过有限观测页则未知，不将全主题lag当单keylag。心跳不增加LastConfirmed。

## 验证入口及未运行项

使用独占Redis和MySQL，禁止指向业务现场。MySQL配置文件为0600 JSON oltp.Config，不把DSN写日志。

```bash
CORE_TEST_REDIS_ADDR=<隔离Redis> \
CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=<独占MySQL配置文件> \
go test -p 1 -race ./pkg/server/ownership ./pkg/server/mq ./pkg/server/event \
  ./pkg/server/router ./pkg/server/trans/websocket/melody -count=1 -timeout=8m

# 包含两个处理进程、两个Gateway、真实mTLS/HTTP/WS及SQL
CORE_TEST_REDIS_ADDR=<隔离Redis> \
CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=<独占MySQL配置文件> \
go test -race ./examples/integration/11-scoped-event-ownership -count=1 -timeout=3m
```

完整RED/GREEN、原生/加速区分和实际结果在发布验收文档。生产Redis HA/持久化回滚演练、外部业务系统fence、Bitzoom G1–G5/UAT/部署/容量均NOT_RUN；明确拒绝的NATS持续归属及在线目录变更不是“已支持但未测”。
