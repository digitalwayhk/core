// Package nosql 验证 ReliableWriteStore 的公开写入、读取、同步绑定、运维清理和关闭契约。
package nosql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/stretchr/testify/require"
)

func testReliableIdentity() ServiceIdentity {
	return ServiceIdentity{ServiceName: "order", DataCenterID: 2, MachineID: 7}
}

func testReliableConfig(t *testing.T) ReliableWriteStoreConfig {
	t.Helper()
	basePath := t.TempDir()
	badgerConfig := DefaultProductionConfig(basePath)
	badgerConfig.AutoSync = false
	return ReliableWriteStoreConfig{
		BasePath: basePath,
		Badger:   badgerConfig,
		Batch: BatchCommitConfig{
			MaxBatch:      8,
			CollectWindow: time.Millisecond,
			QueueCapacity: 32,
		},
		CloseTimeout: time.Second,
	}
}

func cleanupReliableStore(t *testing.T, store *ReliableWriteStore[testFund]) {
	t.Helper()
	if store == nil {
		return
	}
	path := store.config.BasePath
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		_ = CloseSharedManager(path)
	})
}

func TestReliableWriteStoreSaveDeleteAndClose(t *testing.T) {
	store, admin, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	require.NotNil(t, admin)
	path := store.config.BasePath
	t.Cleanup(func() { _ = CloseSharedManager(path) })
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))

	item := newFund("store-user", "HK", 1)
	require.NoError(t, store.Save(context.Background(), item))
	updated := newFund("store-user", "HK", 2)
	require.NoError(t, store.Save(context.Background(), updated))
	require.NoError(t, store.Delete(context.Background(), updated))
	_, err = store.GetLocal(context.Background(), item.GetHash())
	require.ErrorIs(t, err, badger.ErrKeyNotFound)

	err = store.Close(context.Background())
	var pendingErr *PendingSyncError
	require.ErrorAs(t, err, &pendingErr)
	require.ErrorIs(t, store.Save(context.Background(), newFund("closed", "HK", 1)), ErrWriteStoreClosed)
	require.Equal(t, err, store.Close(context.Background()))
}

func TestReliableWriteStoreRejectsWriteBeforeTargetBinding(t *testing.T) {
	store, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)

	err = store.Save(context.Background(), newFund("unbound", "HK", 1))
	require.ErrorIs(t, err, ErrWriteBehindNotBound)
}

func TestReliableWriteStoreAddDelegatesToSave(t *testing.T) {
	store, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))

	item := newFund("add", "HK", 1)
	require.NoError(t, store.Add(context.Background(), item))
	stored, err := store.GetLocal(context.Background(), item.GetHash())
	require.NoError(t, err)
	require.Equal(t, item.Balance, stored.Balance)
}

func TestReliableWriteStoreBatchReturnsCommittedPrefix(t *testing.T) {
	store, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))

	items := []*testFund{
		newFund("batch-a", "HK", 1),
		newFund("batch-b", "HK", 2),
		newFund("batch-c", "HK", 3),
	}
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, BatchWriteResult{Committed: 3}, result)
	result, err = store.DeleteBatch(context.Background(), items[:2])
	require.NoError(t, err)
	require.Equal(t, BatchWriteResult{Committed: 2}, result)
}

func TestReliableWriteStoreScanLocalHidesTombstones(t *testing.T) {
	store, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))

	item := newFund("scan", "HK", 1)
	require.NoError(t, store.Save(context.Background(), item))
	require.NoError(t, store.Delete(context.Background(), item))
	items, err := store.ScanLocal(context.Background(), LocalScanOptions{Prefix: "scan", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestReliableWriteStoreAdminPurgeRemovesPendingIndex(t *testing.T) {
	store, admin, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))

	item := newFund("purge", "HK", 1)
	require.NoError(t, store.Save(context.Background(), item))
	require.Equal(t, 1, store.Metrics().Pending)
	require.NoError(t, admin.PurgeLocal(context.Background(), item))
	require.Zero(t, store.Metrics().Pending)
	_, err = store.GetLocal(context.Background(), item.GetHash())
	require.ErrorIs(t, err, badger.ErrKeyNotFound)
}

func TestReliableWriteStoreRejectsSecondTargetBinding(t *testing.T) {
	store, _, err := NewReliableWriteStore[testFund](testReliableIdentity(), testReliableConfig(t))
	require.NoError(t, err)
	cleanupReliableStore(t, store)
	require.NoError(t, store.UseWriteBehind(confirmAllFundTarget{}))

	err = store.UseWriteBehind(confirmAllFundTarget{})
	require.True(t, errors.Is(err, ErrWriteBehindAlreadyBound))
}
