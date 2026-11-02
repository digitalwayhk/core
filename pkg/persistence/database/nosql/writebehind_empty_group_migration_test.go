// 本文件用真实Badger与既有提交面验证旧终态跨块重核、组标记点查和事务边界。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// emptyGroupMigrationRecords通过正式Save/确认生成同一非空组的多版本终态，不伪造业务完成。
func emptyGroupMigrationRecords(t *testing.T, s *ReliableWriteStore[confirmationLoadRecord], count int) []string {
	t.Helper()
	for revision := 0; revision < 2; revision++ {
		items := make([]*confirmationLoadRecord, count)
		for i := range items {
			items[i] = groupOperationRecord(fmt.Sprint(i), 1, 32+revision)
		}
		result, err := s.SaveBatch(context.Background(), items)
		require.NoError(t, err)
		require.Equal(t, count, result.Committed)
	}
	_, err := s.ForceSyncAll(context.Background())
	require.NoError(t, err)
	require.Zero(t, s.Metrics().Pending)
	records, _, err := s.ListWriteBehindFailures("", 1000)
	require.NoError(t, err)
	require.Len(t, records, 2*count)
	ids := make([]string, 0, len(records))
	for _, r := range records {
		require.True(t, dispositionTerminal(r.State))
		require.Equal(t, "1", r.Group, "实际持久Group必须非空，不能用key fallback替代")
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids
}

// TestTerminalMigrationSkipsMissingGroupRepeatedSeek 固定同组终态跨6个8条事务块，缺标记不再反复创建迭代器。
func TestTerminalMigrationSkipsMissingGroupRepeatedSeek(t *testing.T) {
	for _, marker := range []bool{false, true} {
		t.Run(fmt.Sprint(marker), func(t *testing.T) {
			s := groupOperationsStore(t, t.TempDir(), false)
			ids := emptyGroupMigrationRecords(t, s, 24)
			requireDispositionGroup(t, s.db, 1, false)
			if marker {
				require.NoError(t, s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
					return txn.Set([]byte(s.db.dispositionPrefix()+"g:"+dispositionHash("group:1")), nil)
				}))
			}
			checks := 0
			s.db.disposition.groupCheckObserver = func(_ *badger.Txn, opts badger.IteratorOptions) {
				checks++
				require.Equal(t, s.db.dispositionPrefix()+"q:"+dispositionHash("group:1")+":", string(opts.Prefix))
			}
			for start := 0; start < len(ids); start += 8 {
				require.NoError(t, s.db.initializeTerminalRecordBlock(ids[start:start+8]))
			}
			want := 0
			if marker {
				want = 1
			}
			require.Equal(t, want, checks, "缺失或首块清掉g后，后续块不能重复Seek同组历史")
			requireDispositionGroup(t, s.db, 1, false)
			require.Zero(t, s.Metrics().Pending)
		})
	}
}

// TestTerminalMigrationKeepsLiveGroup 验证有标记的活跃/暂停/未知前序不能因终态导入而移除组或原件。
func TestTerminalMigrationKeepsLiveGroup(t *testing.T) {
	for _, state := range []WriteBehindFailureState{WriteBehindReady, WriteBehindPaused, WriteBehindUnknown} {
		t.Run(string(state), func(t *testing.T) {
			s := groupOperationsStore(t, t.TempDir(), false)
			ids := emptyGroupMigrationRecords(t, s, 8)
			require.NoError(t, s.Save(context.Background(), groupOperationRecord("unresolved", 1, 50)))
			r := currentGroupRecord(t, s, "unresolved")
			r.State = state
			r.Attempts = 2
			require.NoError(t, s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error { return s.db.saveDisposition(txn, &r) }))
			for start := 0; start < len(ids); start += 8 {
				require.NoError(t, s.db.initializeTerminalRecordBlock(ids[start:start+8]))
			}
			requireDispositionGroup(t, s.db, 1, true)
			after := currentGroupRecord(t, s, "unresolved")
			require.Equal(t, state, after.State)
			require.Equal(t, 2, after.Attempts)
			require.Equal(t, r.Fingerprint, after.Fingerprint)
			require.NotNil(t, after.Snapshot.Item)
			require.Equal(t, 1, s.Metrics().Pending)
		})
	}
}

