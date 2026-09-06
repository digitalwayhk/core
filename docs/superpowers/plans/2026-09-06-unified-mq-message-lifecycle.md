# Core 统一 MQ 消息生命周期 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Core 的 Redis Streams 和 NATS JetStream Provider 建立统一、fail-closed、可观测的消息生命周期与安全回收契约。

**Architecture:** 应用通过 `ServiceContext.RequireMessageLifecycle` 声明 Subject 策略，`MQManager` 负责冻结、能力校验、背压、调度和指标汇总，Provider 使用 Broker 原生状态计算全部必需组的连续 ACK 前沿并执行有界回收。旧应用不声明策略时保持无自动删除的现行行为。

**Tech Stack:** Go 1.26.6、go-redis/v9、nats.go JetStream、Prometheus collector、testify、Docker Compose 真实 Redis 7.2/NATS 2.12。

---

### Task 1: 定义统一策略、能力与快照契约

**Files:**
- Create: `pkg/server/mq/lifecycle.go`
- Create: `pkg/server/mq/lifecycle_test.go`
- Modify: `pkg/server/mq/mq.go`

- [ ] **Step 1: 先写策略规范化和校验失败测试**

```go
func TestLifecyclePolicyValidateRejectsUnsafeOrAmbiguousPolicy(t *testing.T) {
	tests := []LifecyclePolicy{
		{Subject: ""},
		{Subject: "fills", Mode: LifecycleModeEnforce},
		{Subject: "fills", Mode: LifecycleModeEnforce, RequiredGroups: []ConsumerGroupRequirement{{Name: "positions"}, {Name: "positions"}}},
		{Subject: "fills", Mode: LifecycleModeEnforce, RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: "latest"}}},
		{Subject: "fills", Mode: LifecycleModeEnforce, RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: StartFromAllRetained}}, Reclaim: ReclaimBudget{BatchSize: -1}},
	}
	for _, policy := range tests {
		require.Error(t, policy.Validate())
	}
}

func TestLifecyclePolicyFingerprintIgnoresGroupDeclarationOrder(t *testing.T) {
	a := validLifecyclePolicy("users", "positions")
	b := validLifecyclePolicy("positions", "users")
	require.Equal(t, a.Fingerprint(), b.Fingerprint())
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/mq -run '^TestLifecyclePolicy' -count=1`

Expected: FAIL，缺少 `LifecyclePolicy`、`Validate` 和 `Fingerprint`。

- [ ] **Step 3: 实现最小公开契约**

```go
const (
	LifecycleModeObserve LifecycleMode = "observe"
	LifecycleModeEnforce LifecycleMode = "enforce"
	StartFromAllRetained ConsumerStartPosition = "all-retained"
	StartFromNew ConsumerStartPosition = "new-only"
)

type LifecyclePolicy struct {
	Subject        string
	Mode           LifecycleMode
	RequiredGroups []ConsumerGroupRequirement
	Retention      RetentionPolicy
	Retry          RetryPolicy
	Capacity       CapacityPolicy
	Reclaim        ReclaimBudget
}

type LifecycleMQProvider interface {
	MQProvider
	LifecycleCapabilities() LifecycleCapabilities
	EnsureLifecycle(context.Context, LifecyclePolicy) error
	InspectLifecycle(context.Context, LifecyclePolicy) (LifecycleSnapshot, error)
	ReclaimLifecycle(context.Context, LifecyclePolicy, LifecycleSnapshot) (ReclaimResult, error)
}
```

`LifecycleCapabilities` 显式包含 durable publish ACK、required groups、retry、DLQ、safe reclaim、retained bytes 和 oldest-age 布尔位。`LifecycleSnapshot` 的数值使用 `*int64`/`*time.Duration`，未采集时为 `nil` 并带 `StateNotCollected`。

- [ ] **Step 4: 运行 GREEN 并格式化**

Run: `gofmt -w pkg/server/mq/lifecycle.go pkg/server/mq/lifecycle_test.go && go test ./pkg/server/mq -run '^TestLifecyclePolicy' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq/lifecycle.go pkg/server/mq/lifecycle_test.go pkg/server/mq/mq.go
git commit -m "feat(mq): define message lifecycle contract"
```

### Task 2: MQManager 策略冻结、回收调度与背压

**Files:**
- Create: `pkg/server/mq/lifecycle_controller.go`
- Create: `pkg/server/mq/lifecycle_controller_test.go`
- Modify: `pkg/server/mq/manager.go`
- Modify: `pkg/server/mq/manager_test.go`

- [ ] **Step 1: 先写 capability、冲突、observe-only、有界回收和硬限测试**

