// 本文件验证当前服务 Manage API 的权限、限域和受控命令边界。
package manage

import (
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

type manageAuthRequest struct{ uid string }

// GetTraceId 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetTraceId() string { return "" }

// GetUser 实现本类型在当前服务边界中的行为。
func (r *manageAuthRequest) GetUser() (string, string) { return r.uid, r.uid }

// GetClientIP 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetClientIP() string { return "" }

// NewID 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) NewID() uint { return 1 }

// Authorized 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) Authorized() bool { return true }

// CallService 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) CallService(servertypes.IRouter, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}

// CallTargetService 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) CallTargetService(servertypes.IRouter, *servertypes.TargetInfo, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}

// GetValue 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetValue(string) string { return "" }

// Bind 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) Bind(interface{}) error { return nil }

// GoZeroBind 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GoZeroBind(interface{}) error { return nil }

// NewResponse 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) NewResponse(interface{}, error) servertypes.IResponse { return nil }

// GetPath 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetPath() string { return "" }

// GetClaims 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetClaims(string) interface{} { return nil }

// ServiceName 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) ServiceName() string { return contract.OrderServiceName }

// GetServerInfo 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetServerInfo() *servertypes.TargetInfo { return nil }

// GetTargetServerInfo 实现本类型在当前服务边界中的行为。
func (*manageAuthRequest) GetTargetServerInfo(string) *servertypes.TargetInfo { return nil }

// TestOrderManageInventoryUsesReadOnlyAndControlledCommands 验证当前场景的业务闭环和边界行为。
func TestOrderManageInventoryUsesReadOnlyAndControlledCommands(t *testing.T) {
	paymentTypes := NewPaymentTypeManage().Routers()
	require.Len(t, paymentTypes, 6)
	orders := NewOrderManage().Routers()
	require.Len(t, orders, 4)
	require.IsType(t, &managepkg.View[models.Order]{}, orders[0])
	require.IsType(t, &managepkg.Search[models.Order]{}, orders[1])
	payments := NewPaymentRecordManage().Routers()
	require.Len(t, payments, 5)
	require.IsType(t, &managepkg.View[models.PaymentRecord]{}, payments[0])
	require.IsType(t, &managepkg.Search[models.PaymentRecord]{}, payments[1])
}

// TestOrderManageRejectsNonAdminBeforeSearch 验证当前场景的业务闭环和边界行为。
func TestOrderManageRejectsNonAdminBeforeSearch(t *testing.T) {
	manage := NewOrderManage()
	manage.Search.SearchItem = &view.SearchItem{}
	_, err, stop := manage.SearchBefore(manage.Search, &manageAuthRequest{uid: "buyer"})
	require.ErrorIs(t, err, contract.ErrForbidden)
	require.True(t, stop)
	_, err, stop = manage.SearchBefore(manage.Search, &manageAuthRequest{uid: contract.PlatformAdminUserID})
	require.NoError(t, err)
	require.False(t, stop)
}
