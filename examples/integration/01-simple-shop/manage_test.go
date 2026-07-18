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

// TestManageAPIs 按 command 运行全部 Manage API 集成测试。
func TestManageAPIs(t *testing.T) {
	t.Run("ProductManageView", testProductManageViewCommand)
	t.Run("ProductManageAdd", testProductManageAddCommand)
	t.Run("ProductManageSearch", testProductManageSearchCommand)
	t.Run("ProductManageEdit", testProductManageEditCommand)
	t.Run("ProductManageRemove", testProductManageRemoveCommand)
	t.Run("OrderManageView", testOrderManageViewCommand)
	t.Run("OrderManageSearch", testOrderManageSearchCommand)
	t.Run("OrderManageAddNotRegistered", testOrderManageAddCommandNotRegistered)
	t.Run("OrderManageEditNotRegistered", testOrderManageEditCommandNotRegistered)
	t.Run("OrderManageRemoveNotRegistered", testOrderManageRemoveCommandNotRegistered)
	t.Run("OrderManageSearchAndForeignSearch", testOrderManageSearchAndForeignSearch)
}

// testProductManageViewCommand 验证商品管理元数据可被管理员读取。
func testProductManageViewCommand(t *testing.T) {
	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/view", suite.TokenFor(t, "product-view-admin", 1), nil)
	require.True(t, response.Success, response.ErrorMessage)
}

// testProductManageAddCommand 验证商品新增、名称校验和名称唯一性。
func testProductManageAddCommand(t *testing.T) {
	adminToken := suite.TokenFor(t, "product-add-admin", 1)
	invalid := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]string{
		"name": "  ", "price": "19.90",
	})
	assert.Equal(t, http.StatusBadRequest, invalid.HTTPStatus)
	assert.Equal(t, "商品名称不能为空", invalid.ErrorMessage)

	productName := fmt.Sprintf("新增商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "19.90")
	require.NotEmpty(t, product.ID)

	duplicate := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]string{
		"name": productName, "price": "29.90",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, duplicate.HTTPStatus)
	assert.Equal(t, "商品名称不能重复", duplicate.ErrorMessage)
}

