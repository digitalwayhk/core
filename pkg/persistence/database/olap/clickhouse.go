package olap

import (
	"crypto/sha256"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type ClickHouse struct {
	db       *gorm.DB
	config   *Config
	configDB *gorm.DB
}

// ClickHouse 配置
type Config struct {
	Host         string
	Port         int
	Database     string
	Username     string
	Password     string
	Debug        bool
	MaxOpenConns int
	MaxIdleConns int
	AutoCreateDB bool // 自动创建数据库
	Logger       logger.Interface
}

func (c *Config) Hash() string {
	data := fmt.Sprintf("%s:%d/%s@%s:%d:%d:%v",
		c.Host, c.Port, c.Database, c.Username,
		c.MaxOpenConns, c.MaxIdleConns, c.Debug)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

// 表引擎配置
type TableEngineConfig struct {
	Engine           string        // MergeTree, ReplicatedMergeTree 等
	PartitionBy      string        // 分区键，如 "toYYYYMM(created_at)"
	OrderBy          []string      // 排序键
	TTL              time.Duration // 数据保留时间
	IndexGranularity int           // 索引粒度
}

// 默认表引擎配置
func DefaultTableEngineConfig() *TableEngineConfig {
	return &TableEngineConfig{
		Engine:           "MergeTree()",
		PartitionBy:      "toYYYYMM(created_at)",
		OrderBy:          []string{"created_at"},
		TTL:              90 * 24 * time.Hour, // 90天
		IndexGranularity: 8192,
	}
}

// 🆕 DecimalValue - 自定义 Decimal 类型包装器,用于 ClickHouse 的 Scan 和 Value 实现
type DecimalValue struct {
	decimal.Decimal
}

// Scan 实现 sql.Scanner 接口,从数据库读取时自动转换
func (d *DecimalValue) Scan(value interface{}) error {
	if value == nil {
		d.Decimal = decimal.Zero
		return nil
	}

	switch v := value.(type) {
	case string:
		// ✅ 从 ClickHouse 读取的 Decimal 是 string 类型
		dec, err := decimal.NewFromString(v)
		if err != nil {
			return err
		}
		d.Decimal = dec
		return nil
	case []byte:
		dec, err := decimal.NewFromString(string(v))
		if err != nil {
			return err
		}
		d.Decimal = dec
		return nil
	case float64:
		d.Decimal = decimal.NewFromFloat(v)
		return nil
	case int64:
		d.Decimal = decimal.NewFromInt(v)
		return nil
	default:
		return fmt.Errorf("无法将 %T 转换为 Decimal", value)
	}
}

// Value 实现 driver.Valuer 接口,写入数据库时自动转换
func (d DecimalValue) Value() (driver.Value, error) {
	// ✅ 写入 ClickHouse 时转换为 string
	return d.Decimal.String(), nil
}

var (
	instances sync.Map
	mu        sync.Mutex
)

func NewClickHouse(cfg *Config) (*ClickHouse, error) {
	// 生成配置的唯一键
	key := cfg.Hash()

	// 检查是否已存在
	if instance, ok := instances.Load(key); ok {
		existingCH := instance.(*ClickHouse)

		// 🔧 验证连接是否有效且数据库存在
		if sqlDB, err := existingCH.db.DB(); err == nil {
			if err := sqlDB.Ping(); err == nil {
				// 🔧 验证数据库是否存在
				var exists uint8
				query := fmt.Sprintf("SELECT 1 FROM system.databases WHERE name = '%s'", cfg.Database)
				if err := existingCH.db.Raw(query).Scan(&exists).Error; err == nil && exists == 1 {
					logx.Infof("♻️ 复用现有 ClickHouse 连接 [%s]", key)
					return existingCH, nil
				}
				// 数据库不存在,删除此连接并重建
				logx.Infof("🔄 数据库 %s 不存在,重建连接 [%s]", cfg.Database, key)
			}
		}
		// 连接无效,删除并重建
		instances.Delete(key)
		// 🔧 关闭旧连接
		if sqlDB, err := existingCH.db.DB(); err == nil {
			sqlDB.Close()
		}
	}

	mu.Lock()
	defer mu.Unlock()

	// 双重检查
	if instance, ok := instances.Load(key); ok {
		return instance.(*ClickHouse), nil
	}

	// 创建新连接（原有逻辑）
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 10
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 5
	}

	// 🔧 修复:自动创建数据库逻辑
	if cfg.AutoCreateDB {
		// 第一步:连接到 default 数据库并创建目标数据库
		defaultDSN := fmt.Sprintf("clickhouse://%s:%s@%s:%d/default?dial_timeout=10s&max_execution_time=60",
			cfg.Username,
			cfg.Password,
			cfg.Host,
			cfg.Port,
		)

		defaultDB, err := gorm.Open(clickhouse.Open(defaultDSN), &gorm.Config{
			NamingStrategy: schema.NamingStrategy{
				SingularTable: true,
			},
			Logger:               nil,
			DisableAutomaticPing: false,
		})
		if err != nil {
			return nil, fmt.Errorf("连接 ClickHouse default 数据库失败: %w", err)
		}

		// 创建目标数据库
		createDBSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", cfg.Database)
		if err := defaultDB.Exec(createDBSQL).Error; err != nil {
			return nil, fmt.Errorf("创建数据库 %s 失败: %w", cfg.Database, err)
		}

		logx.Infof("📊 已创建/验证数据库: %s", cfg.Database)

		// 🔧 关闭 default 数据库连接
		if sqlDB, err := defaultDB.DB(); err == nil {
			sqlDB.Close()
		}
	}

	// 第二步:连接到目标数据库
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s:%d/%s?dial_timeout=10s&max_execution_time=60",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)

	db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
		Logger:               nil,
		DisableAutomaticPing: false,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 ClickHouse 失败: %w", err)
	}

	if cfg.Debug {
		db = db.Debug()
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	instance := &ClickHouse{
		db:     db,
		config: cfg,
	}

	instances.Store(key, instance)
	logx.Infof("✅ ClickHouse 连接成功 [%s]", key)

	return instance, nil
}

// 创建业务表
func (ch *ClickHouse) CreateTable(model interface{}, engineCfg ...*TableEngineConfig) error {
	// 使用默认配置
	cfg := DefaultTableEngineConfig()
	if len(engineCfg) > 0 && engineCfg[0] != nil {
		cfg = engineCfg[0]
	}

	// 获取表名
	tableName := ch.getTableName(model)

	// 检查是否继承自 entity.Model
	if !ch.isEntityModel(model) {
		return fmt.Errorf("模型必须继承自 entity.Model")
	}

	// 生成建表 SQL
	createSQL := ch.generateCreateTableSQL(model, tableName, cfg)

	// 执行建表
	if err := ch.db.Exec(createSQL).Error; err != nil {
		logx.Errorf("创建表失败 [%s]: %v", tableName, err)
		return err
	}

	logx.Infof("✅ 创建业务表成功 [%s]", tableName)

	// 自动创建时间维度统计视图
	if err := ch.createTimeBasedViews(model, tableName); err != nil {
		logx.Errorf("创建统计视图失败: %v", err)
		return err
	}

	return nil
}

// 生成建表 SQL (修复版)
func (ch *ClickHouse) generateCreateTableSQL(model interface{}, tableName string, cfg *TableEngineConfig) string {
	var columns []string

	// 解析模型字段
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// 🔧 修复:递归解析嵌入的结构体字段
	columns = ch.parseStructFields(modelType, "")

	// 构建 SQL
	var sql strings.Builder
	sql.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", tableName))
	sql.WriteString(strings.Join(columns, ",\n"))
	sql.WriteString("\n)")

	// ENGINE
	sql.WriteString(fmt.Sprintf("\nENGINE = %s", cfg.Engine))

	// PARTITION BY
	if cfg.PartitionBy != "" {
		sql.WriteString(fmt.Sprintf("\nPARTITION BY %s", cfg.PartitionBy))
	}

	// ORDER BY
	if len(cfg.OrderBy) > 0 {
		sql.WriteString(fmt.Sprintf("\nORDER BY (%s)", strings.Join(cfg.OrderBy, ", ")))
	}

	// TTL
	if cfg.TTL > 0 {
		days := int(cfg.TTL.Hours() / 24)
		sql.WriteString(fmt.Sprintf("\nTTL created_at + INTERVAL %d DAY", days))
	}

	// SETTINGS
	if cfg.IndexGranularity > 0 {
		sql.WriteString(fmt.Sprintf("\nSETTINGS index_granularity = %d", cfg.IndexGranularity))
	}

	return sql.String()
}

// 🆕 递归解析结构体字段(包括嵌入字段)
func (ch *ClickHouse) parseStructFields(structType reflect.Type, prefix string) []string {
	var columns []string

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// 跳过 gorm 忽略的字段
		if gormTag := field.Tag.Get("gorm"); strings.Contains(gormTag, "-") {
			continue
		}

		// 🔧 处理嵌入结构体(Anonymous field)
		if field.Anonymous {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				// 递归解析嵌入结构体
				embeddedColumns := ch.parseStructFields(fieldType, prefix)
				columns = append(columns, embeddedColumns...)
				continue
			}
		}

		columnName := ch.getColumnName(field)
		columnType := ch.getClickHouseType(field)

		if columnName != "" && columnType != "" {
			fullColumnName := columnName
			if prefix != "" {
				fullColumnName = prefix + "_" + columnName
			}
			columns = append(columns, fmt.Sprintf("    %s %s", fullColumnName, columnType))
		}
	}

	return columns
}

