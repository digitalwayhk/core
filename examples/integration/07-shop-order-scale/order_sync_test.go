// Package shoporderscale 验证 07 单进程本地 pending 与远程同步异常恢复。
package shoporderscale

import (
	"context"
	"errors"
	"testing"
	"time"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestUATPendingSurvivesRemoteFailure 验证远程失败不会丢失本地可靠订单事实。
func TestUATPendingSurvivesRemoteFailure(t *testing.T) {
	runtime := newIntegrationOrderRuntime(t, failingRemote{})
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := "pending-failure-uat-" + time.Now().Format("150405.000000000")
	ids := newBenchmarkIDFactory(24)
	_, err := (orderbusiness.LocalOrderWriter{Store: runtime}).Accept(context.Background(), orderbusiness.CreateOrderCommand{
		OrderID:            ids.NewID(),
		UserID:             150000 + unique,
		SupplierID:         250000 + unique,
		ProductID:          350000 + unique,
		RequestID:          requestID,
		RequestFingerprint: requestID,
		UnitPrice:          decimal.NewFromInt(8),
		Quantity:           2,
		TraceID:            "trace-pending-failure",
		ServiceName:        "shop-order",
		ServiceInstanceID:  "order-a",
	})
	require.NoError(t, err)

	syncer := orderbusiness.RemoteOrderSyncer{Store: runtime}
	_, err = syncer.DrainOnce(context.Background(), 10000)
	require.Error(t, err)
	pending, err := runtime.FindLocalByRequest(context.Background(), 150000+unique, requestID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

type failingRemote struct{}

func (failingRemote) UpsertBatch(_ context.Context, _ []*ordermodels.Order) ([]*ordermodels.Order, error) {
	return nil, errors.New("远程库暂不可用")
}
