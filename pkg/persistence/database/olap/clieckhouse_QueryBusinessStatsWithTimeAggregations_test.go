package olap

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/json"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ==================== 测试套件 ====================

type MinuteGranularityTestSuite struct {
	suite.Suite
	ch          *ClickHouse
	configDB    *gorm.DB
	testDB      string
	passedCount int
	failedCount int
	totalCount  int
	startTime   time.Time
}

// SetupSuite - 套件级初始化
func (s *MinuteGranularityTestSuite) SetupSuite() {
	s.startTime = time.Now()
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("🚀 分钟级时间粒度汇总测试套件 v2.0")
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("")

	// 初始化配置数据库 (SQLite 内存)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	s.Require().NoError(err, "创建配置数据库失败")
	s.configDB = db

	s.T().Log("✅ 配置数据库初始化成功")
}

// SetupTest - 每个测试前执行
func (s *MinuteGranularityTestSuite) SetupTest() {
	s.totalCount++

	// 为每个测试创建独立数据库
	s.testDB = fmt.Sprintf("test_minute_%d", time.Now().UnixNano()%100000)
	ch, err := NewClickHouse(&Config{
		Host:         "localhost",
		Port:         9000,
		Database:     s.testDB,
		Username:     "default",
		Password:     "clickhouse",
		AutoCreateDB: true,
		Debug:        false,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	s.Require().NoError(err, "创建测试连接失败")
	s.ch = ch

	// 设置配置数据库
	s.ch.SetConfigDB(s.configDB)

	// 初始化配置表
	s.Require().NoError(s.ch.InitConfigTable())
}

// TearDownTest - 每个测试后执行
func (s *MinuteGranularityTestSuite) TearDownTest() {
	if !s.T().Failed() {
		s.passedCount++
		s.T().Log("✅ PASSED")
	} else {
		s.failedCount++
		s.T().Log("❌ FAILED")
	}

	// 清理数据库
	if s.ch != nil && s.ch.db != nil {
		dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS %s", s.testDB)
		s.ch.db.Exec(dropSQL)
		s.ch.Close()
	}
}

// TearDownSuite - 套件级清理
func (s *MinuteGranularityTestSuite) TearDownSuite() {
	duration := time.Since(s.startTime)

	s.T().Log("")
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("📊 测试套件执行报告")
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("")
	s.T().Logf("⏱️  总耗时: %v", duration)
	s.T().Log("")
	s.T().Log("📈 测试统计:")
	s.T().Logf("   • 总计: %d 个测试", s.totalCount)
	s.T().Logf("   • 通过: %d ✅", s.passedCount)
	s.T().Logf("   • 失败: %d ❌", s.failedCount)
	s.T().Log("")

	if s.totalCount > 0 {
		passRate := float64(s.passedCount) / float64(s.totalCount) * 100
		s.T().Logf("📊 通过率: %.1f%%", passRate)
	}

	s.T().Log("")
	s.T().Log("=" + strings.Repeat("=", 80))
}

// ==================== 测试用例 ====================

// Test1_Query1MinuteGranularity 测试查询1分钟粒度
func (s *MinuteGranularityTestSuite) Test1_Query1MinuteGranularity() {
	s.T().Log("🧪 测试: 查询1分钟粒度数据")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1m",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（5分钟，每分钟1条）
	baseTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		trade := &Trade{
			UserID: "U001",
			Symbol: "BTCUSDT",
			Amount: 100.0 * float64(i+1),
		}
		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		s.Require().NoError(s.ch.Insert(trade))
	}

	time.Sleep(3 * time.Second)

	// 3. 查询1分钟粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1m",
		map[string]interface{}{"user_id": "U001"},
		baseTime,
		baseTime.Add(10*time.Minute),
		"1m", // 查询1分钟粒度
	)

	s.Require().NoError(err)
	s.Equal(5, len(results), "应该返回5条1分钟数据")

	s.T().Log("📊 查询结果:")
	for i, result := range results {
		time1m := result["time_1m"]
		num1m := result["num_1m"]
		totalAmount := result["total_amount"]

		s.T().Logf("   [%d] time_1m=%v, num_1m=%v, total_amount=%v",
			i+1, time1m, num1m, totalAmount)

		// 验证必须包含这些字段
		s.NotNil(time1m, "应该包含 time_1m")
		s.NotNil(num1m, "应该包含 num_1m")
		s.NotNil(totalAmount, "应该包含 total_amount")
	}

	s.T().Log("✅ 测试通过")
}

