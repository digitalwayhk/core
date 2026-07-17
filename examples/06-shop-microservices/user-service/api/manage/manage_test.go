package manage

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "user-manage-")
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
	body interface{}
}

func (r *manageRequest) GetTraceId() string        { return "" }
func (r *manageRequest) GetUser() (string, string) { return r.uid, r.uid }
func (*manageRequest) GetClientIP() string         { return "" }
func (*manageRequest) NewID() uint                 { return 880001 }
func (*manageRequest) Authorized() bool            { return true }
func (*manageRequest) CallService(servertypes.IRouter, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}
func (*manageRequest) CallTargetService(servertypes.IRouter, *servertypes.TargetInfo, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}
func (*manageRequest) GetValue(string) string { return "" }
func (r *manageRequest) Bind(target interface{}) error {
	data, err := json.Marshal(r.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
func (*manageRequest) GoZeroBind(interface{}) error                         { return nil }
func (*manageRequest) NewResponse(interface{}, error) servertypes.IResponse { return nil }
func (*manageRequest) GetPath() string                                      { return "" }
func (*manageRequest) GetClaims(string) interface{}                         { return nil }
func (*manageRequest) ServiceName() string                                  { return contract.UserServiceName }
func (*manageRequest) GetServerInfo() *servertypes.TargetInfo               { return nil }
func (*manageRequest) GetTargetServerInfo(string) *servertypes.TargetInfo   { return nil }

func requireWhere(t *testing.T, item *view.SearchItem, name string, value interface{}) {
	t.Helper()
	for _, where := range item.WhereList {
		if where.Name == name {
			require.EqualValues(t, value, where.Value)
			return
		}
	}
	t.Fatalf("缺少搜索条件 %s", name)
}

func TestUserAndAddressSearchScopeOwner(t *testing.T) {
	user, err := models.EnsureUser("manage-buyer", "买家")
	require.NoError(t, err)
	userManage := NewUserManage()
	userManage.Search.SearchItem = &view.SearchItem{}
	_, err, stop := userManage.SearchBefore(userManage.Search, &manageRequest{uid: user.AuthUserID})
	require.NoError(t, err)
	require.False(t, stop)
	requireWhere(t, userManage.Search.SearchItem, "ID", user.ID)

	addressManage := NewAddressManage()
	addressManage.Search.SearchItem = &view.SearchItem{}
	_, err, stop = addressManage.SearchBefore(addressManage.Search, &manageRequest{uid: user.AuthUserID})
	require.NoError(t, err)
	require.False(t, stop)
	requireWhere(t, addressManage.Search.SearchItem, "UserID", user.ID)
}

func TestAddressAddInjectsOwnerAndDisabledUserIsReadOnly(t *testing.T) {
	user, err := models.EnsureUser("address-owner", "地址用户")
	require.NoError(t, err)
	manage := NewAddressManage()
	add := managepkg.NewAdd[models.Address](manage)
	add.Model = models.NewAddress()
	add.Model.UserID = user.ID + 10
	_, err, stop := manage.DoBefore(add, &manageRequest{uid: user.AuthUserID})
	require.NoError(t, err)
	require.False(t, stop)
	require.Equal(t, user.ID, add.Model.UserID)

	user.Enabled = false
	require.NoError(t, models.SaveUser(user))
	_, err, stop = manage.DoBefore(add, &manageRequest{uid: user.AuthUserID})
	require.ErrorIs(t, err, contract.ErrSubjectDisabled)
	require.True(t, stop)
}

func TestUserManageHasNoPhysicalRemoveRouter(t *testing.T) {
	routers := NewUserManage().Routers()
	require.Len(t, routers, 4)
	for _, api := range routers {
		require.NotContains(t, api.RouterInfo().GetPath(), "/remove")
	}
}
