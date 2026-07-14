package business

import (
	"testing"

	"github.com/digitalwayhk/core/examples/03-shop-inheritance/models"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierDisableHidesProductsWithoutChangingProductState(t *testing.T) {
	utils.TESTPATH = t.TempDir()

	supplier := models.NewSupplier()
	supplier.SetID(1001)
	supplier.Code = "supplier-a"
	supplier.Name = "供应商 A"
	require.NoError(t, supplier.Insert())
	_, err := NewSupplierService().SetEnabled(supplier.ID, true)
	require.NoError(t, err)

	product := models.NewProduct()
	product.SetID(2001)
	product.Code = "product-a"
	product.Name = "商品 A"
	product.SupplierID = supplier.ID
	product.Price = decimal.RequireFromString("19.90")
	product.Enabled = true
	require.NoError(t, product.Insert())

	available, err := NewProductService().ListAvailable(0, "", "", 0, "")
	require.NoError(t, err)
	require.Len(t, available, 1)

	_, err = NewSupplierService().SetEnabled(supplier.ID, false)
	require.NoError(t, err)

	persisted, err := models.NewProduct().FindByID(product.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.True(t, persisted.Enabled, "禁用供应商不能改写商品自身状态")

	available, err = NewProductService().ListAvailable(0, "", "", 0, "")
	require.NoError(t, err)
	assert.Empty(t, available)

	_, err = NewOrderService().CreateOrder("user-a", product.ID, 1, 3001)
	require.ErrorContains(t, err, "供应商已禁用")

	_, err = NewSupplierService().SetEnabled(supplier.ID, true)
	require.NoError(t, err)
	available, err = NewProductService().ListAvailable(0, "", "", 0, "")
	require.NoError(t, err)
	require.Len(t, available, 1)
}
