// Package common 提供 07 供应商服务 Manage API 的服务级 Hook 基座。
package common

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/zeromicro/go-zero/core/logx"
)

// ServiceManage 是 supplier-service 全部 Manage 的服务级基座。
type ServiceManage[T persistencetypes.IModel] struct {
	*managepkg.HookedManageService[T]
	owner interface{}
}

// NewServiceManage 创建绑定最终 owner 的服务级 Manage 基座。
func NewServiceManage[T persistencetypes.IModel](owner interface{}) *ServiceManage[T] {
	return &ServiceManage[T]{HookedManageService: managepkg.NewHookedManageService[T](owner), owner: owner}
}

// DoAfter 在所有 Manage 命令后统一记录生命周期日志。
func (own *ServiceManage[T]) DoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error) {
	data, err := own.HookedManageService.DoAfter(sender, req)
	own.logManageResult(req, "after", err)
	return data, err
}

// OnSearchAfter 保留查询结果 Tag，方便前端表格继续识别上下文。
func (own *ServiceManage[T]) OnSearchAfter(operation *managepkg.Search[T], result *view.TableData, _ servertypes.IRequest) (interface{}, error) {
	if operation != nil && operation.SearchItem != nil && result != nil {
		result.Tag = operation.SearchItem.Tag
	}
	return result, nil
}

// logManageResult 按统一事件名记录 Manage 生命周期，不输出请求体或业务对象。
func (own *ServiceManage[T]) logManageResult(req servertypes.IRequest, phase string, err error) {
	ownerName := "ServiceManage"
	if own != nil && own.owner != nil {
		ownerName = utils.GetTypeName(own.owner)
	}
	fields := []logx.LogField{logx.Field("owner", ownerName), logx.Field("phase", phase)}
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
