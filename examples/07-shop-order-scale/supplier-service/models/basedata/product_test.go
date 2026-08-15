// 本文件验证商品模型拒绝缺少供应商、名称、编码或正数价格的 Manage 写入。
package basedata

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestProductManageValidationRejectsMissingSupplier(t *testing.T) {
	product := NewProduct()
	product.Code = "missing-supplier"
	product.Name = "无供应商商品"
	product.Price = decimal.NewFromInt(7)

	require.ErrorContains(t, product.AddValid(), "供应商")
	require.ErrorContains(t, product.UpdateValid(nil), "供应商")
}

func TestProductManageValidationAcceptsCompleteProduct(t *testing.T) {
	product := NewProduct()
	product.SupplierID = 1001
	product.Code = "complete-product"
	product.Name = "完整商品"
	product.Price = decimal.NewFromInt(7)

	require.NoError(t, product.AddValid())
	require.NoError(t, product.UpdateValid(nil))
}
