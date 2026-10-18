// 本文件独立声明与 07 既有表兼容的业务字段，不引用 Core 模型。
package reference

import (
	"github.com/shopspring/decimal"
)

// Product 是原生对照服务直接交给 GORM 的记录。
type Product struct {
	Record
	SupplierID uint            `gorm:"not null;index" json:"supplierID"`
	Code       string          `gorm:"not null;uniqueIndex" json:"code"`
	Name       string          `gorm:"not null" json:"name"`
	Price      decimal.Decimal `json:"price"`
	Enabled    bool            `gorm:"index" json:"enabled"`
}
