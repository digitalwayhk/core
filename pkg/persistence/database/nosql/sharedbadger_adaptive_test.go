// 本文件验证自适应收集配置和调度，不依赖外部数据库。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestAdaptiveConfigInvalid 验证新增配置不能被忽略或静默补齐。
func TestAdaptiveConfigInvalid(t *testing.T) {
	for _, input := range []string{
		`{"sync_flush_threshold":32}`,
		`{"sync_max_collect_delay":20000000}`,
		`{"sync_backlog_drain_delay":1}`,
		`{"sync_flush_threshold":-1}`,
		`{"sync_flush_threshold":32,"sync_max_collect_delay":-1}`,
		`{"sync_flush_threshold":32,"sync_max_collect_delay":20000000,"sync_backlog_drain_delay":-1}`,
		`{"sync_batch_size":16,"sync_flush_threshold":32,"sync_max_collect_delay":20000000}`,
	} {
		t.Run(input, func(t *testing.T) {
			cfg := DefaultProductionConfig(t.TempDir())
			if err := json.Unmarshal([]byte(input), &cfg); err != nil {
				t.Fatal(err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatal("无效自适应配置被接受")
			}
		})
	}
}

// TestAdaptiveConfigRoundTrip 验证零值兼容及完整配置可反序列化和保留。
func TestAdaptiveConfigRoundTrip(t *testing.T) {
	cfg := DefaultSharedConfig(t.TempDir())
	require.NoError(t, cfg.Validate())
	require.Zero(t, cfg.SyncFlushThreshold)
	require.Equal(t, 100*time.Millisecond, cfg.SyncBatchDelay)
	require.NoError(t, json.Unmarshal([]byte(`{"sync_flush_threshold":32,"sync_max_collect_delay":20000000,"sync_backlog_drain_delay":3000000}`), &cfg))
	require.NoError(t, cfg.Validate())
	require.Equal(t, 32, cfg.SyncFlushThreshold)
	require.Equal(t, 20*time.Millisecond, cfg.SyncMaxCollectDelay)
	require.Equal(t, 3*time.Millisecond, cfg.SyncBacklogDrainDelay)
	encoded, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	var decoded BadgerDBConfig
	require.NoError(t, yaml.Unmarshal(encoded, &decoded))
	require.NoError(t, decoded.Validate())
	require.Equal(t, cfg, decoded)
}

type adaptiveTarget struct {
	mu         sync.Mutex
	failure    bool
	calls      chan time.Time
	batches    []int
	pause      chan struct{}
	noProgress bool
}

func (a *adaptiveTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
	a.mu.Lock()
	failed := a.failure
	pause := a.pause
	a.pause = nil
	noProgress := a.noProgress
	a.batches = append(a.batches, len(items))
	a.mu.Unlock()
	a.calls <- time.Now()
	if pause != nil {
		<-pause
	}
	if failed {
		return nil, fmt.Errorf("测试目标暂时不可用")
	}
	if noProgress {
		return &WriteBehindResult{}, nil
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return &WriteBehindResult{ConfirmedKeys: keys}, nil
}

// TestAdaptiveConcurrentUpdate 验证旧快照确认不会删除同步期间的新版本，手动同步仍串行。
func TestAdaptiveConcurrentUpdate(t *testing.T) {
	db, target := newAdaptiveDB(t, 20*time.Millisecond, 1)
	gate := make(chan struct{})
	target.mu.Lock()
	target.pause = gate
	target.mu.Unlock()
	require.NoError(t, db.Set(newFund("same", "HK", 1), 0))
	select {
	case <-target.calls:
	case <-time.After(time.Second):
		close(gate)
		t.Fatal("未开始同步")
	}
	require.NoError(t, db.Set(newFund("same", "HK", 2), 0))
	done := make(chan error, 1)
	go func() { _, err := db.ForceSyncBatch(context.Background(), 10); done <- err }()
	close(gate)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("手动同步被阻塞")
	}
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Zero(t, count)
	target.mu.Lock()
	defer target.mu.Unlock()
	require.GreaterOrEqual(t, len(target.batches), 2)
}

// TestAdaptiveZeroProgressBackoff 验证目标无错误但不确认时仍退避且保留 pending。
func TestAdaptiveZeroProgressBackoff(t *testing.T) {
	db, target := newAdaptiveDB(t, 20*time.Millisecond, 1)
	target.mu.Lock()
	target.noProgress = true
	target.mu.Unlock()
	require.NoError(t, db.Set(newFund("wait", "HK", 1), 0))
	var first time.Time
	select {
	case first = <-target.calls:
	case <-time.After(time.Second):
		t.Fatal("未执行首轮")
	}
	for i := 0; i < 100; i++ {
		db.triggerSync()
	}
	select {
	case second := <-target.calls:
		require.GreaterOrEqual(t, second.Sub(first), 100*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("没有有界重试")
	}
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
}

// TestAdaptiveRebindWithoutNewWrites 验证兼容解绑再绑定不依赖新业务写入来唤醒旧 worker。
func TestAdaptiveRebindWithoutNewWrites(t *testing.T) {
	db, target := newAdaptiveDB(t, 20*time.Millisecond, 1)
	target.mu.Lock()
	target.failure = true
	target.mu.Unlock()
	require.NoError(t, db.Set(newFund("rebind", "HK", 1), 0))
	select {
	case <-target.calls:
	case <-time.After(time.Second):
		t.Fatal("未开始失败尝试")
	}
	db.SetSyncDB(nil)
	// 等待首次退避结束，验证解绑期间不再调用旧目标。
	select {
	case <-target.calls:
		t.Fatal("解绑后仍调用旧目标")
	case <-time.After(200 * time.Millisecond):
	}
	replacement := &boundedFundTarget{confirm: 10}
	require.NoError(t, db.UseWriteBehind(replacement))
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, 1500*time.Millisecond, time.Millisecond)
}

func newAdaptiveDB(t *testing.T, delay time.Duration, threshold int) (*PrefixedBadgerDB[testFund], *adaptiveTarget) {
	t.Helper()
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = true
	cfg.SyncFlushThreshold = threshold
	cfg.SyncMaxCollectDelay = delay
	cfg.SyncMinInterval = 100 * time.Millisecond
	cfg.SyncMaxInterval = 200 * time.Millisecond
	cfg.SyncBatchDelay = time.Second
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	target := &adaptiveTarget{calls: make(chan time.Time, 1000)}
	require.NoError(t, db.UseWriteBehind(target))
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	return db, target
}

// TestAdaptiveThresholdAndIdle 验证真实 Badger 数量触发及排空后不会遗留空事务。
func TestAdaptiveThresholdAndIdle(t *testing.T) {
	db, target := newAdaptiveDB(t, time.Second, 4)
	for i := 0; i < 4; i++ {
		require.NoError(t, db.Set(newFund(fmt.Sprint(i), "HK", 1), 0))
	}
	select {
	case <-target.calls:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("达到阈值未提前提交")
	}
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
	require.NoError(t, db.Set(newFund("fresh", "HK", 1), 0))
	select {
	case <-target.calls:
		t.Fatal("新空闲周期错误继承排空状态")
	case <-time.After(30 * time.Millisecond):
	}
}

// TestAdaptiveSameKeyAndDeadline 验证同 key 不累计阈值，后续写入不推迟首条期限。
func TestAdaptiveSameKeyAndDeadline(t *testing.T) {
	db, target := newAdaptiveDB(t, 100*time.Millisecond, 4)
	start := time.Now()
	for i := 0; i < 4; i++ {
		require.NoError(t, db.Set(newFund("same", "HK", float64(i)), 0))
	}
	select {
	case <-target.calls:
		t.Fatal("同 key 写入错误触发数量阈值")
	case <-time.After(20 * time.Millisecond):
	}
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
	require.NoError(t, db.Set(newFund("same", "HK", 9), 0))
	select {
	case when := <-target.calls:
		require.GreaterOrEqual(t, when.Sub(start), 100*time.Millisecond)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("首条期限没有触发同步")
	}
}

// TestAdaptiveFailureSignalsAndRecovery 验证写入风暴不能绕过失败退避，恢复后继续排空。
func TestAdaptiveFailureSignalsAndRecovery(t *testing.T) {
	db, target := newAdaptiveDB(t, 20*time.Millisecond, 1)
	target.mu.Lock()
	target.failure = true
	target.mu.Unlock()
	require.NoError(t, db.Set(newFund("failed", "HK", 1), 0))
	var first time.Time
	select {
	case first = <-target.calls:
	case <-time.After(time.Second):
		t.Fatal("没有首次尝试")
	}
	for i := 0; i < 30; i++ {
		require.NoError(t, db.Set(newFund(fmt.Sprint(i), "HK", 1), 0))
	}
	require.Positive(t, db.GetCachedPendingSyncCount())
	target.mu.Lock()
	target.failure = false
	target.mu.Unlock()
	select {
	case when := <-target.calls:
		require.GreaterOrEqual(t, when.Sub(first), 100*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("未从失败退避恢复")
	}
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
}

// TestAdaptiveCloseAndRestart 验证收集中关闭可中断，旧数据无需重写即可恢复排空。
func TestAdaptiveCloseAndRestart(t *testing.T) {
	db, _ := newAdaptiveDB(t, time.Hour, 32)
	require.NoError(t, db.Set(newFund("restart", "HK", 1), 0))
	path := db.manager.config.Path
	cfg := db.manager.config
	started := time.Now()
	require.Error(t, db.Close()) // 未确认 pending 必须显式返回错误
	require.Less(t, time.Since(started), time.Second)
	require.NoError(t, CloseSharedManager(path))
	reopened, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	target := &boundedFundTarget{confirm: 100}
	require.NoError(t, reopened.UseWriteBehind(target))
	defer func() { _ = reopened.Close(); _ = CloseSharedManager(path) }()
	require.Eventually(t, func() bool { return reopened.GetCachedPendingSyncCount() == 0 }, 300*time.Millisecond, time.Millisecond)
	calls, batches := target.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, []int{1}, batches)
}

// TestAdaptiveLegacyPendingUpgrade 验证旧零配置创建的 pending 可原地启用新策略，无需清数据。
func TestAdaptiveLegacyPendingUpgrade(t *testing.T) {
	old := newBoundedSyncFundDB(t, &boundedFundTarget{}, 3)
	path := old.manager.config.Path
	cfg := old.manager.config
	require.Zero(t, cfg.SyncFlushThreshold)
	require.Error(t, old.Close())
	require.NoError(t, CloseSharedManager(path))
	cfg.AutoSync = true
	cfg.SyncFlushThreshold = 32
	cfg.SyncMaxCollectDelay = time.Hour
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close(); _ = CloseSharedManager(path) }()
	require.Equal(t, 3, db.GetCachedPendingSyncCount())
	target := &boundedFundTarget{confirm: 100}
	require.NoError(t, db.UseWriteBehind(target))
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, 300*time.Millisecond, time.Millisecond)
	_, batches := target.snapshot()
	require.Equal(t, []int{3}, batches)
}

