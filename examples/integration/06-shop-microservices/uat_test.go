package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUATBuyerOrderLifecycle 从最终用户视角验证跨服务活动产生的业务事实。
func TestUATBuyerOrderLifecycle(t *testing.T) {
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	product, supplierToken := addProduct(t, "uat-supplier-"+suffix)
	address, buyerToken := addAddress(t, "uat-buyer-"+suffix)
	otherBuyerToken := suites.user.TokenFor(t, "uat-other-"+suffix, 0)

	created := addOrder(t, buyerToken, product.ID, address.ID)
	assert.Equal(t, product.ID, created.Product.ProductID)
	assert.True(t, created.Product.UnitPrice.Equal(decimal.RequireFromString("12.50")))
	assert.True(t, created.TotalAmount.Equal(decimal.NewFromInt(25)))
	assert.Equal(t, address.ID, created.Address.AddressID)

	changedPrice := suites.supplier.RequestJSON(t, http.MethodPost, "/api/shop-supplier/setproduct", supplierToken,
		map[string]interface{}{"productID": product.ID, "price": "99.00"})
	require.True(t, changedPrice.Success, changedPrice.ErrorMessage)

	require.Eventually(t, func() bool {
		orders := getBuyerOrders(t, buyerToken)
		return len(orders) == 1 && orders[0].ID == created.ID &&
			orders[0].Product.UnitPrice.Equal(decimal.RequireFromString("12.50")) &&
			orders[0].TotalAmount.Equal(decimal.NewFromInt(25))
	}, 5*time.Second, 25*time.Millisecond)
	assert.Empty(t, getBuyerOrders(t, otherBuyerToken))

	paymentType := addPaymentType(t, "uat-pay-"+suffix)
	paying := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", buyerToken,
		map[string]interface{}{"orderID": created.ID, "paymentTypeID": paymentType.ID})
	require.True(t, paying.Success, paying.ErrorMessage)
	var payment orderdto.PaymentRecord
	require.NoError(t, json.Unmarshal(paying.Data, &payment))
	assert.Equal(t, created.ID, payment.OrderID)
	assert.Equal(t, paymentType.ID, payment.PaymentTypeID)
	assert.True(t, payment.Amount.Equal(decimal.NewFromInt(25)))

	adminToken := suites.order.TokenFor(t, "platform-admin", 1)
	confirmed := suites.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/confirmpayment", adminToken,
		map[string]interface{}{"paymentID": payment.ID})
	require.True(t, confirmed.Success, confirmed.ErrorMessage)
	require.Eventually(t, func() bool {
		buyerOrders := getBuyerOrders(t, buyerToken)
		supplierOrders := getSupplierOrders(t, supplierToken)
		return len(buyerOrders) == 1 && len(supplierOrders) == 1 &&
			buyerOrders[0].PaymentStatus == 2 && buyerOrders[0].PaymentID == payment.ID &&
			supplierOrders[0].PaymentStatus == 2
	}, 5*time.Second, 25*time.Millisecond)

	cancelled := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/deleteorder", buyerToken,
		map[string]interface{}{"orderID": created.ID})
	require.True(t, cancelled.Success, cancelled.ErrorMessage)
	require.Eventually(t, func() bool {
		buyerOrders := getBuyerOrders(t, buyerToken)
		supplierOrders := getSupplierOrders(t, supplierToken)
		return len(buyerOrders) == 1 && len(supplierOrders) == 1 &&
			buyerOrders[0].Status == 1 && supplierOrders[0].Status == 1
	}, 5*time.Second, 25*time.Millisecond)
}

func getBuyerOrders(t *testing.T, token string) []*orderdto.Order {
	t.Helper()
	response := suites.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getorders", token, nil)
	require.True(t, response.Success, response.ErrorMessage)
	var orders []*orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}

func getSupplierOrders(t *testing.T, token string) []*orderdto.SupplierOrder {
	t.Helper()
	response := suites.supplier.RequestJSON(t, http.MethodGet, "/api/shop-supplier/getorders", token, nil)
	require.True(t, response.Success, response.ErrorMessage)
	var orders []*orderdto.SupplierOrder
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}
