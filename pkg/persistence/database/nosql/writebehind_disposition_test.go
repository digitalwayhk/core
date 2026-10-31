// 本文件验证显式失败处置的有限预算、组隔离及真实Badger恢复，不连接业务环境。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type failureTestTarget struct {
	sync   func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error)
	handle func(context.Context, WriteBehindFailure[testFund]) (WriteBehindDecision, error)
}

// TestUnknownOutcomeDoesNotBlindlyReapply 验证持续未知只调用有限确认回调，不能重新执行原target。
func TestUnknownOutcomeDoesNotBlindlyReapply(t *testing.T) {
	var targets, handlers atomic.Int32
	a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		targets.Add(1)
		return nil, errors.New("测试写结果未知")
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		handlers.Add(1)
		d := failureDecision(f, WriteBehindResolve)
		d.Outcome = WriteBehindStillUnknown
		return d, nil
	}}
	db := openFailureTestDB(t, t.TempDir(), true, a)
	require.NoError(t, db.Set(newFund("unknown", "g", 1), 0))
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, 2*time.Second, time.Millisecond)
	require.Equal(t, int32(1), targets.Load())
	require.Equal(t, int32(4), handlers.Load())
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
}

// TestInvalidFailureDecisionRetainsOriginal 验证非法身份/版本、无持久引用、panic和回调存储异常均保留原pending并暂停。
func TestInvalidFailureDecisionRetainsOriginal(t *testing.T) {
	for _, mode := range []string{"extra_key", "version", "fingerprint", "invalid_action", "receipt", "panic", "storage"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
				calls.Add(1)
				return nil, errors.New("测试失败")
			}}
			a.handle = func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
				d := failureDecision(f, WriteBehindConfirm)
				d.Outcome = WriteBehindCommitted
				switch mode {
				case "extra_key":
					d.ID = "not_original"
				case "version":
					d.Version = d.Version.Add(time.Second)
				case "fingerprint":
					d.Fingerprint = "different"
				case "invalid_action":
					d.Action = "skip"
				case "receipt":
					d.Action = WriteBehindHandoff
					d.Receipt = &WriteBehindHandoffReceipt{DispositionID: d.ID, Version: d.Version, Fingerprint: d.Fingerprint, ReceiptID: "memory_only"}
				case "panic":
					panic("测试panic")
				case "storage":
					return d, errors.New("测试应用存储不可用")
				}
				return d, nil
			}
			db := openFailureTestDB(t, t.TempDir(), true, a)
			require.NoError(t, db.Set(newFund("bad", "g", 1), 0))
			require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
			require.Equal(t, 1, db.GetCachedPendingSyncCount())
			require.Zero(t, db.GetSyncMetrics().SyncedItems)
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func durableTestReceipt(t *testing.T, app *badger.DB, f WriteBehindFailure[testFund]) *WriteBehindHandoffReceipt {
	t.Helper()
	var receipt WriteBehindHandoffReceipt
	err := app.Update(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(f.Record.ID))
		if e == nil {
			return item.Value(func(b []byte) error {
				var saved struct {
					Record  WriteBehindFailureRecord[testFund]
					Receipt WriteBehindHandoffReceipt
				}
				if e := json.Unmarshal(b, &saved); e != nil {
					return e
				}
				receipt = saved.Receipt
				return nil
			})
		}
		if !errors.Is(e, badger.ErrKeyNotFound) {
			return e
		}
		receipt = WriteBehindHandoffReceipt{DispositionID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, ReceiptID: "durable-" + f.Record.ID, Owner: "test-app", DurableReference: "application-owned-copy:" + f.Record.ID}
		b, e := json.Marshal(struct {
			Record  WriteBehindFailureRecord[testFund]
			Receipt WriteBehindHandoffReceipt
		}{f.Record, receipt})
		if e != nil {
			return e
		}
		return txn.Set([]byte(f.Record.ID), b)
	})
	require.NoError(t, err)
	return &receipt
}