// TestAdaptiveSchedule 验证阈值、首条期限、跨批排空和错误退避的精确时间边界。
func TestAdaptiveSchedule(t *testing.T) {
	cfg := DefaultProductionConfig("unused")
	cfg.SyncFlushThreshold = 32
	cfg.SyncMaxCollectDelay = 20 * time.Millisecond
	cfg.SyncBacklogDrainDelay = 0
	cfg.SyncMinInterval = 100 * time.Millisecond
	cfg.SyncMaxInterval = 200 * time.Millisecond
	s := adaptiveSyncSchedule{config: cfg}
	now := time.Now()
	require.Equal(t, 20*time.Millisecond, s.delay(now, 1, now))
	require.Equal(t, 5*time.Millisecond, s.delay(now.Add(15*time.Millisecond), 31, now))
	require.Zero(t, s.delay(now, 32, now))
	require.Zero(t, s.delay(now.Add(20*time.Millisecond), 1, now))
	s.completed(now, now, 2, nil)
	require.Zero(t, s.delay(now, 1, now))
	// 队列清空后新周期必须重新收集，不能沿用排空状态。
	next := now.Add(time.Millisecond)
	require.Equal(t, 20*time.Millisecond, s.delay(next, 1, next))
	s.completed(now, now, 0, nil)
	require.Equal(t, 100*time.Millisecond, s.delay(now, 100, now))
	require.Equal(t, 50*time.Millisecond, s.delay(now.Add(50*time.Millisecond), 1000, now))
	s.completed(now.Add(100*time.Millisecond), now, 0, nil)
	require.Equal(t, 200*time.Millisecond, s.delay(now.Add(100*time.Millisecond), 1000, now))
	s.completed(now, now, 1, fmt.Errorf("部分失败"))
	require.Zero(t, s.delay(now, 1000, now))
	require.Zero(t, s.retryDelay)
	s.completed(now, now, 1, nil)
	require.Zero(t, s.delay(now, 1, now))
	require.Zero(t, s.retryDelay)
	require.Zero(t, s.delay(now, 1, time.Time{})) // 历史 pending 不收集
	cfg.SyncBacklogDrainDelay = 3 * time.Millisecond
	s = adaptiveSyncSchedule{config: cfg}
	s.completed(now, now, 1, nil)
	require.Equal(t, 3*time.Millisecond, s.delay(now, 100, now))
}

