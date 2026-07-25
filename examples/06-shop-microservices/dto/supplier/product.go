// 本文件定义 06 微服务示例供应商域对外传递的 DTO 能力。
package supplier

import "github.com/shopspring/decimal"

// Product 定义本文件能力使用的核心结构。
type Product struct {
	ID           uint            `json:"id"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode,omitempty"`
	SupplierName string          `json:"supplierName,omitempty"`
	Name         string          `json:"name"`
	Code         string          `json:"code"`
	Price        decimal.Decimal `json:"price"`
	Enabled      bool            `json:"enabled"`
}
