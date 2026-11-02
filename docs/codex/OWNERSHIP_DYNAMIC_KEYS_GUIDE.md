# 持续归属动态key：接入、迁移与恢复

适用正式版本：v1.8.0起。只扩展Core持续归属；普通SubscribeEvent无需登记业务key。

## 根因和职责

v1.7.1的Manifest把完整Keys写入Fingerprint，Runtime只遍历启动目录。Redis reader对未知key返回ErrUnknown，watchKey将当前key所有观察来源改为-1，持续未知后撤销；合法新key因此能影响无关已登记key。

新路径：应用控制生命周期确认合法不透明key → ServiceContext.RegisterOwnedKey → MQManager的显式动态能力校验 → Redis原子幂等登记 → 既有Runtime与reader按冻结分页预算发现 → 原Activate → 应用SQL fence → Restore → Ready → 原批次持久成功后fenced ACK。

Core不理解市场、用户、订单或业务目录；Gateway请求和消息解码回调都不自动注册。key注册成功只表示技术登记，可能因没有槽位、激活/恢复失败而尚未Ready。

## 最小接入

```go
manifest := ownership.Manifest{
    Scope: "summary-v1", Service: "summary", Group: "summary",
    Selector: "opaque-key-v1", Subjects: []string{"input.one", "input.two"},
    Window: 8, MaxBatch: 8,
    DynamicKeys: &ownership.DynamicKeyPolicy{},
}.Normalize()
// 显式维护：首次初始化和原组绑定。正常启动不可用Provision重建丢失的序号。
// manager.ProvisionOwnership(ctx, manifest, positiveSQLFloor)
// manager.BindOwnership(ctx, manifest)

// 服务组合根沿用原SubscribeOwnedEvents与SQL/恢复回调。
runtime, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{
    Manifest: manifest, MaxOwned: 4, NoDueTasks: true,
    Key: selectOpaqueKey, Activate: activateSQLFence,
    Restore: restoreApplicationView, Handler: commitIdempotentBatch,
})
_ = runtime
_ = err

// 应用在自己受信的控制生命周期核验合法性后调用；不在Gateway公开注册路由。
err = sc.RegisterOwnedKey(ctx, manifest, opaqueKey)
```

示例中的回调由应用实现，不是Core新增业务接口。已有EventID、Subject、ShardKey、原逻辑组和业务事务保持。外部Provider未声明DynamicOwnedReliableMQProvider时明确ErrUnsupported，不能仅从普通可靠ACK或固定归属能力推导动态支持。Redis支持，NATS没有声明该能力。

## 冻结合同与默认兼容

- `Manifest.DynamicKeys=nil`：旧固定模式，JSON字节、Fingerprint、Keys与W的默认值保持；有精确旧结构字节和指纹回归。
- 非nil显式开启动态模式，Keys必须为空。DynamicKeyPolicy冻结在Fingerprint中，目录实例另外追加；登记新key不改变Fingerprint或已有凭据/sequence。
- `MaxKeys`默认256、最大256；`PageSize`默认min(32,MaxKeys)、最大MaxKeys；`RefreshInterval`默认250ms，范围1ms..1min。实际发现还受既有Runtime Poll约束；每轮至多一页，不按每条消息拉整目录。
- `UnregisteredWindow`简称U，默认原W，范围1..1024；原W仍为已登记处理窗口且范围1..1024。动态总PEL硬上限为W+U，最大2048/subject/group；固定模式仍是W。不是只把共享池或预取随意调大。
- 每次新读取在同一Lua中核对原PEL完整有界快照、原组last-delivered-id、目录和模式，再按权威登记状态分别为W/U计费。并发读方不能分别超配；并发注册导致旧未知PEL转为已知时可有既存容量债务，只恢复/排空，不继续扩大。调度仍只准入最早W个已登记候选。
- 总量、页数、每条MaxMessageBytes和MaxOwned都必须共同预算。一次subject扫描最多W+U条PEL，纯未读观察至多W+U+1条；每次回调MaxBatch不变。示例11显式W=8/U=8、64KiB消息上限，PEL正文上界1MiB/subject、观察正文上界约1.063MiB/subject；还需另计候选读取、并发watch/worker、JSON/Envelope解码及runtime开销，不将此正文上界冒充进程RSS。
- MaxKeys不保证有足够owner槽位，也不提供均匀再平衡。总槽位必须覆盖希望同时运行的key，并为故障接管留余量。
- 原生命周期required group、容量、安全回收仍生效；配置MaxAckPending时必须容纳整个W+U，而不只容纳W。

