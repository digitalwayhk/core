package olap

import (
	"fmt"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/json"
	"github.com/shopspring/decimal"

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
	DecimalFields   string    `gorm:"column:decimal_fields;type:text" json:"decimal_fields"`   //  Decimal 字段 JSON 数组
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
	if c.Dimensions == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(c.Dimensions), &dims); err != nil {
		logx.Errorf("解析维度字段失败: %v", err)
		return []string{}
	}
	return dims
}

// 获取数值字段数组
func (c *BusinessDimensionConfig) GetNumericFields() []string {
	var fields []string
	if c.NumericFields == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(c.NumericFields), &fields); err != nil {
		logx.Errorf("解析数值字段失败: %v", err)
		return []string{}
	}
	return fields
}

// 获取 Decimal 字段数组
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
	logx.Errorf(" 未设置配置数据库，使用 ClickHouse 连接存储配置（不推荐）")
	return ch.db
}

// 初始化配置表
func (ch *ClickHouse) InitConfigTable() error {
	configDB := ch.getConfigDB()

	if err := configDB.AutoMigrate(&BusinessDimensionConfig{}); err != nil {
		return fmt.Errorf("创建配置表失败: %w", err)
	}

	logx.Info(" 初始化业务视图配置表成功")
	return nil
}

// 保存业务视图配置
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

	//  验证 Decimal 字段格式
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

	logx.Infof(" 保存业务视图配置成功 [%s]", config.ViewName)
	return nil
}

// 创建业务维度统计视图（从配置）- 支持 Decimal
func (ch *ClickHouse) CreateBusinessViewFromConfig(config *BusinessDimensionConfig) error {
	dims := config.GetDimensions()
	if len(dims) == 0 {
		return fmt.Errorf("必须指定至少一个业务维度")
	}

	// 获取数值字段和 Decimal 字段
	numericFields := config.GetNumericFields()
	decimalFields := config.GetDecimalFields()

	if len(numericFields) == 0 && len(decimalFields) == 0 {
		logx.Errorf(" 未指定数值字段和 Decimal 字段，视图将只包含 record_count")
	}

	//  关键修复: 获取时间函数和字段（用于聚合）
	timeFunc, timeField := ch.getTimeFunctionAndField(config.TimeGranularity)
	if timeFunc == "" {
		return fmt.Errorf("不支持的时间粒度: %s", config.TimeGranularity)
	}

	// 保存时间字段到配置
	config.TimeField = timeField

	//  获取所有时间维度级别配置
	timeAggLevels := ch.getTimeAggregationLevels(config.TimeGranularity, timeField)

	// 构建 SELECT 字段（包含所有时间维度）
	var selectFields []string

	// 添加主时间字段
	selectFields = append(selectFields, fmt.Sprintf("%s as %s", timeFunc, timeField))

	//  添加所有时间维度列（从 created_at 计算，因为是从原始表创建）
	for _, level := range timeAggLevels {
		//  替换 timeField 为 created_at（因为从原始表创建）
		timeFuncActual := strings.ReplaceAll(level.TimeFunc, timeField, "created_at")
		numberFuncActual := strings.ReplaceAll(level.NumberFunc, timeField, "created_at")

		selectFields = append(selectFields,
			fmt.Sprintf("%s as %s", timeFuncActual, level.TimestampName),
			fmt.Sprintf("%s as %s", numberFuncActual, level.NumberName),
		)
	}

	// 添加业务维度
	selectFields = append(selectFields, dims...)

	// 添加聚合字段
	selectFields = append(selectFields, "count() as record_count")

	// Decimal 字段
	for _, field := range decimalFields {
		selectFields = append(selectFields,
			fmt.Sprintf("sum(%s) as total_%s", field, field),
			fmt.Sprintf("avg(%s) as avg_%s", field, field),
			fmt.Sprintf("max(%s) as max_%s", field, field),
			fmt.Sprintf("min(%s) as min_%s", field, field),
		)
	}

	// 数值字段
	for _, field := range numericFields {
		selectFields = append(selectFields,
			fmt.Sprintf("sum(%s) as total_%s", field, field),
			fmt.Sprintf("avg(%s) as avg_%s", field, field),
			fmt.Sprintf("max(%s) as max_%s", field, field),
			fmt.Sprintf("min(%s) as min_%s", field, field),
		)
	}

	//  修复: 构建 GROUP BY - 必须包含所有时间函数表达式
	groupByFields := []string{timeFunc} // 主时间函数

	//  添加所有时间维度列的函数表达式到 GROUP BY
	for _, level := range timeAggLevels {
		timeFuncActual := strings.ReplaceAll(level.TimeFunc, timeField, "created_at")
		numberFuncActual := strings.ReplaceAll(level.NumberFunc, timeField, "created_at")

		groupByFields = append(groupByFields, timeFuncActual, numberFuncActual)
	}

	groupByFields = append(groupByFields, dims...) // 最后加业务维度

	// 构建分区策略
	partitionBy := config.PartitionBy
	if partitionBy == "" {
		switch config.TimeGranularity {
		case "minute":
			partitionBy = fmt.Sprintf("toYYYYMMDD(%s)", timeField)
		case "hour", "hourly":
			partitionBy = fmt.Sprintf("toYYYYMM(%s)", timeField)
		case "day", "daily":
			partitionBy = fmt.Sprintf("toYYYYMM(%s)", timeField)
		case "month", "monthly":
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
		config.SourceTableName, //  直接使用原始表名
		whereClause,
		strings.Join(groupByFields, ", "),
	)

	// 执行创建视图
	if err := ch.db.Exec(sql).Error; err != nil {
		logx.Errorf("SQL 执行失败:\n%s", sql)
		return fmt.Errorf("创建业务视图失败 [%s]: %w", config.ViewName, err)
	}

	logx.Infof(" 创建业务统计视图成功 [%s]（基于原始表 %s，包含 %d 个时间维度）",
		config.ViewName, config.SourceTableName, len(timeAggLevels))

	return nil
}

// 通过视图名称创建业务视图
func (ch *ClickHouse) CreateBusinessViewByName(viewName string) error {
	var config BusinessDimensionConfig
	configDB := ch.getConfigDB()

	if err := configDB.Where("view_name = ?", viewName).First(&config).Error; err != nil {
		return fmt.Errorf("查询配置失败: %w", err)
	}

	return ch.CreateBusinessViewFromConfig(&config)
}

// 获取业务视图配置
func (ch *ClickHouse) GetBusinessViewConfig(viewName string) (*BusinessDimensionConfig, error) {
	var config BusinessDimensionConfig
	configDB := ch.getConfigDB()

	if err := configDB.Where("view_name = ?", viewName).First(&config).Error; err != nil {
		return nil, fmt.Errorf("配置不存在: %w", err)
	}
	return &config, nil
}

// 列出所有业务视图配置
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

// 删除业务视图配置
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

	logx.Infof(" 删除业务视图配置成功 [%s]", viewName)
	return nil
}

