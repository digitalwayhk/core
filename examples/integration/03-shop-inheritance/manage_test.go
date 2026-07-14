package inheritanceshop_test

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

func TestManageAPIs(t *testing.T) {
	t.Run("SupplierCRUDAndReadOnlyProducts", testSupplierCRUDAndReadOnlyProducts)
	t.Run("RejectRemovingUsedSupplier", testRejectRemovingUsedSupplier)
	t.Run("RejectEnablingProductForDisabledSupplier", testRejectEnablingProductForDisabledSupplier)
	t.Run("ProductCRUD", testProductCRUD)
	t.Run("RejectRemovingUsedProduct", testRejectRemovingUsedProduct)
	t.Run("PaymentTypeCRUDAndStateCommands", testPaymentTypeCRUDAndStateCommands)
	t.Run("RejectRemovingUsedPaymentType", testRejectRemovingUsedPaymentType)
	t.Run("OrderManageReadOnly", testOrderManageReadOnly)
	t.Run("PaymentRecordCommands", testPaymentRecordCommands)
}

func testRejectEnablingProductForDisabledSupplier(t *testing.T) {
	admin := suite.TokenFor(t, "disabled-supplier-product-admin", 1)
	suffix := time.Now().UnixNano()
	supplier := suite.AddSupplier(t, admin, fmt.Sprintf("disabled-enable-supplier-%d", suffix), "禁用启用供应商", true)
	product := suite.AddProductForSupplier(t, admin, fmt.Sprintf("disabled-enable-product-%d", suffix), "待重新启用商品", "12.80", uintID(t, supplier.ID), true)
	suite.SetBaseDataEnabled(t, admin, "productmanage", product.ID, false)
	suite.SetBaseDataEnabled(t, admin, "suppliermanage", supplier.ID, false)

	enabled := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/productmanage/enablebasedata", admin, map[string]interface{}{"id": product.ID})
	assert.False(t, enabled.Success)
	assert.Contains(t, enabled.ErrorMessage, "供应商已禁用")
}

func testSupplierCRUDAndReadOnlyProducts(t *testing.T) {
	token := suite.TokenFor(t, "manage-supplier", 1)
	view := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/suppliermanage/view", token, nil)
	require.True(t, view.Success, view.ErrorMessage)
	assert.Contains(t, string(view.Data), `"name":"Products"`)
	assert.Contains(t, string(view.Data), `"isadd":false`)
	assert.Contains(t, string(view.Data), `"isedit":false`)
	assert.Contains(t, string(view.Data), `"isremove":false`)

	suffix := time.Now().UnixNano()
	supplier := suite.AddSupplier(t, token, fmt.Sprintf("manage-supplier-%d", suffix), "管理供应商", false)
	suite.SetBaseDataEnabled(t, token, "suppliermanage", supplier.ID, true)
	product := suite.AddProductForSupplier(t, token, fmt.Sprintf("manage-child-product-%d", suffix), "子表商品", "8.80", uintID(t, supplier.ID), true)
	childSearch := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/suppliermanage/search", token, map[string]interface{}{
		"parent":     map[string]interface{}{"id": supplier.ID},
		"childmodel": map[string]interface{}{"name": "Products", "foreignKey": "SupplierID"},
		"page":       1, "size": 20,
	})
	require.True(t, childSearch.Success, childSearch.ErrorMessage)
	assert.Contains(t, string(childSearch.Data), product.ID)
	suite.SetBaseDataEnabled(t, token, "suppliermanage", supplier.ID, false)
	edited := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/suppliermanage/edit", token, map[string]interface{}{
		"id": supplier.ID, "code": supplier.Code, "name": "管理供应商-改", "enabled": false, "description": "修改说明",
	})
	require.True(t, edited.Success, edited.ErrorMessage)
	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/suppliermanage/remove", token, map[string]interface{}{"id": supplier.ID})
	assert.False(t, removed.Success)
	assert.Contains(t, removed.ErrorMessage, "供应商已有商品")

	removable := suite.AddSupplier(t, token, fmt.Sprintf("removable-supplier-%d", suffix), "可删除供应商", false)
	removed = suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/suppliermanage/remove", token, map[string]interface{}{"id": removable.ID})
	require.True(t, removed.Success, removed.ErrorMessage)
}

func testRejectRemovingUsedSupplier(t *testing.T) {
	admin := suite.TokenFor(t, "used-supplier-admin", 1)
	suffix := time.Now().UnixNano()
	supplier := suite.AddSupplier(t, admin, fmt.Sprintf("used-supplier-%d", suffix), "已使用供应商", true)
	suite.AddProductForSupplier(t, admin, fmt.Sprintf("used-supplier-product-%d", suffix), "供应商引用商品", "9.90", uintID(t, supplier.ID), true)
	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/suppliermanage/remove", admin, map[string]interface{}{"id": supplier.ID})
	assert.False(t, removed.Success)
	assert.Contains(t, removed.ErrorMessage, "供应商已有商品")
}

func testProductCRUD(t *testing.T) {
	token := suite.TokenFor(t, "manage-product", 1)
	view := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/productmanage/view", token, nil)
	require.True(t, view.Success, view.ErrorMessage)
	product := suite.AddProduct(t, token, fmt.Sprintf("管理商品-%d", time.Now().UnixNano()), "10.00")
	edited := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/productmanage/edit", token, map[string]interface{}{
		"id": product.ID, "code": product.Code, "name": product.Name + "-改", "price": "12.00", "supplierID": product.SupplierID, "enabled": true,
	})
	require.True(t, edited.Success, edited.ErrorMessage)
	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/productmanage/remove", token, map[string]interface{}{"id": product.ID})
	require.True(t, removed.Success, removed.ErrorMessage)
}

