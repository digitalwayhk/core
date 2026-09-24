// 本文件验证 Core 管理员页面只按稳定 Code 管理主体和角色关系。
package manage

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

// TestManagePrincipalRoleManageSelectsPrincipalByStableCode 验证管理员选择器不依赖数据库 ID。
func TestManagePrincipalRoleManageSelectsPrincipalByStableCode(t *testing.T) {
	manager := NewManagePrincipalRoleManage()
	field := &view.FieldModel{Field: "principalCode", PropField: "PrincipalCode", Type: "string"}

	manager.ViewFieldModel(nil, field)

	requireManageForeignCodeSelector(t, field, "ManagePrincipalModel", "principalCode", "username")
}

// TestManagePrincipalEditRestoresTrustedIdentity 验证页面编辑只允许改变 Enabled。
func TestManagePrincipalEditRestoresTrustedIdentity(t *testing.T) {
	manager := NewManagePrincipalManage()
	slot := smodels.ManagePrincipalBootstrapSlot
	old := smodels.NewManagePrincipalModel()
	old.Code = "principal-1"
	old.Username = "alice"
	old.Provider = servertype.AuthProviderCasdoor
	old.ProviderSubject = "casdoor/alice"
	old.Enabled = true
	old.IsFirst = true
	old.BootstrapSlot = &slot
	requestModel := smodels.NewManagePrincipalModel()
	requestModel.Enabled = true
	edit := manageservice.NewEdit[smodels.ManagePrincipalModel](manager)
	edit.OldItem = old
	edit.Model = requestModel

	_, err, stop := manager.DoBefore(edit, nil)

	require.NoError(t, err)
	require.False(t, stop)
	require.Equal(t, old.Code, requestModel.Code)
	require.Equal(t, old.Username, requestModel.Username)
	require.Equal(t, old.Provider, requestModel.Provider)
	require.Equal(t, old.ProviderSubject, requestModel.ProviderSubject)
	require.Equal(t, old.IsFirst, requestModel.IsFirst)
	require.Equal(t, old.BootstrapSlot, requestModel.BootstrapSlot)
}

// TestManagePrincipalEditRejectsDisablingBootstrapPrincipal 防止停用唯一引导管理员。
func TestManagePrincipalEditRejectsDisablingBootstrapPrincipal(t *testing.T) {
	manager := NewManagePrincipalManage()
	slot := smodels.ManagePrincipalBootstrapSlot
	old := smodels.NewManagePrincipalModel()
	old.Code = "principal-1"
	old.Provider = servertype.AuthProviderCasdoor
	old.ProviderSubject = "casdoor/alice"
	old.Enabled = true
	old.IsFirst = true
	old.BootstrapSlot = &slot
	requestModel := smodels.NewManagePrincipalModel()
	requestModel.Enabled = false
	edit := manageservice.NewEdit[smodels.ManagePrincipalModel](manager)
	edit.OldItem = old
	edit.Model = requestModel

	_, err, stop := manager.DoBefore(edit, nil)

	require.Error(t, err)
	require.True(t, stop)
	require.Equal(t, servertype.ErrorKindForbidden, servertype.ResolvePublicError(err).Kind)
}

// TestManagePrincipalRoleManageSelectsRoleByStableCode 验证角色选择器使用 RoleCode。
func TestManagePrincipalRoleManageSelectsRoleByStableCode(t *testing.T) {
	manager := NewManagePrincipalRoleManage()
	field := &view.FieldModel{Field: "roleCode", PropField: "RoleCode", Type: "string"}

	manager.ViewFieldModel(nil, field)

	requireManageForeignCodeSelector(t, field, "ManageRoleModel", "roleCode", "name")
}

func requireManageForeignCodeSelector(
	t *testing.T,
	field *view.FieldModel,
	objectType string,
	relationField string,
	displayField string,
) {
	t.Helper()
	require.NotNil(t, field.Foreign)
	require.Equal(t, objectType, field.Foreign.OneObjectTypeName)
	require.Equal(t, "code", field.Foreign.OneObjectFieldKey)
	require.Equal(t, displayField, field.Foreign.OneDisplayName)
	require.Equal(t, relationField, field.Foreign.ManyObjectFieldKey)
}