// Test2_Query10MinuteGranularity 测试查询10分钟粒度
func (s *MinuteGranularityTestSuite) Test2_Query10MinuteGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询10分钟聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_10m",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越2个10分钟区间）
	// 10:05-10:09 (5条) -> num_10m=0
	// 10:10-10:14 (5条) -> num_10m=1
	baseTime := time.Date(2026, 1, 29, 10, 5, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		trade := &Trade{
			UserID: "U001",
			Symbol: "BTCUSDT",
			Amount: 100.0,
		}
		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		s.Require().NoError(s.ch.Insert(trade))
	}

	time.Sleep(3 * time.Second)

	// 3. 查询10分钟粒度（应该自动聚合）
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_10m",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-time.Hour),
		baseTime.Add(time.Hour),
		"10m", // 查询10分钟粒度
	)

	s.Require().NoError(err)
	s.Equal(2, len(results), "应该返回2个10分钟聚合结果")

	s.T().Log("📊 10分钟聚合结果:")

	aggregated := make(map[int]int64)
	for i, result := range results {
		time10m := result["time_10m"]
		num10m := result["num_10m"]
		recordCount := result["record_count"]

		var numKey int
		switch v := num10m.(type) {
		case int:
			numKey = v
		case int32:
			numKey = int(v)
		case int64:
			numKey = int(v)
		case uint8:
			numKey = int(v)
		}

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		aggregated[numKey] = count

		s.T().Logf("   [%d] time_10m=%v, num_10m=%d, record_count=%d",
			i+1, time10m, numKey, count)
	}

	// 验证聚合结果
	s.Equal(int64(5), aggregated[0], "num_10m=0 (10:00-10:09) 应该有5条记录")
	s.Equal(int64(5), aggregated[1], "num_10m=1 (10:10-10:19) 应该有5条记录")

	s.T().Log("✅ 测试通过")
}

// Test3_Query30MinuteGranularity 测试查询30分钟粒度
func (s *MinuteGranularityTestSuite) Test3_Query30MinuteGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询30分钟聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_30m",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越2个30分钟区间）
	// 10:05-10:29 (25条) -> num_30m=0 (10:00-10:29)
	// 10:30-10:34 (5条)  -> num_30m=1 (10:30-10:59)
	baseTime := time.Date(2026, 1, 29, 10, 5, 0, 0, time.UTC)

	for i := 0; i < 30; i++ {
		trade := &Trade{
			UserID: "U001",
			Symbol: "BTCUSDT",
			Amount: 100.0,
		}
		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		s.Require().NoError(s.ch.Insert(trade))
	}

	time.Sleep(3 * time.Second)

	// 3. 查询30分钟粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_30m",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-time.Hour),
		baseTime.Add(time.Hour),
		"30m", // 查询30分钟粒度
	)

	s.Require().NoError(err)
	s.Equal(2, len(results), "应该返回2个30分钟聚合结果")

	s.T().Log("📊 30分钟聚合结果:")

	aggregated := make(map[int]int64)
	for i, result := range results {
		time30m := result["time_30m"]
		num30m := result["num_30m"]
		recordCount := result["record_count"]

		var numKey int
		switch v := num30m.(type) {
		case int:
			numKey = v
		case int32:
			numKey = int(v)
		case int64:
			numKey = int(v)
		case uint8:
			numKey = int(v)
		}

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		aggregated[numKey] = count

		s.T().Logf("   [%d] time_30m=%v, num_30m=%d, record_count=%d",
			i+1, time30m, numKey, count)
	}

	// 验证聚合结果
	s.Equal(int64(25), aggregated[0], "num_30m=0 (10:00-10:29) 应该有25条记录")
	s.Equal(int64(5), aggregated[1], "num_30m=1 (10:30-10:59) 应该有5条记录")

	s.T().Log("✅ 测试通过")
}

