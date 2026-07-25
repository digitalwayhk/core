// Package public 提供 07 用户入口服务供应商 facade API。
package public

import (
	"net/http"
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	supplierapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetSuppliers 是普通用户查询供应商的入口 facade。
type GetSuppliers struct{}

// Parse 绑定供应商 facade 请求。
func (*GetSuppliers) Parse(servertypes.IRequest) error { return nil }

// Validation 校验供应商 facade 请求。
func (*GetSuppliers) Validation(servertypes.IRequest) error { return nil }

// Do 调用供应商权威服务内部 Public API。
func (*GetSuppliers) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&supplierapi.GetSuppliers{})
	if err != nil || !response.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, response.GetError()
	}
	var items []*supplierdto.Supplier
	response.GetData(&items)
	return items, nil
}

// GetResponse 返回供应商列表 DTO 类型。
func (*GetSuppliers) GetResponse() interface{} { return []*supplierdto.Supplier{} }

// RouterInfo 返回用户入口供应商查询路由信息，并在入口 facade 启用缓存。
func (own *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	info := userPublicRoute(own, "getsuppliers", http.MethodPost)
	info.UseCache(30 * time.Second)
	return info
}
