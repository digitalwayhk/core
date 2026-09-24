// 本文件定义消费方管理员目录页面；用户只能由可信 Casdoor callback 建立。
package manage

import (
	"github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// AdminUserManage 展示并允许维护已由 Casdoor callback 建立的管理员。
type AdminUserManage struct {
	*manageservice.ManageService[models.AdminUserModel]
}

// NewAdminUserManage 创建管理员 Manage 页面。
func NewAdminUserManage() *AdminUserManage {
	own := &AdminUserManage{}
	own.ManageService = manageservice.NewManageService[models.AdminUserModel](own)
	return own
}

// 管理员只能由可信 Casdoor callback 自动建立；管理页不提供 Add/Remove。
func (own *AdminUserManage) Routers() []servertype.IRouter {
	return []servertype.IRouter{own.View, own.Search, own.Edit}
}

// GetLocaleTitle 返回管理员页面标题。
func (*AdminUserManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administrators"
	}
	return "管理员"
}

// ViewFieldModel 将身份字段设为只读。
func (*AdminUserManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("code") || field.IsFieldOrTitle("provider") ||
		field.IsFieldOrTitle("providersubject") || field.IsFieldOrTitle("isfirst") {
		field.Disabled = true
	}
}
