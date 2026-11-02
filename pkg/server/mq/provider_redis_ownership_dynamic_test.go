// 本文件用真实Redis固定动态key缺口；旧版通过JSON忽略新配置，保持RED可编译。
package mq

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func dynamicManifest(t *testing.T, m ownership.Manifest) ownership.Manifest {
	t.Helper()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var v map[string]any
	require.NoError(t, json.Unmarshal(b, &v))
	v["Keys"] = []string{}
	v["DynamicKeys"] = map[string]any{"MaxKeys": 16, "PageSize": 2, "RefreshInterval": 10000000, "UnregisteredWindow": 8}
	b, err = json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &m))
	return m.Normalize()
}

func TestRedisOwnershipUnregisteredKeyCannotPoisonKnownObservation(t *testing.T) {
	p, _, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	// 先固定原故障：观察A时，原组B以外的新key不能污染A的已知进度。
	require.NoError(t, p.Publish(ctx, "one", []byte("key-new:value"), &PublishOptions{OrderingKey: "key-new"}))
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	rows, err := reader.subjectMessages(ctx, "one", true)
	require.NoError(t, err)
	_, err = reader.mapMessages("one", "A", rows, false)
	require.NoError(t, err, "未注册消息应保留且隔离，不得令已注册key观测变为ErrUnknown")
	require.EqualValues(t, 1, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
}

func TestRedisOwnershipDynamicManifestCanStartWithoutFutureKeys(t *testing.T) {
	_, _, fixed := ownershipFixture(t)
	m := dynamicManifest(t, fixed)
	require.NoError(t, m.Validate(), "动态模式不能再强制预枚举未来key")
}

func dynamicFixture(t *testing.T) (*RedisStreamProvider, ownership.Store, ownership.Manifest) {
	t.Helper()
	p := newPoolBudgetProvider(t, 4, 8)
	fixed := ownership.Manifest{Scope: "dynamic", Service: "generic", Group: "generic", Selector: "opaque-v1", Subjects: []string{"one"}, Keys: []string{"A"}}.Normalize()
	m := dynamicManifest(t, fixed)
	require.NoError(t, p.ProvisionOwnership(context.Background(), m, 0))
	require.NoError(t, p.BindOwnership(context.Background(), m))
	store, err := p.OwnershipStore(context.Background())
	require.NoError(t, err)
	return p, store, m
}

func TestRedisOwnershipDynamicRegistrationIsBoundedIdempotentAndFailClosed(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	registry, ok := store.(ownership.KeyRegistry)
	require.True(t, ok)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- registry.RegisterKey(ctx, m, "A") }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	page, err := registry.ListKeys(ctx, m, 0, 2)
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, []string{"A"}, page.Keys)
	for i := 1; i < 16; i++ {
		require.NoError(t, registry.RegisterKey(ctx, m, string(rune('A'+i))))
	}
	require.NoError(t, registry.RegisterKey(ctx, m, "A"))
	require.ErrorIs(t, registry.RegisterKey(ctx, m, "over-budget"), ownership.ErrCapacity)
	wrong := m
	wrong.Selector = "other"
	require.ErrorIs(t, registry.RegisterKey(ctx, wrong, "X"), ownership.ErrUnknown)
	require.ErrorIs(t, registry.RegisterKey(ctx, m, ""), ownership.ErrUnknown)
	require.ErrorIs(t, func() error { _, err := registry.ListKeys(ctx, m, 0, 3); return err }(), ownership.ErrCapacity)
	c, err := store.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	require.ErrorIs(t, p.RecoverOwnership(ctx, m, 1), ownership.ErrBusy)
	require.ErrorIs(t, p.UnbindOwnership(ctx, m), ownership.ErrBusy)
	require.NoError(t, store.Release(ctx, c))
	require.NoError(t, p.client.Del(ctx, p.ownershipBase(m.Scope)+":sequence").Err())
	require.ErrorIs(t, registry.RegisterKey(ctx, m, "A"), ownership.ErrUnknown)
	require.NoError(t, p.RecoverOwnership(ctx, m, 100))
	next, err := store.Acquire(ctx, ownershipCandidate(m, "A", "b"), m.Timing.Lease)
	require.NoError(t, err)
	require.Greater(t, next.Epoch, int64(100))
	require.NoError(t, store.Release(ctx, next))
	require.NoError(t, p.client.Del(ctx, p.ownershipBase(m.Scope)+":directory").Err())
	_, err = registry.ListKeys(ctx, m, 0, 2)
	require.ErrorIs(t, err, ownership.ErrUnknown)
	require.ErrorIs(t, registry.RegisterKey(ctx, m, "A"), ownership.ErrUnknown)
}

