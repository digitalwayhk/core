// Package shoporderscale 验证 07 单进程供应商角色的资料读取闭环。
package shoporderscale

import (
	"fmt"
	"testing"
	"time"

	suppliermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestUATSupplierProductAuthority 验证供应商和商品保存在供应商权威服务本地库。
func TestUATSupplierProductAuthority(t *testing.T) {
	require.NoError(t, suppliermodels.EnsureStorage())
	unique := uint(time.Now().UnixNano() % 1000000)

	supplier := suppliermodels.NewSupplier()
	supplier.ID = 830000 + unique
	supplier.UserID = 930000 + unique
	supplier.Code = fmt.Sprintf("supplier-uat-%d", unique)
	supplier.Name = "UAT供应商"
	require.NoError(t, suppliermodels.RunTransaction(func(action persistencetypes.IDataAction) error {
		return supplier.InsertWith(action)
	}))

	product := suppliermodels.NewProduct()
	product.ID = 840000 + unique
	product.SupplierID = supplier.ID
	product.Code = fmt.Sprintf("product-uat-%d", unique)
	product.Name = "UAT商品"
	product.Price = decimal.NewFromInt(15)
	require.NoError(t, suppliermodels.RunTransaction(func(action persistencetypes.IDataAction) error {
		return product.InsertWith(action)
	}))

	suppliers, err := suppliermodels.ListSuppliers(true)
	require.NoError(t, err)
	products, err := suppliermodels.ListProducts(product.ID, true)
	require.NoError(t, err)
	require.NotEmpty(t, suppliers)
	require.Len(t, products, 1)
	require.Equal(t, product.ID, products[0].ID)
}
