package shopmicroservices_test

import (
	"encoding/json"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestPrivateAPIs(t *testing.T) {
	t.Run("UserOrderSupplierChain", testUserOrderSupplierChain)
	t.Run("AddressOwnership", testAddressOwnership)
	t.Run("BuyerAndSupplierWebSocketIsolation", testBuyerAndSupplierWebSocketIsolation)
}
func testUserOrderSupplierChain(t *testing.T) {
	product, supplierToken := addProduct(t, "supplier-chain")
	address, userToken := addAddress(t, "buyer-chain")
	order := addOrder(t, userToken, product.ID, address.ID)
	assert.Equal(t, "supplier-chain", order.Product.SupplierID)
	buyerOrders := suites.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getorders", userToken, nil)
	require.True(t, buyerOrders.Success, buyerOrders.ErrorMessage)
	supplierOrders := suites.supplier.RequestJSON(t, http.MethodGet, "/api/shop-supplier/getorders", supplierToken, nil)
	require.True(t, supplierOrders.Success, supplierOrders.ErrorMessage)
	var items []*orderdto.SupplierOrder
	require.NoError(t, json.Unmarshal(supplierOrders.Data, &items))
	require.NotEmpty(t, items)
	assert.Equal(t, order.ID, items[0].ID)
}
func testAddressOwnership(t *testing.T) {
	address, _ := addAddress(t, "buyer-address-a")
	other := suites.user.TokenFor(t, "buyer-address-b", 0)
	response := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/deleteaddress", other, map[string]interface{}{"addressID": address.ID})
	assert.False(t, response.Success)
}
func testBuyerAndSupplierWebSocketIsolation(t *testing.T) {
	product, supplierToken := addProduct(t, "supplier-ws")
	address, userToken := addAddress(t, "buyer-ws")
	other := suites.user.TokenFor(t, "buyer-ws-other", 0)
	buyerWS := connectAndSubscribe(t, suites.user, userToken, "/api/shop-user/getorders")
	defer buyerWS.Close()
	supplierWS := connectAndSubscribe(t, suites.supplier, supplierToken, "/api/shop-supplier/getorders")
	defer supplierWS.Close()
	otherWS := connectAndSubscribe(t, suites.user, other, "/api/shop-user/getorders")
	defer otherWS.Close()
	otherEvents := suites.user.StreamWebSocket(t, otherWS)
	order := addOrder(t, userToken, product.ID, address.ID)
	buyerMessage := suites.user.ReadWebSocket(t, buyerWS, 5*time.Second)
	supplierMessage := suites.supplier.ReadWebSocket(t, supplierWS, 5*time.Second)
	var buyerEvent, supplierEvent eventdto.OrderChanged
	require.NoError(t, json.Unmarshal(buyerMessage.Data, &buyerEvent))
	require.NoError(t, json.Unmarshal(supplierMessage.Data, &supplierEvent))
	assert.Equal(t, order.ID, buyerEvent.OrderID)
	assert.Equal(t, order.ID, supplierEvent.OrderID)
	select {
	case message := <-otherEvents:
		t.Fatalf("其他用户不应收到订单事件: %+v", message)
	case <-time.After(300 * time.Millisecond):
	}
}