func TestRedisOwnershipDynamicMessageBeforeRegistrationAndFullOriginalWindow(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	c, err := store.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, store.Ready(ctx, c))
	var seen []string
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, InstanceID: "a", SessionID: "boot", NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, func(_ context.Context, _ ownership.Credential, rows []*Message) error {
		for _, r := range rows {
			seen = append(seen, r.ID)
		}
		return nil
	}, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	for i := int64(0); i < m.Window; i++ {
		require.NoError(t, p.Publish(ctx, "one", []byte("B:early"), &PublishOptions{OrderingKey: "B"}))
	}
	early, err := p.client.XRange(ctx, p.streamKey("one"), "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, early, int(m.Window))
	require.NoError(t, p.Publish(ctx, "one", []byte("A:healthy"), &PublishOptions{OrderingKey: "A"}))
	for i := 0; i < 3; i++ {
		_, err = reader.step(ctx, c)
		require.NoError(t, err)
	}
	require.Len(t, seen, 1, "原窗口被未登记B占满后，A仍在明确的有限预留预算内推进")
	require.EqualValues(t, m.Window, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "B"))
	b, err := store.Acquire(ctx, ownershipCandidate(m, "B", "b"), m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, store.Ready(ctx, b))
	require.Eventually(t, func() bool { _, e := reader.step(ctx, b); return e == nil && len(seen) == 1+len(early) }, time.Second, time.Millisecond)
	for i, row := range early {
		require.Equal(t, row.ID, seen[i+1], "必须消费原ID，不生成消息副本")
	}
	require.Zero(t, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
}

func TestRedisOwnershipDynamicUnregisteredOrderingBarrierAndFiniteBackpressure(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	c, err := store.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, store.Ready(ctx, c))
	var handled atomic.Int64
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, func(context.Context, ownership.Credential, []*Message) error { handled.Add(1); return nil }, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	require.NoError(t, p.Publish(ctx, "one", []byte("B:first"), &PublishOptions{OrderingKey: "same"}))
	require.NoError(t, p.Publish(ctx, "one", []byte("A:later"), &PublishOptions{OrderingKey: "same"}))
	progress, err := reader.step(ctx, c)
	require.NoError(t, err)
	require.False(t, progress)
	require.Zero(t, handled.Load(), "不能越过未登记前序")
	for i := 0; i < 30; i++ {
		require.NoError(t, p.Publish(ctx, "one", []byte("B:more"), &PublishOptions{OrderingKey: "B"}))
	}
	for i := 0; i < 10; i++ {
		_, err = reader.step(ctx, c)
		require.NoError(t, err)
	}
	require.EqualValues(t, m.DynamicKeys.UnregisteredWindow+1, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count, "只有一个登记前序和U条未知原件，不能借用W容纳更多未知消息")
}

func TestRedisOwnershipDynamicRuntimeDiscoversAppendWithoutRestart(t *testing.T) {
	p, _, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	var received atomic.Int64
	options := func(id string) OwnedReliableOptions {
		return OwnedReliableOptions{Manifest: m, InstanceID: id, SessionID: id, MaxOwned: 2, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}
	}
	handler := func(context.Context, ownership.Credential, []*Message) error { received.Add(1); return nil }
	a, err := p.SubscribeOwnedBatch(ctx, options("a"), handler)
	require.NoError(t, err)
	defer a.Close(ctx)
	b, err := p.SubscribeOwnedBatch(ctx, options("b"), handler)
	require.NoError(t, err)
	defer b.Close(ctx)
	store, _ := p.OwnershipStore(ctx)
	for _, key := range []string{"A", "B", "C"} {
		require.NoError(t, p.RegisterOwnershipKey(ctx, m, key))
		require.NoError(t, p.Publish(ctx, "one", []byte(key+":data"), &PublishOptions{OrderingKey: key}))
		require.Eventually(t, func() bool { _, err := store.Resolve(ctx, m.Scope, key); return err == nil }, 3*time.Second, 5*time.Millisecond)
	}
	require.Eventually(t, func() bool { return received.Load() == 3 }, 3*time.Second, 5*time.Millisecond)
	require.EqualValues(t, 3, a.RuntimeMetricSnapshot(ctx).Gauges["ownership_active"]+b.RuntimeMetricSnapshot(ctx).Gauges["ownership_active"])
}

