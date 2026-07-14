package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProductResponse 验证商品 DTO 只暴露前端需要的字段。
func TestProductResponse(t *testing.T) {
	product := models.NewProduct()
	product.ID = 42
	product.Name = "测试商品"
	product.Price = decimal.RequireFromString("19.90")

	response := NewProductResponse(product)
	require.NotNil(t, response)
	assert.Equal(t, uint(42), response.ID)
	assert.Equal(t, "测试商品", response.Name)
	assert.Equal(t, "19.9", response.Price)
	data, err := json.Marshal(response)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"42","name":"测试商品","price":"19.9"}`, string(data))
}

// TestOrderResponse 验证订单 DTO 保留商品快照并将时间规范到秒。
func TestOrderResponse(t *testing.T) {
	createdAt := time.Date(2026, time.July, 14, 8, 9, 10, 987654321, time.FixedZone("UTC+8", 8*60*60))
	order := models.NewOrder()
	order.ID = 7
	order.CreatedAt = &createdAt
	order.ProductID = 42
	order.ProductName = "测试商品"
	order.UnitPrice = decimal.RequireFromString("19.90")
	order.Quantity = 2
	order.UserID = "user-1"

	response := NewOrderResponse(order)
	require.NotNil(t, response)
	assert.Equal(t, "2026-07-14T00:09:10Z", response.CreatedAt)
	data, err := json.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "hashCode")
	assert.NotContains(t, string(data), "modelState")
}
