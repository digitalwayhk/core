# MQ 生命周期 worker 修复验证记录

日期：2026-09-08。基线：`v1.1.1` / `90a739a58ca0a7bfea95eff89da6dd1c37187577`。发布目标：`v1.1.2`。

## 环境和范围

- macOS arm64，Go 1.26.6。
- 专用 Redis：`redis:7.4-alpine`，`127.0.0.1:52954`，镜像 `sha256:4ab05801a605362b921756ce9dff4893add29c678076fe49a72d8cc3278806c6`。
- 专用 NATS：`nats:2.12.8-alpine`，JetStream，`127.0.0.1:52959`，镜像 `sha256:36c31459ac1dd3166e7b6a56dc48799e3355fc3c2eee66c73bcb6982c096d124`。
- 测试只操作 Core 隔离前缀和上述专用容器。未修改、部署或重启 Bitzoom。
- 原始日志保存于执行机 `/tmp/core-mq-worker-g8TeMR/`；长期契约见 [生命周期指南](MQ_MESSAGE_LIFECYCLE_GUIDE.md)。

## RED → GREEN 证据

| 场景 | 修复前或中间版本的失败 | 最终结果 |
| --- | --- | --- |
| 非 owner controller | 完整 Inspect 次数为 1，应为 0 | 通过，仅轻量容量读取 |
| 慢 Inspect | 90 ms 检查仍进入回收，没有预留阶段预算 | 检查阶段提前截止，不进入回收，整轮保持 100 ms deadline |
| 内置 Provider 协调 | Redis/NATS 均不具备锁前私有协调机制 | 13 方竞争每轮只有一个 owner，续约互斥，旧 owner 释放不能删除新租约 |
| 失败分类和旧 pending | deadline 原因计数为零；旧 pending 仍输出 | 原因计数正确，过期指标不伪装新鲜 |
| Redis Lua 放大 | 所有 standby 也执行 Lua，EVALSHA 为 1848 | GET 过滤 standby，EVALSHA 降至 220 |
| NATS 原地重启 | 已推进 completed / new-only enrollment 被误判为策略冲突 | 保留已存前沿，仍拒绝回退 |
| Redis 零保留期 | 模拟客户端时钟落后，已 ACK 消息回收为零 | 零保留不叠加客户端时间，定向 race 30 次通过 |
| NATS 大批次短预算 | 1000 条已到期消息、100 ms 轮次耗尽在候选读取，返回 deadline | 为 purge 预留剩余时间的一半，有界提交已验证前缀，持续取得进展 |
| Redis 公开直接回收 | 调用方无 deadline 时，200 ms Broker 暂停超过 policy 预算仍返回成功 | Provider 自身强制声明预算，按 deadline 返回 |

曾有一轮完整 race 的 Redis 全组回收断言失败，记录保留在 `race-three-rounds.log`，没有将其当作通过。随后补了零保留期确定性回归。新增定时器测试还发现测试替身的 `expired` 字段并发写，改用 atomic 后通过；不是通过禁用 race 处理。

## 多 controller 与命令放大

`TestLifecycleWorkerThirteenByFortyFour` 在真实 Redis/NATS 上模拟同一进程中的 13 个独立 controller、44 个 subject、100 ms budget，执行三轮竞争；不是 13 个独立业务服务进程 UAT。每个主题都声明实际测试组，容量门禁保持开启。

最终每次结果：

| 指标 | Redis | NATS |
| --- | ---: | ---: |
| 完整 Inspect | 132 | 132 |
| reclaim_fail_total 增量 | 0 | 0 |
| XINFO GROUPS | 132 | 不适用 |
| XINFO STREAM | 132 | 不适用 |
| XPENDING | 132 | 不适用 |
| EVALSHA | 220 | 不适用 |

完整扫描数等于 `44×3`，不再是 `13×44×3`。Redis 计数来自 Broker `INFO commandstats` 差值，包括 Lua 内部原生命令；无需清空统计。1716 是全部实例执行扫描时的逻辑次数，不冒充本次对旧版进行的生产压测结果。EVALSHA 的 1848 是修复中间版本的实测值，不能拿它与 Bitzoom 全量业务 EVALSHA 直接作百分比比较。

