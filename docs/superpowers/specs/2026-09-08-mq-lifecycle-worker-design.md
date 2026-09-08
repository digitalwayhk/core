# MQ 生命周期 worker 去重与预算修复设计

## 范围与基线

基于 `origin/main` 的 `90a739a58ca0a7bfea95eff89da6dd1c37187577`（`v1.1.1`），仅修改 Core。目标是消除 Redis/NATS 生命周期后台检查随实例数放大的问题，并修复 Inspect 消耗回收 context 的问题。不修改或部署 Bitzoom，不变更其 44 subjects、13 services、2 秒周期、2000 条批次、100 ms 时间预算。

## 已核实的根因

- 通用 controller 每 subject 创建固定周期 worker；所有进程重复 Inspect。
- Redis 在 Inspect 后才竞争回收 owner；现有锁只保护删除阶段。
- NATS 在 Reclaim 内再次 Inspect，之后才竞争 owner。
- controller 将同一个 context 用于 Inspect 和 Reclaim，没有预留执行回收的时间。
- 非 owner 若简单停止 Inspect，本地容量快照会过期，触发 `ErrLifecycleCapacityUnknown`；不能只移动锁。

## 采用的方案

保留现有公共 `LifecycleMQProvider` 契约，通过内置 Provider 私有协作接口管理 worker 所有权和轻量容量采集。公开直接调用 Reclaim 的路径仍需独立取得并验证所有权，不允许私有优化绕开安全校验。自定义 Provider 保持原有接口兼容，不宣称具备内置 Provider 的跨实例去重能力。

不采用每轮释放的短锁：抖动之后不同实例仍可能顺序取得锁，依次完成相同扫描。不引入独立调度服务，也不增加应用配置。

### 所有权与故障切换

1. worker 在完整 Inspect 前，以 Broker 原生原子机制取得或续持 subject 租约。非 owner 不执行完整组和 pending 扫描。
2. 租约跨周期保持，并具有有限有效期；停止续持后其他实例可接管。续持只能由当前 token/revision 执行，不能覆盖新 owner。
3. Redis 复用已有 owner key，回收 Lua 仍在同一原子边界检查 owner、generation、fingerprint、组和 PEL。
4. NATS 复用既有 KV 锁的兼容编码，使用 revision 条件更新；purge 前复核所有权和策略。其管理面 ACL、durable 变更维护窗口要求不变，不宣称新增原子 check-and-purge。
5. 旧版短锁与新版租约必须互斥。混跑期间旧进程仍可能产生重复 Inspect，去重收益须在全部实例升级后验收；不得修改旧策略 metadata 或 fingerprint。
6. 清理和关闭不能使用无限期 `context.Background()` Broker 操作。租约过期提供兜底，旧 owner 不得删除新 owner 的锁。

### 预算与调度

- `Reclaim.TimeBudget` 继续表示整轮上限，100 ms 不扩张为两个 100 ms。
- 在总 deadline 内划分所有权/观测阶段与回收阶段，为回收预留时间。每个阶段使用独立子 context；Inspect 超时或剩余时间不足时停止本轮，不启动回收。
- 子阶段取消不能污染后续阶段；总 context 取消必须停止所有后续动作。不得用脱离 context 的 goroutine 或无限重试绕过预算。
- 启动相位及后续周期均加入有界抖动；同一 subject 的轮次不能重叠。注册验证与后台轮次分别计量，不能用启动校验的调用数冒充稳定期放大。
- 调度延迟、预算耗尽和 owner 竞争未获锁需明确区分。未取得 owner 是正常跳过；真正的 deadline/provider/fence 错误不可隐藏为成功。

### 非 owner 的容量与观测

非 owner 使用轻量 retained 数量/字节读取刷新容量门禁，不执行 XINFO GROUPS、XPENDING 或 durable 全组扫描。容量状态与完整生命周期快照的采集时间分离；不能用一次容量读取把旧 pending/lag 标记为新鲜。

硬容量读取失败或超期仍 fail closed。未知 pending、lag、oldest 等指标省略或标记 stale/not_collected，不填假零。现有总失败计数保留，同时增加固定枚举原因 `deadline`、`owner_lost`、`policy_fence`、`provider`，不引入动态 subject、消息 ID、payload 或原始错误标签。

## 不变的安全与兼容契约

策略规范化、fingerprint JSON、generation、enrollment/completed frontier 保持兼容。必需组、离线组、pending、失败重投、保留时间、无组主题检查及 DLQ 承诺不变。禁止清数据、重建 Stream、放宽门禁、改 observe 或延长应用预算作为修复。

## 验收与发布门禁

1. RED：增加多 controller、慢 Inspect、所有权竞争及抖动测试，记录原实现失败原因。
2. GREEN：13 controllers × 44 subjects、100 ms 的稳定空闲测试，失败计数零增量；验证非 owner 完整 Inspect 次数为零。
3. 真实 Redis 命令统计：XINFO/XPENDING 随 subject 数和实际 owner 轮次增长，不再乘实例数；同时记录 EVALSHA，区分发布/ACK/回收/租约操作。
4. 真实 Redis/NATS 覆盖发布、ACK、pending、离线必需组、无组、DLQ、并发 publish/ack/reclaim；MQ 全包 race 连续三轮。
5. 以 v1.1.1 创建实际 metadata 和保留/pending 数据，新实现按同一策略接管；验证指纹不变、未完成消息仍在、恢复后可消费与回收。不得以手写相似 metadata 代替旧版本生成证据。
6. 运行 gofmt、相关测试、config-contract、api-compat、release-contract、logging 和 skill 门禁。失败不得改为跳过；未运行真实测试标记 NOT RUN。
7. 更新长期生命周期指南、消费方权威 skill、changelog 及验证报告。全部必需门禁通过后重新核对远端 main/tag，合并并发布下一兼容补丁版（当前目标 v1.1.2），提供正式 tag、commit 和 Go Module 解析证据。

Core 测试与 Bitzoom 后续同拓扑 5 分钟空闲/容量阶梯复测是两个验收边界。Core 不代替 Bitzoom 声称生产复测通过。
