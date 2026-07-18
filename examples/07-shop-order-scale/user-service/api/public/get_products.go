// Package public 提供 07 用户入口服务商品 facade API。
package public

import (
	"net/http"
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	supplierapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 是普通用户查询商品的入口 facade。
type GetProducts struct {
	ID uint `json:"id"`
}

// Parse 绑定商品 facade 请求。
func (own *GetProducts) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验商品 facade 请求。
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }

// Do 调用供应商权威服务内部 Public API。
func (own *GetProducts) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&supplierapi.GetProducts{ID: own.ID})
	if err != nil || !response.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, response.GetError()
	}
	var items []*supplierdto.Product
	response.GetData(&items)
	return items, nil
}

// GetResponse 返回商品列表 DTO 类型。
func (*GetProducts) GetResponse() interface{} { return []*supplierdto.Product{} }

// RouterInfo 返回用户入口商品查询路由信息，并在入口 facade 启用缓存。
func (own *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := userPublicRoute(own, "getproducts", http.MethodPost)
	info.UseCache(30 * time.Second)
	return info
}
