package test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	utils.TESTPATH = "/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core/pkg/persistence/database/test"
}

// SQLite 测试模型
type SQLiteTestUser struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Name      string    `gorm:"size:100"`
	Email     string    `gorm:"size:100;uniqueIndex"`
	Age       int       `gorm:"default:0"`
	Balance   float64   `gorm:"type:decimal(10,2);default:0"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (SQLiteTestUser) GetLocalDBName() string {
	return "sqlite_test_db"
}

func (SQLiteTestUser) GetRemoteDBName() string {
	return "sqlite_test_db"
}

// 嵌套表测试
type SQLiteTestOrder struct {
	ID        int64                 `gorm:"primaryKey;autoIncrement"`
	UserID    int64                 `gorm:"index"`
	Amount    float64               `gorm:"type:decimal(10,2)"`
	Items     []SQLiteTestOrderItem `gorm:"foreignKey:OrderID"`
	CreatedAt time.Time             `gorm:"autoCreateTime"`
}

func (SQLiteTestOrder) GetLocalDBName() string {
	return "sqlite_test_db"
}

func (SQLiteTestOrder) GetRemoteDBName() string {
	return "sqlite_test_db"
}

type SQLiteTestOrderItem struct {
	ID       int64   `gorm:"primaryKey;autoIncrement"`
	OrderID  int64   `gorm:"index"`
	Product  string  `gorm:"size:100"`
	Price    float64 `gorm:"type:decimal(10,2)"`
	Quantity int     `gorm:"default:1"`
}

func (SQLiteTestOrderItem) GetLocalDBName() string {
	return "sqlite_test_db"
}

func (SQLiteTestOrderItem) GetRemoteDBName() string {
	return "sqlite_test_db"
}

// 测试辅助函数
func setupTestSQLite(t *testing.T) *oltp.Sqlite {
	config.INITSERVER = false
	sqlite := oltp.NewSqlite()
	sqlite.IsLog = false

	// 确保测试数据库可用
	db, err := sqlite.GetDB()
	require.NoError(t, err)
	require.NotNil(t, db)

	return sqlite
}

// 🔧 修复：清理函数不再关闭连接
func cleanupTestDataSQLite(t *testing.T, sqlite *oltp.Sqlite) {
	db, _ := sqlite.GetDB()
	if db != nil {
		// 只删除表,不关闭连接
		db.Exec("DROP TABLE IF EXISTS SQLiteTestOrderItem")
		db.Exec("DROP TABLE IF EXISTS SQLiteTestOrder")
		db.Exec("DROP TABLE IF EXISTS SQLiteTestUser")
	}
	err := sqlite.DeleteDB()
	assert.NoError(t, err)
	// 清除表缓存
	oltp.ClearTableCache()
}

// ========================================
// 基础功能测试
// ========================================

func TestNewSqlite(t *testing.T) {
	sqlite := oltp.NewSqlite()

	assert.NotNil(t, sqlite)
	assert.NotEmpty(t, sqlite.Path)
}

func TestSqlite_GetDB(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	db, err := sqlite.GetDB()
	assert.NoError(t, err)
	assert.NotNil(t, db)

	// 测试连接健康
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	assert.NoError(t, sqlDB.Ping())
}

func TestSqlite_GetDB_Cache(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	db1, err1 := sqlite.GetDB()
	db2, err2 := sqlite.GetDB()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Equal(t, db1, db2, "应返回相同的连接实例")
}

func TestSqlite_DeleteDB(t *testing.T) {
	sqlite := setupTestSQLite(t)

	// 先创建表
	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 记录路径
	dbPath := sqlite.Path

	// 删除数据库
	err = sqlite.DeleteDB()
	assert.NoError(t, err)

	// 验证文件已删除
	assert.False(t, utils.IsExista(dbPath), "数据库文件应该被删除")
}

// ========================================
// 表管理测试
// ========================================

func TestSqlite_HasTable(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	user := &SQLiteTestUser{}
	err := sqlite.HasTable(user)
	assert.NoError(t, err)

	// 验证表是否真的创建
	db, err := sqlite.GetDB()
	require.NoError(t, err)

	var count int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		"SQLiteTestUser").Scan(&count)
	assert.Equal(t, int64(1), count)
}

func TestSqlite_HasTable_Cache(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	user := &SQLiteTestUser{}
	err1 := sqlite.HasTable(user)
	err2 := sqlite.HasTable(user)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

func TestSqlite_HasTable_NestedTables(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	order := &SQLiteTestOrder{}
	err := sqlite.HasTable(order)
	assert.NoError(t, err)

	// 验证主表和嵌套表都创建
	db, _ := sqlite.GetDB()
	var countOrder, countItem int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		"SQLiteTestOrder").Scan(&countOrder)
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		"SQLiteTestOrderItem").Scan(&countItem)

	assert.Equal(t, int64(1), countOrder)
	assert.Equal(t, int64(1), countItem)
}

// ========================================
// CRUD 操作测试
// ========================================

func TestSqlite_Insert(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	user := &SQLiteTestUser{
		Name:    "Alice",
		Email:   "alice@example.com",
		Age:     25,
		Balance: 100.50,
	}

	err = sqlite.Insert(user)
	assert.NoError(t, err)
	assert.Greater(t, user.ID, int64(0))
}

func TestSqlite_Insert_Duplicate(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	user1 := &SQLiteTestUser{Name: "Bob", Email: "bob@example.com", Age: 30}
	user2 := &SQLiteTestUser{Name: "Bob2", Email: "bob@example.com", Age: 31}

	err1 := sqlite.Insert(user1)
	err2 := sqlite.Insert(user2)

	assert.NoError(t, err1)
	assert.Error(t, err2, "应因唯一索引冲突报错")
}

func TestSqlite_Update(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	user := &SQLiteTestUser{Name: "Charlie", Email: "charlie@example.com", Age: 28}
	err = sqlite.Insert(user)
	require.NoError(t, err)

	user.Age = 29
	user.Balance = 200.00
	err = sqlite.Update(user)
	assert.NoError(t, err)

	// 验证更新
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "ID", Value: user.ID}},
	}
	var result SQLiteTestUser
	err = sqlite.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, 29, result.Age)
	assert.Equal(t, 200.00, result.Balance)
}

func TestSqlite_Delete(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	user := &SQLiteTestUser{Name: "David", Email: "david@example.com", Age: 35}
	err = sqlite.Insert(user)
	require.NoError(t, err)

	err = sqlite.Delete(user)
	assert.NoError(t, err)

	// 验证删除
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "ID", Value: user.ID}},
	}
	var result SQLiteTestUser
	err = sqlite.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID, "记录应不存在")
}

func TestSqlite_Load(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 插入测试数据
	users := []*SQLiteTestUser{
		{Name: "Eve", Email: "eve@example.com", Age: 22, Balance: 50.00},
		{Name: "Frank", Email: "frank@example.com", Age: 27, Balance: 150.00},
		{Name: "Grace", Email: "grace@example.com", Age: 32, Balance: 250.00},
	}
	for _, u := range users {
		require.NoError(t, sqlite.Insert(u))
	}

	// 测试查询单条
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "Name", Value: "Eve"}},
	}
	var result SQLiteTestUser
	err = sqlite.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, "Eve", result.Name)
	assert.Equal(t, 22, result.Age)

	// 测试查询多条
	item2 := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "Age", Symbol: ">", Value: 25}},
	}
	var results []SQLiteTestUser
	err = sqlite.Load(item2, &results)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results))
}

// ========================================
// 事务测试
// ========================================

func TestSqlite_Transaction(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	sqlite.Transaction()

	user1 := &SQLiteTestUser{Name: "Henry", Email: "henry@example.com", Age: 40}
	user2 := &SQLiteTestUser{Name: "Iris", Email: "iris@example.com", Age: 45}

	err1 := sqlite.Insert(user1)
	err2 := sqlite.Insert(user2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	err = sqlite.Commit()
	assert.NoError(t, err)

	// 使用新实例查询
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "Name", Value: "Henry"}},
	}
	var result SQLiteTestUser
	err = sqliteQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, "Henry", result.Name)
	assert.Equal(t, 40, result.Age)
}

func TestSqlite_Transaction_Rollback(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	sqlite.Transaction()

	user1 := &SQLiteTestUser{Name: "Jack", Email: "jack@example.com", Age: 50}
	err1 := sqlite.Insert(user1)
	assert.NoError(t, err1)

	// 故意插入重复邮箱触发错误
	user2 := &SQLiteTestUser{Name: "Jack2", Email: "jack@example.com", Age: 51}
	err2 := sqlite.Insert(user2)
	assert.Error(t, err2, "应因唯一索引冲突触发回滚")

	// 用新实例查询验证
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "Email", Value: "jack@example.com"}},
	}
	var result SQLiteTestUser
	err = sqliteQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID, "事务应已回滚")
}

func TestSqlite_Transaction_Nested(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 第一次事务
	sqlite.Transaction()
	user1 := &SQLiteTestUser{Name: "Nested1", Email: "nested1@example.com", Age: 40}
	err = sqlite.Insert(user1)
	assert.NoError(t, err)
	err = sqlite.Commit()
	assert.NoError(t, err)

	// 第二次事务
	sqlite.Transaction()
	user2 := &SQLiteTestUser{Name: "Nested2", Email: "nested2@example.com", Age: 45}
	err = sqlite.Insert(user2)
	assert.NoError(t, err)
	err = sqlite.Commit()
	assert.NoError(t, err)

	// 验证两条数据都存在
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model: &SQLiteTestUser{},
	}
	var results []SQLiteTestUser
	err = sqliteQuery.Load(item, &results)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results), "应该有且仅有2条数据")
}

func TestSqlite_LargeTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大事务测试")
	}

	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	sqlite.Transaction()

	// 插入100条数据
	batchSize := 100
	for i := 0; i < batchSize; i++ {
		user := &SQLiteTestUser{
			Name:    fmt.Sprintf("LargeTx%d", i),
			Email:   fmt.Sprintf("largetx%d@example.com", i),
			Age:     20 + (i % 50),
			Balance: float64(i * 10),
		}
		err := sqlite.Insert(user)
		if err != nil {
			t.Fatalf("大事务插入失败 at %d: %v", i, err)
		}
	}

	err = sqlite.Commit()
	assert.NoError(t, err)

	// 等待事务完全提交
	time.Sleep(200 * time.Millisecond)

	// 使用新实例查询
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model: &SQLiteTestUser{},
		Size:  150, // 🔧 增加查询大小
	}
	var results []SQLiteTestUser
	err = sqliteQuery.Load(item, &results)
	assert.NoError(t, err)

	// 在内存中统计
	largeTxCount := 0
	for _, r := range results {
		if len(r.Name) >= 7 && r.Name[:7] == "LargeTx" {
			largeTxCount++
		}
	}

	assert.Equal(t, batchSize, largeTxCount,
		fmt.Sprintf("期望 %d 条,实际 %d 条,总记录 %d 条", batchSize, largeTxCount, len(results)))
}

func TestSqlite_Transaction_WithQuery(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 先插入一条数据
	user1 := &SQLiteTestUser{Name: "TxQuery1", Email: "txquery1@example.com", Age: 30}
	err = sqlite.Insert(user1)
	require.NoError(t, err)

	// 开启事务
	sqlite.Transaction()

	// 在事务中插入新数据
	user2 := &SQLiteTestUser{Name: "TxQuery2", Email: "txquery2@example.com", Age: 35}
	err = sqlite.Insert(user2)
	assert.NoError(t, err)

	// 在事务中查询
	item := &types.SearchItem{
		Model: &SQLiteTestUser{},
	}
	var results []SQLiteTestUser
	err = sqlite.Load(item, &results)
	assert.NoError(t, err)

	txQueryCount := 0
	for _, r := range results {
		if len(r.Name) >= 7 && r.Name[:7] == "TxQuery" {
			txQueryCount++
		}
	}

	assert.GreaterOrEqual(t, txQueryCount, 2,
		fmt.Sprintf("期望至少 2 条 TxQuery 记录,实际找到 %d 条", txQueryCount))

	err = sqlite.Commit()
	assert.NoError(t, err)
}

func TestSqlite_Transaction_WithUpdate(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 先插入数据
	user := &SQLiteTestUser{Name: "TxUpdate", Email: "txupdate@example.com", Age: 25}
	err = sqlite.Insert(user)
	require.NoError(t, err)

	// 开启事务
	sqlite.Transaction()

	// 在事务中更新
	user.Age = 26
	user.Balance = 100.00
	err = sqlite.Update(user)
	assert.NoError(t, err)

	err = sqlite.Commit()
	assert.NoError(t, err)

	// 验证更新
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "Email", Value: "txupdate@example.com"}},
	}
	var result SQLiteTestUser
	err = sqliteQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, 26, result.Age)
	assert.Equal(t, 100.00, result.Balance)
}

func TestSqlite_Transaction_WithDelete(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 插入数据
	user := &SQLiteTestUser{Name: "TxDelete", Email: "txdelete@example.com", Age: 30}
	err = sqlite.Insert(user)
	require.NoError(t, err)

	// 开启事务
	sqlite.Transaction()

	// 在事务中删除
	err = sqlite.Delete(user)
	assert.NoError(t, err)

	err = sqlite.Commit()
	assert.NoError(t, err)

	// 验证删除
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "Email", Value: "txdelete@example.com"}},
	}
	var result SQLiteTestUser
	err = sqliteQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID)
}

// ========================================
// SQL 操作测试
// ========================================

func TestSqlite_Raw(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	user := &SQLiteTestUser{Name: "Kate", Email: "kate@example.com", Age: 28}
	require.NoError(t, sqlite.Insert(user))

	var results []SQLiteTestUser
	err = sqlite.Raw("SELECT * FROM SQLiteTestUser WHERE Age > 25", &results)
	assert.NoError(t, err)
	assert.Greater(t, len(results), 0)
}

func TestSqlite_Exec(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	user := &SQLiteTestUser{Name: "Leo", Email: "leo@example.com", Age: 33}
	require.NoError(t, sqlite.Insert(user))

	// 直接使用GORM的Exec方法
	db, _ := sqlite.GetDB()
	result := db.Exec(fmt.Sprintf("UPDATE SQLiteTestUser SET Age = 34 WHERE ID = %d", user.ID))
	assert.NoError(t, result.Error)

	// 验证更新
	item := &types.SearchItem{
		Model:     &SQLiteTestUser{},
		WhereList: []*types.WhereItem{{Column: "ID", Value: user.ID}},
	}
	var resultUser SQLiteTestUser
	err = sqlite.Load(item, &resultUser)
	assert.NoError(t, err)
	assert.Equal(t, 34, resultUser.Age)
}

// ========================================
// 并发测试
// ========================================

func TestSqlite_ConcurrentInsert(t *testing.T) {
	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 等待表完全创建
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	errors := make(chan error, 10)
	count := 10

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sqliteInstance := setupTestSQLite(t)

			user := &SQLiteTestUser{
				Name:  fmt.Sprintf("User%d", idx),
				Email: fmt.Sprintf("user%d@example.com", idx),
				Age:   20 + idx,
			}

			if err := sqliteInstance.Insert(user); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 检查错误
	errorCount := 0
	for err := range errors {
		errorCount++
		t.Logf("并发插入错误: %v", err)
	}

	// SQLite 的并发写入限制更严格,允许更多失败
	assert.LessOrEqual(t, errorCount, 5, "错误数量应小于5")

	// 验证数据
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model: &SQLiteTestUser{},
	}
	var results []SQLiteTestUser
	err = sqliteQuery.Load(item, &results)
	assert.NoError(t, err)

	// 只统计 User 开头的记录
	userCount := 0
	for _, r := range results {
		if len(r.Name) >= 4 && r.Name[:4] == "User" {
			userCount++
		}
	}
	assert.GreaterOrEqual(t, userCount, count-errorCount,
		fmt.Sprintf("期望至少 %d 条,实际 %d 条,总记录 %d 条", count-errorCount, userCount, len(results)))
}

func TestSqlite_ConcurrentTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过并发事务测试")
	}

	sqlite := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite)

	err := sqlite.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	var wg sync.WaitGroup
	successCount := int32(0)
	count := 5

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			instance := setupTestSQLite(t)

			instance.Transaction()

			user := &SQLiteTestUser{
				Name:  fmt.Sprintf("ConcTx%d", idx),
				Email: fmt.Sprintf("conctx%d@example.com", idx),
				Age:   50 + idx,
			}

			if err := instance.Insert(user); err != nil {
				t.Logf("并发事务插入失败 %d: %v", idx, err)
				return
			}

			if err := instance.Commit(); err != nil {
				t.Logf("并发事务提交失败 %d: %v", idx, err)
				return
			}

			atomic.AddInt32(&successCount, 1)
		}(i)
	}

	wg.Wait()

	// 等待所有事务完成
	time.Sleep(200 * time.Millisecond)

	// SQLite 并发事务限制更严格
	assert.GreaterOrEqual(t, int(successCount), 1, "至少应有1个事务成功")

	// 使用新实例查询
	sqliteQuery := setupTestSQLite(t)
	item := &types.SearchItem{
		Model: &SQLiteTestUser{},
	}
	var results []SQLiteTestUser
	err = sqliteQuery.Load(item, &results)
	assert.NoError(t, err)

	concTxCount := 0
	for _, r := range results {
		if len(r.Name) >= 6 && r.Name[:6] == "ConcTx" {
			concTxCount++
		}
	}

	assert.Equal(t, int(successCount), concTxCount,
		fmt.Sprintf("期望 %d 条,实际 %d 条,总记录 %d 条", successCount, concTxCount, len(results)))
}

// ========================================
// 特殊功能测试
// ========================================

func TestSqlite_AttachDatabase(t *testing.T) {
	sqlite1 := setupTestSQLite(t)
	defer cleanupTestDataSQLite(t, sqlite1)

	// 创建第二个数据库
	sqlite2 := oltp.NewSqlite()
	sqlite2.Name = "sqlite_test_db2"
	defer sqlite2.DeleteDB()

	err := sqlite2.HasTable(&SQLiteTestUser{})
	require.NoError(t, err)

	// 在第二个数据库插入数据
	user := &SQLiteTestUser{Name: "Attach", Email: "attach@example.com", Age: 40}
	err = sqlite2.Insert(user)
	require.NoError(t, err)

	// 附加数据库
	err = sqlite1.AttachDatabase("db2", sqlite2.Path)
	assert.NoError(t, err)

	// 查询附加的数据库
	var results []SQLiteTestUser
	err = sqlite1.Raw("SELECT * FROM db2.SQLiteTestUser", &results)
	assert.NoError(t, err)
	assert.Greater(t, len(results), 0)

	// 分离数据库
	err = sqlite1.DetachDatabase("db2")
	assert.NoError(t, err)
}

// ========================================
// 测试套件入口
// ========================================

func TestSqliteSuite(t *testing.T) {
	t.Log("========================================")
	t.Log("开始运行 SQLite 完整测试套件")
	t.Log("========================================")

	startTime := time.Now()

	// 基础功能测试
	t.Run("基础功能测试", func(t *testing.T) {
		t.Run("NewSqlite创建实例", TestNewSqlite)
		t.Run("GetDB获取连接", TestSqlite_GetDB)
		t.Run("GetDB连接缓存", TestSqlite_GetDB_Cache)
		t.Run("DeleteDB删除数据库", TestSqlite_DeleteDB)
	})

	// 表管理测试
	t.Run("表管理测试", func(t *testing.T) {
		t.Run("HasTable创建表", TestSqlite_HasTable)
		t.Run("HasTable表缓存", TestSqlite_HasTable_Cache)
		t.Run("HasTable嵌套表", TestSqlite_HasTable_NestedTables)
	})

	// CRUD 操作测试
	t.Run("CRUD操作测试", func(t *testing.T) {
		t.Run("Insert插入数据", TestSqlite_Insert)
		t.Run("Insert重复数据", TestSqlite_Insert_Duplicate)
		t.Run("Update更新数据", TestSqlite_Update)
		t.Run("Delete删除数据", TestSqlite_Delete)
		t.Run("Load查询数据", TestSqlite_Load)
	})

	// 事务测试
	t.Run("事务测试", func(t *testing.T) {
		t.Run("Transaction提交", TestSqlite_Transaction)
		t.Run("Transaction回滚", TestSqlite_Transaction_Rollback)
		t.Run("Transaction嵌套", TestSqlite_Transaction_Nested)
		t.Run("Transaction大事务", TestSqlite_LargeTransaction)
		t.Run("Transaction查询", TestSqlite_Transaction_WithQuery)
		t.Run("Transaction更新", TestSqlite_Transaction_WithUpdate)
		t.Run("Transaction删除", TestSqlite_Transaction_WithDelete)
	})

	// SQL 操作测试
	t.Run("SQL操作测试", func(t *testing.T) {
		t.Run("Raw原始查询", TestSqlite_Raw)
		t.Run("Exec执行SQL", TestSqlite_Exec)
	})

	// 并发测试
	t.Run("并发测试", func(t *testing.T) {
		t.Run("ConcurrentInsert并发插入", TestSqlite_ConcurrentInsert)
		t.Run("ConcurrentTransactions并发事务", TestSqlite_ConcurrentTransactions)
	})

	// 特殊功能测试
	t.Run("特殊功能测试", func(t *testing.T) {
		t.Run("AttachDatabase附加数据库", TestSqlite_AttachDatabase)
	})

	duration := time.Since(startTime)
	t.Log("========================================")
	t.Logf("测试套件执行完毕，总耗时: %v", duration)
	t.Log("========================================")
}

// ========================================
// 快速测试
// ========================================

func TestSqliteQuick(t *testing.T) {
	t.Log("========================================")
	t.Log("SQLite 快速测试模式（核心功能）")
	t.Log("========================================")

	t.Run("基础连接", TestSqlite_GetDB)
	t.Run("插入数据", TestSqlite_Insert)
	t.Run("查询数据", TestSqlite_Load)
	t.Run("更新数据", TestSqlite_Update)
	t.Run("删除数据", TestSqlite_Delete)

	t.Log("快速测试完成")
}

// ========================================
// Benchmark 测试
// ========================================

func BenchmarkSqlite_Insert(b *testing.B) {
	config.INITSERVER = false
	sqlite := oltp.NewSqlite()
	sqlite.Name = "benchmark_test_db"
	sqlite.HasTable(&SQLiteTestUser{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user := &SQLiteTestUser{
			Name:  fmt.Sprintf("Bench%d", i),
			Email: fmt.Sprintf("bench%d@example.com", i),
			Age:   i % 100,
		}
		sqlite.Insert(user)
	}

	b.StopTimer()
	sqlite.DeleteDB()
}

func BenchmarkSqlite_Query(b *testing.B) {
	config.INITSERVER = false
	sqlite := oltp.NewSqlite()
	sqlite.Name = "benchmark_test_db"
	sqlite.HasTable(&SQLiteTestUser{})

	// 准备数据
	for i := 0; i < 100; i++ {
		user := &SQLiteTestUser{
			Name:  fmt.Sprintf("QueryBench%d", i),
			Email: fmt.Sprintf("querybench%d@example.com", i),
			Age:   i % 100,
		}
		sqlite.Insert(user)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item := &types.SearchItem{
			Model:     &SQLiteTestUser{},
			WhereList: []*types.WhereItem{{Column: "Age", Symbol: ">", Value: 50}},
		}
		var results []SQLiteTestUser
		sqlite.Load(item, &results)
	}

	b.StopTimer()
	sqlite.DeleteDB()
}
