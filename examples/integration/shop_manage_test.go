package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProductManageCRUD 验证管理员商品增查改删和普通用户权限隔离。
func TestProductManageCRUD(t *testing.T) {
	adminToken := tokenFor(t, "manage-admin", 1)
	userToken := tokenFor(t, "manage-user", 0)
	invalidName := requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]string{
		"name":  "  ",
		"price": "19.90",
	})
	assert.Equal(t, http.StatusBadRequest, invalidName.HTTPStatus)
	assert.Equal(t, "商品名称不能为空", invalidName.ErrorMessage)
	invalidPrice := requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]string{
		"name":  "无效价格商品",
		"price": "0",
	})
	assert.Equal(t, http.StatusBadRequest, invalidPrice.HTTPStatus)
	assert.Equal(t, "商品价格必须大于 0", invalidPrice.ErrorMessage)
	product := addProduct(t, adminToken, "管理测试商品", "19.90")

	search := requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/search", adminToken, map[string]interface{}{
		"page": 1,
		"size": 100,
	})
	require.True(t, search.Success, search.ErrorMessage)
	var table struct {
		Rows []productDTO `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(search.Data, &table))
	assert.Contains(t, productNames(table.Rows), "管理测试商品")

	edit := requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id":    product.ID,
		"name":  "管理测试商品-已修改",
		"price": "29.90",
	})
	require.True(t, edit.Success, edit.ErrorMessage)

	denied := requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/search", userToken, map[string]int{"page": 1, "size": 10})
	assert.Equal(t, http.StatusUnauthorized, denied.HTTPStatus)

	remove := requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/remove", adminToken, map[string]interface{}{
		"id":    product.ID,
		"name":  "管理测试商品-已修改",
		"price": "29.90",
	})
	require.True(t, remove.Success, remove.ErrorMessage)
}

// TestOrderManageIsReadOnly 验证订单管理只注册 View 与 Search 路由。
func TestOrderManageIsReadOnly(t *testing.T) {
	adminToken := tokenFor(t, "order-admin", 1)
	search := requestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/search", adminToken, map[string]int{"page": 1, "size": 10})
	require.True(t, search.Success, search.ErrorMessage)

	response, err := http.Post(suite.baseURL+"/api/manage/shop/ordermanage/add", "application/json", nil)
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
}

// productNames 提取商品名称，简化列表包含关系断言。
func productNames(products []productDTO) []string {
	names := make([]string, 0, len(products))
	for _, product := range products {
		names = append(names, product.Name)
	}
	return names
}