// TestAdaptiveRecoveredBacklogDrains 验证恢复积压不重复等待旧收集窗口。
func TestAdaptiveRecoveredBacklogDrains(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.SyncBatchSize = 2
	cfg.SyncBatchDelay = time.Second
	require.NoError(t, json.Unmarshal([]byte(`{"sync_flush_threshold":2,"sync_max_collect_delay":20000000}`), &cfg))
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	target := &boundedFundTarget{confirm: 100}
	require.NoError(t, db.UseWriteBehind(target))
	for i := 0; i < 6; i++ {
		require.NoError(t, db.Set(newFund(fmt.Sprint(i), "HK", 1), 0))
	}
	// 测试在 worker 启动前设置配置，避免与运行中的读操作竞争。
	db.manager.config.AutoSync = true
	db.startWriteBehindWorker()
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, 300*time.Millisecond, time.Millisecond)
	_, batches := target.snapshot()
	require.Equal(t, []int{2, 2, 2}, batches)
}

// TestAdaptivePartialProgressDoesNotBackoffHealthyWork 验证达到10秒退避上限后真实确认120条仍按原drain间隔排空。
func TestAdaptivePartialProgressDoesNotBackoffHealthyWork(t *testing.T) {
	for _, drain := range []time.Duration{0, 25 * time.Millisecond} {
		t.Run(drain.String(), func(t *testing.T) {
			cfg := DefaultProductionConfig("unused")
			cfg.SyncFlushThreshold = 8
			cfg.SyncMaxCollectDelay = 20 * time.Millisecond
			cfg.SyncBacklogDrainDelay = drain
			cfg.SyncMinInterval = time.Second
			cfg.SyncMaxInterval = 10 * time.Second
			s := adaptiveSyncSchedule{config: cfg}
			now := time.Unix(100, 0)
			for range 5 {
				s.completed(now, now, 0, errors.New("测试零确认"))
			}
			require.Equal(t, 10*time.Second, s.delay(now, 128, now))
			s.completed(now, now, 120, errors.New("测试剩余8条失败"))
			require.Equal(t, drain, s.delay(now, 128, now))
			require.Zero(t, s.retryDelay)
			// 新通知不能跳过非零drain，部分成功转为零进展后重新从最小退避开始。
			require.Equal(t, drain, s.delay(now, 1000, now))
			s.completed(now, now, 0, nil)
			require.Equal(t, time.Second, s.delay(now, 1000, now))
			require.Equal(t, time.Second, s.retryDelay)
		})
	}
}

