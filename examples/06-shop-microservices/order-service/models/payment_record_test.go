// 本文件验证当前服务模型层的持久化、投影和幂等边界。
package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPaymentRecordHashDistinguishesPaymentAttempts 验证当前场景的业务闭环和边界行为。
func TestPaymentRecordHashDistinguishesPaymentAttempts(t *testing.T) {
	first := NewPaymentRecord()
	first.OrderID = 10
	first.Attempt = 1
	first.PaymentID = "payment-1"
	second := NewPaymentRecord()
	second.OrderID = 10
	second.Attempt = 2
	second.PaymentID = "payment-2"

	require.NotEqual(t, first.GetHash(), second.GetHash())
}

// TestPaymentTypeDefaultsDisabled 验证当前场景的业务闭环和边界行为。
func TestPaymentTypeDefaultsDisabled(t *testing.T) {
	require.False(t, NewPaymentType().Enabled)
}
