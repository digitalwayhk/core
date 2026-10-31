# v1.4.6 跨主题持续归属实施与验收计划

## 目标及授权

按用户提供的《Core市场归属G0交付要求》实施Core能力、skill、示例与v1.4.6发布。输入文件中的“本材料不授权修改Core”是准备材料的历史说明，用户本轮直接要求实施已经授权Core修改；不授权Bitzoom接入、部署、Full-E4操作或业务数据清理。当前基线v1.4.5 / cdf3887，工作分支feat/scoped-event-ownership-v146，保留原未跟踪内容。仓库要求所有实现/测试/审查由主Agent完成，不派发子Agent。

## 已核实调用链及缺口

ServiceContext.SubscribeEvent → ServiceEventBridge.SubscribeEvent → MQBridge.SubscribeReliableWithOptions → MQManager.SubscribeReliable → RedisStreamProvider.runReliableKeyedOwner。SharedKeys每次执行以subject/group/ordering-key创建短期token；其他subject没有同一持续租约，Message没有应用事务凭据，Handler逐条。CallServiceWithKey/OwnsServiceKey使用健康节点一致性哈希，不代表租约；SessionSubscriptions仅保存单会话映射，Runtime订阅指标不保存跨Gateway恢复事实。

复用现有Provider命令池、shared模式标记/PEL有界读取、ServiceResolver精确实例、UseResource与WS注册/退订生命周期，不让应用建worker、MQ适配器、Redis锁或第二套会话账。

## 设计选择

1. 不复用SharedKeys执行token作为应用凭据，也不把健康哈希当租约。新增显式scope合同，scope由应用命名，多subject共享它；scope内每个应用稳定key只有一个可续租持有者。instance/session/token/正epoch组成凭据；恢复完成后才Ready。不同subject不承诺全局顺序。
2. Redis持久scope元数据与epoch序列先显式初始化，运行时缺失/错误/回退不补0。每次归属给新token和递增epoch；应用Activate回调必须在自身SQL事务中等待旧事务fence锁、拒绝不递增epoch。Core验证Broker租约不等于SQL fencing；epoch回退由SQL权威拒绝，Redis未知必须停止准入。初始化、修复epoch下界是维护动作，不在每次启动自动重建。
3. 采用新的可选OwnedReliableMQProvider和EventBridge批量入口，Provider使用原Stream/group/游标/PEL，现有API零值不变。显式维护绑定将shared-v1:W扩展为带scope指纹的模式，旧版本启动因标记不匹配拒绝。W=8不变；回退必须停消费者和实际handler、退出租约后显式解除绑定，不能删除标记或历史。
4. 固定有限归属worker、有限subject/key列表、每subject组级W、批量上限；按原ordering key的未完成前序选取有序前缀组成同scope/key批次。Handler持久成功后原子验证scope凭据并ACK；失败/取消/未知结果保留PEL，重新查询Broker避免ACK响应丢失导致异常重放。超时取消仍等实际handler退出；租约可失效，SQL隔离必须由业务fence保护。
5. 每key区分已交付/未读或未调度/应用报告到期任务，未知单列；有限扫描未覆盖的工作不能当0或把全主题lag摊到key。续租与执行/观测时限明确，真实空闲不换主，进程健康而key停滞仍可撤销Ready与租约。实际任务报告是通用计数/时限，不解析金融业务。
6. 查询通过Ready租约解析精确实例，不走哈希或本地快捷缓存；调用后复核凭据，归属改变拒绝迟到结果，控制结果未知不自动重试。恢复前不提供Ready owner。
7. 在WS成功注册、退订、登出和连接关闭上挂接可选归属引用接口。Core用有界租约化全局引用快照管理多Gateway/session引用；原客户端无需重订。快照带单调revision，视图拒绝迟到旧快照；一Gateway退订只去掉自身引用。owner恢复与持续收敛由框架管理，应用只接恢复后的引用集合，不维护会话账。

## 分步任务和文件责任

### 1. 通用归属凭据、租约及观察合同

新增pkg/server/ownership下按类型/职责拆分的credential.go、store.go、runtime.go、references.go与对应测试。凭据至少包含Scope、Key、Service、InstanceID、SessionID、Token、Epoch；Validate拒绝空身份或非正epoch。Store接口覆盖Acquire/Renew/Ready/Resolve/Validate/Release及有界Reference快照；Runtime管理有限worker和取消/时限。

