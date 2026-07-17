package supplier

import "github.com/shopspring/decimal"

type ProductSnapshot struct {
	ProductID    uint            `json:"productID"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	ProductCode  string          `json:"productCode"`
	ProductName  string          `json:"productName"`
	UnitPrice    decimal.Decimal `json:"unitPrice"`
}
