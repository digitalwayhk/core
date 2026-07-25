package transaction

import (
	commonmanage "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/manage/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

const BusinessManageMaxPageSize = 50

// BusinessManage 为业务模型提供只读查询和公共状态字段规则。
type BusinessManage[T persistencetypes.IModel] struct {
	*commonmanage.ShopManage[T]
}

// NewBusinessManage 创建绑定最终 owner 的业务数据 Manage。
func NewBusinessManage[T persistencetypes.IModel](owner interface{}) *BusinessManage[T] {
	return &BusinessManage[T]{ShopManage: commonmanage.NewShopManage[T](owner)}
}

// Routers 默认只暴露 View 和 Search，避免普通编辑绕过状态机。
func (own *BusinessManage[T]) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

// OnAddBefore 保留 Shop 的服务级 Add 规则，再阻止业务数据绕过状态机使用通用新增。
func (own *BusinessManage[T]) OnAddBefore(operation *managepkg.Add[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnAddBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	return nil, models.NewBusinessError("业务数据必须通过专用命令新增"), true
}

// OnEditBefore 保留 Shop 的服务级 Edit 规则，再阻止业务数据绕过状态机直接修改。
func (own *BusinessManage[T]) OnEditBefore(operation *managepkg.Edit[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnEditBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	return nil, models.NewBusinessError("业务数据必须通过专用命令修改"), true
}

// OnRemoveBefore 保留 Shop 的服务级 Remove 规则，再阻止业务数据绕过撤销或退款流程直接删除。
func (own *BusinessManage[T]) OnRemoveBefore(operation *managepkg.Remove[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnRemoveBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	return nil, models.NewBusinessError("业务数据必须通过专用命令删除"), true
}

// OnSearchBefore 先继承 Shop 的查询规则，再将可能包含预加载的业务数据收紧为每页 50 条。
func (own *BusinessManage[T]) OnSearchBefore(operation *managepkg.Search[T], req servertypes.IRequest) (interface{}, error, bool) {
	data, err, stop := own.ShopManage.OnSearchBefore(operation, req)
	if stop || err != nil {
		return data, err, stop
	}
	if operation != nil && operation.SearchItem != nil && operation.SearchItem.Size > BusinessManageMaxPageSize {
		operation.SearchItem.Size = BusinessManageMaxPageSize
	}
	return nil, nil, false
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
