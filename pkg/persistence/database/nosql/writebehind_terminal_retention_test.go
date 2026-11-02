// 本文件用真实可靠store/Badger验证终态载荷回收；未解决原件及业务删除条件独立保留。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTerminalSnapshotDefaultDropsPayload 验证成功及未投递覆盖均不永久保存完整终态载荷，hook仍持有原件。
func TestTerminalSnapshotDefaultDropsPayload(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain_business", true: "delete_business"}[deleted], func(t *testing.T) {
			probe := &cleanupTestProbe{}
			s := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
			for revision := 1; revision <= 4; revision++ {
				require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "same", revision, deleted, probe)))
			}
			n, e := s.ForceSyncBatch(context.Background(), 16)
			require.NoError(t, e)
			require.Equal(t, 1, n.Confirmed)
			require.Zero(t, s.Metrics().Pending)
			records, _, e := s.ListWriteBehindFailures("", 100)
			require.NoError(t, e)
			require.NotEmpty(t, records)
			for _, r := range records {
				require.True(t, dispositionTerminal(r.State))
				require.NotNil(t, r.Snapshot, "版本和key元数据仍可用于依赖核验")
				require.Nil(t, r.Snapshot.Item, "终态不应默认永久保留每版本完整载荷")
			}
		})
	}
}

// expireTerminalTestRecords只推进技术历史的测试时钟，不修改原模型、预算或业务回执。
func expireTerminalTestRecords(t *testing.T, s *ReliableWriteStore[cleanupTestRecord]) {
	t.Helper()
	records, _, e := s.ListWriteBehindFailures("", 1000)
	require.NoError(t, e)
	for _, r := range records {
		if dispositionTerminal(r.State) {
			previousKey := s.db.terminalHistoryKey(&r)
			r.TerminalAt = time.Unix(1, 0).UTC()
			raw, e := json.Marshal(&r)
			require.NoError(t, e)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &fields))
			fields["HistoryAt"], e = json.Marshal(r.TerminalAt.Add(terminalMetadataGrace))
			require.NoError(t, e)
			raw, e = json.Marshal(fields)
			require.NoError(t, e)
			require.NoError(t, json.Unmarshal(raw, &r))
			require.NoError(t, s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
				if e := txn.Delete([]byte(previousKey)); e != nil {
					return e
				}
				if e := dispositionWrite(txn, s.db.dispositionPrefix()+"r:"+r.ID, &r); e != nil {
					return e
				}
				return txn.Set([]byte(s.db.terminalHistoryKey(&r)), []byte(r.ID))
			}))
		}
	}
}
func compactTerminalTestCycle(t *testing.T, s *ReliableWriteStore[cleanupTestRecord], limit int) {
	t.Helper()
	for range 1000 {
		r, e := s.CompactWriteBehindHistory(context.Background(), limit)
		require.NoError(t, e)
		if r.CycleComplete {
			return
		}
	}
	t.Fatal("有界维护未完成一轮")
}

// TestTerminalHistoryReclaimsVersionsAndBatches 验证持续覆盖与成功后载荷、版本、批次和最新映射均可结束，而非小墓碑永续增长。
func TestTerminalHistoryReclaimsVersionsAndBatches(t *testing.T) {
	s := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	for round := 0; round < 5; round++ {
		for i := 0; i < 10; i++ {
			for revision := 0; revision < 3; revision++ {
				require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), round*3+revision, true, &cleanupTestProbe{})))
			}
		}
		_, e := s.ForceSyncAll(context.Background())
		require.NoError(t, e)
		expireTerminalTestRecords(t, s)
		compactTerminalTestCycle(t, s, 3)
		records, _, e := s.ListWriteBehindFailures("", 1000)
		require.NoError(t, e)
		require.Empty(t, records)
		require.Zero(t, s.Metrics().Pending)
		require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
			for _, suffix := range []string{"r:", "b:", "key:", "ref:", "member:", "gc:", "q:", "g:"} {
				opts := badger.DefaultIteratorOptions
				opts.PrefetchValues = false
				opts.Prefix = []byte(s.db.dispositionPrefix() + suffix)
				it := txn.NewIterator(opts)
				it.Seek(opts.Prefix)
				valid := it.ValidForPrefix(opts.Prefix)
				it.Close()
				require.False(t, valid, "不能留下每版本无限历史: %s", suffix)
			}
			return nil
		}))
	}
}

