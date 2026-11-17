# SQL持久任务的持续归属与业务阻挡

## 正式入口与职责

`MQManager.OwnershipStore(ctx)`或Provider公开Store，加`ownership.StartRuntime`，可复用Core持续归属执行应用SQL持久任务。Runtime已有有界owner槽和Activate/Restore/Step/Observe生命周期，不需要另写归属循环、Redis锁或消息worker。该低层入口不要求`BindOwnership`，也不会读取/ACKManifest中的原MQ消息；原MQ可以只负责将业务事实持久收单，再由此Runtime执行SQL任务。

Manifest仍须显式维护初始化、声明稳定Scope/Service/Group/Selector/Subjects/Keys与预算。正常启动不能用Provision重建丢失序列。SQL任务使用独立且含义稳定的scope，不让两个Runtime争同一scope/key；不能为了过滤候选每实例永久分裂业务组或改声明。Provider能力/连接必须可用；MQ off、NATS或不支持Store不能静默降成本地模拟。

应用负责模型组合根EnsureStorage、任务和业务事务、合法不透明顺序组、到期/暂停/未知/重试分类、有限预算与管理员解除屏障。Core不拥有金融或业务状态机、不跳过/补偿/自动确认任务，也不提供第二套持久队列表。Activate必须在应用同一SQL事务内ActivateFence；Step实际写入先CheckFence并持锁到commit/rollback，不能把候选哈希、Ready或租约到期当SQL权威。

Store/Runtime只负责技术归属；直接StartRuntime的返回资源须由应用交给既有生命周期，例如`sc.UseResource("task-owner", runtime)`，并按既有方式注册指标。UseResource逆序关闭发生在MQManager关闭之前；初始化失败也要关闭已创建Runtime。Close超时不代表实际回调退出，不能卸载旧SQL/远端依赖或宣称干净退出。原execution/renew/SQLfence保护继续生效。

## 显式BlockedDue

v1.14.0增加`ownership.Observation.BlockedDue int64`。它是`Due`中的已核实业务阻挡子集，0保持旧规则且默认JSON省略字段；不是丢弃Due、隐藏待办或推进确认时钟。原Delivered/Unread/Queued/Due未知仍用-1；BlockedDue只接受非负已核实量，负值或大于Due均拒绝，未知时不能猜一个阻挡数。

只有以下条件同时成立才将其视为blocked-only：所有来源计数和At已知有效、Due>0且BlockedDue==Due、Delivered/Unread/Queued全为0、WorkVisible和CapacityBlocked均false、最近Step没有错误。其他任何已知/有限未知/在途工作仍走原保护。BlockedDue不覆盖真正元数据损坏、源持续未知、Step错误、执行超时或续租失败。

应用必须在同一SQL一致快照/聚合中取得Due和BlockedDue。比如已到期PENDING被明确PAUSED前序阻挡，可报告Due=1、BlockedDue=1；原PAUSED及后继记录仍保存。不能将普通PENDING前序、未来重试、数据库失败、执行中或未知结果推断为阻挡。运行中、未解决与其他输入必须完整计入相应来源；有限扫描看不全保持未知/容量边界，不能随手填0。

以下只是应用Observe返回片段；`total`/`blocked`来自应用自己的SQL一致事实，Core不提供业务分类查询：

```go
return ownership.Observation{
    At: time.Now(),
    Delivered: 0, // 仅当确知本Runtime没有这一来源的在途工作
    Unread: 0,
    Queued: 0,
    Due: total,
    BlockedDue: blocked,
}, nil
```

有部分可执行项，例如Due=2、BlockedDue=1，仍有正常进展责任；健康组不能被一个PAUSED用户遮住。Step须选择应用认为可执行的组首；同组原前序不能越过。无可执行项返回`false,nil`，查询/认领/持久化错误保留error。声明阻挡不会让Core替应用选择其他任务，也不保证错误的应用选批实现自动公平。

## 时钟、执行和状态

- HasWork仍对真实Due>0返回true，Status仍返回Due/BlockedDue实际量。轮询和合法阻挡不改LastConfirmed；只有Step真实进展且无错误才记录确认。
- 已知业务全阻挡可保持有效owner，不能因此把短Step查询冒充在途业务。任何实际Activate/Restore/Step仍受原Execution上限；超时取消也必须等回调真正退出。
- 长阻挡解除、同桶出现健康可执行工作时，用独立资格起点给出原Stall窗口，不能因旧确认时间久远马上撤销。它不改变业务LastConfirmed、不重置应用attempt/截止时间。
- 持续Step错误仍会按原停滞保护撤销，分类证明不能掩盖技术失败；观察回调错误/非法分类按原未知保护，续租错误继续失败关闭。已确认进展、可执行资格、租约存活是不同事实。
- 默认15s lease、3s renew、250ms poll、30s execution、45s stall、15s observation均未改变，也没有无限扩大租期。

## 槽位与预算

MaxOwned是持有归属key的容量，不是金融或SQL并发许可。固定64桶需要全服务有效owner槽总量覆盖目录，应用可另设12个SQL许可；二者不能混用。一个Runtime最多64个持有槽，全部槽被占用时仍背压；没有新增调度器、队列、每条goroutine或自动均匀再平衡。候选可用现有服务分配决定是否尝试，实际Store凭据及同事务SQLfence始终是权威。

## 升级与回退

新增字段是显式加性能力，旧Observe零值、默认JSON、Manifest字节/Fingerprint、Store与SQLfence持久格式不变。`go get github.com/digitalwayhk/core@v1.14.0`后，只有应用在Observe填写已核实BlockedDue才启用新语义；其他接线、原Group/Stream/PEL/cursor和业务payload保持，不需迁移、清消息或重建卷。

直接SQL Runtime不需要为了升级Bind/Unbind原MQ；原SubscribeOwnedEvents的Due(int64)回调保持原合同，不自动获得此新分类。不要把它与原MQ owned消费模式切换混为一谈。相关owner实例应升级正式版本及应用分类接线，旧实例仍有旧45s停滞行为，混跑不能认证新能力；严格JSON客户端也须认识正值BlockedDue。

回退先停止准入并确认旧回调实际退出，保留原SQL任务/attempt、scope序列及原消息；同时回退新增应用字段接线，旧版本恢复旧停滞语义。没有新格式门禁需要删除；不能移动tag或清理模式/前序来绕过旧规则。go get不安装skill，消费方须按README/link-consumer-skill流程同步权威规范。

## 验证边界

Core以真实Redis/MySQL默认时限保留旧51秒RED并验证同桶健康SQL实际提交、原暂停/后继/attempt与MQ PEL保全；独立PID正常/硬杀接管验证旧SQL锁跨租约及迟到凭据拒绝。完整命令与失败证据见[专项验收](OWNERSHIP_BLOCKED_DUE_2026-11-17.md)。这些不代表消费方金融功能、Full-E4、500ms–1s目标、UAT或容量通过；应用仍须正式包独立复测。
