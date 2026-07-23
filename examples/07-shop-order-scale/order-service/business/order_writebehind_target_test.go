// Package business 验证 07 订单写回目标的远程原子批次语义。
package business

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/stretchr/testify/require"
)

type recordingRemoteOrderStore struct {
	calls  int
	orders []*models.Order
	err    error
}

func (store *recordingRemoteOrderStore) UpsertBatch(_ context.Context, orders []*models.Order) ([]*models.Order, error) {
	store.calls++
	store.orders = append(store.orders, orders...)
	return orders, store.err
}

// TestOrderWriteBehindTargetUsesOneRemoteBatch 验证一个 write-behind 批次只调用一次远程批量写入。
func TestOrderWriteBehindTargetUsesOneRemoteBatch(t *testing.T) {
	remote := &recordingRemoteOrderStore{}
	target := OrderWriteBehindTarget{Remote: remote}
	first := models.NewOrder()
	second := models.NewOrder()
	items := []*nosql.SyncQueueItem[models.Order]{
		{Key: "order:1", Item: first, Op: nosql.OpInsert},
		{Key: "order:2", Item: second, Op: nosql.OpInsert},
	}

	result, err := target.SyncBatch(context.Background(), items)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []string{"order:1", "order:2"}, result.ConfirmedKeys)
	require.Equal(t, 1, remote.calls)
	require.Equal(t, []*models.Order{first, second}, remote.orders)
}

// TestOrderWriteBehindTargetDoesNotConfirmFailedAtomicBatch 验证远程事务失败时整批保留在 Badger。
func TestOrderWriteBehindTargetDoesNotConfirmFailedAtomicBatch(t *testing.T) {
	remote := &recordingRemoteOrderStore{err: errors.New("remote failed")}
	target := OrderWriteBehindTarget{Remote: remote}
	items := []*nosql.SyncQueueItem[models.Order]{
		{Key: "order:1", Item: models.NewOrder(), Op: nosql.OpInsert},
		{Key: "order:2", Item: models.NewOrder(), Op: nosql.OpInsert},
	}

	result, err := target.SyncBatch(context.Background(), items)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.ConfirmedKeys)
	require.Equal(t, 1, remote.calls)
}