// TestTerminalCompactionPreservesUnresolvedChain 验证未知旧版本、跨组新值及覆盖中间版本，维护不能移除或越过未解决前序。
func TestTerminalCompactionPreservesUnresolvedChain(t *testing.T) {
	target := &cleanupTestTarget{sync: func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		return nil, errors.New("结果未知")
	}}
	s := openCleanupTestStore(t, t.TempDir(), false, true, target)
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "same", 1, false, &cleanupTestProbe{})))
	_, e := s.ForceSyncBatch(context.Background(), 16)
	require.Error(t, e)
	require.Eventually(t, func() bool { m, e := s.WriteBehindDispositionMetrics(); return e == nil && m.Paused == 1 }, time.Second, time.Millisecond)
	for i := 2; i < 6; i++ {
		v := newCleanupTestRecord(t, "same", i, false, &cleanupTestProbe{})
		v.Group = "new-group"
		require.NoError(t, s.Save(context.Background(), v))
	}
	before, e := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	require.Zero(t, before.Confirmed, "压缩前也不能越过更早暂停责任")
	expireTerminalTestRecords(t, s)
	compactTerminalTestCycle(t, s, 2)
	records, _, e := s.ListWriteBehindFailures("", 100)
	require.NoError(t, e)
	require.Len(t, records, 2)
	var old, current WriteBehindFailureRecord[cleanupTestRecord]
	for _, r := range records {
		if r.State == WriteBehindPaused {
			old = r
		} else {
			current = r
		}
	}
	require.NotNil(t, old.Snapshot.Item)
	require.Equal(t, 1, old.Snapshot.Item.Revision)
	require.NotNil(t, current.Snapshot.Item)
	require.Equal(t, 5, current.Snapshot.Item.Revision)
	require.Equal(t, old.ID, current.PreviousID)
	result, e := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	require.Zero(t, result.Confirmed)
	require.Equal(t, old.Attempts, 1)
}

// TestTerminalAuditBudgetAndExpiry 验证显式完整审计条数/字节有界，过期后元数据亦可回收，业务副本仍按hook决定。
func TestTerminalAuditBudgetAndExpiry(t *testing.T) {
	base := t.TempDir()
	cfg := DefaultProductionConfig(base)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.WriteBehindFailure = failureTestPolicy()
	cfg.WriteBehindTerminalRetention = &WriteBehindTerminalRetention{MaxAge: time.Hour, MaxRecords: 2, MaxBytes: 1 << 20}
	s, _, e := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "audit", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg})
	require.NoError(t, e)
	t.Cleanup(func() { _ = s.Close(context.Background()); _ = CloseSharedManager(s.config.BasePath) })
	require.NoError(t, s.UseWriteBehindWithFailureHandler(&cleanupTestTarget{}, &cleanupTestTarget{}))
	for i := 0; i < 8; i++ {
		require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), 1, false, &cleanupTestProbe{})))
	}
	_, e = s.ForceSyncAll(context.Background())
	require.NoError(t, e)
	r, _, e := s.ListWriteBehindFailures("", 100)
	require.NoError(t, e)
	full := 0
	var bytes int64
	for _, r := range r {
		if r.Snapshot.Item != nil {
			full++
			bytes += r.AuditBytes
		}
	}
	require.Equal(t, 2, full)
	require.LessOrEqual(t, bytes, int64(1<<20))
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, CloseSharedManager(s.config.BasePath))
	reopened, _, e := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "audit", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg})
	require.NoError(t, e)
	s = reopened
	require.NoError(t, s.UseWriteBehindWithFailureHandler(&cleanupTestTarget{}, &cleanupTestTarget{}))
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "restart", 1, false, &cleanupTestProbe{})))
	_, e = s.ForceSyncAll(context.Background())
	require.NoError(t, e)
	r, _, e = s.ListWriteBehindFailures("", 100)
	require.NoError(t, e)
	full = 0
	for _, record := range r {
		if record.Snapshot.Item != nil {
			full++
		}
	}
	require.Equal(t, 2, full, "重启不能重新授予已占用审计预算")
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, CloseSharedManager(s.config.BasePath))
	cfg.WriteBehindTerminalRetention = nil
	reopened, _, e = NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "audit", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg})
	require.NoError(t, e)
	s = reopened
	require.NoError(t, s.UseWriteBehindWithFailureHandler(&cleanupTestTarget{}, &cleanupTestTarget{}))
	u, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Zero(t, u.TerminalPayloadBytes, "取消额外审计时绑定有界重核，不能一直等旧时限")
	expireTerminalTestRecords(t, s)
	compactTerminalTestCycle(t, s, 2)
	r, _, e = s.ListWriteBehindFailures("", 100)
	require.NoError(t, e)
	require.Empty(t, r)
	for i := 0; i < 8; i++ {
		v, e := s.GetLocal(context.Background(), fmt.Sprint(i))
		require.NoError(t, e)
		require.NotNil(t, v, "维护不能越过false删除hook")
	}
	require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
		var u [2]int64
		e := dispositionRead(txn, s.db.dispositionPrefix()+"audit-budget", &u)
		require.Equal(t, [2]int64{}, u)
		return e
	}))
}

