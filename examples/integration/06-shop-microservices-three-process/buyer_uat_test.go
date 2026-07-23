// 本文件保存 06 三进程 UAT 中普通用户角色的完整闭环。
// 买家负责维护自己的用户资料和地址，通过用户服务 Private API 下单和支付，
// 并验证其他普通用户不能查询到自己的订单。
package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type threeProcessBuyerRole struct {
	manageToken string
	token       string
	otherToken  string
	address     userdto.Address
}

// TestThreeProcessUATBuyerRoleFlow 验证买家角色可独立完成资料维护、下单、发起支付和本人订单查询闭环。
func TestThreeProcessUATBuyerRoleFlow(t *testing.T) {
	scenario := startThreeProcessUAT(t)

	buyer := scenario.completeBuyerProfile()
	supplier := scenario.publishSupplierProduct()
	paymentType := scenario.configurePaymentType()

	buyerWS := scenario.connectBuyerOrdersWebSocket(buyer.token)
	defer buyerWS.Close()
	otherWS := scenario.connectBuyerOrdersWebSocket(buyer.otherToken)
	defer otherWS.Close()
	otherEvents := scenario.user.StreamWebSocket(t, otherWS)

	created := scenario.buyerCreatesOrder(buyer, supplier)
	scenario.assertBuyerReceivesOrderWebSocket(buyerWS, otherEvents, created)
	payment := scenario.buyerCreatesPayment(buyer, created, paymentType)
	assertPaymentBelongsToOrder(t, payment, created)

	scenario.assertBuyerCanSeeOwnOrder(buyer, created)
	scenario.assertOtherBuyerCannotSeeOrder(buyer, created)
}

// connectBuyerOrdersWebSocket 登录用户服务 WebSocket 并订阅买家订单路由。
func (scenario *threeProcessUAT) connectBuyerOrdersWebSocket(token string) *websocket.Conn {
	t := scenarioTest(scenario)
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(scenario.user.WebSocketURL, nil)
	require.NoError(t, err)
	scenario.user.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	require.Equal(t, "success", scenario.user.ReadWebSocket(t, connection, 3*time.Second).Event)
	scenario.user.WriteWebSocket(t, connection, "sub", "/api/shop-user/getorders", map[string]interface{}{})
	require.Equal(t, "sub", scenario.user.ReadWebSocket(t, connection, 3*time.Second).Event)
	return connection
}

// assertBuyerReceivesOrderWebSocket 验证订单事件只通过 WebSocket 推送给下单买家。
func (scenario *threeProcessUAT) assertBuyerReceivesOrderWebSocket(buyerWS *websocket.Conn, otherEvents <-chan integration.WebSocketMessage, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	message := scenario.user.ReadWebSocket(t, buyerWS, 5*time.Second)
	var orderEvent eventdto.OrderChanged
	require.NoError(t, json.Unmarshal(message.Data, &orderEvent))
	require.Equal(t, created.ID, orderEvent.OrderID)
	select {
	case unexpected := <-otherEvents:
		t.Fatalf("其他买家不应收到订单 WebSocket 事件: %+v", unexpected)
	case <-time.After(300 * time.Millisecond):
	}
}

// completeBuyerProfile 准备买家 Manage 与 Private token，并完成用户资料和地址维护。
func (scenario *threeProcessUAT) completeBuyerProfile() threeProcessBuyerRole {
	buyer := threeProcessBuyerRole{
		manageToken: scenario.user.TokenFor(scenarioTest(scenario), "uat-buyer-"+scenario.suffix, 1),
		token:       scenario.user.TokenFor(scenarioTest(scenario), "uat-buyer-"+scenario.suffix, 0),
		otherToken:  scenario.user.TokenFor(scenarioTest(scenario), "uat-other-buyer-"+scenario.suffix, 0),
	}
	scenario.updateBuyerName(buyer.manageToken, "三进程买家")
	buyer.address = scenario.addBuyerAddress(buyer.manageToken)
	return buyer
}

