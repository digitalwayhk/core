# Redis 服务发现通知流有界修复与验证

日期：2026-09-07。基线：`origin/main` 的 `3d48226`（`v1.1.0`），独立分支 `fix/redis-discovery-events`。本次不修改 Bitzoom，也不创建或移动版本标签。

## 根因与最小修复

`RedisProvider.Heartbeat` 复用 `Register`；注册和注销两段 Lua 原先均执行无界 `XADD`。即使没有消费组，Stream 中的历史也不会因此自动删除。Watch 从 `$` 监听，把事件作为重新读取节点快照的唤醒信号，同时保留 `ttl/2`（最短 100ms）周期对账。通知不是业务事实或历史恢复权威。

两段 Lua 现在都接收私有常量 `redisDiscoveryEventMaxLen = 10000`，执行 `XADD ... MAXLEN ~ ...`。不新增配置或公共 API，不创建消费组，不调用业务 MQ 的 `RequireMessageLifecycle`。节点 TTL 键、索引、service set、MachineID 槽位以及 Watch、切换逻辑均未修改。

Redis 的近似裁剪按内部宏节点执行，10,000 是目标保留数量，不是严格的瞬时长度或字节上限。采用原生默认裁剪预算，不执行无界扫描或独立的大批删除；具体语义见 [Redis XADD 官方说明](https://redis.io/docs/latest/commands/xadd/)。

## RED → GREEN

环境：macOS、Go 1.26.6；隔离 Docker Redis 7.4.10，standalone，AOF 开启，使用默认 Stream 宏节点配置。真实测试通过 `CORE_TEST_REDIS_ADDR` 接入，每个测试使用独立随机前缀；没有使用 Bitzoom Redis。

| 场景 | 修复前 XLEN | 修复后 XLEN | 验证内容 |
| --- | ---: | ---: | --- |
| 20,137 次重复 Register（另有首次注册） | 20,138 | 10,038 | 旧通知已移除、节点仍在、TTL 有效、槽位冲突仍拒绝 |
| 20,137 次 Heartbeat（另有首次注册） | 20,138 | 10,038 | 心跳刷新节点/索引/槽位 TTL，未创建消费组 |
| 预置 20,000 条旧历史后单独 Deregister | 20,002 | 10,011 | 注销自身触发裁剪、节点/索引/槽位已删除 |

上表为非 race 实测；race 下注销后为 10,084，符合近似裁剪语义。修复前三个容量断言均真实失败，修复后通过。

Watch 补偿测试在初始快照回调中暂停 Watch，在此期间注册或注销节点，再用其他服务通知挤出全部相关事件。确认 Stream 内不含被监视服务的通知后恢复 Watch，验证它仍通过周期对账收敛到最新节点状态。已有的 Watch 注册/注销通知测试同时通过。

## 已运行检查

- `gofmt`。
- `CORE_TEST_REDIS_ADDR=... go test ./pkg/server/cluster -count=1`：通过，含真实 Redis。
- `CORE_TEST_REDIS_ADDR=... go test -race ./pkg/server/cluster -count=1`：通过，含真实 Redis。
- `scripts/test.sh config-contract`：通过。
- `scripts/test.sh release-contract`：通过，包含 `api-compat`、`public-api`、配置及安全门禁，仅 candidate 检查。
- `scripts/check-logging.sh`、`git diff --check`：通过。
- `pkg/server/mq`、`pkg/server/event` 相对基线无改动，保留 `v1.1.0` 的 retention-only lifecycle。

本机完整日志位于 `/tmp/core-discovery-events-cAk9t5/`：`red.log`、`green-cluster.log`、`race-cluster.log`、`release-contract.log`。测试时长约 40 秒，不是 12 天持续运行或生产内存容量验证。

## 升级边界

- 建议合并后发布兼容补丁 `v1.1.1`；正式标签创建前只能引用修复分支的精确提交，`v1.1.0` 不含此修复。
- 使用同一 discovery 前缀的所有写入进程都应升级；混合部署时旧进程仍可无界追加，不能承诺全局持续有界。
- 已有超大通知历史会随后续注册、心跳或注销按 Redis 原生预算逐步回收。没有新写入时不会主动清理；不承诺第一条心跳立即清空约千万条历史，也不承诺 Redis RSS 同步下降。
- 保持 discovery 与业务 MQ 的 Prefix 隔离。应用不应直接订阅或依赖内部通知流的历史，更不应对业务 MQ 套用本次裁剪规则。
- 现场 1.03GB Stream、OOM 恢复、Bitzoom 部署、Redis 多节点故障切换与跨版本实机矩阵：`NOT RUN`。本次没有修改或清理任何现场数据。
