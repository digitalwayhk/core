// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
package public

import (
	"errors"
	"net/http"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetOrders 定义本文件能力使用的核心结构。
type GetOrders struct {
	UserID uint `json:"userID"`
}

// Parse 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Validation(servertypes.IRequest) error {
	if own.UserID == 0 {
		return errors.New("用户不能为空")
	}
	return nil
}

// Do 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Do(servertypes.IRequest) (interface{}, error) {
	return business.UserOrders(own.UserID)
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "getorders", http.MethodPost)
}
