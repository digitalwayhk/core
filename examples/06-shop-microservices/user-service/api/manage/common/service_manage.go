package common

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/zeromicro/go-zero/core/logx"
)

type OwnerColumnProvider interface {
	UserOwnerColumn() string
}

type WriteScope struct {
	UserID    uint
	AdminOnly bool
}

type WriteScopeResolver interface {
	ResolveUserWriteScope(sender interface{}, actor Actor) (WriteScope, error, bool)
}

// ServiceManage 是 user-service 全部 Manage 的服务级基座。
// owner 必须传最终具体 Manage，确保 Hook 分派到最末层实现。
type ServiceManage[T persistencetypes.IModel] struct {
	*managepkg.HookedManageService[T]
	owner interface{}
}

func NewServiceManage[T persistencetypes.IModel](owner interface{}) *ServiceManage[T] {
	return &ServiceManage[T]{HookedManageService: managepkg.NewHookedManageService[T](owner), owner: owner}
}

func (own *ServiceManage[T]) DoBefore(sender interface{}, req servertypes.IRequest) (data interface{}, err error, stop bool) {
	defer func() {
		if err != nil {
			own.logManageResult(req, "before", err)
		}
	}()
	if data, err, stop = own.HookedManageService.DoBefore(sender, req); stop || err != nil || data != nil {
		return data, err, stop
	}
	return nil, nil, false
}

func (own *ServiceManage[T]) DoAfter(sender interface{}, req servertypes.IRequest) (data interface{}, err error) {
	defer func() { own.logManageResult(req, "after", err) }()
	return own.HookedManageService.DoAfter(sender, req)
}

func (own *ServiceManage[T]) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	operation, ok := sender.(*managepkg.Search[T])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	if scoped, ok := own.owner.(OwnerColumnProvider); ok {
		return OwnerSearch(operation.SearchItem, req, scoped.UserOwnerColumn())
	}
	return own.HookedManageService.SearchBefore(sender, req)
}

func (own *ServiceManage[T]) OnDoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	resolver, ok := own.owner.(WriteScopeResolver)
	if !ok {
		return nil, nil, false
	}
	actor, err := ActorFrom(req)
	if err != nil {
		return nil, err, true
	}
	scope, err, stop := resolver.ResolveUserWriteScope(sender, actor)
	if stop || err != nil {
		return nil, err, stop
	}
	if scope.AdminOnly && !actor.Admin {
		return nil, contract.ErrForbidden, true
	}
	if scope.UserID != 0 {
		if err := AuthorizeWrite(actor, scope.UserID); err != nil {
			return nil, err, true
		}
	}
	return nil, nil, false
}

func (own *ServiceManage[T]) OnSearchAfter(operation *managepkg.Search[T], result *view.TableData, _ servertypes.IRequest) (interface{}, error) {
	if operation != nil && operation.SearchItem != nil && result != nil {
		result.Tag = operation.SearchItem.Tag
	}
	return result, nil
}

func (own *ServiceManage[T]) logManageResult(req servertypes.IRequest, phase string, err error) {
	ownerName := "ServiceManage"
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
		publicErr := servertypes.ResolvePublicError(err)
		fields = append(fields, logx.Field("code", publicErr.Code))
		logx.Infow("shop_manage_operation_failed", fields...)
		return
	}
	logx.Infow("shop_manage_operation_succeeded", fields...)
}
