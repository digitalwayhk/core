package olap

import (
	"fmt"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/json"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// 业务维度配置
type BusinessDimensionConfig struct {
	ViewName        string    `gorm:"primaryKey;column:view_name" json:"view_name"`            // 视图名称
	SourceTableName string    `gorm:"column:source_table_name;index" json:"source_table_name"` // 源表名
	Dimensions      string    `gorm:"column:dimensions;type:text" json:"dimensions"`           // 业务维度字段 JSON 数组，如 ["user_id", "symbol"]
	TimeGranularity string    `gorm:"column:time_granularity" json:"time_granularity"`         // 时间粒度: "minute", "hour", "day", "month"
	NumericFields   string    `gorm:"column:numeric_fields;type:text" json:"numeric_fields"`   // 要聚合的数值字段 JSON 数组
	DecimalFields   string    `gorm:"column:decimal_fields;type:text" json:"decimal_fields"`   // 🆕 Decimal 字段 JSON 数组
	Filters         string    `gorm:"column:filters" json:"filters"`                           // WHERE 条件，如 "status = 'completed'"
	PartitionBy     string    `gorm:"column:partition_by" json:"partition_by"`                 // 自定义分区（为空则使用默认）
	TTLDays         int       `gorm:"column:ttl_days" json:"ttl_days"`                         // 数据保留天数（0表示永久保留）
	TimeField       string    `gorm:"column:time_field" json:"time_field"`                     // 时间字段名（stat_time, stat_date等）
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	Description     string    `gorm:"column:description" json:"description"` // 视图描述
}

func (BusinessDimensionConfig) TableName() string {
	return "business_dimension_configs"
}

// 获取维度数组
func (c *BusinessDimensionConfig) GetDimensions() []string {
	var dims []string
	if err := json.Unmarshal([]byte(c.Dimensions), &dims); err != nil {
		logx.Errorf("解析维度字段失败: %v", err)
		return []string{}
	}
	return dims
}

// 获取数值字段数组
func (c *BusinessDimensionConfig) GetNumericFields() []string {
	var fields []string
	if err := json.Unmarshal([]byte(c.NumericFields), &fields); err != nil {
		logx.Errorf("解析数值字段失败: %v", err)
		return []string{}
	}
	return fields
}

// 🆕 获取 Decimal 字段数组
func (c *BusinessDimensionConfig) GetDecimalFields() []string {
	if c.DecimalFields == "" {
		return []string{}
	}
	var fields []string
	if err := json.Unmarshal([]byte(c.DecimalFields), &fields); err != nil {
		logx.Errorf("解析 Decimal 字段失败: %v", err)
		return []string{}
	}
	return fields
}

// 获取 TTL
func (c *BusinessDimensionConfig) GetTTL() time.Duration {
	if c.TTLDays <= 0 {
		return 0
	}
	return time.Duration(c.TTLDays) * 24 * time.Hour
}

// 设置配置数据库（用于存储配置信息）
func (ch *ClickHouse) SetConfigDB(configDB *gorm.DB) {
	ch.configDB = configDB
}

// 获取配置数据库连接
func (ch *ClickHouse) getConfigDB() *gorm.DB {
	if ch.configDB != nil {
		return ch.configDB
	}
	// 如果没有设置配置数据库，使用 ClickHouse 连接（不推荐）
	logx.Errorf("⚠️ 未设置配置数据库，使用 ClickHouse 连接存储配置（不推荐）")
	return ch.db
}

// 🆕 初始化配置表
func (ch *ClickHouse) InitConfigTable() error {
	configDB := ch.getConfigDB()

	if err := configDB.AutoMigrate(&BusinessDimensionConfig{}); err != nil {
		return fmt.Errorf("创建配置表失败: %w", err)
	}

	logx.Info("✅ 初始化业务视图配置表成功")
	return nil
}

// 🆕 保存业务视图配置
func (ch *ClickHouse) SaveBusinessViewConfig(config *BusinessDimensionConfig) error {
	// 验证配置
	if config.ViewName == "" {
		return fmt.Errorf("视图名称不能为空")
	}

	if config.SourceTableName == "" {
		return fmt.Errorf("源表名不能为空")
	}

	dims := config.GetDimensions()
	if len(dims) == 0 {
		return fmt.Errorf("必须指定至少一个业务维度")
	}

	// 验证 JSON 格式
	if !isValidJSON(config.Dimensions) {
		return fmt.Errorf("维度字段格式错误，必须是 JSON 数组")
	}

	if config.NumericFields != "" && !isValidJSON(config.NumericFields) {
		return fmt.Errorf("数值字段格式错误，必须是 JSON 数组")
	}

	// 🆕 验证 Decimal 字段格式
	if config.DecimalFields != "" && !isValidJSON(config.DecimalFields) {
		return fmt.Errorf("Decimal 字段格式错误，必须是 JSON 数组")
	}

	// 设置默认值
	if config.TimeGranularity == "" {
		config.TimeGranularity = "hour"
	}

	// 获取时间字段
	_, timeField := ch.getTimeFunctionAndField(config.TimeGranularity)
	if timeField == "" {
		return fmt.Errorf("不支持的时间粒度: %s", config.TimeGranularity)
	}
	config.TimeField = timeField

	// 保存到配置数据库
	configDB := ch.getConfigDB()
	if err := configDB.Save(config).Error; err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	logx.Infof("✅ 保存业务视图配置成功 [%s]", config.ViewName)
	return nil
}

// 🆕 创建业务维度统计视图（从配置）- 支持 Decimal
func (ch *ClickHouse) CreateBusinessViewFromConfig(config *BusinessDimensionConfig) error {
	dims := config.GetDimensions()
	if len(dims) == 0 {
		return fmt.Errorf("必须指定至少一个业务维度")
	}

	// 获取时间函数和字段名
	timeFunc, timeField := ch.getTimeFunctionAndField(config.TimeGranularity)
	if timeFunc == "" {
		return fmt.Errorf("不支持的时间粒度: %s", config.TimeGranularity)
	}

	// 获取数值字段和 Decimal 字段
	numericFields := config.GetNumericFields()
	decimalFields := config.GetDecimalFields()

	if len(numericFields) == 0 && len(decimalFields) == 0 {
		logx.Errorf("⚠️ 未指定数值字段和 Decimal 字段，视图将只包含 record_count")
	}

	// 生成聚合字段

	aggregations := ch.getAggregations(numericFields, decimalFields)

	// 构建 GROUP BY
	groupByFields := []string{timeField}
	groupByFields = append(groupByFields, dims...)

	// 构建分区策略
	partitionBy := config.PartitionBy
	if partitionBy == "" {
		switch config.TimeGranularity {
		case "minute":
			partitionBy = fmt.Sprintf("toYYYYMMDD(%s)", timeField)
		case "hour":
			partitionBy = fmt.Sprintf("toYYYYMM(%s)", timeField)
		case "day":
			partitionBy = fmt.Sprintf("toYYYYMM(%s)", timeField)
		case "month":
			partitionBy = fmt.Sprintf("toYear(%s)", timeField)
		}
	}

	// 构建 ORDER BY
	orderBy := append([]string{timeField}, dims...)

	// 构建 WHERE 条件
	whereClause := ""
	if config.Filters != "" {
		whereClause = fmt.Sprintf("\nWHERE %s", config.Filters)
	}

	// 构建 TTL
	ttlClause := ""
	if config.TTLDays > 0 {
		ttlClause = fmt.Sprintf("\nTTL %s + INTERVAL %d DAY", timeField, config.TTLDays)
	}

	// 构建 SELECT 字段
	selectFields := []string{
		fmt.Sprintf("%s as %s", timeFunc, timeField),
	}
	selectFields = append(selectFields, dims...)
	selectFields = append(selectFields, aggregations...)

	// 生成 SQL
	sql := fmt.Sprintf(`
CREATE MATERIALIZED VIEW IF NOT EXISTS %s
ENGINE = SummingMergeTree()
PARTITION BY %s
ORDER BY (%s)%s
POPULATE
AS SELECT
    %s
FROM %s%s
GROUP BY %s`,
		config.ViewName,
		partitionBy,
		strings.Join(orderBy, ", "),
		ttlClause,
		strings.Join(selectFields, ",\n    "),
		config.SourceTableName,
		whereClause,
		strings.Join(groupByFields, ", "),
	)

	// 执行创建视图
	if err := ch.db.Exec(sql).Error; err != nil {
		logx.Errorf("SQL 执行失败:\n%s", sql)
		return fmt.Errorf("创建业务视图失败 [%s]: %w", config.ViewName, err)
	}

	logx.Infof("✅ 创建业务统计视图成功 [%s]", config.ViewName)

	// 🆕 记录字段信息
	if len(numericFields) > 0 {
		logx.Infof("   - 数值字段: %v", numericFields)
	}
	if len(decimalFields) > 0 {
		logx.Infof("   - Decimal 字段: %v (精度保持)", decimalFields)
	}

	return nil
}

// 🆕 通过视图名称创建业务视图
func (ch *ClickHouse) CreateBusinessViewByName(viewName string) error {
	var config BusinessDimensionConfig
	configDB := ch.getConfigDB()

	if err := configDB.Where("view_name = ?", viewName).First(&config).Error; err != nil {
		return fmt.Errorf("查询配置失败: %w", err)
	}

	return ch.CreateBusinessViewFromConfig(&config)
}

// 🆕 获取业务视图配置
func (ch *ClickHouse) GetBusinessViewConfig(viewName string) (*BusinessDimensionConfig, error) {
	var config BusinessDimensionConfig
	configDB := ch.getConfigDB()

	if err := configDB.Where("view_name = ?", viewName).First(&config).Error; err != nil {
		return nil, fmt.Errorf("配置不存在: %w", err)
	}
	return &config, nil
}

// 🆕 查询业务统计数据（通过视图名称和参数）
func (ch *ClickHouse) QueryBusinessStatsByName(
	viewName string,
	dimensions map[string]interface{}, // 维度过滤条件
	startTime, endTime time.Time,
) ([]map[string]interface{}, error) {
	// 获取配置
	config, err := ch.GetBusinessViewConfig(viewName)
	if err != nil {
		return nil, err
	}

	// 查询数据（使用 ClickHouse 连接）
	query := ch.db.Table(viewName).
		Where(fmt.Sprintf("%s BETWEEN ? AND ?", config.TimeField), startTime, endTime)

	// 添加维度过滤
	for dim, value := range dimensions {
		query = query.Where(fmt.Sprintf("%s = ?", dim), value)
	}

	var results []map[string]interface{}
	err = query.Order(fmt.Sprintf("%s DESC", config.TimeField)).Find(&results).Error

	// 🆕 自动转换 Decimal 字段 (字符串 -> decimal.Decimal)
	// 注意: 这里返回的是 map,调用方需要根据配置的 DecimalFields 进行类型转换
	// 示例:
	// decimalFields := config.GetDecimalFields()
	// for _, result := range results {
	//     for _, field := range decimalFields {
	//         if strVal, ok := result["total_"+field].(string); ok {
	//             result["total_"+field], _ = decimal.NewFromString(strVal)
	//         }
	//     }
	// }

	return results, err
}

// 🆕 列出所有业务视图配置
func (ch *ClickHouse) ListBusinessViewConfigs(sourceTableName string) ([]*BusinessDimensionConfig, error) {
	var configs []*BusinessDimensionConfig
	configDB := ch.getConfigDB()
	query := configDB.Model(&BusinessDimensionConfig{})

	if sourceTableName != "" {
		query = query.Where("source_table_name = ?", sourceTableName)
	}

	if err := query.Order("created_at DESC").Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// 🆕 删除业务视图配置
func (ch *ClickHouse) DeleteBusinessViewConfig(viewName string) error {
	// 先删除物化视图（ClickHouse）
	dropSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s", viewName)
	if err := ch.db.Exec(dropSQL).Error; err != nil {
		logx.Errorf("删除视图失败 [%s]: %v", viewName, err)
		// 继续删除配置，即使视图删除失败
	}

	// 删除配置（配置数据库）
	configDB := ch.getConfigDB()
	if err := configDB.Where("view_name = ?", viewName).Delete(&BusinessDimensionConfig{}).Error; err != nil {
		return fmt.Errorf("删除配置失败: %w", err)
	}

	logx.Infof("✅ 删除业务视图配置成功 [%s]", viewName)
	return nil
}

// 🆕 更新业务视图配置
func (ch *ClickHouse) UpdateBusinessViewConfig(config *BusinessDimensionConfig) error {
	// 先删除旧视图
	dropSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s", config.ViewName)
	if err := ch.db.Exec(dropSQL).Error; err != nil {
		logx.Errorf("删除旧视图失败 [%s]: %v", config.ViewName, err)
	}

	// 更新配置
	config.UpdatedAt = time.Now()
	configDB := ch.getConfigDB()
	if err := configDB.Save(config).Error; err != nil {
		return fmt.Errorf("更新配置失败: %w", err)
	}

	// 重新创建视图
	return ch.CreateBusinessViewFromConfig(config)
}

// 🆕 批量创建业务视图（从配置列表）
func (ch *ClickHouse) CreateBusinessViewsFromConfigs(configs []*BusinessDimensionConfig) error {
	for _, config := range configs {
		// 保存配置
		if err := ch.SaveBusinessViewConfig(config); err != nil {
			logx.Errorf("保存配置失败 [%s]: %v", config.ViewName, err)
			return err
		}

		// 创建视图
		if err := ch.CreateBusinessViewFromConfig(config); err != nil {
			logx.Errorf("创建视图失败 [%s]: %v", config.ViewName, err)
			return err
		}
	}
	return nil
}

// 🆕 从模型自动创建业务视图配置
func (ch *ClickHouse) CreateBusinessViewConfigFromModel(
	viewName string,
	model interface{},
	dimensions []string,
	granularity string,
	filters string,
) (*BusinessDimensionConfig, error) {
	tableName := ch.getTableName(model)

	// 自动识别数值字段和 Decimal 字段
	numericFields := ch.getNumericFields(model)
	decimalFields := ch.getDecimalFields(model)

	// 转换为 JSON
	dimensionsJSON, _ := json.Marshal(dimensions)
	numericFieldsJSON, _ := json.Marshal(numericFields)
	decimalFieldsJSON, _ := json.Marshal(decimalFields)

	config := &BusinessDimensionConfig{
		ViewName:        viewName,
		SourceTableName: tableName,
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: granularity,
		NumericFields:   string(numericFieldsJSON),
		DecimalFields:   string(decimalFieldsJSON), // 🆕
		Filters:         filters,
		Description:     fmt.Sprintf("自动生成的业务视图: %s", viewName),
	}

	// 保存并创建
	if err := ch.SaveBusinessViewConfig(config); err != nil {
		return nil, err
	}

	if err := ch.CreateBusinessViewFromConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

// 辅助方法：获取时间函数和字段名
func (ch *ClickHouse) getTimeFunctionAndField(granularity string) (string, string) {
	switch granularity {
	case "minute":
		return "toStartOfMinute(created_at)", "stat_time"
	case "hour", "hourly":
		return "toStartOfHour(created_at)", "stat_time"
	case "day", "daily":
		return "toDate(created_at)", "stat_date"
	case "month", "monthly":
		return "toStartOfMonth(created_at)", "stat_month"
	default:
		return "", ""
	}
}

// 🆕 验证 JSON 格式
func isValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}
