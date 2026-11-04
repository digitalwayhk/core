// 本文件用真实Badger核对原错误引用的容量、代际、事务失败与旧Unknown隔离，不重置预算。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type deferredCauseFixture struct {
	store   *ReliableWriteStore[cleanupTestRecord]
	gate    chan struct{}
	entered chan struct{}
	release sync.Once
}

func newDeferredCauseFixture(t *testing.T, limit int, target *cleanupTestTarget, observe func(WriteBehindFailure[cleanupTestRecord])) *deferredCauseFixture {
	t.Helper()
	f := &deferredCauseFixture{gate: make(chan struct{}), entered: make(chan struct{}, 2)}
	target.handle = func(_ context.Context, failure WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		if failure.Record.Snapshot.Item.Revision == 1 {
			f.entered <- struct{}{}
			<-f.gate
		}
		if observe != nil {
			observe(failure)
		}
		return cleanupTestDecision(failure.Record, WriteBehindPause), nil
	}
	base := t.TempDir()
	cfg := DefaultProductionConfig(base)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.SyncBatchSize = 32
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "deferred-cause-boundaries", MaxAttempts: 3, MaxElapsed: time.Minute, MinRetryInterval: time.Millisecond, MaxRetryInterval: 20 * time.Millisecond, HandlerMaxAttempts: 3, HandlerTimeout: 20 * time.Second, HandlerWorkers: 2, ScanLimit: limit}
	store, _, err := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "deferred-cause-boundaries"}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg})
	require.NoError(t, err)
	f.store = store
	t.Cleanup(func() {
		f.release.Do(func() { close(f.gate) })
		_ = store.Close(context.Background())
		require.NoError(t, CloseSharedManager(store.config.BasePath))
	})
	require.NoError(t, store.UseWriteBehindWithFailureHandler(target, target))
	return f
}

func deferredProof(items []*SyncQueueItem[cleanupTestRecord]) error {
	proof := &deferredCauseProof{versions: map[string]time.Time{}, fingerprints: map[string]string{}}
	for _, item := range items {
		b, _ := json.Marshal(item)
		proof.versions[item.Key] = item.UpdatedAt
		proof.fingerprints[item.Key] = dispositionHash(string(b))
	}
	return proof
}

func (f *deferredCauseFixture) save(t *testing.T, key string, revision int) {
	t.Helper()
	require.NoError(t, f.store.Save(context.Background(), newCleanupTestRecord(t, key, revision, false, &cleanupTestProbe{})))
}
func (f *deferredCauseFixture) round() { _, _ = f.store.ForceSyncBatch(context.Background(), 32) }
func (f *deferredCauseFixture) records(t *testing.T) map[string]WriteBehindFailureRecord[cleanupTestRecord] {
	t.Helper()
	records, _, err := f.store.ListWriteBehindFailures("", 1000)
	require.NoError(t, err)
	result := map[string]WriteBehindFailureRecord[cleanupTestRecord]{}
	for _, r := range records {
		if r.Snapshot != nil && r.Snapshot.Item != nil {
			result[r.Snapshot.Item.Key] = r
		}
	}
	return result
}
func (f *deferredCauseFixture) blockWorkersAndQueue(t *testing.T) {
	t.Helper()
	f.save(t, "primer-a", 1)
	f.save(t, "primer-b", 1)
	f.round()
	for range 2 {
		select {
		case <-f.entered:
		case <-time.After(time.Second):
			t.Fatal("受控worker未开始")
		}
	}
	f.save(t, "queued-a", 2)
	f.save(t, "queued-b", 2)
	require.Eventually(t, func() bool { f.round(); return len(f.store.db.disposition.jobs) == 2 }, time.Second, time.Millisecond)
}
func (f *deferredCauseFixture) finish(t *testing.T, expected int) {
	t.Helper()
	f.release.Do(func() { close(f.gate) })
	require.Eventually(t, func() bool {
		f.round()
		m, e := f.store.WriteBehindDispositionMetrics()
		return e == nil && m.Paused == expected
	}, 5*time.Second, time.Millisecond)
}