```go
func TestRequireMessageLifecycleFailsClosedWithoutCapability(t *testing.T) {
	m := managerWithProvider(&fakeProvider{})
	require.ErrorIs(t, m.RequireMessageLifecycle(context.Background(), validLifecyclePolicy("positions")), ErrLifecycleUnsupported)
}

func TestRequireMessageLifecycleRejectsConflictingFrozenPolicy(t *testing.T) {
	m, provider := managerWithLifecycleProvider()
	require.NoError(t, m.RequireMessageLifecycle(context.Background(), validLifecyclePolicy("positions")))
	require.ErrorIs(t, m.RequireMessageLifecycle(context.Background(), validLifecyclePolicy("users")), ErrLifecyclePolicyConflict)
	require.Equal(t, 1, provider.ensureCalls)
}

func TestLifecycleObserveNeverReclaims(t *testing.T) {
	m, provider := managerWithLifecycleProvider()
	policy := validLifecyclePolicy("positions")
	policy.Mode = LifecycleModeObserve
	require.NoError(t, m.RequireMessageLifecycle(context.Background(), policy))
	provider.runScheduledInspection()
	require.Zero(t, provider.reclaimCalls)
}

func TestLifecycleEnforceHonorsBatchAndTimeBudget(t *testing.T) {
	m, provider := managerWithLifecycleProvider()
	policy := validLifecyclePolicy("positions")
	policy.Reclaim = ReclaimBudget{Interval: time.Hour, BatchSize: 7, TimeBudget: 20 * time.Millisecond}
	require.NoError(t, m.RequireMessageLifecycle(context.Background(), policy))
	provider.runScheduledInspection()
	require.Equal(t, ReclaimBudget{Interval: time.Hour, BatchSize: 7, TimeBudget: 20 * time.Millisecond}, provider.lastReclaimBudget)
}

func TestPublishRejectsAtFreshHardCapacityAndFailsClosedWhenSnapshotStale(t *testing.T) {
	m, provider := managerWithLifecycleProvider()
	policy := validLifecyclePolicy("positions")
	policy.Capacity.HardMessages = 10
	require.NoError(t, m.RequireMessageLifecycle(context.Background(), policy))
	provider.setSnapshot(11, time.Now())
	require.ErrorIs(t, m.Publish(context.Background(), policy.Subject, []byte("x"), nil), ErrLifecycleBackpressure)
	provider.setSnapshot(1, time.Now().Add(-2*policy.Reclaim.Interval))
	require.ErrorIs(t, m.Publish(context.Background(), policy.Subject, []byte("x"), nil), ErrLifecycleCapacityUnknown)
}

func TestCloseStopsLifecycleWorkersBeforeProviderClose(t *testing.T) {
	m, provider := managerWithLifecycleProvider()
	require.NoError(t, m.RequireMessageLifecycle(context.Background(), validLifecyclePolicy("positions")))
	require.NoError(t, m.Close())
	require.Equal(t, []string{"worker-stopped", "provider-closed"}, provider.lifecycleEvents())
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/mq -run 'Test(RequireMessageLifecycle|Lifecycle|PublishRejects)' -count=1`

Expected: FAIL，Manager 还没有 lifecycle controller。

- [ ] **Step 3: 实现 controller 和发布门禁**

```go
func (m *MQManager) RequireMessageLifecycle(ctx context.Context, policy LifecyclePolicy) error {
	provider, err := m.lifecycleProvider()
	if err != nil { return err }
	return m.lifecycle.require(ctx, provider, policy)
}

func (m *MQManager) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	if err := m.lifecycle.allowPublish(subject); err != nil { return err }
	return m.publishCurrent(ctx, subject, data, opts)
}
```

controller 每个 Subject 只启动一个 worker。首次 require 同步 `EnsureLifecycle` 和 `InspectLifecycle`；enforce 才调用 reclaim。硬限使用有最大年龄的快照，快照过期或未采集时返回 `ErrLifecycleCapacityUnknown`。

- [ ] **Step 4: 运行 GREEN 与 race**

Run: `gofmt -w pkg/server/mq/lifecycle_controller.go pkg/server/mq/lifecycle_controller_test.go pkg/server/mq/manager.go pkg/server/mq/manager_test.go && go test -race ./pkg/server/mq -run 'Test(RequireMessageLifecycle|Lifecycle|PublishRejects|Close)' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq/lifecycle_controller.go pkg/server/mq/lifecycle_controller_test.go pkg/server/mq/manager.go pkg/server/mq/manager_test.go
git commit -m "feat(mq): enforce lifecycle policies in manager"
```

### Task 3: ServiceContext 声明与实际订阅组校验