// TestTerminalCompactionRollbackKeepsCountersAndReferences 验证真实本地事务提交前失败，逻辑删除/预算/结果均回滚。
func TestTerminalCompactionRollbackKeepsCountersAndReferences(t *testing.T) {
	s := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	for i := 0; i < 8; i++ {
		require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), 1, false, &cleanupTestProbe{})))
	}
	_, e := s.ForceSyncAll(context.Background())
	require.NoError(t, e)
	expireTerminalTestRecords(t, s)
	before, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	failure := errors.New("测试本地确认事务回滚")
	s.db.disposition.historyCommitObserver = func(*badger.Txn) error { return failure }
	result, e := s.CompactWriteBehindHistory(context.Background(), 8)
	require.ErrorIs(t, e, failure)
	require.Zero(t, result.Removed)
	after, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Equal(t, before, after)
	s.db.disposition.historyCommitObserver = nil
	compactTerminalTestCycle(t, s, 8)
	after, e = s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Zero(t, after.TerminalRecords)
	require.Equal(t, int64(8), after.BusinessRecords)
	require.Zero(t, after.Pending)
}

// TestTerminalRetentionRejectsUnboundedAudit 验证空对象/单一预算以及legacy模式均不能悄悄启用无限审计。
func TestTerminalRetentionRejectsUnboundedAudit(t *testing.T) {
	for _, r := range []*WriteBehindTerminalRetention{{}, {MaxAge: time.Hour}, {MaxAge: time.Hour, MaxRecords: 1}, {MaxAge: -1, MaxRecords: 1, MaxBytes: 1}} {
		cfg := DefaultProductionConfig(t.TempDir())
		cfg.WriteBehindFailure = failureTestPolicy()
		cfg.WriteBehindTerminalRetention = r
		require.ErrorIs(t, cfg.Validate(), ErrWriteBehindDisposition)
	}
	cfg := DefaultProductionConfig(t.TempDir())
	cfg.WriteBehindTerminalRetention = &WriteBehindTerminalRetention{MaxAge: time.Hour, MaxRecords: 1, MaxBytes: 1}
	require.ErrorIs(t, cfg.Validate(), ErrWriteBehindDisposition)
}

// TestTerminalHealthyGCIndependentOfActiveFailure一个真实未退出回调只能保护自身引用，不能阻止无关成功组回收。
func TestTerminalHealthyGCIndependentOfActiveFailure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	target := &cleanupTestTarget{sync: func(_ context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
		r := &WriteBehindResult{}
		for _, v := range items {
			if v.Item.Key == "blocked" {
				return nil, errors.New("应用结果待确认")
			}
			r.ConfirmedKeys = append(r.ConfirmedKeys, v.Key)
		}
		return r, nil
	}, handle: func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
		close(entered)
		<-release
		return cleanupTestDecision(f.Record, WriteBehindPause), nil
	}}
	s := openCleanupTestStore(t, t.TempDir(), false, true, target)
	defer close(release)
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "blocked", 1, false, &cleanupTestProbe{})))
	_, e := s.ForceSyncBatch(context.Background(), 16)
	require.Error(t, e)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("真实回调未进入")
	}
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "healthy", 1, false, &cleanupTestProbe{})))
	result, e := s.ForceSyncBatch(context.Background(), 16)
	require.NoError(t, e)
	require.Equal(t, 1, result.Confirmed)
	expireTerminalTestRecords(t, s)
	compactTerminalTestCycle(t, s, 16)
	usage, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Zero(t, usage.TerminalRecords, "无关在途回调不得让健康成功历史永久堆积")
	require.Equal(t, int64(1), usage.UnresolvedRecords)
	m, e := s.WriteBehindDispositionMetrics()
	require.NoError(t, e)
	require.Equal(t, 1, m.HandlerActive)
}

// TestTerminalNativeGCIsExplicitAndBounded 真实Badger上显式有限原生GC不能删除仍存活的业务副本及未解决原件。
func TestTerminalNativeGCIsExplicitAndBounded(t *testing.T) {
	s := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "pending", 1, false, &cleanupTestProbe{})))
	admin := &ReliableWriteStoreAdmin[cleanupTestRecord]{db: s.db}
	result, e := admin.ReclaimStorage(context.Background(), 2)
	require.NoError(t, e)
	require.LessOrEqual(t, result.Attempts, 2)
	_, e = admin.ReclaimStorage(context.Background(), 0)
	require.ErrorIs(t, e, ErrInvalidSyncLimit)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, e = admin.ReclaimStorage(canceled, 1)
	require.ErrorIs(t, e, context.Canceled)
	require.Zero(t, result.Attempts)
	v, e := s.GetLocal(context.Background(), "pending")
	require.NoError(t, e)
	require.NotNil(t, v)
	r, _, e := s.ListWriteBehindFailures("", 10)
	require.NoError(t, e)
	require.Len(t, r, 1)
	require.NotNil(t, r[0].Snapshot.Item)
	require.Equal(t, 1, s.Metrics().Pending)
}

