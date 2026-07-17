package supplier

import "github.com/shopspring/decimal"

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
