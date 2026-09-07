# 显式无必需消费组修复验收记录

日期：2026-09-07。范围：Core 的 `NoRequiredGroups` 加性契约、Redis Streams、NATS JetStream、统一文档及权威 skill。工作基线为 `feat/mq-message-lifecycle` 的 `e7cf45a` 加本次工作树补丁；没有修改 Bitzoom、创建版本、推送或合并。

## 契约与边界

- `NoRequiredGroups:true`、空必需组、正数保留时间、零重试表示纯保留主题；漏配继续拒绝。
- 零值不进入 fingerprint JSON，旧非空组策略的指纹表示不变；无组/有组切换必须受控迁移。
- capability 独立声明；普通/可靠订阅均不得为纯保留主题创建消费组。已存在或意外出现的组阻断回收，不忽略 pending。
- Redis Lua 原子检查空组集合并有界删除；NATS 在 purge 前复核无消费者，但管理面 ACL 和维护窗口仍是前提，不宣称提供原子 check-and-purge。
- 长期规范及接入示例见 [MQ 消息生命周期与 Provider 扩展标准](MQ_MESSAGE_LIFECYCLE_GUIDE.md)。

## RED → GREEN 证据

| 场景 | 修复前实际失败 | 修复后 |
| --- | --- | --- |
| 显式无组校验 | `required groups are empty` | 通过；漏配、组冲突、零保留、重试继续拒绝 |
| Redis 空主题 | `ERR no such key` | 原子初始化空 Stream，不插入伪消息 |
| NATS 无组回收 | 到期后期望回收 2，实际为 0 | 到期有界回收 |
| 普通订阅绕过 | 期望组不匹配错误，实际 nil | 在进入 Provider 前拒绝 |
| NATS 拒绝既有消费者 | 拒绝后仍存在错误 fingerprint | 检查通过后才登记指纹 |
| skill/文档同步 | 两份正文缺少 `NoRequiredGroups`，门禁失败 | 更新正文后门禁通过 |

## 真实 Broker 与自动化验证

隔离容器：`core-no-groups-redis-20260907`（Redis 7.4.10，AOF 开启）与 `core-no-groups-nats-20260907`（NATS 2.12.8，JetStream）。未使用 Bitzoom 的 Redis/NATS。宿主为 macOS，Broker 为本机 Docker 单节点。

- 真实 Redis/NATS 生命周期测试：通过。覆盖到期前保留、observe 到期不删、批量上限、无组策略冲突、容量拒绝、意外消费组保护和空主题观测。
- 共享 `VerifyMessageLifecycleConformance`：两个内置 Provider 均运行无组分支以及原有多组、失败 pending、离线组、恢复消费和回收分支。
- 持续发布/回收：每 Provider 发布 40 条，每 20ms 一条；最小保留 100ms、每批最多 4 条，回收循环间隔 10ms。发布完成时已回收超过一半消息，随后全部回收，验证保留量没有随累计发布量一直增长。这是小规模功能验证，不是生产容量 benchmark。
- `go test -race ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -count=1`：四包通过，设置了两个真实 Broker 环境变量。
- `go test -race ./pkg/server/mq -run 'NoRequiredGroups' -count=5`：真实 Redis/NATS 无组场景连续五轮通过。
- 真实 Broker 重启：`TestMQ{Redis,NATS}LifecycleBrokerRestartPrepare` → `docker restart` → `TestMQ{Redis,NATS}LifecycleBrokerRestartRecover`，准备与恢复是不同 Go 测试进程。原有 pending 消息恢复消费；新增无组主题的 3 条历史跨重启保留，observe 不删，enforce 分两批回收 2+1。
- 重启验证第一轮因 Docker 随机映射端口改变而连接失败；重新读取 `docker port` 后完整恢复测试通过，没有重建容器或丢弃数据来规避恢复验证。
- `api-compat`、`release-contract`、`check-ai-skill.sh`、`check-logging.sh`、`git diff --check`：通过。发布契约仅执行 candidate 检查，不会发布版本。

复验命令（地址由隔离 Broker 的当前映射提供）：

```bash
CORE_TEST_REDIS_ADDR=<redis地址> CORE_TEST_NATS_URL=<nats地址> \
  go test -race ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -count=1

# 两个阶段之间必须实际重启同一组 Broker，并保留数据；随机映射端口可能变化。
CORE_TEST_MQ_RESTART_TOKEN=<同一隔离令牌> \
CORE_TEST_REDIS_ADDR=<redis地址> CORE_TEST_NATS_URL=<nats地址> \
  go test -race -tags=integration ./tests/integration \
  -run '^TestMQ(Redis|NATS)LifecycleBrokerRestartPrepare$' -count=1
# 重启后将 Prepare 替换为 Recover，以相同令牌执行。
```

## 后续复验发现与修正（2026-09-07）

首次复验四包联跑出现 MQ 失败，后续单跑曾通过；没有将重跑通过等同于问题已消失。由于该首轮输出未完整保留，无法严格证明它的唯一失败原因。随后将完整输出保存到文件，复现并定位了以下两处既有测试问题：

