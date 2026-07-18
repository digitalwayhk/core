// 本文件验证 07 订单服务本地可靠写入与远程同步器的失败重试闭环。
package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/yitter/idgenerator-go/idgen"
)

// TestOrderSyncerRetriesRemoteFailure 验证远程失败时 Badger 本地订单保留，恢复后可同步成功。
func TestOrderSyncerRetriesRemoteFailure(t *testing.T) {
	t.Setenv("SHOP_LOCAL_PENDING_DIR", t.TempDir())
	require.NoError(t, models.StartOrderWriteStore())
	t.Cleanup(func() { require.NoError(t, models.StopOrderWriteStore()) })
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := fmt.Sprintf("syncer-retry-request-%d", unique)
	ids := newBusinessTestIDFactory(25)

	command := CreateOrderCommand{
		OrderID:            ids.NewID(),
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
	require.Error(t, syncer.DrainOnce(context.Background(), 10000))

	pending, err := models.FindLocalOrderByRequest(command.UserID, command.RequestID)
	require.NoError(t, err)
	require.NotNil(t, pending)

	remote.fail = false
	remote.calls = 0
	remote.seen = make(map[uint]bool)
	require.NoError(t, syncer.DrainOnce(context.Background(), 10000))
	require.GreaterOrEqual(t, remote.calls, 1)
	require.True(t, remote.seen[command.OrderID])

	pending, err = models.FindLocalOrderByRequest(command.UserID, command.RequestID)
	require.NoError(t, err)
	require.Nil(t, pending)
	pendingItems, err := models.PendingLocalOrders(10000)
	require.NoError(t, err)
	for _, item := range pendingItems {
		require.NotEqual(t, command.RequestID, item.RequestID)
	}

	require.Equal(t, command.OrderID, remote.stored.ID)
}

type businessTestIDFactory struct {
	worker idgen.ISnowWorker
}

func newBusinessTestIDFactory(machineID uint) businessTestIDFactory {
	return businessTestIDFactory{worker: utils.NewAlgorithmSnowFlake(machineID, 4)}
}

func (f businessTestIDFactory) NewID() uint { return uint(f.worker.NextId()) }

type retryRemoteStore struct {
	fail   bool
	calls  int
	seen   map[uint]bool
	stored *models.Order
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
	s.stored = order
	return order, nil
}
