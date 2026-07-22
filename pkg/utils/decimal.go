// 本文件定义兼容 shopspring/decimal 的具名 Decimal 类型。
package utils

import "github.com/shopspring/decimal"

// Decimal 是 decimal.Decimal 的兼容具名类型。
type Decimal decimal.Decimal

// Equals 报告两个 Decimal 是否相等。
func (d Decimal) Equals(other Decimal) bool {
	return decimal.Decimal(d).Equal(decimal.Decimal(other))
}

// Less 报告当前 Decimal 是否小于另一个值。
func (d Decimal) Less(other Decimal) bool {
	return decimal.Decimal(d).LessThan(decimal.Decimal(other))
}
