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


## 显式同租约独立组（新增MINOR能力）

旧`Options.Step`保持同步、真实返回后才确认的原合同。它没有组内独立完成通知；若应用Step内部等一批回调全部返回，健康组的后继仍会等最慢组。`MaxOwned`只分归属key，MQ的KeyConcurrency与Badger IndependentGroups都不能充当这里的SQL组配置。

新`Options.GroupWork`与Step严格二选一；零值不启用。它复用原Runtime、Store、Activate/Restore/Observe、租约和查询/WS入口，不读/ACK原MQ，不保存业务任务副本。Core只认识应用传入的不透明group；顺序范围是`scope + key + group`，不同scope/key不保证同用户顺序。

在启动组合根先创建实例共用预算：

```go
budget, err := sc.NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions{
    Workers: 12,
    Prefetch: 24,
})
if err != nil { return err }
// 所有scope共用这一个budget；不能每桶创建一份。
options := ownership.Options{
    Manifest: manifest, InstanceID: sc.ServiceInstanceID, SessionID: session,
    MaxOwned: ownerSlots, Activate: activate, Restore: restore, Observe: observe,
    GroupWork: &ownership.GroupWorkOptions{
        Budget: budget,
        NextGroups: func(ctx context.Context, c ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
            return taskModel.FindExecutableGroups(ctx, c.Key, q.Limit, q.ExcludedGroups)
        },
        StepGroup: func(ctx context.Context, c ownership.Credential, group string) (bool, error) {
            return taskModel.CommitEarliestUnresolved(ctx, c, group)
        },
    },
}
runtime, err := ownership.StartRuntime(ctx, ownershipStore, options)
if err != nil { return err }
if err := sc.UseResource("sql-task-owner", runtime); err != nil {
    _ = runtime.Close(context.Background())
    return err
}
if err := sc.RegisterRuntimeMetricProviders(runtime); err != nil {
    _ = runtime.Close(context.Background())
    return err
}
```

`taskModel`/session/回调是应用自己的原模型和生命周期变量，不是Core现成业务API。模型的DDL仍只能由启动EnsureStorage处理。一个ServiceContext仅允许创建一次正式预算；重复声明失败。直接`NewGroupWorkBudget(instanceID,options)`的低层使用者须在同实例所有Runtime共用该句柄、自行登记资源及指标；句柄拒绝不同实例和不同逻辑服务，关闭后拒绝新Runtime。

### 发现与真实执行

- `NextGroups`只在原SQL发现当前可执行组身份，最多`q.Limit`项，必须排除`q.ExcludedGroups`；不claim、不执行业务、不读取无限payload。选择须符合应用真实前序/PAUSED/未知/重试期限；有限扫描看不全不能报空或假零。返回重复、已占、空、超过512字节或超过额度的组失败关闭，不能悄悄截断。
- 实际固定worker接纳`StepGroup`后，才在同一应用事务查/claim最早未解决原件，并核对原attempt/版本/指纹/BatchID/前序。Core不替应用制造这些SQL字段，也不改它们；选择到身份不是认领原件或业务提交。
- `true,nil`只表示该单元已经可靠完成原提交；经当前Ready凭据复核才记LastConfirmed。入队、查询、认领、心跳和取消后的迟到返回均不算进展。发生commit未知必须保留原事实，由应用查询/幂等收敛，不能盲重做。
- 同组从预取、排队到回调真实退出持续占用。B1真实完成后释放自身，B2无需等A1；A2不得越过未退出的A1进入真实组首查询。错误/false仅按原Poll技术间隔，应用决定有限业务重试、暂停、接管和解除屏障；Core不跳过失败，不新增业务重试计数。
- 回调必须线程安全，不能依赖原Step全局串行；跨组原子业务事务仍由应用保留，不能拆开后声称原子性不变。暂停/未知状态不能作为成功，已提交结果也不能因同组其他项失败而抹去。

### 实例资源、时钟与退出

Workers必须1..64，Prefetch必须Workers..1024。Prefetch涵盖选择预留、排队和执行身份总额，不是每scope额外额度；单次发现至多Workers。预算仅排有限身份/原凭据引用，无payload队列或公开Submit。最多4096个登记租约，仍受每Runtime最多64owner/每ServiceContext最多64scope边界。全worker真实不退出或额度耗尽时明确背压；不补位、不无限创建goroutine，也不保证在没有空余槽时健康组无延迟。

