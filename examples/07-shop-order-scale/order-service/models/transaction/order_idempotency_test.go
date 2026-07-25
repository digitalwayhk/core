// 本文件验证 07 订单远程权威库用 UserID + requestID 收敛重复订单的能力。
package transaction_test

import (
	"strings"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestRemoteOrderIdempotency 验证同一买家同一 requestID 只能形成一笔远程权威订单。
func TestRemoteOrderIdempotency(t *testing.T) {
	if err := models.EnsureStorage(); err != nil {
		if strings.Contains(err.Error(), "dial tcp") || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("MySQL 权威库不可用，跳过远程幂等测试: %v", err)
		}
		require.NoError(t, err)
	}
	firstIDs := utils.NewAlgorithmSnowFlake(26, 4)
	secondIDs := utils.NewAlgorithmSnowFlake(27, 4)

	first := models.NewOrder()
	first.ID = uint(firstIDs.NextId())
	first.UserID = 1001
	first.SupplierID = 2001
	first.ProductID = 3001
	first.RequestID = "request-same"
	first.RequestFingerprint = "fingerprint-a"
	first.Quantity = 2
	first.UnitPrice = decimal.NewFromInt(10)
	first.TotalAmount = decimal.NewFromInt(20)
	first.TraceID = "trace-idempotency"
	first.ServiceName = "shop-order"
	first.ServiceInstanceID = "order-a"

	var firstStored *models.Order
	require.NoError(t, models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		firstStored, err = models.UpsertRemoteOrderWith(action, first)
		return err
	}))

	second := models.NewOrder()
	second.ID = uint(secondIDs.NextId())
	second.UserID = first.UserID
	second.SupplierID = first.SupplierID
	second.ProductID = first.ProductID
	second.RequestID = first.RequestID
	second.RequestFingerprint = first.RequestFingerprint
	second.Quantity = first.Quantity
	second.UnitPrice = first.UnitPrice
	second.TotalAmount = first.TotalAmount
	second.TraceID = "trace-idempotency-retry"
	second.ServiceName = "shop-order"
	second.ServiceInstanceID = "order-b"

	var secondStored *models.Order
	require.NoError(t, models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		secondStored, err = models.UpsertRemoteOrderWith(action, second)
		return err
	}))

	require.Equal(t, firstStored.ID, secondStored.ID)
	require.Equal(t, firstStored.RequestID, secondStored.RequestID)
	require.Equal(t, firstStored.UserID, secondStored.UserID)
}
