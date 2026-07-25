package common

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/zeromicro/go-zero/core/logx"
)

const ShopManageMaxPageSize = 100

// IDoBefore 定义 Shop Manage 的细粒度前置 Hook。
// 具体 Manage 通过嵌入 ShopManage 自动获得默认实现，只需重写关心的方法。
type IDoBefore[T persistencetypes.IModel] interface {
	OnDoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool)
	OnViewBefore(operation *managepkg.View[T], req servertypes.IRequest) (interface{}, error, bool)
	OnAddBefore(operation *managepkg.Add[T], req servertypes.IRequest) (interface{}, error, bool)
	OnEditBefore(operation *managepkg.Edit[T], req servertypes.IRequest) (interface{}, error, bool)
	OnRemoveBefore(operation *managepkg.Remove[T], req servertypes.IRequest) (interface{}, error, bool)
	OnSearchBefore(operation *managepkg.Search[T], req servertypes.IRequest) (interface{}, error, bool)
}

// IDoAfter 定义 Shop Manage 的细粒度后置 Hook。
// 返回 nil 表示继续使用 ShopManage 的默认返回值或查询结果。
type IDoAfter[T persistencetypes.IModel] interface {
	OnDoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error)
	OnViewAfter(operation *managepkg.View[T], req servertypes.IRequest) (interface{}, error)
	OnAddAfter(operation *managepkg.Add[T], req servertypes.IRequest) (interface{}, error)
	OnEditAfter(operation *managepkg.Edit[T], req servertypes.IRequest) (interface{}, error)
	OnRemoveAfter(operation *managepkg.Remove[T], req servertypes.IRequest) (interface{}, error)
	OnSearchAfter(operation *managepkg.Search[T], result *view.TableData, req servertypes.IRequest) (interface{}, error)
}

// ShopManage 是继承商城全部 Manage 的服务级公共层。
// owner 必须始终是最终具体 Manage，确保框架 hook 分派正确。
type ShopManage[T persistencetypes.IModel] struct {
	*managepkg.ManageService[T]
	owner interface{}
}

// NewShopManage 创建绑定最终 owner 的服务级 Manage。
func NewShopManage[T persistencetypes.IModel](owner interface{}) *ShopManage[T] {
	return &ShopManage[T]{ManageService: managepkg.NewManageService[T](owner), owner: owner}
}

// DoBefore 是服务级前置总入口，会把所有派生 Manage 的命令分派到对应 On...Before。
// 具体 Manage 可直接重写某个 On 方法替换父级行为，也可先调用父级再附加条件或业务。
func (own *ShopManage[T]) DoBefore(sender interface{}, req servertypes.IRequest) (data interface{}, err error, stop bool) {
	defer func() {
		if err != nil {
			own.logManageResult(req, "before", err)
		}
	}()
	hook := own.doBeforeHook()
	if data, err, stop = hook.OnDoBefore(sender, req); stop || err != nil {
		return
	}
	switch operation := sender.(type) {
	case *managepkg.View[T]:
		data, err, stop = hook.OnViewBefore(operation, req)
	case *managepkg.Add[T]:
		data, err, stop = hook.OnAddBefore(operation, req)
	case *managepkg.Edit[T]:
		data, err, stop = hook.OnEditBefore(operation, req)
	case *managepkg.Remove[T]:
		data, err, stop = hook.OnRemoveBefore(operation, req)
	}
	return
}

// DoAfter 是服务级后置总入口，会把所有派生 Manage 的成功命令分派到对应 On...After。
// 各层可在这些方法中分拆审计、缓存失效、事件发布或返回值转换，避免每个最终 Manage 重复实现。
func (own *ShopManage[T]) DoAfter(sender interface{}, req servertypes.IRequest) (data interface{}, err error) {
	defer func() {
		own.logManageResult(req, "after", err)
	}()
	hook := own.doAfterHook()
	if data, err = hook.OnDoAfter(sender, req); data != nil || err != nil {
		return
	}
	switch operation := sender.(type) {
	case *managepkg.View[T]:
		data, err = hook.OnViewAfter(operation, req)
	case *managepkg.Add[T]:
		data, err = hook.OnAddAfter(operation, req)
	case *managepkg.Edit[T]:
		data, err = hook.OnEditAfter(operation, req)
	case *managepkg.Remove[T]:
		data, err = hook.OnRemoveAfter(operation, req)
	}
	return
}

// SearchBefore 是所有派生 Manage 普通查询的服务级分派入口。
func (own *ShopManage[T]) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	if search, ok := sender.(*managepkg.Search[T]); ok {
		return own.doBeforeHook().OnSearchBefore(search, req)
	}
	return nil, nil, false
}

// SearchAfter 先保留框架默认项行为，再分派到最终 Manage 的 OnSearchAfter。
func (own *ShopManage[T]) SearchAfter(sender interface{}, result *view.TableData, req servertypes.IRequest) (interface{}, error) {
	data, err := own.ManageService.SearchAfter(sender, result, req)
	if err != nil {
		return nil, err
	}
	if current, ok := data.(*view.TableData); ok {
		result = current
	}
	if search, ok := sender.(*managepkg.Search[T]); ok {
		if custom, customErr := own.doAfterHook().OnSearchAfter(search, result, req); custom != nil || customErr != nil {
			return custom, customErr
		}
	}
	return result, nil
}

func (own *ShopManage[T]) doBeforeHook() IDoBefore[T] {
	if hook, ok := own.owner.(IDoBefore[T]); ok {
		return hook
	}
	return own
}

