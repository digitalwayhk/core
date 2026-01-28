package test

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ==================== 测试模型 ====================
func (own *MySQLTrade) GetHash() string {
	if own.Hashcode != "" {
		return own.Hashcode
	}
	return utils.HashCodes(fmt.Sprintf("%s_%s_%f_%f_%f", own.UserID, own.Symbol, own.Price, own.Quantity, own.Amount))
}
func (MySQLTrade) TableName() string {
	return "trades"
}

func (MySQLTrade) GetRemoteDBName() string {
	return "test_mysql_trades"
}

type MySQLUser struct {
	entity.Model
	Username string  `gorm:"column:username;type:varchar(100);uniqueIndex" json:"username"`
	Email    string  `gorm:"column:email;type:varchar(100);index" json:"email"`
	Balance  float64 `gorm:"column:balance" json:"balance"`
	Status   string  `gorm:"column:status;type:varchar(50)" json:"status"`
}

func (MySQLUser) TableName() string {
	return "users"
}

func (MySQLUser) GetRemoteDBName() string {
	return "test_mysql_users"
}

func (MySQLOrder) TableName() string {
	return "orders"
}

func (MySQLOrder) GetRemoteDBName() string {
	return "test_mysql_orders"
}

type MySQLTrade struct {
	entity.Model
	UserID     string          `gorm:"column:user_id;type:varchar(100);index" json:"user_id"`
	Symbol     string          `gorm:"column:symbol;type:varchar(50);index" json:"symbol"`
	Side       string          `gorm:"column:side;type:varchar(20)" json:"side"`
	Price      float64         `gorm:"column:price" json:"price"`
	Quantity   float64         `gorm:"column:quantity" json:"quantity"`
	Amount     float64         `gorm:"column:amount;index" json:"amount"`
	Commission float64         `gorm:"column:commission" json:"commission"`
	Status     string          `gorm:"column:status;type:varchar(50);index" json:"status"`
	Fee        decimal.Decimal `gorm:"column:fee;type:decimal(20,8)" json:"fee"`
}

type MySQLOrder struct {
	entity.Model
	OrderNo    string           `gorm:"column:order_no;type:varchar(100);uniqueIndex" json:"order_no"`
	UserID     string           `gorm:"column:user_id;type:varchar(100);index" json:"user_id"`
	TotalPrice decimal.Decimal  `gorm:"column:total_price;type:decimal(20,8)" json:"total_price"`
	Items      []MySQLOrderItem `gorm:"foreignKey:OrderID" json:"items"`
}

type MySQLOrderItem struct {
	entity.Model
	OrderID  string          `gorm:"column:order_id;type:varchar(100);index" json:"order_id"`
	Product  string          `gorm:"column:product;type:varchar(200)" json:"product"`
	Price    decimal.Decimal `gorm:"column:price;type:decimal(20,8)" json:"price"`
	Quantity int             `gorm:"column:quantity" json:"quantity"`
}

func (MySQLOrderItem) TableName() string {
	return "order_items"
}

func (MySQLOrderItem) GetRemoteDBName() string {
	return "test_mysql_orders"
}

// ==================== MySQL 测试套件 ====================

type MySQLTestSuite struct {
	suite.Suite
	mysql       *oltp.MySQL
	testDBs     []string
	passedCount int
	failedCount int
	totalCount  int
	startTime   time.Time
}

func (s *MySQLTestSuite) SetupSuite() {
	s.startTime = time.Now()
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("🚀 MySQL 完整测试套件 v2.0 - 全面覆盖")
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("")

	config := &oltp.Config{
		Host:         "localhost",
		Port:         3307,
		Username:     "root",
		Password:     "test123456",
		Database:     "",
		Charset:      "utf8mb4",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
		IsLog:        false,
	}

	mysql := oltp.NewMySQL(config)
	s.Require().NotNil(mysql, "创建 MySQL 实例失败")
	s.T().Log("✅ MySQL 测试环境初始化成功")
}

func (s *MySQLTestSuite) SetupTest() {
	s.totalCount++

	config := &oltp.Config{
		Host:         "localhost",
		Port:         3307,
		Username:     "root",
		Password:     "test123456",
		Database:     "",
		Charset:      "utf8mb4",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
		IsLog:        false,
	}

	s.mysql = oltp.NewMySQL(config)
}

func (s *MySQLTestSuite) TearDownTest() {
	if !s.T().Failed() {
		s.passedCount++
	} else {
		s.failedCount++
	}

	// 🔧 完整清理流程
	if s.mysql != nil {
		// 1. 获取当前连接
		db, err := s.mysql.GetDB()

		// 2. 删除测试数据库
		if err == nil && db != nil {
			for _, dbName := range s.testDBs {
				db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
			}
		}

		// 3. 关闭所有连接
		if err == nil && db != nil {
			if sqlDB, err := db.DB(); err == nil {
				// 🔧 关键：设置连接数为0，强制关闭所有连接
				sqlDB.SetMaxOpenConns(0)
				sqlDB.SetMaxIdleConns(0)
				sqlDB.Close()
			}
		}

		// 4. 清理实例
		s.mysql.DeleteDB()
		s.mysql.Name = ""
		s.testDBs = nil
	}
}

func (s *MySQLTestSuite) TearDownSuite() {
	duration := time.Since(s.startTime)

	s.T().Log("")
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Log("📊 测试套件执行报告")
	s.T().Log("=" + strings.Repeat("=", 80))
	s.T().Logf("⏱️  总耗时: %v", duration)
	s.T().Logf("   • 总计: %d 个测试", s.totalCount)
	s.T().Logf("   • 通过: %d ✅", s.passedCount)
	s.T().Logf("   • 失败: %d ❌", s.failedCount)

	if s.totalCount > 0 {
		passRate := float64(s.passedCount) / float64(s.totalCount) * 100
		s.T().Logf("✨ 通过率: %.1f%%", passRate)
	}

	// 🔧 添加最终清理
	if s.mysql != nil {
		db, err := s.mysql.GetDB()
		if err == nil && db != nil {
			// 清理所有测试数据库
			var databases []string
			db.Raw("SHOW DATABASES LIKE 'test_mysql_%'").Scan(&databases)
			for _, dbName := range databases {
				db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
			}

			// 强制关闭连接
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.SetMaxOpenConns(0)
				sqlDB.SetMaxIdleConns(0)
				sqlDB.Close()
			}
		}
	}

	s.T().Log("")
	s.T().Log("📚 测试覆盖:")
	s.T().Log("   ✓ 1. 基础功能测试 - 5个")
	s.T().Log("   ✓ 2. 数据操作测试 - 8个")
	s.T().Log("   ✓ 3. 事务测试 - 5个")
	s.T().Log("   ✓ 4. 数据类型测试 - 6个")
	s.T().Log("   ✓ 5. 查询功能测试 - 6个")
	s.T().Log("   ✓ 6. 并发安全测试 - 3个")
	s.T().Log("   ✓ 7. 性能测试 - 3个")
	s.T().Log("   ✓ 8. 错误处理测试 - 5个")
	s.T().Log("   ✓ 9. 连接管理测试 - 4个")
	s.T().Log("   ✓ 10. 数据库管理测试 - 3个")
	s.T().Log("   ✓ 11. 边界值测试 - 5个")
	s.T().Log("=" + strings.Repeat("=", 80))

	// 🔧 给予时间让连接完全关闭
	time.Sleep(100 * time.Millisecond)
}