**Files:**
- Modify: `pkg/server/router/servicecontext.go`
- Create: `pkg/server/router/servicecontext_mq_lifecycle_test.go`
- Modify: `pkg/server/event/mqbridge.go`
- Modify: `pkg/server/event/mqbridge_reliable_test.go`

- [ ] **Step 1: 先写组合根 API 和本地订阅与 manifest 冲突测试**

```go
func TestServiceContextRequireMessageLifecycleDelegatesToManager(t *testing.T) {
	sc, provider := newLifecycleServiceContext(t, "positions")
	policy := validLifecyclePolicy("positions")
	require.NoError(t, sc.RequireMessageLifecycle(context.Background(), policy))
	require.Equal(t, policy.Fingerprint(), provider.ensuredPolicy.Fingerprint())
}

func TestReliableSubscriptionRejectedWhenLocalGroupMissingFromFrozenPolicy(t *testing.T) {
	sc, _ := newLifecycleServiceContext(t, "positions")
	require.NoError(t, sc.RequireMessageLifecycle(context.Background(), validLifecyclePolicy("users")))
	_, err := sc.SubscribeEvent(event.Subscription{Subject: "fills", Reliable: true, Handler: successfulHandler})
	require.ErrorIs(t, err, mq.ErrLifecycleRequiredGroupMismatch)
}

func TestLegacyReliableSubscriptionWithoutLifecyclePolicyRemainsCompatible(t *testing.T) {
	sc, _ := newLifecycleServiceContext(t, "positions")
	cancel, err := sc.SubscribeEvent(event.Subscription{Subject: "fills", Reliable: true, Handler: successfulHandler})
	require.NoError(t, err)
	cancel()
}

func TestBroadcastLifecycleRequiresExplicitStableInstanceGroup(t *testing.T) {
	sc, _ := newLifecycleServiceContext(t, "positions")
	policy := validLifecyclePolicy("positions")
	require.NoError(t, sc.RequireMessageLifecycle(context.Background(), policy))
	_, err := sc.SubscribeEvent(event.Subscription{Subject: "fills", Reliable: true, Broadcast: true, Handler: successfulHandler})
	require.ErrorIs(t, err, mq.ErrLifecycleRequiredGroupMismatch)
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/router ./pkg/server/event -run 'Test(ServiceContextRequireMessageLifecycle|ReliableSubscriptionRejected|LegacyReliable|BroadcastLifecycle)' -count=1`

Expected: FAIL，缺少 ServiceContext API 和 manager group validation。

- [ ] **Step 3: 实现最小委托**

```go
func (own *ServiceContext) RequireMessageLifecycle(ctx context.Context, policy mq.LifecyclePolicy) error {
	if own == nil || own.MQManager == nil { return mq.ErrNotConnected }
	return own.MQManager.RequireMessageLifecycle(ctx, policy)
}
```

`MQBridge.SubscribeReliableWithOptions` 在订阅前调用 `ValidateRequiredGroup(subject, subscriberID)`。只在已冻结策略存在时校验，因此不改变旧应用。

- [ ] **Step 4: 运行 GREEN 和 race**

Run: `gofmt -w pkg/server/router/servicecontext.go pkg/server/router/servicecontext_mq_lifecycle_test.go pkg/server/event/mqbridge.go pkg/server/event/mqbridge_reliable_test.go && go test -race ./pkg/server/router ./pkg/server/event -run 'MessageLifecycle|ReliableSubscription|LegacyReliable|BroadcastLifecycle' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/router/servicecontext.go pkg/server/router/servicecontext_mq_lifecycle_test.go pkg/server/event/mqbridge.go pkg/server/event/mqbridge_reliable_test.go
git commit -m "feat(event): bind subscriptions to lifecycle manifest"
```

### Task 4: Redis 必需组、安全前沿与有界回收

**Files:**
- Create: `pkg/server/mq/provider_redis_lifecycle.go`
- Create: `pkg/server/mq/provider_redis_lifecycle_test.go`
- Modify: `pkg/server/mq/provider_redis.go`

- [ ] **Step 1: 先写真实 Redis 失败测试**

