package sync

import (
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	utils.TESTPATH = "/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core/pkg/persistence/database/oltp/sync"
	config.INITSERVER = false
}

// 🔧 测试模型 - 实现 IDBName 接口
type TestSyncUser struct {
	*entity.Model
	Name   string `gorm:"size:100;not null;uniqueIndex"`
	Email  string `gorm:"size:100;not null;uniqueIndex"`
	Status string `gorm:"size:50;not null"`
}

// 🔧 实现 IDBName 接口
func (t *TestSyncUser) GetLocalDBName() string {
	return "test_local_db"
}

func (t *TestSyncUser) GetRemoteDBName() string {
	return "test_remote_db"
}

type TestOrderWithItems struct {
	*entity.Model
	UserID int64           `gorm:"not null;index"`
	Amount float64         `gorm:"not null"`
	Items  []TestOrderItem `gorm:"foreignKey:OrderID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

// 🔧 实现 IDBName 接口
func (t *TestOrderWithItems) GetLocalDBName() string {
	return "test_local_db"
}

func (t *TestOrderWithItems) GetRemoteDBName() string {
	return "test_remote_db"
}

type TestOrderItem struct {
	*entity.Model
	OrderID  int64   `gorm:"not null;index"`
	Product  string  `gorm:"size:100;not null"`
	Price    float64 `gorm:"not null"`
	Quantity int     `gorm:"not null"`
}

// 🔧 实现 IDBName 接口
func (t *TestOrderItem) GetLocalDBName() string {
	return "test_local_db"
}

func (t *TestOrderItem) GetRemoteDBName() string {
	return "test_remote_db"
}

// ========================================
// 测试辅助函数
// ========================================

// 🔧 从模型获取数据库名称
func getDBNamesFromModel(model types.IDBName) (localName, remoteName string) {
	return model.GetLocalDBName(), model.GetRemoteDBName()
}

// 设置测试 SQLite 数据库
func setupTestSQLiteWithData(t *testing.T, model types.IDBName) *oltp.Sqlite {
	localDBName, _ := getDBNamesFromModel(model)

	sqlite := oltp.NewSqlite()
	sqlite.Name = localDBName
	sqlite.IsLog = false

	// 确保数据库可用
	db, err := sqlite.GetDB()
	require.NoError(t, err)
	require.NotNil(t, db)

	t.Logf("✅ 创建 SQLite 数据库: %s", localDBName)
	return sqlite
}

// 清理测试数据
func cleanupTestSQLiteData(t *testing.T, sqlite *oltp.Sqlite) {
	if sqlite == nil {
		return
	}

	db, err := sqlite.GetDB()
	if err == nil && db != nil {
		// 只删除表，不关闭连接
		db.Exec("DROP TABLE IF EXISTS TestOrderItem")
		db.Exec("DROP TABLE IF EXISTS TestOrderWithItems")
		db.Exec("DROP TABLE IF EXISTS TestSyncUser")
		t.Logf("✅ 清理 SQLite 表")
	}

	// 清除表缓存
	oltp.ClearTableCache()
}

// 设置测试 MySQL 数据库
func setupTestMySQL(t *testing.T, model types.IDBName) *oltp.Mysql {
	_, remoteDBName := getDBNamesFromModel(model)

	mysql := oltp.NewMysql(
		"localhost",
		"root",
		"123456Test",
		3306,
		false,
		false,
	)

	db, err := mysql.GetDB()
	require.NoError(t, err)

	// 创建测试数据库
	err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4", remoteDBName)).Error
	require.NoError(t, err)

	// 切换到测试数据库
	mysql.Name = remoteDBName
	db, err = mysql.GetDB()
	require.NoError(t, err)

	t.Logf("✅ 创建 MySQL 数据库: %s", remoteDBName)
	return mysql
}

// 清理测试 MySQL 数据库
func cleanupTestMySQL(t *testing.T, model types.IDBName) {
	_, remoteDBName := getDBNamesFromModel(model)

	mysql := oltp.NewMysql(
		"localhost",
		"root",
		"123456Test",
		3306,
		false,
		false,
	)

	db, err := mysql.GetDB()
	if err != nil {
		t.Logf("⚠️  连接 MySQL 失败: %v", err)
		return
	}

	// 删除测试数据库
	err = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", remoteDBName)).Error
	if err != nil {
		t.Logf("⚠️  删除测试数据库失败: %v", err)
	} else {
		t.Logf("✅ 删除 MySQL 数据库: %s", remoteDBName)
	}
}

// 插入测试用户数据
func insertTestUsers(t *testing.T, sqlite *oltp.Sqlite, count int) []*TestSyncUser {
	err := sqlite.HasTable(&TestSyncUser{})
	require.NoError(t, err)

	users := make([]*TestSyncUser, 0, count)
	timestamp := time.Now().UnixNano()

	for i := 0; i < count; i++ {
		user := &TestSyncUser{
			Model:  entity.NewModel(),
			Name:   fmt.Sprintf("User%d_%d", timestamp, i),
			Email:  fmt.Sprintf("user%d_%d@example.com", timestamp, i),
			Status: "active",
		}

		err := sqlite.Insert(user)
		require.NoError(t, err, "插入用户失败 at %d", i)
		users = append(users, user)
	}

	t.Logf("✅ 成功插入 %d 条用户数据", count)
	return users
}

// 插入测试订单数据
func insertTestOrders(t *testing.T, sqlite *oltp.Sqlite, count int) []*TestOrderWithItems {
	err := sqlite.HasTable(&TestOrderWithItems{})
	require.NoError(t, err)

	orders := make([]*TestOrderWithItems, 0, count)

	for i := 0; i < count; i++ {
		order := &TestOrderWithItems{
			Model:  entity.NewModel(),
			UserID: int64(i + 1),
			Amount: float64(100 * (i + 1)),
			Items: []TestOrderItem{
				{
					Model:    entity.NewModel(),
					Product:  fmt.Sprintf("Product%d-1", i),
					Price:    50.00,
					Quantity: 1,
				},
				{
					Model:    entity.NewModel(),
					Product:  fmt.Sprintf("Product%d-2", i),
					Price:    50.00,
					Quantity: 1,
				},
			},
		}

		err := sqlite.Insert(order)
		require.NoError(t, err, "插入订单失败 at %d", i)
		orders = append(orders, order)
	}

	t.Logf("✅ 成功插入 %d 条订单数据", count)
	return orders
}

// 验证同步结果
func verifySync(t *testing.T, mysql *oltp.Mysql, tableName string, expectedCount int) {
	db, err := mysql.GetDB()
	require.NoError(t, err)

	var count int64
	err = db.Table(tableName).Count(&count).Error
	require.NoError(t, err)

	assert.Equal(t, int64(expectedCount), count,
		"MySQL 中 %s 表的记录数不符合预期", tableName)
	t.Logf("✅ %s 表同步验证成功: %d/%d 条", tableName, count, expectedCount)
}

// ========================================
// 集成测试 - 单表同步
// ========================================

func TestSync_SingleTable_ToRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}

	// 🔧 从模型获取数据库名称
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s -> %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	// 插入测试数据
	userCount := 10
	insertTestUsers(t, sqlite, userCount)

	// 创建同步管理器
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     20,
		Direction:     SyncToRemote,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(10 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	// 额外等待确保同步完成
	time.Sleep(2 * time.Second)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 同步统计: 总计=%d, 失败=%d, 上传=%d, 下载=%d",
		total, failed, toRemote, fromRemote)

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 MySQL 数据
	verifySync(t, mysql, "TestSyncUser", userCount)

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

func TestSync_SingleTable_FromRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s <- %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	// 在 MySQL 中插入测试数据
	err := mysql.HasTable(&TestSyncUser{})
	require.NoError(t, err)

	timestamp := time.Now().UnixNano()
	mysqlUserCount := 15
	for i := 0; i < mysqlUserCount; i++ {
		user := &TestSyncUser{
			Model:  entity.NewModel(),
			Name:   fmt.Sprintf("MySQLUser%d_%d", timestamp, i),
			Email:  fmt.Sprintf("mysqluser%d_%d@example.com", timestamp, i),
			Status: "active",
		}
		err := mysql.Insert(user)
		require.NoError(t, err)
	}

	// 创建空的 SQLite 数据库
	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	err = sqlite.HasTable(&TestSyncUser{})
	require.NoError(t, err)

	// 创建同步管理器
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     20,
		Direction:     SyncFromRemote,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(10 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 同步统计: 总计=%d, 失败=%d, 上传=%d, 下载=%d",
		total, failed, toRemote, fromRemote)

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 SQLite 数据
	item := &types.SearchItem{
		Model: &TestSyncUser{
			Model: entity.NewModel(),
		},
	}
	var results []TestSyncUser
	err = sqlite.Load(item, &results)
	require.NoError(t, err)

	mysqlUserFoundCount := 0
	for _, r := range results {
		if len(r.Name) >= 9 && r.Name[:9] == "MySQLUser" {
			mysqlUserFoundCount++
		}
	}

	assert.Equal(t, mysqlUserCount, mysqlUserFoundCount,
		"SQLite 中应有 %d 条 MySQLUser 记录", mysqlUserCount)
	t.Logf("✅ SQLite 同步验证成功: %d/%d 条 MySQLUser 记录",
		mysqlUserFoundCount, mysqlUserCount)

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

func TestSync_SingleTable_Both(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s <-> %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	// 在 SQLite 插入数据
	sqliteUserCount := 8
	insertTestUsers(t, sqlite, sqliteUserCount)

	// 在 MySQL 插入不同的数据
	err := mysql.HasTable(&TestSyncUser{
		Model: entity.NewModel(),
	})
	require.NoError(t, err)

	timestamp := time.Now().UnixNano()
	mysqlUserCount := 12
	for i := 0; i < mysqlUserCount; i++ {
		user := &TestSyncUser{
			Model:  entity.NewModel(),
			Name:   fmt.Sprintf("RemoteUser%d_%d", timestamp, i),
			Email:  fmt.Sprintf("remoteuser%d_%d@example.com", timestamp, i),
			Status: "active",
		}
		err := mysql.Insert(user)
		require.NoError(t, err)
	}

	// 创建同步管理器
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     20,
		Direction:     SyncBoth,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(10 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	time.Sleep(3 * time.Second)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 双向同步统计: 总计=%d, 失败=%d, 上传=%d, 下载=%d",
		total, failed, toRemote, fromRemote)

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 MySQL 数据（应包含 SQLite 上传的数据）
	mysqlDB, _ := mysql.GetDB()
	var mysqlCount int64
	err = mysqlDB.Table("TestSyncUser").Count(&mysqlCount).Error
	require.NoError(t, err)
	t.Logf("📊 MySQL 记录数: %d", mysqlCount)

	// 验证 SQLite 数据（应包含 MySQL 下载的数据）
	item := &types.SearchItem{
		Model: &TestSyncUser{
			Model: entity.NewModel(),
		},
	}
	var sqliteResults []TestSyncUser
	err = sqlite.Load(item, &sqliteResults)
	require.NoError(t, err)
	t.Logf("📊 SQLite 记录数: %d", len(sqliteResults))

	// 应该至少有本地和远程数据
	assert.GreaterOrEqual(t, int(mysqlCount), sqliteUserCount,
		"MySQL 应至少包含 SQLite 上传的 %d 条记录", sqliteUserCount)
	assert.GreaterOrEqual(t, len(sqliteResults), mysqlUserCount,
		"SQLite 应至少包含 MySQL 下载的 %d 条记录", mysqlUserCount)

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

// ========================================
// 集成测试 - 多表同步
// ========================================

func TestSync_MultipleTables(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s -> %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	// 插入用户数据
	userCount := 5
	insertTestUsers(t, sqlite, userCount)

	// 插入订单数据
	orderCount := 3
	insertTestOrders(t, sqlite, orderCount)

	// 创建同步管理器
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     20,
		Direction:     SyncToRemote,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(10 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	time.Sleep(3 * time.Second)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 多表同步统计: 总计=%d, 失败=%d, 上传=%d, 下载=%d",
		total, failed, toRemote, fromRemote)

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 MySQL 数据
	verifySync(t, mysql, "TestSyncUser", userCount)
	verifySync(t, mysql, "TestOrderWithItems", orderCount)
	verifySync(t, mysql, "TestOrderItem", orderCount*2) // 每个订单2个项目

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

// ========================================
// 集成测试 - 大数据量同步
// ========================================

func TestSync_LargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大数据量测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s -> %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	// 插入大量数据
	largeCount := 100
	insertTestUsers(t, sqlite, largeCount)

	// 创建同步管理器
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     50, // 较大的批量大小
		Direction:     SyncToRemote,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	startTime := time.Now()
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(30 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	time.Sleep(5 * time.Second)
	duration := time.Since(startTime)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 大数据量同步统计:")
	t.Logf("  总计: %d", total)
	t.Logf("  失败: %d", failed)
	t.Logf("  上传: %d", toRemote)
	t.Logf("  下载: %d", fromRemote)
	t.Logf("  耗时: %v", duration)
	t.Logf("  速率: %.2f 条/秒", float64(largeCount)/duration.Seconds())

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 MySQL 数据
	verifySync(t, mysql, "TestSyncUser", largeCount)

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

// ========================================
// 集成测试 - 过滤器功能
// ========================================

func TestSync_WithTableFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s -> %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	// 插入用户和订单数据
	userCount := 5
	insertTestUsers(t, sqlite, userCount)

	orderCount := 3
	insertTestOrders(t, sqlite, orderCount)

	// 创建同步管理器（只同步用户表）
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     20,
		Direction:     SyncToRemote,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
		TableFilter: func(tableName string) bool {
			// 只同步 TestSyncUser 表
			return tableName == "TestSyncUser"
		},
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(10 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 表过滤同步统计: 总计=%d, 失败=%d, 上传=%d, 下载=%d",
		total, failed, toRemote, fromRemote)

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 MySQL 数据
	verifySync(t, mysql, "TestSyncUser", userCount)

	// 验证订单表不应存在
	mysqlDB, _ := mysql.GetDB()
	var orderTableCount int64
	err = mysqlDB.Raw("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=?",
		remoteDBName, "TestOrderWithItems").Scan(&orderTableCount).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), orderTableCount, "订单表不应被同步")

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

func TestSync_WithRecordFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 🔧 创建测试模型实例
	testModel := &TestSyncUser{}
	localDBName, remoteDBName := getDBNamesFromModel(testModel)
	t.Logf("📊 数据库映射: %s -> %s", localDBName, remoteDBName)

	// 准备测试环境
	mysql := setupTestMySQL(t, testModel)
	defer cleanupTestMySQL(t, testModel)

	sqlite := setupTestSQLiteWithData(t, testModel)
	defer cleanupTestSQLiteData(t, sqlite)

	// 插入不同状态的用户数据
	err := sqlite.HasTable(&TestSyncUser{
		Model: entity.NewModel(),
	})
	require.NoError(t, err)

	timestamp := time.Now().UnixNano()
	activeCount := 5
	inactiveCount := 3

	// 插入 active 用户
	for i := 0; i < activeCount; i++ {
		user := &TestSyncUser{
			Model:  entity.NewModel(),
			Name:   fmt.Sprintf("ActiveUser%d_%d", timestamp, i),
			Email:  fmt.Sprintf("active%d_%d@example.com", timestamp, i),
			Status: "active",
		}
		err := sqlite.Insert(user)
		require.NoError(t, err)
	}

	// 插入 inactive 用户
	for i := 0; i < inactiveCount; i++ {
		user := &TestSyncUser{
			Model:  entity.NewModel(),
			Name:   fmt.Sprintf("InactiveUser%d_%d", timestamp, i),
			Email:  fmt.Sprintf("inactive%d_%d@example.com", timestamp, i),
			Status: "inactive",
		}
		err := sqlite.Insert(user)
		require.NoError(t, err)
	}

	// 创建同步管理器（只同步 active 状态的记录）
	config := &SyncConfig{
		MySQLHost:     "localhost",
		MySQLPort:     3306,
		MySQLUser:     "root",
		MySQLPass:     "123456Test",
		Interval:      100 * time.Millisecond,
		BatchSize:     20,
		Direction:     SyncToRemote,
		ConflictMode:  ConflictModeNewest,
		EnableLogging: true,
		RecordFilter: func(record interface{}) bool {
			if r, ok := record.(map[string]interface{}); ok {
				return r["status"] == "active"
			}
			return true
		},
	}

	manager, err := NewDBSyncManager(config)
	require.NoError(t, err)

	// 启动同步
	err = manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 等待同步完成
	helper := NewSyncHelper(manager)
	err = helper.WaitForFirstSync(10 * time.Second)
	if err != nil {
		t.Logf("⚠️  等待首次同步超时: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 验证统计
	stats := manager.GetStats()
	total, failed, toRemote, fromRemote, _ := stats.GetStats()
	t.Logf("📊 记录过滤同步统计: 总计=%d, 失败=%d, 上传=%d, 下载=%d",
		total, failed, toRemote, fromRemote)

	// 生成报告
	report := helper.GenerateSyncReport()
	t.Log(report)

	// 验证 MySQL 数据（应该只有 active 用户）
	verifySync(t, mysql, "TestSyncUser", activeCount)

	// 停止管理器
	manager.Stop()
	time.Sleep(100 * time.Millisecond)
}

// ========================================
// 集成测试套件
// ========================================

func TestSyncIntegrationSuite(t *testing.T) {
	t.Log("========================================")
	t.Log("开始运行同步集成测试套件")
	t.Log("========================================")

	startTime := time.Now()

	// 单表同步测试
	t.Run("单表同步测试", func(t *testing.T) {
		t.Run("同步到远程", TestSync_SingleTable_ToRemote)
		t.Run("从远程同步", TestSync_SingleTable_FromRemote)
		t.Run("双向同步", TestSync_SingleTable_Both)
	})

	// 多表同步测试
	t.Run("多表同步测试", func(t *testing.T) {
		t.Run("多表同步", TestSync_MultipleTables)
	})

	// 过滤器测试
	t.Run("过滤器测试", func(t *testing.T) {
		t.Run("表过滤器", TestSync_WithTableFilter)
		t.Run("记录过滤器", TestSync_WithRecordFilter)
	})

	// 大数据量测试
	if !testing.Short() {
		t.Run("大数据量测试", func(t *testing.T) {
			t.Run("大数据量同步", TestSync_LargeDataset)
		})
	}

	duration := time.Since(startTime)
	t.Log("========================================")
	t.Logf("同步集成测试套件执行完毕，总耗时: %v", duration)
	t.Log("========================================")
}
