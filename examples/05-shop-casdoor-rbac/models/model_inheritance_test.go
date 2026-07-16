package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcreteModelsInitializeCompleteInheritanceChain(t *testing.T) {
	tests := []struct {
		name  string
		model interface{ GetShopModel() *ShopModel }
	}{
		{name: "商品", model: NewProduct()},
		{name: "供应商", model: NewSupplier()},
		{name: "支付类型", model: NewPaymentType()},
		{name: "订单", model: NewOrder()},
		{name: "支付流水", model: NewPaymentRecord()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.model.GetShopModel())
			require.NotNil(t, tt.model.GetShopModel().Model)
		})
	}
}

func TestBaseDataHashUsesNormalizedCode(t *testing.T) {
	supplier := NewSupplier()
	supplier.Code = "  ACME  "
	supplier.Name = " 艾克米供应商 "

	require.NoError(t, supplier.NormalizeBaseData())
	assert.Equal(t, "acme", supplier.Code)
	assert.Equal(t, "艾克米供应商", supplier.Name)
	assert.NotEmpty(t, supplier.GetHash())
	assert.False(t, supplier.Enabled)
}

func TestBusinessModelsExposeTypedChineseStatus(t *testing.T) {
	order := NewOrder()
	record := NewPaymentRecord()

	assert.Equal(t, OrderStatusNormal, order.OrderStatus())
	assert.Equal(t, "正常", order.OrderStatus().String())
	assert.Equal(t, PaymentStatusPending, record.PaymentStatus())
	assert.Equal(t, "支付中", record.PaymentStatus().String())
}
