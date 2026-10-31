// Package nosql 验证 Badger 可靠 Save/Delete 原语的事务顺序、tombstone 和磁盘指标。
package nosql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

type largeOperationModel struct {
	*entity.Model
	Key     string `json:"key"`
	Payload string `json:"payload"`
}

func newLargeOperationModel(key string) *largeOperationModel {
	return &largeOperationModel{Model: entity.NewModel(), Key: key, Payload: strings.Repeat("x", 512<<10)}
}

func (m *largeOperationModel) NewModel() {
	if m.Model == nil {
		m.Model = entity.NewModel()
	}
}

func (m *largeOperationModel) GetHash() string { return m.Key }

type confirmAllFundTarget struct{}

func (confirmAllFundTarget) SyncBatch(_ context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil {
			keys = append(keys, item.Key)
		}
	}
	return &WriteBehindResult{ConfirmedKeys: keys}, nil
}

func newOperationsTestDB(t *testing.T) *PrefixedBadgerDB[testFund] {
	t.Helper()
	path := t.TempDir()
	config := DefaultProductionConfig(path)
	config.AutoSync = false
	db, err := NewSharedBadgerDB[testFund](path, config)
	require.NoError(t, err)
	require.NoError(t, db.UseWriteBehind(confirmAllFundTarget{}))
	t.Cleanup(func() {
		_ = db.Close()
		_ = CloseSharedManager(path)
	})
	return db
}

func TestApplyWriteOperationsPreservesSameKeyOrder(t *testing.T) {
	db := newOperationsTestDB(t)
	item := newFund("ordered", "HK", 10)
	updated := newFund("ordered", "HK", 20)

	result, err := db.ApplyWriteOperations([]WriteOperation[testFund]{
		{Type: WriteOperationSave, Item: item},
		{Type: WriteOperationSave, Item: updated},
		{Type: WriteOperationDelete, Item: updated},
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.Committed)
	_, err = db.Get(updated.GetHash())
	require.ErrorIs(t, err, badger.ErrKeyNotFound)
	require.Equal(t, 1, db.GetCachedPendingSyncCount())
	wrapper, err := db.getWrapper(db.generateKey(updated))
	require.NoError(t, err)
	require.Equal(t, OpDelete, wrapper.Op)
	require.True(t, wrapper.IsDeleted)
}

func TestReliableSaveChoosesInsertThenUpdate(t *testing.T) {
	db := newOperationsTestDB(t)
	first := newFund("upsert", "HK", 1)
	_, err := db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: first}})
	require.NoError(t, err)
	wrapper, err := db.getWrapper(db.generateKey(first))
	require.NoError(t, err)
	require.Equal(t, OpInsert, wrapper.Op)

	updated := newFund("upsert", "HK", 2)
	_, err = db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: updated}})
	require.NoError(t, err)
	wrapper, err = db.getWrapper(db.generateKey(updated))
	require.NoError(t, err)
	require.Equal(t, OpUpdate, wrapper.Op)
	require.Equal(t, float64(2), wrapper.Item.Balance)
}

func TestReliableDeleteIsIdempotent(t *testing.T) {
	db := newOperationsTestDB(t)
	item := newFund("missing", "HK", 1)

	result, err := db.ApplyWriteOperations([]WriteOperation[testFund]{
		{Type: WriteOperationDelete, Item: item},
		{Type: WriteOperationDelete, Item: item},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Committed)
	require.Equal(t, 0, db.GetCachedPendingSyncCount())
}

func TestReliableSaveRejectsDeletedKey(t *testing.T) {
	db := newOperationsTestDB(t)
	item := newFund("deleted", "HK", 1)
	_, err := db.ApplyWriteOperations([]WriteOperation[testFund]{
		{Type: WriteOperationSave, Item: item},
		{Type: WriteOperationDelete, Item: item},
	})
	require.NoError(t, err)

	_, err = db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: item}})
	require.ErrorIs(t, err, ErrWriteConflictDeleted)
}

func TestBadgerStorageSizeUsesNativeMetrics(t *testing.T) {
	db := newOperationsTestDB(t)
	_, err := db.ApplyWriteOperations([]WriteOperation[testFund]{{Type: WriteOperationSave, Item: newFund("size", "HK", 1)}})
	require.NoError(t, err)

	size := db.StorageSize()
	require.GreaterOrEqual(t, size.LSMBytes, int64(0))
	require.GreaterOrEqual(t, size.VLogBytes, int64(0))
}