// Test4_Query1HourGranularity 测试查询1小时粒度
func (s *MinuteGranularityTestSuite) Test4_Query1HourGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询1小时聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1h",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越3个小时）
	baseTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)

	for h := 0; h < 3; h++ {
		for m := 0; m < 20; m++ { // 每小时20分钟数据
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = baseTime.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
	}

	time.Sleep(3 * time.Second)

	// 3. 查询1小时粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1h",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-time.Hour),
		baseTime.Add(4*time.Hour),
		"1h", // 查询1小时粒度
	)

	s.Require().NoError(err)
	s.Equal(3, len(results), "应该返回3个小时聚合结果")

	s.T().Log("📊 1小时聚合结果:")

	aggregated := make(map[int]int64)
	for i, result := range results {
		time1h := result["time_1h"]
		num1h := result["num_1h"]
		recordCount := result["record_count"]

		var numKey int
		switch v := num1h.(type) {
		case int:
			numKey = v
		case int32:
			numKey = int(v)
		case int64:
			numKey = int(v)
		case uint8:
			numKey = int(v)
		}

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		aggregated[numKey] = count

		s.T().Logf("   [%d] time_1h=%v, num_1h=%d, record_count=%d",
			i+1, time1h, numKey, count)
	}

	// 验证每个小时都有20条记录
	for h := 10; h < 13; h++ {
		s.Equal(int64(20), aggregated[h], fmt.Sprintf("num_1h=%d 应该有20条记录", h))
	}

	s.T().Log("✅ 测试通过")
}

// Test5_Query1DayGranularity 测试查询1天粒度
func (s *MinuteGranularityTestSuite) Test5_Query1DayGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询1天聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1d",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         7,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越3天）
	baseTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)

	for d := 0; d < 3; d++ {
		for i := 0; i < 30; i++ { // 每天30条数据
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = baseTime.Add(time.Duration(d)*24*time.Hour + time.Duration(i)*time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
	}

	time.Sleep(3 * time.Second)

	// 3. 查询1天粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1d",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-24*time.Hour),
		baseTime.Add(4*24*time.Hour),
		"1d", // 查询1天粒度
	)

	s.Require().NoError(err)
	s.Equal(3, len(results), "应该返回3个天聚合结果")

	s.T().Log("📊 1天聚合结果:")

	for i, result := range results {
		time1d := result["time_1d"]
		num1d := result["num_1d"]
		recordCount := result["record_count"]

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		s.T().Logf("   [%d] time_1d=%v, num_1d=%v, record_count=%d",
			i+1, time1d, num1d, count)

		s.Equal(int64(30), count, "每天应该有30条记录")
	}

	s.T().Log("✅ 测试通过")
}

