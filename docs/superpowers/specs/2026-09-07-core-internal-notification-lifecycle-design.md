# Core 内部通知生命周期修复设计

状态：用户已确认方案 1，设计提交 `7d29e84`；实现与验证记录见 `docs/codex/CORE_INTERNAL_NOTIFICATION_VERIFICATION.md`。目标发布为 `v1.1.1`，以兼容性和真实集成验证通过为前提。

## 1. 范围与已核实事实

本次检查 Core 自行产生的消息是否存在无界历史，不扩展到通用连接、注册表和缓存对象清理。

| 消息 | 当前路径 | 修复边界 |
| --- | --- | --- |
| 服务发现唤醒 | Redis ClusterProvider 的 Register/Heartbeat/Deregister Lua XADD | 已在 `ffff565` 统一使用私有上限 10000 的近似裁剪；节点 TTL 状态和周期快照不变 |
| 缓存失效 | `<service>.routecache.invalidate`，普通 MQBridge 订阅 | 改为框架内部广播加权威状态对账 |
| Casdoor 身份变更 | `<service>.auth.casdoor.identity.changed`，shared 模式普通 MQBridge 订阅 | 改为框架内部广播加认证权威校验、会话保护 |
| 本地认证不可用、WebSocket 本地通知 | 进程内 EventBridge | 不新增 Broker 持久化；验证队列和失败路径 |
| 应用事件、Outbox、重试、DLQ | 业务 MQ 生命周期 | 不裁剪、不改变 ACK/重试/必需消费组规则 |

当前内置 MQ Provider 为 Redis Streams 和 NATS JetStream。其他 ClusterProvider 的发现机制不能据此等同于 Redis Stream；继续核对其实际消息存储，未发现追加历史的实现不增加无意义的删除操作。

缓存和认证的普通外部订阅使用共享消费组/consumer，并非每副本独立广播。Redis ACK 不删除 Stream entry；NATS 普通 Stream 未设置历史保留上限。因此仅添加保留时间不能同时修复历史增长和副本通知问题。

## 2. 选定方案与可靠性边界

使用原生 Redis Pub/Sub、Core NATS 普通订阅实现内部广播，不使用 queue group，不创建 Stream 或 durable consumer。广播仅加速同步，不承诺离线重放、持久化确认或每副本消费完成。