func (own *ShopManage[T]) doAfterHook() IDoAfter[T] {
	if hook, ok := own.owner.(IDoAfter[T]); ok {
		return hook
	}
	return own
}

// OnDoBefore 代表整个 Shop 服务所有 Manage 命令共用的前置层。
// 适合放置全服务授权、维护窗口、幂等键或审计上下文等与动作无关的逻辑。
func (own *ShopManage[T]) OnDoBefore(interface{}, servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

// OnViewBefore 是视图生成前的可选扩展点。
func (own *ShopManage[T]) OnViewBefore(*managepkg.View[T], servertypes.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}

// OnAddBefore 会在所有派生 Manage 的 Add 持久化前执行，默认要求有效管理身份。
func (own *ShopManage[T]) OnAddBefore(_ *managepkg.Add[T], req servertypes.IRequest) (interface{}, error, bool) {
	return validateShopManager(req)
}

// OnEditBefore 会在所有派生 Manage 的 Edit 持久化前执行，默认要求有效管理身份。
func (own *ShopManage[T]) OnEditBefore(_ *managepkg.Edit[T], req servertypes.IRequest) (interface{}, error, bool) {
	return validateShopManager(req)
}

// OnRemoveBefore 会在所有派生 Manage 的 Remove 持久化前执行，默认要求有效管理身份。
func (own *ShopManage[T]) OnRemoveBefore(_ *managepkg.Remove[T], req servertypes.IRequest) (interface{}, error, bool) {
	return validateShopManager(req)
}

// OnSearchBefore 会在所有派生 Manage 的 Search 前执行，统一限制分页并补齐稳定排序。
func (own *ShopManage[T]) OnSearchBefore(search *managepkg.Search[T], _ servertypes.IRequest) (interface{}, error, bool) {
	if search != nil && search.SearchItem != nil {
		if search.SearchItem.Size > ShopManageMaxPageSize {
			search.SearchItem.Size = ShopManageMaxPageSize
		}
		if len(search.SearchItem.SortList) == 0 {
			search.SearchItem.SortList = []*view.SearchSort{{Name: "ID", Isdesc: true}}
		}
	}
	return nil, nil, false
}

// OnDoAfter 代表整个 Shop 服务所有 Manage 命令共用的后置层。
// 适合放置通用事件发布、统计或返回值包装；默认不替换后续结果。
func (own *ShopManage[T]) OnDoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

// OnViewAfter 是视图生成成功后的可选扩展点。
func (own *ShopManage[T]) OnViewAfter(*managepkg.View[T], servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

// OnAddAfter 会在所有派生 Manage 的 Add 成功后执行，日志由 DoAfter 统一记录。
func (own *ShopManage[T]) OnAddAfter(operation *managepkg.Add[T], _ servertypes.IRequest) (interface{}, error) {
	return operation.Model, nil
}

// OnEditAfter 会在所有派生 Manage 的 Edit 成功后执行，默认返回已合并的持久化对象。
func (own *ShopManage[T]) OnEditAfter(operation *managepkg.Edit[T], _ servertypes.IRequest) (interface{}, error) {
	if operation.OldItem != nil {
		return operation.OldItem, nil
	}
	return operation.Model, nil
}

// OnRemoveAfter 会在所有派生 Manage 的 Remove 成功后执行，默认返回被删除对象。
func (own *ShopManage[T]) OnRemoveAfter(operation *managepkg.Remove[T], _ servertypes.IRequest) (interface{}, error) {
	return operation.Model, nil
}

// OnSearchAfter 会在所有派生 Manage 的 Search 成功后执行，默认把请求 Tag 透传回前端。
func (own *ShopManage[T]) OnSearchAfter(operation *managepkg.Search[T], result *view.TableData, _ servertypes.IRequest) (interface{}, error) {
	if operation != nil && operation.SearchItem != nil && result != nil {
		result.Tag = operation.SearchItem.Tag
	}
	return result, nil
}

func validateShopManager(req servertypes.IRequest) (interface{}, error, bool) {
	if req != nil {
		if uid, _ := req.GetUser(); uid != "" {
			return nil, nil, false
		}
	}
	return nil, servertypes.NewPublicError(
		servertypes.ErrorKindUnauthenticated,
		servertypes.PublicCodeUnauthenticated,
		"管理身份无效",
		nil,
	), true
}

func (own *ShopManage[T]) logManageResult(req servertypes.IRequest, phase string, err error) {
	ownerName := "ShopManage"
	if own != nil && own.owner != nil {
		ownerName = utils.GetTypeName(own.owner)
	}
	fields := []logx.LogField{
		logx.Field("owner", ownerName),
		logx.Field("phase", phase),
	}
	if req != nil {
		fields = append(fields,
			logx.Field("service", req.ServiceName()),
			logx.Field("route", req.GetPath()),
			logx.Field("trace_id", req.GetTraceId()),
		)
	}
	if err != nil {
		contract := servertypes.ResolvePublicError(err)
		fields = append(fields, logx.Field("code", contract.Code))
		logx.Infow("shop_manage_operation_failed", fields...)
		return
	}
	logx.Infow("shop_manage_operation_succeeded", fields...)
}

// ViewFieldModel 配置框架公共字段。
func (own *ShopManage[T]) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("ID") || field.IsFieldOrTitle("CreatedAt") || field.IsFieldOrTitle("UpdatedAt") {
		field.IsEdit = false
	}
}

// ViewChildModel 保留服务级子表配置扩展点。
func (own *ShopManage[T]) ViewChildModel(*view.ViewChildModel) {}
