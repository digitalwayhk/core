package business

import (
	"os"
	"testing"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shop-order-test-")
	if err != nil {
		panic(err)
	}
	utils.TESTPATH = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCreateOrderKeepsSnapshotsAndConvergesIdempotency(t *testing.T) {
	before, err := models.PendingOutbox()
	require.NoError(t, err)
	product := supplierdto.ProductSnapshot{ProductID: 8, SupplierID: "supplier-a", SupplierName: "供应商 A", ProductCode: "p-8", ProductName: "商品 A", UnitPrice: decimal.NewFromInt(12)}
	address := userdto.AddressSnapshot{AddressID: 9, Recipient: "用户 A", Phone: "10086", Region: "测试区", Detail: "1 号"}

	created, err := CreateOrder(1001, "buyer-a", "request-1", "event-1", product, address, 2)
	require.NoError(t, err)
	assert.Equal(t, uint(1001), created.ID)
	assert.Equal(t, "商品 A", created.Product.ProductName)
	assert.Equal(t, "1 号", created.Address.Detail)
	assert.True(t, created.TotalAmount.Equal(decimal.NewFromInt(24)))

	repeated, err := CreateOrder(1002, "buyer-a", "request-1", "event-2", product, address, 2)
	require.NoError(t, err)
	assert.Equal(t, created.ID, repeated.ID)
	orders, err := UserOrders("buyer-a")
	require.NoError(t, err)
	assert.Len(t, orders, 1)
	pending, err := models.PendingOutbox()
	require.NoError(t, err)
	assert.Len(t, pending, len(before)+1)
}

func TestDeleteOrderRejectsOtherUserAndWritesOutboxAtomically(t *testing.T) {
	before, err := models.PendingOutbox()
	require.NoError(t, err)
	product := supplierdto.ProductSnapshot{ProductID: 18, SupplierID: "supplier-b", ProductName: "商品 B", UnitPrice: decimal.NewFromInt(5)}
	address := userdto.AddressSnapshot{AddressID: 19, Recipient: "B"}
	created, err := CreateOrder(2001, "buyer-b", "request-delete", "event-create-delete", product, address, 1)
	require.NoError(t, err)

	_, err = DeleteOrCancel("buyer-other", created.ID, "event-forbidden")
	require.Error(t, err)
	_, err = DeleteOrCancel("buyer-b", created.ID, "event-delete")
	require.NoError(t, err)
	orders, err := UserOrders("buyer-b")
	require.NoError(t, err)
	assert.Empty(t, orders)
	pending, err := models.PendingOutbox()
	require.NoError(t, err)
	assert.Len(t, pending, len(before)+2)
}

func TestPaymentRequiresEnabledTypeAndKeepsStateTransaction(t *testing.T) {
	paymentType := models.NewPaymentType()
	paymentType.SetID(3001)
	paymentType.Name = "测试支付"
	paymentType.Code = "test-pay"
	paymentType.Enabled = true
	require.NoError(t, models.SavePaymentType(paymentType))
	product := supplierdto.ProductSnapshot{ProductID: 28, SupplierID: "supplier-pay", ProductName: "支付商品", UnitPrice: decimal.NewFromInt(20)}
	address := userdto.AddressSnapshot{AddressID: 29, Recipient: "用户 C"}
	created, err := CreateOrder(3002, "buyer-pay", "request-pay", "event-create-pay", product, address, 2)
	require.NoError(t, err)
	record, err := CreatePayment("buyer-pay", created.ID, paymentType.ID, 3003, "event-payment")
	require.NoError(t, err)
	assert.Equal(t, models.PaymentStatusProcessing, record.Status)
	assert.Equal(t, paymentType.ID, record.PaymentTypeID)
	paid, err := ConfirmPayment(record.ID, "event-paid")
	require.NoError(t, err)
	assert.Equal(t, models.PaymentStatusPaid, paid.PaymentStatus)
	assert.Equal(t, record.ID, paid.PaymentID)
	_, err = DeleteOrCancel("buyer-pay", created.ID, "event-cancel-paid")
	require.NoError(t, err)
	stored, err := models.FindOrder(created.ID)
	require.NoError(t, err)
	assert.Equal(t, models.OrderStatusCancelled, stored.Status)
}
