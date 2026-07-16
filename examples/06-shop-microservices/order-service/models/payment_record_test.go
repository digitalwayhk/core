package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaymentRecordHashDistinguishesPaymentAttempts(t *testing.T) {
	first := NewPaymentRecord()
	first.SetID(101)
	first.OrderID = 20
	first.PaymentTypeID = 1

	second := NewPaymentRecord()
	second.SetID(102)
	second.OrderID = 20
	second.PaymentTypeID = 2

	assert.NotEqual(t, first.GetHash(), second.GetHash())
	assert.Equal(t, first.GetHash(), first.GetHash())
}
