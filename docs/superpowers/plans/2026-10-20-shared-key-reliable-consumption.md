# 可靠事件跨实例分键分担实施计划

> 本仓库禁止子智能体；由当前主 Agent 逐项执行、保留 RED/GREEN 并自审。

目标：原 subject、逻辑 group、消息身份和 PEL 不变时，多个独立服务进程共同执行不同 OrderingKey，保留同键最早失败屏障、有限资源和成功后 ACK。

基线：main / d260046（已发布 v1.4.3，包含 v1.4.2 跨读取窗口修复）。工作区：`/Users/vincent/.codex/worktrees/3505/core-codex`；分支：`fix/shared-key-reliable-consumption`。不操作 Bitzoom，不复用其 Redis、容器或数据库。用户已确认验证后发布 v1.4.4，不重写 v1.4.3。

## 已核对调用链与职责

`router.ServiceContext.SubscribeEvent` 保留逻辑服务名组和 stable 准入包装，转发 `event.Subscription`。`ServiceEventBridge` 注册本地 control handler、引用计数外部订阅；`MQBridge` 将 ShardKey/订阅要求传至 `MQManager`；Manager 校验 capability、生命周期 manifest 后调用 Provider。Redis 用 `subject+group` 的 owner 整体互斥，只让一个 `runReliableKeyedOwner` 进入读取循环；其固定 worker 已解决跨窗口整批等待。最终 `processReliableMessage -> MQBridge callback -> Stream.PublishControl -> handler`，成功后 provider ACK。NATS 仅声明可靠 ACK，没有有序、分键并发或跨实例分键能力。

通用层只表达可靠性、顺序作用域、实例分担要求和预算，不暴露 Redis key/Lua。Provider 自行实现竞争、租约、认领、ACK fencing 与恢复。业务数据库继续负责跨副本幂等和迟到远端写约束，MQ 不提供 exactly-once 或强杀任意业务代码。

## 最小实现选择

- 加性 `SharedKeys` 显式要求与独立 capability；false 逐字保留旧默认。该要求必须 reliable、有 subject、不能与 Broadcast 合用；缺外部桥或 capability 必须拒绝。相同 subject 的外部引用必须检查并发和分担模式冲突。
- Redis 沿用原 Stream/group/游标。所有实例分页读取同组 PEL；每 key 仅保存最早未完成 work，真正得到空闲 worker 时才竞争该 key 的短租约，避免空闲实例被另一实例的本地预排队占住 key。
- 原子 acquire 同时校验消息仍 pending、建立随机执行 token、XCLAIM。运行期间有界续约；ACK/DLQ 脚本校验该执行 token。失主不能重获旧 token 并 ACK；旧 handler 未退出仍占用原 worker，远端业务调用不会因 lease 到期自动停止。
- 新读取用 Lua 将共享 PEL 数检查与 XREADGROUP 合并，Count 为共享模式的组级预算，各参与者必须一致；MaxAckPending 小值优先。接管旧超限 PEL 只分页恢复。有限预算不保证穿过任意长热 key 前缀找到后方冷 key。
- 共享组元数据冻结模式和预算；迁移需停止旧订阅、等待 handler/旧 lease 结束后开启，保留全部 PEL/消息/游标。用旧整体 owner 槽的保留标记阻止旧二进制继续抢整体 owner；新代码发现模式冲突直接拒绝。无自动回退或删元数据入口，不支持未经协调的新旧混跑。
- 复用现有固定 worker、结果 channel、成功回扫及 ACK 丢失后的 PEL 复核，不恢复 wg.Wait 批屏障。不新增业务适配器或发布双写。

## 执行清单

- [ ] 先写真实 Redis 双子进程用例，通过正式 SubscribeEvent；A 阻塞时 B 必须在另一 PID 执行。旧基线先 RED，保留日志和 SHA。
- [ ] 补通用能力、桥接传递、引用冲突及 NATS/旧自定义 Provider 拒绝测试，先 RED 再最小实现。文件：event/subscription.go、servicebridge.go、mqbridge.go；mq/mq.go、manager.go、provider_nats_reliable.go。
- [ ] 实现 Redis 私有分键租约、共享组模式 fence、原子有界新读取与 PEL 分页；修改 provider_redis.go、provider_redis_keyed.go、provider_redis_retry.go，新增 provider_redis_shared.go；不改 Publish payload。
- [ ] 用真实 Broker 覆盖同 key/失败、慢 key/跨窗口、入退成员、硬杀/重启、lease 失主/迟到完成、网络中断、ACK 响应丢失、已 ACK 不异常再执行、旧 PEL/新消息及旧默认兼容。原生 lease 等待和加速 TTL 用例分开记录。
- [ ] 增加通用分担 conformance，注明多对象 conformance 不是跨进程证明，另保留真实双进程验收。使用专属随机端口无盘 Redis，逐轮串行避免全局命令统计污染。
- [ ] 补读取/调度/handler/ACK 时钟、实例处理量、实际并发、group pending、key owner 变更固定指标；不记录正文、OrderingKey、业务 ID 或凭据。
- [ ] 独立容量调查：核对 allowPublish 的硬容量条件、容量快照 freshness、worker 和安全回收；真实 Broker 复现已 ACK 历史仍占容量、未完成必需组禁止删除、原策略恢复后重新放行。没有现场状态不编造现场根因，不通过提阈值、清消息或改 observe 过关。结果写独立文档；确认具体代码缺陷才另做 RED→GREEN 修复。
- [ ] 更新生命周期/NATS/能力矩阵/API 兼容文档、权威 skill 与中文验收报告；普通/race/原跨窗口回归、公开 API/配置/安全发布门禁通过后才合 main。未运行生产 Broker 拓扑与业务部署逐项 NOT_RUN。

## 补充：第 10 个多实例服务演示

- 使用 `examples/10-reliable-multi-instance` 和 `examples/integration/10-reliable-multi-instance`，真实 Core WebServer 启动一个发布服务和两个同名消费服务；通过正式 ServiceContext.Initialize/SubscribeEvent 建立共享逻辑组。
- Private 提交接口从 req.GetUser() 取得用户、建立 user:<UID> 顺序键和稳定请求事件身份；消费 API 返回本用户在当前实例的有界演示进度及固定 MQ 指标。
- 演示调度结果保存在每进程有界内存，只用于验证处理分布，不假装成跨副本数据库 Inbox/业务事务，不声称 exactly-once。生产业务需自行选择持久幂等/事务方案。
- 真实 HTTP 多进程测试覆盖 JWT 未登录拒绝、不同用户隔离、慢 A 时 B 在另一 PID 完成、同键序列、失败重试与事件身份；复用 examples/integration/helpers.go 管理进程/端口/Token。

## 停止条件

若破坏旧默认、顺序、失败屏障、原有消息身份、安全回收或出现无法解释的 ACK，停止合并；不绕过失败用例。正式发布不覆盖现有 tag。外部 Broker 故障验收只针对本轮自有实例。

## 参考

Redis 官方 `XPENDING`、`XCLAIM` 和 Lua 原子执行语义：
- https://redis.io/docs/latest/commands/xpending/
- https://redis.io/docs/latest/commands/xclaim/
- https://redis.io/docs/latest/develop/programmability/eval-intro/
