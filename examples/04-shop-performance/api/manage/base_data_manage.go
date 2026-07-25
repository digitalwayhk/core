package manage

import (
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// BaseDataManage 为基础资料提供 CRUD、字段格式和启停命令。
type BaseDataManage[T persistencetypes.IModel] struct {
	*ShopManage[T]
	Enable  *EnableBaseData[T]
	Disable *DisableBaseData[T]
}

// NewBaseDataManage 创建绑定最终 owner 的基础资料 Manage。
func NewBaseDataManage[T persistencetypes.IModel](owner interface{}) *BaseDataManage[T] {
	return &BaseDataManage[T]{
		ShopManage: NewShopManage[T](owner),
		Enable:     NewEnableBaseData[T](owner),
		Disable:    NewDisableBaseData[T](owner),
	}
}

// Routers 暴露基础资料的完整 CRUD 和启停命令。
func (own *BaseDataManage[T]) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.Enable, own.Disable}
}

// ParseAfter 先执行服务级处理，再规范化基础资料并强制新增项默认禁用。
func (own *BaseDataManage[T]) ParseAfter(sender interface{}, req servertypes.IRequest) error {
	if err := own.ShopManage.ParseAfter(sender, req); err != nil {
		return err
	}
	var item models.IBaseDataModel
	switch operation := sender.(type) {
	case *managepkg.Add[T]:
		item, _ = any(operation.Model).(models.IBaseDataModel)
		if item != nil {
			item.GetBaseDataModel().Enabled = false
		}
	case *managepkg.Edit[T]:
		item, _ = any(operation.Model).(models.IBaseDataModel)
	}
	if item != nil {
		return item.GetBaseDataModel().NormalizeBaseData()
	}
	return nil
}

// ValidationAfter 先执行服务级校验，再调用具体模型的新增、修改或删除校验。
func (own *BaseDataManage[T]) ValidationAfter(sender interface{}, req servertypes.IRequest) error {
	if err := own.ShopManage.ValidationAfter(sender, req); err != nil {
		return err
	}
	switch operation := sender.(type) {
	case *managepkg.Add[T]:
		if operation.Model != nil {
			if item, ok := any(operation.Model).(models.IBaseDataModel); ok {
				item.GetBaseDataModel().Enabled = false
			}
			if validator, ok := any(operation.Model).(persistencetypes.IModelValidHook); ok {
				return validator.AddValid()
			}
		}
	case *managepkg.Edit[T]:
		if operation.Model != nil {
			current, currentOK := any(operation.Model).(models.IBaseDataModel)
			old, oldOK := any(operation.OldItem).(models.IBaseDataModel)
			if currentOK && oldOK && current.GetBaseDataModel().Enabled != old.GetBaseDataModel().Enabled {
				return models.NewBusinessError("启用状态只能通过启用或禁用命令修改")
			}
			if validator, ok := any(operation.Model).(persistencetypes.IModelValidHook); ok {
				return validator.UpdateValid(operation.OldItem)
			}
		}
	case *managepkg.Remove[T]:
		if operation.Model != nil {
			if validator, ok := any(operation.Model).(persistencetypes.IModelValidHook); ok {
				return validator.RemoveValid()
			}
		}
	}
	return nil
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
