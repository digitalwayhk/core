// 本文件验证 07 订单远程权威库用 UserID + requestID 收敛重复订单的能力。
package transaction_test

import (
	"os"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestRemoteOrderIdempotency 验证同一买家同一 requestID 只能形成一笔远程权威订单。
func TestRemoteOrderIdempotency(t *testing.T) {
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("设置 CORE_TEST_MYSQL=1 后运行远程幂等 MySQL 集成测试")
	}
	require.NoError(t, models.EnsureStorage())
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