```go
func TestRedisLifecycleOfflineRequiredGroupBlocksReclaim(t *testing.T) {
	h := newRedisLifecycleHarness(t, "online", "offline")
	h.publishOld("m1")
	h.consumeAndAck("online", "m1")
	require.Zero(t, h.reclaim().Reclaimed)
	require.Equal(t, []string{"m1"}, h.streamBodies())
}

func TestRedisLifecyclePendingMessageBlocksReclaim(t *testing.T) {
	h := newRedisLifecycleHarness(t, "positions")
	h.publishOld("m1")
	h.deliverWithoutAck("positions", "m1")
	require.Zero(t, h.reclaim().Reclaimed)
	require.Equal(t, int64(1), h.pending("positions"))
}

func TestRedisLifecycleAllGroupsAndMinAgePermitBoundedReclaim(t *testing.T) {
	h := newRedisLifecycleHarness(t, "users", "positions")
	h.setBatchSize(2)
	h.publishOld("m1", "m2", "m3")
	h.consumeAndAckAll("users")
	h.consumeAndAckAll("positions")
	result := h.reclaim()
	require.LessOrEqual(t, result.Reclaimed, int64(2))
	require.NotContains(t, h.streamBodies(), "m1")
}

func TestRedisLifecycleStartFromNewPersistsEnrollmentFrontier(t *testing.T) {
	h := newRedisLifecycleHarness(t)
	h.publishOld("history")
	h.addGroup("new-reader", StartFromNew)
	require.Equal(t, h.lastStreamID(), h.enrollmentFrontier("new-reader"))
}

func TestRedisLifecycleMissingGroupOrRegressedFrontierFailsClosed(t *testing.T) {
	h := newRedisLifecycleHarness(t, "positions")
	h.destroyGroup("positions")
	_, err := h.inspect()
	require.ErrorIs(t, err, ErrLifecycleStateUncertain)
}

func TestRedisLifecycleConcurrentPublishAckAndReclaimDoesNotLoseMessages(t *testing.T) {
	h := newRedisLifecycleHarness(t, "positions")
	h.runConcurrentPublishConsumeReclaim(500)
	require.Equal(t, 500, h.uniqueConsumedCount())
	require.Empty(t, h.missingPublishedIDs())
}
```

测试使用 `CORE_TEST_REDIS_ADDR`，每个 case 生成独立 prefix，清理只删除该 prefix。

- [ ] **Step 2: 运行 RED**

Run: `CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test ./pkg/server/mq -run '^TestRedisLifecycle' -count=1`

Expected: FAIL，Redis Provider 未实现 `LifecycleMQProvider`。

- [ ] **Step 3: 实现 Redis 原生机制**

```go
func (r *RedisStreamProvider) LifecycleCapabilities() LifecycleCapabilities {
	return redisLifecycleCapabilities()
}
func (r *RedisStreamProvider) EnsureLifecycle(ctx context.Context, policy LifecyclePolicy) error {
	return r.lifecycle.ensure(ctx, r.client, policy)
}
func (r *RedisStreamProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	return r.lifecycle.inspect(ctx, r.client, policy)
}
func (r *RedisStreamProvider) ReclaimLifecycle(ctx context.Context, policy LifecyclePolicy, snapshot LifecycleSnapshot) (ReclaimResult, error) {
	return r.lifecycle.reclaim(ctx, r.client, policy, snapshot)
}
```

元数据 key 只含 prefix+subject hash，值中保存 policy fingerprint、generation、group enrollment frontier 和 monotonic completed frontier。回收脚本必须同时比较 lease owner、generation 和 fingerprint。

- [ ] **Step 4: 运行 GREEN 、重复性和 race**

Run: `gofmt -w pkg/server/mq/provider_redis_lifecycle.go pkg/server/mq/provider_redis_lifecycle_test.go pkg/server/mq/provider_redis.go && CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test -race ./pkg/server/mq -run '^TestRedisLifecycle' -count=1`

Run: `CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test ./pkg/server/mq -run '^TestRedisLifecycle' -count=10`

Expected: 两者 PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq/provider_redis.go pkg/server/mq/provider_redis_lifecycle.go pkg/server/mq/provider_redis_lifecycle_test.go
git commit -m "feat(mq): reclaim Redis streams after every required group"
```

### Task 5: Redis 重试与原子 DLQ

**Files:**
- Create: `pkg/server/mq/provider_redis_retry.go`
- Create: `pkg/server/mq/provider_redis_retry_test.go`
- Modify: `pkg/server/mq/provider_redis.go`

- [ ] **Step 1: 先写真 Redis 重试/DLQ 测试**

```go
func TestRedisRetryKeepsFailedMessagePendingBeforeLimit(t *testing.T) {
	h := newRedisRetryHarness(t, 3)
	h.failNextDelivery()
	require.Equal(t, int64(1), h.pending())
	require.Empty(t, h.deadLetters())
}

func TestRedisRetryMovesToDLQBeforeAckAtLimit(t *testing.T) {
	h := newRedisRetryHarness(t, 2)
	h.failDeliveries(2)
	require.Zero(t, h.pending())
	require.Len(t, h.deadLetters(), 1)
}

func TestRedisDLQScriptIsIdempotentAcrossProcessRestart(t *testing.T) {
	h := newRedisRetryHarness(t, 1)
	h.interruptAfterDLQCommit()
	h.restartProvider()
	h.resume()
	require.Len(t, h.deadLetters(), 1)
	require.Zero(t, h.pending())
}

