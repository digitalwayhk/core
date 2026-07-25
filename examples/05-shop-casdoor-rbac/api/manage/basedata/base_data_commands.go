package basedata

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

type baseDataStateOwner[T persistencetypes.IModel] interface {
	SetBaseDataEnabled(id uint, enabled bool) (*T, error)
}

// EnableBaseData 是基础资料继承得到的通用启用命令。
type EnableBaseData[T persistencetypes.IModel] struct {
	managepkg.Operation[T]
}

// NewEnableBaseData 创建绑定最终 owner 的启用命令。
func NewEnableBaseData[T persistencetypes.IModel](owner interface{}) *EnableBaseData[T] {
	return &EnableBaseData[T]{Operation: managepkg.NewOperation[T](owner)}
}

// New 为一次请求创建独立命令实例。
func (own *EnableBaseData[T]) New(instance interface{}) servertypes.IRouter {
	next := &EnableBaseData[T]{Operation: managepkg.NewOperation[T](nil)}
	next.Operation.New(instance)
	return next
}

// Validation 校验命令必须选择有效基础资料。
func (own *EnableBaseData[T]) Validation(servertypes.IRequest) error {
	if own.Model == nil || (*own.Model).GetID() == 0 {
		return models.NewValidationError("请选择要启用的数据")
	}
	return nil
}

// Do 通过最终具体 owner 执行业务启用规则。
func (own *EnableBaseData[T]) Do(servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(baseDataStateOwner[T])
	if !ok {
		return nil, models.NewBusinessError("当前管理服务不支持启用")
	}
	return owner.SetBaseDataEnabled((*own.Model).GetID(), true)
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *EnableBaseData[T]) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

// DisableBaseData 是基础资料继承得到的通用禁用命令。
type DisableBaseData[T persistencetypes.IModel] struct {
	managepkg.Operation[T]
}

// NewDisableBaseData 创建绑定最终 owner 的禁用命令。
func NewDisableBaseData[T persistencetypes.IModel](owner interface{}) *DisableBaseData[T] {
	return &DisableBaseData[T]{Operation: managepkg.NewOperation[T](owner)}
}

// New 为一次请求创建独立命令实例。
func (own *DisableBaseData[T]) New(instance interface{}) servertypes.IRouter {
	next := &DisableBaseData[T]{Operation: managepkg.NewOperation[T](nil)}
	next.Operation.New(instance)
	return next
}

// Validation 校验命令必须选择有效基础资料。
func (own *DisableBaseData[T]) Validation(servertypes.IRequest) error {
	if own.Model == nil || (*own.Model).GetID() == 0 {
		return models.NewValidationError("请选择要禁用的数据")
	}
	return nil
}

// Do 通过最终具体 owner 执行业务禁用规则。
func (own *DisableBaseData[T]) Do(servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(baseDataStateOwner[T])
	if !ok {
		return nil, models.NewBusinessError("当前管理服务不支持禁用")
	}
	return owner.SetBaseDataEnabled((*own.Model).GetID(), false)
}

// RouterInfo 注册 Manage 自定义命令路径。
func (own *DisableBaseData[T]) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