// Test6_DecimalFieldAggregation 测试 Decimal 字段聚合
func (s *MinuteGranularityTestSuite) Test6_DecimalFieldAggregation() {
	s.T().Log("🧪 测试: Decimal 字段在10分钟粒度下的聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	decimalFieldsJSON, _ := json.Marshal([]string{"fee"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_decimal",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		DecimalFields:   string(decimalFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（10条，每条不同的 fee）
	baseTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 10; i++ {
		trade := &Trade{
			UserID: "U001",
			Symbol: "BTCUSDT",
			Fee:    decimal.NewFromFloat(1.12345678 * float64(i+1)),
		}
		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		s.Require().NoError(s.ch.Insert(trade))
	}

	time.Sleep(3 * time.Second)

	// 3. 查询10分钟粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_decimal",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-time.Hour),
		baseTime.Add(time.Hour),
		"10m", // 查询10分钟粒度
	)

	s.Require().NoError(err)
	s.NotEmpty(results)

	s.T().Log("📊 Decimal 字段聚合结果:")

	for i, result := range results {
		totalFee := result["total_fee"]
		avgFee := result["avg_fee"]
		maxFee := result["max_fee"]
		minFee := result["min_fee"]

		s.T().Logf("   [%d] total_fee=%v (类型:%T)", i+1, totalFee, totalFee)
		s.T().Logf("       avg_fee=%v (类型:%T)", avgFee, avgFee)
		s.T().Logf("       max_fee=%v (类型:%T)", maxFee, maxFee)
		s.T().Logf("       min_fee=%v (类型:%T)", minFee, minFee)

		// 验证类型
		_, ok1 := totalFee.(decimal.Decimal)
		_, ok2 := avgFee.(decimal.Decimal)
		_, ok3 := maxFee.(decimal.Decimal)
		_, ok4 := minFee.(decimal.Decimal)

		s.True(ok1, "total_fee 应该是 decimal.Decimal 类型")
		s.True(ok2, "avg_fee 应该是 decimal.Decimal 类型")
		s.True(ok3, "max_fee 应该是 decimal.Decimal 类型")
		s.True(ok4, "min_fee 应该是 decimal.Decimal 类型")
	}

	s.T().Log("✅ 测试通过")
}

// Test7_MultiDimensionFiltering 测试多维度过滤
// Test7_MultiDimensionFiltering 测试多维度过滤
func (s *MinuteGranularityTestSuite) Test7_MultiDimensionFiltering() {
	s.T().Log("🧪 测试: 多维度过滤（user_id + symbol）")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id", "symbol"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_multi_dim",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 🔧 等待视图创建完成
	time.Sleep(2 * time.Second)

	// 2. 插入多维度数据（使用唯一时间戳）
	baseTime := time.Date(2026, 1, 29, 10, 30, 0, 0, time.UTC) // 🔧 使用不同的基础时间
	testData := []struct {
		userID string
		symbol string
	}{
		{"U_TEST7_001", "BTCUSDT"}, // 🔧 使用测试专用用户ID
		{"U_TEST7_001", "ETHUSDT"},
		{"U_TEST7_002", "BTCUSDT"},
	}

	for i, data := range testData {
		trade := &Trade{
			UserID: data.userID,
			Symbol: data.symbol,
			Amount: 100.0 * float64(i+1), // 🔧 使用不同金额便于调试
		}
		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		s.Require().NoError(s.ch.Insert(trade))
		s.T().Logf("插入数据: user_id=%s, symbol=%s, time=%v",
			data.userID, data.symbol, trade.CreatedAt)
	}

	time.Sleep(3 * time.Second)

	// 3. 只查询 U_TEST7_001 + BTCUSDT
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_multi_dim",
		map[string]interface{}{
			"user_id": "U_TEST7_001",
			"symbol":  "BTCUSDT",
		},
		baseTime.Add(-time.Hour),
		baseTime.Add(time.Hour),
		"1m", // 查询1分钟粒度
	)

	s.Require().NoError(err)

	// 🔧 打印所有结果用于调试
	s.T().Logf("📊 查询结果数: %d", len(results))
	for i, result := range results {
		s.T().Logf("   [%d] user_id=%v, symbol=%v, total_amount=%v, time_1m=%v",
			i+1, result["user_id"], result["symbol"], result["total_amount"], result["time_1m"])
	}

	s.Equal(1, len(results), "应该只返回1条符合条件的数据")

	if len(results) > 0 {
		result := results[0]
		userID := result["user_id"]
		symbol := result["symbol"]

		s.Equal("U_TEST7_001", userID)
		s.Equal("BTCUSDT", symbol)

		s.T().Logf("✅ 过滤正确: user_id=%s, symbol=%s", userID, symbol)
	}

	s.T().Log("✅ 测试通过")
}

// Test8_QueryWeekGranularity 测试查询周粒度
func (s *MinuteGranularityTestSuite) Test8_QueryWeekGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询周聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1w",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         30,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越3周）
	baseTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC) // 周一

	for w := 0; w < 3; w++ {
		for i := 0; i < 50; i++ { // 每周50条数据
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = baseTime.Add(time.Duration(w*7)*24*time.Hour + time.Duration(i)*time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
	}

	time.Sleep(3 * time.Second)

	// 3. 查询周粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1w",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-7*24*time.Hour),
		baseTime.Add(4*7*24*time.Hour),
		"1w", // 查询周粒度
	)

	s.Require().NoError(err)
	s.Equal(3, len(results), "应该返回3个周聚合结果")

	s.T().Log("📊 周聚合结果:")

	totalRecords := int64(0)
	for i, result := range results {
		time1w := result["time_1w"]
		num1w := result["num_1w"]
		recordCount := result["record_count"]

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		totalRecords += count

		s.T().Logf("   [%d] time_1w=%v, num_1w=%v, record_count=%d",
			i+1, time1w, num1w, count)

		s.Equal(int64(50), count, "每周应该有50条记录")
	}

	s.Equal(int64(150), totalRecords, "总共应该有150条记录")
	s.T().Log("✅ 测试通过")
}

