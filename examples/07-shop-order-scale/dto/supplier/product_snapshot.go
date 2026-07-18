// Package supplier 定义 07 订单水平扩展示例供应商域对外传递的 DTO。
package supplier

import "github.com/shopspring/decimal"

// ProductSnapshot 定义订单创建时固化的商品和供应商快照。
type ProductSnapshot struct {
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	ProductID    uint            `json:"productID"`
	ProductCode  string          `json:"productCode"`
	ProductName  string          `json:"productName"`
	UnitPrice    decimal.Decimal `json:"unitPrice"`
	Enabled      bool            `json:"enabled"`
	TraceID      string          `json:"traceID,omitempty"`
}
