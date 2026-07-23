package manage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

type shopManageTestRequest struct {
	servertypes.IRequest
	uid string
}

var _ IDoBefore[models.Product] = (*ProductManage)(nil)
var _ IDoAfter[models.Product] = (*ProductManage)(nil)
var _ IDoBefore[models.Supplier] = (*SupplierManage)(nil)
var _ IDoAfter[models.Supplier] = (*SupplierManage)(nil)
var _ IDoBefore[models.PaymentType] = (*PaymentTypeManage)(nil)
var _ IDoAfter[models.PaymentType] = (*PaymentTypeManage)(nil)
var _ IDoBefore[models.Order] = (*OrderManage)(nil)
var _ IDoAfter[models.Order] = (*OrderManage)(nil)
var _ IDoBefore[models.PaymentRecord] = (*PaymentRecordManage)(nil)
var _ IDoAfter[models.PaymentRecord] = (*PaymentRecordManage)(nil)
var _ IDoBefore[models.IdentityEventRecord] = (*IdentityEventManage)(nil)
var _ IDoAfter[models.IdentityEventRecord] = (*IdentityEventManage)(nil)

func (req *shopManageTestRequest) GetUser() (string, string) { return req.uid, "管理员" }
func (*shopManageTestRequest) ServiceName() string           { return "shop-casdoor-rbac" }
func (*shopManageTestRequest) GetPath() string               { return "/api/manage/shop/product/add" }
func (*shopManageTestRequest) GetTraceId() string            { return "trace-manage" }

func TestBaseDataManageForcesAddDisabledAndRejectsEditStateChange(t *testing.T) {
	productManage := NewProductManage()
	added := models.NewProduct()
	added.Code = "product-add"
	added.Name = "新增商品"
	added.SupplierID = 1
	added.Price = decimal.NewFromInt(10)
	added.Enabled = true
	add := managepkg.NewAdd[models.Product](productManage)
	add.Model = added

	_, err, stop := productManage.BaseDataManage.OnAddBefore(add, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.False(t, stop)
	assert.False(t, added.Enabled)

	old := models.NewProduct()
	old.Enabled = true
	edited := models.NewProduct()
	edited.Enabled = false
	edit := managepkg.NewEdit[models.Product](productManage)
	edit.Model = edited
	edit.OldItem = old

	_, err, stop = productManage.BaseDataManage.OnEditBefore(edit, &shopManageTestRequest{uid: "admin-1"})
	require.True(t, stop)
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

func TestShopManageAppliesServiceWideWriteAndSearchRules(t *testing.T) {
	manager := NewShopManage[models.Product](nil)
	product := models.NewProduct()
	add := managepkg.NewAdd[models.Product](manager)
	add.Model = product

	_, err, stop := manager.DoBefore(add, &shopManageTestRequest{})
	require.True(t, stop)
	require.Equal(t, 401, servertypes.ResolvePublicError(err).HTTPStatus)

	_, err, stop = manager.DoBefore(add, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.False(t, stop)
	result, err := manager.DoAfter(add, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	assert.Same(t, product, result)

	search := managepkg.NewSearch[models.Product](manager)
	search.SearchItem = &view.SearchItem{Size: 500, Tag: "product-query"}
	_, err, stop = manager.SearchBefore(search, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.False(t, stop)
	assert.Equal(t, shopManageMaxPageSize, search.SearchItem.Size)
	require.Len(t, search.SearchItem.SortList, 1)
	assert.Equal(t, "ID", search.SearchItem.SortList[0].Name)
	assert.True(t, search.SearchItem.SortList[0].Isdesc)

	data, err := manager.SearchAfter(search, &view.TableData{}, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	table, ok := data.(*view.TableData)
	require.True(t, ok)
	assert.Equal(t, "product-query", table.Tag)
}

func TestShopManageLogsOnlyAtLifecycleEntrances(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.SetWriter(previous)
		logx.Reset()
	})

	manager := NewProductManage()
	operation := managepkg.NewAdd[models.Product](manager)
	operation.Model = models.NewProduct()

	_, err, stop := manager.DoBefore(operation, &shopManageTestRequest{})
	require.Error(t, err)
	require.True(t, stop)
	_, err = manager.OnAddAfter(operation, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	result, err := manager.DoAfter(operation, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.Same(t, operation.Model, result)

	logs := output.String()
	assert.Equal(t, 1, strings.Count(logs, "shop_manage_operation_failed"))
	assert.Equal(t, 1, strings.Count(logs, "shop_manage_operation_succeeded"))
	assert.Contains(t, logs, `"phase":"before"`)
	assert.Contains(t, logs, `"phase":"after"`)
	assert.Contains(t, logs, `"owner":"ProductManage"`)
}

func TestBusinessAndConcreteManageLayerSearchRules(t *testing.T) {
	orderManager := NewOrderManage()
	businessSearch := managepkg.NewSearch[models.Order](orderManager)
	businessSearch.SearchItem = &view.SearchItem{Size: 500}

	_, err, stop := orderManager.BusinessManage.OnSearchBefore(businessSearch, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.False(t, stop)
	assert.Equal(t, businessManageMaxPageSize, businessSearch.SearchItem.Size)

	orderSearch := managepkg.NewSearch[models.Order](orderManager)
	orderSearch.SearchItem = &view.SearchItem{Size: 500}
	_, err, stop = orderManager.SearchBefore(orderSearch, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.False(t, stop)
	assert.Equal(t, orderManageMaxPageSize, orderSearch.SearchItem.Size)

	paymentManager := NewPaymentRecordManage()
	paymentSearch := managepkg.NewSearch[models.PaymentRecord](paymentManager)
	paymentSearch.SearchItem = &view.SearchItem{Size: 500}
	_, err, stop = paymentManager.SearchBefore(paymentSearch, &shopManageTestRequest{uid: "admin-1"})
	require.NoError(t, err)
	require.False(t, stop)
	assert.Equal(t, paymentRecordManageMaxPageSize, paymentSearch.SearchItem.Size)
}

func TestBusinessManageRejectsGenericWriteCommands(t *testing.T) {
	manager := NewOrderManage()
	add := managepkg.NewAdd[models.Order](manager)
	add.Model = models.NewOrder()

	_, err, stop := manager.BusinessManage.OnAddBefore(add, &shopManageTestRequest{uid: "admin-1"})

	require.True(t, stop)
	require.ErrorContains(t, err, "必须通过专用命令新增")
}

func TestOrderViewModelReliesOnGeneratedPaymentStatusField(t *testing.T) {
	manager := NewOrderManage()
	model := &view.ViewModel{}

	manager.ViewModel(model)

	assert.Empty(t, model.Fields, "ViewModel 不应重复创建由模型反射生成的 PaymentStatus 字段")
	assert.Equal(t, "订单管理", model.Title)
}
