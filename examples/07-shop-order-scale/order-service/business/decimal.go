// Package business 提供 07 订单服务业务层 decimal 辅助能力。
package business

import "github.com/shopspring/decimal"

func modelsDecimalFromInt(value int) decimal.Decimal {
	return decimal.NewFromInt(int64(value))
}
