package business

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var orderTestSequence atomic.Uint64

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

func nextValue(prefix string) (uint, string) {
	value := orderTestSequence.Add(1)
	return uint(700000 + value), fmt.Sprintf("%s-%d", prefix, value)
}

func fixedProductSnapshot() supplierdto.ProductSnapshot {
	return supplierdto.ProductSnapshot{
		ProductID: 91, SupplierID: 81, SupplierCode: "supplier-code", SupplierName: "供应商快照",
		ProductCode: "product-code", ProductName: "商品快照", UnitPrice: decimal.NewFromInt(15),
	}
}

func createOrderCommand(requestID string, userID uint, quantity int) CreateOrderCommand {
	orderID, eventID := nextValue("order-created")
	return CreateOrderCommand{
		OrderID: orderID, UserID: userID, RequestID: requestID, EventID: eventID,
		ProductID: fixedProductSnapshot().ProductID, Quantity: quantity,
		Address: userdto.AddressSnapshot{AddressID: 71, Recipient: " 收件人 ", Phone: " 10086 ", Region: " 华南 ", Detail: " 完整地址 "},
	}
}

func TestCreateOrderRejectsIdempotencyKeyReuseWithDifferentFingerprint(t *testing.T) {
	_, requestID := nextValue("buyer-request")
	command := createOrderCommand(requestID, 10, 2)
	first, err := CreateOrder(command, fixedProductSnapshot())
	require.NoError(t, err)

	changed := command
	changed.Quantity = 3
	changed.OrderID, changed.EventID = nextValue("changed-order")
	second, err := CreateOrder(changed, fixedProductSnapshot())
	require.ErrorIs(t, err, contract.ErrIdempotencyKeyReused)
	require.Nil(t, second)
	require.Equal(t, uint64(1), first.OrderRevision)
}

func TestCreateOrderConvergesAndPreservesSnapshots(t *testing.T) {
	_, requestID := nextValue("same-request")
	command := createOrderCommand(requestID, 11, 2)
	first, err := CreateOrder(command, fixedProductSnapshot())
	require.NoError(t, err)
	repeated := command
	repeated.OrderID, repeated.EventID = nextValue("repeated-order")
	second, err := CreateOrder(repeated, fixedProductSnapshot())
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "供应商快照", second.Product.SupplierName)
	require.Equal(t, "完整地址", second.Address.Detail)
	require.True(t, decimal.NewFromInt(30).Equal(second.TotalAmount))
}

func TestConcurrentCreateOrderConvergesOnOneFact(t *testing.T) {
	_, requestID := nextValue("concurrent-request")
	base := createOrderCommand(requestID, 12, 1)
	const workers = 8
	ids := make(chan uint, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			command := base
			command.OrderID = base.OrderID + uint(index)
			command.EventID = fmt.Sprintf("%s-%d", base.EventID, index)
			order, err := CreateOrder(command, fixedProductSnapshot())
			if err == nil {
				ids <- order.ID
			}
			errs <- err
		}(index)
	}
	wait.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var winner uint
	for id := range ids {
		if winner == 0 {
			winner = id
		}
		require.Equal(t, winner, id)
	}
}

func TestCancelOrderKeepsFactAndAdvancesRevision(t *testing.T) {
	_, requestID := nextValue("cancel-request")
	created, err := CreateOrder(createOrderCommand(requestID, 13, 1), fixedProductSnapshot())
	require.NoError(t, err)
	_, eventID := nextValue("cancel-event")
	cancelled, err := CancelOrder(created.UserID, created.ID, eventID)
	require.NoError(t, err)
	require.Equal(t, models.OrderStatusCancelled, cancelled.OrderStatus)
	require.Equal(t, uint64(2), cancelled.OrderRevision)
	stored, err := models.FindOrder(created.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
}

func TestPaymentAttemptsAndRefundStateMachine(t *testing.T) {
	paymentType := models.NewPaymentType()
	paymentType.SetID(720001)
	paymentType.Name, paymentType.Code, paymentType.Enabled = "测试支付", "test-pay", true
	require.NoError(t, models.SavePaymentType(paymentType))

	_, requestID := nextValue("payment-request")
	created, err := CreateOrder(createOrderCommand(requestID, 14, 2), fixedProductSnapshot())
	require.NoError(t, err)
	_, paymentID := nextValue("payment")
	_, paymentEvent := nextValue("payment-processing")
	record, err := CreatePayment(created.UserID, created.ID, paymentType.ID, paymentID, paymentEvent)
	require.NoError(t, err)
	require.Equal(t, uint(1), record.Attempt)
	require.Equal(t, paymentID, record.PaymentID)

	_, anotherPaymentID := nextValue("payment")
	_, anotherEvent := nextValue("payment-processing")
	_, err = CreatePayment(created.UserID, created.ID, paymentType.ID, anotherPaymentID, anotherEvent)
	require.Error(t, err)

	_, paidEvent := nextValue("payment-paid")
	paid, err := ConfirmPayment(paymentID, paidEvent)
	require.NoError(t, err)
	require.Equal(t, models.PaymentStatusPaid, paid.PaymentStatus)
	require.Equal(t, uint64(3), paid.OrderRevision)

	_, cancelEvent := nextValue("paid-cancel")
	refunding, err := CancelOrder(created.UserID, created.ID, cancelEvent)
	require.NoError(t, err)
	require.Equal(t, models.OrderStatusCancelling, refunding.OrderStatus)
	require.Equal(t, models.PaymentStatusRefunding, refunding.PaymentStatus)

	_, refundEvent := nextValue("payment-refunded")
	refunded, err := ConfirmRefund(paymentID, refundEvent)
	require.NoError(t, err)
	require.Equal(t, models.OrderStatusCancelled, refunded.OrderStatus)
	require.Equal(t, models.PaymentStatusRefunded, refunded.PaymentStatus)
}

func TestUsedPaymentTypeCannotBeDeletedOrRecoded(t *testing.T) {
	paymentType := models.NewPaymentType()
	paymentType.SetID(730001)
	paymentType.Name, paymentType.Code, paymentType.Enabled = "被引用支付", "used-pay", true
	require.NoError(t, models.SavePaymentType(paymentType))
	_, requestID := nextValue("used-payment-request")
	created, err := CreateOrder(createOrderCommand(requestID, 15, 1), fixedProductSnapshot())
	require.NoError(t, err)
	_, paymentID := nextValue("used-payment")
	_, eventID := nextValue("used-payment-event")
	_, err = CreatePayment(created.UserID, created.ID, paymentType.ID, paymentID, eventID)
	require.NoError(t, err)

	require.ErrorIs(t, models.DeletePaymentType(paymentType), contract.ErrResourceInUse)
	paymentType.Code = "changed-code"
	require.ErrorIs(t, models.SavePaymentType(paymentType), contract.ErrResourceInUse)
}
