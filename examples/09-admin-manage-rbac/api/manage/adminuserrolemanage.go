package manage

import (
	"github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type AdminUserRoleManage struct {
	*manageservice.ManageService[models.AdminUserRoleModel]
}

func NewAdminUserRoleManage() *AdminUserRoleManage {
	own := &AdminUserRoleManage{}
	own.ManageService = manageservice.NewManageService[models.AdminUserRoleModel](own)
	return own
}

func (own *AdminUserRoleManage) Routers() []servertype.IRouter {
	return []servertype.IRouter{own.View, own.Search, own.Add, own.Remove}
}

func (*AdminUserRoleManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administrator Roles"
	}
	return "管理员角色绑定"
}

func (*AdminUserRoleManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	switch {
	case field.IsFieldOrTitle("usercode"):
		field.Title = "管理员编码"
		field.Required = true
		field.Foreign = codeSelector(utils.GetTypeName(models.NewAdminUserModel()), "user", "username", "userCode")
	case field.IsFieldOrTitle("rolecode"):
		field.Title = "角色编码"
		field.Required = true
		field.Foreign = codeSelector(utils.GetTypeName(smodels.NewManageRoleModel()), "role", "name", "roleCode")
	}
}

func codeSelector(targetType, targetField, displayField, relationField string) *view.ForeignModel {
	relationType := utils.GetTypeName(models.NewAdminUserRoleModel())
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