func TestRedisDLQFailureLeavesOriginalPending(t *testing.T) {
	h := newRedisRetryHarness(t, 1)
	h.breakDeadLetterDestination()
	h.failNextDelivery()
	require.Equal(t, int64(1), h.pending())
}
```

- [ ] **Step 2: 运行 RED**

Run: `CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test ./pkg/server/mq -run '^TestRedis(Retry|DLQ)' -count=1`

Expected: FAIL，当前失败无上限且无 DLQ。

- [ ] **Step 3: 实现调度退避与 Lua 原子转移**

```lua
-- KEYS: retry hash, dlq stream, source stream, dedupe hash
-- ARGV: group, source id, stable dlq id, payload metadata
-- 已转移则只 XACK；未转移则 XADD DLQ、记录 dedupe、XACK、HDEL retry。
```

Handler 每次真实投递前原子增加 attempt；达上限前保持 pending 并按策略 backoff，达上限后执行原子 DLQ 转移。DLQ 不记录 payload。

- [ ] **Step 4: 运行 GREEN 与 race**

Run: `gofmt -w pkg/server/mq/provider_redis_retry.go pkg/server/mq/provider_redis_retry_test.go pkg/server/mq/provider_redis.go && CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test -race ./pkg/server/mq -run '^TestRedis(Retry|DLQ)' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq/provider_redis.go pkg/server/mq/provider_redis_retry.go pkg/server/mq/provider_redis_retry_test.go
git commit -m "feat(mq): add bounded Redis retry and atomic dead letter"
```

### Task 6: NATS 统一可靠订阅、重试与 DLQ

**Files:**
- Create: `pkg/server/mq/provider_nats_reliable.go`
- Create: `pkg/server/mq/provider_nats_reliable_test.go`
- Modify: `pkg/server/mq/provider_nats.go`

- [ ] **Step 1: 先写真 NATS 可靠语义测试**

```go
func TestNATSReliableSubscribersUseIndependentDurables(t *testing.T) {
	h := newNATSReliableHarness(t, "users", "positions")
	h.publish("m1")
	require.Equal(t, "m1", h.receiveAndAck("users"))
	require.Equal(t, "m1", h.receiveAndAck("positions"))
}

func TestNATSReliableHandlerFailureRedeliversAfterBackoff(t *testing.T) {
	h := newNATSReliableHarness(t, "positions")
	h.failOnceThenSucceed()
	require.Equal(t, uint64(2), h.lastDeliveryAttempt())
	require.GreaterOrEqual(t, h.retryDelay(), h.policy().Retry.Backoff[0])
}

func TestNATSReliableRestartContinuesFromDurableState(t *testing.T) {
	h := newNATSReliableHarness(t, "positions")
	h.publish("m1", "m2")
	require.Equal(t, "m1", h.receiveAndAck("positions"))
	h.restartProvider()
	require.Equal(t, "m2", h.receiveAndAck("positions"))
}

func TestNATSReliableAtLimitPublishesDLQBeforeTerm(t *testing.T) {
	h := newNATSReliableHarness(t, "positions")
	h.failUntilLimit()
	require.Len(t, h.deadLetters(), 1)
	require.True(t, h.originalTerminatedAfterDLQPublishAck())
}

func TestNATSReliableDLQPublishFailureDoesNotTermOriginal(t *testing.T) {
	h := newNATSReliableHarness(t, "positions")
	h.makeDLQPublishFail()
	h.failUntilLimit()
	require.False(t, h.originalTerminated())
	require.Greater(t, h.consumerPending(), uint64(0))
}

func TestNATSReliableMaxAckPendingAppliesBackpressure(t *testing.T) {
	h := newNATSReliableHarness(t, "positions")
	h.setMaxAckPending(1)
	h.blockFirstHandlerAndPublishTwo()
	require.Equal(t, 1, h.concurrentHandlers())
}
```

- [ ] **Step 2: 运行 RED**

Run: `CORE_TEST_NATS_URL=nats://127.0.0.1:4222 go test ./pkg/server/mq -run '^TestNATSReliable' -count=1`

Expected: FAIL，NATS Provider 未实现 `ReliableMQProvider`。

- [ ] **Step 3: 实现 JetStream durable consumer**

```go
func (n *NATSJetStreamProvider) SubscribeReliable(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	return n.subscribeReliable(ctx, subject, options, handler)
}
```

从 JetStream message metadata 读取 delivery count。Handler 失败且未到上限时 `NakWithDelay`；到上限时先使用稳定 `Nats-Msg-Id` 发布 DLQ 并等待 ACK，成功后 `Term`。panic 转为 Handler 失败。

