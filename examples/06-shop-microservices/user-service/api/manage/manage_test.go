// 本文件验证当前服务 Manage API 的权限、限域和受控命令边界。
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

// TestMain 验证当前场景的业务闭环和边界行为。
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

// GetTraceId 实现本类型在当前服务边界中的行为。
func (r *manageRequest) GetTraceId() string { return "" }

// GetUser 实现本类型在当前服务边界中的行为。
func (r *manageRequest) GetUser() (string, string) { return r.uid, r.uid }

// GetClientIP 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetClientIP() string { return "" }

// NewID 实现本类型在当前服务边界中的行为。
func (*manageRequest) NewID() uint { return 880001 }

// Authorized 实现本类型在当前服务边界中的行为。
func (*manageRequest) Authorized() bool { return true }

// CallService 实现本类型在当前服务边界中的行为。
func (*manageRequest) CallService(servertypes.IRouter, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}

// CallTargetService 实现本类型在当前服务边界中的行为。
func (*manageRequest) CallTargetService(servertypes.IRouter, *servertypes.TargetInfo, ...func(servertypes.IResponse)) (servertypes.IResponse, error) {
	return nil, nil
}

// GetValue 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetValue(string) string { return "" }

// Bind 实现本类型在当前服务边界中的行为。
func (r *manageRequest) Bind(target interface{}) error {
	data, err := json.Marshal(r.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// GoZeroBind 实现本类型在当前服务边界中的行为。
func (*manageRequest) GoZeroBind(interface{}) error { return nil }

// NewResponse 实现本类型在当前服务边界中的行为。
func (*manageRequest) NewResponse(interface{}, error) servertypes.IResponse { return nil }

// GetPath 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetPath() string { return "" }

// GetClaims 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetClaims(string) interface{} { return nil }

// ServiceName 实现本类型在当前服务边界中的行为。
func (*manageRequest) ServiceName() string { return contract.UserServiceName }

// GetServerInfo 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetServerInfo() *servertypes.TargetInfo { return nil }

// GetTargetServerInfo 实现本类型在当前服务边界中的行为。
func (*manageRequest) GetTargetServerInfo(string) *servertypes.TargetInfo { return nil }

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

// TestUserAndAddressSearchScopeOwner 验证当前场景的业务闭环和边界行为。
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

// TestAddressAddInjectsOwnerAndDisabledUserIsReadOnly 验证当前场景的业务闭环和边界行为。
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

// TestUserManageHasNoPhysicalRemoveRouter 验证当前场景的业务闭环和边界行为。
func TestUserManageHasNoPhysicalRemoveRouter(t *testing.T) {
	routers := NewUserManage().Routers()
	require.Len(t, routers, 4)
	for _, api := range routers {
		require.NotContains(t, api.RouterInfo().GetPath(), "/remove")
	}
}
