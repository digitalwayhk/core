// 本文件验证示例模型只保存稳定业务键并保护管理员与角色关系身份。
package adminrbac

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminUserRoleStoresStableCodesWithoutRoleDatabaseID(t *testing.T) {
	typeOfRelation := reflect.TypeOf(AdminUserRoleModel{})
	_, hasRoleCode := typeOfRelation.FieldByName("RoleCode")
	_, hasRoleID := typeOfRelation.FieldByName("RoleID")
	_, hasUserCode := typeOfRelation.FieldByName("UserCode")

	require.True(t, hasRoleCode)
	require.True(t, hasUserCode)
	require.False(t, hasRoleID)

	relation := NewAdminUserRoleModel()
	relation.UserCode = "casdoor-user-1"
	relation.RoleCode = "ops.approver"
	require.NoError(t, relation.AddValid())
	require.NotEmpty(t, relation.GetHash())
}

func TestAdminUserUsesProviderIdentityAsStableCode(t *testing.T) {
	user := NewAdminUserModel()
	user.Code = "casdoor-user-1"
	user.Provider = "casdoor"
	user.ProviderSubject = "alice"
	user.Enabled = true

	require.NoError(t, user.AddValid())
	require.NotEmpty(t, user.GetHash())
}
