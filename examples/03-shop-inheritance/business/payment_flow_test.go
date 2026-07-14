package business

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/examples/03-shop-inheritance/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentFlowSupportsFailureRetryAndRefund(t *testing.T) {
	utils.TESTPATH = t.TempDir()

	supplier := insertEnabledSupplier(t, 100, "payment-supplier")
	product := models.NewProduct()
	product.SetID(101)
	product.Code = "payment-product"
	product.Name = "测试商品"
	product.SupplierID = supplier.ID
	product.Price = decimal.RequireFromString("19.90")
	product.Enabled = true
	require.NoError(t, product.Insert())

	paymentType := models.NewPaymentType()
	paymentType.SetID(201)
	paymentType.Code = "test-pay"
	paymentType.Name = "测试支付"
	paymentType.Enabled = true
	require.NoError(t, paymentType.Insert())

	orders := NewOrderService()
	payments := NewPaymentService()

	created, err := orders.CreateOrder("user-a", product.ID, 2, 301)
	require.NoError(t, err)
	assert.Equal(t, "created", created.Action)
	assert.Equal(t, models.PaymentStatusUnpaid, created.Order.PaymentStatus)

	first, err := payments.CreatePayment("user-a", created.Order.ID, paymentType.ID, 401)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Payment.Attempt)
	assert.Equal(t, "39.8", first.Payment.Amount.String())
	assert.Equal(t, models.PaymentStatusPending, first.Order.PaymentStatus)

	_, err = orders.DeleteUnpaidOrder("user-a", created.Order.ID)
	require.ErrorContains(t, err, "支付处理中")

	failed, err := payments.FailPayment(first.Payment.ID)
	require.NoError(t, err)
	assert.Equal(t, "payment_failed", failed.Action)
	assert.Equal(t, models.PaymentStatusFailed, failed.Order.PaymentStatus)

	second, err := payments.CreatePayment("user-a", created.Order.ID, paymentType.ID, 402)
	require.NoError(t, err)
	assert.Equal(t, 2, second.Payment.Attempt)
	assert.Equal(t, models.PaymentStatusPending, second.Order.PaymentStatus)
	_, err = NewPaymentTypeService().Disable(paymentType.ID)
	require.NoError(t, err)

	paid, err := payments.ConfirmPayment(second.Payment.ID)
	require.NoError(t, err)
	assert.Equal(t, "paid", paid.Action)
	assert.Equal(t, models.PaymentStatusPaid, paid.Order.PaymentStatus)
	_, err = payments.ConfirmPayment(second.Payment.ID)
	require.NoError(t, err, "重复确认已支付流水应保持幂等")

	_, err = orders.DeleteUnpaidOrder("user-a", created.Order.ID)
	require.ErrorContains(t, err, "已支付订单不能删除")

	refunding, err := orders.RequestCancellation("user-a", created.Order.ID)
	require.NoError(t, err)
	assert.Equal(t, "refund_pending", refunding.Action)
	assert.Equal(t, models.OrderStatusCancelling, refunding.Order.OrderStatus())
	assert.Equal(t, models.PaymentStatusRefunding, refunding.Order.PaymentStatus)

	refunded, err := payments.ConfirmRefund(second.Payment.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", refunded.Action)
	assert.Equal(t, models.OrderStatusCancelled, refunded.Order.OrderStatus())
	assert.Equal(t, models.PaymentStatusRefunded, refunded.Order.PaymentStatus)
	_, err = payments.ConfirmRefund(second.Payment.ID)
	require.NoError(t, err, "重复确认已退款流水应保持幂等")

	records, err := models.NewPaymentRecord().QueryByOrder(created.Order.ID)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, models.PaymentStatusFailed, records[0].PaymentStatus())
	assert.Equal(t, models.PaymentStatusRefunded, records[1].PaymentStatus())

	require.ErrorContains(t, NewProductService().EnsureRemovable(product.ID), "商品已被订单使用")
	require.ErrorContains(t, NewPaymentTypeService().EnsureRemovable(paymentType.ID), "支付类型已被支付流水使用")

	rollbackRecord := models.NewPaymentRecord()
	rollbackRecord.SetID(499)
	rollbackRecord.OrderID = created.Order.ID
	rollbackRecord.UserID = "user-a"
	rollbackRecord.PaymentTypeID = paymentType.ID
	rollbackRecord.PaymentTypeCode = paymentType.Code
	rollbackRecord.PaymentTypeName = paymentType.Name
	rollbackRecord.Amount = decimal.RequireFromString("1.00")
	rollbackRecord.Attempt = 99
	err = models.RunInTransaction(func(action persistencetypes.IDataAction) error {
		require.NoError(t, rollbackRecord.InsertWith(action))
		return errors.New("触发回滚")
	})
	require.ErrorContains(t, err, "触发回滚")
	rolledBack, err := models.NewPaymentRecord().FindByID(rollbackRecord.ID)
	require.NoError(t, err)
	assert.Nil(t, rolledBack, "事务失败后不能保留支付流水")

	_, err = NewPaymentTypeService().Enable(paymentType.ID)
	require.NoError(t, err)
	concurrentOrder, err := orders.CreateOrder("user-concurrent", product.ID, 1, 302)
	require.NoError(t, err)
	var succeeded atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func(paymentID uint) {
			defer group.Done()
			if _, createErr := payments.CreatePayment("user-concurrent", concurrentOrder.Order.ID, paymentType.ID, paymentID); createErr == nil {
				succeeded.Add(1)
			}
		}(uint(500 + index))
	}
	group.Wait()
	assert.Equal(t, int32(1), succeeded.Load(), "同一订单并发发起支付只能成功一次")
}

func insertEnabledSupplier(t *testing.T, id uint, code string) *models.Supplier {
	t.Helper()
	supplier := models.NewSupplier()
	supplier.SetID(id)
	supplier.Code = code
	supplier.Name = "测试供应商-" + code
	supplier.Enabled = true
	require.NoError(t, supplier.Insert())
	return supplier
}