func TestRedisOwnershipFixedToDynamicMigrationPreservesPELAndFence(t *testing.T) {
	p, store, from := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, from))
	require.NoError(t, p.Publish(ctx, "one", []byte("A:retained"), &PublishOptions{OrderingKey: "A"}))
	rows, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: from.Group, Consumer: "old", Streams: []string{p.streamKey("one"), ">"}, Count: 1, Block: -1}).Result()
	require.NoError(t, err)
	c, err := store.Acquire(ctx, ownershipCandidate(from, "A", "old"), from.Timing.Lease)
	require.NoError(t, err)
	to := dynamicManifest(t, from)
	require.ErrorIs(t, p.MigrateOwnershipKeys(ctx, from, to, c.Epoch), ownership.ErrBusy)
	require.NoError(t, store.Release(ctx, c))
	seq := p.client.Get(ctx, p.ownershipBase(from.Scope)+":sequence").Val()
	require.NoError(t, p.MigrateOwnershipKeys(ctx, from, to, c.Epoch))
	require.NoError(t, p.MigrateOwnershipKeys(ctx, from, to, c.Epoch))
	require.Equal(t, seq, p.client.Get(ctx, p.ownershipBase(from.Scope)+":sequence").Val())
	require.EqualValues(t, 1, p.client.XPending(ctx, p.streamKey("one"), from.Group).Val().Count)
	pending, err := p.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: p.streamKey("one"), Group: from.Group, Start: "-", End: "+", Count: 1}).Result()
	require.NoError(t, err)
	require.Equal(t, rows[0].Messages[0].ID, pending[0].ID)
	require.Error(t, p.BindOwnership(ctx, from))
	require.NoError(t, p.RegisterOwnershipKey(ctx, to, "C"))
	next, err := store.Acquire(ctx, ownershipCandidate(to, "C", "new"), to.Timing.Lease)
	require.NoError(t, err)
	require.Greater(t, next.Epoch, c.Epoch)
	require.ErrorIs(t, store.Renew(ctx, c, from.Timing.Lease), ownership.ErrFenced)
	require.True(t, errors.Is(store.Release(ctx, c), ownership.ErrFenced))
}

