package paymentshop

import (
	"testing"

	"github.com/digitalwayhk/core/examples/02-shop-payment/contract"
	"github.com/stretchr/testify/assert"
)

func TestPaymentShopServiceRegistersCompleteExample(t *testing.T) {
	service := &ShopService{}

	assert.Equal(t, contract.ServiceName, service.ServiceName())
	assert.Len(t, service.Routers(), 26)
}