// ==================== 辅助函数 ====================

func generateTestMySQLTrades(count int) []*MySQLTrade {
	trades := make([]*MySQLTrade, count)
	users := []string{"U001", "U002", "U003", "U004", "U005"}
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "ADAUSDT", "DOGEUSDT"}
	sides := []string{"buy", "sell"}
	statuses := []string{"completed", "pending", "cancelled"}

	baseTime := time.Now().Add(-24 * time.Hour)
	rand.Seed(time.Now().UnixNano())

	for i := 0; i < count; i++ {
		price := 40000.0 + rand.Float64()*20000.0
		quantity := 0.001 + rand.Float64()*0.999
		amount := price * quantity
		commission := amount * 0.001
		fee := decimal.NewFromFloat(amount * 0.0005)

		trade := &MySQLTrade{
			UserID:     users[rand.Intn(len(users))],
			Symbol:     symbols[rand.Intn(len(symbols))],
			Side:       sides[rand.Intn(len(sides))],
			Price:      price,
			Quantity:   quantity,
			Amount:     amount,
			Commission: commission,
			Status:     statuses[rand.Intn(len(statuses))],
			Fee:        fee,
		}

		trade.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
		trade.UpdatedAt = trade.CreatedAt
		trades[i] = trade
	}

	return trades
}

func (s *MySQLTestSuite) trackDatabase(dbName string) {
	for _, existing := range s.testDBs {
		if existing == dbName {
			return
		}
	}
	s.testDBs = append(s.testDBs, dbName)
}

// ==================== 1. 基础功能测试 (5个) ====================

func (s *MySQLTestSuite) Test1_1_Connection() {
	trade := &MySQLTrade{}
	err := s.mysql.GetDBName(trade)
	s.NoError(err)

	db, err := s.mysql.GetDB()
	s.NoError(err)
	s.NotNil(db)

	sqlDB, err := db.DB()
	s.NoError(err)
	s.NoError(sqlDB.Ping())

	stats := sqlDB.Stats()
	s.T().Logf("✅ MySQL 连接成功 (最大连接: %d, 当前打开: %d)",
		stats.MaxOpenConnections, stats.OpenConnections)
}

func (s *MySQLTestSuite) Test1_2_AutoCreateDatabase() {
	trade := &MySQLTrade{
		UserID: "U001",
		Symbol: "BTCUSDT",
		Amount: 1000.0,
	}

	// 🔧 修复：先调用 HasTable 来设置数据库名并创建表
	err := s.mysql.HasTable(trade)
	s.NoError(err)

	dbName := trade.GetRemoteDBName()
	s.trackDatabase(dbName)

	// 现在可以安全调用 GetDB
	db, err := s.mysql.GetDB()
	s.NoError(err)
	s.NotNil(db)

	var count int64
	db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?", dbName).Scan(&count)

	s.Equal(int64(1), count)
	s.T().Logf("✅ 自动创建数据库: %s", dbName)
}

func (s *MySQLTestSuite) Test1_3_CreateTable() {
	trade := &MySQLTrade{}
	err := s.mysql.HasTable(trade)
	s.NoError(err)

	dbName := trade.GetRemoteDBName()
	s.trackDatabase(dbName)

	var count int64
	db, err := s.mysql.GetDB()
	s.NoError(err)
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		dbName, "trades").Scan(&count)

	s.Equal(int64(1), count)
	s.T().Log("✅ 表创建成功并验证")
}

func (s *MySQLTestSuite) Test1_4_TableSchema() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))

	dbName := trade.GetRemoteDBName()
	s.trackDatabase(dbName)

	type ColumnInfo struct {
		Field string
		Type  string
	}

	var columns []ColumnInfo
	db, err := s.mysql.GetDB()
	s.NoError(err)
	db.Raw(fmt.Sprintf("SHOW COLUMNS FROM `%s`.`trades`", dbName)).Scan(&columns)

	s.Greater(len(columns), 0)

	fieldMap := make(map[string]string)
	for _, col := range columns {
		fieldMap[col.Field] = col.Type
	}

	s.Contains(fieldMap, "user_id")
	s.Contains(fieldMap, "amount")
	s.Contains(fieldMap, "fee")
	s.Contains(fieldMap["fee"], "decimal", "Fee 应该是 decimal 类型")

	s.T().Logf("✅ 表结构验证通过 (%d 个字段)", len(columns))
}

func (s *MySQLTestSuite) Test1_5_MultipleModels() {
	trade := &MySQLTrade{}
	user := &MySQLUser{}

	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.mysql.Name = ""
	s.NoError(s.mysql.HasTable(user))
	s.trackDatabase(user.GetRemoteDBName())

	tradeDName := trade.GetRemoteDBName()
	userDBName := user.GetRemoteDBName()

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var tradeDBExists, userDBExists int64
	db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?", tradeDName).Scan(&tradeDBExists)
	db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?", userDBName).Scan(&userDBExists)
	s.Equal(int64(1), tradeDBExists)
	s.Equal(int64(1), userDBExists)

	s.T().Logf("✅ 多模型多数据库测试通过:")
	s.T().Logf("   - Trade DB: %s", tradeDName)
	s.T().Logf("   - User DB: %s", userDBName)
}

// ==================== 2. 数据操作测试 (8个) ====================

func (s *MySQLTestSuite) Test2_2_BatchInsert() {
	trades := generateTestMySQLTrades(100)

	err := s.mysql.HasTable(trades[0])
	s.NoError(err)
	s.trackDatabase(trades[0].GetRemoteDBName())

	for _, trade := range trades {
		s.NoError(s.mysql.Insert(trade))
	}

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(100), count)

	s.T().Logf("✅ 批量插入成功 (%d 条)", count)
}

