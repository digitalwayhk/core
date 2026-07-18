// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"errors"
	"net/http"
	"strconv"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 定义本文件能力使用的核心结构。
type CancelOrder struct {
	UserID  uint `json:"userID"`
	OrderID uint `json:"orderID"`
}

// Parse 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 {
		return errors.New("用户和订单不能为空")
	}
	return nil
}

// Do 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	return business.CancelOrder(own.UserID, own.OrderID, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "cancelorder", http.MethodPost)
}
