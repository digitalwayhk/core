package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type IGetDefaultItems[T pt.IModel] interface {
	GetDefaultItems() []*T
}

// DmpBase 是目录、菜单、权限等管理的基础结构体
type DmpBase[T pt.IModel] struct {
	*manage.ManageService[T]
	instance interface{} // 用于存储实例
}

func NewDmpBase[T pt.IModel](instance interface{}) *DmpBase[T] {
	own := &DmpBase[T]{
		instance: instance,
	}
	own.ManageService = manage.NewManageService[T](instance)
	return own
}

func (own *DmpBase[T]) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0)
	routers = append(routers, own.View)
	routers = append(routers, own.Search)
	routers = append(routers, own.Edit)
	return routers
}
func (own *DmpBase[T]) ViewModel(view *view.ViewModel) {
	view.AutoLoad = true
}
func (own *DmpBase[T]) ViewChildModel(child *view.ViewChildModel) {
	if child.Name == "MenuItems" {
		child.Title = "菜单成员"
	}
	if child.Name == "Permissions" {
		child.Title = "权限列表"
	}
	child.IsAdd = false
	child.IsEdit = true
	child.IsRemove = false
}
func (own *DmpBase[T]) ViewFieldModel(model interface{}, field *view.FieldModel) {
	if strings.Contains(field.Field, "id") ||
		field.IsFieldOrTitle("updatedat") {
		field.IsEdit = false
		field.Visible = false
		field.IsSearch = false
	}
	if field.IsFieldOrTitle("name") || field.IsFieldOrTitle("url") {
		field.Disabled = true
		// if field.IsFieldOrTitle("url") {
		// 	field.IsEdit = false
		// }
	}
	own.OnViewFieldModel(model, field)
}

func (own *DmpBase[T]) OnViewFieldModel(model interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("name") {
		field.Title = "名称"
	}
	if field.IsFieldOrTitle("title") {
		field.Title = "标题"
	}
	if field.IsFieldOrTitle("description") {
		field.Title = "描述"
	}
	if field.IsFieldOrTitle("sort") {
		field.Title = "排序索引"
	}
	if field.IsFieldOrTitle("icon") {
		field.Title = "图标"
	}
	if field.IsFieldOrTitle("url") {
		field.Title = "链接"
	}
}

func (own *DmpBase[T]) SearchAfter(sender interface{}, result *view.TableData, req types.IRequest) (interface{}, error) {
	if result.Total == 0 {
		if idg, ok := own.instance.(IGetDefaultItems[T]); ok {
			if items := idg.GetDefaultItems(); len(items) > 0 {
				if list := own.GetList().(*entity.ModelList[T]); list != nil {
					if err := list.Add(items...); err != nil {
						return nil, err
					}
					if err := list.Save(); err != nil {
						return nil, err
					}
					result.Rows = items
					result.Total = int64(len(items))
				}
			}
		}
	}
	return result, nil
}