// TestAdaptivePartialProgressAutomaticWorker 验证真实Badger与UseWriteBehind自动排空两批健康数据，失败尾部退避后恢复原key。
func TestAdaptivePartialProgressAutomaticWorker(t *testing.T) {
	for _, drain := range []time.Duration{0, 25 * time.Millisecond} {
		t.Run(drain.String(), func(t *testing.T) {
			path := t.TempDir()
			cfg := DefaultProductionConfig(path)
			cfg.AutoSync = true
			cfg.SyncBatchSize = 128
			cfg.SyncFlushThreshold = 128
			cfg.SyncMaxCollectDelay = time.Hour
			cfg.SyncBacklogDrainDelay = drain
			cfg.SyncMinInterval = 2 * time.Second
			cfg.SyncMaxInterval = 4 * time.Second
			db, err := NewSharedBadgerDB[testFund](path, cfg)
			require.NoError(t, err)
			gate := make(chan struct{})
			var release sync.Once
			var first sync.Once
			var recovered atomic.Bool
			type round struct {
				at              time.Time
				healthy, failed int
			}
			calls := make(chan round, 16)
			target := fundTargetFunc(func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
				value := round{at: time.Now()}
				keys := []string{}
				all := recovered.Load()
				for _, item := range items {
					if !all && strings.HasPrefix(item.Item.UserID, "00-failed-") {
						value.failed++
					} else {
						value.healthy++
						keys = append(keys, item.Key)
					}
				}
				calls <- value
				first.Do(func() { <-gate })
				result := &WriteBehindResult{ConfirmedKeys: keys}
				if value.failed > 0 {
					return result, errors.New("测试部分失败")
				}
				return result, nil
			})
			t.Cleanup(func() { release.Do(func() { close(gate) }); _ = db.Close(); _ = CloseSharedManager(path) })
			require.NoError(t, db.UseWriteBehind(target))
			for i := range 8 {
				require.NoError(t, db.Set(newFund(fmt.Sprintf("00-failed-%02d", i), "spot", 1), 0))
			}
			for i := range 240 {
				require.NoError(t, db.Set(newFund(fmt.Sprintf("10-healthy-%03d", i), "spot", 1), 0))
			}
			require.Equal(t, 248, db.GetCachedPendingSyncCount())
			persistent, err := db.GetPendingSyncCount()
			require.NoError(t, err)
			require.Equal(t, 248, persistent)
			next := func(timeout time.Duration) round {
				t.Helper()
				select {
				case c := <-calls:
					return c
				case <-time.After(timeout):
					t.Fatal("自动worker未按drain推进，不能用ForceSync代替")
					return round{}
				}
			}
			a := next(time.Second)
			require.Equal(t, 120, a.healthy)
			require.Equal(t, 8, a.failed)
			releasedAt := time.Now()
			release.Do(func() { close(gate) })
			b := next(time.Second)
			require.Equal(t, 120, b.healthy)
			require.Equal(t, 8, b.failed)
			require.GreaterOrEqual(t, b.at.Sub(releasedAt), drain)
			tail := next(time.Second)
			require.Zero(t, tail.healthy)
			require.Equal(t, 8, tail.failed)
			require.Eventually(t, func() bool { return db.GetSyncMetrics().Attempts >= 3 }, time.Second, time.Millisecond)
			metrics := db.GetSyncMetrics()
			require.Equal(t, uint64(240), metrics.SyncedItems)
			require.Equal(t, uint64(3), metrics.Failures)
			require.True(t, metrics.LastSuccessAt.IsZero())
			persistent, err = db.GetPendingSyncCount()
			require.NoError(t, err)
			require.Equal(t, 8, persistent)
			require.Equal(t, 8, db.GetCachedPendingSyncCount())
			for i := range 8 {
				w, e := db.getWrapper(db.generateKey(newFund(fmt.Sprintf("00-failed-%02d", i), "spot", 1)))
				require.NoError(t, e)
				require.False(t, w.IsSynced)
			}
			// 真实新写入和通知风暴均不能让全失败尾部绕过已建立的零进展退避。
			require.NoError(t, db.Set(newFund("00-failed-00", "spot", 2), 0))
			for range 100 {
				db.triggerSync()
			}
			select {
			case <-calls:
				t.Fatal("新通知绕过了零进展退避")
			case <-time.After(time.Second):
			}
			recovered.Store(true)
			retry := next(3 * time.Second)
			require.GreaterOrEqual(t, retry.at.Sub(tail.at), cfg.SyncMinInterval)
			require.Equal(t, 8, retry.healthy)
			require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 && db.GetSyncMetrics().SyncedItems == 248 }, time.Second, time.Millisecond)
			persistent, err = db.GetPendingSyncCount()
			require.NoError(t, err)
			require.Zero(t, persistent)
			final := db.GetSyncMetrics()
			require.Equal(t, uint64(4), final.Attempts)
			require.Equal(t, uint64(3), final.Failures)
			require.False(t, final.LastSuccessAt.IsZero())
			t.Logf("自动worker: 两批120确认/8失败；尾部0确认后退避%s；恢复8个原key；drain=%s", cfg.SyncMinInterval, drain)
		})
	}
}