func TestScanMissingNarrowPrefixDoesNotPrefetchUnrelatedValues(t *testing.T) {
	db := newTestDBLocal(t)
	items := make([]*testFund, 1024)
	for index := range items {
		items[index] = newFund(fmt.Sprintf("scan-%04d", index), "HK", 1)
	}
	require.NoError(t, db.BatchInsert(items))

	allocations := testing.AllocsPerRun(5, func() {
		found, err := db.Scan("scan-0000-not-found", 1)
		require.NoError(t, err)
		require.Empty(t, found)
	})
	require.Less(t, allocations, float64(200), "空前缀扫描不应预取相邻 key 的 value")
}

func TestApplyWriteOperationsReturnsCommittedPrefixWhenLaterBatchFails(t *testing.T) {
	db := newOperationsTestDB(t)
	operations := make([]WriteOperation[testFund], 0, localWriteTransactionMaxOperations+1)
	for index := 0; index < localWriteTransactionMaxOperations; index++ {
		operations = append(operations, WriteOperation[testFund]{
			Type: WriteOperationSave,
			Item: newFund(fmt.Sprintf("batch-%d", index), "HK", 1),
		})
	}
	operations = append(operations, WriteOperation[testFund]{Type: 0, Item: newFund("invalid", "HK", 1)})

	result, err := db.ApplyWriteOperations(operations)
	require.ErrorIs(t, err, ErrInvalidWriteOperation)
	require.Equal(t, localWriteTransactionMaxOperations, result.Committed)
	first, err := db.Get("batch-0:HK")
	require.NoError(t, err)
	require.NotNil(t, first)
	_, err = db.Get("invalid:HK")
	require.ErrorIs(t, err, badger.ErrKeyNotFound)
}

func TestApplyWriteOperationsSplitsTransactionTooBig(t *testing.T) {
	path := t.TempDir()
	config := DefaultProductionConfig(path)
	config.AutoSync = false
	config.ValueThreshold = 1 << 20
	db, err := NewSharedBadgerDB[largeOperationModel](path, config)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_ = CloseSharedManager(path)
	})
	operations := make([]WriteOperation[largeOperationModel], 0, 8)
	for index := range 8 {
		operations = append(operations, WriteOperation[largeOperationModel]{
			Type: WriteOperationSave,
			Item: newLargeOperationModel(fmt.Sprintf("large-%d", index)),
		})
	}

	result, err := db.ApplyWriteOperations(operations)
	require.NoError(t, err)
	require.Equal(t, 8, result.Committed)
}

// groupOperationsStore 使用已有合成模型，保持key稳定而允许组身份变化，所有测试仍可靠落盘。
func groupOperationsStore(t *testing.T, path string, inlineLarge bool) *ReliableWriteStore[confirmationLoadRecord] {
	t.Helper()
	cfg := DefaultProductionConfig(path)
	cfg.EnableLogger = false
	cfg.AutoSync = false
	if inlineLarge {
		cfg.ValueThreshold = 1 << 20
	}
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "group-operations", MaxAttempts: 3, MinRetryInterval: time.Second, MaxRetryInterval: 10 * time.Second, HandlerMaxAttempts: 2, HandlerTimeout: time.Second, HandlerWorkers: 1, ScanLimit: 128}
	store, _, err := NewReliableWriteStore[confirmationLoadRecord](ServiceIdentity{ServiceName: "group-operations"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg, Batch: BatchCommitConfig{MaxBatch: 128, CollectBacklog: 1, CollectWindow: time.Millisecond, QueueCapacity: 1024}})
	require.NoError(t, err)
	a := &confirmationLoadTarget{}
	require.NoError(t, store.UseWriteBehindWithFailureHandler(a, a))
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	return store
}

func groupOperationRecord(key string, group int, payload int) *confirmationLoadRecord {
	return &confirmationLoadRecord{Model: entity.NewModel(), Key: key, Group: group, Payload: []byte(strings.Repeat("x", payload))}
}

