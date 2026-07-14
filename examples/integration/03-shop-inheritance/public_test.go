package inheritanceshop_test

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

func TestPublicAPIs(t *testing.T) {
	t.Run("GetSuppliers", testGetSuppliers)
	t.Run("GetProducts", testGetProducts)
	t.Run("SupplierDisableControlsProductVisibility", testSupplierDisableControlsProductVisibility)
	t.Run("GetPaymentTypes", testGetPaymentTypes)
}

func testGetSuppliers(t *testing.T) {
	admin := suite.TokenFor(t, "public-supplier-admin", 1)
	suffix := time.Now().UnixNano()
	code := fmt.Sprintf("public-supplier-%d", suffix)
	enabled := suite.AddSupplier(t, admin, code, "公开供应商", true)
	disabled := suite.AddSupplier(t, admin, code+"-off", "禁用供应商", false)
	response := suite.RequestJSON(t, http.MethodGet, "/api/inheritanceshop/getsuppliers?code="+url.QueryEscape(code), "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []SupplierDTO
	require.NoError(t, json.Unmarshal(response.Data, &items))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	assert.Contains(t, ids, enabled.ID)
	assert.NotContains(t, ids, disabled.ID)
	assert.NotContains(t, string(response.Data), "enabled")
}

func testGetProducts(t *testing.T) {
	admin := suite.TokenFor(t, "public-product-admin", 1)
	name := fmt.Sprintf("公开商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, admin, name, "39.80")
	response := suite.RequestJSON(t, http.MethodGet, "/api/inheritanceshop/getproducts?name="+url.QueryEscape(name), "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []ProductDTO
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 1)
	assert.Equal(t, product.ID, items[0].ID)
	assert.NotEmpty(t, items[0].SupplierCode)
	assert.NotEmpty(t, items[0].SupplierName)
	assert.NotContains(t, string(response.Data), "hashCode")
}

func testSupplierDisableControlsProductVisibility(t *testing.T) {
	admin := suite.TokenFor(t, "public-supplier-state-admin", 1)
	suffix := time.Now().UnixNano()
	supplier := suite.AddSupplier(t, admin, fmt.Sprintf("state-supplier-%d", suffix), "状态供应商", true)
	product := suite.AddProductForSupplier(t, admin, fmt.Sprintf("state-product-%d", suffix), "状态商品", "28.80", uintID(t, supplier.ID), true)
	path := "/api/inheritanceshop/getproducts?supplierCode=" + url.QueryEscape(supplier.Code)

	visible := suite.RequestJSON(t, http.MethodGet, path, "", nil)
	require.True(t, visible.Success, visible.ErrorMessage)
	var visibleItems []ProductDTO
	require.NoError(t, json.Unmarshal(visible.Data, &visibleItems))
	require.Len(t, visibleItems, 1)
	assert.Equal(t, product.ID, visibleItems[0].ID)

	suite.SetBaseDataEnabled(t, admin, "suppliermanage", supplier.ID, false)
	hidden := suite.RequestJSON(t, http.MethodGet, path, "", nil)
	require.True(t, hidden.Success, hidden.ErrorMessage)
	var hiddenItems []ProductDTO
	require.NoError(t, json.Unmarshal(hidden.Data, &hiddenItems))
	assert.Empty(t, hiddenItems)

	suite.SetBaseDataEnabled(t, admin, "suppliermanage", supplier.ID, true)
	restored := suite.RequestJSON(t, http.MethodGet, path, "", nil)
	require.True(t, restored.Success, restored.ErrorMessage)
	var restoredItems []ProductDTO
	require.NoError(t, json.Unmarshal(restored.Data, &restoredItems))
	require.Len(t, restoredItems, 1)
}

func testGetPaymentTypes(t *testing.T) {
	admin := suite.TokenFor(t, "public-payment-admin", 1)
	suffix := time.Now().UnixNano()
	code := fmt.Sprintf("public-%d", suffix)
	enabled := suite.AddPaymentType(t, admin, code, fmt.Sprintf("公开支付-%d", suffix), true)
	disabled := suite.AddPaymentType(t, admin, code+"-off", fmt.Sprintf("禁用支付-%d", suffix), false)
	response := suite.RequestJSON(t, http.MethodGet, "/api/inheritanceshop/getpaymenttypes?code="+url.QueryEscape(code), "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []PaymentTypeDTO
	require.NoError(t, json.Unmarshal(response.Data, &items))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	assert.Contains(t, ids, enabled.ID)
	assert.NotContains(t, ids, disabled.ID)
	assert.NotContains(t, string(response.Data), "enabled")
}