// 创建时间维度统计视图
func (ch *ClickHouse) createTimeBasedViews(model interface{}, tableName string) error {
	// 获取数值型字段（用于聚合）
	numericFields := ch.getNumericFields(model)
	decimalFields := ch.getDecimalFields(model)

	// 🆕 1. 创建分钟统计视图
	if err := ch.createMinuteView(tableName, numericFields, decimalFields); err != nil {
		return err
	}

	// 2. 创建小时统计视图
	if err := ch.createHourlyView(tableName, numericFields, decimalFields); err != nil {
		return err
	}

	// 3. 创建日统计视图
	if err := ch.createDailyView(tableName, numericFields, decimalFields); err != nil {
		return err
	}

	// 4. 创建月统计视图
	if err := ch.createMonthlyView(tableName, numericFields, decimalFields); err != nil {
		return err
	}

	return nil
}
func (ch *ClickHouse) getAggregations(numericFields, decimalFields []string) []string {
	var aggregations []string
	aggregations = append(aggregations, "count() as record_count")

	// 普通数值字段
	for _, field := range numericFields {
		aggregations = append(aggregations,
			fmt.Sprintf("sum(%s) as total_%s", field, field),
			fmt.Sprintf("avg(%s) as avg_%s", field, field),
			fmt.Sprintf("max(%s) as max_%s", field, field),
			fmt.Sprintf("min(%s) as min_%s", field, field),
		)
	}

	// ✅ Decimal 字段 - 使用 toString() 保持精度
	for _, field := range decimalFields {
		aggregations = append(aggregations,
			fmt.Sprintf("sum(%s) as total_%s", field, field),
			fmt.Sprintf("avg(%s) as avg_%s", field, field),
			fmt.Sprintf("max(%s) as max_%s", field, field),
			fmt.Sprintf("min(%s) as min_%s", field, field),
		)
	}
	return aggregations
}

