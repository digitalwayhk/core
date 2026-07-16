package shopmicroservices_test

import (
	"encoding/json"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManageAPIs(t *testing.T) {
	t.Run("PaymentTypeAndConfirmation", testPaymentTypeAndConfirmation)
	t.Run("SupplierManageRequiresPlatformAdmin", testSupplierManageRequiresPlatformAdmin)
	t.Run("SupplierChangeInvalidatesProductCache", testSupplierChangeInvalidatesProductCache)
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

func testSupplierChangeInvalidatesProductCache(t *testing.T) {
	userID := "supplier-cache-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	product, _ := addProduct(t, userID)
	productPath := "/api/shop-user/getproducts?code=" + product.Code
	cached := suites.user.RequestJSON(t, http.MethodGet, productPath, "", nil)
	require.True(t, cached.Success, cached.ErrorMessage)
	assert.Contains(t, string(cached.Data), product.Code)

	admin := suites.supplier.TokenFor(t, "platform-admin", 1)
	searched := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/search", admin, map[string]interface{}{"page": 1, "size": 100})
	require.True(t, searched.Success, searched.ErrorMessage)
	var table struct {
		Rows []struct {
			ID     string `json:"id"`
			UserID string `json:"userID"`
			Name   string `json:"name"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(searched.Data, &table))
	var supplierID, supplierName string
	for _, row := range table.Rows {
		if row.UserID == userID {
			supplierID, supplierName = row.ID, row.Name
			break
		}
	}
	require.NotEmpty(t, supplierID)
	edited := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/edit", admin, map[string]interface{}{"id": supplierID, "name": supplierName, "enabled": false})
	require.True(t, edited.Success, edited.ErrorMessage)
	require.Eventually(t, func() bool {
		response := suites.user.RequestJSON(t, http.MethodGet, productPath, "", nil)
		return response.Success && !jsonContains(response.Data, product.Code)
	}, 5*time.Second, 25*time.Millisecond)
}

func jsonContains(data json.RawMessage, value string) bool {
	return strings.Contains(string(data), value)
}
