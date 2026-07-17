package business

import (
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierOwnsProductAndOutboxFacts(t *testing.T) {
	utils.TESTPATH = t.TempDir()
	supplierA, err := EnsureSupplier("supplier-a", "供应商 A")
	require.NoError(t, err)
	supplierB, err := EnsureSupplier("supplier-b", "供应商 B")
	require.NoError(t, err)
	require.NotZero(t, supplierA.ID)
	require.NotZero(t, supplierB.ID)

	product, err := CreateProduct(supplierA.ID, "测试商品", "product-a", decimal.NewFromInt(12), 1001, "event-create")
	require.NoError(t, err)
	assert.False(t, product.Enabled)
	_, err = ProductSnapshot(product.ID)
	require.ErrorContains(t, err, "未上架")

	enabled := true
	price := decimal.NewFromInt(18)
	_, err = UpdateOwnedProduct(supplierB.ID, product.ID, &price, &enabled, "event-forbidden")
	require.ErrorContains(t, err, "无权")
	updated, err := UpdateOwnedProduct(supplierA.ID, product.ID, &price, &enabled, "event-update")
	require.NoError(t, err)
	assert.True(t, updated.Enabled)
	assert.True(t, updated.Price.Equal(price))

	snapshot, err := ProductSnapshot(product.ID)
	require.NoError(t, err)
	assert.Equal(t, supplierA.ID, snapshot.SupplierID)
	assert.True(t, snapshot.UnitPrice.Equal(price))

	pending, err := models.PendingOutbox()
	require.NoError(t, err)
	assert.Len(t, pending, 2)
}