可靠性来自权威状态及补偿，不来自瞬时消息。Redis Pub/Sub 为至多一次且不按 DB 隔离，见 [Redis 官方说明](https://redis.io/docs/latest/develop/pubsub/)；Core NATS 发布订阅见 [NATS 官方说明](https://docs.nats.io/learn/core-nats/)。因此必须显式处理丢通知和命名空间，不能把正常 publish 返回当作全副本处理成功。

不采用以下替代方案：

- 给现有单共享消费组加保留：无法保证全部副本完成缓存失效或认证撤销。
- 每个临时副本建立持久消费组：需要离线成员退出协议；不能自动删除离线组来维持容量，不符合本次最小修复范围。

## 3. 组件边界

由 ServiceContext 组合根装配专用内部通知适配器，复用现有本地事件分发和配置中的连接信息。只接管框架明确登记的缓存失效、身份变更主题；不得通过字符串后缀匹配接管应用自定义消息。

- 不修改公共 `MQProvider` 接口、普通 `Publish/Subscribe` 或 `RequireMessageLifecycle` 的语义。
- 不把新的通用瞬时消息 API 作为本次 PATCH 的公共能力导出。
- 独立命名空间包含环境前缀、服务及协议版本；Redis 还必须显式编码 DB 隔离维度。
- NATS 新 subject 不得被已有 JetStream subject/wildcard 捕获；启动校验无法证明隔离或权限不足时拒绝启用相关 shared 能力，不能假装无持久化。
- 不支持内部广播机制的自定义 Provider 不静默降级；错误明确指出受影响的 shared 功能。普通业务 MQ 不因此改写。
- 订阅、恢复 worker 和连接由 ServiceContext 持有并关闭；初始化失败同样回收。不能把短时初始化 context 当作长期订阅生命周期。

## 4. 状态恢复与失败保护

内部通知健康状态分为：初始化、可用、失效、恢复中、已关闭。恢复代次用于阻止旧 worker 或旧连接在新一轮断线之后错误地宣告可用。

订阅成功并完成首次权威同步后才允许依赖通知的本地加速。断线、重连、慢消费者、接收队列溢出、解析/处理失败和健康检查超时均使通道失效；任何不确定状态保守处理。重连成功不等于恢复完成。

### 缓存

- 通道失效时停止相关 L1/L2 命中，走现有降级路径；不把旧缓存当作权威结果。
- 重新订阅后读取权威 generation 并使本地记录版本逻辑失效，确认本轮恢复期间没有再次失效后才能恢复加速。实现采用原物理 key 内的版本标记，避免每秒扫描删除 L2 或生成新的一套 key；原 TTL/容量继续物理回收。
- 定期对账必须覆盖“状态修改成功、通知发布前进程崩溃”，不能只处理消费者连接断开。
- 对 key 级失效验证现有 generation 是否足以识别漏通知；不足时采用有界的本地清空补偿，不能只刷新 route generation 后宣称 key 已收敛。
- 保留既有 TTL 上限，不延长旧值可见窗口。对账间隔、超时和实际最坏收敛窗口在实现计划及测试中固定，不能宣称广播提供线性一致性。

### 认证和 WebSocket

- HTTP/新会话授权继续读取认证权威，禁止用通知快照替代 `Authorize` 的权威校验。
- 通道失效时关闭可能漏撤销的 Casdoor 会话，并阻止恢复前重新建立同类长连接；其他认证域不受影响。
- 仅断线回调不足以覆盖发布方崩溃：对存量会话周期核对权威身份 generation/blocked，带批量与时间预算；无法在规定新鲜度窗口内完成检查则关闭受影响会话。
- 权威不可用时 fail closed，不延长旧身份有效期。并发登录、撤销和恢复须通过代次或等价同步防止旧校验结果重新激活失效会话。
- 撤销传播存在可测量的检测窗口，文档必须公布实测及配置/内部常量界限，不承诺网络分区发生瞬间就同步关闭远端连接。

## 5. 容量与观测

新通道不保留 Broker 历史；客户端待处理队列、重连缓冲、payload 大小和后台任务并发仍必须有界。缓冲溢出或发布失败必须报错/触发保护，不允许无界重连缓冲或重试队列。

沿用低基数日志/指标惯例，记录通道状态、发布失败、连续性间隙、缓存补偿成功/失败与最近耗时。队列压力及受保护会话数量此版未采集，不能填假零；参见长期标准中的观测边界。不得加入 UserID、消息 ID、token 或 payload 标签。

不提供离线积压/ACK 指标来伪装原生广播具有可靠消费组。业务 MQ 的 pending/lag、DLQ 和容量指标保持原契约。

## 6. 升级、旧数据与回滚

公共 Go API 和配置结构保持兼容，但内部通知协议发生变化，须在 Changelog 和迁移指南显著说明：

1. 同一环境、同一逻辑服务的相关实例采用协调维护窗口升级；停止旧实例后再开放新实例流量。不声称旧 Stream 协议与新广播协议可安全混跑。
2. 新实例不继续向旧内部 Stream 追加，也不消费旧通知作为恢复权威；首次同步读取当前状态。
3. 升级不自动删除旧 Stream、consumer 或其他历史键。因此修复阻止新增堆积，不等同于已释放存量内存。
4. 文档提供精确资源清单与维护步骤：确认全部旧写入方停止、资源仅含框架内部通知、无应用依赖后，由运维显式授权清理旧资源。禁止通配删除，业务项目无需编写日常 XTRIM。
5. 回滚同样停止新实例后统一恢复旧版本。旧版本重新无界写入的风险必须告警，不能把回滚当作容量修复。

若实现证明必须改变已登记公共 API 或不能满足 PATCH 兼容性，停止发布 `v1.1.1` 并报告，不自行改版本号或绕过兼容门禁。

## 7. 测试与交付门禁

按 RED → GREEN → 定向 race 执行，记录每轮命令、精确提交、Broker 版本、工作负载和结果。

- Redis 发现：保留现有超过上限的 Register/Heartbeat/Deregister、Watch 及裁剪后周期对账真实测试。
- Redis/NATS 内部通知：至少两个独立进程/连接的同服务副本均收到通知；无 consumer group/durable 创建；持续发布不新增内部历史 Stream。
- 故障：订阅断开、Broker 重启、发布进程在权威写入后崩溃、接收端慢处理、队列满、错误 payload、初始化失败、关闭和恢复竞争。
- 缓存：route/key 失效、漏通知后对账收敛、恢复前不得命中陈旧本地值。
- 认证：真实 WebSocket 撤销、断线保护、恢复期间登录竞争、漏广播后权威核对、其他用户和认证域隔离。
- 隔离：Redis 不同 DB/Prefix，NATS subject 捕获冲突与权限不足，应用同名/相似 subject 不被接管。
- 回归：业务 MQ ACK、pending、多组、retention-only、重试、DLQ 与 Outbox 不变；旧内部 Stream 升级后仍保留且新发布不增长。
- 执行 gofmt、受影响包测试/race、cluster 全包测试/race、config-contract、api-compat、public-api、security、release-contract、check-logging。

未执行的真实 Broker/进程测试标为 `NOT RUN`，不能以 mock 代替。完成后更新唯一权威 skill 分片、统一消息生命周期指南、配置能力矩阵、迁移说明、CHANGELOG 和验证证据，再合并 main 并发布。不得修改 Bitzoom 工作区。
