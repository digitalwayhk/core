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

func (*manageAuthRequest) GetTraceId() string          { return "" }
func (r *manageAuthRequest) GetUser() (string, string) { return r.uid, r.uid }
func (*manageAuthRequest) GetClientIP() string         { return "" }
func (*manageAuthRequest) NewID() uint                 { return 1 }
func (*manageAuthRequest) Authorized() bool            { return true }
func (*manageAuthRequest) CallService(servertypes.IRouter, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}
func (*manageAuthRequest) CallTargetService(servertypes.IRouter, *servertypes.TargetInfo, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}
func (*manageAuthRequest) GetValue(string) string                               { return "" }
func (*manageAuthRequest) Bind(interface{}) error                               { return nil }
func (*manageAuthRequest) GoZeroBind(interface{}) error                         { return nil }
func (*manageAuthRequest) NewResponse(interface{}, error) servertypes.IResponse { return nil }
func (*manageAuthRequest) GetPath() string                                      { return "" }
func (*manageAuthRequest) GetClaims(string) interface{}                         { return nil }
func (*manageAuthRequest) ServiceName() string                                  { return contract.OrderServiceName }
func (*manageAuthRequest) GetServerInfo() *servertypes.TargetInfo               { return nil }
func (*manageAuthRequest) GetTargetServerInfo(string) *servertypes.TargetInfo   { return nil }

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
