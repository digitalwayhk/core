// 本文件验证 Core 内置管理员身份和角色关系的稳定持久化契约。
package smodels

import (
	"encoding/json"
	"reflect"
	"testing"

	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestManagePrincipalReservesBootstrapSlot 验证首个真实管理员由数据库唯一槽位仲裁。
func TestManagePrincipalReservesBootstrapSlot(t *testing.T) {
	field, ok := reflect.TypeOf(ManagePrincipalModel{}).FieldByName("BootstrapSlot")
	require.True(t, ok)
	require.Equal(t, reflect.Pointer, field.Type.Kind())
	require.Contains(t, field.Tag.Get("gorm"), "uniqueIndex")
	require.Equal(t, "-", field.Tag.Get("json"))
}

// TestManagePrincipalRoleUsesStableCodes 验证管理员角色关系不依赖数据库 ID。
func TestManagePrincipalRoleUsesStableCodes(t *testing.T) {
	relation := NewManagePrincipalRoleModel()
	relation.PrincipalCode = "manager-1"
	relation.RoleCode = servertype.ManageRoleViewer

	require.NotEmpty(t, relation.GetHash())
	require.NoError(t, relation.AddValid())
	require.Equal(t, servertype.ManageRoleViewer, relation.RoleCode)
}

// TestBootstrapManagePrincipalRoleCannotBeRemoved 验证首管理员的系统管理员关系不可解绑。
func TestBootstrapManagePrincipalRoleCannotBeRemoved(t *testing.T) {
	relation := NewManagePrincipalRoleModel()
	relation.PrincipalCode = "manager-1"
	relation.RoleCode = servertype.ManageRoleSystemAdmin
	relation.IsBootstrap = true

	require.NoError(t, relation.AddValid())
	err := relation.RemoveValid()
	require.Error(t, err)
	require.Equal(t, servertype.ErrorKindForbidden, servertype.ResolvePublicError(err).Kind)

	encoded, marshalErr := json.Marshal(relation)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), "isBootstrap")
}
