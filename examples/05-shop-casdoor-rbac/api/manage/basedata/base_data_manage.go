package basedata

import (
	commonmanage "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// BaseDataManage 为基础资料提供 CRUD、字段格式和启停命令。
type BaseDataManage[T persistencetypes.IModel] struct {
	*commonmanage.ShopManage[T]
	Enable  *EnableBaseData[T]
	Disable *DisableBaseData[T]
}

// NewBaseDataManage 创建绑定最终 owner 的基础资料 Manage。
func NewBaseDataManage[T persistencetypes.IModel](owner interface{}) *BaseDataManage[T] {
	return &BaseDataManage[T]{
		ShopManage: commonmanage.NewShopManage[T](owner),
		Enable:     NewEnableBaseData[T](owner),
		Disable:    NewDisableBaseData[T](owner),
	}
}

// Routers 暴露基础资料的完整 CRUD 和启停命令。
func (own *BaseDataManage[T]) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.Enable, own.Disable}
}

// OnAddBefore 先保留 Shop 服务的 Add 规则，再对所有基础资料强制默认禁用并执行模型新增校验。
func (own *BaseDataManage[T]) OnAddBefore(operation *managepkg.Add[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnAddBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.Model != nil {
		if item, ok := any(operation.Model).(models.IBaseDataModel); ok {
			item.GetBaseDataModel().Enabled = false
		}
		if validator, ok := any(operation.Model).(persistencetypes.IModelValidHook); ok {
			if err := validator.AddValid(); err != nil {
				return nil, err, true
			}
		}
	}
	return nil, nil, false
}

// OnEditBefore 先保留 Shop 服务的 Edit 规则，再统一阻止基础资料绕过启停命令修改状态。
func (own *BaseDataManage[T]) OnEditBefore(operation *managepkg.Edit[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnEditBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.Model != nil {
		current, currentOK := any(operation.Model).(models.IBaseDataModel)
		old, oldOK := any(operation.OldItem).(models.IBaseDataModel)
		if currentOK && oldOK && current.GetBaseDataModel().Enabled != old.GetBaseDataModel().Enabled {
			return nil, models.NewBusinessError("启用状态只能通过启用或禁用命令修改"), true
		}
		if validator, ok := any(operation.Model).(persistencetypes.IModelValidHook); ok {
			if err := validator.UpdateValid(operation.OldItem); err != nil {
				return nil, err, true
			}
		}
	}
	return nil, nil, false
}

// OnRemoveBefore 先保留 Shop 服务的 Remove 规则，再统一执行基础资料模型的删除校验。
func (own *BaseDataManage[T]) OnRemoveBefore(operation *managepkg.Remove[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnRemoveBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.Model != nil {
		if validator, ok := any(operation.Model).(persistencetypes.IModelValidHook); ok {
			if err := validator.RemoveValid(); err != nil {
				return nil, err, true
			}
		}
	}
	return nil, nil, false
}

// ViewFieldModel 先应用服务公共字段规则，再配置基础资料字段。
func (own *BaseDataManage[T]) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.ShopManage.ViewFieldModel(model, field)
	switch {
	case field.IsFieldOrTitle("Code"):
		field.Title = "编码"
		field.IsSearch = true
	case field.IsFieldOrTitle("Name"):
		field.Title = "名称"
		field.IsSearch = true
	case field.IsFieldOrTitle("Enabled"):
		field.Title = "启用状态"
		field.IsEdit = false
		field.ComBoxValue(0, "禁用")
		field.ComBoxValue(1, "启用")
	case field.IsFieldOrTitle("Description"):
		field.Title = "说明"
	}
}

// ViewCommandModel 配置通用启用和禁用按钮。
func (own *BaseDataManage[T]) ViewCommandModel(command *view.CommandModel) {
	command.Visible = true
	command.IsSelectRow = true
	command.IsAlert = true
	switch command.Name {
	case "EnableBaseData":
		command.Title = "启用"
		command.Icon = "check"
	case "DisableBaseData":
		command.Title = "禁用"
		command.Icon = "ban"
	}
}

// ViewChildModel 先应用服务级子表规则。
func (own *BaseDataManage[T]) ViewChildModel(child *view.ViewChildModel) {
	own.ShopManage.ViewChildModel(child)
}
