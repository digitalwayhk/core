// Package shoporderscalemultiprocess 验证 07 Docker 多进程下买家角色的跨服务闭环。
// 本文件覆盖买家经 user-service 下单、重复 requestID 幂等、order 双副本同步到共享 MySQL 后本人查询可见。
package shoporderscalemultiprocess

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// TestDockerUATBuyerRoleFlow 验证买家角色在 Docker 多 order 副本下的下单和订单查询闭环。
func TestDockerUATBuyerRoleFlow(t *testing.T) {
	compose := startDockerOrderScaleStack(t)
	user := &integration.Suite{BaseURL: "http://127.0.0.1:18181"}
	supplier := &integration.Suite{BaseURL: "http://127.0.0.1:18182"}
	waitDockerHTTPReady(t, 18181)
	waitDockerHTTPReady(t, 18182)
	user.WebSocketURL = "ws://127.0.0.1:18181/ws"
	verifyDockerOrderReplicaDiscovery(t, compose)

	adminFixture := prepareDockerAdminFixture(t, supplier)
	supplierFixture := prepareDockerSupplierFixture(t, supplier, adminFixture)
	requireDockerBuyerPublicFacade(t, user, supplierFixture.ProductID)

	buyerToken := user.TokenFor(t, "720101", 0)
	otherToken := user.TokenFor(t, "720102", 0)
	buyerWS := connectDockerBuyerOrdersWebSocket(t, user, buyerToken)
	defer buyerWS.Close()
	otherWS := connectDockerBuyerOrdersWebSocket(t, user, otherToken)
	defer otherWS.Close()
	otherEvents := user.StreamWebSocket(t, otherWS)

	requestID := "docker-buyer-role-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	created := createDockerBuyerOrderWithRequest(t, user, buyerToken, supplierFixture.ProductID, requestID)
	retried := createDockerBuyerOrderWithRequest(t, user, buyerToken, supplierFixture.ProductID, requestID)
	require.Equal(t, created.OrderID, retried.OrderID)
	waitDockerOrderVisible(t, user, buyerToken, created.OrderID)
	require.Equal(t, "Docker买家", dockerBuyerOrderByID(t, user, buyerToken, created.OrderID).Address.ReceiverName)
	requireDockerOrderEvent(t, user, buyerWS, created.OrderID, "", "unpaid")

	paid := payDockerBuyerOrder(t, user, buyerToken, created.OrderID, adminFixture.PaymentTypeID)
	require.Equal(t, "paid", paid.PaymentStatus)
	requireDockerOrderEvent(t, user, buyerWS, created.OrderID, "", "paid")

	cancelled := createDockerBuyerOrder(t, user, buyerToken, supplierFixture.ProductID)
	waitDockerOrderVisible(t, user, buyerToken, cancelled.OrderID)
	requireDockerOrderEvent(t, user, buyerWS, cancelled.OrderID, "", "unpaid")
	cancelled = cancelDockerBuyerOrder(t, user, buyerToken, cancelled.OrderID)
	require.Equal(t, "cancelled", cancelled.OrderStatus)
	requireDockerOrderEvent(t, user, buyerWS, cancelled.OrderID, "cancelled", "")

	requireDockerBuyerCannotSeeOtherOrder(t, user, otherToken, created.OrderID)
	select {
	case unexpected := <-otherEvents:
		t.Fatalf("其他买家不应收到订单 WebSocket 事件: %+v", unexpected)
	case <-time.After(300 * time.Millisecond):
	}
}

// requireDockerBuyerPublicFacade 验证买家可通过 user-service facade 查询基础资料。
func requireDockerBuyerPublicFacade(t *testing.T, user *integration.Suite, productID uint) {
	t.Helper()
	productsResponse := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getproducts", "", map[string]interface{}{})
	require.True(t, productsResponse.Success, productsResponse.ErrorMessage)
	var products []*supplierdto.Product
	require.NoError(t, json.Unmarshal(productsResponse.Data, &products))
	require.True(t, dockerProductExists(products, productID))

	suppliersResponse := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getsuppliers", "", map[string]interface{}{})
	require.True(t, suppliersResponse.Success, suppliersResponse.ErrorMessage)
	paymentTypesResponse := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getpaymenttypes", "", map[string]interface{}{})
	require.True(t, paymentTypesResponse.Success, paymentTypesResponse.ErrorMessage)
}

// requireDockerBuyerCannotSeeOtherOrder 验证其他买家不能查询到当前买家的订单。
func requireDockerBuyerCannotSeeOtherOrder(t *testing.T, user *integration.Suite, otherToken string, orderID uint) {
	t.Helper()
	orders := dockerBuyerOrders(t, user, otherToken)
	for _, order := range orders {
		require.NotEqual(t, orderID, order.OrderID)
	}
}

// dockerBuyerOrderByID 从买家订单列表中读取指定订单。
func dockerBuyerOrderByID(t *testing.T, user *integration.Suite, buyerToken string, orderID uint) orderdto.Order {
	t.Helper()
	orders := dockerBuyerOrders(t, user, buyerToken)
	for _, order := range orders {
		if order.OrderID == orderID {
			return order
		}
	}
	t.Fatalf("买家订单列表缺少订单 %d", orderID)
	return orderdto.Order{}
}

// dockerBuyerOrders 查询买家本人订单列表。
func dockerBuyerOrders(t *testing.T, user *integration.Suite, buyerToken string) []orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getorders", buyerToken, map[string]interface{}{"page": 1, "size": 20})
	require.True(t, response.Success, response.ErrorMessage)
	var orders []orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}
