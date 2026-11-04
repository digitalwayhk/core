// 本文件以真实可靠store和固定回调背压诊断当前进程的原错误链保留边界，不连接消费方。
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

type deferredCauseProof struct {
	versions     map[string]time.Time
	fingerprints map[string]string
}

// Error 返回合成错误说明，不承载真实业务回滚证明。
func (*deferredCauseProof) Error() string {
	return "合成target已结束且未提交；仅测试类型传递"
}

type deferredCauseObservation struct {
	typed bool
	bound bool
}
type deferredCauseSummary struct{ typed, text int }

func runDeferredCauseBackpressure(t *testing.T, auto bool) deferredCauseSummary {
	t.Helper()
	ctx := context.Background()
	gate := make(chan struct{})
	entered := make(chan struct{}, 2)
	var release sync.Once
	var targetCalls atomic.Int32
	observed := make(chan deferredCauseObservation, 32)
	target := &cleanupTestTarget{}
	target.sync = func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		targetCalls.Add(1)
		result := &WriteBehindResult{}
		proof := &deferredCauseProof{versions: map[string]time.Time{}, fingerprints: map[string]string{}}
		for _, item := range items {
			if item.Item.Group == "healthy" {
				result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key)
				continue
			}
			b, e := json.Marshal(item)
			if e != nil {
				return nil, e
			}
			proof.versions[item.Key] = item.UpdatedAt
			proof.fingerprints[item.Key] = dispositionHash(string(b))
		}
		if len(proof.versions) > 0 {
			return result, proof
		}
		return result, nil
	}
	target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		if f.Record.Snapshot.Item.Revision == 1 {
			entered <- struct{}{}
			<-gate
		}
		var proof *deferredCauseProof
		typed := errors.As(f.Cause, &proof)
		bound := false
		if typed {
			bound = proof.versions[f.Record.Snapshot.Key].Equal(f.Record.Snapshot.UpdatedAt) && proof.fingerprints[f.Record.Snapshot.Key] == f.Record.Fingerprint
		}
		observed <- deferredCauseObservation{typed, bound}
		return cleanupTestDecision(f.Record, WriteBehindPause), nil
	}
	base := t.TempDir()
	cfg := DefaultProductionConfig(base)
	cfg.EnableLogger = false
	cfg.AutoSync = auto
	cfg.SyncBatchSize = 32
	cfg.SyncInterval = 10 * time.Millisecond
	cfg.SyncMinInterval = time.Millisecond
	cfg.SyncMaxInterval = 20 * time.Millisecond
	cfg.SyncFlushThreshold = 1
	cfg.SyncMaxCollectDelay = time.Millisecond
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "deferred-cause-diagnostic", MaxAttempts: 3, MaxElapsed: time.Minute, MinRetryInterval: 10 * time.Millisecond, MaxRetryInterval: 20 * time.Millisecond, HandlerMaxAttempts: 3, HandlerTimeout: 20 * time.Second, HandlerWorkers: 2, ScanLimit: 64}
	store, _, err := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "deferred-cause-diagnostic"}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg})
	require.NoError(t, err)
	t.Cleanup(func() {
		release.Do(func() { close(gate) })
		var pending *PendingSyncError
		require.ErrorAs(t, store.Close(ctx), &pending)
		require.Equal(t, 32, pending.Count)
		require.NoError(t, CloseSharedManager(store.config.BasePath))
	})
	require.NoError(t, store.UseWriteBehindWithFailureHandler(target, target))
	save := func(start, end, revision int) {
		var records []*cleanupTestRecord
		for i := start; i < end; i++ {
			r := newCleanupTestRecord(t, fmt.Sprintf("group-%02d", i), revision, false, &cleanupTestProbe{})
			records = append(records, r)
		}
		result, e := store.SaveBatch(ctx, records)
		require.NoError(t, e)
		require.Equal(t, len(records), result.Committed)
	}
	round := func() {
		if !auto {
			_, e := store.ForceSyncBatch(ctx, 32)
			if e != nil {
				var proof *deferredCauseProof
				require.ErrorAs(t, e, &proof)
			}
		}
	}
	save(0, 2, 1)
	round()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("两个真实handler未进入受控屏障")
		}
	}
	save(2, 32, 2)
	round()
	require.Eventually(t, func() bool { return len(store.db.disposition.jobs) == 2 }, 5*time.Second, time.Millisecond)
	records, _, e := store.ListWriteBehindFailures("", 64)
	require.NoError(t, e)
	require.Len(t, records, 32)
	states := map[WriteBehindFailureState]int{}
	for _, r := range records {
		states[r.State]++
		require.Equal(t, 1, r.Attempts)
	}
	require.Equal(t, 4, states[WriteBehindHandling])
	require.Equal(t, 28, states[WriteBehindUnknown])
	require.Len(t, store.db.disposition.jobs, 2)
	// 回调仍阻塞时，独立健康组继续真实target确认，不增加worker或队列预算。
	healthy := newCleanupTestRecord(t, "healthy", 2, false, &cleanupTestProbe{})
	require.NoError(t, store.Save(ctx, healthy))
	round()
	require.Eventually(t, func() bool { return store.Metrics().Sync.SyncedItems == 1 }, 5*time.Second, time.Millisecond)
	require.Equal(t, 32, store.Metrics().Pending)
	release.Do(func() { close(gate) })
	require.Eventually(t, func() bool { round(); m, e := store.WriteBehindDispositionMetrics(); return e == nil && m.Paused == 32 }, 10*time.Second, 2*time.Millisecond)
	result := deferredCauseSummary{}
	for range 32 {
		select {
		case got := <-observed:
			if got.typed {
				result.typed++
				require.True(t, got.bound, "带类型证明须仍绑定原版本和指纹")
			} else {
				result.text++
			}
		case <-time.After(time.Second):
			t.Fatal("观察量不足32")
		}
	}
	records, _, e = store.ListWriteBehindFailures("", 64)
	require.NoError(t, e)
	paused := 0
	for _, r := range records {
		if r.State == WriteBehindCompleted {
			continue
		}
		paused++
		require.Equal(t, WriteBehindPaused, r.State)
		require.Equal(t, 1, r.Attempts)
		require.Equal(t, 1, r.HandlerAttempts)
		require.False(t, r.LocalConfirmed)
	}
	require.Equal(t, 32, paused)
	require.Equal(t, 32, store.Metrics().Pending)
	require.Equal(t, int32(3), targetCalls.Load())
	t.Logf("auto=%t groups=32 typed=%d text_only=%d pending=32 target_calls=%d healthy_confirmed=1 workers=2 queue=2", auto, result.typed, result.text, targetCalls.Load())
	return result
}

// TestDeferredCauseRetentionRequirement 验证真实32组背压后，手动和自动同步均保留本次失败的原错误链。
func TestDeferredCauseRetentionRequirement(t *testing.T) {
	for _, auto := range []bool{false, true} {
		t.Run(fmt.Sprintf("auto=%t", auto), func(t *testing.T) {
			got := runDeferredCauseBackpressure(t, auto)
			require.Equal(t, 32, got.typed, "同进程队列背压后丢失原始typed cause")
		})
	}
}