func currentGroupRecord(t *testing.T, store *ReliableWriteStore[confirmationLoadRecord], key string) WriteBehindFailureRecord[confirmationLoadRecord] {
	t.Helper()
	var r WriteBehindFailureRecord[confirmationLoadRecord]
	require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
		var id string
		if err := dispositionRead(txn, store.db.dispositionPrefix()+"key:"+dispositionHash(store.db.prefix+key), &id); err != nil {
			return err
		}
		return dispositionRead(txn, store.db.dispositionPrefix()+"r:"+id, &r)
	}))
	return r
}

func requireDispositionGroup(t *testing.T, db *PrefixedBadgerDB[confirmationLoadRecord], group int, exists bool) {
	t.Helper()
	err := db.manager.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(db.dispositionPrefix() + "g:" + dispositionHash(fmt.Sprint("group:", group))))
		return err
	})
	if exists {
		require.NoError(t, err)
	} else {
		require.ErrorIs(t, err, badger.ErrKeyNotFound)
	}
}

// TestDispositionSameGroupReplacementSkipsEmptyChecks 固定同组替换的无效核验，原历史、原件和新队列项必须保留。
func TestDispositionSameGroupReplacementSkipsEmptyChecks(t *testing.T) {
	for _, count := range []int{1, 64} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := groupOperationsStore(t, t.TempDir(), false)
			items := make([]*confirmationLoadRecord, count)
			for i := range items {
				items[i] = groupOperationRecord(fmt.Sprint(i), 1, 10)
			}
			result, err := store.SaveBatch(context.Background(), items)
			require.NoError(t, err)
			require.Equal(t, count, result.Committed)
			old := currentGroupRecord(t, store, "0")
			checks := 0
			store.db.disposition.groupCheckObserver = func(*badger.Txn, badger.IteratorOptions) { checks++ }
			for i := range items {
				items[i] = groupOperationRecord(fmt.Sprint(i), 1, 11)
			}
			result, err = store.SaveBatch(context.Background(), items)
			require.NoError(t, err)
			require.Equal(t, count, result.Committed)
			require.Zero(t, checks, "同事务新版本仍在相同实际组，不应核验临时空组")
			next := currentGroupRecord(t, store, "0")
			require.Equal(t, old.ID, next.PreviousID)
			require.NotEqual(t, old.ID, next.ID)
			require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
				var archived WriteBehindFailureRecord[confirmationLoadRecord]
				if e := dispositionRead(txn, store.db.dispositionPrefix()+"r:"+old.ID, &archived); e != nil {
					return e
				}
				require.Equal(t, WriteBehindSuperseded, archived.State)
				require.Len(t, archived.Snapshot.Item.Payload, 10)
				_, e := txn.Get([]byte(store.db.dispositionQueueKey(&next)))
				return e
			}))
			requireDispositionGroup(t, store.db, 1, true)
			require.Equal(t, count, store.Metrics().Pending)
		})
	}
}

// TestDispositionCrossGroupReplacementDefersAndBoundsChecks 固定批量跨组只核验一次，并观察真实传入Badger的组前缀。
func TestDispositionCrossGroupReplacementDefersAndBoundsChecks(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	items := make([]*confirmationLoadRecord, 32)
	for i := range items {
		items[i] = groupOperationRecord(fmt.Sprint(i), 1, 10)
	}
	_, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	var prefixes []string
	store.db.disposition.groupCheckObserver = func(_ *badger.Txn, options badger.IteratorOptions) {
		prefixes = append(prefixes, string(options.Prefix))
		require.False(t, options.PrefetchValues)
	}
	for i := range items {
		items[i] = groupOperationRecord(fmt.Sprint(i), 2, 11)
	}
	_, err = store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, []string{store.db.dispositionPrefix() + "q:" + dispositionHash("group:1") + ":"}, prefixes)
	requireDispositionGroup(t, store.db, 1, false)
	requireDispositionGroup(t, store.db, 2, true)
}

// TestDispositionFinalWriteSetRejoinsGroup 覆盖同事务删除旧队列项再加入原组，不能复用缺失后续pendingWrites的迭代器。
func TestDispositionFinalWriteSetRejoinsGroup(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	require.NoError(t, store.Save(context.Background(), groupOperationRecord("same", 1, 10)))
	result, err := store.SaveBatch(context.Background(), []*confirmationLoadRecord{groupOperationRecord("same", 2, 11), groupOperationRecord("same", 1, 12)})
	require.NoError(t, err)
	require.Equal(t, 2, result.Committed)
	requireDispositionGroup(t, store.db, 1, true)
	requireDispositionGroup(t, store.db, 2, false)
	resultSync, err := store.ForceSyncAll(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, resultSync.Confirmed)
	requireDispositionGroup(t, store.db, 1, false)
	require.Zero(t, store.Metrics().Pending)
}

