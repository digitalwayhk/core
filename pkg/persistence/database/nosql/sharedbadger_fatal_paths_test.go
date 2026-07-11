package nosql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

func fatalTestItems() []*SyncQueueItem[testLedger] {
	items := make([]*SyncQueueItem[testLedger], 0, 3)
	for i := 0; i < 3; i++ {
		ledger := newLedger("fatal_branch", string(rune('A'+i)), float64(i), "memory")
		items = append(items, &SyncQueueItem[testLedger]{Key: ledger.GetHash(), Item: ledger})
	}
	return items
}

func prepareMemoryRows(t *testing.T, action *memoryAction, items []*SyncQueueItem[testLedger]) {
	t.Helper()
	for _, item := range items {
		require.NoError(t, action.store(item.Item))
	}
}

func runBatchOperation(db *PrefixedBadgerDB[testLedger], action *memoryAction, operation string, items []*SyncQueueItem[testLedger]) {
	switch operation {
	case "insert":
		db.batchInsertWithErrorHandling(items, action)
	case "update":
		db.batchUpdateWithErrorHandling(items, action)
	case "delete":
		db.batchDeleteWithErrorHandling(items, action)
	}
}

func TestBatchFatalTransactionAndCommitDoNotFallback(t *testing.T) {
	fatalErrors := []struct {
		name string
		err  error
	}{
		{name: "context 取消", err: context.Canceled},
		{name: "连接不可用", err: errors.New("driver: bad connection")},
		{name: "事务已回滚", err: errors.New("transaction has already been rolled back")},
	}

	for _, phase := range []string{"transaction", "commit"} {
		for _, operation := range []string{"insert", "update", "delete"} {
			for _, fatal := range fatalErrors {
				t.Run(phase+"/"+operation+"/"+fatal.name, func(t *testing.T) {
					action := newMemoryAction()
					items := fatalTestItems()
					if operation != "insert" {
						prepareMemoryRows(t, action, items)
					}
					if phase == "transaction" {
						action.setTransactionError(fatal.err)
					} else {
						action.setCommitError(fatal.err)
					}
					db := newManualSyncDBWithConfig(t, newTestConfig(t.TempDir()), entity.NewModelList[testLedger](action))

					runBatchOperation(db, action, operation, items)

					wantCalls := 0
					if phase == "commit" {
						wantCalls = len(items)
					}
					require.Equal(t, wantCalls, action.operationCallCount(operation), "致命事务错误后不得进入逐条回退")
				})
			}
		}
	}
}

func TestFatalSecondaryActionStopsCurrentLoop(t *testing.T) {
	duplicateErr := errors.New("Duplicate entry")
	notFoundErr := errors.New("record not found")
	fatalErr := context.Canceled

	tests := []struct {
		name       string
		prepare    func(*memoryAction, []*SyncQueueItem[testLedger])
		run        func(*PrefixedBadgerDB[testLedger], *memoryAction, []*SyncQueueItem[testLedger])
		wantInsert int
		wantUpdate int
	}{
		{
			name: "批量插入冲突后更新致命失败",
			prepare: func(a *memoryAction, _ []*SyncQueueItem[testLedger]) {
				a.scriptOperation("insert", duplicateErr)
				a.scriptOperation("update", fatalErr)
			},
			run: func(db *PrefixedBadgerDB[testLedger], a *memoryAction, items []*SyncQueueItem[testLedger]) {
				db.batchInsertWithErrorHandling(items, a)
			},
			wantInsert: 1, wantUpdate: 1,
		},
		{
			name: "批量更新插入冲突后更新致命失败",
			prepare: func(a *memoryAction, _ []*SyncQueueItem[testLedger]) {
				a.scriptOperation("insert", duplicateErr)
				a.scriptOperation("update", fatalErr)
			},
			run: func(db *PrefixedBadgerDB[testLedger], a *memoryAction, items []*SyncQueueItem[testLedger]) {
				db.batchUpdateWithErrorHandling(items, a)
			},
			wantInsert: 1, wantUpdate: 1,
		},
		{
			name: "批量更新未找到后插入致命失败",
			prepare: func(a *memoryAction, items []*SyncQueueItem[testLedger]) {
				prepareMemoryRows(t, a, items)
				a.scriptOperation("update", notFoundErr)
				a.scriptOperation("insert", fatalErr)
			},
			run: func(db *PrefixedBadgerDB[testLedger], a *memoryAction, items []*SyncQueueItem[testLedger]) {
				db.batchUpdateWithErrorHandling(items, a)
			},
			wantInsert: 1, wantUpdate: 1,
		},
		{
			name: "逐条插入冲突后更新致命失败",
			prepare: func(a *memoryAction, _ []*SyncQueueItem[testLedger]) {
				a.scriptOperation("insert", duplicateErr)
				a.scriptOperation("update", fatalErr)
			},
			run: func(db *PrefixedBadgerDB[testLedger], _ *memoryAction, items []*SyncQueueItem[testLedger]) {
				db.insertItemsOneByOne(items)
			},
			wantInsert: 1, wantUpdate: 1,
		},
		{
			name: "逐条更新未找到后插入致命失败",
			prepare: func(a *memoryAction, items []*SyncQueueItem[testLedger]) {
				prepareMemoryRows(t, a, items)
				a.scriptOperation("update", notFoundErr)
				a.scriptOperation("insert", fatalErr)
			},
			run: func(db *PrefixedBadgerDB[testLedger], _ *memoryAction, items []*SyncQueueItem[testLedger]) {
				db.updateItemsOneByOne(items)
			},
			wantInsert: 1, wantUpdate: 1,
		},
		{
			name: "逐条更新重试更新致命失败",
			prepare: func(a *memoryAction, items []*SyncQueueItem[testLedger]) {
				prepareMemoryRows(t, a, items)
				a.scriptOperation("update", notFoundErr, fatalErr)
				a.scriptOperation("insert", duplicateErr)
			},
			run: func(db *PrefixedBadgerDB[testLedger], _ *memoryAction, items []*SyncQueueItem[testLedger]) {
				db.updateItemsOneByOne(items)
			},
			wantInsert: 1, wantUpdate: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action := newMemoryAction()
			items := fatalTestItems()[:2]
			test.prepare(action, items)
			db := newManualSyncDBWithConfig(t, newTestConfig(t.TempDir()), entity.NewModelList[testLedger](action))

			test.run(db, action, items)

			require.Equal(t, test.wantInsert, action.operationCallCount("insert"))
			require.Equal(t, test.wantUpdate, action.operationCallCount("update"))
		})
	}
}

