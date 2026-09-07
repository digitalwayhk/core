// 本文件用可控通知状态和真实 Manager 验证断线旁路及漏通知补偿的并发边界。
package routecache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type managedInvalidationTestBridge struct {
	*fakeInvalidationBridge
	epoch               atomic.Uint64
	down                atomic.Bool
	subscriptionContext context.Context
}

func (b *managedInvalidationTestBridge) NotificationState() (uint64, bool) {
	return b.epoch.Load(), !b.down.Load()
}
func (b *managedInvalidationTestBridge) SubscribeExternal(ctx context.Context, subject string) (func(), error) {
	b.subscriptionContext = ctx
	return b.fakeInvalidationBridge.SubscribeExternal(ctx, subject)
}

func newManagedCacheForTest(t *testing.T) (*Manager, *managedInvalidationTestBridge) {
	t.Helper()
	b := &managedInvalidationTestBridge{fakeInvalidationBridge: newFakeInvalidationBridge(newFakeInvalidationBus())}
	b.epoch.Store(1)
	m, err := NewManager("service-a", sharedCacheConfig(), WithRedisClient(newFakeRedisClient()), WithInvalidationBridge(b))
	require.NoError(t, err)
	t.Cleanup(m.Close)
	require.NoError(t, m.EnableRoute("/api/items", time.Minute))
	return m, b
}

func TestNotificationSubscriptionOutlivesInitialization(t *testing.T) {
	_, b := newManagedCacheForTest(t)
	require.NoError(t, b.subscriptionContext.Err(), "构造函数返回不能取消长期订阅")
}

func TestNotificationGapBypassesExistingCache(t *testing.T) {
	m, b := newManagedCacheForTest(t)
	require.NoError(t, m.Set("/api/items", "key", 1, time.Minute))
	b.down.Store(true)
	b.epoch.Add(1)
	_, ok, err := m.Get("/api/items", "key")
	require.NoError(t, err)
	require.False(t, ok, "通知断线期间不得命中旧 L1")
}

func TestNotificationMissingKeyEventReconciles(t *testing.T) {
	m, _ := newManagedCacheForTest(t)
	require.NoError(t, m.Set("/api/items", "key", 1, time.Minute))
	key, _, err := m.cacheKey("/api/items", "key")
	require.NoError(t, err)
	// 模拟发布进程写完权威缓存后崩溃：没有任何通知，也没有订阅断线。
	require.NoError(t, m.redis.Set(context.Background(), key, json.RawMessage(`2`), time.Minute))
	require.Eventually(t, func() bool {
		value, ok, err := m.Get("/api/items", "key")
		return err == nil && ok && string(value.(json.RawMessage)) == "2"
	}, 2500*time.Millisecond, 10*time.Millisecond, "周期补偿未清除漏通知的 key 缓存")
}

func TestNotificationFreshnessExpiresOnHotPath(t *testing.T) {
	m, _ := newManagedCacheForTest(t)
	require.NoError(t, m.Set("/api/items", "key", 1, time.Minute))
	m.notificationSynced.Store(time.Now().Add(-notificationFreshness - time.Second).UnixNano())
	_, hit, err := m.Get("/api/items", "key")
	require.NoError(t, err)
	require.False(t, hit)
	require.Equal(t, StateDegraded, m.State())
}

type recoveryBoundaryRedis struct {
	RedisClient
	armed            atomic.Bool
	entered, release chan struct{}
}

func (r *recoveryBoundaryRedis) PingCtx(ctx context.Context) bool {
	if r.armed.CompareAndSwap(true, false) {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return false
		}
	}
	return r.RedisClient.PingCtx(ctx)
}

func TestNotificationRecoveryCannotOverwriteNewGap(t *testing.T) {
	r := &recoveryBoundaryRedis{RedisClient: newFakeRedisClient(), entered: make(chan struct{}), release: make(chan struct{})}
	b := &managedInvalidationTestBridge{fakeInvalidationBridge: newFakeInvalidationBridge(newFakeInvalidationBus())}
	b.epoch.Store(1)
	m, err := NewManager("service-a", sharedCacheConfig(), WithRedisClient(r), WithInvalidationBridge(b))
	require.NoError(t, err)
	defer m.Close()
	r.armed.Store(true)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _, err := m.Recover(ctx); done <- err }()
	select {
	case <-r.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	b.epoch.Add(1)
	b.down.Store(true)
	close(r.release)
	require.Error(t, <-done)
	require.NotEqual(t, StateEnabled, m.State())
}

func TestNotificationRealRedisReplicasRecoverMissingEventAndL2(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 缺少真实 Redis 地址")
	}
	cfg := sharedCacheConfig()
	cfg.Redis.Addr = addr
	cfg.Redis.Prefix = fmt.Sprintf("notify-cache-test:%d", time.Now().UnixNano())
	open := func() (*Manager, *managedInvalidationTestBridge) {
		local := cfg
		local.L2 = testBadgerL2Config(t.TempDir())
		b := &managedInvalidationTestBridge{fakeInvalidationBridge: newFakeInvalidationBridge(newFakeInvalidationBus())}
		b.epoch.Store(1)
		m, err := NewManager("replicas", local, WithInvalidationBridge(b))
		require.NoError(t, err)
		t.Cleanup(m.Close)
		require.NoError(t, m.EnableRoute("/api/items", time.Minute))
		return m, b
	}
	a, _ := open()
	b, health := open()
	require.NoError(t, a.Set("/api/items", "key", 1, time.Minute))
	_, hit, err := b.Get("/api/items", "key")
	require.NoError(t, err)
	require.True(t, hit)
	key, _, err := b.cacheKey("/api/items", "key")
	require.NoError(t, err)
	_, hit, err = b.l2.Get(key)
	require.NoError(t, err)
	require.True(t, hit, "已进入真实 L2")
	health.down.Store(true)
	_, hit, err = b.Get("/api/items", "key")
	require.NoError(t, err)
	require.False(t, hit, "断线不得回退旧 L2")
	require.NoError(t, a.Set("/api/items", "key", 2, time.Minute))
	health.epoch.Add(1)
	health.down.Store(false)
	require.Eventually(t, func() bool {
		v, hit, err := b.Get("/api/items", "key")
		return err == nil && hit && string(v.(json.RawMessage)) == "2"
	}, 4*time.Second, 20*time.Millisecond)
	// 两 Manager 的通知 bus 故意隔离；下一次无断线漏通知仍须收敛。
	require.NoError(t, a.Set("/api/items", "key", 3, time.Minute))
	require.Eventually(t, func() bool {
		v, hit, err := b.Get("/api/items", "key")
		return err == nil && hit && string(v.(json.RawMessage)) == "3"
	}, 4*time.Second, 20*time.Millisecond)
}
