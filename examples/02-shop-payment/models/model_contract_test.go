package models

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentTypeNormalizesCodeAndBuildsStableHash(t *testing.T) {
	paymentType := NewPaymentType()
	paymentType.Code = "  AliPay  "
	paymentType.Name = " 支付宝 "

	require.NoError(t, paymentType.Normalize())
	assert.Equal(t, "alipay", paymentType.Code)
	assert.Equal(t, "支付宝", paymentType.Name)
	assert.NotEmpty(t, paymentType.GetHash())
}

func TestNewOrderStartsUnpaidAndNormal(t *testing.T) {
	order := NewOrder()

	assert.Equal(t, OrderStatusNormal, order.Status)
	assert.Equal(t, PaymentStatusUnpaid, order.PaymentStatus)
	assert.Zero(t, order.PaymentID)
}

func TestPaymentRecordHashUsesOrderAndAttempt(t *testing.T) {
	first := NewPaymentRecord()
	first.OrderID = 42
	first.Attempt = 1
	first.Amount = decimal.RequireFromString("19.90")

	second := NewPaymentRecord()
	second.OrderID = 42
	second.Attempt = 2
	second.Amount = decimal.RequireFromString("19.90")

	assert.NotEmpty(t, first.GetHash())
	assert.NotEqual(t, first.GetHash(), second.GetHash())
}

func TestPaymentStatusNamesAreStableChineseLabels(t *testing.T) {
	assert.Equal(t, "未支付", PaymentStatusUnpaid.String())
	assert.Equal(t, "支付中", PaymentStatusPending.String())
	assert.Equal(t, "已支付", PaymentStatusPaid.String())
	assert.Equal(t, "支付失败", PaymentStatusFailed.String())
	assert.Equal(t, "退款中", PaymentStatusRefunding.String())
	assert.Equal(t, "已退款", PaymentStatusRefunded.String())
}
