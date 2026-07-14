package simpleshop_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicAPIs 验证商品列表的空条件、ID、名称、组合筛选与最小响应模型。
func TestPublicAPIs(t *testing.T) {
	adminToken := suite.TokenFor(t, "public-admin", 1)
	productName := fmt.Sprintf("公开商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "39.80")
	productID := integration.UintID(t, product.ID)

	all := suite.RequestJSON(t, http.MethodGet, "/api/shop/getproducts", "", nil)
	require.True(t, all.Success, all.ErrorMessage)
	var products []integration.ProductDTO
	require.NoError(t, json.Unmarshal(all.Data, &products))
	assert.Contains(t, integration.ProductNames(products), productName)
	assert.NotContains(t, string(all.Data), "hashCode")
	assert.NotContains(t, string(all.Data), "modelState")
	assert.NotContains(t, string(all.Data), "createdAt")

	idOnly := suite.RequestJSON(t, http.MethodGet, fmt.Sprintf("/api/shop/getproducts?id=%d", productID), "", nil)
	require.True(t, idOnly.Success, idOnly.ErrorMessage)
	require.NoError(t, json.Unmarshal(idOnly.Data, &products))
	require.Len(t, products, 1)
	assert.Equal(t, productName, products[0].Name)

	nameOnly := suite.RequestJSON(t, http.MethodGet, "/api/shop/getproducts?name="+url.QueryEscape(productName), "", nil)
	require.True(t, nameOnly.Success, nameOnly.ErrorMessage)
	require.NoError(t, json.Unmarshal(nameOnly.Data, &products))
	require.Len(t, products, 1)

	combinedPath := fmt.Sprintf("/api/shop/getproducts?id=%d&name=%s", productID, url.QueryEscape(productName))
	combined := suite.RequestJSON(t, http.MethodGet, combinedPath, "", nil)
	require.True(t, combined.Success, combined.ErrorMessage)
	require.NoError(t, json.Unmarshal(combined.Data, &products))
	require.Len(t, products, 1)
	assert.Equal(t, "39.8", products[0].Price)

	empty := suite.RequestJSON(t, http.MethodGet, fmt.Sprintf("/api/shop/getproducts?id=%d&name=%s", productID, url.QueryEscape("不匹配")), "", nil)
	require.NoError(t, json.Unmarshal(empty.Data, &products))
	assert.Empty(t, products)

	invalidID := suite.RequestJSON(t, http.MethodGet, "/api/shop/getproducts?id=invalid", "", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, invalidID.HTTPStatus)
	assert.Equal(t, "商品 ID 格式错误", invalidID.ErrorMessage)
}
