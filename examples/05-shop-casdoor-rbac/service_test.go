package casdoorrbacshop

import (
	"testing"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/stretchr/testify/assert"
)

func TestShopServiceRegistersCompleteInheritanceExample(t *testing.T) {
	service := &ShopService{}

	assert.Equal(t, contract.ServiceName, service.ServiceName())
	assert.Len(t, service.Routers(), 36)
	assert.Nil(t, service.SubscribeRouters())
}
