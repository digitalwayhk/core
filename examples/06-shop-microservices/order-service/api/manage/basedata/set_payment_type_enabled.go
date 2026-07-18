// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"strconv"
)

// SetPaymentTypeEnabled 定义本文件能力使用的核心结构。
type SetPaymentTypeEnabled struct {
	managepkg.Operation[models.PaymentType]
}

// NewSetPaymentTypeEnabled 执行本文件能力对应的业务操作。
func NewSetPaymentTypeEnabled(owner interface{}) *SetPaymentTypeEnabled {
	return &SetPaymentTypeEnabled{Operation: managepkg.NewOperation[models.PaymentType](owner)}
}

// New 实现本类型在当前服务边界中的行为。
func (own *SetPaymentTypeEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetPaymentTypeEnabled(nil)
	next.Operation.New(instance)
	return next
}

// Validation 实现本类型在当前服务边界中的行为。
func (own *SetPaymentTypeEnabled) Validation(servertypes.IRequest) error {
	if own.Model == nil || own.Model.ID == 0 {
		return contract.ErrResourceNotFound
	}
	return nil
}

// Do 实现本类型在当前服务边界中的行为。
func (own *SetPaymentTypeEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(*PaymentTypeManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return business.SetPaymentTypeEnabled(own.Model.ID, own.Model.Enabled, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
}

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *SetPaymentTypeEnabled) RouterInfo() *servertypes.RouterInfo {
	return managepkg.RouterInfo(own)
}
