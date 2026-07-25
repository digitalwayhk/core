// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// SetProductEnabled 是商品上下架的唯一写入口。
type SetProductEnabled struct {
	managepkg.Operation[models.Product]
}

// NewSetProductEnabled 执行本文件能力对应的业务操作。
func NewSetProductEnabled(owner interface{}) *SetProductEnabled {
	return &SetProductEnabled{Operation: managepkg.NewOperation[models.Product](owner)}
}

// New 实现本类型在当前服务边界中的行为。
func (own *SetProductEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetProductEnabled(nil)
	next.Operation.New(instance)
	return next
}

// Validation 实现本类型在当前服务边界中的行为。
func (own *SetProductEnabled) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return contract.ErrResourceNotFound
	}
	return nil
}

// Do 实现本类型在当前服务边界中的行为。
func (own *SetProductEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(*ProductManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return business.SetProductEnabled(own.Model.ID, own.Model.Enabled, req.GetTraceId(), models.EventID(req.NewID()))
}

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *SetProductEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
