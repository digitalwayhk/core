// Package shoporderscale 验证 07 单进程买家角色的订单闭环。
package shoporderscale

import (
	"context"
	"fmt"
	"testing"
	"time"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestUATBuyerOrderLifecycle 验证买家下单、同步、查询和支付的核心闭环。
func TestUATBuyerOrderLifecycle(t *testing.T) {
	requireOrderMySQL(t)
	runtime := newIntegrationOrderRuntime(t, nil)
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := fmt.Sprintf("buyer-uat-%d", unique)
	ids := newBenchmarkIDFactory(23)

	_, err := (orderbusiness.LocalOrderWriter{Store: runtime}).Accept(context.Background(), orderbusiness.CreateOrderCommand{
		OrderID:            ids.NewID(),
		UserID:             110000 + unique,
		RequestID:          requestID,
		RequestFingerprint: "fingerprint-" + requestID,
		SupplierID:         210000 + unique,
		ProductID:          310000 + unique,
		UnitPrice:          decimal.NewFromInt(12),
		Quantity:           2,
		TraceID:            "trace-" + requestID,
		ServiceName:        "shop-order",
		ServiceInstanceID:  "order-a",
	})
	require.NoError(t, err)

	_, err = (orderbusiness.RemoteOrderSyncer{Store: runtime}).DrainOnce(context.Background(), 10000)
	require.NoError(t, err)
	orders, _, err := orderbusiness.ListOrders(runtime, ordermodels.OrderQueryFilter{UserID: 110000 + unique}, 1, 20)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.Equal(t, ordermodels.OrderStatusSynced, orders[0].OrderStatus)

	paid, err := orderbusiness.PayOrder(runtime, orders[0].ID, orders[0].UserID, "payment-"+requestID, "trace-payment-"+requestID)
	require.NoError(t, err)
	require.Equal(t, ordermodels.PaymentStatusPaid, paid.PaymentStatus)
}