// testProductManageSearchCommand 验证商品管理列表查询与管理权限。
func testProductManageSearchCommand(t *testing.T) {
	adminToken := suite.TokenFor(t, "product-search-admin", 1)
	productName := fmt.Sprintf("搜索商品-%d", time.Now().UnixNano())
	suite.AddProduct(t, adminToken, productName, "19.90")

	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/search", adminToken, map[string]interface{}{
		"page": 1, "size": 100,
	})
	require.True(t, response.Success, response.ErrorMessage)
	var table struct {
		Rows []ProductDTO `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &table))
	assert.Contains(t, ProductNames(table.Rows), productName)

	denied := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/search", suite.TokenFor(t, "product-search-user", 0), map[string]int{"page": 1, "size": 10})
	assert.Equal(t, http.StatusUnauthorized, denied.HTTPStatus)
}

// testProductManageEditCommand 验证商品修改成功，且修改时不允许产生重复名称。
func testProductManageEditCommand(t *testing.T) {
	adminToken := suite.TokenFor(t, "product-edit-admin", 1)
	productName := fmt.Sprintf("修改商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "19.90")
	secondName := productName + "-第二个"
	secondProduct := suite.AddProduct(t, adminToken, secondName, "29.90")

	duplicate := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id": secondProduct.ID, "name": productName, "price": "29.90",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, duplicate.HTTPStatus)
	unchanged := suite.GetProducts(t, "?id="+secondProduct.ID)
	require.Len(t, unchanged, 1)
	assert.Equal(t, secondName, unchanged[0].Name)

	updatedName := productName + "-已修改"
	edited := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/edit", adminToken, map[string]interface{}{
		"id": product.ID, "name": updatedName, "price": "29.90",
	})
	require.True(t, edited.Success, edited.ErrorMessage)
	updated := suite.GetProducts(t, "?id="+product.ID)
	require.Len(t, updated, 1)
	assert.Equal(t, updatedName, updated[0].Name)
	assert.Equal(t, "29.9", updated[0].Price)
}

// testProductManageRemoveCommand 验证商品可被物理删除。
func testProductManageRemoveCommand(t *testing.T) {
	adminToken := suite.TokenFor(t, "product-remove-admin", 1)
	productName := fmt.Sprintf("删除商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "19.90")

	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/remove", adminToken, map[string]interface{}{
		"id": product.ID, "name": productName, "price": "19.90",
	})
	require.True(t, response.Success, response.ErrorMessage)
	assert.Empty(t, suite.GetProducts(t, "?id="+product.ID))
}

// testOrderManageViewCommand 验证订单管理元数据可被管理员读取。
func testOrderManageViewCommand(t *testing.T) {
	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/view", suite.TokenFor(t, "order-view-admin", 1), nil)
	require.True(t, response.Success, response.ErrorMessage)
}

// testOrderManageSearchCommand 验证管理员只读查询订单列表。
func testOrderManageSearchCommand(t *testing.T) {
	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/search", suite.TokenFor(t, "order-search-admin", 1), map[string]int{"page": 1, "size": 10})
	require.True(t, response.Success, response.ErrorMessage)
}

// testOrderManageSearchAndForeignSearch 演示管理查询能力：
// 先用订单主查询查到刚创建的订单，再用订单视图中的商品外键元数据查到对应商品。
func testOrderManageSearchAndForeignSearch(t *testing.T) {
	adminToken := suite.TokenFor(t, "order-query-admin", 1)
	buyerToken := suite.TokenFor(t, "order-query-buyer", 0)
	productName := fmt.Sprintf("订单查询商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, adminToken, productName, "39.90")
	order := suite.AddOrder(t, buyerToken, product.ID, 3)

	mainSearch := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/search", adminToken, map[string]interface{}{
		"page": 1,
		"size": 20,
		"whereList": []map[string]interface{}{
			{"name": "ID", "value": order.ID},
		},
	})
	require.True(t, mainSearch.Success, mainSearch.ErrorMessage)
	var orderTable struct {
		Rows  []OrderDTO `json:"rows"`
		Total int64      `json:"total"`
	}
	require.NoError(t, json.Unmarshal(mainSearch.Data, &orderTable), string(mainSearch.Data))
	require.Equal(t, int64(1), orderTable.Total)
	require.Len(t, orderTable.Rows, 1)
	require.Equal(t, order.ID, orderTable.Rows[0].ID)
	require.Equal(t, productName, orderTable.Rows[0].ProductName)

	viewResponse := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/view", adminToken, nil)
	require.True(t, viewResponse.Success, viewResponse.ErrorMessage)
	var viewModel struct {
		Fields []map[string]interface{} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(viewResponse.Data, &viewModel), string(viewResponse.Data))
	productField := fieldByName(viewModel.Fields, "productID")
	require.NotNil(t, productField, string(viewResponse.Data))
	require.NotNil(t, productField["foreign"], string(viewResponse.Data))

	foreignSearch := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/search", adminToken, map[string]interface{}{
		"page":    1,
		"size":    20,
		"field":   productField,
		"foreign": productField["foreign"],
		"whereList": []map[string]interface{}{
			{"name": "ID", "value": product.ID},
		},
	})
	require.True(t, foreignSearch.Success, foreignSearch.ErrorMessage)
	var productTable struct {
		Rows  []ProductDTO `json:"rows"`
		Total int64        `json:"total"`
	}
	require.NoError(t, json.Unmarshal(foreignSearch.Data, &productTable), string(foreignSearch.Data))
	require.Equal(t, int64(1), productTable.Total)
	require.Len(t, productTable.Rows, 1)
	require.Equal(t, product.ID, productTable.Rows[0].ID)
	require.Equal(t, productName, productTable.Rows[0].Name)
}

// testOrderManageAddCommandNotRegistered 验证订单管理不暴露新增 command。
func testOrderManageAddCommandNotRegistered(t *testing.T) {
	assertOrderManageCommandNotRegistered(t, "add")
}

// testOrderManageEditCommandNotRegistered 验证订单管理不暴露修改 command。
func testOrderManageEditCommandNotRegistered(t *testing.T) {
	assertOrderManageCommandNotRegistered(t, "edit")
}

// testOrderManageRemoveCommandNotRegistered 验证订单管理不暴露删除 command。
func testOrderManageRemoveCommandNotRegistered(t *testing.T) {
	assertOrderManageCommandNotRegistered(t, "remove")
}

func assertOrderManageCommandNotRegistered(t *testing.T, command string) {
	t.Helper()
	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/shop/ordermanage/"+command, suite.TokenFor(t, "order-readonly-admin-"+command, 1), map[string]interface{}{})
	assert.Equal(t, http.StatusNotFound, response.HTTPStatus)
}

func fieldByName(fields []map[string]interface{}, name string) map[string]interface{} {
	for _, field := range fields {
		if field["field"] == name || field["porpfield"] == name {
			return field
		}
	}
	return nil
}