- [ ] **Step 4: 运行 GREEN 与 race**

Run: `gofmt -w pkg/server/mq/provider_nats.go pkg/server/mq/provider_nats_reliable.go pkg/server/mq/provider_nats_reliable_test.go && CORE_TEST_NATS_URL=nats://127.0.0.1:4222 go test -race ./pkg/server/mq -run '^TestNATSReliable' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq/provider_nats.go pkg/server/mq/provider_nats_reliable.go pkg/server/mq/provider_nats_reliable_test.go
git commit -m "feat(mq): implement reliable JetStream consumption"
```

### Task 7: NATS 必需 durable 前沿和安全回收

**Files:**
- Create: `pkg/server/mq/provider_nats_lifecycle.go`
- Create: `pkg/server/mq/provider_nats_lifecycle_test.go`
- Modify: `pkg/server/mq/provider_nats.go`

- [ ] **Step 1: 先写真 NATS 生命周期测试**

```go
func TestNATSLifecyclePrecreatesOfflineRequiredDurables(t *testing.T) {
	h := newNATSLifecycleHarness(t, "online", "offline")
	require.ElementsMatch(t, []string{"online", "offline"}, h.durableNames())
}

func TestNATSLifecycleAckFloorProtectsPendingAndUndeliveredMessages(t *testing.T) {
	h := newNATSLifecycleHarness(t, "online", "offline")
	h.publishOld("m1")
	h.receiveAndAck("online")
	require.Zero(t, h.reclaim().Reclaimed)
	require.Equal(t, []string{"m1"}, h.streamBodies())
}

func TestNATSLifecycleAllDurablesAndMinAgePermitBoundedPurge(t *testing.T) {
	h := newNATSLifecycleHarness(t, "users", "positions")
	h.setBatchSize(2)
	h.publishOld("m1", "m2", "m3")
	h.receiveAndAckAll("users")
	h.receiveAndAckAll("positions")
	require.LessOrEqual(t, h.reclaim().Reclaimed, int64(2))
}

func TestNATSLifecycleStartFromNewPersistsEnrollmentSequence(t *testing.T) {
	h := newNATSLifecycleHarness(t)
	h.publishOld("history")
	h.addDurable("new-reader", StartFromNew)
	require.Equal(t, h.lastSequence(), h.enrollmentSequence("new-reader"))
}

func TestNATSLifecycleRejectsUnsafeExistingStreamLimits(t *testing.T) {
	h := newNATSLifecycleHarness(t)
	h.createUnsafeMaxAgeStream(time.Minute)
	require.ErrorIs(t, h.ensure(), ErrLifecycleUnsafeBrokerPolicy)
}

func TestNATSLifecycleMissingDurableOrRegressedAckFloorFailsClosed(t *testing.T) {
	h := newNATSLifecycleHarness(t, "positions")
	h.deleteDurable("positions")
	_, err := h.inspect()
	require.ErrorIs(t, err, ErrLifecycleStateUncertain)
}

func TestNATSLifecycleConcurrentPublishAckAndPurgeDoesNotLoseMessages(t *testing.T) {
	h := newNATSLifecycleHarness(t, "positions")
	h.runConcurrentPublishConsumeReclaim(500)
	require.Equal(t, 500, h.uniqueConsumedCount())
	require.Empty(t, h.missingPublishedSequences())
}
```

- [ ] **Step 2: 运行 RED**

Run: `CORE_TEST_NATS_URL=nats://127.0.0.1:4222 go test ./pkg/server/mq -run '^TestNATSLifecycle' -count=1`

Expected: FAIL，NATS Provider 未实现 lifecycle capability。

- [ ] **Step 3: 实现 JetStream 管理面**

```go
func (n *NATSJetStreamProvider) EnsureLifecycle(ctx context.Context, policy LifecyclePolicy) error {
	return n.lifecycle.ensure(ctx, n.js, policy)
}
func (n *NATSJetStreamProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	return n.lifecycle.inspect(ctx, n.js, policy)
}
func (n *NATSJetStreamProvider) ReclaimLifecycle(ctx context.Context, policy LifecyclePolicy, snapshot LifecycleSnapshot) (ReclaimResult, error) {
	return n.lifecycle.reclaim(ctx, n.js, policy, snapshot)
}
```

stream 保持 `LimitsPolicy` 且不设会无条件删除未完成消息的 limits。安全前沿使用所有 required durable 的 `AckFloor.Stream` 和 enrollment sequence。单轮 purge sequence 不超过 safe frontier、MinAge frontier 和 first sequence + batch 三者的最小值。

- [ ] **Step 4: 运行 GREEN、重复性和 race**

