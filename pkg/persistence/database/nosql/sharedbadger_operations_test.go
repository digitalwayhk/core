// Package nosql 验证 Badger 可靠 Save/Delete 原语的事务顺序、tombstone 和磁盘指标。
package nosql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dgraph-io/badger/v3"
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
