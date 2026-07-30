// Package common 提供 07 订单服务 Manage API 的公共权限、日志和 Hook 基座。
package common

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/zeromicro/go-zero/core/logx"
)

// ServiceManage 是 order-service 全部 Manage 的服务级基座。
type ServiceManage[T persistencetypes.IModel] struct {
	*managepkg.HookedManageService[T]
	owner interface{}
}

// NewServiceManage 创建绑定最终 owner 的服务级 Manage 基座。
func NewServiceManage[T persistencetypes.IModel](owner interface{}) *ServiceManage[T] {
	return &ServiceManage[T]{HookedManageService: managepkg.NewHookedManageService[T](owner), owner: owner}
}

// GetList 将 order-service 的标准 Manage CRUD 绑定到共享 MySQL 权威库。
// 查询仍由 Core ModelList 执行，以完整保留筛选、排序、分页及关联查询能力。
func (*ServiceManage[T]) GetList() interface{} {
	return entity.NewModelList[T](models.RemoteDataAction())
}

// DoBefore 在所有自定义命令前统一执行管理员权限和父级 Hook。
func (own *ServiceManage[T]) DoBefore(sender interface{}, req servertypes.IRequest) (data interface{}, err error, stop bool) {
	defer func() {
		if err != nil {
			own.logManageResult(req, "before", err)
		}
	}()
	if err := AdminOnly(req); err != nil {
		return nil, err, true
	}
	return own.HookedManageService.DoBefore(sender, req)
}

// DoAfter 在所有自定义命令后统一记录 Manage 生命周期日志。
func (own *ServiceManage[T]) DoAfter(sender interface{}, req servertypes.IRequest) (interface{}, error) {
	data, err := own.HookedManageService.DoAfter(sender, req)
	own.logManageResult(req, "after", err)
	return data, err
}

// SearchBefore 在所有查询前统一执行管理员权限。
func (own *ServiceManage[T]) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	if err := AdminOnly(req); err != nil {
		return nil, err, true
	}
	return own.HookedManageService.SearchBefore(sender, req)
}

// OnSearchAfter 保留查询结果 Tag，方便前端表格继续识别上下文。
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
