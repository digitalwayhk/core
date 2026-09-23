package manage

import (
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// CatalogItemManage 组装资料条目的完整管理界面能力：CRUD、提交发布、外键、子表、导入导出和复制。
type CatalogItemManage struct {
	*managepkg.ManageService[models.CatalogItem]
	ImportData *ImportData
	ExportData *ExportData
	CloneItem  *CloneItem
}

// NewCatalogItemManage 创建资料条目管理服务并绑定自定义命令。
func NewCatalogItemManage() *CatalogItemManage {
	own := &CatalogItemManage{}
	own.ManageService = managepkg.NewManageService[models.CatalogItem](own)
	own.ImportData = NewImportData(own)
	own.ExportData = NewExportData(own)
	own.CloneItem = NewCloneItem(own)
	return own
}

// GetList 通过 models 层取得当前服务统一的 Manage 模型列表。
func (*CatalogItemManage) GetList() interface{} {
	return models.NewManageModelList[models.CatalogItem]()
}

// GetLocaleTitle 返回资料条目管理页的中英标题。
func (*CatalogItemManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Catalog Items"
	}
	return "资料条目"
}

// GetDefaultItems 在条目表为空时按已有分类写入演示数据。
func (*CatalogItemManage) GetDefaultItems() []*models.CatalogItem {
	categories, err := models.NewCategory().Query(0, "")
	if err != nil || len(categories) == 0 {
		return nil
	}
	return models.DefaultCatalogItems(categories)
}

// Routers 暴露条目完整命令，包括导入、导出和带表单的复制。
func (own *CatalogItemManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{
		own.View, own.Search, own.Add, own.Edit, own.Remove, own.Submit, own.Release,
		own.ImportData, own.ExportData, own.CloneItem,
	}
}

