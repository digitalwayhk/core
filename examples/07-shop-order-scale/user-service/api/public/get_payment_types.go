// Package public 提供 07 用户入口服务支付类型 facade API。
package public

import (
	"net/http"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetPaymentTypes 是普通用户查询支付类型的入口 facade。
type GetPaymentTypes struct{}

// Parse 绑定支付类型 facade 请求。
func (*GetPaymentTypes) Parse(servertypes.IRequest) error { return nil }

// Validation 校验支付类型 facade 请求。
func (*GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }

// Do 调用订单权威服务内部 Public API。
func (*GetPaymentTypes) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&orderapi.GetPaymentTypes{})
	if err != nil || !response.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, response.GetError()
	}
	var items []*orderdto.PaymentType
	response.GetData(&items)
	return items, nil
}

// GetResponse 返回支付类型列表 DTO 类型。
func (*GetPaymentTypes) GetResponse() interface{} { return []*orderdto.PaymentType{} }

// RouterInfo 返回用户入口支付类型查询路由信息，并在入口 facade 启用缓存。
func (own *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	info := userPublicRoute(own, "getpaymenttypes", http.MethodPost)
	info.UseCache(30 * time.Second)
	return info
}
