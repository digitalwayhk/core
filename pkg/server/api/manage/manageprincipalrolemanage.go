// 本文件提供 Core 管理员主体与稳定 RoleCode 的绑定页面。
package manage

import (
	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ManagePrincipalRoleManage 提供管理员与角色的选择、绑定和解除绑定。
type ManagePrincipalRoleManage struct {
	*DmpBase[smodels.ManagePrincipalRoleModel]
}

// NewManagePrincipalRoleManage 创建管理员角色绑定页面。
func NewManagePrincipalRoleManage() *ManagePrincipalRoleManage {
	own := &ManagePrincipalRoleManage{}
	own.DmpBase = NewDmpBase[smodels.ManagePrincipalRoleModel](own)
	return own
}

// Routers 只开放查看、搜索、绑定和解除绑定。
func (own *ManagePrincipalRoleManage) Routers() []servertype.IRouter {
	return []servertype.IRouter{own.View, own.Search, own.Add, own.Remove}
}

// GetLocaleTitle 返回管理员角色绑定页面标题。
func (*ManagePrincipalRoleManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administrator Roles"
	}
	return "管理员角色绑定"
}

// ViewFieldModel 用 PrincipalCode 和 RoleCode 作为选择器稳定键。
func (own *ManagePrincipalRoleManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.DmpBase.ViewFieldModel(model, field)
	switch {
	case field.IsFieldOrTitle("principalcode"):
		field.Title = "管理员编码"
		field.Required = true
		field.Foreign = manageCodeSelector(
			utils.GetTypeName(smodels.NewManagePrincipalModel()), "principal", "username", "principalCode",
		)
	case field.IsFieldOrTitle("rolecode"):
		field.Title = "角色编码"
		field.Required = true
		field.Foreign = manageCodeSelector(
			utils.GetTypeName(smodels.NewManageRoleModel()), "role", "name", "roleCode",
		)
	}
}

func manageCodeSelector(targetType, targetField, displayField, relationField string) *view.ForeignModel {
	relationType := utils.GetTypeName(smodels.NewManagePrincipalRoleModel())
	return &view.ForeignModel{
		IsFkey:             true,
		OneObjectTypeName:  targetType,
		OneObjectName:      targetType,
		OneObjectField:     targetField,
		OneObjectFieldKey:  "code",
		OneDisplayName:     displayField,
		ManyObjectTypeName: relationType,
		ManyObjectName:     relationType,
		ManyObjectField:    targetField,
		ManyObjectFieldKey: relationField,
		FModel: &view.ViewModel{
			Name:  targetType,
			Title: targetType,
			Fields: []*view.FieldModel{
				{Field: "code", IsKey: true, Type: "string", IsSearch: true, Visible: true, Sorter: true},
				{Field: displayField, Type: "string", IsSearch: true, Visible: true, Sorter: true},
			},
		},
	}
}
