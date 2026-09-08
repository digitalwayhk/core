# MQ 生命周期 worker 实施与验证清单

> 执行方式：使用 `superpowers:executing-plans` 在主线程顺序实施；按仓库 AGENTS.md 不派发子代理。

**目标：** 消除内置 Redis/NATS 多实例重复扫描和过期 context 回收，保持 v1.1.1 数据与策略兼容。

**架构：** controller 通过私有接口在 Inspect 前获取跨周期租约，非 owner 只更新容量；整轮预算内隔离观测/回收 context。Provider 继续持有原生 fencing 与消费安全边界。

**技术栈：** Go 1.26.6、go-redis、NATS JetStream、真实隔离 Broker、Go race。

## 实施顺序

- [x] 阅读权威 skill、controller、Provider、共享契约及发布规则，创建独立 worktree。
- [x] 在 `lifecycle_worker_test.go` 添加非 owner、慢 Inspect、独立 context、失败原因和旧 pending 指标测试，观察 RED 后修改 `lifecycle_controller.go`、`lifecycle_worker.go`、`lifecycle_metrics.go`。
- [x] 在 `lifecycle_worker_broker_test.go` 添加真实租约竞争测试，观察 Redis/NATS 缺失私有协作能力的 RED；实现两个 `provider_*_lifecycle_worker.go`，接入原有原子删除与 purge 路径。
- [x] 实施跨周期租约关闭释放、13×44 扫描门禁及真实 worker 启动分散验证。新增 EVALSHA 门禁先失败（1848），Redis standby 改 GET 后通过（220）。
- [x] 使用旧版 v1.1.1 二进制生成真实 metadata/pending 数据。发现 NATS 已推进前沿重启冲突，补 `TestNATSLifecycleReensurePreservesAdvancedFrontier` 的 RED→GREEN，不重置存量前沿。
- [x] 回归中发现 Redis 零保留期的客户端时钟边界问题，以 `TestRedisLifecycleZeroRetentionDoesNotUseClientClock` 复现，改为零保留只依赖消费完成、正保留使用 Broker TIME；定向 race 30 次通过。
- [x] 更新长期生命周期指南、消费者权威 skill、changelog；保留 `scripts/testdata/mq-worker-migration/main.go` 供双版本验证复用。
- [x] 合并前审查补充 NATS 大批次短预算和 Redis 公开直接回收的 RED→GREEN：候选读取为 purge 预留时间，Provider 直接调用也强制预算，真实 Broker 定向 race 与全包三轮通过。
- [x] 最终源码真实 Redis/NATS 全包 race 三轮、全仓测试及发布门禁全部通过。
- [ ] 核对最新远端 main、兼容 API 和候选版本，提交、合并、发布并验证 Go Module 解析。

## 可重复验证入口

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:52954 \
CORE_TEST_NATS_URL=nats://127.0.0.1:52959 \
CORE_TEST_LIFECYCLE_PAUSE_REDIS=1 \
go test -race ./pkg/server/mq -count=3 -v

SHOP_REDIS_ADDR=127.0.0.1:52954 go test -p 2 ./... -count=1
go vet ./pkg/server/mq ./pkg/server/observability
./scripts/test.sh release-contract
./scripts/check-ai-skill.sh
./scripts/check-logging.sh
```

`CORE_TEST_LIFECYCLE_PAUSE_REDIS=1` 仅允许对专用测试 Redis 开启：测试会执行一次 200 ms CLIENT PAUSE，以验证实际网络 deadline；不能指向应用 Redis。完整输出与未运行边界记录在 `docs/codex/MQ_LIFECYCLE_WORKER_VERIFICATION.md`。
