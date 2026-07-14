package manage

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// BusinessManage 为业务模型提供只读查询和公共状态字段规则。
type BusinessManage[T persistencetypes.IModel] struct {
	*ShopManage[T]
}

// NewBusinessManage 创建绑定最终 owner 的业务数据 Manage。
func NewBusinessManage[T persistencetypes.IModel](owner interface{}) *BusinessManage[T] {
	return &BusinessManage[T]{ShopManage: NewShopManage[T](owner)}
}

// Routers 默认只暴露 View 和 Search，避免普通编辑绕过状态机。
func (own *BusinessManage[T]) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

// ViewFieldModel 先应用服务公共字段规则，再把业务状态设为只读。
func (own *BusinessManage[T]) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.ShopManage.ViewFieldModel(model, field)
	if field.IsFieldOrTitle("Status") {
		field.Title = "业务状态"
		field.IsEdit = false
		field.IsSearch = true
	}
}
