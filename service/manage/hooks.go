package manage

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// IDoBefore 定义 ManageService 可选的细粒度前置 Hook。
// 业务基座嵌入 HookedManageService 后，只需要在最终 Manage 或中间基座重写关心的方法。
type IDoBefore[T persistencetypes.IModel] interface {
	OnDoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool)
	OnViewBefore(operation *View[T], req servertypes.IRequest) (interface{}, error, bool)
	OnAddBefore(operation *Add[T], req servertypes.IRequest) (interface{}, error, bool)
	OnEditBefore(operation *Edit[T], req servertypes.IRequest) (interface{}, error, bool)
	OnRemoveBefore(operation *Remove[T], req servertypes.IRequest) (interface{}, error, bool)
	OnSearchBefore(operation *Search[T], req servertypes.IRequest) (interface{}, error, bool)
}

// IDoAfter 定义 ManageService 可选的细粒度后置 Hook。
// 返回 nil 表示继续使用框架默认返回值或查询结果。
type IDoAfter[T persistencetypes.IModel] interface {
	OnDoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error)
	OnViewAfter(operation *View[T], req servertypes.IRequest) (interface{}, error)
	OnAddAfter(operation *Add[T], req servertypes.IRequest) (interface{}, error)
	OnEditAfter(operation *Edit[T], req servertypes.IRequest) (interface{}, error)
	OnRemoveAfter(operation *Remove[T], req servertypes.IRequest) (interface{}, error)
	OnSearchAfter(operation *Search[T], result *view.TableData, req servertypes.IRequest) (interface{}, error)
}

// HookedManageService 是 ManageService 的可选辅助基类。
// 它只负责把框架粗粒度 Do/Search Hook 分派到细粒度 On... 方法，不改变 ManageService 的默认语义。
type HookedManageService[T persistencetypes.IModel] struct {
	*ManageService[T]
	owner interface{}
}

func NewHookedManageService[T persistencetypes.IModel](owner interface{}) *HookedManageService[T] {
	return &HookedManageService[T]{
		ManageService: NewManageService[T](owner),
		owner:         owner,
	}
}

func (own *HookedManageService[T]) DoBefore(sender interface{}, req servertypes.IRequest) (data interface{}, err error, stop bool) {
	hook := own.beforeHook()
	if data, err, stop = hook.OnDoBefore(sender, req); stop || err != nil {
		return data, err, stop
	}
	switch operation := sender.(type) {
	case *View[T]:
		return hook.OnViewBefore(operation, req)
	case *Add[T]:
		return hook.OnAddBefore(operation, req)
	case *Edit[T]:
		return hook.OnEditBefore(operation, req)
	case *Remove[T]:
		return hook.OnRemoveBefore(operation, req)
	default:
		return nil, nil, false
	}
}

func (own *HookedManageService[T]) DoAfter(sender interface{}, req servertypes.IRequest) (data interface{}, err error) {
	hook := own.afterHook()
	if data, err = hook.OnDoAfter(sender, req); data != nil || err != nil {
		return data, err
	}
	switch operation := sender.(type) {
	case *View[T]:
		return hook.OnViewAfter(operation, req)
	case *Add[T]:
		return hook.OnAddAfter(operation, req)
	case *Edit[T]:
		return hook.OnEditAfter(operation, req)
	case *Remove[T]:
		return hook.OnRemoveAfter(operation, req)
	default:
		return nil, nil
	}
}

func (own *HookedManageService[T]) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	if operation, ok := sender.(*Search[T]); ok {
		return own.beforeHook().OnSearchBefore(operation, req)
	}
	return nil, nil, false
}

func (own *HookedManageService[T]) SearchAfter(sender interface{}, result *view.TableData, req servertypes.IRequest) (interface{}, error) {
	data, err := own.ManageService.SearchAfter(sender, result, req)
	if err != nil {
		return nil, err
	}
	if current, ok := data.(*view.TableData); ok {
		result = current
	}
	if operation, ok := sender.(*Search[T]); ok {
		if custom, customErr := own.afterHook().OnSearchAfter(operation, result, req); custom != nil || customErr != nil {
			return custom, customErr
		}
	}
	return result, nil
}

func (own *HookedManageService[T]) beforeHook() IDoBefore[T] {
	if own != nil && own.owner != nil {
		if hook, ok := own.owner.(IDoBefore[T]); ok {
			return hook
		}
	}
	return own
}

func (own *HookedManageService[T]) afterHook() IDoAfter[T] {
	if own != nil && own.owner != nil {
		if hook, ok := own.owner.(IDoAfter[T]); ok {
			return hook
		}
	}
	return own
}

func (own *HookedManageService[T]) OnDoBefore(interface{}, servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *HookedManageService[T]) OnViewBefore(*View[T], servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *HookedManageService[T]) OnAddBefore(*Add[T], servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *HookedManageService[T]) OnEditBefore(*Edit[T], servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *HookedManageService[T]) OnRemoveBefore(*Remove[T], servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *HookedManageService[T]) OnSearchBefore(*Search[T], servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

func (own *HookedManageService[T]) OnDoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (own *HookedManageService[T]) OnViewAfter(*View[T], servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (own *HookedManageService[T]) OnAddAfter(*Add[T], servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (own *HookedManageService[T]) OnEditAfter(*Edit[T], servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (own *HookedManageService[T]) OnRemoveAfter(*Remove[T], servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (own *HookedManageService[T]) OnSearchAfter(*Search[T], *view.TableData, servertypes.IRequest) (interface{}, error) {
	return nil, nil
}