// Test9_QueryMonthGranularity 测试查询月粒度
func (s *MinuteGranularityTestSuite) Test9_QueryMonthGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询月聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1M",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         90,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越3个月）
	baseTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	for m := 0; m < 3; m++ {
		for i := 0; i < 60; i++ { // 每月60条数据
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			// 每月的第1天
			monthTime := baseTime.AddDate(0, m, 0)
			trade.CreatedAt = monthTime.Add(time.Duration(i) * time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
	}

	time.Sleep(3 * time.Second)

	// 3. 查询月粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1M",
		map[string]interface{}{"user_id": "U001"},
		baseTime.AddDate(0, -1, 0),
		baseTime.AddDate(0, 4, 0),
		"1M", // 查询月粒度
	)

	s.Require().NoError(err)
	s.Equal(3, len(results), "应该返回3个月聚合结果")

	s.T().Log("📊 月聚合结果:")

	for i, result := range results {
		time1M := result["time_1M"]
		num1M := result["num_1M"]
		recordCount := result["record_count"]

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		s.T().Logf("   [%d] time_1M=%v, num_1M=%v, record_count=%d",
			i+1, time1M, num1M, count)

		s.Equal(int64(60), count, "每月应该有60条记录")
	}

	s.T().Log("✅ 测试通过")
}

// Test10_QueryQuarterGranularity 测试查询季度粒度
func (s *MinuteGranularityTestSuite) Test10_QueryQuarterGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询季度聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1q",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         365,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越4个季度）
	baseTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC) // Q1

	quarters := []struct {
		startMonth int
		records    int
	}{
		{1, 100},  // Q1: 1-3月
		{4, 120},  // Q2: 4-6月
		{7, 140},  // Q3: 7-9月
		{10, 160}, // Q4: 10-12月
	}

	for qi, q := range quarters {
		quarterTime := time.Date(2026, time.Month(q.startMonth), 1, 10, 0, 0, 0, time.UTC)
		for i := 0; i < q.records; i++ {
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = quarterTime.Add(time.Duration(i) * time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
		s.T().Logf("插入 Q%d 数据: %d 条", qi+1, q.records)
	}

	time.Sleep(3 * time.Second)

	// 3. 查询季度粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1q",
		map[string]interface{}{"user_id": "U001"},
		baseTime.AddDate(-1, 0, 0),
		baseTime.AddDate(1, 0, 0),
		"1q", // 查询季度粒度
	)

	s.Require().NoError(err)
	s.Equal(4, len(results), "应该返回4个季度聚合结果")

	s.T().Log("📊 季度聚合结果:")

	// 🔧 修复：结果按 DESC 排序，所以期望值应该是 Q4→Q3→Q2→Q1
	expectedCounts := []int64{160, 140, 120, 100} // 降序：Q4, Q3, Q2, Q1
	expectedQuarters := []int{4, 3, 2, 1}         // 降序

	for i, result := range results {
		time1q := result["time_1q"]
		num1q := result["num_1q"]
		recordCount := result["record_count"]

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		var quarter int
		switch v := num1q.(type) {
		case int:
			quarter = v
		case int32:
			quarter = int(v)
		case int64:
			quarter = int(v)
		case uint8:
			quarter = int(v)
		}

		s.T().Logf("   [%d] time_1q=%v, num_1q=%v, record_count=%d",
			i+1, time1q, quarter, count)

		s.Equal(expectedQuarters[i], quarter, fmt.Sprintf("索引 %d 应该是 Q%d", i, expectedQuarters[i]))
		s.Equal(expectedCounts[i], count, fmt.Sprintf("Q%d 应该有 %d 条记录", quarter, expectedCounts[i]))
	}

	s.T().Log("✅ 测试通过")
}

