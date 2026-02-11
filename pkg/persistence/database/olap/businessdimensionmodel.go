// filepath: /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core/pkg/persistence/database/olap/businessdimensionmodel.go

package olap

import (
	"time"

	"github.com/shopspring/decimal"
)

// BusinessDimensionModel 业务维度统计模型
type BusinessDimensionModel struct {
	// 时间维度（所有粒度的时间戳）
	Time1m  time.Time `json:"time1m"`  // 1分钟粒度时间
	Time10m time.Time `json:"time10m"` // 10分钟粒度时间
	Time30m time.Time `json:"time30m"` // 30分钟粒度时间
	Time1h  time.Time `json:"time1h"`  // 1小时粒度时间
	Time8h  time.Time `json:"time8h"`  // 8小时粒度时间
	Time12h time.Time `json:"time12h"` // 12小时粒度时间
	Time1d  time.Time `json:"time1d"`  // 1天粒度时间
	Time1w  time.Time `json:"time1w"`  // 1周粒度时间
	Time1M  time.Time `json:"time1M"`  // 1月粒度时间
	Time1q  time.Time `json:"time1q"`  // 1季度粒度时间
	Time1y  time.Time `json:"time1y"`  // 1年粒度时间

	// 时间编号（用于聚合分组）
	Num1m  int `json:"num1m"`  // 分钟编号 (0-59)
	Num10m int `json:"num10m"` // 10分钟编号 (0-5)
	Num30m int `json:"num30m"` // 30分钟编号 (0-1)
	Num1h  int `json:"num1h"`  // 小时编号 (0-23)
	Num8h  int `json:"num8h"`  // 8小时编号 (0-2)
	Num12h int `json:"num12h"` // 12小时编号 (0-1)
	Num1d  int `json:"num1d"`  // 天编号 (1-31)
	Num1w  int `json:"num1w"`  // 周编号 (1-53)
	Num1M  int `json:"num1M"`  // 月份编号 (1-12)
	Num1q  int `json:"num1q"`  // 季度编号 (1-4)
	Num1y  int `json:"num1y"`  // 年份编号 (2026...)

	// // 业务维度（动态字段，根据配置而定）
	// UserID   string `json:"userId,omitempty"`   // 用户ID
	// PostType string `json:"postType,omitempty"` // 类型（in/out/both）
	// 可扩展其他业务维度...

	// 统计数据
	RecordCount uint64          `json:"recordCount"` // 记录数
	TotalAmount decimal.Decimal `json:"totalAmount"` // 总金额
	AvgAmount   decimal.Decimal `json:"avgAmount"`   // 平均金额
	MaxAmount   decimal.Decimal `json:"maxAmount"`   // 最大金额
	MinAmount   decimal.Decimal `json:"minAmount"`   // 最小金额

	// USDT价值统计
	TotalUsdtValue decimal.Decimal `json:"totalUsdtValue,omitempty"` // 总USDT价值
	AvgUsdtValue   decimal.Decimal `json:"avgUsdtValue,omitempty"`   // 平均USDT价值
	MaxUsdtValue   decimal.Decimal `json:"maxUsdtValue,omitempty"`   // 最大USDT价值
	MinUsdtValue   decimal.Decimal `json:"minUsdtValue,omitempty"`   // 最小USDT价值

	// 原始数据（用于存储未映射的业务维度字段）
	//RawData map[string]interface{} `json:"rawData,omitempty"`
}