## 未登记消息及背压

有效Key回调返回未登记key时，保留原消息、原ID、原Stream/组/PEL，不ACK、不复制、不调整原组游标。它仍阻止同一原OrderingKey的后续消息。无关key可在W/U预算内继续，登记后原消息由正常持有者恢复处理。

U耗尽或总PEL耗尽时，后方仍会背压；不得宣传任意未知消息量下都能推进。观察在元数据有效、仅因有限扫描看不全时显式标记CapacityBlocked，保留准确的未知计数，不将其伪造成0；无已知本key工作时不把这种容量边界当元数据失联撤销。若本key确实有工作但无进展，原stall/取消边界仍生效。JSON/Key回调失败、正文丢失、真正目录/sequence缺失或损坏仍失败关闭，不按未登记key忽略。

## 从现有固定scope迁移

1. 固定原Scope/Service/Group、Subject、Selector、Timing、W、MaxBatch、消息大小及引用预算。取现有原声明为from；to仅清空Keys并增加DynamicKeys。不能直接删掉原Fingerprint中的Keys后用Provision覆盖。
2. 停止全部旧Runtime，核实进程、实际SQL/handler和已发出的远端调用均退出。租约消失本身不证明回调已退出。
3. 从应用SQL核对该scope所有fence的最大正epoch下界floor。原Redis sequence必须存在、有效且不低于floor；若不满足，沿原正式RecoverOwnership流程处理，不能用注册接口重建0。
4. 受信维护入口调用：

```go
from := original.Normalize()
to := from.Normalize()
to.Keys = nil
to.DynamicKeys = &ownership.DynamicKeyPolicy{UnregisteredWindow: from.Window}
to = to.Normalize()
err := manager.MigrateOwnershipKeys(ctx, from, to, positiveSQLFloor)
```

5. 单次Lua核对旧声明、模式、正sequence及所有旧key无租约后更新目录合同/模式，已有key幂等装入追加目录；保留sequence数值、SQL fence、引用、原Stream/PEL/last-delivered-id。未知/活动租约/冲突不修改成成功。重复同一迁移可重试。
6. 全部节点使用新动态声明启动，再由控制生命周期追加key。原待确认消息和SQL幂等仍沿原身份恢复。已有Gateway活跃会话无需重新订阅。

禁止新旧模式混跑同组；旧固定声明在Bind/Subscribe前被新Fingerprint/模式拒绝。动态目录启用后，本轮不提供自动转回固定目录的迁移。回退应停稳实际工作、保留状态并使用兼容修复版本；不能拿旧二进制直接打开新状态、删除目录标记或清PEL。Unbind/Recover检查整个有限动态目录的租约，不能只检查原Keys空切片。

## WS恢复

owner在Ready前仍完成Activate/SQL fence/Restore。归属Gateway复用原3s引用心跳、现有会话索引和受信SyncSubscription中继，每轮最多128条订阅摘要、最多1024个本地摘要/1024个peer，并受原3s上下文约束。没有新线程、队列或WS协议；不永久缓存peerID，因此同标识/同地址的新进程也可重获索引。

重放在当前本地订阅读锁内发送，退订false排在该次true之后；连接关闭仍从原会话索引移除。它只恢复技术订阅索引，不重发业务消息、不授权旧owner、不绕过用户/租户认证或mTLS。原Notice仍是既有异步尽力交付；owner Ready、引用已恢复、对等中继索引已恢复是不同观察边界。示例诊断RelayReady明确观察后者，真实测试在索引恢复后检查原连接收到业务通知，不用任意sleep冒充恢复。

## 可观测性

现有Runtime组件增加目录key数/发现错误、Activate失败、Restore失败与累计耗时、renew失败以及execution/observation/stall撤销原因。Redis组件增加登记接受调用、登记失败/容量拒绝、未登记消息观察及读取推迟计数。重复幂等登记计为接受调用，不冒充新增key数量；未登记观察可重复采样，不是唯一消息总数。未登记观察与读取推迟累计值属于Provider，关闭订阅不回退；`ownership_directory_read_deferred_total`包含有限预算和竞争快照重查推迟，不冒充唯一消息数。所有指标不以业务key/UserID/订单/Token作标签。

## 证据与责任边界

验收及原始FAIL/RED/GREEN/普通/race/真实进程/性能对照见本轮修复报告。Core组件验证不等于Bitzoom已接入，也不代表Full-E4、业务UAT或容量通过。消费方升级依赖后须按README运行`scripts/link-consumer-skill.sh`刷新权威skill链接；go get本身不会刷新复制的旧skill。
