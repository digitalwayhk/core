package manage

import (
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 组装商品的查看、查询、新增、修改和删除管理路由。
type ProductManage struct {
	*managepkg.ManageService[models.Product]
}

// NewProductManage 创建商品管理服务并传入正确的 hook owner。
func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = managepkg.NewManageService[models.Product](own)
	return own
}

// Routers 只暴露本示例需要的商品 CRUD，不启用状态提交与发布。
func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove}
}

// ValidationAfter 在 ModelList 通用唯一检查前执行商品模型校验，返回明确的重名业务错误。
func (own *ProductManage) ValidationAfter(sender interface{}, _ servertypes.IRequest) error {
	switch operation := sender.(type) {
	case *managepkg.Add[models.Product]:
		if operation.Model != nil {
			return operation.Model.AddValid()
		}
	case *managepkg.Edit[models.Product]:
		if operation.Model != nil {
			return operation.Model.UpdateValid(operation.OldItem)
		}
	}
	return nil
}

// ViewModel 设置管理界面的商品标题和自动加载行为。
func (own *ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品管理"
	model.AutoLoad = true
}
