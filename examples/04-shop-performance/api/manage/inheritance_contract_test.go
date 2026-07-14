package manage

import (
	"testing"

	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseDataManageForcesAddDisabledAndRejectsEditStateChange(t *testing.T) {
	productManage := NewProductManage()
	added := models.NewProduct()
	added.Code = "product-add"
	added.Name = "新增商品"
	added.Enabled = true
	add := managepkg.NewAdd[models.Product](productManage)
	add.Model = added

	require.NoError(t, productManage.BaseDataManage.ParseAfter(add, nil))
	assert.False(t, added.Enabled)

	old := models.NewProduct()
	old.Enabled = true
	edited := models.NewProduct()
	edited.Enabled = false
	edit := managepkg.NewEdit[models.Product](productManage)
	edit.Model = edited
	edit.OldItem = old

	err := productManage.BaseDataManage.ValidationAfter(edit, nil)
	require.ErrorContains(t, err, "启用状态只能通过启用或禁用命令修改")
}

func TestProductManageKeepsConcreteOwnerAcrossInheritance(t *testing.T) {
	productManage := NewProductManage()

	assert.Same(t, productManage, productManage.View.GetInstance())
	assert.Same(t, productManage, productManage.Search.GetInstance())
	assert.Len(t, productManage.Routers(), 7)
}

func TestProductManageCombinesParentAndConcreteFieldRules(t *testing.T) {
	productManage := NewProductManage()

	enabled := &view.FieldModel{Field: "enabled", PropField: "Enabled", IsEdit: true}
	productManage.ViewFieldModel(nil, enabled)
	assert.Equal(t, "启用状态", enabled.Title)
	assert.False(t, enabled.IsEdit)

	price := &view.FieldModel{Field: "price", PropField: "Price"}
	productManage.ViewFieldModel(nil, price)
	assert.Equal(t, "商品价格", price.Title)
	assert.Equal(t, 2, price.Precision)
}

func TestSupplierProductsChildIsReadOnly(t *testing.T) {
	supplierManage := NewSupplierManage()
	child := &view.ViewChildModel{
		ViewModel: view.ViewModel{Name: "Products"},
		IsAdd:     true,
		IsEdit:    true,
		IsRemove:  true,
	}

	supplierManage.ViewChildModel(child)

	require.Equal(t, "Products", child.Name)
	assert.False(t, child.IsAdd)
	assert.False(t, child.IsEdit)
	assert.False(t, child.IsRemove)
}

func TestRemainingManageTypesExposeInheritedAndControlledCommands(t *testing.T) {
	paymentTypeManage := NewPaymentTypeManage()
	orderManage := NewOrderManage()
	paymentRecordManage := NewPaymentRecordManage()

	assert.Same(t, paymentTypeManage, paymentTypeManage.View.GetInstance())
	assert.Len(t, paymentTypeManage.Routers(), 7)
	assert.Len(t, orderManage.Routers(), 2)
	assert.Len(t, paymentRecordManage.Routers(), 5)

	orderStatus := &view.FieldModel{Field: "status", PropField: "Status", IsEdit: true}
	orderManage.ViewFieldModel(nil, orderStatus)
	assert.Equal(t, "订单状态", orderStatus.Title)
	assert.False(t, orderStatus.IsEdit)
	require.NotNil(t, orderStatus.ComVtp)
	assert.Len(t, orderStatus.ComVtp.Items, 3)

	paymentStatus := &view.FieldModel{Field: "status", PropField: "Status", IsEdit: true}
	paymentRecordManage.ViewFieldModel(nil, paymentStatus)
	assert.Equal(t, "支付状态", paymentStatus.Title)
	assert.False(t, paymentStatus.IsEdit)
	require.NotNil(t, paymentStatus.ComVtp)
	assert.Len(t, paymentStatus.ComVtp.Items, 5)
}