Run: `gofmt -w pkg/server/mq/provider_nats_lifecycle.go pkg/server/mq/provider_nats_lifecycle_test.go pkg/server/mq/provider_nats.go && CORE_TEST_NATS_URL=nats://127.0.0.1:4222 go test -race ./pkg/server/mq -run '^TestNATSLifecycle' -count=1`

Run: `CORE_TEST_NATS_URL=nats://127.0.0.1:4222 go test ./pkg/server/mq -run '^TestNATSLifecycle' -count=10`

Expected: 两者 PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq/provider_nats.go pkg/server/mq/provider_nats_lifecycle.go pkg/server/mq/provider_nats_lifecycle_test.go
git commit -m "feat(mq): reclaim JetStream after every required durable"
```

### Task 8: 共享 conformance 与可观测诚实性

**Files:**
- Create: `pkg/server/mq/lifecycle_conformance.go`
- Create: `pkg/server/mq/lifecycle_conformance_test.go`
- Create: `pkg/server/mq/lifecycle_metrics.go`
- Modify: `pkg/server/mq/provider_redis.go`
- Modify: `pkg/server/mq/provider_nats.go`
- Modify: `pkg/server/observability/provider.go`
- Modify: `pkg/server/observability/collector_test.go`

- [ ] **Step 1: 先写共享契约和未采集指标测试**

```go
func TestLifecycleConformanceSingleAndMultipleGroups(t *testing.T) {
	VerifyMessageLifecycle(t, newFakeLifecycleFactory(t))
}

func TestLifecycleConformanceSlowOfflineAndFailedConsumers(t *testing.T) {
	factory := newFakeLifecycleFactory(t)
	result := runSlowOfflineFailureScenario(t, factory)
	require.Zero(t, result.ReclaimedWhileIncomplete)
	require.Equal(t, result.Published, result.ConsumedAfterRecovery)
}

func TestLifecycleConformanceRetentionConvergesAndRecoveryContinues(t *testing.T) {
	factory := newFakeLifecycleFactory(t)
	result := runSteadyStateAndRestartScenario(t, factory, 1_000)
	require.LessOrEqual(t, result.FinalRetained, result.PolicyBound)
	require.Empty(t, result.MissingIDs)
}

func TestLifecycleMetricsDoNotExposeMessageIdentityOrPayload(t *testing.T) {
	snapshot := lifecycleSnapshotWithSensitiveFixture()
	labels := exportLifecycleMetricLabels(snapshot)
	require.NotContains(t, strings.Join(labels, " "), "message-123")
	require.NotContains(t, strings.Join(labels, " "), "secret-payload")
}

func TestLifecycleUnknownMetricsRemainNotCollectedInsteadOfZero(t *testing.T) {
	snapshot := LifecycleSnapshot{State: StateNotCollected}
	metrics := lifecycleRuntimeSnapshot(snapshot)
	require.Equal(t, "not_collected", metrics.State)
	require.NotContains(t, metrics.Gauges, "retained_bytes")
}
```

- [ ] **Step 2: 运行 RED**

Run: `go test ./pkg/server/mq ./pkg/server/observability -run 'TestLifecycle(Conformance|Metrics|Unknown)' -count=1`

Expected: FAIL，缺少共享 suite 和指标映射。

- [ ] **Step 3: 实现共享验收函数与低基数指标**

```go
type LifecycleConformanceFactory interface {
	Provider() LifecycleMQProvider
	RestartProvider() LifecycleMQProvider
	RestartBroker(context.Context) error
}

func VerifyMessageLifecycle(t *testing.T, factory LifecycleConformanceFactory) {
	t.Helper()
	t.Run("single-group", func(t *testing.T) { verifySingleGroup(t, factory) })
	t.Run("multiple-groups", func(t *testing.T) { verifyMultipleGroups(t, factory) })
	t.Run("offline-and-pending-protection", func(t *testing.T) { verifyIncompleteMessagesRetained(t, factory) })
	t.Run("retry-and-dead-letter", func(t *testing.T) { verifyRetryAndDeadLetter(t, factory) })
	t.Run("process-and-broker-restart", func(t *testing.T) { verifyRestartRecovery(t, factory) })
	t.Run("bounded-retention-convergence", func(t *testing.T) { verifyBoundedConvergence(t, factory) })
}
```

Provider 快照汇总为 `retained_messages`、`retained_bytes`、`backlog_messages`、`pending_messages`、`oldest_age_sec`、`reclaimed_total`、`reclaim_fail_total`、`redelivered_total`、`dead_letter_total`、`publish_rejected_total`。没有数值时不输出数值样本，Runtime 快照保留 `not_collected` 状态。

- [ ] **Step 4: 运行 GREEN 与 race**

Run: `gofmt -w pkg/server/mq/lifecycle_conformance.go pkg/server/mq/lifecycle_conformance_test.go pkg/server/mq/lifecycle_metrics.go pkg/server/mq/provider_redis.go pkg/server/mq/provider_nats.go pkg/server/observability/provider.go pkg/server/observability/collector_test.go && go test -race ./pkg/server/mq ./pkg/server/observability -count=1`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/server/mq pkg/server/observability/provider.go pkg/server/observability/collector_test.go
git commit -m "feat(mq): expose lifecycle conformance and metrics"
```