// 🆕 创建分钟统计视图 (支持 Decimal)
func (ch *ClickHouse) createMinuteView(tableName string, numericFields, decimalFields []string) error {
	viewName := fmt.Sprintf("%s_stats_minute", tableName)

	aggregations := ch.getAggregations(numericFields, decimalFields)

	sql := fmt.Sprintf(`
        CREATE MATERIALIZED VIEW IF NOT EXISTS %s
        ENGINE = SummingMergeTree()
        PARTITION BY toYYYYMMDD(stat_time)
        ORDER BY stat_time
        TTL stat_time + INTERVAL 7 DAY
        POPULATE
        AS SELECT
            toStartOfMinute(created_at) as stat_time,
            %s
        FROM %s
        GROUP BY stat_time
    `, viewName, strings.Join(aggregations, ",\n\t\t\t"), tableName)

	if err := ch.db.Exec(sql).Error; err != nil {
		return fmt.Errorf("创建分钟视图失败: %w", err)
	}

	logx.Infof("✅ 创建分钟统计视图成功 [%s]", viewName)
	return nil
}

// 🆕 创建小时统计视图 (支持 Decimal)
func (ch *ClickHouse) createHourlyView(tableName string, numericFields, decimalFields []string) error {
	viewName := fmt.Sprintf("%s_stats_hourly", tableName)

	aggregations := ch.getAggregations(numericFields, decimalFields)

	sql := fmt.Sprintf(`
        CREATE MATERIALIZED VIEW IF NOT EXISTS %s
        ENGINE = SummingMergeTree()
        PARTITION BY toYYYYMM(stat_time)
        ORDER BY stat_time
        POPULATE
        AS SELECT
            toStartOfHour(created_at) as stat_time,
            %s
        FROM %s
        GROUP BY stat_time
    `, viewName, strings.Join(aggregations, ",\n\t\t\t"), tableName)

	if err := ch.db.Exec(sql).Error; err != nil {
		return fmt.Errorf("创建小时视图失败: %w", err)
	}

	logx.Infof("✅ 创建小时统计视图成功 [%s]", viewName)
	return nil
}

