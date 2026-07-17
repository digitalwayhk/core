package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/stretchr/testify/require"
)

func TestAddressManageEnforcesOwnership(t *testing.T) {
	address, _ := addAddress(t, "buyer-address-a")
	other := suites.user.TokenFor(t, "buyer-address-b", 1)
	response := suites.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/remove", other, map[string]interface{}{"id": address.ID})
	require.False(t, response.Success)
}

func TestBuyerWebSocketIsolation(t *testing.T) {
	product, _ := addProduct(t, "supplier-ws")
	address, buyerToken := addAddress(t, "buyer-ws")
	otherToken := suites.user.TokenFor(t, "buyer-ws-other", 0)
	buyerWS := connectAndSubscribe(t, suites.user, buyerToken, "/api/shop-user/getorders")
	defer buyerWS.Close()
	otherWS := connectAndSubscribe(t, suites.user, otherToken, "/api/shop-user/getorders")
	defer otherWS.Close()
	otherEvents := suites.user.StreamWebSocket(t, otherWS)
	order := addOrder(t, buyerToken, product.ID, address.ID)
	message := suites.user.ReadWebSocket(t, buyerWS, 5*time.Second)
	var orderEvent eventdto.OrderChanged
	require.NoError(t, json.Unmarshal(message.Data, &orderEvent))
	require.Equal(t, order.ID, orderEvent.OrderID)
	select {
	case unexpected := <-otherEvents:
		t.Fatalf("其他用户不应收到订单事件: %+v", unexpected)
	case <-time.After(300 * time.Millisecond):
	}
}
