// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// SetUserEnabled 定义本文件能力使用的核心结构。
type SetUserEnabled struct {
	managepkg.Operation[models.User]
}

// NewSetUserEnabled 执行本文件能力对应的业务操作。
func NewSetUserEnabled(owner interface{}) *SetUserEnabled {
	return &SetUserEnabled{Operation: managepkg.NewOperation[models.User](owner)}
}

// New 实现本类型在当前服务边界中的行为。
func (own *SetUserEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetUserEnabled(nil)
	next.Operation.New(instance)
	return next
}

// Do 实现本类型在当前服务边界中的行为。
func (own *SetUserEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(*UserManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	if own.Model == nil {
		return nil, contract.ErrResourceNotFound
	}
	current, err := models.FindUserByID(own.Model.ID)
	if err != nil || current == nil {
		return nil, contract.ErrResourceNotFound
	}
	current.TraceID = req.GetTraceId()
	current.Enabled = own.Model.Enabled
	return current, models.SaveUser(current)
}

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *SetUserEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