// 🆕 查询分钟统计数据
func (ch *ClickHouse) QueryMinuteStats(tableName string, startTime, endTime time.Time) ([]map[string]interface{}, error) {
	viewName := fmt.Sprintf("%s_stats_minute", tableName)

	var results []map[string]interface{}
	err := ch.db.Table(viewName).
		Where("stat_time BETWEEN ? AND ?", startTime, endTime).
		Order("stat_time DESC").
		Find(&results).Error

	return results, err
}

// 🆕 创建日统计视图 (支持 Decimal)
func (ch *ClickHouse) createDailyView(tableName string, numericFields, decimalFields []string) error {
	viewName := fmt.Sprintf("%s_stats_daily", tableName)

	aggregations := ch.getAggregations(numericFields, decimalFields)

	sql := fmt.Sprintf(`
        CREATE MATERIALIZED VIEW IF NOT EXISTS %s
        ENGINE = SummingMergeTree()
        PARTITION BY toYYYYMM(stat_date)
        ORDER BY stat_date
        POPULATE
        AS SELECT
            toDate(created_at) as stat_date,
            %s
        FROM %s
        GROUP BY stat_date
    `, viewName, strings.Join(aggregations, ",\n\t\t\t"), tableName)

	if err := ch.db.Exec(sql).Error; err != nil {
		return fmt.Errorf("创建日视图失败: %w", err)
	}

	logx.Infof("✅ 创建日统计视图成功 [%s]", viewName)
	return nil
}

// 🆕 创建月统计视图 (支持 Decimal)
func (ch *ClickHouse) createMonthlyView(tableName string, numericFields, decimalFields []string) error {
	viewName := fmt.Sprintf("%s_stats_monthly", tableName)

	aggregations := ch.getAggregations(numericFields, decimalFields)

	sql := fmt.Sprintf(`
        CREATE MATERIALIZED VIEW IF NOT EXISTS %s
        ENGINE = SummingMergeTree()
        PARTITION BY toYear(stat_month)
        ORDER BY stat_month
        POPULATE
        AS SELECT
            toStartOfMonth(created_at) as stat_month,
            %s
        FROM %s
        GROUP BY stat_month
    `, viewName, strings.Join(aggregations, ",\n\t\t\t"), tableName)

	if err := ch.db.Exec(sql).Error; err != nil {
		return fmt.Errorf("创建月视图失败: %w", err)
	}

	logx.Infof("✅ 创建月统计视图成功 [%s]", viewName)
	return nil
}

// 查询统计数据
func (ch *ClickHouse) QueryHourlyStats(tableName string, startTime, endTime time.Time) ([]map[string]interface{}, error) {
	viewName := fmt.Sprintf("%s_stats_hourly", tableName)

	var results []map[string]interface{}
	err := ch.db.Table(viewName).
		Where("stat_time BETWEEN ? AND ?", startTime, endTime).
		Order("stat_time DESC").
		Find(&results).Error

	return results, err
}