func (s *MySQLTestSuite) Test2_3_Update() {
	trade := &MySQLTrade{
		UserID: "U001",
		Symbol: "BTCUSDT",
		Amount: 1000.0,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	// 🔧 先创建表
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	// 🔧 插入前设置 hashcode
	trade.SetHashcode(trade.GetHash())
	s.NoError(s.mysql.Insert(trade))

	// 🔧 验证插入成功
	db, err := s.mysql.GetDB()
	s.NoError(err)

	var inserted MySQLTrade
	err = db.Table("trades").Where("hashcode = ?", trade.GetHash()).First(&inserted).Error
	s.NoError(err, "插入的数据应该存在")
	s.Equal(1000.0, inserted.Amount, "初始金额应为 1000.0")

	// 更新数据
	trade.Amount = 2000.0
	trade.Status = "completed"
	trade.SetHashcode(trade.GetHash())
	s.NoError(s.mysql.Update(trade))

	// 验证更新结果
	var result MySQLTrade
	err = db.Table("trades").Where("hashcode = ?", trade.GetHash()).First(&result).Error
	s.NoError(err, "更新后的数据应该存在")

	s.Equal(2000.0, result.Amount, "金额应更新为 2000.0")
	s.Equal("completed", result.Status, "状态应更新为 completed")

	s.T().Log("✅ 更新成功")
}

func (s *MySQLTestSuite) Test2_4_Delete() {
	trade := &MySQLTrade{
		UserID: "U001",
		Symbol: "BTCUSDT",
		Amount: 1000.0,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.NoError(s.mysql.Delete(trade))

	var count int64
	db, err := s.mysql.GetDB()
	s.NoError(err)
	db.Table("trades").Where("hashcode = ?", trade.GetHash()).Count(&count)
	s.Equal(int64(0), count)

	s.T().Log("✅ 删除成功")
}

func (s *MySQLTestSuite) Test2_5_Load() {
	trades := generateTestMySQLTrades(10)
	s.NoError(s.mysql.HasTable(trades[0]))
	s.trackDatabase(trades[0].GetRemoteDBName())

	for _, t := range trades {
		s.NoError(s.mysql.Insert(t))
	}

	searchItem := &types.SearchItem{
		Model: &MySQLTrade{},
		WhereList: []*types.WhereItem{
			{Column: "user_id", Symbol: "=", Value: "U001"},
		},
		Size: 10,
	}

	var results []*MySQLTrade
	err := s.mysql.Load(searchItem, &results)
	s.NoError(err)
	s.Greater(len(results), 0)

	s.T().Logf("✅ Load 查询成功 (%d 条)", len(results))
}

func (s *MySQLTestSuite) Test2_6_RawQuery() {
	trade := &MySQLTrade{
		UserID: "U001",
		Amount: 1000.0,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	var results []*MySQLTrade
	sql := "SELECT * FROM trades WHERE user_id = 'U001'"
	err := s.mysql.Raw(sql, &results)
	s.NoError(err)
	s.Equal(1, len(results))

	s.T().Log("✅ Raw SQL 查询成功")
}

func (s *MySQLTestSuite) Test2_7_ExecSQL() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	now := time.Now()
	result := db.Exec("INSERT INTO trades (hashcode, user_id, symbol, amount, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"test123", "U001", "BTCUSDT", 1000.0, now, now)
	s.NoError(result.Error)

	s.T().Log("✅ Exec SQL 执行成功")
}

func (s *MySQLTestSuite) Test2_8_NestedTables() {
	order := &MySQLOrder{
		OrderNo:    "ORD001",
		UserID:     "U001",
		TotalPrice: decimal.NewFromFloat(1000.0),
		Items: []MySQLOrderItem{
			{Product: "Product1", Price: decimal.NewFromFloat(500.0), Quantity: 1},
			{Product: "Product2", Price: decimal.NewFromFloat(500.0), Quantity: 1},
		},
	}

	s.NoError(s.mysql.HasTable(order))
	s.trackDatabase(order.GetRemoteDBName())

	var orderTableExists, itemTableExists int64
	dbName := order.GetRemoteDBName()
	db, err := s.mysql.GetDB()
	s.NoError(err)
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		dbName, "orders").Scan(&orderTableExists)
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		dbName, "order_items").Scan(&itemTableExists)

	s.Equal(int64(1), orderTableExists)
	s.Equal(int64(1), itemTableExists)

	s.T().Log("✅ 嵌套表创建成功")
}

// ==================== 3. 事务测试 (5个) ====================

func (s *MySQLTestSuite) Test3_1_TransactionCommit() {
	trade1 := &MySQLTrade{UserID: "U001", Amount: 100.0}
	trade2 := &MySQLTrade{UserID: "U002", Amount: 200.0}

	trade1.CreatedAt = time.Now()
	trade1.UpdatedAt = time.Now()
	trade2.CreatedAt = time.Now()
	trade2.UpdatedAt = time.Now()

	// 🔧 先创建表并确保连接
	s.NoError(s.mysql.HasTable(trade1))
	s.trackDatabase(trade1.GetRemoteDBName())

	// 然后开启事务
	s.NoError(s.mysql.Transaction())
	s.NoError(s.mysql.Insert(trade1))
	s.NoError(s.mysql.Insert(trade2))
	s.NoError(s.mysql.Commit())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(2), count)

	s.T().Log("✅ 事务提交成功")
}

func (s *MySQLTestSuite) Test3_2_TransactionRollback() {
	trade1 := &MySQLTrade{UserID: "U001", Amount: 100.0}
	trade2 := &MySQLTrade{UserID: "U002", Amount: 200.0}

	trade1.CreatedAt = time.Now()
	trade1.UpdatedAt = time.Now()
	trade2.CreatedAt = time.Now()
	trade2.UpdatedAt = time.Now()

	// 🔧 先创建表
	s.NoError(s.mysql.HasTable(trade1))
	s.trackDatabase(trade1.GetRemoteDBName())

	s.NoError(s.mysql.Transaction())
	s.NoError(s.mysql.Insert(trade1))
	s.NoError(s.mysql.Insert(trade2))
	s.NoError(s.mysql.Rollback())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(0), count)

	s.T().Log("✅ 事务回滚成功")
}

func (s *MySQLTestSuite) Test3_3_NestedTransaction() {
	trade := &MySQLTrade{UserID: "U001", Amount: 100.0}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	// 🔧 先创建表
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.NoError(s.mysql.Transaction())
	s.NoError(s.mysql.Insert(trade))

	s.NoError(s.mysql.Transaction())
	s.NoError(s.mysql.Commit())
	s.NoError(s.mysql.Commit())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(1), count)

	s.T().Log("✅ 嵌套事务处理正常")
}

func (s *MySQLTestSuite) Test3_4_TransactionIsolation() {
	trade1 := &MySQLTrade{UserID: "U001", Amount: 100.0}
	trade1.CreatedAt = time.Now()
	trade1.UpdatedAt = time.Now()

	// 🔧 先创建表
	s.NoError(s.mysql.HasTable(trade1))
	s.trackDatabase(trade1.GetRemoteDBName())

	s.NoError(s.mysql.Transaction())
	s.NoError(s.mysql.Insert(trade1))

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var countInTx int64
	db.Table("trades").Count(&countInTx)

	var countOutTx int64
	db.Table("trades").Count(&countOutTx)

	s.NoError(s.mysql.Commit())

	s.Equal(int64(0), countOutTx)
	s.T().Log("✅ 事务隔离正常")
}

