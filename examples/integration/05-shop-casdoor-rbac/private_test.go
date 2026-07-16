package casdoorrbacshop_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateAPIs(t *testing.T) {
	t.Run("AddOrder", testAddOrder)
	t.Run("RejectDisabledSupplierOrder", testRejectDisabledSupplierOrder)
	t.Run("GetOrders", testGetOrders)
	t.Run("RejectCrossUserOrderMutations", testRejectCrossUserOrderMutations)
	t.Run("DeleteOrder", testDeleteOrder)
	t.Run("CreatePayment", testCreatePayment)
	t.Run("CancelOrder", testCancelOrder)
	t.Run("WebSocketPaymentFlow", testWebSocketPaymentFlow)
}

func testRejectCrossUserOrderMutations(t *testing.T) {
	admin := suite.TokenFor(t, "cross-user-admin", 1)
	userA := suite.TokenFor(t, "cross-user-a", 0)
	userB := suite.TokenFor(t, "cross-user-b", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("跨用户商品-%d", time.Now().UnixNano()), "25.00")
	paymentType := suite.AddPaymentType(t, admin, fmt.Sprintf("cross-user-pay-%d", time.Now().UnixNano()), "跨用户支付", true)
	order := suite.AddOrder(t, userA, uintID(t, product.ID), 1)

	deleted := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/deleteorder", userB, map[string]interface{}{"id": order.ID})
	assert.False(t, deleted.Success)
	assert.Contains(t, deleted.ErrorMessage, "订单不存在或无权操作")

	paidByOther := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/createpayment", userB, map[string]interface{}{
		"orderID": order.ID, "paymentTypeID": paymentType.ID,
	})
	assert.False(t, paidByOther.Success)
	assert.Contains(t, paidByOther.ErrorMessage, "订单不存在或无权操作")

	pending := suite.CreatePayment(t, userA, order.ID, paymentType.ID)
	suite.PaymentCommand(t, admin, "confirmpayment", pending.PaymentID)
	cancelledByOther := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/cancelorder", userB, map[string]interface{}{"id": order.ID})
	assert.False(t, cancelledByOther.Success)
	assert.Contains(t, cancelledByOther.ErrorMessage, "订单不存在或无权操作")
}