func (ch *ClickHouse) QueryDailyStats(tableName string, startDate, endDate time.Time) ([]map[string]interface{}, error) {
	viewName := fmt.Sprintf("%s_stats_daily", tableName)

	var results []map[string]interface{}
	err := ch.db.Table(viewName).
		Where("stat_date BETWEEN ? AND ?", startDate, endDate).
		Order("stat_date DESC").
		Find(&results).Error

	return results, err
}

// 🆕 通用查询方法 - 支持所有粒度
func (ch *ClickHouse) QueryStats(tableName string, granularity string, startTime, endTime time.Time) ([]map[string]interface{}, error) {
	var viewName string
	var timeField string

	switch granularity {
	case "minute":
		viewName = fmt.Sprintf("%s_stats_minute", tableName)
		timeField = "stat_time"
	case "hour", "hourly":
		viewName = fmt.Sprintf("%s_stats_hourly", tableName)
		timeField = "stat_time"
	case "day", "daily":
		viewName = fmt.Sprintf("%s_stats_daily", tableName)
		timeField = "stat_date"
	case "month", "monthly":
		viewName = fmt.Sprintf("%s_stats_monthly", tableName)
		timeField = "stat_month"
	default:
		return nil, fmt.Errorf("不支持的时间粒度: %s", granularity)
	}

	var results []map[string]interface{}
	err := ch.db.Table(viewName).
		Where(fmt.Sprintf("%s BETWEEN ? AND ?", timeField), startTime, endTime).
		Order(fmt.Sprintf("%s DESC", timeField)).
		Find(&results).Error

	return results, err
}

// 插入数据
func (ch *ClickHouse) Insert(data interface{}) error {
	if data == nil {
		return fmt.Errorf("插入数据不能为 nil")
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return fmt.Errorf("插入数据不能为空指针")
	}

	return ch.db.Create(data).Error
}

// 批量插入
func (ch *ClickHouse) BatchInsert(data interface{}) error {
	if data == nil {
		return fmt.Errorf("批量插入数据不能为 nil")
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Slice {
		return fmt.Errorf("批量插入需要切片类型,当前类型: %s", v.Kind())
	}

	if v.Len() == 0 {
		logx.Info("批量插入数据为空,跳过")
		return nil
	}

	// 检查切片元素
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		if elem.Kind() == reflect.Ptr && elem.IsNil() {
			return fmt.Errorf("批量插入数据第 %d 个元素为 nil", i)
		}
	}

	return ch.db.CreateInBatches(data, 1000).Error
}

// 辅助方法

func (ch *ClickHouse) getTableName(model interface{}) string {
	stmt := &gorm.Statement{DB: ch.db}
	stmt.Parse(model)
	return stmt.Schema.Table
}

func (ch *ClickHouse) isEntityModel(model interface{}) bool {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// 检查是否有 CreatedAt 字段
	_, hasCreatedAt := modelType.FieldByName("CreatedAt")
	return hasCreatedAt
}

func (ch *ClickHouse) getColumnName(field reflect.StructField) string {
	// 检查 gorm tag
	if gormTag := field.Tag.Get("gorm"); gormTag != "" {
		tags := strings.Split(gormTag, ";")
		for _, tag := range tags {
			if strings.HasPrefix(tag, "column:") {
				return strings.TrimPrefix(tag, "column:")
			}
		}
	}

	// 使用 snake_case
	return toSnakeCase(field.Name)
}

