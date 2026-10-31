// 本文件独立声明与 07 既有表兼容的业务字段，不引用 Core 模型。
package reference

import (
	"github.com/shopspring/decimal"
)

// OrderRule 是原生对照服务直接交给 GORM 的记录。
type OrderRule struct {
	Record
	RuleCode       string          `gorm:"type:varchar(191);not null;uniqueIndex" json:"ruleCode"`
	RuleName       string          `json:"ruleName"`
	MinQuantity    int             `json:"minQuantity"`
	MaxQuantity    int             `json:"maxQuantity"`
	MaxOrderAmount decimal.Decimal `json:"maxOrderAmount"`
	Enabled        bool            `gorm:"index" json:"enabled"`
	RuleRevision   int             `json:"ruleRevision"`
}