// TestHandoffReplyLostReusesOriginalReceipt 验证真实独立应用存储先提交；回执丢失与双存储重开后复用原身份，交接不解除组屏障。
func TestHandoffReplyLostReusesOriginalReceipt(t *testing.T) {
	appPath := t.TempDir()
	app, err := badger.Open(badger.DefaultOptions(appPath).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Close() })
	var lost atomic.Bool
	lost.Store(true)
	var firstCalls, laterCalls atomic.Int32
	a := &failureTestTarget{}
	a.sync = func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, v := range items {
			if v.Item.UserID == "first" {
				firstCalls.Add(1)
			} else {
				laterCalls.Add(1)
				keys = append(keys, v.Key)
			}
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, errors.New("测试首项结果未知")
	}
	a.handle = func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		r := durableTestReceipt(t, app, f)
		d := failureDecision(f, WriteBehindHandoff)
		d.Receipt = r
		if lost.Swap(false) {
			return d, errors.New("测试应用提交后回执丢失")
		}
		return d, nil
	}
	path := t.TempDir()
	db := openFailureTestDB(t, path, true, a)
	require.NoError(t, db.Set(newFund("first", "ordered", 1), 0))
	require.NoError(t, db.Set(newFund("later", "ordered", 1), 0))
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	r, _, err := db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	var original WriteBehindFailureRecord[testFund]
	for _, v := range r {
		if v.Snapshot.Item.UserID == "first" {
			original = v
		}
	}
	require.NotEmpty(t, original.ID)
	require.Equal(t, int32(0), laterCalls.Load())
	require.Equal(t, 2, db.GetCachedPendingSyncCount())
	require.Error(t, db.Close())
	require.NoError(t, CloseSharedManager(path))
	require.NoError(t, app.Close())
	app, err = badger.Open(badger.DefaultOptions(appPath).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	reopened := openFailureTestDB(t, path, true, a)
	d := WriteBehindDecision{ID: original.ID, Version: original.Snapshot.UpdatedAt, Fingerprint: original.Fingerprint, Scope: WriteBehindScopeGroup, Action: WriteBehindResolve, Outcome: WriteBehindStillUnknown}
	require.NoError(t, reopened.ApplyWriteBehindDecision(context.Background(), d))
	require.Eventually(t, func() bool { m, e := reopened.GetWriteBehindDispositionMetrics(); return e == nil && m.HandedOff == 1 }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), firstCalls.Load())
	require.Equal(t, int32(0), laterCalls.Load())
	require.Equal(t, 1, reopened.GetCachedPendingSyncCount())
	require.Zero(t, reopened.GetSyncMetrics().SyncedItems)
	entries := 0
	require.NoError(t, app.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			entries++
		}
		return nil
	}))
	require.Equal(t, 1, entries)
	// Core已持久保存回执后的再次重开，不重做target，也不自动放行同组后序。
	require.Error(t, reopened.Close())
	require.NoError(t, CloseSharedManager(path))
	final := openFailureTestDB(t, path, true, a)
	_, err = final.ForceSyncBatch(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, int32(0), laterCalls.Load())
	d.Action = WriteBehindRelease
	d.Outcome = WriteBehindOrderingReleased
	require.NoError(t, final.ApplyWriteBehindDecision(context.Background(), d))
	require.Eventually(t, func() bool { return final.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), laterCalls.Load())
}

// TestStaleDispositionCannotAcknowledgeNewVersion 验证失败版本原件不被并发新值抹去，旧接管不能确认新值。
func TestStaleDispositionCannotAcknowledgeNewVersion(t *testing.T) {
	a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		return nil, errors.New("测试原版本失败")
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	value := newFund("same", "g", 1)
	require.NoError(t, db.Set(value, 0))
	_, err := db.ForceSyncBatch(context.Background(), 4)
	require.Error(t, err)
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	records, _, err := db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	old := records[0]
	require.NoError(t, db.Set(newFund("same", "g", 2), 0))
	d := WriteBehindDecision{ID: old.ID, Version: old.Snapshot.UpdatedAt, Fingerprint: old.Fingerprint, Action: WriteBehindConfirm, Scope: WriteBehindScopeGroup, Outcome: WriteBehindCommitted}
	require.NoError(t, db.ApplyWriteBehindDecision(context.Background(), d))
	w, err := db.getWrapper(db.generateKey(value))
	require.NoError(t, err)
	require.False(t, w.IsSynced)
	require.Equal(t, float64(2), w.Item.Balance)
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
	require.Zero(t, db.GetSyncMetrics().SyncedItems)
	records, _, err = db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, r := range records {
		if r.ID == old.ID {
			require.Equal(t, float64(1), r.Snapshot.Item.Balance)
		}
	}
	d.Version = w.UpdatedAt
	require.Error(t, db.ApplyWriteBehindDecision(context.Background(), d))
}

