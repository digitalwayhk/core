// Package business 验证 07 订单写回目标的部分成功确认语义。
package business

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/stretchr/testify/require"
)

type failNthRemoteOrderStore struct {
	calls  int
	failAt int
}

func (store *failNthRemoteOrderStore) Upsert(_ context.Context, order *models.Order) (*models.Order, error) {
	store.calls++
	if store.calls == store.failAt {
		return nil, errors.New("remote failed")
	}
	return order, nil
}

// TestOrderWriteBehindTargetConfirmsOrdersBeforeFailure 验证批次中途失败前已写入订单仍返回确认 key。
func TestOrderWriteBehindTargetConfirmsOrdersBeforeFailure(t *testing.T) {
	target := OrderWriteBehindTarget{Remote: &failNthRemoteOrderStore{failAt: 2}}
	first := models.NewOrder()
	second := models.NewOrder()
	items := []*nosql.SyncQueueItem[models.Order]{
		{Key: "order:1", Item: first, Op: nosql.OpInsert},
		{Key: "order:2", Item: second, Op: nosql.OpInsert},
	}

	result, err := target.SyncBatch(context.Background(), items)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, []string{"order:1"}, result.ConfirmedKeys)
}
