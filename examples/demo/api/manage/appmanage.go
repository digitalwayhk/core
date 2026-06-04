package manage

import (
	"strings"

	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// IDoBefore 为每种写操作提供类型安全的前置钩子接口。
// AppManage[T].DoBefore 会按操作类型分派到对应方法，
// 子类只需覆盖需要的方法，无需手写 type switch。
type IDoBefore[T pt.IModel] interface {
	AddBefore(add *manage.Add[T], req st.IRequest) (interface{}, error, bool)
	EditBefore(edit *manage.Edit[T], req st.IRequest) (interface{}, error, bool)
	RemoveBefore(remove *manage.Remove[T], req st.IRequest) (interface{}, error, bool)
}

// AppManage 是应用层通用管理服务基类，封装跨业务公共逻辑。
//
// 默认只暴露 View 和 Search 路由（只读）。
// 需要写操作的子类在自己的 Routers() 中显式追加 own.Add / own.Edit / own.Remove。
//
// 每层的 ViewModel / ViewFieldModel / ViewCommandModel 均须先调用上层方法，
// 再追加本层逻辑，避免上层公共配置被覆盖。
type AppManage[T pt.IModel] struct {
	*manage.ManageService[T]
	instance interface{}
}

// NewAppManage 创建 AppManage。instance 须指向最终叶子类（self），
// 用于 IDoBefore 分派和框架钩子回调。
func NewAppManage[T pt.IModel](instance interface{}) *AppManage[T] {
	own := &AppManage[T]{instance: instance}
	own.ManageService = manage.NewManageService[T](instance)
	return own
}

// Routers 默认只注册只读路由（View + Search）。
// 需要写操作的子类须覆盖此方法并追加所需路由：
//
//	func (own *MyManage) Routers() []st.IRouter {
//	    routers := own.AppManage.Routers()   // [View, Search]
//	    return append(routers, own.Add, own.Edit, own.Remove)
//	}
func (own *AppManage[T]) Routers() []st.IRouter {
	return []st.IRouter{own.View, own.Search}
}

// ViewModel 设置公共视图属性。子类覆盖时须先调用 own.AppManage.ViewModel(v)。
func (own *AppManage[T]) ViewModel(v *view.ViewModel) {
	own.ManageService.ViewModel(v)
	v.AutoLoad = true
}

// ViewFieldModel 隐藏公共系统字段。子类覆盖时须先调用 own.AppManage.ViewFieldModel(model, field)。
func (own *AppManage[T]) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.ManageService.ViewFieldModel(model, field)
	name := strings.ToLower(field.Field)
	// 隐藏所有非主键 id 后缀字段，子类可按需恢复
	if strings.HasSuffix(name, "id") && !field.IsKey {
		field.Visible = false
	}
	switch name {
	case "updatedat", "updateduserid", "createduserid":
		field.Visible = false
	case "createdat":
		field.DataTimeType.SetDate(true)
	}
}

// DoBefore 将通用写入前置事件分派到 IDoBefore[T] 的具体方法，
// 子类通过实现 IDoBefore[T] 获得类型安全的生命周期钩子，无需手写 type switch。
func (own *AppManage[T]) DoBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	if idb, ok := own.instance.(IDoBefore[T]); ok {
		switch s := sender.(type) {
		case *manage.Add[T]:
			return idb.AddBefore(s, req)
		case *manage.Edit[T]:
			return idb.EditBefore(s, req)
		case *manage.Remove[T]:
			return idb.RemoveBefore(s, req)
		}
	}
	return nil, nil, false
}

// AppManage 提供 IDoBefore 的默认空实现，子类选择性覆盖需要的方法即可。
func (own *AppManage[T]) AddBefore(_ *manage.Add[T], _ st.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *AppManage[T]) EditBefore(_ *manage.Edit[T], _ st.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *AppManage[T]) RemoveBefore(_ *manage.Remove[T], _ st.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}