func testRejectRemovingUsedProduct(t *testing.T) {
	admin := suite.TokenFor(t, "used-product-admin", 1)
	user := suite.TokenFor(t, "used-product-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("已使用商品-%d", time.Now().UnixNano()), "20.00")
	suite.AddOrder(t, user, uintID(t, product.ID), 1)
	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/productmanage/remove", admin, map[string]interface{}{"id": product.ID})
	assert.False(t, removed.Success)
	assert.Contains(t, removed.ErrorMessage, "商品已被订单使用")
}

func testPaymentTypeCRUDAndStateCommands(t *testing.T) {
	admin := suite.TokenFor(t, "payment-type-admin", 1)
	item := suite.AddPaymentType(t, admin, fmt.Sprintf("type-%d", time.Now().UnixNano()), "测试支付", false)

	enabled := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymenttypemanage/enablebasedata", admin, map[string]interface{}{"id": item.ID})
	require.True(t, enabled.Success, enabled.ErrorMessage)
	disabled := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymenttypemanage/disablebasedata", admin, map[string]interface{}{"id": item.ID})
	require.True(t, disabled.Success, disabled.ErrorMessage)

	edited := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymenttypemanage/edit", admin, map[string]interface{}{
		"id": item.ID, "code": item.Code, "name": "测试支付已修改", "enabled": false, "description": "新说明",
	})
	require.True(t, edited.Success, edited.ErrorMessage)
	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymenttypemanage/remove", admin, map[string]interface{}{"id": item.ID})
	require.True(t, removed.Success, removed.ErrorMessage)
}

func testRejectRemovingUsedPaymentType(t *testing.T) {
	admin := suite.TokenFor(t, "used-payment-admin", 1)
	user := suite.TokenFor(t, "used-payment-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("支付引用商品-%d", time.Now().UnixNano()), "30.00")
	typeItem := suite.AddPaymentType(t, admin, fmt.Sprintf("used-%d", time.Now().UnixNano()), "已使用支付", true)
	order := suite.AddOrder(t, user, uintID(t, product.ID), 1)
	suite.CreatePayment(t, user, order.ID, typeItem.ID)

	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymenttypemanage/remove", admin, map[string]interface{}{"id": typeItem.ID})
	assert.False(t, removed.Success)
	assert.Contains(t, removed.ErrorMessage, "支付类型已被支付流水使用")

	changedCode := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymenttypemanage/edit", admin, map[string]interface{}{
		"id": typeItem.ID, "code": typeItem.Code + "-new", "name": typeItem.Name, "enabled": true,
	})
	assert.False(t, changedCode.Success)
	assert.Contains(t, changedCode.ErrorMessage, "编码不能修改")
}

func testOrderManageReadOnly(t *testing.T) {
	admin := suite.TokenFor(t, "order-readonly-admin", 1)
	view := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/ordermanage/view", admin, nil)
	require.True(t, view.Success, view.ErrorMessage)
	assert.Contains(t, string(view.Data), "支付状态")
	for _, command := range []string{"add", "edit", "remove"} {
		response := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/ordermanage/"+command, admin, map[string]interface{}{})
		assert.Equal(t, http.StatusNotFound, response.HTTPStatus)
	}
}

func testPaymentRecordCommands(t *testing.T) {
	admin := suite.TokenFor(t, "payment-command-admin", 1)
	user := suite.TokenFor(t, "payment-command-user", 0)
	product := suite.AddProduct(t, admin, fmt.Sprintf("支付命令商品-%d", time.Now().UnixNano()), "40.00")
	typeItem := suite.AddPaymentType(t, admin, fmt.Sprintf("command-%d", time.Now().UnixNano()), "命令支付", true)
	order := suite.AddOrder(t, user, uintID(t, product.ID), 1)
	first := suite.CreatePayment(t, user, order.ID, typeItem.ID)
	failed := suite.PaymentCommand(t, admin, "failpayment", first.PaymentID)
	assert.Equal(t, "支付失败", failed.PaymentStatusName)
	second := suite.CreatePayment(t, user, order.ID, typeItem.ID)
	paid := suite.PaymentCommand(t, admin, "confirmpayment", second.PaymentID)
	assert.Equal(t, "已支付", paid.PaymentStatusName)

	cancelled := suite.RequestJSON(t, http.MethodPost, "/api/inheritanceshop/cancelorder", user, map[string]interface{}{"id": order.ID})
	require.True(t, cancelled.Success, cancelled.ErrorMessage)
	refunded := suite.PaymentCommand(t, admin, "confirmrefund", second.PaymentID)
	assert.Equal(t, "已撤销", refunded.StatusName)
	assert.Equal(t, "已退款", refunded.PaymentStatusName)

	search := suite.RequestJSON(t, http.MethodPost, "/api/manage/inheritanceshop/paymentrecordmanage/search", admin, map[string]int{"page": 1, "size": 100})
	require.True(t, search.Success, search.ErrorMessage)
	var table struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(search.Data, &table))
	assert.GreaterOrEqual(t, len(table.Rows), 2)
}