// TestSlowFailureHandlerDoesNotBlockHealthyGroups 验证不合作回调超时后仍占原槽，健康组继续且迟到确认不得ACK。
func TestSlowFailureHandlerDoesNotBlockHealthyGroups(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	exited := make(chan struct{}, 1)
	var healthy atomic.Int32
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, v := range items {
			if v.Item.UserID == "healthy" {
				healthy.Add(1)
				keys = append(keys, v.Key)
			}
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, errors.New("测试失败")
	}}
	a.handle = func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		entered <- struct{}{}
		<-gate
		exited <- struct{}{}
		d := failureDecision(f, WriteBehindConfirm)
		d.Outcome = WriteBehindCommitted
		return d, nil
	}
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = true
	cfg.SyncBatchSize = 4
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.HandlerTimeout = 60 * time.Millisecond
	cfg.WriteBehindFailure.HandlerWorkers = 1
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehindWithFailureHandler(a, a))
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
		}
		_ = db.Close()
		_ = CloseSharedManager(path)
	})
	require.NoError(t, db.Set(newFund("slow", "a", 1), 0))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("回调未进入")
	}
	require.NoError(t, db.Set(newFund("healthy", "b", 1), 0))
	require.Eventually(t, func() bool { return healthy.Load() == 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	snapshot, e := db.GetWriteBehindDispositionMetrics()
	require.NoError(t, e)
	require.Equal(t, 1, snapshot.HandlerActive)
	require.LessOrEqual(t, snapshot.HandlerActive, snapshot.HandlerLimit)
	require.Error(t, db.CloseWithTimeout(10*time.Millisecond, 10*time.Millisecond))
	close(gate)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("受控回调没有退出")
	}
	db.wg.Wait()
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
	require.Equal(t, uint64(1), db.GetSyncMetrics().SyncedItems)
}

func (a *failureTestTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
	return a.sync(ctx, items)
}
func (a *failureTestTarget) OrderingGroup(item *SyncQueueItem[testFund]) (string, error) {
	return item.Item.Market, nil
}
func (a *failureTestTarget) HandleFailure(ctx context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
	return a.handle(ctx, f)
}

func failureTestPolicy() *WriteBehindFailurePolicy {
	return &WriteBehindFailurePolicy{StoreID: "test-store", MaxAttempts: 2, MinRetryInterval: 50 * time.Millisecond, MaxRetryInterval: 100 * time.Millisecond, HandlerMaxAttempts: 4, HandlerTimeout: time.Second, HandlerWorkers: 2, ScanLimit: 4}
}

func failureDecision(f WriteBehindFailure[testFund], action WriteBehindFailureAction) WriteBehindDecision {
	return WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: action, Scope: WriteBehindScopeKey}
}

func openFailureTestDB(t *testing.T, path string, auto bool, target *failureTestTarget) *PrefixedBadgerDB[testFund] {
	t.Helper()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = auto
	cfg.SyncBatchSize = 4
	cfg.WriteBehindFailure = failureTestPolicy()
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehindWithFailureHandler(target, target))
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	return db
}

// TestFailurePolicyRequiresFiniteBudget 验证显式配置没有无限默认，旧入口不得遗漏处置器。
func TestFailurePolicyRequiresFiniteBudget(t *testing.T) {
	for _, mutate := range []func(*WriteBehindFailurePolicy){
		func(p *WriteBehindFailurePolicy) { p.MaxAttempts = 0 },
		func(p *WriteBehindFailurePolicy) { p.MinRetryInterval = -1 },
		func(p *WriteBehindFailurePolicy) { p.HandlerMaxAttempts = 0 },
		func(p *WriteBehindFailurePolicy) { p.HandlerTimeout = 0 },
		func(p *WriteBehindFailurePolicy) { p.ScanLimit = 0 },
	} {
		p := failureTestPolicy()
		mutate(p)
		require.Error(t, p.Validate())
	}
	p := failureTestPolicy()
	p.MaxAttempts = 0
	p.MaxElapsed = time.Second
	require.NoError(t, p.Validate())
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.WriteBehindFailure = failureTestPolicy()
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	require.Error(t, db.UseWriteBehind(&boundedFundTarget{confirm: 4}))
}

