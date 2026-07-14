package simpleshop_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var suite *shopSuite

// TestMain 启动一个真实商城进程，供 Manage、Public 和 Private 三组测试共用。
func TestMain(m *testing.M) {
	created, err := startShopSuite()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	suite = created
	code := m.Run()
	if code != 0 {
		suite.PrintLog()
	}
	suite.Stop()
	os.Exit(code)
}

// TestManageAPIs 验证商品全部 CRUD、名称唯一性、权限隔离与订单只读管理面。
func TestManageAPIs(t *testing.T) {
	adminToken := suite.TokenFor(t, "manage-admin", 1)
	userToken := suite.TokenFor(t, "manage-user", 0)
	productName := fmt.Sprintf("管理商品-%d", time.Now().UnixNano())

	productView := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/view", adminToken, nil)
	require.True(t, productView.Success, productView.ErrorMessage)
	orderView := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/view", adminToken, nil)
	require.True(t, orderView.Success, orderView.ErrorMessage)

	invalidName := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]string{
		"name": "  ", "price": "19.90",
	})
	assert.Equal(t, http.StatusBadRequest, invalidName.HTTPStatus)
	assert.Equal(t, "商品名称不能为空", invalidName.ErrorMessage)

	product := suite.AddProduct(t, adminToken, productName, "19.90")
	duplicate := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]string{
		"name": productName, "price": "29.90",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, duplicate.HTTPStatus)
	assert.Equal(t, "商品名称不能重复", duplicate.ErrorMessage)

	secondProduct := suite.AddProduct(t, adminToken, productName+"-第二个", "29.90")
	require.NotEqual(t, product.ID, secondProduct.ID)
	duplicateEdit := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id": secondProduct.ID, "name": productName, "price": "29.90",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, duplicateEdit.HTTPStatus)
	afterDuplicateEdit := suite.RequestJSON(t, http.MethodGet, "/api/shop/getproducts?id="+secondProduct.ID, "", nil)
	var unchangedProducts []ProductDTO
	require.NoError(t, json.Unmarshal(afterDuplicateEdit.Data, &unchangedProducts))
	require.Len(t, unchangedProducts, 1)
	assert.Equal(t, productName+"-第二个", unchangedProducts[0].Name)

	search := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/search", adminToken, map[string]interface{}{
		"page": 1, "size": 100,
	})
	require.True(t, search.Success, search.ErrorMessage)
	var table struct {
		Rows []ProductDTO `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(search.Data, &table))
	assert.Contains(t, ProductNames(table.Rows), productName)

	updatedName := productName + "-已修改"
	edit := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id": product.ID, "name": updatedName, "price": "29.90",
	})
	require.True(t, edit.Success, edit.ErrorMessage)

	denied := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/search", userToken, map[string]int{"page": 1, "size": 10})
	assert.Equal(t, http.StatusUnauthorized, denied.HTTPStatus)

	orderSearch := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/search", adminToken, map[string]int{"page": 1, "size": 10})
	require.True(t, orderSearch.Success, orderSearch.ErrorMessage)
	for _, operation := range []string{"add", "edit", "remove"} {
		response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/"+operation, adminToken, map[string]interface{}{})
		assert.Equal(t, http.StatusNotFound, response.HTTPStatus, operation)
	}

	remove := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/remove", adminToken, map[string]interface{}{
		"id": product.ID, "name": updatedName, "price": "29.90",
	})
	require.True(t, remove.Success, remove.ErrorMessage)
	removeSecond := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/remove", adminToken, map[string]interface{}{
		"id": secondProduct.ID, "name": productName + "-第二个", "price": "29.90",
	})
	require.True(t, removeSecond.Success, removeSecond.ErrorMessage)
}