func (ch *ClickHouse) getClickHouseType(field reflect.StructField) string {
	fieldType := field.Type

	// 🔧 处理指针类型
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// 🔧 新增: 处理 decimal.Decimal 类型
	if fieldType.PkgPath() == "github.com/shopspring/decimal" && fieldType.Name() == "Decimal" {
		return "Decimal(20, 8)" // 20位总长度, 8位小数
	}

	switch fieldType.Kind() {
	case reflect.Bool:
		return "UInt8"
	case reflect.Int8:
		return "Int8"
	case reflect.Int16:
		return "Int16"
	case reflect.Int32, reflect.Int:
		return "Int32"
	case reflect.Int64:
		return "Int64"
	case reflect.Uint8:
		return "UInt8"
	case reflect.Uint16:
		return "UInt16"
	case reflect.Uint32, reflect.Uint:
		return "UInt32"
	case reflect.Uint64:
		return "UInt64"
	case reflect.Float32:
		return "Float32"
	case reflect.Float64:
		return "Float64"
	case reflect.String:
		return "String"
	case reflect.Struct:
		// 处理 time.Time 类型
		if fieldType.PkgPath() == "time" && fieldType.Name() == "Time" {
			return "DateTime"
		}
		return "String"
	default:
		return "String"
	}
}

// 🆕 获取数值型字段(排除 Decimal)
func (ch *ClickHouse) getNumericFields(model interface{}) []string {
	var fields []string

	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// 跳过 gorm 忽略的字段
		if gormTag := field.Tag.Get("gorm"); strings.Contains(gormTag, "-") {
			continue
		}

		// 🔧 处理嵌入结构体
		if field.Anonymous {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				continue // 跳过嵌入结构体本身
			}
		}

		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// ✅ 跳过 Decimal 类型(单独处理)
		isDecimal := fieldType.PkgPath() == "github.com/shopspring/decimal" &&
			fieldType.Name() == "Decimal"
		if isDecimal {
			continue
		}

		// 只统计普通数值类型字段
		switch fieldType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:

			columnName := ch.getColumnName(field)
			// 排除 ID 和时间戳字段
			if columnName != "id" && columnName != "created_at" && columnName != "updated_at" {
				fields = append(fields, columnName)
			}
		}
	}

	return fields
}

// 🆕 获取 Decimal 类型字段
func (ch *ClickHouse) getDecimalFields(model interface{}) []string {
	var fields []string

	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// 跳过 gorm 忽略的字段
		if gormTag := field.Tag.Get("gorm"); strings.Contains(gormTag, "-") {
			continue
		}

		// 🔧 处理嵌入结构体
		if field.Anonymous {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				continue
			}
		}

		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// ✅ 检查是否为 decimal.Decimal 类型
		isDecimal := fieldType.PkgPath() == "github.com/shopspring/decimal" &&
			fieldType.Name() == "Decimal"

		if isDecimal {
			columnName := ch.getColumnName(field)
			// 排除 ID 和时间戳字段
			if columnName != "id" && columnName != "created_at" && columnName != "updated_at" {
				fields = append(fields, columnName)
			}
		}
	}

	return fields
}

func (ch *ClickHouse) Close() error {
	sqlDB, err := ch.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// 工具函数：转换为 snake_case
// toSnakeCase 将驼峰命名转换为下划线命名
// 支持:
// - UserID -> user_id
// - HTTPServer -> http_server
// - XMLHTTPRequest -> xmlhttp_request
// - createdAt -> created_at
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}

	var result []rune
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// 如果是大写字母
		if unicode.IsUpper(r) {
			// 不是第一个字符时需要判断是否添加下划线
			if i > 0 {
				prevChar := runes[i-1]

				// 情况1: 前一个是小写字母 (如 userId 的 I)
				if unicode.IsLower(prevChar) {
					result = append(result, '_')
				} else if unicode.IsUpper(prevChar) && i+1 < len(runes) {
					// 情况2: 前一个是大写,当前是大写,下一个是小写
					// (如 HTTPServer 的 S, 需要在 S 前加下划线)
					nextChar := runes[i+1]
					if unicode.IsLower(nextChar) {
						result = append(result, '_')
					}
				}
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}

	return string(result)
}

// 关闭所有连接
func CloseAllConnections() error {
	var errs []error
	instances.Range(func(key, value interface{}) bool {
		if ch, ok := value.(*ClickHouse); ok {
			if err := ch.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		instances.Delete(key)
		return true
	})

	if len(errs) > 0 {
		return fmt.Errorf("关闭连接时发生 %d 个错误", len(errs))
	}
	return nil
}
