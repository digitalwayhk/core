// Package nosql 验证 SQLWriteBehindTarget 的操作分组和确认 key 行为。
package nosql

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSQLWriteBehindStore struct {
	upserts   []*testFund
	deletes   []*testFund
	deleteErr error
}

func (store *fakeSQLWriteBehindStore) UpsertBatch(_ context.Context, items []*testFund) ([]*testFund, error) {
	store.upserts = append(store.upserts, items...)
	return items, nil
}

func (store *fakeSQLWriteBehindStore) DeleteBatch(_ context.Context, items []*testFund) error {
	store.deletes = append(store.deletes, items...)
	return store.deleteErr
}

// TestSQLWriteBehindTargetConfirmsUpsertsBeforeDeleteFailure 验证删除失败时已提交 upsert 不会重复重试。
func TestSQLWriteBehindTargetConfirmsUpsertsBeforeDeleteFailure(t *testing.T) {
	store := &fakeSQLWriteBehindStore{deleteErr: errors.New("delete failed")}
	target := NewSQLWriteBehindTarget[testFund](store)
	items := []*SyncQueueItem[testFund]{
		{Key: "testFund:sql-upsert:spot", Item: newFund("sql-upsert", "spot", 1), Op: OpInsert},
		{Key: "testFund:sql-delete:spot", Item: newFund("sql-delete", "spot", 2), Op: OpDelete},
	}

	result, err := target.SyncBatch(context.Background(), items)
	require.Error(t, err)
	require.Equal(t, []string{"testFund:sql-upsert:spot"}, result.ConfirmedKeys)
}

// TestSQLWriteBehindTargetGroupsOperations 验证 insert/update 合并为 upsert，delete 单独删除。
func TestSQLWriteBehindTargetGroupsOperations(t *testing.T) {
	store := &fakeSQLWriteBehindStore{}
	target := NewSQLWriteBehindTarget[testFund](store)
	items := []*SyncQueueItem[testFund]{
		{Key: "testFund:sql-a:spot", Item: newFund("sql-a", "spot", 1), Op: OpInsert},
		{Key: "testFund:sql-b:spot", Item: newFund("sql-b", "spot", 2), Op: OpUpdate},
		{Key: "testFund:sql-c:spot", Item: newFund("sql-c", "spot", 3), Op: OpDelete},
	}

	result, err := target.SyncBatch(context.Background(), items)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"testFund:sql-a:spot", "testFund:sql-b:spot", "testFund:sql-c:spot"}, result.ConfirmedKeys)
	require.Len(t, store.upserts, 2)
	require.Len(t, store.deletes, 1)
}
