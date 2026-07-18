package transaction

import (
	"errors"
	"testing"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransactionDoesNotCaptureConcurrentOrderInsert 验证普通写入不会串入另一个业务事务。
func TestTransactionDoesNotCaptureConcurrentOrderInsert(t *testing.T) {
	utils.TESTPATH = t.TempDir()

	supplier := insertEnabledSupplier(t, 1100, "isolation-supplier")
	product := models.NewProduct()
	product.SetID(1101)
	product.Code = "isolation-product"
	product.Name = "事务隔离测试商品"
	product.SupplierID = supplier.ID
	product.Price = decimal.RequireFromString("9.90")
	product.Enabled = true
	require.NoError(t, product.Insert())

	rollbackRecord := models.NewPaymentRecord()
	rollbackRecord.SetID(1301)
	rollbackRecord.OrderID = 1201
	rollbackRecord.UserID = "isolation-user"
	rollbackRecord.PaymentTypeID = 1401
	rollbackRecord.PaymentTypeCode = "isolation-pay"
	rollbackRecord.PaymentTypeName = "事务隔离测试支付"
	rollbackRecord.Amount = product.Price
	rollbackRecord.Attempt = 1

	transactionStarted := make(chan struct{})
	orderInserted := make(chan struct{})
	transactionResult := make(chan error, 1)
	go func() {
		transactionResult <- models.RunInTransaction(func(action persistencetypes.IDataAction) error {
			close(transactionStarted)
			<-orderInserted
			if err := rollbackRecord.InsertWith(action); err != nil {
				return err
			}
			return errors.New("主动回滚测试事务")
		})
	}()

	<-transactionStarted
	created, err := NewOrderService().CreateOrder("isolation-user", product.ID, 1, 1201)
	require.NoError(t, err)
	close(orderInserted)
	require.ErrorContains(t, <-transactionResult, "主动回滚")

	persisted, err := models.NewOrder().FindByID(created.Order.ID)
	require.NoError(t, err)
	assert.NotNil(t, persisted, "普通下单不应被其他事务回滚")

	rolledBack, err := models.NewPaymentRecord().FindByID(rollbackRecord.ID)
	require.NoError(t, err)
	assert.Nil(t, rolledBack, "事务内支付流水必须随事务回滚")
}