// TestAdaptiveFailedPrefixWindowBoundary 单列当前队头范围：失败占满前缀窗口时，后方健康项仍不会被选中。
func TestAdaptiveFailedPrefixWindowBoundary(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.SyncBatchSize = 8
	cfg.SyncFlushThreshold = 8
	cfg.SyncMaxCollectDelay = time.Hour
	cfg.SyncMinInterval = 100 * time.Millisecond
	cfg.SyncMaxInterval = 200 * time.Millisecond
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	var healthySelected atomic.Int32
	calls := make(chan time.Time, 16)
	var recoverAll atomic.Bool
	require.NoError(t, db.UseWriteBehind(fundTargetFunc(func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		calls <- time.Now()
		keys := []string{}
		for _, item := range items {
			if strings.HasPrefix(item.Item.UserID, "10-") {
				healthySelected.Add(1)
			}
			if recoverAll.Load() {
				keys = append(keys, item.Key)
			}
		}
		if len(keys) == 0 {
			return nil, errors.New("测试前缀全失败")
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, nil
	})))
	for i := range 8 {
		require.NoError(t, db.Set(newFund(fmt.Sprintf("00-failed-%02d", i), "spot", 1), 0))
	}
	require.NoError(t, db.Set(newFund("10-healthy", "spot", 1), 0))
	cfg.AutoSync = true
	db.manager.config.AutoSync = true
	db.startWriteBehindWorker()
	var first, time2 time.Time
	select {
	case first = <-calls:
	case <-time.After(time.Second):
		t.Fatal("未开始自动同步")
	}
	select {
	case time2 = <-calls:
	case <-time.After(time.Second):
		t.Fatal("未进行有界重试")
	}
	require.GreaterOrEqual(t, time2.Sub(first), cfg.SyncMinInterval)
	require.Zero(t, healthySelected.Load(), "本次未引入公平lane或跳过失败前缀")
	exact, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 9, exact)
	require.Equal(t, 9, db.GetCachedPendingSyncCount())
	recoverAll.Store(true)
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
	require.Positive(t, healthySelected.Load())
	t.Log("边界已复现：前8条失败使窗口后健康项保持pending；解除前缀故障后自动收敛")
}