// TestDeferredCauseOverflowFailsClosed 验证未准入引用最多ScanLimit，超限只失去证明而不丢原件或阻塞健康target。
func TestDeferredCauseOverflowFailsClosed(t *testing.T) {
	var mu sync.Mutex
	typed, text := 0, 0
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if len(items) == 1 && items[0].Item.Key == "healthy" {
			return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
		}
		return nil, deferredProof(items)
	}}
	f := newDeferredCauseFixture(t, 4, target, func(failure WriteBehindFailure[cleanupTestRecord]) {
		mu.Lock()
		defer mu.Unlock()
		var proof *deferredCauseProof
		if errors.As(failure.Cause, &proof) {
			typed++
		} else {
			text++
		}
	})
	f.blockWorkersAndQueue(t)
	for i := range 6 {
		f.save(t, fmt.Sprintf("overflow-%d", i), 2)
	}
	require.Eventually(t, func() bool {
		f.round()
		rs := f.records(t)
		for _, r := range rs {
			if r.Attempts != 1 {
				return false
			}
		}
		return len(rs) == 10
	}, 2*time.Second, time.Millisecond)
	m, err := f.store.WriteBehindDispositionMetrics()
	require.NoError(t, err)
	require.Equal(t, 4, m.CauseCapacity)
	require.Equal(t, 4, m.CauseRetained)
	require.Equal(t, uint64(2), m.CauseOverflow)
	f.save(t, "healthy", 2)
	require.Eventually(t, func() bool { f.round(); return f.store.Metrics().Sync.SyncedItems == 1 }, time.Second, time.Millisecond)
	f.finish(t, 10)
	mu.Lock()
	require.Equal(t, 8, typed)
	require.Equal(t, 2, text)
	mu.Unlock()
	require.Equal(t, 10, f.store.Metrics().Pending)
	m, err = f.store.WriteBehindDispositionMetrics()
	require.NoError(t, err)
	require.Zero(t, m.CauseRetained)
	require.Equal(t, uint64(2), m.CauseOverflow)
	for _, r := range f.records(t) {
		if r.State == WriteBehindCompleted {
			continue
		}
		require.Equal(t, 1, r.Attempts)
		require.Equal(t, 1, r.HandlerAttempts)
	}
}

// TestDeferredCauseOldUnknownCannotInheritNewBatch 验证旧Unknown与新失败同轮选出也不能获得另一批原错误。
func TestDeferredCauseOldUnknownCannotInheritNewBatch(t *testing.T) {
	observed := make(chan WriteBehindFailure[cleanupTestRecord], 6)
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if len(items) == 1 && items[0].Item.Key == "old-unknown" {
			return nil, nil
		}
		return nil, deferredProof(items)
	}}
	f := newDeferredCauseFixture(t, 64, target, func(failure WriteBehindFailure[cleanupTestRecord]) { observed <- failure })
	f.blockWorkersAndQueue(t)
	f.save(t, "old-unknown", 2)
	f.round()
	require.Equal(t, WriteBehindUnknown, f.records(t)["old-unknown"].State)
	f.save(t, "new-failure", 2)
	f.round()
	require.Equal(t, WriteBehindUnknown, f.records(t)["new-failure"].State)
	f.finish(t, 6)
	for range 6 {
		failure := <-observed
		var proof *deferredCauseProof
		typed := errors.As(failure.Cause, &proof)
		if failure.Record.Snapshot.Item.Key == "old-unknown" {
			require.False(t, typed)
		} else {
			require.True(t, typed)
			require.Equal(t, failure.Record.Fingerprint, proof.fingerprints[failure.Record.Snapshot.Key])
		}
	}
}

// TestDeferredCauseOldAttemptCannotProveLaterUnknown 验证显式Retry后无新证明的第二次结果不继承第一次回滚错误。
func TestDeferredCauseOldAttemptCannotProveLaterUnknown(t *testing.T) {
	observed := make(chan WriteBehindFailure[cleanupTestRecord], 5)
	victimCalls := 0
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		if len(items) == 1 && items[0].Item.Key == "victim" {
			victimCalls++
			if victimCalls == 2 {
				return nil, nil
			}
		}
		return nil, deferredProof(items)
	}}
	f := newDeferredCauseFixture(t, 64, target, func(failure WriteBehindFailure[cleanupTestRecord]) { observed <- failure })
	f.blockWorkersAndQueue(t)
	f.save(t, "victim", 2)
	f.round()
	old := f.records(t)["victim"]
	require.Equal(t, 1, old.Attempts)
	retained, _, _ := f.store.db.disposition.causes.metrics()
	require.Equal(t, 1, retained)
	decision := cleanupTestDecision(old, WriteBehindRetry)
	decision.Outcome = WriteBehindNotCommitted
	decision.RetryAfter = time.Millisecond
	require.NoError(t, f.store.ApplyWriteBehindDecision(context.Background(), decision))
	require.Eventually(t, func() bool { f.round(); return f.records(t)["victim"].Attempts == 2 }, time.Second, time.Millisecond)
	retained, _, _ = f.store.db.disposition.causes.metrics()
	require.Zero(t, retained)
	f.finish(t, 5)
	for range 5 {
		failure := <-observed
		if failure.Record.Snapshot.Item.Key != "victim" {
			continue
		}
		var proof *deferredCauseProof
		require.False(t, errors.As(failure.Cause, &proof))
		require.Equal(t, 2, failure.Record.Attempts)
	}
	require.Equal(t, 2, victimCalls)
	require.Equal(t, 5, f.store.Metrics().Pending)
}