// Test11_QueryYearGranularity 测试查询年粒度
func (s *MinuteGranularityTestSuite) Test11_QueryYearGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询年聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_1y",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         730, // 2年
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（只测试2年，避免跨年边界问题）
	years := []struct {
		year    int
		records int
	}{
		{2025, 300},
		{2026, 400},
	}

	for _, y := range years {
		yearTime := time.Date(y.year, 6, 15, 10, 0, 0, 0, time.UTC) // 使用年中时间，避免边界问题
		for i := 0; i < y.records; i++ {
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = yearTime.Add(time.Duration(i) * time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
		s.T().Logf("插入 %d 年数据: %d 条", y.year, y.records)
	}

	time.Sleep(2 * time.Second) // 减少等待时间

	// 3. 查询年粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_1y",
		map[string]interface{}{"user_id": "U001"},
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		"1y", // 查询年粒度
	)

	s.Require().NoError(err)
	s.Equal(2, len(results), "应该返回2个年聚合结果") // 修改期望值

	s.T().Log("📊 年聚合结果:")

	expectedCounts := []int64{400, 300} // 按降序排列（ORDER BY DESC）
	expectedYears := []int{2026, 2025}

	for i, result := range results {
		time1y := result["time_1y"]
		num1y := result["num_1y"]
		recordCount := result["record_count"]

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		var year int
		switch v := num1y.(type) {
		case int:
			year = v
		case int32:
			year = int(v)
		case int64:
			year = int(v)
		case uint16:
			year = int(v)
		}

		s.T().Logf("   [%d] time_1y=%v, num_1y=%v, record_count=%d",
			i+1, time1y, num1y, count)

		s.Equal(expectedYears[i], year, fmt.Sprintf("索引 %d 应该是年份 %d", i, expectedYears[i]))
		s.Equal(expectedCounts[i], count, fmt.Sprintf("年份 %d 应该有 %d 条记录", year, expectedCounts[i]))
	}

	s.T().Log("✅ 测试通过")
}

// Test12_Query8HourGranularity 测试查询8小时粒度
func (s *MinuteGranularityTestSuite) Test12_Query8HourGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询8小时聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_8h",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         7,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越3个8小时区间）
	// 00:00-07:59 (num_8h=0), 08:00-15:59 (num_8h=1), 16:00-23:59 (num_8h=2)
	baseTime := time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)

	for h8 := 0; h8 < 3; h8++ {
		for i := 0; i < 40; i++ { // 每个8小时区间40条数据
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = baseTime.Add(time.Duration(h8*8)*time.Hour + time.Duration(i)*time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
	}

	time.Sleep(3 * time.Second)

	// 3. 查询8小时粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_8h",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-24*time.Hour),
		baseTime.Add(48*time.Hour),
		"8h", // 查询8小时粒度
	)

	s.Require().NoError(err)
	s.Equal(3, len(results), "应该返回3个8小时聚合结果")

	s.T().Log("📊 8小时聚合结果:")

	aggregated := make(map[int]int64)
	for i, result := range results {
		time8h := result["time_8h"]
		num8h := result["num_8h"]
		recordCount := result["record_count"]

		var numKey int
		switch v := num8h.(type) {
		case int:
			numKey = v
		case int32:
			numKey = int(v)
		case int64:
			numKey = int(v)
		case uint8:
			numKey = int(v)
		}

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		aggregated[numKey] = count

		s.T().Logf("   [%d] time_8h=%v, num_8h=%d, record_count=%d",
			i+1, time8h, numKey, count)
	}

	// 验证聚合结果
	s.Equal(int64(40), aggregated[0], "num_8h=0 (00:00-07:59) 应该有40条记录")
	s.Equal(int64(40), aggregated[1], "num_8h=1 (08:00-15:59) 应该有40条记录")
	s.Equal(int64(40), aggregated[2], "num_8h=2 (16:00-23:59) 应该有40条记录")

	s.T().Log("✅ 测试通过")
}

