package paymentshop_test

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
	t.Run("GetProducts", testGetProducts)
	t.Run("GetPaymentTypes", testGetPaymentTypes)
}

func testGetProducts(t *testing.T) {
	admin := suite.TokenFor(t, "public-product-admin", 1)
	name := fmt.Sprintf("公开商品-%d", time.Now().UnixNano())
	product := suite.AddProduct(t, admin, name, "39.80")
	response := suite.RequestJSON(t, http.MethodGet, "/api/paymentshop/getproducts?name="+url.QueryEscape(name), "", nil)
	require.True(t, response.Success, response.ErrorMessage)
	var items []ProductDTO
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 1)
	assert.Equal(t, product.ID, items[0].ID)
	assert.NotContains(t, string(response.Data), "hashCode")
}

func testGetPaymentTypes(t *testing.T) {
	admin := suite.TokenFor(t, "public-payment-admin", 1)
	suffix := time.Now().UnixNano()
	code := fmt.Sprintf("public-%d", suffix)
	enabled := suite.AddPaymentType(t, admin, code, fmt.Sprintf("公开支付-%d", suffix), true)
	disabled := suite.AddPaymentType(t, admin, code+"-off", fmt.Sprintf("禁用支付-%d", suffix), false)
	response := suite.RequestJSON(t, http.MethodGet, "/api/paymentshop/getpaymenttypes?code="+url.QueryEscape(code), "", nil)
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
