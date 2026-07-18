// Package nosql 验证 SQLWriteBehindTarget 的操作分组和确认 key 行为。
package nosql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSQLWriteBehindStore struct {
	upserts []*testFund
	deletes []*testFund
}

func (store *fakeSQLWriteBehindStore) UpsertBatch(_ context.Context, items []*testFund) ([]*testFund, error) {
	store.upserts = append(store.upserts, items...)
	return items, nil
}

func (store *fakeSQLWriteBehindStore) DeleteBatch(_ context.Context, items []*testFund) error {
	store.deletes = append(store.deletes, items...)
	return nil
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
