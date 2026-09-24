package manage

import (
	"github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type AdminUserManage struct {
	*manageservice.ManageService[models.AdminUserModel]
}

func NewAdminUserManage() *AdminUserManage {
	own := &AdminUserManage{}
	own.ManageService = manageservice.NewManageService[models.AdminUserModel](own)
	return own
}

// 管理员只能由可信 Casdoor callback 自动建立；管理页不提供 Add/Remove。
func (own *AdminUserManage) Routers() []servertype.IRouter {
	return []servertype.IRouter{own.View, own.Search, own.Edit}
}

func (*AdminUserManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administrators"
	}
	return "管理员"
}

func (*AdminUserManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("code") || field.IsFieldOrTitle("provider") ||
		field.IsFieldOrTitle("providersubject") || field.IsFieldOrTitle("isfirst") {
		field.Disabled = true
	}
}
