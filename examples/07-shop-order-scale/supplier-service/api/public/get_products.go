// Package public 提供 07 供应商服务商品查询内部 Public API。
package public

import (
	"net/http"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 是 user/order 服务查询商品资料的内部 Public API。
type GetProducts struct {
	ID uint `json:"id"`
}

// Parse 绑定商品查询请求。
func (own *GetProducts) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验商品查询请求。
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }

// Do 返回启用商品资料 DTO。
func (own *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.ListProducts(own.ID, true)
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Product, 0, len(items))
	for _, item := range items {
		result = append(result, &supplierdto.Product{ID: item.ID, SupplierID: item.SupplierID, Code: item.Code, Name: item.Name, Price: item.Price, Enabled: item.Enabled, TraceID: item.TraceID})
	}
	return result, nil
}

// GetResponse 返回商品列表响应 DTO 类型。
func (*GetProducts) GetResponse() interface{} { return []*supplierdto.Product{} }

// RouterInfo 返回商品查询内部 Public 路由信息。
func (own *GetProducts) RouterInfo() *servertypes.RouterInfo {
	return supplierPublicRoute(own, "getproducts", http.MethodPost)
}