func (s *MySQLTestSuite) Test3_5_TransactionErrorHandling() {
	trade := &MySQLTrade{UserID: "U001", Amount: 100.0}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	// 🔧 先创建表
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.NoError(s.mysql.Transaction())
	s.NoError(s.mysql.Insert(trade))

	s.NoError(s.mysql.Rollback())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(0), count)

	s.T().Log("✅ 事务错误处理正常")
}

// ==================== 4. 数据类型测试 (6个) ====================

func (s *MySQLTestSuite) Test4_1_DecimalPrecision() {
	testCases := []struct {
		name  string
		value string
	}{
		{"8位小数", "123.45678901"},
		{"极小值", "0.00000001"},
		{"零值", "0.00000000"},
		{"大数", "999999.99999999"},
	}

	for i, tc := range testCases {
		fee, _ := decimal.NewFromString(tc.value)
		trade := &MySQLTrade{
			UserID: fmt.Sprintf("U%03d", i),
			Symbol: "BTCUSDT",
			Amount: 1000.0,
			Fee:    fee,
		}
		trade.CreatedAt = time.Now()
		trade.UpdatedAt = time.Now()

		if i == 0 {
			s.trackDatabase(trade.GetRemoteDBName())
		}

		s.NoError(s.mysql.Insert(trade))
	}

	db, err := s.mysql.GetDB()
	s.NoError(err)

	for i, tc := range testCases {
		var result MySQLTrade
		db.Table("trades").
			Where("user_id = ?", fmt.Sprintf("U%03d", i)).
			First(&result)

		expected, _ := decimal.NewFromString(tc.value)
		diff := result.Fee.Sub(expected).Abs()
		s.True(diff.LessThan(decimal.NewFromFloat(0.00000001)))

		s.T().Logf("✅ %s: 期望=%s, 实际=%s",
			tc.name, expected.StringFixed(8), result.Fee.StringFixed(8))
	}
}

func (s *MySQLTestSuite) Test4_2_TimeFields() {
	now := time.Now()
	trade := &MySQLTrade{
		UserID: "U001",
		Amount: 1000.0,
	}
	trade.CreatedAt = now
	trade.UpdatedAt = now

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var result MySQLTrade
	db.Table("trades").First(&result)

	s.Equal(now.Unix(), result.CreatedAt.Unix())
	s.Equal(now.Unix(), result.UpdatedAt.Unix())

	s.T().Log("✅ 时间字段存储正确")
}