// updateBuyerName 通过用户 Manage API 修改当前买家的基础资料，验证自管理限域。
func (scenario *threeProcessUAT) updateBuyerName(manageToken, name string) {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/usermanage/search", manageToken, map[string]interface{}{"page": 1, "size": 10})
	require.True(t, response.Success, response.ErrorMessage)
	var table struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &table))
	require.Len(t, table.Rows, 1)
	edited := scenario.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/usermanage/edit", manageToken, map[string]interface{}{"id": table.Rows[0].ID, "name": name})
	require.True(t, edited.Success, edited.ErrorMessage)
}

// addBuyerAddress 通过用户 Manage API 新增当前买家的收货地址。
func (scenario *threeProcessUAT) addBuyerAddress(manageToken string) userdto.Address {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/add", manageToken, map[string]interface{}{"recipient": "三进程买家", "phone": "10086", "region": "测试区", "detail": "三进程 1 号"})
	require.True(t, response.Success, response.ErrorMessage)
	var raw struct {
		ID                               string `json:"id"`
		Recipient, Phone, Region, Detail string
	}
	require.NoError(t, json.Unmarshal(response.Data, &raw))
	id, err := strconv.ParseUint(raw.ID, 10, 64)
	require.NoError(t, err)
	return userdto.Address{ID: uint(id), Recipient: raw.Recipient, Phone: raw.Phone, Region: raw.Region, Detail: raw.Detail}
}

// buyerCreatesOrder 通过用户服务 Private API 下单，触发 User -> Order -> Supplier 内部调用链。
func (scenario *threeProcessUAT) buyerCreatesOrder(buyer threeProcessBuyerRole, supplier threeProcessSupplierRole) orderdto.Order {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyer.token, map[string]interface{}{"requestID": "uat-request-" + scenario.suffix, "productID": supplier.product.ID, "quantity": 2, "addressID": buyer.address.ID})
	require.True(t, response.Success, response.ErrorMessage)
	var result orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &result))
	return result
}

// buyerCreatesPayment 通过用户服务 Private API 创建支付记录，订单服务是支付事实权威。
func (scenario *threeProcessUAT) buyerCreatesPayment(buyer threeProcessBuyerRole, order orderdto.Order, paymentType orderdto.PaymentType) orderdto.PaymentRecord {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", buyer.token, map[string]interface{}{"orderID": order.ID, "paymentTypeID": paymentType.ID})
	require.True(t, response.Success, response.ErrorMessage)
	var result orderdto.PaymentRecord
	require.NoError(t, json.Unmarshal(response.Data, &result))
	return result
}

// assertBuyerCanSeeOwnOrder 验证买家能在用户服务查询到自己的订单和地址快照。
func (scenario *threeProcessUAT) assertBuyerCanSeeOwnOrder(buyer threeProcessBuyerRole, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	require.Eventually(t, func() bool {
		orders := scenario.getBuyerOrders(buyer.token)
		found := findOrderByID(orders, created.ID)
		return found != nil && found.UserID == created.UserID && found.Address.Detail == buyer.address.Detail
	}, 5*time.Second, 25*time.Millisecond)
}

// assertOtherBuyerCannotSeeOrder 验证其他普通用户不能通过用户服务查询到当前买家的订单。
func (scenario *threeProcessUAT) assertOtherBuyerCannotSeeOrder(buyer threeProcessBuyerRole, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	otherBuyerOrders := scenario.getBuyerOrders(buyer.otherToken)
	require.Nil(t, findOrderByID(otherBuyerOrders, created.ID), "其他普通用户不能查询到该订单")
}

// getBuyerOrders 通过用户服务 Private API 查询当前登录买家的订单列表。
func (scenario *threeProcessUAT) getBuyerOrders(token string) []*orderdto.Order {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getorders", token, nil)
	require.True(t, response.Success, response.ErrorMessage)
	var orders []*orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}
