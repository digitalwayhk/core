// Package public 提供 07 供应商服务供应商查询内部 Public API。
package public

import (
	"net/http"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetSuppliers 是 user-service 查询供应商资料的内部 Public API。
type GetSuppliers struct{}

// Parse 绑定供应商查询请求。
func (*GetSuppliers) Parse(servertypes.IRequest) error { return nil }

// Validation 校验供应商查询请求。
func (*GetSuppliers) Validation(servertypes.IRequest) error { return nil }

// Do 返回启用供应商资料 DTO。
func (*GetSuppliers) Do(servertypes.IRequest) (interface{}, error) {
	items, err := business.ListSuppliers(true)
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Supplier, 0, len(items))
	for _, item := range items {
		result = append(result, &supplierdto.Supplier{ID: item.ID, Code: item.Code, Name: item.Name, Description: item.Description, Enabled: item.Enabled, TraceID: item.TraceID})
	}
	return result, nil
}

// GetResponse 返回供应商列表响应 DTO 类型。
func (*GetSuppliers) GetResponse() interface{} { return []*supplierdto.Supplier{} }

// RouterInfo 返回供应商查询内部 Public 路由信息。
func (own *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	return supplierPublicRoute(own, "getsuppliers", http.MethodPost)
}