// TestPausedWindowDoesNotStarveHealthyGroups 验证完整失败窗口暂停后，后方健康组由自动worker推进。
func TestPausedWindowDoesNotStarveHealthyGroups(t *testing.T) {
	var healthy atomic.Int32
	target := &failureTestTarget{}
	target.sync = func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, v := range items {
			if strings.HasPrefix(v.Item.UserID, "healthy") {
				healthy.Add(1)
				keys = append(keys, v.Key)
			}
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, errors.New("测试未确认结果")
	}
	target.handle = func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}
	db := openFailureTestDB(t, t.TempDir(), false, target)
	for i := range 12 {
		require.NoError(t, db.Set(newFund(fmt.Sprintf("failed-%02d", i), fmt.Sprintf("group-%02d", i), 1), 0))
	}
	db.manager.config.AutoSync = true
	db.startWriteBehindWorker()
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused >= 4 }, time.Second, time.Millisecond)
	require.NoError(t, db.Set(newFund("healthy", "independent", 1), 0))
	require.Eventually(t, func() bool { return healthy.Load() == 1 }, 3*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 12 }, 3*time.Second, time.Millisecond)
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	require.Equal(t, 12, count)
}

// TestRetryBudgetSurvivesRestart 验证target预算不会因进程式重开或重新通知重置。
func TestRetryBudgetSurvivesRestart(t *testing.T) {
	path := t.TempDir()
	var calls atomic.Int32
	target := &failureTestTarget{}
	target.sync = func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		calls.Add(1)
		return nil, errors.New("测试确定回滚，应用负责分类")
	}
	target.handle = func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		d := failureDecision(f, WriteBehindRetry)
		d.Outcome = WriteBehindNotCommitted
		d.RetryAfter = 50 * time.Millisecond
		return d, nil
	}
	db := openFailureTestDB(t, path, false, target)
	require.NoError(t, db.Set(newFund("retry", "g", 1), 0))
	_, err := db.ForceSyncBatch(context.Background(), 4)
	require.Error(t, err)
	require.Eventually(t, func() bool {
		r, _, e := db.ListWriteBehindFailures("", 10)
		return e == nil && len(r) == 1 && r[0].State == WriteBehindRetrying
	}, time.Second, time.Millisecond)
	require.Error(t, db.Close())
	require.NoError(t, CloseSharedManager(path))
	reopened := openFailureTestDB(t, path, true, target)
	require.Eventually(t, func() bool { m, e := reopened.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, 3*time.Second, time.Millisecond)
	require.Equal(t, int32(2), calls.Load())
	r, _, err := reopened.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, r, 1)
	require.Equal(t, 2, r[0].Attempts)
	require.Equal(t, "target_budget_exhausted", r[0].Reason)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, int32(2), calls.Load())
}

// TestRepeatedHandoffDecisionIsIdempotent 验证Core提交后的调用方回执丢失可重复查询/提交，不能重复扣pending或换回执。
func TestRepeatedHandoffDecisionIsIdempotent(t *testing.T) {
	a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		return nil, errors.New("测试失败")
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	require.NoError(t, db.Set(newFund("handoff", "g", 1), 0))
	_, err := db.ForceSyncBatch(context.Background(), 4)
	require.Error(t, err)
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	r, _, err := db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, r, 1)
	app, err := badger.Open(badger.DefaultOptions(t.TempDir()).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	defer app.Close()
	f := WriteBehindFailure[testFund]{Record: r[0]}
	d := failureDecision(f, WriteBehindHandoff)
	d.Receipt = durableTestReceipt(t, app, f)
	require.NoError(t, db.ApplyWriteBehindDecision(context.Background(), d))
	require.NoError(t, db.ApplyWriteBehindDecision(context.Background(), d))
	require.Zero(t, db.GetCachedPendingSyncCount())
	require.Zero(t, db.GetSyncMetrics().SyncedItems)
	wrapper, readErr := db.getWrapper(r[0].Snapshot.Key)
	require.NoError(t, readErr)
	require.False(t, wrapper.IsSynced, "应用接管不能冒充远端业务同步成功")
	d.Receipt.ReceiptID = "different"
	require.Error(t, db.ApplyWriteBehindDecision(context.Background(), d))
}

// TestDispositionModeCannotBeUnboundIntoLegacy 验证兼容入口不允许静默绕过新模式及未处置责任。
func TestDispositionModeCannotBeUnboundIntoLegacy(t *testing.T) {
	a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) { return nil, nil }, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	db.SetSyncDB(nil)
	db.syncLock.RLock()
	bound := db.syncDB
	db.syncLock.RUnlock()
	require.True(t, bound, "旧解绑入口不能改变处置mode")
}