// TestDispositionEmptyCheckBoundsDeletedNeighbours 目标空组后有其他组的历史/删除标记，底层Seek也必须受Prefix约束。
func TestDispositionEmptyCheckBoundsDeletedNeighbours(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	groups := []string{dispositionHash("group:1"), dispositionHash("group:2")}
	if groups[0] > groups[1] {
		groups[0], groups[1] = groups[1], groups[0]
	}
	base := store.db.dispositionPrefix()
	for round := range 3 {
		require.NoError(t, store.db.manager.db.Update(func(txn *badger.Txn) error {
			for i := range 1024 {
				if err := txn.Set([]byte(fmt.Sprintf("%sq:%s:%020d:history", base, groups[1], i)), []byte{byte(round)}); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	require.NoError(t, store.db.manager.db.Update(func(txn *badger.Txn) error {
		for i := range 1024 {
			if err := txn.Delete([]byte(fmt.Sprintf("%sq:%s:%020d:history", base, groups[1], i))); err != nil {
				return err
			}
		}
		return txn.Set([]byte(base+"g:"+groups[0]), nil)
	}))
	var observed []string
	store.db.disposition.groupCheckObserver = func(_ *badger.Txn, options badger.IteratorOptions) {
		observed = append(observed, string(options.Prefix))
	}
	require.NoError(t, store.db.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		return store.db.deleteEmptyDispositionGroups(txn, map[string]struct{}{groups[0]: {}})
	}))
	require.Equal(t, []string{base + "q:" + groups[0] + ":"}, observed)
	err := store.db.manager.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(base + "g:" + groups[0]))
		return err
	})
	require.ErrorIs(t, err, badger.ErrKeyNotFound)
}

// TestDispositionReplacementRollbackRetainsGroups 事务失败不能发布组清理或污染下一次实际事务的集合。
func TestDispositionReplacementRollbackRetainsGroups(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	_, err := store.SaveBatch(context.Background(), []*confirmationLoadRecord{groupOperationRecord("a", 1, 10), groupOperationRecord("b", 1, 10)})
	require.NoError(t, err)
	old := currentGroupRecord(t, store, "a")
	result, err := store.SaveBatch(context.Background(), []*confirmationLoadRecord{groupOperationRecord("a", 2, 11), nil})
	require.ErrorIs(t, err, ErrInvalidWriteOperation)
	require.Zero(t, result.Committed)
	require.Equal(t, old.ID, currentGroupRecord(t, store, "a").ID)
	require.NoError(t, store.Save(context.Background(), groupOperationRecord("b", 3, 12)))
	requireDispositionGroup(t, store.db, 1, true)
	requireDispositionGroup(t, store.db, 2, false)
	requireDispositionGroup(t, store.db, 3, true)
	require.Equal(t, 2, store.Metrics().Pending)
}

// TestDispositionGroupChecksStayWithinSplitTransactions 用真实TooBig和后半失败固定集合生命周期及已提交连续前缀。
func TestDispositionGroupChecksStayWithinSplitTransactions(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), true)
	items := make([]*confirmationLoadRecord, 6)
	for i := range items {
		items[i] = groupOperationRecord(fmt.Sprint(i), 1, 384<<10)
	}
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 6, result.Committed)
	checks := map[*badger.Txn][]string{}
	store.db.disposition.groupCheckObserver = func(txn *badger.Txn, options badger.IteratorOptions) {
		checks[txn] = append(checks[txn], string(options.Prefix))
	}
	for i := range items {
		items[i] = groupOperationRecord(fmt.Sprint(i), 2, (384<<10)+1)
	}
	result, err = store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 6, result.Committed)
	require.Positive(t, store.Metrics().Sync.ForegroundLocal.TooBig)
	require.Greater(t, len(checks), 1, "实际拆成多个提交事务，每个事务独立收集")
	for _, prefixes := range checks {
		require.Equal(t, []string{store.db.dispositionPrefix() + "q:" + dispositionHash("group:1") + ":"}, prefixes)
	}
	requireDispositionGroup(t, store.db, 1, false)
	requireDispositionGroup(t, store.db, 2, true)
	// 前面实际提交的子块保留，遇到nil的后块回滚，不能报告整组成功。
	items = append(items, nil)
	for i := range 6 {
		items[i] = groupOperationRecord(fmt.Sprint(i), 3, (384<<10)+2)
	}
	result, err = store.SaveBatch(context.Background(), items)
	require.ErrorIs(t, err, ErrInvalidWriteOperation)
	require.Greater(t, result.Committed, 0)
	require.Less(t, result.Committed, 6)
	for i := range 6 {
		expected := 2
		if i < result.Committed {
			expected = 3
		}
		require.Equal(t, fmt.Sprint(expected), currentGroupRecord(t, store, fmt.Sprint(i)).Group)
	}
	requireDispositionGroup(t, store.db, 2, true)
	requireDispositionGroup(t, store.db, 3, true)
	require.Equal(t, 6, store.Metrics().Pending)
}

