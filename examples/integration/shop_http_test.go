package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicProductsAndPrivateOrders 验证商品筛选、价格快照和订单所有权边界。
func TestPublicProductsAndPrivateOrders(t *testing.T) {
	adminToken := tokenFor(t, "http-admin", 1)
	userAToken := tokenFor(t, "http-user-a", 0)
	userBToken := tokenFor(t, "http-user-b", 0)
	productName := fmt.Sprintf("HTTP 筛选商品-%d", time.Now().UnixNano())
	product := addProduct(t, adminToken, productName, "39.80")
	productID := uintID(t, product.ID)

	allProducts := requestJSON(t, http.MethodGet, "/api/shop/getproducts", "", nil)
	require.True(t, allProducts.Success, allProducts.ErrorMessage)
	var products []productDTO
	require.NoError(t, json.Unmarshal(allProducts.Data, &products))
	assert.Contains(t, productNames(products), productName)
	idOnly := requestJSON(t, http.MethodGet, fmt.Sprintf("/api/shop/getproducts?id=%d", productID), "", nil)
	require.NoError(t, json.Unmarshal(idOnly.Data, &products))
	require.Len(t, products, 1)
	nameOnly := requestJSON(t, http.MethodGet, "/api/shop/getproducts?name="+url.QueryEscape(productName), "", nil)
	require.NoError(t, json.Unmarshal(nameOnly.Data, &products))
	require.Len(t, products, 1)

	filterPath := fmt.Sprintf("/api/shop/getproducts?id=%d&name=%s", productID, url.QueryEscape(productName))
	filtered := requestJSON(t, http.MethodGet, filterPath, "", nil)
	require.True(t, filtered.Success, filtered.ErrorMessage)
	require.NoError(t, json.Unmarshal(filtered.Data, &products))
	require.Len(t, products, 1)
	assert.Equal(t, "39.8", products[0].Price)

	created := requestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": productID,
		"quantity":  2,
	})
	require.True(t, created.Success, created.ErrorMessage)
	var order orderDTO
	require.NoError(t, json.Unmarshal(created.Data, &order))
	assert.Equal(t, "http-user-a", order.UserID)
	assert.Equal(t, productName, order.ProductName)
	assert.Equal(t, "39.8", order.UnitPrice)
	missing := requestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": uint(999999999),
		"quantity":  1,
	})
	assert.Equal(t, http.StatusUnprocessableEntity, missing.HTTPStatus)
	assert.Equal(t, "商品不存在", missing.ErrorMessage)

	invalid := requestJSON(t, http.MethodPost, "/api/shop/addorder", userAToken, map[string]interface{}{
		"productID": productID,
		"quantity":  0,
	})
	assert.Equal(t, http.StatusBadRequest, invalid.HTTPStatus, invalid.Body)
	assert.False(t, invalid.Success)
	assert.Equal(t, "订单数量必须大于 0", invalid.ErrorMessage)

	ordersA := requestJSON(t, http.MethodGet, "/api/shop/getorders", userAToken, nil)
	require.True(t, ordersA.Success, ordersA.ErrorMessage)
	var userAOrders []orderDTO
	require.NoError(t, json.Unmarshal(ordersA.Data, &userAOrders))
	assert.Contains(t, orderIDs(userAOrders), order.ID)
	requestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id":    product.ID,
		"name":  productName + "-新名称",
		"price": "88.00",
	})
	ordersAfterProductEdit := requestJSON(t, http.MethodGet, "/api/shop/getorders", userAToken, nil)
	require.NoError(t, json.Unmarshal(ordersAfterProductEdit.Data, &userAOrders))
	for _, savedOrder := range userAOrders {
		if savedOrder.ID == order.ID {
			assert.Equal(t, productName, savedOrder.ProductName)
			assert.Equal(t, "39.8", savedOrder.UnitPrice)
		}
	}

	ordersB := requestJSON(t, http.MethodGet, "/api/shop/getorders", userBToken, nil)
	require.True(t, ordersB.Success, ordersB.ErrorMessage)
	var userBOrders []orderDTO
	require.NoError(t, json.Unmarshal(ordersB.Data, &userBOrders))
	assert.NotContains(t, orderIDs(userBOrders), order.ID)

	forbidden := requestJSON(t, http.MethodPost, "/api/shop/deleteorder", userBToken, map[string]string{"id": order.ID})
	assert.Equal(t, http.StatusUnprocessableEntity, forbidden.HTTPStatus)
	assert.False(t, forbidden.Success)
	assert.Equal(t, "订单不存在或无权操作", forbidden.ErrorMessage)

	deleted := requestJSON(t, http.MethodPost, "/api/shop/deleteorder", userAToken, map[string]string{"id": order.ID})
	require.True(t, deleted.Success, deleted.ErrorMessage)

	afterDelete := requestJSON(t, http.MethodGet, "/api/shop/getorders", userAToken, nil)
	require.NoError(t, json.Unmarshal(afterDelete.Data, &userAOrders))
	assert.NotContains(t, orderIDs(userAOrders), order.ID)
}

// orderIDs 提取订单 ID，简化所有权与删除结果断言。
func orderIDs(orders []orderDTO) []string {
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
	}
	return ids
}