// TestDispositionImportedPendingRequiresConfirmation 验证旧模式投递次数与结果未知时不自动重放旧pending。
func TestDispositionImportedPendingRequiresConfirmation(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	old, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	require.NoError(t, old.UseWriteBehind(&boundedFundTarget{}))
	require.NoError(t, old.Set(newFund("legacy", "g", 1), 0))
	require.Error(t, old.Close())
	require.NoError(t, CloseSharedManager(path))
	var calls atomic.Int32
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		calls.Add(1)
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, path, true, a)
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	require.Zero(t, calls.Load())
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
}

// TestDispositionConcurrentAdmissions 验证并发本地可靠提交和同组确认不会因新增调度元数据冲突而失败或丢组索引。
func TestDispositionConcurrentAdmissions(t *testing.T) {
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		keys := []string{}
		for _, v := range items {
			keys = append(keys, v.Key)
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, nil
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, t.TempDir(), true, a)
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 64)
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- db.Set(newFund(fmt.Sprintf("concurrent-%03d", i), "ordered", 1), 0)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err, "新增持久调度元数据不得让独立key写入互相冲突")
	}
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 && db.GetSyncMetrics().SyncedItems == 64 }, 3*time.Second, time.Millisecond)
}

// TestFailureSnapshotIncludesBatchScope 验证应用收到同一原子批的未确认版本范围，不能要求其编造坏条目。
func TestFailureSnapshotIncludesBatchScope(t *testing.T) {
	observed := make(chan map[string]json.RawMessage, 2)
	original := errors.New("测试整批原始错误")
	a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) { return nil, original }, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		b, _ := json.Marshal(f)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(b, &fields)
		observed <- fields
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	require.NoError(t, db.Set(newFund("one", "a", 1), 0))
	require.NoError(t, db.Set(newFund("two", "b", 1), 0))
	_, err := db.ForceSyncBatch(context.Background(), 4)
	require.ErrorIs(t, err, original)
	var batchID string
	for range 2 {
		select {
		case fields := <-observed:
			var id string
			require.NoError(t, json.Unmarshal(fields["BatchID"], &id))
			require.NotEmpty(t, id)
			var refs []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(fields["Unconfirmed"], &refs))
			require.Len(t, refs, 2)
			require.NotEmpty(t, refs[0]["Snapshot"], "未确认范围必须携带完整原快照，而不仅是key引用")
			if batchID == "" {
				batchID = id
			} else {
				require.Equal(t, batchID, id)
			}
		case <-time.After(time.Second):
			t.Fatal("未收到有界失败快照")
		}
	}
}

// TestApplicationTargetScopeBlocksAdmission 验证应用明确声明目标故障后，即使有其他健康组也不能越过目标级暂停。
func TestApplicationTargetScopeBlocksAdmission(t *testing.T) {
	var fixed atomic.Bool
	var calls atomic.Int32
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		calls.Add(1)
		if !fixed.Load() {
			return nil, errors.New("测试目标整体不可达")
		}
		keys := []string{}
		for _, v := range items {
			keys = append(keys, v.Key)
		}
		return &WriteBehindResult{ConfirmedKeys: keys}, nil
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		d := failureDecision(f, WriteBehindPause)
		d.Scope = WriteBehindScopeTarget
		return d, nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	require.NoError(t, db.Set(newFund("first", "a", 1), 0))
	_, err := db.ForceSyncBatch(context.Background(), 4)
	require.Error(t, err)
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	require.NoError(t, db.Set(newFund("later", "b", 1), 0))
	_, err = db.ForceSyncBatch(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
	records, _, err := db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	var first WriteBehindFailureRecord[testFund]
	for _, r := range records {
		if r.Snapshot.Item.UserID == "first" {
			first = r
		}
	}
	fixed.Store(true)
	d := failureDecision(WriteBehindFailure[testFund]{Record: first}, WriteBehindRetry)
	d.Scope = WriteBehindScopeTarget
	d.Outcome = WriteBehindNotCommitted
	d.RetryAfter = 50 * time.Millisecond
	require.NoError(t, db.ApplyWriteBehindDecision(context.Background(), d))
	for range 100 {
		db.triggerSync()
	}
	_, err = db.ForceSyncBatch(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "手动同步和新通知都不能跳过持久next-attempt")
	require.Eventually(t, func() bool {
		_, e := db.ForceSyncBatch(context.Background(), 4)
		return e == nil && db.GetCachedPendingSyncCount() == 0
	}, time.Second, time.Millisecond)
	require.Equal(t, int32(3), calls.Load())
}

// TestCanceledTargetResultIsUnknown 验证取消后即便target迟到声称成功，也不能作为Core本地确认。
func TestCanceledTargetResultIsUnknown(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		once.Do(func() { close(entered) })
		<-release
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		return failureDecision(f, WriteBehindPause), nil
	}}
	db := openFailureTestDB(t, t.TempDir(), false, a)
	require.NoError(t, db.Set(newFund("cancel", "g", 1), 0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := db.ForceSyncBatch(ctx, 4); done <- err }()
	<-entered
	cancel()
	close(release)
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("取消路径未退出")
	}
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
	require.Zero(t, db.GetSyncMetrics().SyncedItems)
}

