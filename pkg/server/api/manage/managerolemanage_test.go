package manage

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestManageRoleManageExposesStandardCRUD(t *testing.T) {
	manage := NewManageRoleManage()
	commands := make([]string, 0)
	for _, route := range manage.Routers() {
		commands = append(commands, route.RouterInfo().GetCommand())
	}

	require.ElementsMatch(t, []string{"view", "search", "add", "edit", "remove"}, commands)
}

func TestManageRolePermissionManageExposesBindingAndExactCRUD(t *testing.T) {
	manage := NewManageRolePermissionManage()
	commands := make([]string, 0)
	for _, route := range manage.Routers() {
		commands = append(commands, route.RouterInfo().GetCommand())
	}

	require.ElementsMatch(t, []string{"view", "search", "add", "edit", "remove", "bindmenu"}, commands)
}

func TestDefaultMenuPermissionRowsOnlyIncludeViewAndSearch(t *testing.T) {
	menu := smodels.NewMenuModel()
	menu.Url = "/api/manage/orders/ordermanage"
	for _, command := range []string{"view", "search", "add", "approve"} {
		permission := smodels.NewPermissionsModel()
		permission.Name = command
		permission.Url = menu.Url + "/" + command
		menu.Permissions = append(menu.Permissions, permission)
	}

	rows := defaultMenuPermissionRows("ops.operator", "orders", menu)

	require.Len(t, rows, 2)
	require.Equal(t, "view", rows[0].Command)
	require.Equal(t, menu.Url+"/view", rows[0].Path)
	require.Equal(t, "search", rows[1].Command)
	require.Equal(t, menu.Url+"/search", rows[1].Path)
}

func TestBindDefaultMenuPermissionsIsIdempotent(t *testing.T) {
	role := smodels.NewManageRoleModel()
	role.Code = "ops.operator"
	role.Enabled = true
	role.Policy = servertype.ManageRolePolicyExplicit
	menu := smodels.NewMenuModel()
	menu.Url = "/api/manage/orders/ordermanage"
	for _, command := range []string{"view", "search", "remove"} {
		permission := smodels.NewPermissionsModel()
		permission.Name = command
		permission.Url = menu.Url + "/" + command
		menu.Permissions = append(menu.Permissions, permission)
	}
	store := &manageRoleBindingStoreStub{role: role, menu: menu}
	binding := ManageRoleMenuBinding{RoleCode: role.Code, Service: "orders", MenuPath: menu.Url}

	first, err := bindDefaultMenuPermissions(context.Background(), store, binding)
	require.NoError(t, err)
	second, err := bindDefaultMenuPermissions(context.Background(), store, binding)
	require.NoError(t, err)

	require.Len(t, first, 2)
	require.Len(t, second, 2)
	require.Len(t, store.saved, 2, "重复绑定不得新增重复权限")
	require.Contains(t, store.saved, permissionBindingKey(role.Code, "orders", menu.Url+"/view", "view"))
	require.Contains(t, store.saved, permissionBindingKey(role.Code, "orders", menu.Url+"/search", "search"))
	require.NotContains(t, store.saved, permissionBindingKey(role.Code, "orders", menu.Url+"/remove", "remove"))
}

func TestBindDefaultMenuPermissionsRejectsBuiltInRole(t *testing.T) {
	role := smodels.NewBuiltInManageRoles()[1]
	menu := smodels.NewMenuModel()
	menu.Url = "/api/manage/orders/ordermanage"
	store := &manageRoleBindingStoreStub{role: role, menu: menu}

	_, err := bindDefaultMenuPermissions(context.Background(), store, ManageRoleMenuBinding{
		RoleCode: role.Code, Service: "orders", MenuPath: menu.Url,
	})

	require.Error(t, err)
	require.Equal(t, servertype.ErrorKindForbidden, servertype.ResolvePublicError(err).Kind)
}

type manageRoleBindingStoreStub struct {
	role  *smodels.ManageRoleModel
	menu  *smodels.MenuModel
	saved map[string]*smodels.ManageRolePermissionModel
}

func (s *manageRoleBindingStoreStub) FindRole(context.Context, string) (*smodels.ManageRoleModel, error) {
	return s.role, nil
}

func (s *manageRoleBindingStoreStub) FindMenu(context.Context, string) (*smodels.MenuModel, error) {
	return s.menu, nil
}

func (s *manageRoleBindingStoreStub) EnsurePermissions(_ context.Context, rows []*smodels.ManageRolePermissionModel) error {
	if s.saved == nil {
		s.saved = make(map[string]*smodels.ManageRolePermissionModel)
	}
	for _, row := range rows {
		key := permissionBindingKey(row.RoleCode, row.Service, row.Path, row.Command)
		if _, exists := s.saved[key]; !exists {
			s.saved[key] = row
		}
	}
	return nil
}

func permissionBindingKey(roleCode, service, path, command string) string {
	return roleCode + "\x00" + service + "\x00" + path + "\x00" + command
}
