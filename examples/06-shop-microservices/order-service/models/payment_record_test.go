package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestPaymentTypeDefaultsDisabled(t *testing.T) {
	require.False(t, NewPaymentType().Enabled)
}
