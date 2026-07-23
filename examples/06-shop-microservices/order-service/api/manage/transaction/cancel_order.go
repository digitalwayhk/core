// 本文件提供当前服务交易域 Manage API 的查询、状态命令和受控操作能力。
package transaction

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// CancelOrder 定义本文件能力使用的核心结构。
type CancelOrder struct {
	managepkg.Operation[models.Order]
}

// NewCancelOrder 执行本文件能力对应的业务操作。
func NewCancelOrder(owner interface{}) *CancelOrder {
	return &CancelOrder{Operation: managepkg.NewOperation[models.Order](owner)}
}

// New 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) New(instance interface{}) servertypes.IRouter {
	next := NewCancelOrder(nil)
	next.Operation.New(instance)
	return next
}

// Do 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	owner, ok := own.GetInstance().(*OrderManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	return cancelSelectedOrder(own.Model, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
}

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
