# 跨主题持续归属（v1.4.6）

本分片是持续归属的权威接入规范；实现、测试和公开兼容面优先。完整迁移与证据见 [运行指南](../../codex/SCOPED_EVENT_OWNERSHIP_GUIDE.md)、[发布验收](../../codex/RELEASE_V1_4_6_2026-10-21.md)和[示例11](../../../examples/11-scoped-event-ownership/README.md)。本能力不自动启用，不改变原 `SubscribeEvent`、`SharedKeys` 或消息生命周期策略。

## 先区分三种合同

| 入口 | 归属/顺序作用域 | 能力边界 |
| --- | --- | --- |
| `SubscribeEvent`，`SharedKeys=false` | 一个 subject/group 的 active owner 内分键 | KeyConcurrency 仅为实例内并发 |
| `SubscribeEvent`，`SharedKeys=true` | subject/group/原 OrderingKey 的单次执行 | 多副本分担；执行 token 不代表跨主题持续归属 |
| `SubscribeOwnedEvents` | 应用声明的 scope/key，覆盖多个 subject 的同一逻辑 group | 持续凭据、SQL fence、恢复后Ready、有界多输入批次及按Ready owner调用 |

Redis Streams 声明 `OwnedReliableMQProvider`；NATS、无MQ和没有该可选能力的自定义Provider明确拒绝，不降级到本地事件或哈希候选。Redis锁、Lua、Stream命名不是应用合同。

## 声明和启动

1. 应用在 contract 包提供同一个 `ownership.Manifest`：稳定 Scope、Service、原 Group、Selector版本、Subjects、有限Keys和预算。Service/Group必须是逻辑服务名，不能每实例建业务组。Key选择是应用的纯函数；Core不理解市场或金融payload，也不改原ShardKey/EventID。
2. 首次启用或原shared/W=8迁移必须走显式维护流程：停旧消费者并确认实际handler、SQL事务及远端调用退出，然后 `MQManager.ProvisionOwnership(ctx,m,sqlFloor)`、`BindOwnership(ctx,m)`。正常启动只订阅，不自动初始化/重建。原Stream/group/PEL/游标和payload保留；标记冲突拒绝。
3. 每个服务在 `Initialize` 调用 models.EnsureStorage，随后一次性注册正式订阅，并由ServiceContext管理返回Runtime生命周期：

```go
owner, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{
    Manifest: contract.Manifest(), MaxOwned: 1,
    Key: appRuntime.Key,
    Activate: appRuntime.Store.Activate,
    Restore: appRuntime.Restore,
    NoDueTasks: true, // 示例11明确没有到期任务；有任务时改为Due回调，二选一
    Handler: appRuntime.Handle,
})
```

这是现有EventBridge的正式加性入口，不允许应用自建消费循环、Redis锁、调度器、第二套队列或MQ适配器。Activate/Restore/Handler都须接受取消、遵守执行上限；Key是无副作用确定性函数，Due可与handler并行，必须线程安全、有界且明确未知错误。

## SQL才是业务fence权威

`ownership.Credential` 包含 Scope、Key、Service、InstanceID、SessionID、正Epoch及秘密Token。Token不序列化、不记录日志、不放入用户响应。应用在**自己的同一SQL事务**实现 `FenceTransaction`；LockFence须按scope/key锁定已初始化行并持锁到真实commit/rollback。随后：

- Activate：`ownership.ActivateFence(tx,c)`，等待旧事务退出，拒绝旧epoch/相同epoch不同token；同一凭据可幂等重试。
- 业务处理：事务内先 `CheckFence(tx,c)`，再写业务事实及Inbox或等价幂等依据，同事务提交；批次返回nil后Core才ACK。
- 需要权威查询/控制时也须检查应用SQL fence，不能仅依赖本地缓存。示例11提供 `ReadFenced`。

