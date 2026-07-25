// Package public 提供 07 订单服务支付类型内部 Public API。
package public

import (
	"net/http"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetPaymentTypes 是 user-service 查询支付类型的内部 Public API。
type GetPaymentTypes struct{}

// Parse 绑定支付类型查询请求。
func (*GetPaymentTypes) Parse(servertypes.IRequest) error { return nil }

// Validation 校验支付类型查询请求。
func (*GetPaymentTypes) Validation(servertypes.IRequest) error { return nil }

// Do 从共享远程权威库读取启用支付类型。
func (*GetPaymentTypes) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.ListPaymentTypes(true)
	if err != nil {
		return nil, err
	}
	result := make([]*orderdto.PaymentType, 0, len(items))
	for _, item := range items {
		result = append(result, &orderdto.PaymentType{ID: item.ID, Name: item.Name, Code: item.Code, Enabled: item.Enabled, TraceID: item.TraceID})
	}
	return result, nil
}

// GetResponse 返回支付类型列表响应 DTO 类型。
func (*GetPaymentTypes) GetResponse() interface{} { return []*orderdto.PaymentType{} }

// RouterInfo 返回支付类型内部 Public 路由信息。
func (own *GetPaymentTypes) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "getpaymenttypes", http.MethodPost)
}