// TestDeferredCauseUnknownCommitFailureHasNoProof 验证本地Unknown事务失败时不发布内存证明或提前扣pending。
func TestDeferredCauseUnknownCommitFailureHasNoProof(t *testing.T) {
	observed := make(chan WriteBehindFailure[cleanupTestRecord], 1)
	var targetReturned atomic.Bool
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		targetReturned.Store(true)
		return nil, deferredProof(items)
	}}
	f := newDeferredCauseFixture(t, 64, target, func(failure WriteBehindFailure[cleanupTestRecord]) { observed <- failure })
	f.save(t, "original", 2)
	original := f.store.db.localSubmit
	injected := errors.New("测试Unknown落盘失败")
	f.store.db.localSubmit = func(fn func() error) error {
		if targetReturned.Load() {
			return injected
		}
		return original(fn)
	}
	_, err := f.store.ForceSyncBatch(context.Background(), 32)
	require.ErrorIs(t, err, injected)
	f.store.db.localSubmit = original
	require.Equal(t, WriteBehindInFlight, f.records(t)["original"].State)
	retained, _, _ := f.store.db.disposition.causes.metrics()
	require.Zero(t, retained)
	f.finish(t, 1)
	failure := <-observed
	var proof *deferredCauseProof
	require.False(t, errors.As(failure.Cause, &proof))
	require.Equal(t, 1, failure.Record.Attempts)
	require.Equal(t, 1, f.store.Metrics().Pending)
}

// TestDeferredCauseIdentityAndLifetime 验证每个绑定维度、旧失效通知和关闭后的引用上限与释放。
func TestDeferredCauseIdentityAndLifetime(t *testing.T) {
	base := writeBehindCauseIdentity{id: "record", version: time.Now(), fingerprint: "fingerprint", attempt: 1, batch: "batch"}
	original := errors.New("测试原错误引用")
	changes := []func(*writeBehindCauseIdentity){func(i *writeBehindCauseIdentity) { i.id = "other" }, func(i *writeBehindCauseIdentity) { i.version = i.version.Add(time.Nanosecond) }, func(i *writeBehindCauseIdentity) { i.fingerprint = "new" }, func(i *writeBehindCauseIdentity) { i.attempt++ }, func(i *writeBehindCauseIdentity) { i.batch = "new" }}
	for n, change := range changes {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c := writeBehindCauseRetention{limit: 1}
			c.remember(base, original)
			different := base
			change(&different)
			require.Nil(t, c.take(different))
			c.close()
			retained, _, _ := c.metrics()
			require.Zero(t, retained)
		})
	}
	c := writeBehindCauseRetention{limit: 1}
	c.remember(base, original)
	newer := base
	newer.attempt++
	newer.batch = "later"
	c.remember(newer, original)
	c.forget(base)
	require.Nil(t, c.take(base), "旧通知不能误领新证明，也不能删除它")
	require.ErrorIs(t, c.take(newer), original)
	c.remember(base, original)
	c.close()
	c.remember(base, original)
	require.Nil(t, c.take(base))
}

// TestDeferredCauseCloseInvalidatesRetainedReferences 验证真实store关闭会清未准入引用，但不把尚未退出handler算成成功。
func TestDeferredCauseCloseInvalidatesRetainedReferences(t *testing.T) {
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return nil, deferredProof(items)
	}}
	f := newDeferredCauseFixture(t, 64, target, nil)
	f.blockWorkersAndQueue(t)
	f.save(t, "waiting", 2)
	f.round()
	retained, _, _ := f.store.db.disposition.causes.metrics()
	require.Equal(t, 1, retained)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := f.store.Close(ctx)
	require.ErrorIs(t, err, ErrWriteBehindDispositionCloseTimeout)
	retained, _, _ = f.store.db.disposition.causes.metrics()
	require.Zero(t, retained)
	require.True(t, f.store.db.disposition.causes.closed)
	f.release.Do(func() { close(f.gate) })
	f.store.db.wg.Wait()
	require.Equal(t, 5, f.store.Metrics().Pending)
}
