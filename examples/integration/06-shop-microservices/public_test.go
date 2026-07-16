package shopmicroservices_test

import (
	"encoding/json"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestPublicAPIs(t *testing.T) { t.Run("ProductFacade", testProductFacade) }
func testProductFacade(t *testing.T) {
	product, _ := addProduct(t, "supplier-public")
	response := suites.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getproducts?code="+product.Code, "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []*supplierdto.Product
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 1)
	assert.Equal(t, "supplier-public", items[0].SupplierID)
}
