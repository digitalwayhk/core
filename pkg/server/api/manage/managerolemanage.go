// 本文件提供 Core 系统角色目录的 Manage 页面及内置角色保护。
package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ManageRoleManage 提供 Core 系统角色目录的标准 Manage 页面。
type ManageRoleManage struct {
	*DmpBase[smodels.ManageRoleModel]
}

// NewManageRoleManage 创建系统角色 Manage 页面。
func NewManageRoleManage() *ManageRoleManage {
	own := &ManageRoleManage{}
	own.DmpBase = NewDmpBase[smodels.ManageRoleModel](own)
	return own
}

// Routers 提供角色查看、搜索、编辑、新增和删除路由。
func (own *ManageRoleManage) Routers() []types.IRouter {
	routers := own.DmpBase.Routers()
	routers = append(routers, own.Add, own.Remove)
	return routers
}

// GetDefaultItems 返回必须存在且不可删除的内置角色。
func (*ManageRoleManage) GetDefaultItems() []*smodels.ManageRoleModel {
	return smodels.NewBuiltInManageRoles()
}

// ParseAfter 强制自定义角色使用 explicit 策略；客户端不能自行创建系统策略角色。
func (own *ManageRoleManage) ParseAfter(sender interface{}, _ types.IRequest) error {
	var role *smodels.ManageRoleModel
	switch operation := sender.(type) {
	case *manageservice.Add[smodels.ManageRoleModel]:
		role = operation.Model
	case *manageservice.Edit[smodels.ManageRoleModel]:
		role = operation.Model
	}
	if role != nil && !smodels.IsBuiltInManageRoleCode(role.Code) {
		role.Code = strings.ToLower(strings.TrimSpace(role.Code))
		role.Policy = types.ManageRolePolicyExplicit
		role.IsSystem = false
	}
	return nil
}

// ViewFieldModel 配置角色控制面字段属性。
func (own *ManageRoleManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.DmpBase.ViewFieldModel(model, field)
	switch {
	case field.IsFieldOrTitle("code"):
		field.Title = "角色编码"
		field.Required = true
	case field.IsFieldOrTitle("name"):
		field.Title = "角色名称"
		field.Required = true
	case field.IsFieldOrTitle("description"):
		field.Title = "说明"
	case field.IsFieldOrTitle("enabled"):
		field.Title = "启用"
	case field.IsFieldOrTitle("isdefault"):
		field.Title = "默认角色"
		field.Disabled = true
	case field.IsFieldOrTitle("issystem"):
		field.Title = "系统角色"
		field.Disabled = true
		field.IsSearch = true
	case field.IsFieldOrTitle("policy"):
		field.Title = "权限策略"
		field.Disabled = true
	}
}
