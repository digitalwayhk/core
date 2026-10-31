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

// TestOrderWriteBehindAdmissionAndNotification 验证准入失败不接触远端，提交成功才唤醒发布。
func TestOrderWriteBehindAdmissionAndNotification(t *testing.T) {
	remote := &recordingRemoteOrderStore{}
	blocked := errors.New("旧 Boot 已隔离")
	notifications := 0
	target := OrderWriteBehindTarget{Remote: remote, BeforeSync: func() error { return blocked }, AfterSync: func() { notifications++ }}
	items := []*nosql.SyncQueueItem[models.Order]{{Key: "order:1", Item: models.NewOrder(), Op: nosql.OpInsert}}
	result, err := target.SyncBatch(context.Background(), items)
	require.ErrorIs(t, err, blocked)
	require.Empty(t, result.ConfirmedKeys)
	require.Zero(t, remote.calls)
	target.BeforeSync = func() error { return nil }
	remote.err = errors.New("提交失败")
	_, err = target.SyncBatch(context.Background(), items)
	require.Error(t, err)
	require.Zero(t, notifications)
	remote.err = nil
	_, err = target.SyncBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 1, notifications)
}
