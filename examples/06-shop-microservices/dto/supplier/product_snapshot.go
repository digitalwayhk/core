// 本文件定义 06 微服务示例供应商域对外传递的 DTO 能力。
package supplier

import "github.com/shopspring/decimal"

// ProductSnapshot 定义本文件能力使用的核心结构。
type ProductSnapshot struct {
	ProductID    uint            `json:"productID"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	ProductCode  string          `json:"productCode"`
	ProductName  string          `json:"productName"`
	UnitPrice    decimal.Decimal `json:"unitPrice"`
}