func TestRedisOwnershipDynamicConcurrentReadsDoNotBorrowOtherBudget(t *testing.T) {
	p, _, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	for i := 0; i < 32; i++ {
		require.NoError(t, p.Publish(ctx, "one", []byte("A:registered"), &PublishOptions{OrderingKey: "A"}))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
			for j := 0; j < 3; j++ {
				_, e := reader.subjectMessages(ctx, "one", true)
				if e != nil {
					errs <- e
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.EqualValues(t, m.Window, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count, "登记消息不能借用U突破W，多读方必须在Lua里原子计费")
}

func TestRedisOwnershipDynamicRegisteredWithoutOwnerSlotIsNotReady(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "new"))
	_, err := store.Resolve(ctx, m.Scope, "new")
	require.ErrorIs(t, err, ownership.ErrNotReady)
	require.ErrorIs(t, store.Ready(ctx, ownershipCandidate(m, "new", "forged")), ownership.ErrUnknown)
}

func TestRedisOwnershipDynamicCapacityDoesNotRevokeUnrelatedIdleOwner(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := dynamicManifest(t, ownership.Manifest{Scope: "capacity-idle", Service: "generic", Group: "generic", Selector: "opaque-v1", Subjects: []string{"one"}, Timing: ownership.Timing{Lease: 600 * time.Millisecond, Renew: 100 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 100 * time.Millisecond, Stall: 400 * time.Millisecond, Observation: 150 * time.Millisecond}}.Normalize())
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	store, _ := p.OwnershipStore(ctx)
	runtime, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "a", SessionID: "boot", MaxOwned: 1, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, func(context.Context, ownership.Credential, []*Message) error { return nil })
	require.NoError(t, err)
	defer runtime.Close(ctx)
	var original ownership.Credential
	require.Eventually(t, func() bool { var e error; original, e = store.Resolve(ctx, m.Scope, "A"); return e == nil }, time.Second, time.Millisecond)
	for i := 0; i < 40; i++ {
		require.NoError(t, p.Publish(ctx, "one", []byte("B:unknown"), &PublishOptions{OrderingKey: "B"}))
	}
	// 实际Runtime在超过原unknown撤销时限后仍须同一凭据；不是把阈值调大。
	until := time.Now().Add(3 * m.Timing.Observation)
	for time.Now().Before(until) {
		current, e := store.Resolve(ctx, m.Scope, "A")
		require.NoError(t, e)
		require.True(t, current.Same(original))
		time.Sleep(10 * time.Millisecond)
	}
	require.Zero(t, runtime.RuntimeMetricSnapshot(ctx).Counters["ownership_revoked_total"])
	require.EqualValues(t, m.DynamicKeys.UnregisteredWindow, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
	require.NoError(t, p.client.Del(ctx, p.ownershipBase(m.Scope)+":sequence").Err())
	require.Eventually(t, func() bool { return runtime.RuntimeMetricSnapshot(ctx).Gauges["ownership_ready"] == 0 }, time.Second, time.Millisecond, "真正序号丢失仍失败关闭")
}

type fixedOwnershipOnly struct {
	MQProvider
	OwnedReliableMQProvider
}

func TestManagerDynamicOwnershipRequiresExplicitCapability(t *testing.T) {
	p, _, m := dynamicFixture(t)
	manager := NewManager()
	manager.current = fixedOwnershipOnly{MQProvider: p, OwnedReliableMQProvider: p}
	require.ErrorIs(t, manager.ProvisionOwnership(context.Background(), m, 1), ownership.ErrUnsupported, "不能让只声明固定范围的Provider静默接受动态合同")
	require.ErrorIs(t, manager.BindOwnership(context.Background(), m), ownership.ErrUnsupported)
	_, err := manager.SubscribeOwnedBatch(context.Background(), OwnedReliableOptions{Manifest: m}, nil)
	require.ErrorIs(t, err, ownership.ErrUnsupported)
}

func TestRedisOwnershipDynamicRestoreFailureNeverPublishesReadyOrACK(t *testing.T) {
	p, store, m := dynamicFixture(t)
	ctx := context.Background()
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	require.NoError(t, p.Publish(ctx, "one", []byte("A:original"), &PublishOptions{OrderingKey: "A"}))
	var handled atomic.Int64
	runtime, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "bad-restore", SessionID: "boot", MaxOwned: 1, NoDueTasks: true, Key: func(msg *Message) (string, error) { return strings.Split(string(msg.Data), ":")[0], nil }, Activate: func(context.Context, ownership.Credential) error { return nil }, Restore: func(context.Context, ownership.Credential, ownership.ReferenceSnapshot) error {
		return errors.New("restore failed")
	}}, func(context.Context, ownership.Credential, []*Message) error { handled.Add(1); return nil })
	require.NoError(t, err)
	defer runtime.Close(ctx)
	require.Eventually(t, func() bool {
		return runtime.RuntimeMetricSnapshot(ctx).Counters["ownership_restore_failures_total"] > 0
	}, time.Second, time.Millisecond)
	_, err = store.Resolve(ctx, m.Scope, "A")
	require.Error(t, err)
	require.Zero(t, handled.Load())
	groups, err := p.client.XInfoGroups(ctx, p.streamKey("one")).Result()
	require.NoError(t, err)
	require.Equal(t, "0-0", groups[0].LastDeliveredID, "恢复失败不能读完或ACK原消息")
}

func TestRedisOwnershipDynamicOversizedMessageRetainsOriginalWithoutReadOrACK(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := dynamicManifest(t, ownership.Manifest{Scope: "dynamic-bytes", Service: "generic", Group: "generic", Selector: "key-v1", Subjects: []string{"one"}, MaxMessageBytes: 64}.Normalize())
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	require.NoError(t, p.Publish(ctx, "one", []byte(strings.Repeat("x", 128)), &PublishOptions{OrderingKey: "A"}))
	reader := newRedisOwnedReader(p, OwnedReliableOptions{Manifest: m, Key: func(*Message) (string, error) { return "A", nil }}, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
	_, err := reader.subjectMessages(ctx, "one", true)
	require.ErrorIs(t, err, ownership.ErrCapacity)
	require.Zero(t, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
	require.EqualValues(t, 1, p.client.XLen(ctx, p.streamKey("one")).Val())
}