func testAddOrder(t *testing.T) {
	admin := suite.TokenFor(t, "private-add-admin", 1)
	user := suite.TokenFor(t, "private-add-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("下单商品-%d", time.Now().UnixNano()), "15.00")
	unauthorized := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/addorder", "", map[string]interface{}{"productID": uintID(t, product.ID), "quantity": 1})
	assert.Equal(t, http.StatusUnauthorized, unauthorized.HTTPStatus)
	order := suite.AddOrder(t, user, uintID(t, product.ID), 2)
	assert.Equal(t, "30", order.Amount)
	assert.Equal(t, "未支付", order.PaymentStatusName)
	assert.Equal(t, product.SupplierID, order.SupplierID)
	assert.NotEmpty(t, order.SupplierCode)
	assert.NotEmpty(t, order.SupplierName)
}

func testRejectDisabledSupplierOrder(t *testing.T) {
	admin := suite.TokenFor(t, "private-disabled-supplier-admin", 1)
	user := suite.TokenFor(t, "private-disabled-supplier-user", 0)
	suffix := time.Now().UnixNano()
	supplier := suite.AddSupplier(t, admin, fmt.Sprintf("private-disabled-supplier-%d", suffix), "下单禁用供应商", true)
	product := suite.AddProductForSupplier(t, admin, fmt.Sprintf("private-disabled-product-%d", suffix), "下单禁用商品", "11.00", uintID(t, supplier.ID), true)
	suite.SetBaseDataEnabled(t, admin, "suppliermanage", supplier.ID, false)

	rejected := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/addorder", user, map[string]interface{}{"productID": uintID(t, product.ID), "quantity": 1})
	assert.False(t, rejected.Success)
	assert.Contains(t, rejected.ErrorMessage, "供应商已禁用")
}

func testGetOrders(t *testing.T) {
	admin := suite.TokenFor(t, "private-get-admin", 1)
	userA := suite.TokenFor(t, "private-get-a", 0)
	userB := suite.TokenFor(t, "private-get-b", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("查询订单-%d", time.Now().UnixNano()), "16.00")
	order := suite.AddOrder(t, userA, uintID(t, product.ID), 1)
	assert.Contains(t, orderIDs(suite.GetOrders(t, userA)), order.ID)
	assert.NotContains(t, orderIDs(suite.GetOrders(t, userB)), order.ID)
}

func testDeleteOrder(t *testing.T) {
	admin := suite.TokenFor(t, "private-delete-admin", 1)
	user := suite.TokenFor(t, "private-delete-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("删除订单-%d", time.Now().UnixNano()), "17.00")
	order := suite.AddOrder(t, user, uintID(t, product.ID), 1)
	deleted := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/deleteorder", user, map[string]interface{}{"id": order.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)
	assert.NotContains(t, orderIDs(suite.GetOrders(t, user)), order.ID)
}

func testCreatePayment(t *testing.T) {
	admin := suite.TokenFor(t, "private-pay-admin", 1)
	user := suite.TokenFor(t, "private-pay-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("发起支付-%d", time.Now().UnixNano()), "18.00")
	disabled := suite.AddPaymentType(t, admin, fmt.Sprintf("disabled-%d", time.Now().UnixNano()), "禁用支付", false)
	enabled := suite.AddPaymentType(t, admin, fmt.Sprintf("enabled-%d", time.Now().UnixNano()), "启用支付", true)
	order := suite.AddOrder(t, user, uintID(t, product.ID), 1)
	rejected := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/createpayment", user, map[string]interface{}{"orderID": order.ID, "paymentTypeID": disabled.ID})
	assert.False(t, rejected.Success)
	paying := suite.CreatePayment(t, user, order.ID, enabled.ID)
	assert.Equal(t, "支付中", paying.PaymentStatusName)
	second := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/createpayment", user, map[string]interface{}{"orderID": order.ID, "paymentTypeID": enabled.ID})
	assert.False(t, second.Success)
	deleteAttempt := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/deleteorder", user, map[string]interface{}{"id": order.ID})
	assert.Contains(t, deleteAttempt.ErrorMessage, "支付处理中")
}

func testCancelOrder(t *testing.T) {
	admin := suite.TokenFor(t, "private-cancel-admin", 1)
	user := suite.TokenFor(t, "private-cancel-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("撤销订单-%d", time.Now().UnixNano()), "19.00")
	typeItem := suite.AddPaymentType(t, admin, fmt.Sprintf("cancel-%d", time.Now().UnixNano()), "撤销支付", true)
	order := suite.AddOrder(t, user, uintID(t, product.ID), 1)
	paying := suite.CreatePayment(t, user, order.ID, typeItem.ID)
	paid := suite.PaymentCommand(t, admin, "confirmpayment", paying.PaymentID)
	assert.Equal(t, "已支付", paid.PaymentStatusName)
	deleted := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/deleteorder", user, map[string]interface{}{"id": order.ID})
	assert.Contains(t, deleted.ErrorMessage, "已支付订单不能删除")
	cancelled := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/cancelorder", user, map[string]interface{}{"id": order.ID})
	require.True(t, cancelled.Success, cancelled.ErrorMessage)
	result := decodeOrder(t, cancelled.Data)
	assert.Equal(t, "撤销处理中", result.StatusName)
	assert.Equal(t, "退款中", result.PaymentStatusName)
}

func testWebSocketPaymentFlow(t *testing.T) {
	admin := suite.TokenFor(t, "ws-payment-admin", 1)
	userA := suite.TokenFor(t, "ws-payment-a", 0)
	userB := suite.TokenFor(t, "ws-payment-b", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("WS 支付-%d", time.Now().UnixNano()), "21.00")
	typeItem := suite.AddPaymentType(t, admin, fmt.Sprintf("ws-%d", time.Now().UnixNano()), "WS 支付", true)
	connectionA := suite.ConnectAndSubscribe(t, userA)
	defer connectionA.Close()
	connectionB := suite.ConnectAndSubscribe(t, userB)
	defer connectionB.Close()
	messagesB := suite.StreamWebSocket(t, connectionB)

	order := suite.AddOrder(t, userA, uintID(t, product.ID), 1)
	assert.Equal(t, "created", suite.ReadOrderEvent(t, connectionA).Action)
	paying := suite.CreatePayment(t, userA, order.ID, typeItem.ID)
	assert.Equal(t, "payment_pending", suite.ReadOrderEvent(t, connectionA).Action)
	suite.PaymentCommand(t, admin, "confirmpayment", paying.PaymentID)
	assert.Equal(t, "paid", suite.ReadOrderEvent(t, connectionA).Action)
	cancel := suite.RequestJSON(t, http.MethodPost, "/api/casdoorrbacshop/cancelorder", userA, map[string]interface{}{"id": order.ID})
	require.True(t, cancel.Success, cancel.ErrorMessage)
	assert.Equal(t, "refund_pending", suite.ReadOrderEvent(t, connectionA).Action)
	suite.PaymentCommand(t, admin, "confirmrefund", paying.PaymentID)
	assert.Equal(t, "cancelled", suite.ReadOrderEvent(t, connectionA).Action)

	select {
	case message := <-messagesB:
		t.Fatalf("其他用户不应收到订单事件: %+v", message)
	case <-time.After(250 * time.Millisecond):
	}
}

// orderIDs 提取订单 ID 以简化用户隔离断言。
func orderIDs(items []OrderDTO) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.ID)
	}
	return result
}
