// Package supplier 保存 Supplier Service 的稳定传输结构。
package supplier

import "github.com/shopspring/decimal"

type Supplier struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type Product struct {
	ID           uint            `json:"id"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	Name         string          `json:"name"`
	Code         string          `json:"code"`
	Price        decimal.Decimal `json:"price"`
	Enabled      bool            `json:"enabled"`
}

// ProductSnapshot 是 Order Service 下单时保存的商品事实。
type ProductSnapshot struct {
	ProductID    uint            `json:"productID"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	ProductCode  string          `json:"productCode"`
	ProductName  string          `json:"productName"`
	UnitPrice    decimal.Decimal `json:"unitPrice"`
}