先写无效凭据、旧epoch、空闲/待办/未知观察、旧快照拒绝等测试，再运行RED，再实现GREEN：

```bash
GOWORK=off go test ./pkg/server/ownership -count=1
GOWORK=off go test -race ./pkg/server/ownership -count=1
```

### 2. Redis Provider与有界批量、保全迁移

新增pkg/server/mq/provider_redis_ownership*.go、ownership_contract.go、manager_ownership.go及真实Redis测试。复用r.client命令池与现有shared读取脚本；只为scope合同增加独立脚本，原串行/keyed入口不改。维护初始化与模式绑定/解除分别为显式接口；Manager校验required group、生命周期与Provider可选能力，NATS拒绝，作用期禁止Provider切换。不能表达的现行策略必须明确拒绝，不能静默弱化。

先在v1.4.5保留缺少scope合同的RED，之后真实Redis验证Acquire唯一性、Ready、正epoch、旧Release/ACK fencing、元数据未知、窗口8及批次多条。每个subject最多一页pending和一页新读取轮换，旧PEL优先；预算内无法探查的后方key明确背压边界。

```bash
CORE_TEST_REDIS_ADDR=<本轮独占Redis> GOWORK=off go test -p 1 ./pkg/server/mq -run 'Ownership|Owned' -count=1 -v
```

### 3. 正式EventBridge与ServiceContext入口

新增pkg/server/event/owned_subscription.go、mqbridge_owned.go及router/owned_service.go、ServiceResolver精确实例方法。批次对应用只暴露Envelope和公共凭据，不暴露Redis键/Lua。ServiceContext把Runtime注册到UseResource；查询必须先解析Ready owner并对结果再次校验，不允许旧本地快路径。订阅启动必须校验正式能力，无MQ/NATS不能降级本地。

```bash
CORE_TEST_REDIS_ADDR=<独占Redis> GOWORK=off go test -p 1 ./pkg/server/event ./pkg/server/router -run 'Owned|Ownership' -count=1 -v
```

### 4. WS会话引用与恢复

新增pkg/server/trans/websocket/melody/ownership_references.go和相应真实WS测试，在现有SessionSubscriptions生命周期最小挂接。Router可选声明scope/key/语义引用名；用户身份仍由现有认证确定，客户端不能声明owner凭据。多Gateway租约及引用数量有上限；恢复快照revision与退订交错需确定性RED→GREEN。

```bash
GOWORK=off go test -race ./pkg/server/trans/websocket/melody -count=1
```

### 5. 示例、真实进程与SQLfence验证

增加独立Core演示/集成夹具，使用两真实消费者进程、多个key、多个subject、正式ServiceContext、真实Redis和现有WS。SQLfence夹具通过框架存储初始化建立技术记录，不把生产业务幂等猜入Core；验证旧SQL持锁不退出、新激活等待，激活后旧凭据拒写。分别保留优雅退出、硬崩溃、网络中断/迟到ACK、健康进程停滞、pending0但未读、到期任务、真实空闲、观测失联、非owner路由、WS不重订恢复。原生时限与加速注入分别记录，不以同进程对象替代独立PID。

### 6. 回归、审查与发布

完整mq/event/router/ownership/WS普通和race；v1.4.2跨窗、v1.4.4共享、v1.4.5连接池与生命周期不回退；config/public-api/security/web-dist-sync/release-contract。主Agent按需求映射自审，未执行场景标NOT_RUN。更新唯一权威docs/ai/core-skill主题、能力矩阵、兼容指南、中文发布说明、示例README与CHANGELOG。最终干净副本运行正式发布检查，再合本地main、annotated v1.4.6及发布，官方代理精确来源和无replace消费方验证。Bitzoom UAT/容量不作为Core交付冒充通过。

## 完成判据

每条G0行为有正式接口、真实证据和资源/迁移边界才称完成。不把编译错误当行为RED，不把部分功能或mock当G0验收；未达安全边界不能发布“全部支持”。执行记录继续追加在专项验收文档中。

## 执行补充

正式能力、Redis/SQL/独立PID/真实WS已实现并逐项保留RED/GREEN。自审补充了满额引用替换、顺序屏障后待办计数、未知元数据启动拒绝、显式停稳epoch下界恢复、原WS中继可信gRPC接线、多scope指标和ServiceEventBridge逻辑组校验。现行合同与精确NOT_RUN见[发布验收](../../codex/RELEASE_V1_4_6_2026-10-21.md)，旧计划不是接入权威。
