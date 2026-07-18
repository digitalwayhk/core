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
	require.NoError(t, ordermodels.EnsureStorage())
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := fmt.Sprintf("buyer-uat-%d", unique)

	_, err := (orderbusiness.LocalOrderWriter{}).Accept(context.Background(), orderbusiness.CreateOrderCommand{
		OrderID:            810000 + unique,
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

	require.NoError(t, (orderbusiness.RemoteOrderSyncer{}).DrainOnce(context.Background(), 10000))
	orders, _, err := orderbusiness.ListOrders(ordermodels.OrderQueryFilter{UserID: 110000 + unique}, 1, 20)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.Equal(t, ordermodels.OrderStatusSynced, orders[0].OrderStatus)

	paid, err := orderbusiness.PayOrder(orders[0].ID, orders[0].UserID, "payment-"+requestID, "trace-payment-"+requestID)
	require.NoError(t, err)
	require.Equal(t, ordermodels.PaymentStatusPaid, paid.PaymentStatus)
}
