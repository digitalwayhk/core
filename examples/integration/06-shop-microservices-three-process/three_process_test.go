package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThreeProcessDiscoveryAndRemoteCalls(t *testing.T) {
	user, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/user", BinaryName: "shop-user", TempPrefix: "core-shop-user-", ServiceCount: 2, ServiceIndex: 1, Arguments: []string{"-view", "0", "-socket", "29080"}})
	require.NoError(t, err)
	defer user.Stop()
	supplier, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/supplier", BinaryName: "shop-supplier", TempPrefix: "core-shop-supplier-", ServiceCount: 2, ServiceIndex: 1, Arguments: []string{"-view", "0", "-socket", "29081"}})
	require.NoError(t, err)
	defer supplier.Stop()
	order, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/order", BinaryName: "shop-order", TempPrefix: "core-shop-order-", ServiceCount: 2, ServiceIndex: 1, Arguments: []string{"-view", "0", "-socket", "29082"}})
	require.NoError(t, err)
	defer order.Stop()
	waitProcessReady(t, user, "/api/shop-user/getproducts")
	waitProcessReady(t, supplier, "/api/shop-supplier/getproducts")

	supplierToken := supplier.TokenFor(t, "supplier-remote", 0)
	createdProduct := supplier.RequestJSON(t, http.MethodPost, "/api/shop-supplier/addproduct", supplierToken, map[string]interface{}{"name": "远程商品", "code": "remote-product", "price": "9.90"})
	require.True(t, createdProduct.Success, createdProduct.ErrorMessage)
	var product supplierdto.Product
	require.NoError(t, json.Unmarshal(createdProduct.Data, &product))
	updated := supplier.RequestJSON(t, http.MethodPost, "/api/shop-supplier/setproduct", supplierToken, map[string]interface{}{"productID": product.ID, "enabled": true})
	require.True(t, updated.Success, updated.ErrorMessage)

	userToken := user.TokenFor(t, "buyer-remote", 0)
	createdAddress := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addaddress", userToken, map[string]interface{}{"recipient": "远程用户", "detail": "2 号"})
	require.True(t, createdAddress.Success, createdAddress.ErrorMessage)
	var address userdto.Address
	require.NoError(t, json.Unmarshal(createdAddress.Data, &address))
	createdOrder := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", userToken, map[string]interface{}{"productID": product.ID, "quantity": 3, "addressID": address.ID})
	require.True(t, createdOrder.Success, createdOrder.ErrorMessage)
	var result orderdto.Order
	require.NoError(t, json.Unmarshal(createdOrder.Data, &result))
	assert.Equal(t, "supplier-remote", result.Product.SupplierID)
	assert.Equal(t, 3, result.Quantity)
}

func waitProcessReady(t *testing.T, suite *integration.Suite, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := suite.DoJSON(http.MethodGet, path, "", nil)
		if err == nil && response.Success {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	suite.PrintLog()
	t.Fatalf("等待进程启动超时: %s", path)
}