// TestEmptyGroupSnapshotMissConflictsWithConcurrentMarker 验证真实快照缺g点读后另一事务写g，不能把冲突当成功。
func TestEmptyGroupSnapshotMissConflictsWithConcurrentMarker(t *testing.T) {
	s := groupOperationsStore(t, t.TempDir(), false)
	group := dispositionHash("group:1")
	base := s.db.dispositionPrefix()
	txn := s.db.manager.db.NewTransaction(true)
	defer txn.Discard()
	require.NoError(t, txn.Set([]byte(base+"test-progress"), []byte("not-yet-committed")))
	require.NoError(t, s.db.deleteEmptyDispositionGroups(txn, map[string]struct{}{group: {}}))
	require.NoError(t, s.db.manager.db.Update(func(other *badger.Txn) error {
		if err := other.Set([]byte(base+"g:"+group), nil); err != nil {
			return err
		}
		return other.Set([]byte(base+"q:"+group+":test"), []byte("new"))
	}))
	require.ErrorIs(t, txn.Commit(), badger.ErrConflict)
	requireDispositionGroup(t, s.db, 1, true)
	require.NoError(t, s.db.manager.db.View(func(view *badger.Txn) error {
		_, err := view.Get([]byte(base + "test-progress"))
		require.ErrorIs(t, err, badger.ErrKeyNotFound)
		_, err = view.Get([]byte(base + "q:" + group + ":test"))
		return err
	}))
}

// TestEmptyGroupMarkerFinalWritesAndRollback 验证本事务删除后重加、缺标记不修q、真实回滚均不泄漏判断。
func TestEmptyGroupMarkerFinalWritesAndRollback(t *testing.T) {
	s := groupOperationsStore(t, t.TempDir(), false)
	require.NoError(t, s.Save(context.Background(), groupOperationRecord("live", 1, 10)))
	r := currentGroupRecord(t, s, "live")
	g := dispositionHash("group:1")
	key := []byte(s.db.dispositionPrefix() + "g:" + g)
	groups := map[string]struct{}{g: {}}
	require.NoError(t, s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		if err := txn.Delete(key); err != nil {
			return err
		}
		if err := s.db.deleteEmptyDispositionGroups(txn, groups); err != nil {
			return err
		}
		if _, err := txn.Get([]byte(s.db.dispositionQueueKey(&r))); err != nil {
			return err
		}
		if err := txn.Set(key, nil); err != nil {
			return err
		}
		return s.db.deleteEmptyDispositionGroups(txn, groups)
	}))
	requireDispositionGroup(t, s.db, 1, true)
	injected := errors.New("测试真实事务回滚")
	err := s.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		if err := txn.Delete([]byte(s.db.dispositionQueueKey(&r))); err != nil {
			return err
		}
		if err := s.db.deleteEmptyDispositionGroups(txn, groups); err != nil {
			return err
		}
		if err := dispositionWrite(txn, s.db.dispositionPrefix()+"history-init", "must-not-publish"); err != nil {
			return err
		}
		return injected
	})
	require.ErrorIs(t, err, injected)
	requireDispositionGroup(t, s.db, 1, true)
	require.NoError(t, s.db.manager.db.View(func(txn *badger.Txn) error {
		var cursor string
		if err := dispositionRead(txn, s.db.dispositionPrefix()+"history-init", &cursor); err != nil {
			return err
		}
		require.NotEqual(t, "must-not-publish", cursor)
		_, err := txn.Get([]byte(s.db.dispositionQueueKey(&r)))
		return err
	}))
}

// TestEmptyGroupMarkerReadError 不得把已丢弃事务等真实读取错误伪装成“无标记”并成功。
func TestEmptyGroupMarkerReadError(t *testing.T) {
	s := groupOperationsStore(t, t.TempDir(), false)
	txn := s.db.manager.db.NewTransaction(true)
	txn.Discard()
	err := s.db.deleteEmptyDispositionGroups(txn, map[string]struct{}{dispositionHash("group:1"): {}})
	require.ErrorIs(t, err, badger.ErrDiscardedTxn)
}