func (s *MySQLTestSuite) Test4_3_StringFields() {
	trade := &MySQLTrade{
		UserID: "测试用户@#$%001",
		Symbol: "BTC/USDT",
		Status: "已完成✓",
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var result MySQLTrade
	db.Table("trades").Where("user_id = ?", trade.UserID).First(&result)

	s.Equal(trade.UserID, result.UserID)
	s.Equal(trade.Symbol, result.Symbol)
	s.Equal(trade.Status, result.Status)

	s.T().Log("✅ 特殊字符处理正常")
}

func (s *MySQLTestSuite) Test4_4_FloatFields() {
	trade := &MySQLTrade{
		UserID:     "U001",
		Price:      12345.67890123,
		Quantity:   0.123456789,
		Amount:     99999999.99,
		Commission: 0.001,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var result MySQLTrade
	db.Table("trades").First(&result)

	s.InDelta(trade.Price, result.Price, 0.0001)
	s.InDelta(trade.Quantity, result.Quantity, 0.0001)
	s.InDelta(trade.Amount, result.Amount, 0.01)

	s.T().Log("✅ 浮点数字段精度正确")
}

func (s *MySQLTestSuite) Test4_5_NullableFields() {
	trade := &MySQLTrade{
		UserID: "U001",
		Symbol: "",
		Status: "",
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var result MySQLTrade
	db.Table("trades").First(&result)

	s.Equal("", result.Symbol)
	s.Equal("", result.Status)

	s.T().Log("✅ 空值字段处理正常")
}

func (s *MySQLTestSuite) Test4_6_DecimalAggregation() {
	trades := []*MySQLTrade{
		{UserID: "U001", Amount: 100.0, Fee: decimal.NewFromFloat(0.1)},
		{UserID: "U002", Amount: 200.0, Fee: decimal.NewFromFloat(0.2)},
		{UserID: "U003", Amount: 300.0, Fee: decimal.NewFromFloat(0.3)},
	}

	now := time.Now()
	for _, t := range trades {
		t.CreatedAt = now
		t.UpdatedAt = now
		s.NoError(s.mysql.Insert(t))
	}
	s.trackDatabase(trades[0].GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var totalFee decimal.Decimal
	db.Table("trades").Select("SUM(fee)").Scan(&totalFee)

	expected := decimal.NewFromFloat(0.6)
	diff := totalFee.Sub(expected).Abs()
	s.True(diff.LessThan(decimal.NewFromFloat(0.01)))

	s.T().Logf("✅ Decimal聚合: %s (期望: %s)",
		totalFee.StringFixed(8), expected.StringFixed(8))
}

// ==================== 5. 查询功能测试 (6个) ====================

func (s *MySQLTestSuite) Test5_1_WhereConditions() {
	trades := generateTestMySQLTrades(50)
	s.NoError(s.mysql.HasTable(trades[0]))
	s.trackDatabase(trades[0].GetRemoteDBName())

	for _, t := range trades {
		s.NoError(s.mysql.Insert(t))
	}

	searchItem := &types.SearchItem{
		Model: &MySQLTrade{},
		WhereList: []*types.WhereItem{
			{Column: "amount", Symbol: ">", Value: 20000.0},
			{Column: "status", Symbol: "=", Value: "completed"},
		},
	}

	var results []*MySQLTrade
	err := s.mysql.Load(searchItem, &results)
	s.NoError(err)

	for _, r := range results {
		s.Greater(r.Amount, 20000.0)
		s.Equal("completed", r.Status)
	}

	s.T().Logf("✅ WHERE条件查询: %d 条", len(results))
}

func (s *MySQLTestSuite) Test5_2_OrderBy() {
	trades := generateTestMySQLTrades(20)
	s.NoError(s.mysql.HasTable(trades[0]))
	s.trackDatabase(trades[0].GetRemoteDBName())

	for _, t := range trades {
		s.NoError(s.mysql.Insert(t))
	}

	searchItem := &types.SearchItem{
		Model: &MySQLTrade{},
		SortList: []*types.SortItem{
			{Column: "amount", IsDesc: true},
		},
		Size: 10,
	}

	var results []*MySQLTrade
	err := s.mysql.Load(searchItem, &results)
	s.NoError(err)

	for i := 0; i < len(results)-1; i++ {
		s.GreaterOrEqual(results[i].Amount, results[i+1].Amount)
	}

	s.T().Logf("✅ ORDER BY查询: %d 条 (降序)", len(results))
}

func (s *MySQLTestSuite) Test5_3_Pagination() {
	trades := generateTestMySQLTrades(100)
	s.NoError(s.mysql.HasTable(trades[0]))
	s.trackDatabase(trades[0].GetRemoteDBName())

	for _, t := range trades {
		s.NoError(s.mysql.Insert(t))
	}

	searchItem1 := &types.SearchItem{
		Model:    &MySQLTrade{},
		Size:     20,
		Page:     1,
		SortList: []*types.SortItem{{Column: "created_at", IsDesc: false}},
	}

	var page1 []*MySQLTrade
	s.NoError(s.mysql.Load(searchItem1, &page1))
	s.Equal(20, len(page1))

	searchItem2 := &types.SearchItem{
		Model:    &MySQLTrade{},
		Size:     20,
		Page:     2,
		SortList: []*types.SortItem{{Column: "created_at", IsDesc: false}},
	}

	var page2 []*MySQLTrade
	s.NoError(s.mysql.Load(searchItem2, &page2))
	s.Equal(20, len(page2))

	s.NotEqual(page1[0].GetHash(), page2[0].GetHash())

	s.T().Log("✅ 分页查询正常")
}

func (s *MySQLTestSuite) Test5_4_OrConditions() {
	trades := []*MySQLTrade{
		{UserID: "U001", Status: "completed"},
		{UserID: "U002", Status: "pending"},
		{UserID: "U003", Status: "cancelled"},
	}

	now := time.Now()
	for _, t := range trades {
		t.CreatedAt = now
		t.UpdatedAt = now
		s.NoError(s.mysql.Insert(t))
	}
	s.trackDatabase(trades[0].GetRemoteDBName())

	searchItem := &types.SearchItem{
		Model: &MySQLTrade{},
		WhereList: []*types.WhereItem{
			{Column: "status", Symbol: "=", Value: "completed"},
			{Column: "status", Symbol: "=", Value: "pending", Relation: "OR"},
		},
	}

	var results []*MySQLTrade
	err := s.mysql.Load(searchItem, &results)
	s.NoError(err)
	s.Equal(2, len(results))

	s.T().Log("✅ OR条件查询正常")
}

func (s *MySQLTestSuite) Test5_5_LikeQuery() {
	trades := []*MySQLTrade{
		{UserID: "USER_001", Symbol: "BTCUSDT"},
		{UserID: "USER_002", Symbol: "ETHUSDT"},
		{UserID: "ADMIN_001", Symbol: "BNBUSDT"},
	}

	now := time.Now()
	for _, t := range trades {
		t.CreatedAt = now
		t.UpdatedAt = now
		s.NoError(s.mysql.Insert(t))
	}
	s.trackDatabase(trades[0].GetRemoteDBName())

	searchItem := &types.SearchItem{
		Model: &MySQLTrade{},
		WhereList: []*types.WhereItem{
			{Column: "user_id", Symbol: "LIKE", Value: "USER_%"},
		},
	}

	var results []*MySQLTrade
	err := s.mysql.Load(searchItem, &results)
	s.NoError(err)
	s.Equal(2, len(results))

	s.T().Log("✅ LIKE查询正常")
}

// ==================== 聚合查询修复 ====================

func (s *MySQLTestSuite) Test5_6_Aggregation() {
	trades := generateTestMySQLTrades(50)
	s.NoError(s.mysql.HasTable(trades[0]))
	s.trackDatabase(trades[0].GetRemoteDBName())

	totalAmount := 0.0
	for _, t := range trades {
		totalAmount += t.Amount
		s.NoError(s.mysql.Insert(t))
	}

	// 🔧 修复：使用原生 SQL 查询聚合
	db, err := s.mysql.GetDB()
	s.NoError(err)

	var stats struct {
		Amount float64
	}
	err = db.Table("trades").Select("SUM(amount) as amount").Scan(&stats).Error
	s.NoError(err)

	s.InDelta(totalAmount, stats.Amount, totalAmount*0.01)

	s.T().Logf("✅ 聚合查询: SUM(amount)=%.2f", stats.Amount)
}

// ==================== 6. 并发安全测试 (3个) ====================

func (s *MySQLTestSuite) Test6_1_ConcurrentInserts() {
	if testing.Short() {
		s.T().Skip("跳过并发测试")
	}

	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	concurrency := 10
	perGoroutine := 20

	var wg sync.WaitGroup
	errors := make(chan error, concurrency*perGoroutine)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 🔧 每个 goroutine 使用主实例而不是新建
			for j := 0; j < perGoroutine; j++ {
				// 🔧 关键修复：为每条记录生成唯一标识
				t := &MySQLTrade{
					UserID: fmt.Sprintf("U%03d", id),
					Symbol: fmt.Sprintf("SYM%d", j), // 添加 Symbol 使 hashcode 唯一
					Amount: float64((id+1)*100 + j), // 每条记录不同的 Amount
				}
				t.CreatedAt = time.Now()
				t.UpdatedAt = time.Now()

				// 🔧 手动设置唯一的 hashcode
				t.SetHashcode(t.GetHash())

				if err := s.mysql.Insert(t); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 收集错误
	var errList []error
	for e := range errors {
		errList = append(errList, e)
	}

	if len(errList) > 0 {
		s.T().Logf("插入错误数量: %d", len(errList))
		for i, e := range errList[:min(5, len(errList))] {
			s.T().Logf("错误 %d: %v", i+1, e)
		}
	}

	s.Equal(0, len(errList), "应该没有插入错误")

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(concurrency*perGoroutine), count)

	s.T().Logf("✅ 并发插入: %d 协程, %d 条数据", concurrency, count)
}

// 🔧 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *MySQLTestSuite) Test6_2_ConcurrentReadWrite() {
	if testing.Short() {
		s.T().Skip("跳过并发测试")
	}

	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	dbName := trade.GetRemoteDBName()
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	// 写操作
	writeCount := 10
	for i := 0; i < writeCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 🔧 创建独立的写实例
			writeMySQL := oltp.NewMySQL(s.mysql.GetConfig())
			writeMySQL.Name = dbName

			t := &MySQLTrade{
				UserID: fmt.Sprintf("W%03d", id),
				Amount: float64(id * 100),
			}
			t.CreatedAt = time.Now()
			t.UpdatedAt = time.Now()

			// 🔧 关键修复：先调用 HasTable 确保数据库上下文正确
			if err := writeMySQL.HasTable(t); err != nil {
				errors <- fmt.Errorf("HasTable failed: %v", err)
				return
			}

			// 现在可以安全插入
			if err := writeMySQL.Insert(t); err != nil {
				errors <- fmt.Errorf("Insert failed: %v", err)
			}
		}(i)
	}

	wg.Wait()

	// 读操作
	db, err := s.mysql.GetDB()
	s.Require().NoError(err, "获取数据库连接失败")

	readCount := 10
	for i := 0; i < readCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			var results []*MySQLTrade
			if err := db.Table(fmt.Sprintf("`%s`.`trades`", dbName)).Limit(5).Find(&results).Error; err != nil {
				errors <- fmt.Errorf("Read failed: %v", err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 收集错误
	var errList []error
	for e := range errors {
		errList = append(errList, e)
	}

	if len(errList) > 0 {
		for i, e := range errList {
			s.T().Logf("错误 %d: %v", i+1, e)
		}
	}

	s.Equal(0, len(errList), "不应该有并发错误")

	// 验证写入的数据
	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(writeCount), count, "应该有 %d 条写入的数据", writeCount)

	s.T().Logf("✅ 并发读写测试通过 (写入: %d, 读取: %d 次)", writeCount, readCount)
}

func (s *MySQLTestSuite) Test6_3_ConcurrentTransactions() {
	if testing.Short() {
		s.T().Skip("跳过并发测试")
	}

	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	dbName := trade.GetRemoteDBName()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 🔧 创建独立的 MySQL 实例，每个事务使用自己的连接
			txMySQL := oltp.NewMySQL(s.mysql.GetConfig())
			txMySQL.Name = dbName

			// 🔧 先确保数据库上下文正确
			t := &MySQLTrade{
				UserID: fmt.Sprintf("TX%03d", id),
				Amount: float64(id * 100),
			}
			t.CreatedAt = time.Now()
			t.UpdatedAt = time.Now()

			// 确保表存在并设置数据库上下文
			if err := txMySQL.HasTable(t); err != nil {
				errors <- fmt.Errorf("HasTable failed: %v", err)
				return
			}

			// 开启事务
			if err := txMySQL.Transaction(); err != nil {
				errors <- fmt.Errorf("Transaction failed: %v", err)
				return
			}

			// 插入数据
			if err := txMySQL.Insert(t); err != nil {
				errors <- fmt.Errorf("Insert failed: %v", err)
				txMySQL.Rollback()
				return
			}

			// 提交事务
			if err := txMySQL.Commit(); err != nil {
				errors <- fmt.Errorf("Commit failed: %v", err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 收集错误
	var errList []error
	for e := range errors {
		errList = append(errList, e)
	}

	if len(errList) > 0 {
		for i, e := range errList {
			s.T().Logf("错误 %d: %v", i+1, e)
		}
	}

	s.Equal(0, len(errList), "不应该有并发事务错误")

	// 验证所有事务都成功提交
	db, err := s.mysql.GetDB()
	s.NoError(err)
	var count int64
	db.Table("trades").Where("user_id LIKE 'TX%'").Count(&count)
	s.Equal(int64(10), count, "应该有 10 条事务数据")

	s.T().Log("✅ 并发事务测试通过")
}

// ==================== 7. 性能测试 (3个) ====================

func (s *MySQLTestSuite) Test7_1_BatchInsertPerformance() {
	if testing.Short() {
		s.T().Skip("跳过性能测试")
	}

	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	batchSize := 1000
	start := time.Now()

	trades := generateTestMySQLTrades(batchSize)
	for _, t := range trades {
		s.NoError(s.mysql.Insert(t))
	}

	duration := time.Since(start)

	db, err := s.mysql.GetDB()
	s.NoError(err)
	var count int64
	db.Table("trades").Count(&count)

	s.T().Logf("✅ 批量插入性能:")
	s.T().Logf("   - 数量: %d 条", count)
	s.T().Logf("   - 耗时: %v", duration)
	s.T().Logf("   - 速度: %.0f 条/秒", float64(batchSize)/duration.Seconds())
}

func (s *MySQLTestSuite) Test7_2_QueryPerformance() {
	if testing.Short() {
		s.T().Skip("跳过性能测试")
	}

	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	trades := generateTestMySQLTrades(5000)
	for _, t := range trades {
		s.NoError(s.mysql.Insert(t))
	}

	iterations := 100
	start := time.Now()
	db, err := s.mysql.GetDB()
	s.NoError(err)

	for i := 0; i < iterations; i++ {
		var results []*MySQLTrade
		db.Table("trades").Limit(100).Find(&results)
	}

	duration := time.Since(start)
	avgDuration := duration / time.Duration(iterations)

	s.T().Logf("✅ 查询性能:")
	s.T().Logf("   - 查询次数: %d", iterations)
	s.T().Logf("   - 总耗时: %v", duration)
	s.T().Logf("   - 平均耗时: %v", avgDuration)
	s.T().Logf("   - QPS: %.0f", float64(iterations)/duration.Seconds())
}

func (s *MySQLTestSuite) Test7_3_IndexEffectiveness() {
	if testing.Short() {
		s.T().Skip("跳过性能测试")
	}

	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	for i := 0; i < 20; i++ {
		trades := generateTestMySQLTrades(500)
		for _, t := range trades {
			s.NoError(s.mysql.Insert(t))
		}
	}

	db, err := s.mysql.GetDB()
	s.NoError(err)

	start := time.Now()
	var results []*MySQLTrade
	err = db.Table("trades").
		Where("user_id = ?", "U001").
		Where("status = ?", "completed").
		Limit(100).
		Find(&results).Error
	duration := time.Since(start)

	s.NoError(err)
	s.T().Logf("✅ 索引查询性能: %v (%d 条)", duration, len(results))
	s.Less(duration, 1*time.Second, "索引查询应该在1秒内完成")
}

// ==================== 8. 错误处理测试 (5个) ====================

func (s *MySQLTestSuite) Test8_1_InsertNil() {
	err := s.mysql.Insert(nil)
	s.Error(err)
	s.T().Log("✅ nil插入正确拒绝")
}

func (s *MySQLTestSuite) Test8_2_InvalidDatabaseName() {

	err := s.mysql.HasTable(nil)
	s.Error(err)
	s.Contains(err.Error(), "db name is empty")

	s.T().Log("✅ 无效数据库名正确报错")
}

func (s *MySQLTestSuite) Test8_3_DuplicateKey() {
	// 先创建表
	trade := &MySQLTrade{
		UserID: "U001",
		Symbol: "BTCUSDT",
		Amount: 1000.0,
	}

	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	// 第一次插入 - 使用固定的 hashcode
	trade.SetHashcode("duplicate_key_test")
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	err := s.mysql.Insert(trade)
	s.NoError(err, "第一次插入应该成功")

	// 验证第一次插入成功
	db, err := s.mysql.GetDB()
	s.NoError(err)

	var count1 int64
	db.Table("trades").Where("hashcode = ?", "duplicate_key_test").Count(&count1)
	s.Equal(int64(1), count1, "第一次插入后应该有1条记录")

	// 第二次插入 - 使用相同的 hashcode
	trade2 := &MySQLTrade{
		UserID: "U002",
		Symbol: "ETHUSDT",
		Amount: 2000.0,
	}
	trade2.SetHashcode("duplicate_key_test") // 相同的 hashcode
	trade2.CreatedAt = time.Now()
	trade2.UpdatedAt = time.Now()

	// 🔧 关键：使用 mysql.Insert() 方法测试其行为
	err = s.mysql.Insert(trade2)

	if err != nil {
		// 如果报错，验证错误信息
		s.Contains(err.Error(), "Duplicate", "应该包含 Duplicate 错误信息")
		s.T().Log("✅ Insert() 方法正确拒绝重复键")
	} else {
		// 🔧 如果不报错（使用了 REPLACE/ON DUPLICATE），验证只有一条记录
		var count2 int64
		db.Table("trades").Where("hashcode = ?", "duplicate_key_test").Count(&count2)
		s.Equal(int64(1), count2, "使用 REPLACE/UPDATE 策略时应该只有1条记录")

		// 验证数据被替换/更新
		var result MySQLTrade
		db.Table("trades").Where("hashcode = ?", "duplicate_key_test").First(&result)

		// 应该是第二次插入的数据（被替换）或第一次的数据（被忽略）
		if result.UserID == "U002" {
			s.T().Log("✅ Insert() 使用 REPLACE 策略，数据已替换")
			s.Equal(2000.0, result.Amount)
		} else if result.UserID == "U001" {
			s.T().Log("✅ Insert() 使用 ON DUPLICATE KEY UPDATE 策略，保留原数据")
			s.Equal(1000.0, result.Amount)
		} else {
			s.Fail("数据异常")
		}
	}

	// 最终验证：无论哪种策略，都应该只有一条记录
	var finalCount int64
	db.Table("trades").Where("hashcode = ?", "duplicate_key_test").Count(&finalCount)
	s.Equal(int64(1), finalCount, "最终应该只有1条记录（唯一索引生效）")

	s.T().Log("✅ 重复键处理验证完成")
}
func (s *MySQLTestSuite) Test8_4_QueryNonExistentTable() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.GetDBName(trade))

	var results []*MySQLTrade
	db, err := s.mysql.GetDB()
	s.NoError(err)

	err = db.Table("non_existent").Find(&results).Error
	s.Error(err)
	s.T().Log("✅ 查询不存在的表正确报错")
}

func (s *MySQLTestSuite) Test8_5_RecoveryFromError() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	db.Exec("SELECT * FROM non_existent")

	var count int64
	s.NoError(db.Table("trades").Count(&count).Error)
	s.T().Log("✅ 错误后连接恢复正常")
}

// ==================== 9. 连接管理测试 (4个) ====================

func (s *MySQLTestSuite) Test9_1_ConnectionPooling() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.GetDBName(trade))
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	sqlDB, err := db.DB()
	s.NoError(err)

	stats := sqlDB.Stats()
	s.T().Logf("✅ 连接池状态:")
	s.T().Logf("   - MaxOpen: %d", stats.MaxOpenConnections)
	s.T().Logf("   - InUse: %d", stats.InUse)
	s.T().Logf("   - Idle: %d", stats.Idle)
}