// TestResumeWaitsForActualHandlerExit 验证超时只是观测事实，人工重试也不能在旧应用回调仍执行时重做同组target。
func TestResumeWaitsForActualHandlerExit(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	var calls atomic.Int32
	a := &failureTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("测试失败")
		}
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key}}, nil
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		<-gate
		return failureDecision(f, WriteBehindPause), nil
	}}
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = true
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.HandlerTimeout = 20 * time.Millisecond
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { release.Do(func() { close(gate) }); _ = db.Close(); _ = CloseSharedManager(path) })
	require.NoError(t, db.UseWriteBehindWithFailureHandler(a, a))
	require.NoError(t, db.Set(newFund("blocked", "g", 1), 0))
	require.Eventually(t, func() bool { m, e := db.GetWriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	r, _, err := db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, r, 1)
	d := failureDecision(WriteBehindFailure[testFund]{Record: r[0]}, WriteBehindRetry)
	d.Outcome = WriteBehindNotCommitted
	d.RetryAfter = 50 * time.Millisecond
	require.NoError(t, db.ApplyWriteBehindDecision(context.Background(), d))
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load(), "租约/超时不是原回调停止的证明")
	release.Do(func() { close(gate) })
	require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, time.Second, time.Millisecond)
	require.Equal(t, int32(2), calls.Load())
}

// TestDispositionCloseTimeoutIsNotSuccessful 验证技术pending已交接为零也不能隐藏仍在执行的应用回调关闭超时。
func TestDispositionCloseTimeoutIsNotSuccessful(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	a := &failureTestTarget{sync: func(context.Context, []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		return nil, errors.New("测试未知结果")
	}, handle: func(_ context.Context, f WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		<-gate
		return failureDecision(f, WriteBehindPause), nil
	}}
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = true
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindFailure.HandlerTimeout = 20 * time.Millisecond
	db, err := NewSharedBadgerDB[testFund](path, cfg)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehindWithFailureHandler(a, a))
	t.Cleanup(func() { release.Do(func() { close(gate) }); db.wg.Wait(); _ = db.Close(); _ = CloseSharedManager(path) })
	require.NoError(t, db.Set(newFund("close", "g", 1), 0))
	require.Eventually(t, func() bool {
		m, e := db.GetWriteBehindDispositionMetrics()
		return e == nil && m.Paused == 1 && m.HandlerActive == 1
	}, time.Second, time.Millisecond)
	r, _, err := db.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, r, 1)
	app, err := badger.Open(badger.DefaultOptions(t.TempDir()).WithLogger(nil).WithSyncWrites(true))
	require.NoError(t, err)
	defer app.Close()
	f := WriteBehindFailure[testFund]{Record: r[0]}
	d := failureDecision(f, WriteBehindHandoff)
	d.Receipt = durableTestReceipt(t, app, f)
	require.NoError(t, db.ApplyWriteBehindDecision(context.Background(), d))
	require.Zero(t, db.GetCachedPendingSyncCount())
	require.Error(t, db.CloseWithTimeout(10*time.Millisecond, 10*time.Millisecond), "没有pending不代表在途应用回调已经退出")
	release.Do(func() { close(gate) })
}

// TestLegacyNilTargetValidationPrecedesDispositionLookup 验证旧零值实例的nil参数校验不因新增持久mode检查而panic。
func TestLegacyNilTargetValidationPrecedesDispositionLookup(t *testing.T) {
	var db PrefixedBadgerDB[testFund]
	require.NotPanics(t, func() { require.ErrorIs(t, db.UseWriteBehind(nil), ErrUnsafeWriteBehindConfig) })
	require.NotPanics(t, func() { require.ErrorIs(t, db.EnableWriteBehind(nil), ErrUnsafeWriteBehindConfig) })
}