每个实际组单元使用原Execution时限的独立context；持续排空循环总年龄超过30s不会被当作一个超时任务。单元超时仅取消该单元，真实回调未退出仍占原worker/group，其他组在剩余预算内继续。Activate/Restore/NextGroups的控制阶段Execution保护、续租/观测未知/聚合Stall仍沿原规则；全部挂住仍停滞撤销。不得把观察BlockedDue当作掩盖仍执行工作；Core以下限补齐实际组在途量，不与应用已经统计的同一工作重复相加。

原引用revision确实改变且有Restore时，本key停止新准入并等自身实际组退出再恢复；这是引用恢复维护屏障，普通排空没有每批Wait。未变更revision不会等待其他组。其他key仍可用剩余预算推进。

Runtime.Close与Budget.Close均取消准入及排队/在途context，但必须等实际回调退出才Done；超时只表示调用方等待到期。Budget.Close取消已登记租约并等固定worker与租约退出，不能替代应用SQL远端操作实际终止。资源按预算先登记、Runtime后登记，逆序先关Runtime再关预算；SQL连接依赖最后关闭。所有权丢失后旧业务事务是否还在执行，必须由真实SQL fence保护，不能仅信本地cancel。

### 显式共享Check，排他Activate保持

旧`FenceTransaction.LockFence`、`CheckFence`、`ActivateFence`仍要求排他锁，绝不静默降锁。需要独立组SQL事务并行时，显式实现新`SharedFenceTransaction.LockFenceShared`，并在原业务事务内调用`CheckFenceShared`：

```go
func (t sharedFenceTransaction) LockFenceShared(scope, key string) (ownership.FenceState, error) {
    var row OwnerFence
    err := t.tx.Clauses(clause.Locking{Strength: "SHARE"}).
        Where("scope = ? AND key_name = ?", scope, key).Take(&row).Error
    return ownership.FenceState{Scope: row.Scope, Key: row.KeyName,
        Epoch: row.Epoch, Token: row.Token}, err
}
// 业务事务内先CheckFenceShared，再锁定/执行原组首；共享锁保持至真实commit/rollback。
// Activate仍传原排他fenceTransaction，不能把Activate也换SHARE。
```

这是MySQL 8的共享**锁定当前读**，不是普通快照SELECT、缓存或先查后插；必须有预初始化唯一(scope,key)行，完整epoch/token相等且Scope/Key一致。RR事务先有旧快照时，锁定当前读仍须拒绝旧凭据。多个独立组S可共存；新Activate X必须等待所有旧S事务真实commit/rollback，再恢复/Ready，旧epoch/token后续拒绝。账户/任务行排他锁、业务前序、SQL幂等和未知commit仍由应用维护；共享guard不能替代它们。其他SQL引擎的锁适配未实测不得宣称等价。

### 升级与回退

本能力为显式加性API，无新配置字段/StoreID/manifest指纹/持久格式/消息变化。无需清原SQL、重建scope/Stream/group/PEL/cursor。先升级正式Core和消费方skill，再在停止准入、等旧回调真实退出后启用GroupWork及共享Check；同一应用生命周期不要为同一scope重复登记旧Step和新组Runtime。GroupWork不是Broker持久模式标记，启动只校验本地Step/GroupWork互斥；不能据此推断其他进程均已启用。旧Step副本赢得归属时仍有旧串行/整批成本，混跑不作为新模式并发验收；本轮不提供回调策略在线热切换。迁移不重置epoch、SQL fence、原attempt或截止时间。

回退同样先停稳真实回调，改回Step与旧排他Check；原SQL事实和guard仍可用，不删状态。但旧版本恢复原桶串行/整批等待成本，混跑不能验收新独立完成合同。此能力不自动改变SubscribeOwnedEvents的Broker消费模型；例11现有MQ/WS路径保持零值兼容。

实际RED/GREEN、SQL/进程/性能与NOT_RUN见[本轮验收](OWNERSHIP_SQL_INDEPENDENT_GROUPS_2026-11-18.md)。Core组件通过不代表Bitzoom已经接入、金融/UAT/Full-E4或500/s容量通过。