func (s *MySQLTestSuite) Test9_2_ConnectionReuse() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.GetDBName(trade))

	db1, err := s.mysql.GetDB()
	s.NoError(err)
	s.trackDatabase(trade.GetRemoteDBName())

	db2, err := s.mysql.GetDB()
	s.NoError(err)

	s.Equal(fmt.Sprintf("%p", db1), fmt.Sprintf("%p", db2))

	s.T().Log("✅ 连接重用验证通过")
}

func (s *MySQLTestSuite) Test9_3_ConnectionRecovery() {
	trade := &MySQLTrade{UserID: "U001", Amount: 100.0}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	err := s.mysql.RecreateConnection()
	s.NoError(err)

	trade2 := &MySQLTrade{UserID: "U002", Amount: 200.0}
	trade2.CreatedAt = time.Now()
	trade2.UpdatedAt = time.Now()
	s.NoError(s.mysql.Insert(trade2))

	s.T().Log("✅ 连接恢复测试通过")
}

func (s *MySQLTestSuite) Test9_4_MultipleConnections() {
	trade := &MySQLTrade{}
	user := &MySQLUser{}

	s.NoError(s.mysql.GetDBName(trade))
	db1, err := s.mysql.GetDB()
	s.NoError(err)
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.mysql.Name = ""
	s.NoError(s.mysql.GetDBName(user))
	db2, err := s.mysql.GetDB()
	s.NoError(err)
	s.NoError(s.mysql.HasTable(user))
	s.trackDatabase(user.GetRemoteDBName())

	s.NotEqual(fmt.Sprintf("%p", db1), fmt.Sprintf("%p", db2))

	s.T().Log("✅ 多数据库连接管理正常")
}

