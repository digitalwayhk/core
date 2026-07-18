// 本文件验证 06 all-in-one 集成场景下 Public API 的入口 facade 和内部调用边界。
// 用户服务可以对外提供商品目录 facade，供应商和订单服务的受限 Public API 不能被直接 HTTP 调用。
package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"testing"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	"github.com/stretchr/testify/require"
)

// TestProductFacade 验证用户服务通过内部调用读取供应商商品，并作为买家入口 facade 返回商品目录。
func TestProductFacade(t *testing.T) {
	product, _ := addProduct(t, "supplier-public")
	response := suites.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getproducts?code="+product.Code, "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []*supplierdto.Product
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 1)
	require.Equal(t, product.SupplierID, items[0].SupplierID)
}

// TestInternalPublicRoutesRejectDirectHTTP 验证供应商/订单服务的内部 Public 路由拒绝未信任的直接 HTTP 请求。
func TestInternalPublicRoutesRejectDirectHTTP(t *testing.T) {
	for _, target := range []struct {
		suiteURL string
		path     string
	}{
		{"supplier", "/api/shop-supplier/getsuppliers"},
		{"supplier", "/api/shop-supplier/getproducts"},
		{"order", "/api/shop-order/getpaymenttypes"},
		{"order", "/api/shop-order/createorder"},
	} {
		suite := suites.supplier
		if target.suiteURL == "order" {
			suite = suites.order
		}
		response := suite.RequestJSON(t, http.MethodGet, target.path, "", nil)
		require.False(t, response.Success, target.path)
	}
}