### Task 9: 长期设计文档、Core skill 与 Bitzoom 接入示例

**Files:**
- Create: `docs/codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md`
- Modify: `docs/ai/core-skill/SKILL.md`
- Modify: `docs/ai/core-skill/multiservice-and-observability.md`
- Modify: `docs/ai/core-skill/common-mistakes.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Modify: `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `scripts/check-ai-skill.sh`

- [ ] **Step 1: 先写文档契约门禁**

`scripts/check-ai-skill.sh` 和配置/公开 API 契约测试必须新增断言：长期指南存在，skill 链接它，Redis/NATS/自定义 Provider 能力矩阵完整，旧配置无策略时不回收。

- [ ] **Step 2: 运行 RED**

Run: `./scripts/ci.sh required/ai-skill && ./scripts/test.sh config-contract && ./scripts/test.sh api-compat`

Expected: FAIL，新指南和契约文本尚不存在。

- [ ] **Step 3: 使用 `superpowers:writing-skills` 更新权威 skill 和永久指南**

`MQ_MESSAGE_LIFECYCLE_GUIDE.md` 必须包含：状态机、Provider 责任边界、capability 表、新 Provider 接入步骤、安全前沿算法、组变更维护流程、容量公式、指标状态、迁移与真 Broker 验收。

Bitzoom 示例只展示：

```go
policy := contract.TradeFilledLifecyclePolicy()
// RequiredGroups 由 Bitzoom 当前可靠 SubscribeEvent 注册点生成的 contract manifest 提供。
if err := sc.RequireMessageLifecycle(ctx, policy); err != nil {
	return err
}
```

不在 Core 文档中写出或猜测具体服务名。

- [ ] **Step 4: 运行 GREEN**

Run: `./scripts/ci.sh required/ai-skill && ./scripts/test.sh config-contract && ./scripts/test.sh api-compat && ./scripts/check-logging.sh`

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add docs/ai/core-skill docs/codex README.md CHANGELOG.md scripts/check-ai-skill.sh
git commit -m "docs(mq): publish lifecycle extension standard"
```

### Task 10: 真实 Broker、race 与发布契约总验收

**Files:**
- Modify: `tests/integration/mq_provider_test.go`
- Modify: `scripts/test.sh`
- Modify: `scripts/test-external-integration.sh`

- [ ] **Step 1: 把共享 lifecycle suite 接入 Redis 和 NATS 真实工厂**

```go
func TestMQRedisLifecycle(t *testing.T) { mq.VerifyMessageLifecycle(t, newRedisLifecycleFactory(t)) }
func TestMQNATSLifecycle(t *testing.T) { mq.VerifyMessageLifecycle(t, newNATSLifecycleFactory(t)) }
```

Broker restart 必须使用显式测试 hook，不在普通库代码执行 Docker 命令。

- [ ] **Step 2: 运行全部单元/契约门禁**

Run: `./scripts/test.sh quick`

Run: `./scripts/test.sh release-contract`

Run: `go test -race ./pkg/server/mq ./pkg/server/event ./pkg/server/router ./pkg/server/observability -count=1`

Expected: 全部 PASS。

- [ ] **Step 3: 运行真实 Redis/NATS 集成验收**

Run: `./scripts/test.sh integration-external-docker`

Expected: Redis 与 NATS lifecycle 用例 PASS，包含多组、离线、pending、重试、DLQ、并发回收、进程/Broker 重启和保留收敛。若环境无 Docker/Broker，交付明确写 `NOT RUN`，不改写为 PASS。

- [ ] **Step 4: 运行工作区和安全检查**

Run: `git diff --check && ./scripts/check-logging.sh && git status --short`

Expected: `git diff --check` 和日志检查 PASS；status 只包含本计划文件。

- [ ] **Step 5: 提交集成门禁**

```bash
git add tests/integration/mq_provider_test.go scripts/test.sh scripts/test-external-integration.sh
git commit -m "test(mq): verify lifecycle against real brokers"
```

- [ ] **Step 6: 记录交付证据**

交付说明分开列出 RED、GREEN、race、真 Redis、真 NATS、Broker restart、未运行项、兼容边界和 Bitzoom 后续接入步骤。不推送、不合并、不发布版本。