// Test13_Query12HourGranularity 测试查询12小时粒度
func (s *MinuteGranularityTestSuite) Test13_Query12HourGranularity() {
	s.T().Log("🧪 测试: 从分钟级视图查询12小时聚合")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_12h",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         7,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据（跨越2个12小时区间）
	// 00:00-11:59 (num_12h=0), 12:00-23:59 (num_12h=1)
	baseTime := time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)

	for h12 := 0; h12 < 2; h12++ {
		for i := 0; i < 50; i++ { // 每个12小时区间50条数据
			trade := &Trade{
				UserID: "U001",
				Symbol: "BTCUSDT",
				Amount: 100.0,
			}
			trade.CreatedAt = baseTime.Add(time.Duration(h12*12)*time.Hour + time.Duration(i)*time.Minute)
			trade.UpdatedAt = trade.CreatedAt
			s.Require().NoError(s.ch.Insert(trade))
		}
	}

	time.Sleep(3 * time.Second)

	// 3. 查询12小时粒度
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_12h",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-24*time.Hour),
		baseTime.Add(48*time.Hour),
		"12h", // 查询12小时粒度
	)

	s.Require().NoError(err)
	s.Equal(2, len(results), "应该返回2个12小时聚合结果")

	s.T().Log("📊 12小时聚合结果:")

	aggregated := make(map[int]int64)
	for i, result := range results {
		time12h := result["time_12h"]
		num12h := result["num_12h"]
		recordCount := result["record_count"]

		var numKey int
		switch v := num12h.(type) {
		case int:
			numKey = v
		case int32:
			numKey = int(v)
		case int64:
			numKey = int(v)
		case uint8:
			numKey = int(v)
		}

		var count int64
		switch v := recordCount.(type) {
		case int:
			count = int64(v)
		case int32:
			count = int64(v)
		case int64:
			count = v
		case uint64:
			count = int64(v)
		}

		aggregated[numKey] = count

		s.T().Logf("   [%d] time_12h=%v, num_12h=%d, record_count=%d",
			i+1, time12h, numKey, count)
	}

	// 验证聚合结果
	s.Equal(int64(50), aggregated[0], "num_12h=0 (00:00-11:59) 应该有50条记录")
	s.Equal(int64(50), aggregated[1], "num_12h=1 (12:00-23:59) 应该有50条记录")

	s.T().Log("✅ 测试通过")
}

// Test14_MixedGranularityQuery 测试混合粒度查询（验证所有时间列同时存在）
func (s *MinuteGranularityTestSuite) Test14_MixedGranularityQuery() {
	s.T().Log("🧪 测试: 验证单次查询返回所有时间粒度列")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_mixed",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         365,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入测试数据
	baseTime := time.Date(2026, 1, 29, 10, 15, 0, 0, time.UTC)

	trade := &Trade{
		UserID: "U001",
		Symbol: "BTCUSDT",
		Amount: 1000.0,
	}
	trade.CreatedAt = baseTime
	trade.UpdatedAt = baseTime
	s.Require().NoError(s.ch.Insert(trade))

	time.Sleep(3 * time.Second)

	// 3. 查询1分钟粒度（但应该返回所有时间粒度的列）
	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_mixed",
		map[string]interface{}{"user_id": "U001"},
		baseTime.Add(-time.Hour),
		baseTime.Add(time.Hour),
	)

	s.Require().NoError(err)
	s.Equal(1, len(results), "应该返回1条数据")

	s.T().Log("📊 验证所有时间粒度列:")

	result := results[0]

	// 定义所有应该存在的时间列
	expectedColumns := []struct {
		timeCol   string
		numberCol string
	}{
		{"time_1m", "num_1m"},
		{"time_10m", "num_10m"},
		{"time_30m", "num_30m"},
		{"time_1h", "num_1h"},
		{"time_8h", "num_8h"},
		{"time_12h", "num_12h"},
		{"time_1d", "num_1d"},
		{"time_1w", "num_1w"},
		{"time_1M", "num_1M"},
		{"time_1q", "num_1q"},
		{"time_1y", "num_1y"},
	}

	for _, col := range expectedColumns {
		timeVal, timeExists := result[col.timeCol]
		numVal, numExists := result[col.numberCol]

		s.True(timeExists, fmt.Sprintf("应该包含时间列: %s", col.timeCol))
		s.True(numExists, fmt.Sprintf("应该包含编号列: %s", col.numberCol))

		if timeExists && numExists {
			s.T().Logf("   ✅ %s=%v, %s=%v", col.timeCol, timeVal, col.numberCol, numVal)
		}
	}

	s.T().Log("✅ 测试通过: 所有11个时间粒度列都存在")
}