// Parse 解析查询结果到模型
func (model *BusinessDimensionModel) Parse(result map[string]interface{}) {
	// 保存原始数据
	//	model.RawData = result

	// 解析时间戳字段
	model.Time1m = parseTime(result, "time_1m")
	model.Time10m = parseTime(result, "time_10m")
	model.Time30m = parseTime(result, "time_30m")
	model.Time1h = parseTime(result, "time_1h")
	model.Time8h = parseTime(result, "time_8h")
	model.Time12h = parseTime(result, "time_12h")
	model.Time1d = parseTime(result, "time_1d")
	model.Time1w = parseTime(result, "time_1w")
	model.Time1M = parseTime(result, "time_1M")
	model.Time1q = parseTime(result, "time_1q")
	model.Time1y = parseTime(result, "time_1y")

	// 解析时间编号
	model.Num1m = parseInt(result, "num_1m")
	model.Num10m = parseInt(result, "num_10m")
	model.Num30m = parseInt(result, "num_30m")
	model.Num1h = parseInt(result, "num_1h")
	model.Num8h = parseInt(result, "num_8h")
	model.Num12h = parseInt(result, "num_12h")
	model.Num1d = parseInt(result, "num_1d")
	model.Num1w = parseInt(result, "num_1w")
	model.Num1M = parseInt(result, "num_1M")
	model.Num1q = parseInt(result, "num_1q")
	model.Num1y = parseInt(result, "num_1y")

	// 解析业务维度
	// model.UserID = parseString(result, "user_id")
	// model.PostType = parseString(result, "post_type")

	// 解析统计数据
	model.RecordCount = parseUint64(result, "record_count")
	model.TotalAmount = parseDecimal(result, "total_amount")
	model.AvgAmount = parseDecimal(result, "avg_amount")
	model.MaxAmount = parseDecimal(result, "max_amount")
	model.MinAmount = parseDecimal(result, "min_amount")

	// 解析USDT价值（可选字段）
	model.TotalUsdtValue = parseDecimal(result, "total_usdt_value")
	model.AvgUsdtValue = parseDecimal(result, "avg_usdt_value")
	model.MaxUsdtValue = parseDecimal(result, "max_usdt_value")
	model.MinUsdtValue = parseDecimal(result, "min_usdt_value")
}

// ParseBusinessDimensionModels 批量解析查询结果
func ParseBusinessDimensionModels(results []map[string]interface{}) []*BusinessDimensionModel {
	models := make([]*BusinessDimensionModel, 0, len(results))

	for _, result := range results {
		model := &BusinessDimensionModel{}
		model.Parse(result)
		models = append(models, model)
	}

	return models
}

// ==================== 辅助解析函数 ====================

// parseTime 解析时间字段
func parseTime(data map[string]interface{}, key string) time.Time {
	if v, ok := data[key]; ok {
		switch val := v.(type) {
		case time.Time:
			return val
		case string:
			// 尝试解析字符串时间
			if t, err := time.Parse("2006-01-02 15:04:05", val); err == nil {
				return t
			}
			if t, err := time.Parse(time.RFC3339, val); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// parseInt 解析整数字段（支持多种类型）
func parseInt(data map[string]interface{}, key string) int {
	if v, ok := data[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int8:
			return int(val)
		case int16:
			return int(val)
		case int32:
			return int(val)
		case int64:
			return int(val)
		case uint:
			return int(val)
		case uint8:
			return int(val)
		case uint16:
			return int(val)
		case uint32:
			return int(val)
		case uint64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}

// parseUint64 解析 uint64 字段
func parseUint64(data map[string]interface{}, key string) uint64 {
	if v, ok := data[key]; ok {
		switch val := v.(type) {
		case uint64:
			return val
		case int:
			return uint64(val)
		case int64:
			return uint64(val)
		case uint:
			return uint64(val)
		case float64:
			return uint64(val)
		}
	}
	return 0
}

// parseDecimal 解析 Decimal 字段
func parseDecimal(data map[string]interface{}, key string) decimal.Decimal {
	if v, ok := data[key]; ok {
		switch val := v.(type) {
		case decimal.Decimal:
			return val
		case string:
			if d, err := decimal.NewFromString(val); err == nil {
				return d
			}
		case float64:
			return decimal.NewFromFloat(val)
		case int64:
			return decimal.NewFromInt(val)
		case int:
			return decimal.NewFromInt(int64(val))
		}
	}
	return decimal.Zero
}

// ==================== 业务方法 ====================

// GetPrimaryTime 获取主要时间字段（根据最小粒度）
func (model *BusinessDimensionModel) GetPrimaryTime() time.Time {
	if !model.Time1m.IsZero() {
		return model.Time1m
	}
	if !model.Time10m.IsZero() {
		return model.Time10m
	}
	if !model.Time30m.IsZero() {
		return model.Time30m
	}
	if !model.Time1h.IsZero() {
		return model.Time1h
	}
	if !model.Time1d.IsZero() {
		return model.Time1d
	}
	if !model.Time1M.IsZero() {
		return model.Time1M
	}
	return time.Time{}
}

// GetDimensionValue 获取业务维度字段值（动态）
// func (model *BusinessDimensionModel) GetDimensionValue(key string) interface{} {
// 	if model.RawData != nil {
// 		return model.RawData[key]
// 	}
// 	return nil
// }
