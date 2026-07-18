// 本文件验证 07 订单服务本地可靠写入与远程同步器的失败重试闭环。
package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestOrderSyncerRetriesRemoteFailure 验证远程失败时 pending 保留，恢复后可同步成功。
func TestOrderSyncerRetriesRemoteFailure(t *testing.T) {
	require.NoError(t, models.EnsureStorage())
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := fmt.Sprintf("syncer-retry-request-%d", unique)

	command := CreateOrderCommand{
		OrderID:            740000 + unique,
		UserID:             140000 + unique,
		RequestID:          requestID,
		RequestFingerprint: "fingerprint-" + requestID,
		SupplierID:         240000 + unique,
		ProductID:          340000 + unique,
		Quantity:           2,
		UnitPrice:          decimal.NewFromInt(9),
		TraceID:            "trace-syncer-retry",
		ServiceName:        "shop-order",
		ServiceInstanceID:  "order-syncer-a",
	}

	writer := LocalOrderWriter{}
	orderID, err := writer.Accept(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, command.OrderID, orderID)

	remote := &retryRemoteStore{fail: true}
	syncer := RemoteOrderSyncer{Remote: remote}
	require.NoError(t, syncer.DrainOnce(context.Background(), 10000))

	pending, err := models.FindLocalPendingByRequest(command.UserID, command.RequestID)
	require.NoError(t, err)
	require.Equal(t, models.PendingStatusFailed, pending.SyncStatus)
	require.Equal(t, 1, pending.RetryCount)

	remote.fail = false
	remote.calls = 0
	remote.seen = make(map[uint]bool)
	require.NoError(t, syncer.DrainOnce(context.Background(), 10000))
	require.GreaterOrEqual(t, remote.calls, 1)
	require.True(t, remote.seen[command.OrderID])

	pending, err = models.FindLocalPendingByRequest(command.UserID, command.RequestID)
	require.NoError(t, err)
	require.Equal(t, models.PendingStatusSynced, pending.SyncStatus)
	pendingItems, err := models.PendingLocalOrders(10000)
	require.NoError(t, err)
	for _, item := range pendingItems {
		require.NotEqual(t, command.RequestID, item.RequestID)
	}

	var stored *models.Order
	require.NoError(t, models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		stored, err = models.FindRemoteOrderByIdempotencyWith(action, command.UserID, command.RequestID)
		return err
	}))
	require.Equal(t, command.OrderID, stored.ID)
}

type retryRemoteStore struct {
	fail  bool
	calls int
	seen  map[uint]bool
}

func (s *retryRemoteStore) Upsert(ctx context.Context, order *models.Order) (*models.Order, error) {
	if s.seen == nil {
		s.seen = make(map[uint]bool)
	}
	s.calls++
	s.seen[order.ID] = true
	if s.fail {
		return nil, errors.New("remote temporarily unavailable")
	}
	var stored *models.Order
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		stored, err = models.UpsertRemoteOrderWith(action, order)
		return err
	})
	return stored, err
}
