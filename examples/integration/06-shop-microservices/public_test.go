package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"testing"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	"github.com/stretchr/testify/require"
)

func TestProductFacade(t *testing.T) {
	product, _ := addProduct(t, "supplier-public")
	response := suites.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getproducts?code="+product.Code, "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []*supplierdto.Product
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 1)
	require.Equal(t, product.SupplierID, items[0].SupplierID)
}

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
