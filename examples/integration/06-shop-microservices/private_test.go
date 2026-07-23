// 本文件验证 06 all-in-one 集成场景下用户 Private API 和用户资料 Manage 的身份边界。
// 普通用户只能维护自己的地址和订单订阅，WebSocket 订单事件必须按登录用户隔离。
package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/stretchr/testify/require"
)

// TestAddressManageEnforcesOwnership 验证其他普通用户不能删除当前买家的地址。
func TestAddressManageEnforcesOwnership(t *testing.T) {
	address, _ := addAddress(t, "buyer-address-a")
	other := suites.user.TokenFor(t, "buyer-address-b", 1)
	response := suites.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/remove", other, map[string]interface{}{"id": address.ID})
	require.False(t, response.Success)
}

// TestBuyerWebSocketIsolation 验证订单事件只推送给下单买家，不会被其他用户订阅收到。
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