// TestDispositionReplacementRetainsAttemptedVersions 在途、失败或已有尝试的版本仍承担恢复责任，不能被新值替代。
func TestDispositionReplacementRetainsAttemptedVersions(t *testing.T) {
	for _, state := range []WriteBehindFailureState{WriteBehindInFlight, WriteBehindUnknown, WriteBehindPaused, WriteBehindRetrying, WriteBehindReady} {
		t.Run(string(state), func(t *testing.T) {
			store := groupOperationsStore(t, t.TempDir(), false)
			require.NoError(t, store.Save(context.Background(), groupOperationRecord("same", 1, 10)))
			old := currentGroupRecord(t, store, "same")
			old.State, old.Attempts = state, 1
			require.NoError(t, store.db.updateWriteBehindTransaction(func(txn *badger.Txn) error { return store.db.saveDisposition(txn, &old) }))
			require.NoError(t, store.Save(context.Background(), groupOperationRecord("same", 2, 11)))
			require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
				var retained WriteBehindFailureRecord[confirmationLoadRecord]
				if e := dispositionRead(txn, store.db.dispositionPrefix()+"r:"+old.ID, &retained); e != nil {
					return e
				}
				require.Equal(t, state, retained.State)
				require.Equal(t, 1, retained.Attempts)
				require.Equal(t, old.Fingerprint, retained.Fingerprint)
				require.Len(t, retained.Snapshot.Item.Payload, 10)
				return nil
			}))
			requireDispositionGroup(t, store.db, 1, true)
			requireDispositionGroup(t, store.db, 2, true)
			if state == WriteBehindInFlight {
				confirmed, _, e := store.db.commitDispositionResultBlock([]WriteBehindFailureRecord[confirmationLoadRecord]{old}, map[string]bool{old.Snapshot.Key: true}, nil)
				require.NoError(t, e)
				require.Zero(t, confirmed, "旧target成功也不能确认新版本")
				require.Equal(t, 1, store.Metrics().Pending)
				current, e := store.GetLocal(context.Background(), "same")
				require.NoError(t, e)
				require.Len(t, current.Payload, 11)
			}
		})
	}
}

// TestDispositionLegacyWriteEntryPoints 保留Set/BatchInsert兼容入口，同时同组跳过、跨组在本事务末去重。
func TestDispositionLegacyWriteEntryPoints(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			store := groupOperationsStore(t, t.TempDir(), false)
			count := 1
			if batch {
				count = 32
			}
			write := func(group, payload int) {
				items := make([]*confirmationLoadRecord, count)
				for i := range items {
					items[i] = groupOperationRecord(fmt.Sprint(i), group, payload)
				}
				if batch {
					require.NoError(t, store.db.BatchInsert(items))
				} else {
					require.NoError(t, store.db.Set(items[0], 0))
				}
			}
			write(1, 10)
			checks := 0
			store.db.disposition.groupCheckObserver = func(*badger.Txn, badger.IteratorOptions) { checks++ }
			write(1, 11)
			require.Zero(t, checks)
			write(2, 12)
			require.Equal(t, 1, checks)
			requireDispositionGroup(t, store.db, 1, false)
			requireDispositionGroup(t, store.db, 2, true)
			require.Equal(t, count, store.Metrics().Pending)
		})
	}
}