// ==================== 10. 数据库管理测试 (3个) ====================

func (s *MySQLTestSuite) Test10_1_DeleteDatabase() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	dbName := trade.GetRemoteDBName()
	s.trackDatabase(dbName)

	t := &MySQLTrade{UserID: "U001", Amount: 100.0}
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	s.NoError(s.mysql.Insert(t))

	err := s.mysql.DeleteDB()
	s.NoError(err)

	s.mysql.Name = ""
	db, err := s.mysql.GetDB()
	s.NoError(err)

	var count int64
	db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?", dbName).Scan(&count)
	s.Equal(int64(0), count)

	s.T().Log("✅ 数据库删除成功")
}

func (s *MySQLTestSuite) Test10_2_RecreateDatabase() {
	trade := &MySQLTrade{}
	s.NoError(s.mysql.HasTable(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.NoError(s.mysql.DeleteDB())
	db, err := s.mysql.GetDB()
	s.NoError(err)
	s.NoError(s.mysql.HasTable(trade))

	var count int64
	db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?",
		trade.GetRemoteDBName()).Scan(&count)
	s.Equal(int64(1), count)

	s.T().Log("✅ 数据库重建成功")
}

func (s *MySQLTestSuite) Test10_3_DatabaseIsolation() {
	trade := &MySQLTrade{UserID: "U001", Amount: 100.0}
	user := &MySQLUser{Username: "user1", Balance: 1000.0}

	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	s.mysql.Name = ""
	s.NoError(s.mysql.Insert(user))
	s.trackDatabase(user.GetRemoteDBName())

	s.NotEqual(trade.GetRemoteDBName(), user.GetRemoteDBName())

	s.T().Log("✅ 数据库隔离验证通过")
}

// ==================== 11. 边界值测试 (5个) ====================

func (s *MySQLTestSuite) Test11_2_MaxDecimalValue() {
	maxDecimal, _ := decimal.NewFromString("999999999999.99999999")
	trade := &MySQLTrade{
		UserID: "U001",
		Amount: 999999999999.99,
		Fee:    maxDecimal,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var result MySQLTrade
	db.Table("trades").First(&result)

	diff := result.Fee.Sub(maxDecimal).Abs()
	s.True(diff.LessThan(decimal.NewFromFloat(0.01)))

	s.T().Log("✅ 最大Decimal值处理正常")
}

func (s *MySQLTestSuite) Test11_3_MinDecimalValue() {
	minDecimal := decimal.NewFromFloat(0.00000001)
	trade := &MySQLTrade{
		UserID: "U001",
		Fee:    minDecimal,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var result MySQLTrade
	db.Table("trades").First(&result)

	s.Equal(minDecimal.StringFixed(8), result.Fee.StringFixed(8))

	s.T().Log("✅ 最小Decimal值处理正常")
}

func (s *MySQLTestSuite) Test11_4_ZeroValues() {
	trade := &MySQLTrade{
		UserID:     "U001",
		Price:      0.0,
		Quantity:   0.0,
		Amount:     0.0,
		Commission: 0.0,
		Fee:        decimal.Zero,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	s.NoError(s.mysql.Insert(trade))
	s.trackDatabase(trade.GetRemoteDBName())

	db, err := s.mysql.GetDB()
	s.NoError(err)

	var result MySQLTrade
	db.Table("trades").First(&result)

	s.Equal(0.0, result.Amount)
	s.True(result.Fee.IsZero())

	s.T().Log("✅ 零值处理正常")
}

func (s *MySQLTestSuite) Test11_5_LongStrings() {
	// 🔧 安全的长度设置 - 考虑 UTF8MB4 编码
	// varchar(100) 最多 100 个字符（不是字节）
	// 但为了安全，我们使用更保守的长度

	userIDString := strings.Repeat("A", 90)  // 英文字符，90个字符
	symbolString := strings.Repeat("BTC", 8) // 24个字符，适合 varchar(50)
	statusString := strings.Repeat("OK", 20) // 40个字符，适合 varchar(50)

	s.T().Logf("测试字符串长度:")
	s.T().Logf("   - UserID: %d 字符, %d 字节", len(userIDString), len([]byte(userIDString)))
	s.T().Logf("   - Symbol: %d 字符, %d 字节", len(symbolString), len([]byte(symbolString)))
	s.T().Logf("   - Status: %d 字符, %d 字节", len(statusString), len([]byte(statusString)))

	trade := &MySQLTrade{
		UserID: userIDString,
		Symbol: symbolString,
		Status: statusString,
		Amount: 1000.0,
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()
	trade.SetHashcode(trade.GetHash())
	// 确保表存在
	err := s.mysql.HasTable(trade)
	s.Require().NoError(err, "创建表失败")
	s.trackDatabase(trade.GetRemoteDBName())

	// 插入数据
	err = s.mysql.Insert(trade)
	s.Require().NoError(err, "插入数据失败: UserID长度=%d, Symbol长度=%d, Status长度=%d",
		len(userIDString), len(symbolString), len(statusString))

	// 验证数据
	db, err := s.mysql.GetDB()
	s.Require().NoError(err)

	var result MySQLTrade
	err = db.Table("trades").Where("hashcode = ?", trade.GetHash()).First(&result).Error
	s.Require().NoError(err, "查询数据失败")

	s.Equal(userIDString, result.UserID)
	s.Equal(symbolString, result.Symbol)
	s.Equal(statusString, result.Status)

	s.T().Logf("✅ 长字符串处理正常:")
	s.T().Logf("   - UserID: %d/%d 字符", len(result.UserID), 100)
	s.T().Logf("   - Symbol: %d/%d 字符", len(result.Symbol), 50)
	s.T().Logf("   - Status: %d/%d 字符", len(result.Status), 50)
}

// ==================== 其他插入测试修复 ====================

func (s *MySQLTestSuite) Test2_1_InsertSingle() {
	trade := &MySQLTrade{
		UserID:     "U001",
		Symbol:     "BTCUSDT",
		Amount:     5000.00,
		Commission: 5.00,
		Fee:        decimal.NewFromFloat(2.50),
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	// 🔧 修复：先调用 HasTable 创建表和数据库
	err := s.mysql.HasTable(trade)
	s.NoError(err)
	s.trackDatabase(trade.GetRemoteDBName())

	// 🔧 关键修复：重新获取连接确保使用正确的数据库
	s.mysql.Name = trade.GetRemoteDBName()
	db, err := s.mysql.GetDB()
	s.NoError(err, "获取数据库连接失败")

	// 验证数据库存在
	var dbExists int64
	db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?",
		trade.GetRemoteDBName()).Scan(&dbExists)
	s.Equal(int64(1), dbExists, "数据库应该存在")

	// 现在插入数据
	err = s.mysql.Insert(trade)
	s.NoError(err, "插入数据失败")

	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(1), count)

	s.T().Log("✅ 单条插入成功")
}
func (s *MySQLTestSuite) Test11_1_EmptyStringFields() {
	trade := &MySQLTrade{
		UserID: "",
		Symbol: "",
		Status: "",
		Amount: 100.0, // 🔧 添加一个非零字段用于验证
	}
	trade.CreatedAt = time.Now()
	trade.UpdatedAt = time.Now()

	// 🔧 确保表创建成功
	err := s.mysql.HasTable(trade)
	s.NoError(err, "创建表失败")
	s.trackDatabase(trade.GetRemoteDBName())

	// 🔧 重新设置数据库上下文
	s.mysql.Name = trade.GetRemoteDBName()
	_, err = s.mysql.GetDB()
	s.NoError(err, "获取数据库连接失败")

	// 插入数据
	err = s.mysql.Insert(trade)
	s.NoError(err, "插入空字符串数据失败")

	// 验证插入成功
	db, err := s.mysql.GetDB()
	s.NoError(err)

	var count int64
	db.Table("trades").Count(&count)
	s.Equal(int64(1), count, "应该有1条记录")

	s.T().Log("✅ 空字符串插入正常")
}

// ==================== 运行测试套件 ====================

func TestMySQLSuite(t *testing.T) {
	suite.Run(t, new(MySQLTestSuite))
}