// ValidationAfter 执行条目字段和唯一性校验。
func (*CatalogItemManage) ValidationAfter(sender interface{}, _ servertypes.IRequest) error {
	switch operation := sender.(type) {
	case *managepkg.Add[models.CatalogItem]:
		if operation.Model != nil {
			return operation.Model.AddValid()
		}
	case *managepkg.Edit[models.CatalogItem]:
		if operation.Model != nil {
			return operation.Model.UpdateValid(operation.OldItem)
		}
	case *managepkg.Remove[models.CatalogItem]:
		if operation.Model != nil {
			current, err := models.NewCatalogItem().FindByID(operation.Model.ID)
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

// DoAfter 在新增或修改成功后按 modelState 持久化子表。
func (own *CatalogItemManage) DoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error) {
	switch operation := sender.(type) {
	case *managepkg.Add[models.CatalogItem]:
		if operation.Model != nil {
			if err := operation.Model.SaveChildren(); err != nil {
				return nil, err
			}
			return operation.Model, nil
		}
	case *managepkg.Edit[models.CatalogItem]:
		if operation.Model != nil {
			if err := operation.Model.SaveChildren(); err != nil {
				return nil, err
			}
			return operation.Model, nil
		}
	}
	return own.ManageService.DoAfter(sender, req)
}

// SearchAfter 先保留默认演示数据，再把子表写入明细表并补齐外键对象。
func (own *CatalogItemManage) SearchAfter(sender interface{}, result *view.TableData, req servertypes.IRequest) (interface{}, error) {
	data, err := own.ManageService.SearchAfter(sender, result, req)
	if err != nil {
		return data, err
	}
	table, _ := data.(*view.TableData)
	if table == nil {
		table = result
	}
	if table == nil {
		return data, nil
	}
	rows, _ := table.Rows.([]*models.CatalogItem)
	for _, row := range rows {
		if row == nil {
			continue
		}
		if err := row.LoadChildren(); err != nil {
			return nil, err
		}
		if row.CategoryID > 0 && row.Category == nil {
			row.Category, _ = models.NewCategory().FindByID(row.CategoryID)
		}
	}
	return table, nil
}

// ViewModel 设置资料条目管理页面。
func (*CatalogItemManage) ViewModel(model *view.ViewModel) {
	model.Title = "资料条目"
	model.AutoLoad = true
	model.ShowComvtp = true
	model.AutoSearch = true
	model.Desc = "验证外键选择、多子表页签、字段控件、导入导出、高级搜索和列定制。"
}

// ViewFieldModel 配置条目字段控件：外键、枚举、密码、备注、数值和日期。
func (*CatalogItemManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	applyKindField(field)
	if field.IsFieldOrTitle("State") {
		field.Title = "状态"
	}
	if field.IsFieldOrTitle("Describe") {
		field.Title = "说明"
		field.IsRemark = true
		field.IsSearch = false
	}
	if field.IsFieldOrTitle("Code") {
		field.Title = "条目编码"
		field.Required = true
	}
	if field.IsFieldOrTitle("Name") {
		field.Title = "条目名称"
		field.Required = true
	}
	if field.IsFieldOrTitle("CategoryID") {
		field.Title = "所属分类"
		field.Required = true
	}
	if field.IsFieldOrTitle("Price") {
		field.Title = "价格"
		field.Precision = 2
	}
	if field.IsFieldOrTitle("Stock") {
		field.Title = "库存"
		field.Min = 0
	}
	if field.IsFieldOrTitle("Enabled") {
		field.Title = "启用"
	}
	if field.IsFieldOrTitle("Secret") {
		field.Title = "访问口令"
		field.IsPassword = true
		field.IsSearch = false
		field.Length = 64
	}
	if field.IsFieldOrTitle("Note") {
		field.Title = "备注"
		field.IsRemark = true
	}
	if field.IsFieldOrTitle("PublishedAt") {
		field.Title = "上架时间"
		field.DataTimeType.SetDate(true)
		field.DataTimeType.SetTime(true)
	}
}

// ViewCommandModel 配置导入导出不要求选中行，复制打开表单；
// 导入、导出、复制挂到新增下拉，演示 issplit / splitname。
func (*CatalogItemManage) ViewCommandModel(cmd *view.CommandModel) {
	switch cmd.Command {
	case "importdata":
		cmd.Title = "导入"
		cmd.IsSelectRow = false
		cmd.IsAlert = false
		cmd.IsSplit = true
		cmd.SplitName = "add"
	case "exportdata":
		cmd.Title = "导出"
		cmd.IsSelectRow = false
		cmd.IsAlert = false
		cmd.IsSplit = true
		cmd.SplitName = "add"
	case "cloneitem":
		cmd.Title = "复制"
		cmd.EditShow = true
		cmd.IsSelectRow = true
		cmd.IsAlert = false
		cmd.IsSplit = true
		cmd.SplitName = "add"
	}
}

// ViewChildModel 配置明细行和规格参数两个可增删改子表。
func (*CatalogItemManage) ViewChildModel(child *view.ViewChildModel) {
	if child.Name == "Lines" {
		child.Title = "明细行"
		child.ForeignKey = "ItemID"
		child.Sortindex = 0
		child.IsAdd = true
		child.IsEdit = true
		child.IsRemove = true
		for _, field := range child.Fields {
			if field.IsFieldOrTitle("ItemID") {
				field.Visible = false
				field.IsEdit = false
			}
			if field.IsFieldOrTitle("LineNo") {
				field.Title = "行号"
			}
			if field.IsFieldOrTitle("Name") {
				field.Title = "明细名称"
			}
			if field.IsFieldOrTitle("Quantity") {
				field.Title = "数量"
			}
			if field.IsFieldOrTitle("Amount") {
				field.Title = "金额"
			}
		}
	}
	if child.Name == "Specs" {
		child.Title = "规格参数"
		child.ForeignKey = "ItemID"
		child.Sortindex = 1
		child.IsAdd = true
		child.IsEdit = true
		child.IsRemove = true
		for _, field := range child.Fields {
			if field.IsFieldOrTitle("ItemID") {
				field.Visible = false
				field.IsEdit = false
			}
			if field.IsFieldOrTitle("SortNo") {
				field.Title = "排序"
			}
			if field.IsFieldOrTitle("SpecName") {
				field.Title = "参数名"
			}
			if field.IsFieldOrTitle("SpecValue") {
				field.Title = "参数值"
			}
		}
	}
}