- `TestRedisReliableKeyedPoisonKeyDoesNotBlockOtherKeys`：收到 B key 完成通知就立即断言 A 已尝试，错误假定不同 key 的执行顺序。原测试定向 race 100 次中失败 19 次，均为 `aAttempts=0`。正式修复用独立 channel 等待 A 的尝试通知，保留 B 不受阻、A2 不越序及 A 恢复后顺序处理的原断言；没有修改 Provider 或添加固定 sleep。
- `TestNewServiceContextWithConfig_EventBridgeAutoInit`：固定服务名对应的 ServiceContext 未在测试结束时关闭，`-count=3` 可复现 `service context config conflict`。修复通过 `t.Cleanup` 关闭并注销上下文，并断言关闭无错误、registry 已移除；不以随机服务名掩盖资源泄漏，不放宽生产配置冲突校验。

本轮只修改上述测试和本记录，未推送、合并或发布。

本轮复验使用新的隔离 Redis 7.4.10（AOF）与 NATS 2.12.8（JetStream）容器，完整日志保留在 `/tmp/core-mq-test-fix-eoZcO9/`：

- MQ 失败 key 测试定向 `-race -count=100`：通过（`mq-100.log`）。
- Router EventBridge 自动初始化测试定向 `-race -count=30`：通过（`router-30.log`）。
- 撤回临时配置后，两个已修复测试联合定向 `-race -count=3`：通过（`targeted-final-3.log`）。
- 四包联合 `-race -count=3`：MQ、Event、Observability 通过，Router 失败；不得将此轮记为全部通过（`four-packages-3.log`）。
- Router 新失败为 `TestNewServiceContext_ReadConfigInitializesRuntimeSubsystems`，单独 `-count=3` 同样复现：首次通过，后两次 `CrossNodeBroker` 未初始化（`readconfig-red.log`）。
- 根因是 LocalProvider 的槽位分配与注册规则不一致：`AllocateMachineID` 只排除 Running 节点，仍会选中处于冷却期的 Offline 槽位；`Register` 则拒绝该槽位。开启 `AutoMachineID` 的诊断尝试仍反复选中同一槽位，最终 panic（`readconfig-30.log`）。该测试配置尝试已撤回，`four-packages-final-3.log` 是尝试期间的失败诊断日志，不是最终验收结果。
- 这一问题涉及独立的 Cluster 生产逻辑，本轮未通过随机服务名、等待冷却或禁用冷却来掩盖，也未修改分配器。联合重复测试门禁仍未闭环。
- `git diff --check`、`check-ai-skill.sh`、`check-logging.sh`：通过。

## MachineID 阻塞项闭环（2026-09-07）

经用户授权，独立修复 `LocalProvider.AllocateMachineID`：分配时排除 Running 以及最后心跳仍在冷却期内的 Offline 槽位，与 `Register` 的现行条件一致。冷却时长、计时基准、注册写锁及冲突裁决保持不变；分配仅返回候选，不构成原子预留。没有修改 Router 启动测试配置来规避冷却。

新增五个表驱动场景覆盖冷却跳过、槽位耗尽、到期重用、服务隔离及数据中心隔离。先运行未修复代码，前两个场景实际失败（返回 0，分别期望 1 和 -1），再实施修复。日志目录：`/tmp/core-machineid-fix-EgdukR/`。

- `go test -race ./pkg/server/cluster -count=3`：通过（`cluster-race.log`）。
- 原失败启动测试 `TestNewServiceContext_ReadConfigInitializesRuntimeSubsystems` 定向 `-race -count=10`：通过（`router-repeat.log`）。
- 使用本轮隔离 Redis（AOF）与 NATS 2.12.8（JetStream），MQ、Event、Router、Observability 四包联合 `-race -count=3` 全部通过（`four-packages-3.log`），上述联合重复测试阻塞项闭环。
- `api-compat`、`release-contract --candidate`、`check-ai-skill.sh`、`check-logging.sh`、`git diff --check`：通过。未创建 tag、推送、合并或发布。
- 本轮未重跑 Broker 重启或多节点故障测试；此前的真实重启证据与以下未执行边界仍按各自批次记录。

## 未执行与不支持（范围保持不变）

- Bitzoom 接入、远程部署、生产负载与 OOM 恢复：`NOT RUN`。
- 多节点 Broker 故障切换、生产账号 ACL 端到端验证、宿主掉电：`NOT RUN`。
- “有可选消费者且允许过期删除其 pending”：未实现、不支持，不包含在本次纯保留主题语义中。
- 未协调的 NATS consumer 创建/重建与 purge 并发：不支持；定向 race 不证明跨 Broker 管理事务安全。
- 本次没有运行包含 etcd/Consul 的完整 `integration-external-docker` 套件，只对本次涉及的两种 MQ 执行隔离 Broker 与真实重启验证。
