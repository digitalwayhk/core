package performanceshop

import (
	"testing"

	"github.com/digitalwayhk/core/examples/04-shop-performance/contract"
	"github.com/stretchr/testify/assert"
)

func TestShopServiceRegistersCompleteInheritanceExample(t *testing.T) {
	service := &ShopService{}

	assert.Equal(t, contract.ServiceName, service.ServiceName())
	assert.Len(t, service.Routers(), 36)
}