func TestClosedInstanceStopsAllOneByOneFallbacks(t *testing.T) {
	for _, operation := range []string{"insert", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			action := newMemoryAction()
			items := fatalTestItems()[:1]
			prepareMemoryRows(t, action, items)
			config := newTestConfig(t.TempDir())
			db, err := NewSharedBadgerDB[testLedger](config.Path, config)
			require.NoError(t, err)
			db.syncDB = true
			db.syncList = entity.NewModelList[testLedger](action)
			require.NoError(t, db.Close())

			switch operation {
			case "insert":
				db.insertItemsOneByOne(items)
			case "update":
				db.updateItemsOneByOne(items)
			case "delete":
				db.deleteItemsOneByOne(items, action)
			}

			require.Zero(t, action.operationCallCount(operation))
		})
	}
}

func TestFatalExistsStopsBatchAndRollsBack(t *testing.T) {
	for _, operation := range []string{"insert", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			action := newMemoryAction()
			items := fatalTestItems()[:2]
			if operation != "insert" {
				for _, item := range items {
					original := newLedger(item.Item.Owner, item.Item.Code, -1, item.Item.DBName)
					require.NoError(t, action.store(original))
				}
			}
			action.scriptExists(
				memoryExistsResult{exists: operation != "insert"},
				memoryExistsResult{err: context.Canceled},
			)
			db := newManualSyncDBWithConfig(t, newTestConfig(t.TempDir()), entity.NewModelList[testLedger](action))

			successKeys := runBatchOperationWithResult(db, action, operation, items)

			require.Empty(t, successKeys, "Exists 致命错误后当前事务不得确认任何 key")
			require.Equal(t, 1, action.operationCallCount(operation), "Exists 致命错误后不得调用后续数据操作")
			stored, exists := memoryValueAs[testLedger](action, items[0].Item)
			if operation == "insert" {
				require.False(t, exists, "fatal 前暂存的 Insert 必须回滚")
			} else {
				require.True(t, exists)
				require.Equal(t, -1.0, stored.Amount, "fatal 前暂存的更新/删除不得提交")
			}
		})
	}
}

func runBatchOperationWithResult(db *PrefixedBadgerDB[testLedger], action *memoryAction, operation string, items []*SyncQueueItem[testLedger]) []string {
	switch operation {
	case "insert":
		return db.batchInsertWithErrorHandling(items, action)
	case "update":
		return db.batchUpdateWithErrorHandling(items, action)
	case "delete":
		return db.batchDeleteWithErrorHandling(items, action)
	default:
		return nil
	}
}

func TestFatalExistsStopsAllOneByOneFallbacks(t *testing.T) {
	for _, operation := range []string{"insert", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			action := newMemoryAction()
			items := fatalTestItems()[:2]
			prepareMemoryRows(t, action, items)
			action.scriptExists(memoryExistsResult{err: errors.New("invalid connection")})
			db := newManualSyncDBWithConfig(t, newTestConfig(t.TempDir()), entity.NewModelList[testLedger](action))

			var successKeys []string
			switch operation {
			case "insert":
				successKeys = db.insertItemsOneByOne(items)
			case "update":
				successKeys = db.updateItemsOneByOne(items)
			case "delete":
				successKeys = db.deleteItemsOneByOne(items, action)
			}

			require.Empty(t, successKeys)
			require.Zero(t, action.operationCallCount(operation), "Exists 致命错误后不得执行数据操作")
		})
	}
}

func TestNonFatalExistsErrorKeepsFallbackBehavior(t *testing.T) {
	action := newMemoryAction()
	action.scriptExists(memoryExistsResult{err: errors.New("临时查询失败")})
	db := newManualSyncDBWithConfig(t, newTestConfig(t.TempDir()), entity.NewModelList[testLedger](action))
	items := fatalTestItems()[:1]

	successKeys := db.batchInsertWithErrorHandling(items, action)

	require.Equal(t, []string{items[0].Key}, successKeys)
	require.Equal(t, 1, action.operationCallCount("insert"))
}

func TestMemoryActionStoresValueSnapshot(t *testing.T) {
	action := newMemoryAction()
	ledger := newLedger("snapshot", "BTC", 10, "memory")
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	wantCreatedAt := createdAt
	ledger.Model.Hashcode = "original-hash"
	ledger.Model.CreatedAt = &createdAt
	require.NoError(t, action.store(ledger))

	ledger.Amount = 99
	ledger.Model.Hashcode = "mutated-hash"
	mutatedCreatedAt := createdAt.Add(24 * time.Hour)
	*ledger.Model.CreatedAt = mutatedCreatedAt
	stored, exists := memoryValueAs[testLedger](action, ledger)

	require.True(t, exists)
	require.Equal(t, 10.0, stored.Amount)
	require.Equal(t, "original-hash", stored.Model.Hashcode)
	require.Equal(t, wantCreatedAt, *stored.Model.CreatedAt)
}
