package shopmicroservices_test

import (
	"encoding/json"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"strconv"
	"testing"
)

func TestManageAPIs(t *testing.T) {
	t.Run("PaymentTypeAndConfirmation", testPaymentTypeAndConfirmation)
	t.Run("SupplierManageRequiresPlatformAdmin", testSupplierManageRequiresPlatformAdmin)
}
func testPaymentTypeAndConfirmation(t *testing.T) {
	admin := suites.order.TokenFor(t, "platform-admin", 1)
	created := suites.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/add", admin, map[string]interface{}{"name": "集成支付", "code": "integration-pay", "enabled": true})
	require.True(t, created.Success, created.ErrorMessage)
	var paymentType struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(created.Data, &paymentType))
	paymentTypeID, err := strconv.ParseUint(paymentType.ID, 10, 64)
	require.NoError(t, err)
	product, _ := addProduct(t, "supplier-payment")
	address, userToken := addAddress(t, "buyer-payment")
	order := addOrder(t, userToken, product.ID, address.ID)
	paying := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", userToken, map[string]interface{}{"orderID": order.ID, "paymentTypeID": paymentTypeID})
	require.True(t, paying.Success, paying.ErrorMessage)
	var record orderdto.PaymentRecord
	require.NoError(t, json.Unmarshal(paying.Data, &record))
	confirmed := suites.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/confirmpayment", admin, map[string]interface{}{"paymentID": record.ID})
	require.True(t, confirmed.Success, confirmed.ErrorMessage)
	var paid orderdto.Order
	require.NoError(t, json.Unmarshal(confirmed.Data, &paid))
	assert.Equal(t, 2, paid.PaymentStatus)
}
func testSupplierManageRequiresPlatformAdmin(t *testing.T) {
	supplierManage := suites.supplier.TokenFor(t, "supplier-not-admin", 1)
	denied := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", supplierManage, map[string]interface{}{"page": 1, "size": 10})
	assert.False(t, denied.Success)
	admin := suites.supplier.TokenFor(t, "platform-admin", 1)
	allowed := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", admin, map[string]interface{}{"page": 1, "size": 10})
	assert.True(t, allowed.Success, allowed.ErrorMessage)
}