// 更新业务视图配置
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

// 批量创建业务视图（从配置列表）
func (ch *ClickHouse) CreateBusinessViewsFromConfigs(configs []*BusinessDimensionConfig) error {
	for _, config := range configs {
		config.CreatedAt = time.Now()
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

// 从模型自动创建业务视图配置
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
		DecimalFields:   string(decimalFieldsJSON), //
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

// 验证 JSON 格式
func isValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

// 时间汇总粒度配置（包含时间戳和编号）
type TimeAggregationLevel struct {
	Granularity   string //  粒度标识: "1m", "10m", "1h" 等
	TimestampName string // 时间戳列名: time_1m, time_10m 等
	TimeFunc      string // ClickHouse 时间函数
	NumberName    string // 时间编号列名: num_10m, num_1h 等
	NumberFunc    string // ClickHouse 编号函数
}

// 获取向上汇总的时间列配置（根据视图基础粒度）
func (ch *ClickHouse) getTimeAggregationLevels(baseGranularity string, timeField string) []TimeAggregationLevel {
	levels := []TimeAggregationLevel{}

	switch baseGranularity {
	case "minute":
		// 分钟级视图：保留原始分钟 + 向上汇总
		levels = []TimeAggregationLevel{
			{
				Granularity:   "1m",
				TimestampName: "time_1m",
				TimeFunc:      fmt.Sprintf("toStartOfMinute(%s)", timeField),
				NumberName:    "num_1m",
				NumberFunc:    fmt.Sprintf("toMinute(%s)", timeField), // 0-59
			},
			{
				Granularity:   "10m",
				TimestampName: "time_10m",
				TimeFunc:      fmt.Sprintf("toStartOfInterval(%s, INTERVAL 10 MINUTE)", timeField),
				NumberName:    "num_10m",
				NumberFunc:    fmt.Sprintf("toInt32(toMinute(%s) / 10)", timeField), // 0-5
			},
			{
				Granularity:   "30m",
				TimestampName: "time_30m",
				TimeFunc:      fmt.Sprintf("toStartOfInterval(%s, INTERVAL 30 MINUTE)", timeField),
				NumberName:    "num_30m",
				NumberFunc:    fmt.Sprintf("toInt32(toMinute(%s) / 30)", timeField), // 0-1
			},
			{
				Granularity:   "1h",
				TimestampName: "time_1h",
				TimeFunc:      fmt.Sprintf("toStartOfHour(%s)", timeField),
				NumberName:    "num_1h",
				NumberFunc:    fmt.Sprintf("toHour(%s)", timeField), // 0-23
			},
			{
				Granularity:   "8h",
				TimestampName: "time_8h",
				TimeFunc:      fmt.Sprintf("toStartOfInterval(%s, INTERVAL 8 HOUR)", timeField),
				NumberName:    "num_8h",
				NumberFunc:    fmt.Sprintf("toInt32(toHour(%s) / 8)", timeField), // 0-2
			},
			{
				Granularity:   "12h",
				TimestampName: "time_12h",
				TimeFunc:      fmt.Sprintf("toStartOfInterval(%s, INTERVAL 12 HOUR)", timeField),
				NumberName:    "num_12h",
				NumberFunc:    fmt.Sprintf("toInt32(toHour(%s) / 12)", timeField), // 0-1
			},
			{
				Granularity:   "1d",
				TimestampName: "time_1d",
				TimeFunc:      fmt.Sprintf("toDate(%s)", timeField),
				NumberName:    "num_1d",
				NumberFunc:    fmt.Sprintf("toDayOfMonth(%s)", timeField), // 1-31
			},
			{
				Granularity:   "1w",
				TimestampName: "time_1w",
				TimeFunc:      fmt.Sprintf("toMonday(%s)", timeField),
				NumberName:    "num_1w",
				NumberFunc:    fmt.Sprintf("toISOWeek(%s)", timeField), // 1-53
			},
			{
				Granularity:   "1M",
				TimestampName: "time_1M",
				TimeFunc:      fmt.Sprintf("toStartOfMonth(%s)", timeField),
				NumberName:    "num_1M",
				NumberFunc:    fmt.Sprintf("toMonth(%s)", timeField), // 1-12
			},
			{
				Granularity:   "1q",
				TimestampName: "time_1q",
				TimeFunc:      fmt.Sprintf("toStartOfQuarter(%s)", timeField),
				NumberName:    "num_1q",
				NumberFunc:    fmt.Sprintf("toQuarter(%s)", timeField), // 1-4
			},
			{
				Granularity:   "1y",
				TimestampName: "time_1y",
				TimeFunc:      fmt.Sprintf("toStartOfYear(%s)", timeField),
				NumberName:    "num_1y",
				NumberFunc:    fmt.Sprintf("toYear(%s)", timeField), // 2026...
			},
		}

	case "hour":
		// 小时级视图：保留原始小时 + 向上汇总
		levels = []TimeAggregationLevel{
			{
				Granularity:   "1h",
				TimestampName: "time_1h",
				TimeFunc:      fmt.Sprintf("toStartOfHour(%s)", timeField),
				NumberName:    "num_1h",
				NumberFunc:    fmt.Sprintf("toHour(%s)", timeField), // 0-23
			},
			{
				Granularity:   "8h",
				TimestampName: "time_8h",
				TimeFunc:      fmt.Sprintf("toStartOfInterval(%s, INTERVAL 8 HOUR)", timeField),
				NumberName:    "num_8h",
				NumberFunc:    fmt.Sprintf("toInt32(toHour(%s) / 8)", timeField), // 0-2
			},
			{
				Granularity:   "12h",
				TimestampName: "time_12h",
				TimeFunc:      fmt.Sprintf("toStartOfInterval(%s, INTERVAL 12 HOUR)", timeField),
				NumberName:    "num_12h",
				NumberFunc:    fmt.Sprintf("toInt32(toHour(%s) / 12)", timeField), // 0-1
			},
			{
				Granularity:   "1d",
				TimestampName: "time_1d",
				TimeFunc:      fmt.Sprintf("toDate(%s)", timeField),
				NumberName:    "num_1d",
				NumberFunc:    fmt.Sprintf("toDayOfMonth(%s)", timeField), // 1-31
			},
			{
				Granularity:   "1w",
				TimestampName: "time_1w",
				TimeFunc:      fmt.Sprintf("toMonday(%s)", timeField),
				NumberName:    "num_1w",
				NumberFunc:    fmt.Sprintf("toISOWeek(%s)", timeField), // 1-53
			},
			{
				Granularity:   "1M",
				TimestampName: "time_1M",
				TimeFunc:      fmt.Sprintf("toStartOfMonth(%s)", timeField),
				NumberName:    "num_1M",
				NumberFunc:    fmt.Sprintf("toMonth(%s)", timeField), // 1-12
			},
			{
				Granularity:   "1q",
				TimestampName: "time_1q",
				TimeFunc:      fmt.Sprintf("toStartOfQuarter(%s)", timeField),
				NumberName:    "num_1q",
				NumberFunc:    fmt.Sprintf("toQuarter(%s)", timeField), // 1-4
			},
			{
				Granularity:   "1y",
				TimestampName: "time_1y",
				TimeFunc:      fmt.Sprintf("toStartOfYear(%s)", timeField),
				NumberName:    "num_1y",
				NumberFunc:    fmt.Sprintf("toYear(%s)", timeField),
			},
		}

	case "day":
		// 天级视图：保留原始天 + 向上汇总
		levels = []TimeAggregationLevel{
			{
				Granularity:   "1d",
				TimestampName: "time_1d",
				TimeFunc:      fmt.Sprintf("toDate(%s)", timeField),
				NumberName:    "num_1d",
				NumberFunc:    fmt.Sprintf("toDayOfMonth(%s)", timeField), // 1-31
			},
			{
				Granularity:   "1w",
				TimestampName: "time_1w",
				TimeFunc:      fmt.Sprintf("toMonday(%s)", timeField),
				NumberName:    "num_1w",
				NumberFunc:    fmt.Sprintf("toISOWeek(%s)", timeField), // 1-53
			},
			{
				Granularity:   "1M",
				TimestampName: "time_1M",
				TimeFunc:      fmt.Sprintf("toStartOfMonth(%s)", timeField),
				NumberName:    "num_1M",
				NumberFunc:    fmt.Sprintf("toMonth(%s)", timeField), // 1-12
			},
			{
				Granularity:   "1q",
				TimestampName: "time_1q",
				TimeFunc:      fmt.Sprintf("toStartOfQuarter(%s)", timeField),
				NumberName:    "num_1q",
				NumberFunc:    fmt.Sprintf("toQuarter(%s)", timeField), // 1-4
			},
			{
				Granularity:   "1y",
				TimestampName: "time_1y",
				TimeFunc:      fmt.Sprintf("toStartOfYear(%s)", timeField),
				NumberName:    "num_1y",
				NumberFunc:    fmt.Sprintf("toYear(%s)", timeField),
			},
		}

	case "month":
		// 月级视图：保留原始月 + 向上汇总
		levels = []TimeAggregationLevel{
			{
				Granularity:   "1M",
				TimestampName: "time_1M",
				TimeFunc:      fmt.Sprintf("toStartOfMonth(%s)", timeField),
				NumberName:    "num_1M",
				NumberFunc:    fmt.Sprintf("toMonth(%s)", timeField), // 1-12
			},
			{
				Granularity:   "1q",
				TimestampName: "time_1q",
				TimeFunc:      fmt.Sprintf("toStartOfQuarter(%s)", timeField),
				NumberName:    "num_1q",
				NumberFunc:    fmt.Sprintf("toQuarter(%s)", timeField), // 1-4
			},
			{
				Granularity:   "1y",
				TimestampName: "time_1y",
				TimeFunc:      fmt.Sprintf("toStartOfYear(%s)", timeField),
				NumberName:    "num_1y",
				NumberFunc:    fmt.Sprintf("toYear(%s)", timeField),
			},
		}
	}

	return levels
}

// 时间粒度类型定义
type GranularityTimeType string

const (
	GranularityMinute  GranularityTimeType = "1m"   // 1分钟
	Granularity10Min   GranularityTimeType = "10m"  // 10分钟
	Granularity30Min   GranularityTimeType = "30m"  // 30分钟
	GranularityHour    GranularityTimeType = "1h"   // 1小时
	Granularity8Hour   GranularityTimeType = "8h"   // 8小时
	Granularity12Hour  GranularityTimeType = "12h"  // 12小时
	GranularityDay     GranularityTimeType = "1d"   // 1天
	GranularityWeek    GranularityTimeType = "1w"   // 1周
	GranularityMonth   GranularityTimeType = "1M"   // 1月
	GranularityQuarter GranularityTimeType = "1q"   // 1季度
	GranularityYear    GranularityTimeType = "1y"   // 1年
	GranularityNone    GranularityTimeType = "none" // 不聚合，返回所有时间粒度列
)

//	查询业务统计数据(带多时间粒度汇总列 + 时间编号)
//
// granularity: 可选参数,指定查询粒度
//   - GranularityMinute (1m): 查询1分钟级聚合 + 向上汇总
//   - Granularity10Min (10m): 查询10分钟级聚合 + 向上汇总
//   - Granularity30Min (30m): 查询30分钟级聚合 + 向上汇总
//   - GranularityHour (1h): 查询1小时级聚合 + 向上汇总
//   - Granularity8Hour (8h): 查询8小时级聚合 + 向上汇总
//   - Granularity12Hour (12h): 查询12小时级聚合 + 向上汇总
//   - GranularityDay (1d): 查询1天级聚合 + 向上汇总
//   - GranularityWeek (1w): 查询1周级聚合 + 向上汇总
//   - GranularityMonth (1M): 查询1月级聚合 + 向上汇总
//   - GranularityQuarter (1q): 查询1季度级聚合 + 向上汇总
//   - GranularityYear (1y): 查询1年级聚合
//   - GranularityNone 或不传: 返回所有时间粒度列，按原始时间字段分组（不聚合）
func (ch *ClickHouse) QueryBusinessStatsWithTimeAggregations(
	viewName string,
	dimensions map[string]interface{},
	startTime, endTime time.Time,
	granularity ...GranularityTimeType, //  改为 GranularityTimeType 枚举类型
) ([]map[string]interface{}, error) {
	// 获取配置
	config, err := ch.GetBusinessViewConfig(viewName)
	if err != nil {
		return nil, fmt.Errorf("获取配置失败: %w", err)
	}

	dims := config.GetDimensions()
	decimalFields := config.GetDecimalFields()
	numericFields := config.GetNumericFields()

	//  修复: 根据 granularity 参数决定查询哪些时间列
	var timeAggLevels []TimeAggregationLevel
	var shouldAggregate bool
	var requestedGranularity string

	if len(granularity) > 0 && granularity[0] != GranularityNone {
		// 指定了粒度，返回该粒度及以上的所有时间列
		requestedGranularity = string(granularity[0]) //  转换为字符串
		allLevels := ch.getTimeAggregationLevels(config.TimeGranularity, config.TimeField)

		foundStart := false
		for _, level := range allLevels {
			if level.Granularity == requestedGranularity {
				foundStart = true
			}
			if foundStart {
				timeAggLevels = append(timeAggLevels, level)
			}
		}

		if len(timeAggLevels) == 0 {
			return nil, fmt.Errorf("查询粒度 %s 不能小于视图粒度 %s", requestedGranularity, config.TimeGranularity)
		}

		shouldAggregate = true
	} else {
		// 未指定粒度 或 GranularityNone: 返回所有时间粒度列，不聚合
		timeAggLevels = ch.getTimeAggregationLevels(config.TimeGranularity, config.TimeField)
		if len(timeAggLevels) == 0 {
			return nil, fmt.Errorf("不支持的时间粒度: %s", config.TimeGranularity)
		}

		requestedGranularity = "all"
		shouldAggregate = false
	}

	// 构建 SELECT 字段
	var selectFields []string

	//  关键修复: 直接 SELECT 视图中预计算的列名，不使用函数表达式
	if shouldAggregate {
		// 聚合模式: 第一个粒度不包装，其他粒度用 any() 包装
		for i, level := range timeAggLevels {
			if i == 0 {
				// 第一个粒度（最细粒度）：直接使用列名
				selectFields = append(selectFields,
					level.TimestampName, // 直接使用列名: time_1m
					fmt.Sprintf("any(%s) as %s", level.NumberName, level.NumberName), // 编号列用 any()
				)
			} else {
				// 其他粒度（更粗粒度）：全部用 any() 包装
				selectFields = append(selectFields,
					fmt.Sprintf("any(%s) as %s", level.TimestampName, level.TimestampName),
					fmt.Sprintf("any(%s) as %s", level.NumberName, level.NumberName),
				)
			}
		}
	} else {
		// 不聚合模式: 直接使用列名
		for _, level := range timeAggLevels {
			selectFields = append(selectFields,
				level.TimestampName, // 直接使用列名
				level.NumberName,
			)
		}
	}

	// 添加业务维度
	selectFields = append(selectFields, dims...)

	// 添加聚合字段
	selectFields = append(selectFields, "sum(record_count) as record_count")

	// Decimal 字段
	for _, field := range decimalFields {
		selectFields = append(selectFields,
			fmt.Sprintf("sum(total_%s) as total_%s", field, field),
			fmt.Sprintf("CAST(avg(avg_%s) AS Decimal(20, 8)) as avg_%s", field, field),
			fmt.Sprintf("max(max_%s) as max_%s", field, field),
			fmt.Sprintf("min(min_%s) as min_%s", field, field),
		)
	}

	// 数值字段
	for _, field := range numericFields {
		selectFields = append(selectFields,
			fmt.Sprintf("sum(total_%s) as total_%s", field, field),
			fmt.Sprintf("avg(avg_%s) as avg_%s", field, field),
			fmt.Sprintf("max(max_%s) as max_%s", field, field),
			fmt.Sprintf("min(min_%s) as min_%s", field, field),
		)
	}

	// 构建 WHERE 条件
	whereConditions := []string{
		fmt.Sprintf("%s BETWEEN ? AND ?", config.TimeField),
	}
	whereParams := []interface{}{startTime, endTime}

	for dim, value := range dimensions {
		whereConditions = append(whereConditions, fmt.Sprintf("%s = ?", dim))
		whereParams = append(whereParams, value)
	}

	//  关键修复: GROUP BY 直接使用预计算的列名
	var groupByFields []string
	var orderByField string

	if shouldAggregate {
		// 聚合模式: 只按最小粒度的时间戳列分组
		requestedLevel := timeAggLevels[0]
		groupByFields = []string{
			requestedLevel.TimestampName, // 直接使用列名: time_1m, time_10m 等
		}
		groupByFields = append(groupByFields, dims...)
		orderByField = requestedLevel.TimestampName
	} else {
		//  修复：不聚合模式 - 按原始时间字段 + 所有时间列 + 业务维度分组
		groupByFields = []string{config.TimeField} // stat_time

		//  添加所有时间列到 GROUP BY
		for _, level := range timeAggLevels {
			groupByFields = append(groupByFields,
				level.TimestampName, // time_1m, time_10m, ...
				level.NumberName,    // num_1m, num_10m, ...
			)
		}

		groupByFields = append(groupByFields, dims...)
		orderByField = config.TimeField
	}

	// 构建完整 SQL
	sql := fmt.Sprintf(`
        SELECT %s
        FROM %s
        WHERE %s
        GROUP BY %s
        ORDER BY %s DESC
    `,
		strings.Join(selectFields, ", "),
		viewName,
		strings.Join(whereConditions, " AND "),
		strings.Join(groupByFields, ", "),
		orderByField,
	)

	logx.Debugw("clickhouse_query_prepared",
		logx.Field("view", viewName),
		logx.Field("aggregate", shouldAggregate),
		logx.Field("granularity", requestedGranularity),
		logx.Field("time_column_count", len(timeAggLevels)*2),
		logx.Field("argument_count", len(whereParams)),
	)

	// 执行查询
	var results []map[string]interface{}
	err = ch.db.Raw(sql, whereParams...).Find(&results).Error
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}

	// 转换 Decimal 字段
	for _, result := range results {
		for _, field := range decimalFields {
			ch.convertToDecimal(result, "total_"+field)
			ch.convertToDecimal(result, "avg_"+field)
			ch.convertToDecimal(result, "min_"+field)
			ch.convertToDecimal(result, "max_"+field)
		}
	}

	logx.Debugw("clickhouse_query_completed", logx.Field("result_count", len(results)))
	return results, nil
}

// 辅助方法：将 map 中的字符串转换为 Decimal
func (ch *ClickHouse) convertToDecimal(result map[string]interface{}, key string) {
	if val, ok := result[key]; ok {
		switch v := val.(type) {
		case string:
			if dec, err := decimal.NewFromString(v); err == nil {
				result[key] = dec
			}
		case float64:
			result[key] = decimal.NewFromFloat(v)
		}
	}
}

func (ch *ClickHouse) QueryBusinessStatsByName(
	viewName string,
	dimensions map[string]interface{},
	startTime, endTime time.Time,
) ([]map[string]interface{}, error) {
	return ch.QueryBusinessStatsWithTimeAggregations(viewName, dimensions, startTime, endTime, GranularityNone)
}
