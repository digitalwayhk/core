package manage

import (
	"github.com/digitalwayhk/core/examples/03-manage-crud/models"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 嵌入 ManageService 后自动拥有标准管理路由。
type ProductManage struct {
	*managepkg.ManageService[models.Product]
}

// NewProductManage 创建商品管理服务。
func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = managepkg.NewManageService[models.Product](own)
	return own
}

// ViewModel 可以调整自动生成给前端的页面 schema。
func (own *ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品管理"
	model.AutoLoad = true
}
