// Package supplier 定义 07 订单水平扩展示例商品 DTO。
package supplier

import "github.com/shopspring/decimal"

// Product 定义跨服务返回的商品资料快照。
type Product struct {
	ID           uint            `json:"id"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Price        decimal.Decimal `json:"price"`
	Enabled      bool            `json:"enabled"`
	TraceID      string          `json:"traceID"`
}
