// 本文件验证角色稳定键、内置策略保护和权限哈希唯一身份。
package smodels

import (
	"reflect"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestBuiltInManageRoles(t *testing.T) {
	roles := NewBuiltInManageRoles()

	require.Len(t, roles, 2)
	require.Equal(t, types.ManageRoleSystemAdmin, roles[0].Code)
	require.Equal(t, types.ManageRolePolicyGrantAll, roles[0].Policy)
	require.True(t, roles[0].IsSystem)
	require.False(t, roles[0].IsDefault)

	require.Equal(t, types.ManageRoleViewer, roles[1].Code)
	require.Equal(t, types.ManageRolePolicyReadOnly, roles[1].Policy)
	require.True(t, roles[1].IsSystem)
	require.True(t, roles[1].IsDefault)
}

func TestManageRoleModelUsesStableCodeHash(t *testing.T) {
	first := NewManageRoleModel()
	first.Code = "ops.approver"
	second := NewManageRoleModel()
	second.Code = " ops.approver "

	require.NotEmpty(t, first.GetHash())
	require.Equal(t, first.GetHash(), second.GetHash())

	field, ok := reflect.TypeOf(ManageRoleModel{}).FieldByName("Code")
	require.True(t, ok)
	require.Contains(t, field.Tag.Get("gorm"), "uniqueIndex")
}

func TestManageRoleModelRejectsInvalidPolicyAndCodeChanges(t *testing.T) {
	role := NewManageRoleModel()
	role.Code = "ops.approver"
	role.Name = "Approver"
	role.Policy = types.ManageRolePolicyGrantAll
	require.Error(t, role.AddValid(), "custom roles must not grant all")

	role.Policy = types.ManageRolePolicyExplicit
	require.NoError(t, role.AddValid())

	old := NewManageRoleModel()
	old.Code = "ops.approver"
	old.Policy = types.ManageRolePolicyExplicit
	role.Code = "ops.operator"
	require.Error(t, role.UpdateValid(old), "role code must remain immutable")
}

func TestManageRoleModelRejectsCustomDefaultRole(t *testing.T) {
	role := NewManageRoleModel()
	role.Code = "ops.default"
	role.Name = "Default operator"
	role.Enabled = true
	role.IsDefault = true
	role.Policy = types.ManageRolePolicyExplicit

	require.Error(t, role.AddValid())
}

func TestManageRoleModelProtectsBuiltInRoles(t *testing.T) {
	viewer := NewBuiltInManageRoles()[1]
	require.Error(t, viewer.RemoveValid())

	changed := *viewer
	changed.Policy = types.ManageRolePolicyExplicit
	require.Error(t, changed.UpdateValid(viewer))
}

func TestManageRolePermissionUsesHashIdentityWithoutOversizedCompositeIndex(t *testing.T) {
	permission := NewManageRolePermissionModel()
	permission.RoleCode = "ops.approver"
	permission.Service = "orders"
	permission.Path = "/api/manage/orders/ordermanage/approve"
	permission.Command = "approve"

	require.NoError(t, permission.AddValid())
	require.NotEmpty(t, permission.GetHash())
	require.NotContains(t, permission.GetHash(), permission.Path)

	sameIdentity := NewManageRolePermissionModel()
	sameIdentity.RoleCode = permission.RoleCode
	sameIdentity.Service = permission.Service
	sameIdentity.Path = permission.Path
	sameIdentity.Command = permission.Command
	require.Equal(t, permission.GetHash(), sameIdentity.GetHash())

	typeOfPermission := reflect.TypeOf(ManageRolePermissionModel{})
	for _, name := range []string{"RoleCode", "Service", "Path", "Command"} {
		field, ok := typeOfPermission.FieldByName(name)
		require.True(t, ok)
		tag := field.Tag.Get("gorm")
		require.NotContains(t, tag, "uniqueIndex:idx_manage_role_permission", tag)
	}
}

func TestManageRolePermissionRejectsUnsafeOrMissingValues(t *testing.T) {
	tests := []ManageRolePermissionModel{
		{RoleCode: "", Service: "orders", Path: "/api/manage/orders/view", Command: "view"},
		{RoleCode: "ops.approver", Service: "", Path: "/api/manage/orders/view", Command: "view"},
		{RoleCode: "ops.approver", Service: "orders", Path: "relative", Command: "view"},
		{RoleCode: "ops.approver", Service: "orders", Path: "/api/manage/orders/view", Command: "VIEW"},
		{RoleCode: "ops.approver", Service: "orders", Path: "/api/manage/orders/view", Command: "view,edit"},
	}

	for _, value := range tests {
		permission := value
		permission.Model = NewManageRolePermissionModel().Model
		require.Error(t, permission.AddValid(), "%+v", value)
	}
}
