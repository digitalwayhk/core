package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/02-shop-payment/business"
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 演示商品 CRUD、输入规范化、唯一校验和引用删除保护。
type ProductManage struct {
	*managepkg.ManageService[models.Product]
}

// NewProductManage 创建商品管理服务并正确传入 hook owner。
func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = managepkg.NewManageService[models.Product](own)
	return own
}

// Routers 暴露商品完整 CRUD。
func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove}
}

// ParseAfter 在校验前规范化商品名称。
func (own *ProductManage) ParseAfter(sender interface{}, _ servertypes.IRequest) error {
	switch operation := sender.(type) {
	case *managepkg.Add[models.Product]:
		if operation.Model != nil {
			operation.Model.Name = strings.TrimSpace(operation.Model.Name)
		}
	case *managepkg.Edit[models.Product]:
		if operation.Model != nil {
			operation.Model.Name = strings.TrimSpace(operation.Model.Name)
		}
	}
	return nil
}

// ValidationAfter 调用业务层校验新增、编辑和删除约束。
func (own *ProductManage) ValidationAfter(sender interface{}, _ servertypes.IRequest) error {
	service := business.NewProductService()
	switch operation := sender.(type) {
	case *managepkg.Add[models.Product]:
		if operation.Model != nil {
			return service.ValidateCreate(operation.Model)
		}
	case *managepkg.Edit[models.Product]:
		if operation.Model != nil {
			return service.ValidateUpdate(operation.Model, operation.OldItem)
		}
	case *managepkg.Remove[models.Product]:
		if operation.Model != nil {
			return service.EnsureRemovable(operation.Model.ID)
		}
	}
	return nil
}

// ViewModel 设置商品管理页面。
func (own *ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品管理"
	model.AutoLoad = true
}

// ViewFieldModel 配置商品字段展示和查询能力。
func (own *ProductManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("Name") {
		field.Title = "商品名称"
		field.IsSearch = true
	}
	if field.IsFieldOrTitle("Price") {
		field.Title = "商品价格"
		field.Precision = 2
	}
}
