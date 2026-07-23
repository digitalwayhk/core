// 本文件验证当前服务 Manage API 的权限、限域和受控命令边界。
package manage

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var manageRequestID atomic.Uint64

// TestMain 验证当前场景的业务闭环和边界行为。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "supplier-manage-")
	if err != nil {
		panic(err)
	}
	utils.TESTPATH = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type manageRequest struct {
	uid  string
	name string
	body interface{}
}

func requestFor(uid string) *manageRequest { return &manageRequest{uid: uid, name: uid} }

// ServiceName 实现本类型在当前服务边界中的行为。
func (r *manageRequest) ServiceName() string { return contract.SupplierServiceName }

// Bind 实现本类型在当前服务边界中的行为。
func (r *manageRequest) Bind(target interface{}) error {
	if r.body == nil {
		return nil
	}
	data, err := json.Marshal(r.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// GoZeroBind 实现本类型在当前服务边界中的行为。
func (*manageRequest) GoZeroBind(interface{}) error { return nil }

// GetTraceId 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetTraceId() string { return "" }

// GetUser 实现本类型在当前服务边界中的行为。
func (r *manageRequest) GetUser() (string, string) { return r.uid, r.name }

// GetClientIP 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetClientIP() string { return "127.0.0.1" }

// Authorized 实现本类型在当前服务边界中的行为。
func (*manageRequest) Authorized() bool { return true }

// GetValue 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetValue(string) string { return "" }

// GetPath 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetPath() string { return "" }

// GetClaims 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetClaims(string) interface{} { return nil }

// GetServerInfo 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetServerInfo() *servertypes.TargetInfo { return nil }

// GetTargetServerInfo 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetTargetServerInfo(string) *servertypes.TargetInfo { return nil }

// NewResponse 实现本类型在当前服务边界中的行为。
func (*manageRequest) NewResponse(interface{}, error) servertypes.IResponse { return nil }

// CallService 实现本类型在当前服务边界中的行为。
func (*manageRequest) CallService(servertypes.IRouter, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}

// CallTargetService 实现本类型在当前服务边界中的行为。
func (*manageRequest) CallTargetService(servertypes.IRouter, *servertypes.TargetInfo, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}

// NewID 实现本类型在当前服务边界中的行为。
func (*manageRequest) NewID() uint {
	return uint(900000 + manageRequestID.Add(1))
}

func newSupplier(t *testing.T, authID string, enabled bool) *models.Supplier {
	t.Helper()
	supplier, err := business.EnsureSupplier(authID, authID)
	require.NoError(t, err)
	supplier.Enabled = enabled
	require.NoError(t, supplier.Save())
	return supplier
}

func newProduct(t *testing.T, supplier *models.Supplier, code string) *models.Product {
	t.Helper()
	product, err := business.CreateProduct(supplier.ID, code, code, decimal.NewFromInt(12), requestFor("seed").NewID(), "trace-product-"+code, "product-"+code)
	require.NoError(t, err)
	return product
}

func requireWhere(t *testing.T, item *view.SearchItem, name string, value interface{}) {
	t.Helper()
	for _, where := range item.WhereList {
		if where != nil && where.Name == name {
			require.EqualValues(t, value, where.Value)
			return
		}
	}
	t.Fatalf("搜索条件中缺少 %s", name)
}

func requireNoWhere(t *testing.T, item *view.SearchItem, name string) {
	t.Helper()
	for _, where := range item.WhereList {
		if where != nil && where.Name == name {
			t.Fatalf("搜索条件不应包含 %s", name)
		}
	}
}

// TestManageSearchScopesSupplierButNotAdmin 验证当前场景的业务闭环和边界行为。
func TestManageSearchScopesSupplierButNotAdmin(t *testing.T) {
	supplier := newSupplier(t, "search-owner", true)
	tests := []struct {
		name   string
		manage interface {
			SearchBefore(interface{}, servertypes.IRequest) (interface{}, error, bool)
		}
		search interface{}
		column string
	}{
		{name: "供应商", manage: NewSupplierManage(), column: "ID"},
		{name: "商品", manage: NewProductManage(), column: "SupplierID"},
		{name: "订单", manage: NewOrderManage(), column: "SupplierID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &view.SearchItem{}
			switch manage := tt.manage.(type) {
			case *SupplierManage:
				manage.Search.SearchItem, tt.search = item, manage.Search
			case *ProductManage:
				manage.Search.SearchItem, tt.search = item, manage.Search
			case *OrderManage:
				manage.Search.SearchItem, tt.search = item, manage.Search
			}
			_, err, stop := tt.manage.SearchBefore(tt.search, requestFor(supplier.AuthUserID))
			require.NoError(t, err)
			require.False(t, stop)
			requireWhere(t, item, tt.column, supplier.ID)

			adminItem := &view.SearchItem{}
			switch manage := tt.manage.(type) {
			case *SupplierManage:
				manage.Search.SearchItem = adminItem
			case *ProductManage:
				manage.Search.SearchItem = adminItem
			case *OrderManage:
				manage.Search.SearchItem = adminItem
			}
			_, err, stop = tt.manage.SearchBefore(tt.search, requestFor(contract.PlatformAdminUserID))
			require.NoError(t, err)
			require.False(t, stop)
			requireNoWhere(t, adminItem, tt.column)
		})
	}
}

// TestSupplierEditPreservesIdentityAndEnabled 验证当前场景的业务闭环和边界行为。
func TestSupplierEditPreservesIdentityAndEnabled(t *testing.T) {
	supplier := newSupplier(t, "supplier-edit-owner", true)
	manage := NewSupplierManage()
	operation := managepkg.NewEdit[models.Supplier](manage)
	operation.OldItem = supplier
	operation.Model = models.NewSupplier()
	operation.Model.SetID(supplier.ID)
	operation.Model.AuthUserID = "spoofed"
	operation.Model.Name = "新名称"
	operation.Model.Code = "new-code"
	operation.Model.Description = "新说明"
	operation.Model.Enabled = false

	result, err, stop := manage.DoBefore(operation, requestFor(supplier.AuthUserID))
	require.NoError(t, err)
	require.True(t, stop)
	updated := result.(*models.Supplier)
	require.Equal(t, supplier.AuthUserID, updated.AuthUserID)
	require.True(t, updated.Enabled)
	require.Equal(t, "新名称", updated.Name)
	require.Equal(t, "new-code", updated.Code)
}

// TestDisabledSupplierIsReadOnly 验证当前场景的业务闭环和边界行为。
func TestDisabledSupplierIsReadOnly(t *testing.T) {
	supplier := newSupplier(t, "disabled-owner", false)
	manage := NewProductManage()
	operation := managepkg.NewAdd[models.Product](manage)
	operation.Model = models.NewProduct()
	operation.Model.Name = "商品"
	operation.Model.Code = "disabled-product"
	operation.Model.Price = decimal.NewFromInt(10)

	_, err, stop := manage.DoBefore(operation, requestFor(supplier.AuthUserID))
	require.ErrorIs(t, err, contract.ErrSubjectDisabled)
	require.True(t, stop)
}

// TestSupplierEnabledCanOnlyBeChangedByAdmin 验证当前场景的业务闭环和边界行为。
func TestSupplierEnabledCanOnlyBeChangedByAdmin(t *testing.T) {
	supplier := newSupplier(t, "supplier-state-owner", true)
	manage := NewSupplierManage()
	manage.SetEnabled.Model = models.NewSupplier()
	manage.SetEnabled.Model.SetID(supplier.ID)
	manage.SetEnabled.Model.Enabled = false

	_, err := manage.SetEnabled.Do(requestFor(supplier.AuthUserID))
	require.ErrorIs(t, err, contract.ErrForbidden)
	result, err := manage.SetEnabled.Do(requestFor(contract.PlatformAdminUserID))
	require.NoError(t, err)
	require.False(t, result.(*models.Supplier).Enabled)
}

// TestProductEditAndEnabledCommandsEnforceOwnership 验证当前场景的业务闭环和边界行为。
func TestProductEditAndEnabledCommandsEnforceOwnership(t *testing.T) {
	owner := newSupplier(t, "product-owner", true)
	other := newSupplier(t, "product-other", true)
	product := newProduct(t, owner, "owned-product")
	manage := NewProductManage()

	edit := managepkg.NewEdit[models.Product](manage)
	edit.OldItem = product
	edit.Model = models.NewProduct()
	edit.Model.SetID(product.ID)
	edit.Model.SupplierID = other.ID
	edit.Model.Name = "更新商品"
	edit.Model.Code = "updated-product"
	edit.Model.Price = decimal.NewFromInt(20)
	edit.Model.Enabled = true
	result, err, stop := manage.DoBefore(edit, requestFor(owner.AuthUserID))
	require.NoError(t, err)
	require.True(t, stop)
	updated := result.(*models.Product)
	require.Equal(t, owner.ID, updated.SupplierID)
	require.False(t, updated.Enabled)

	manage.SetEnabled.Model = models.NewProduct()
	manage.SetEnabled.Model.SetID(product.ID)
	manage.SetEnabled.Model.Enabled = true
	_, err = manage.SetEnabled.Do(requestFor(other.AuthUserID))
	require.ErrorIs(t, err, contract.ErrForbidden)
	result, err = manage.SetEnabled.Do(requestFor(owner.AuthUserID))
	require.NoError(t, err)
	require.True(t, result.(*models.Product).Enabled)
}

// TestSupplierOrderManageIsReadOnly 验证当前场景的业务闭环和边界行为。
func TestSupplierOrderManageIsReadOnly(t *testing.T) {
	routers := NewOrderManage().Routers()
	require.Len(t, routers, 2)
	require.IsType(t, &managepkg.View[models.SupplierOrder]{}, routers[0])
	require.IsType(t, &managepkg.Search[models.SupplierOrder]{}, routers[1])
}
