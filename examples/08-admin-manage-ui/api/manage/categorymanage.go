package manage

import (
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// CategoryManage 组装分类的查看、查询、增删改、提交和发布，并提供默认演示数据。
type CategoryManage struct {
	*managepkg.ManageService[models.Category]
}

// NewCategoryManage 创建分类管理服务并传入正确的 hook owner。
func NewCategoryManage() *CategoryManage {
	own := &CategoryManage{}
	own.ManageService = managepkg.NewManageService[models.Category](own)
	return own
}

// GetList 通过 models 层取得当前服务统一的 Manage 模型列表。
func (*CategoryManage) GetList() interface{} {
	return models.NewManageModelList[models.Category]()
}

// GetLocaleTitle 返回分类管理页的中英标题。
func (*CategoryManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Categories"
	}
	return "分类管理"
}

// GetDefaultItems 在分类表为空时写入演示分类，供外键选择和枚举筛选使用。
func (*CategoryManage) GetDefaultItems() []*models.Category {
	return models.DefaultCategories()
}

// Routers 暴露分类完整 CRUD 以及提交、发布。
func (own *CategoryManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.Submit, own.Release}
}

// ValidationAfter 在 ModelList 通用唯一检查前执行分类字段校验。
func (*CategoryManage) ValidationAfter(sender interface{}, _ servertypes.IRequest) error {
	switch operation := sender.(type) {
	case *managepkg.Add[models.Category]:
		if operation.Model != nil {
			return operation.Model.AddValid()
		}
	case *managepkg.Edit[models.Category]:
		if operation.Model != nil {
			return operation.Model.UpdateValid(operation.OldItem)
		}
	case *managepkg.Remove[models.Category]:
		if operation.Model != nil {
			current, err := models.NewCategory().FindByID(operation.Model.ID)
			if err != nil {
				return err
			}
			if current != nil {
				return current.RemoveValid()
			}
		}
	}
	return nil
}

// ViewModel 设置分类管理页面。
func (*CategoryManage) ViewModel(model *view.ViewModel) {
	model.Title = "分类管理"
	model.AutoLoad = true
	model.ShowComvtp = true
	model.AutoSearch = true
	model.Desc = "验证枚举分段筛选、提交发布和默认数据。"
}

// ViewFieldModel 配置分类字段标题、必填和 Comvtp。
func (*CategoryManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	applyKindField(field)
	if field.IsFieldOrTitle("State") {
		field.Title = "状态"
	}
	if field.IsFieldOrTitle("Code") {
		field.Title = "分类编码"
		field.Required = true
	}
	if field.IsFieldOrTitle("Name") {
		field.Title = "分类名称"
		field.Required = true
	}
	if field.IsFieldOrTitle("Enabled") {
		field.Title = "启用"
	}
	if field.IsFieldOrTitle("Describe") {
		field.Title = "说明"
		field.IsRemark = true
	}
}