Core不能判断应用是否真的提交、不能强杀任意Go回调或已发出的远端业务调用。租约到期不证明旧SQL已停，Broker ACK fencing不能替代SQL行锁。未提交、失败、取消、超时或未知结果不ACK；已提交但响应未知可重投，跨副本幂等仍归业务。语义是at-least-once，不是exactly-once。Outbox/Inbox记录是否独立保存，仍按现行业务确认清单与使用者确认。

## 查询、进度与恢复

- `types.IRequestOwnedServiceCaller.CallServiceOwned(ctx,api,scope,key)` 保留原路由权限，先查Ready租约，再用ServiceResolver精确实例；结果返回前复核凭据。归属未知/未恢复/目标不可达均报错，不走哈希或旧本地缓存。迟到控制结果可能未知，Core不自动重试副作用。跨进程内部专用Public使用mTLS和`WithInternalCallers`。
- `Runtime.Status(ctx,key)` 仅返回有效本地owner的状态，非owner或本地已取消的key须拒绝，即使Release响应未知而Broker旧lease暂未到期；非owner应调用正式owner查询。Observation的Delivered/Unread/Queued/Due为-1表示未知；At/LastConfirmed/ExecutionSince区分观测、确认和执行。未确认进度为nil，不用心跳伪造业务进展。
- 观测统计包含被顺序屏障阻挡的待办；有限窗口看不全时明确未知，不把全主题lag摊成单键lag，也不能认为pending=0就没有未读或到期任务。
- WS路由可实现 `IWebSocketOwnedReference` 返回scope/key/语义名；声明在原认证、Parse、Validation后读取。会话身份仍由Core注入，客户端不提供owner凭据。Core从现有会话生成有界全量快照，应用Restore接收收敛结果，不另建会话账。
- 两个Gateway各自引用同一键时，一个退订不关闭另一个。迟到revision拒绝；租约失效后Gateway自动从仍活跃会话重建，不要求客户端重订。Restore必须按当前凭据建立/替换视图，不能把旧归属缓存当全局事实。订阅ACK只证明恢复引用写入成功，不证明业务SQL完成或owner已Ready。
- 归属WS与普通Public/Private WS可共存；`IWebSocketOwnedReference`仍为可选接口。两个精确中继gRPC路径共同校验同服务可信mTLS、SourceService及摘要节点；普通路由交回已注册SyncSubscription/ForwardNotice，归属路由保留内建处理。不能因未声明可选接口截断普通中继。HTTP及用户Token中继拒绝，不授予其他Manage/ServerManage权限。

## 硬边界

默认 lease15s、renew3s、poll250ms、execution30s、stall45s、observation15s。执行超时撤销单key，仍等实际回调退出；有工作而停滞、观测持续未知或续租失败可撤销；真实空闲不换主。单key失败后本实例冷却Stall时长，让其他有空余容量的实例接管。无副本有空余容量时不能承诺接管时延。

默认 MaxOwned4、每subject原组W=8、MaxBatch8、每条编码后MQ data 1MiB。上限：64个owner worker/Runtime、16个subject、256个显式key、W/Batch各1024、每条16MiB、每key引用4096；ServiceContext最多64个scope（终态资源名不重复注册）。Gateway最多1024条语义引用，Provider命名空间最多1024个活跃Gateway。固定模式声明目录和预算冻结；v1.8.0可显式使用DynamicKeys追加目录实例，但仍不支持删除key、改Selector或原地改W；不提供自动均匀再平衡，部署必须让总归属槽位覆盖键目录。

每subject整个group共享W；某key占满W时窗口后方其他key会背压，不能用无限预取越过它。批次取原OrderingKey最早可执行前缀；同OrderingKey即使跨scope key也不能越序。各subject之间**没有全局用户顺序**。资源预算必须同时考虑MaxOwned、批次、窗口、消息字节及JSON解码开销，不能只看CPU或只调大并发。

本入口没有整批有限次数DLQ合同：已声明 `Retry.MaxDeliveries>0` 时明确拒绝，不悄悄忽略旧策略；其它原生命周期required group、容量、保留与回收门禁不变。所有者在自己的Key循环中持续读取，不恢复跨批wg.Wait屏障。