// TestTerminalAutomaticMaintenanceWithoutNewSave自动worker在没有新请求时继续回收安全终态，不重投已确认项。
func TestTerminalAutomaticMaintenanceWithoutNewSave(t *testing.T) {
	target := &cleanupTestTarget{}
	s := openCleanupTestStore(t, t.TempDir(), true, true, target)
	for i := 0; i < 32; i++ {
		require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprint(i), 1, false, &cleanupTestProbe{})))
	}
	require.Eventually(t, func() bool { return s.Metrics().Pending == 0 }, 3*time.Second, time.Millisecond)
	s.db.syncExecMu.Lock()
	expireTerminalTestRecords(t, s)
	s.db.syncExecMu.Unlock()
	require.Eventually(t, func() bool {
		u, e := s.WriteBehindHistoryUsage(context.Background())
		return e == nil && u.TerminalRecords == 0
	}, 3*time.Second, time.Millisecond)
	require.Zero(t, s.Metrics().Pending)
	require.Equal(t, uint64(32), s.Metrics().Sync.SyncedItems)
	u, e := s.WriteBehindHistoryUsage(context.Background())
	require.NoError(t, e)
	require.Equal(t, int64(32), u.BusinessRecords, "false hook的业务副本仍保留")
}

// TestTerminalReclaimedDecisionCannotConfirmNewVersion已回收旧身份不保留无限回执，但必须拒绝它并保留后来新版本pending。
func TestTerminalReclaimedDecisionCannotConfirmNewVersion(t *testing.T) {
	s := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "same", 1, false, &cleanupTestProbe{})))
	_, e := s.ForceSyncAll(context.Background())
	require.NoError(t, e)
	r, _, e := s.ListWriteBehindFailures("", 100)
	require.NoError(t, e)
	require.Len(t, r, 1)
	d := cleanupTestDecision(r[0], WriteBehindConfirm)
	d.Outcome = WriteBehindCommitted
	require.NoError(t, s.ApplyWriteBehindDecision(context.Background(), d), "诊断窗口内的同身份确认仍幂等")
	expireTerminalTestRecords(t, s)
	compactTerminalTestCycle(t, s, 8)
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "same", 2, false, &cleanupTestProbe{})))
	require.Error(t, s.ApplyWriteBehindDecision(context.Background(), d), "不能为无限幂等留下每版本永续墓碑，也不能猜成新版本成功")
	require.Equal(t, 1, s.Metrics().Pending)
	v, e := s.GetLocal(context.Background(), "same")
	require.NoError(t, e)
	require.Equal(t, 2, v.Revision)
}

// TestTerminalSweepCannotChaseNewEntriesForever在每页后持续追加新成功项，已到期的旧项不能因游标追着新项而永久跳过。
func TestTerminalSweepCannotChaseNewEntriesForever(t *testing.T) {
	s := openCleanupTestStore(t, t.TempDir(), false, true, &cleanupTestTarget{})
	require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, "due", 1, false, &cleanupTestProbe{})))
	_, e := s.ForceSyncAll(context.Background())
	require.NoError(t, e)
	records, _, e := s.ListWriteBehindFailures("", 100)
	require.NoError(t, e)
	require.Len(t, records, 1)
	original := records[0].ID
	_, e = s.CompactWriteBehindHistory(context.Background(), 1)
	require.NoError(t, e)
	expireTerminalTestRecords(t, s)
	for i := 0; i < 20; i++ {
		require.NoError(t, s.Save(context.Background(), newCleanupTestRecord(t, fmt.Sprintf("new-%d", i), 1, false, &cleanupTestProbe{})))
		_, e = s.ForceSyncAll(context.Background())
		require.NoError(t, e)
		_, e = s.CompactWriteBehindHistory(context.Background(), 1)
		require.NoError(t, e)
	}
	require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
		_, e := txn.Get([]byte(s.db.dispositionPrefix() + "r:" + original))
		require.ErrorIs(t, e, badger.ErrKeyNotFound, "连续新项不能让已到期旧历史永远无法回收")
		return nil
	}))
}