// Test15_EmptyResultQuery 测试空结果查询
func (s *MinuteGranularityTestSuite) Test15_EmptyResultQuery() {
	s.T().Log("🧪 测试: 查询不存在的时间范围（空结果）")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_empty",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入数据到特定时间
	dataTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)

	trade := &Trade{
		UserID: "U001",
		Symbol: "BTCUSDT",
		Amount: 100.0,
	}
	trade.CreatedAt = dataTime
	trade.UpdatedAt = dataTime
	s.Require().NoError(s.ch.Insert(trade))

	time.Sleep(3 * time.Second)

	// 3. 查询不存在的时间范围（1天前）
	queryStart := dataTime.Add(-25 * time.Hour)
	queryEnd := dataTime.Add(-24 * time.Hour)

	results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
		"trades_empty",
		map[string]interface{}{"user_id": "U001"},
		queryStart,
		queryEnd,
	)

	s.Require().NoError(err)
	s.Empty(results, "应该返回空结果")

	s.T().Logf("📊 查询时间范围: %v ~ %v", queryStart, queryEnd)
	s.T().Logf("📊 数据时间: %v", dataTime)
	s.T().Logf("📊 返回结果数: %d", len(results))

	s.T().Log("✅ 测试通过: 空结果处理正确")
}

// Test16_CrossGranularityComparison 测试跨粒度数据一致性
func (s *MinuteGranularityTestSuite) Test16_CrossGranularityComparison() {
	s.T().Log("🧪 测试: 跨粒度数据一致性验证")

	// 1. 创建源表和分钟级视图
	s.Require().NoError(s.ch.CreateTable(&Trade{}))

	dimensionsJSON, _ := json.Marshal([]string{"user_id"})
	numericFieldsJSON, _ := json.Marshal([]string{"amount"})

	config := &BusinessDimensionConfig{
		ViewName:        "trades_consistency",
		SourceTableName: "trades",
		Dimensions:      string(dimensionsJSON),
		TimeGranularity: "minute",
		NumericFields:   string(numericFieldsJSON),
		TTLDays:         1,
	}

	s.Require().NoError(s.ch.SaveBusinessViewConfig(config))
	s.Require().NoError(s.ch.CreateBusinessViewFromConfig(config))

	// 2. 插入60条数据（1小时）
	baseTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)
	totalAmount := 0.0

	for i := 0; i < 60; i++ {
		amount := 100.0 * float64(i+1)
		totalAmount += amount

		trade := &Trade{
			UserID: "U001",
			Symbol: "BTCUSDT",
			Amount: amount,
		}
		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		s.Require().NoError(s.ch.Insert(trade))
	}

	time.Sleep(3 * time.Second)

	s.T().Logf("插入了60条数据，总金额: %.2f", totalAmount)

	// 3. 分别查询不同粒度，验证总金额一致
	granularities := []GranularityTimeType{"1m", "10m", "30m", "1h"}

	for _, granularity := range granularities {
		results, err := s.ch.QueryBusinessStatsWithTimeAggregations(
			"trades_consistency",
			map[string]interface{}{"user_id": "U001"},
			baseTime.Add(-time.Hour),
			baseTime.Add(2*time.Hour),
			granularity,
		)

		s.Require().NoError(err)
		s.NotEmpty(results)

		// 累加所有记录的 total_amount
		sumAmount := 0.0
		for _, result := range results {
			if amt, ok := result["total_amount"]; ok {
				switch v := amt.(type) {
				case float64:
					sumAmount += v
				case float32:
					sumAmount += float64(v)
				}
			}
		}

		s.T().Logf("粒度 %s: 返回 %d 条记录, 总金额=%.2f", granularity, len(results), sumAmount)

		// 验证总金额一致（允许浮点误差）
		s.InDelta(totalAmount, sumAmount, 0.01, fmt.Sprintf("粒度 %s 的总金额应该一致", granularity))
	}

	s.T().Log("✅ 测试通过: 跨粒度数据一致性验证通过")
}

// ==================== 运行测试套件 ====================

func TestMinuteGranularitySuite(t *testing.T) {
	suite.Run(t, new(MinuteGranularityTestSuite))
}