Redis缺失/损坏元数据fail closed，不补0。显式 `RecoverOwnership(ctx,m,positiveSQLFloor)` 只在停止并重建全部owner运行时、确认旧SQL/远端调用退出后使用；要求原声明、原scope模式、所有租约缺失，保留大于floor的现有序列。floor须核对该范围所有SQL fence的最大epoch，后续新epoch严格更高。它只恢复缺失或提升有效序列，不修复损坏值/丢失manifest，也不构成Redis HA共识或防止所有持久副本同时回退。应用SQL仍负责拒绝回退epoch。

回退先停稳新消费者及真实回调，再 `UnbindOwnership`，恢复原shared/W标记后启动旧版；不删除标记、组、PEL或跳游标。不能让不同版本混跑同一组。迁移必须由消费方签收，Core本地验收不代替Bitzoom UAT或生产容量结果。

## v1.8.0：显式动态key目录

固定`DynamicKeys=nil`保留旧JSON/Fingerprint与W。非nil开启追加模式，Keys必须空；`DynamicKeyPolicy`与scope/service/group/subject/selector/预算冻结在指纹中，运行期用`ServiceContext.RegisterOwnedKey(ctx, manifest, key)`受信幂等登记。应用先核验合法不透明key；普通消息、Gateway和WS输入不得隐式注册。成功不是Ready，仍需原Activate→应用SQLfence→Restore→Ready，槽位不足可持续未Ready。

MaxKeys默认/上限256；PageSize默认min(32,MaxKeys)，每发现间隔至多一页；RefreshInterval默认250ms且受Runtime Poll约束。未登记预留U默认原W、上限1024，动态总PEL硬限W+U，登记处理仍只用W。原子读取核对PEL/原游标/权威目录后分别计费；原消息/ID/组/OrderingKey/业务幂等不变，未知前序不ACK、不复制，不让后续同ordering key越过。无关key只在有限预算内前进，U或总预算用尽仍背压。CapacityBlocked区分有效但看不全的窗口和真正元数据未知，不能把-1改成0；真正sequence/目录损坏仍fail closed。

固定→动态必须停稳全部旧回调、核对正SQL最大fence下界，通过`MQManager.MigrateOwnershipKeys(ctx, from, to, floor)`原子保全迁移。不能改Manifest后调用Provision覆盖，不能新旧混跑，不提供自动降回固定目录；原pending、游标、sequence和SQLfence保留。Unbind/Recover会检查所有动态登记key的活动租约。NATS或只声明固定归属的Provider明确拒绝动态模式。

归属Gateway在已有3s引用心跳中从当前会话索引有界重放正式订阅摘要，补新进程/同地址peer的中继索引；退订与关闭不复活会话，认证边界不变。owner Ready不等于异步中继索引已收敛，更不等于业务消息已持久完成。完整接入、预算/迁移与回退边界见[动态key指南](../../codex/OWNERSHIP_DYNAMIC_KEYS_GUIDE.md)。

## v1.8.1：归属Gateway退出生命周期

归属引用心跳从既有按服务同步转发器注册表取得当前中继，不在后台读取可被SetRunState清空的CrossNodeBroker。Melody在启动组合根取得MQManager引用；引用开启时使用该稳定句柄，不能从后台反复读ServiceContext公开字段。关闭取消在途刷新、等待实际返回与原worker退出，之后关闭Gateway lease；关闭后不能再刷新或重新开户。原单轮3秒、128摘要、mTLS、用户隔离、退订不复活和恢复合同不变。

RegisterOwnedKey、CallServiceOwned及其Resolver使用原生命周期锁取得快照，锁不跨Redis、SQL或网络调用持有。停止后新调用返回NotReady/Unavailable；已经执行的控制调用若结果未知，不自动重试副作用。公开的MQManager/CrossNodeBroker/ServiceResolver字段不是无锁运行时取句柄接口；应用须通过正式API，或在自己启动生命周期取得既有Store并自行保证使用/退出次序。本补丁不修改API、配置、动态manifest、消息或卷格式，无需清PEL或重新迁移。