// TestAdaptivePartialProgressCloseAndRestart 验证部分成功后的失败pending在关闭返回错误后仍可原地重启自动恢复。
func TestAdaptivePartialProgressCloseAndRestart(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.SyncBatchSize = 128
	cfg.SyncFlushThreshold = 128
	cfg.SyncMaxCollectDelay = time.Hour
	cfg.SyncMinInterval = time.Second
	cfg.SyncMaxInterval = 2 * time.Second
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	require.NoError(t, db.UseWriteBehind(fundTargetFunc(func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, item := range items {
			if !strings.HasPrefix(item.Item.UserID, "00-") {
				keys = append(keys, item.Key)
			}
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, errors.New("测试保留失败pending")
	})))
	for i := range 8 {
		require.NoError(t, db.Set(newFund(fmt.Sprintf("00-failed-%02d", i), "spot", 1), 0))
	}
	for i := range 120 {
		require.NoError(t, db.Set(newFund(fmt.Sprintf("10-healthy-%03d", i), "spot", 1), 0))
	}
	cfg.AutoSync = true
	db.manager.config.AutoSync = true
	db.startWriteBehindWorker()
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 8 }, time.Second, time.Millisecond)
	var pending *PendingSyncError
	require.ErrorAs(t, db.Close(), &pending)
	require.Equal(t, 8, pending.Count)
	require.NoError(t, CloseSharedManager(path))
	reopened, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close(); _ = CloseSharedManager(path) })
	exact, err := reopened.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 8, exact)
	require.Equal(t, 8, reopened.GetCachedPendingSyncCount())
	require.NoError(t, reopened.UseWriteBehind(&boundedFundTarget{confirm: 128}))
	require.Eventually(t, func() bool { return reopened.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
	require.Equal(t, uint64(8), reopened.GetSyncMetrics().SyncedItems)
}