`TestLifecycleRegisteredWorkersSpreadStartup` 实际启动 13×44 个 worker 定时器，验证首次执行在时间窗口内分散；另验证周期抖动范围。非 owner 的 retained 容量观测不延长 pending/lag 新鲜度；跨实例相同 manifest 的 retained gauge 不应直接求和当作 Broker 去重后的总量。

## 最终测试

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:52954 \
CORE_TEST_NATS_URL=nats://127.0.0.1:52959 \
CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 \
go test -race ./pkg/server/mq -count=3 -v
```

最终通过，30.737 秒，日志 `race-purge-budget.log`（前一轮 `race-release.log` 也通过）。覆盖共享生命周期、发布/ACK、pending、离线必需组、无组连续回收、DLQ、失败重试、并发 publish/ack/reclaim、前沿回退、13×44 去重、启动抖动及大批次短预算推进。

真实网络超时测试只对专用 Redis 执行一次 200 ms `CLIENT PAUSE`，断言轮次在 150 ms 调度容差内返回 deadline；测试整个函数还包含等待 Broker 恢复及关闭清理，不能把函数总耗时当轮次预算。不得在应用 Redis 上开启 `CORE_TEST_LIFECYCLE_PAUSE_REDIS`。

- `SHOP_REDIS_ADDR=127.0.0.1:52954 go test -p 2 ./... -count=1`：最终重跑通过，日志 `full-suite-final.log`；示例 06 三进程测试 99.830 秒。默认外部环境保护下跳过的用例不因此视作真实外部验收。
- `go vet ./pkg/server/mq ./pkg/server/observability`：通过。
- `./scripts/test.sh release-contract`：通过，包含 api-compat、public-api、config-contract、security。
- `./scripts/check-ai-skill.sh`、`./scripts/check-logging.sh`、`git diff --check`：通过。
- `git diff --exit-code v1.1.1 -- go.mod go.sum pkg/server/mq/lifecycle.go`：通过；应用策略类型、规范化和指纹源码未变。

## v1.1.1 原地升级和 Broker 重启

使用同一 `scripts/testdata/mq-worker-migration/main.go` 分别链接旧版与候选源码，旧版实际模块校验和为：

```text
github.com/digitalwayhk/core v1.1.1
h1:jtPqT7GIMwGF+k6IeAYJ/XY0PBJJbsUBxsOFX78vxqU=
```

旧进程创建两个必需组，其中一个离线，发布两条消息，保存已推进前沿及一条 pending 后退出。新进程先检查原两条消息及 pending 仍在，再恢复两组消费，由 controller 回收；没有删除 metadata 或重建 Stream。

Redis/NATS 均输出：

```text
fingerprint=110f18cc4888cc319591aa40c0c202afdcaaee45ad349ab68f105927272fe3cc
preserved=2 pending>=1 recovered_and_reclaimed=true
```

最终源码再次验证通过，日志为 `migration-final-seed.log`、`migration-final-check.log`。另重新用旧版生成隔离数据，实际重启 `core-internal-notify-redis` 和 `core-internal-notify-nats` 两个专用容器，再由候选程序验证；两者均通过，日志 `broker-restart-seed.log`、`broker-restart-check.log`。

迁移测试曾使用 25 秒总等待，在 NATS 默认 `AckWait=30s` 时超时。原生状态证据为 primary pending=1、AckFloor=1，offline pending=0、AckFloor=2。测试总恢复窗口改为 60 秒后复测通过，应用 policy 始终为 `Interval=2s / BatchSize=2000 / TimeBudget=100ms`；没有延长应用回收预算。

## 发布与消费方边界

本修复无公共 Go API 或应用配置字段扩张。正保留期的 Redis ACL 需允许 `TIME`，权限不足停止回收；混跑旧版仍有旧进程重复扫描，全部升级后验收去重效果。不得移动已发布 tag。

Bitzoom 同拓扑 5 分钟空闲与容量阶梯复测：**NOT RUN，由 Bitzoom 升级正式版本后执行**。保持原 44 subjects、13 services 和 2 秒/100 ms policy；观察全体实例失败总量及固定原因、Broker 命令差值、pending/lag 和恢复回收，不只查看原 owner Users。
