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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试模型
type TestUser struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Name      string    `gorm:"size:100"`
	Email     string    `gorm:"size:100;uniqueIndex"`
	Age       int       `gorm:"default:0"`
	Balance   float64   `gorm:"type:decimal(10,2);default:0"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (TestUser) GetRemoteDBName() string {
	return "test_db"
}

func (TestUser) GetLocalDBName() string {
	return "test_db"
}

// 嵌套表测试
type TestOrder struct {
	ID        int64           `gorm:"primaryKey;autoIncrement"`
	UserID    int64           `gorm:"index"`
	Amount    float64         `gorm:"type:decimal(10,2)"`
	Items     []TestOrderItem `gorm:"foreignKey:OrderID"`
	CreatedAt time.Time       `gorm:"autoCreateTime"`
}

func (TestOrder) GetRemoteDBName() string {
	return "test_db"
}

func (TestOrder) GetLocalDBName() string {
	return "test_db"
}

type TestOrderItem struct {
	ID       int64   `gorm:"primaryKey;autoIncrement"`
	OrderID  int64   `gorm:"index"`
	Product  string  `gorm:"size:100"`
	Price    float64 `gorm:"type:decimal(10,2)"`
	Quantity int     `gorm:"default:1"`
}

func (TestOrderItem) GetRemoteDBName() string {
	return "test_db"
}

func (TestOrderItem) GetLocalDBName() string {
	return "test_db"
}

// 测试辅助函数
func setupTestMySQL(t *testing.T) *oltp.Mysql {
	config.INITSERVER = false
	mysql := oltp.NewMysql(
		"localhost",
		"root",
		"123456Test",
		3306,
		false, // 日志关闭
		true,  // 自动建表
	)
	mysql.Name = "test_db"

	// 确保测试数据库存在
	db, err := mysql.GetDB()
	require.NoError(t, err)
	require.NotNil(t, db)

	return mysql
}

// 🔧 新增：测试前清理函数
func cleanupBeforeTest(t *testing.T, mysql *oltp.Mysql) {
	db, _ := mysql.GetDB()
	if db != nil {
		// 删除所有测试表
		db.Exec("DROP TABLE IF EXISTS TestOrderItem")
		db.Exec("DROP TABLE IF EXISTS TestOrder")
		db.Exec("DROP TABLE IF EXISTS TestUser")
	}

	// 清除表缓存
	oltp.ClearMysqlTableCache()

	// 等待MySQL完成删除操作
	time.Sleep(50 * time.Millisecond)
}

func cleanupTestData(t *testing.T, mysql *oltp.Mysql) {
	db, _ := mysql.GetDB()
	if db != nil {
		db.Exec("DROP TABLE IF EXISTS TestOrderItem")
		db.Exec("DROP TABLE IF EXISTS TestOrder")
		db.Exec("DROP TABLE IF EXISTS TestUser")
	}

	// 清除表缓存
	oltp.ClearMysqlTableCache()
}

// ========================================
// 基础功能测试
// ========================================

func TestNewMysql(t *testing.T) {
	mysql := oltp.NewMysql("localhost", "root", "123456Test", 3306, true, true)

	assert.NotNil(t, mysql)
	assert.Equal(t, "localhost", mysql.Host)
	assert.Equal(t, uint(3306), mysql.Port)
	assert.Equal(t, "root", mysql.User)
	assert.Equal(t, "123456Test", mysql.Pass)
	assert.Equal(t, uint(100), mysql.ConMax)
	assert.Equal(t, uint(20), mysql.ConPool)
	assert.True(t, mysql.IsLog)
	assert.True(t, mysql.AutoTable)
}

func TestMysql_GetDB(t *testing.T) {
	mysql := setupTestMySQL(t)
	defer cleanupTestData(t, mysql)

	db, err := mysql.GetDB()
	assert.NoError(t, err)
	assert.NotNil(t, db)

	// 测试连接健康
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	assert.NoError(t, sqlDB.Ping())
}

func TestMysql_GetDB_Cache(t *testing.T) {
	mysql := setupTestMySQL(t)
	defer cleanupTestData(t, mysql)

	db1, err1 := mysql.GetDB()
	db2, err2 := mysql.GetDB()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Equal(t, db1, db2, "应返回相同的连接实例")
}

// ========================================
// 表管理测试
// ========================================

func TestMysql_HasTable(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	user := &TestUser{}
	err := mysql.HasTable(user)
	assert.NoError(t, err)

	// 验证表是否真的创建
	db, _ := mysql.GetDB()
	var count int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?",
		"test_db", "TestUser").Scan(&count)
	assert.Equal(t, int64(1), count)
}

func TestMysql_HasTable_Cache(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	user := &TestUser{}
	err1 := mysql.HasTable(user)
	err2 := mysql.HasTable(user)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

func TestMysql_HasTable_NestedTables(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	order := &TestOrder{}
	err := mysql.HasTable(order)
	assert.NoError(t, err)

	// 验证主表和嵌套表都创建
	db, _ := mysql.GetDB()
	var countOrder, countItem int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?",
		"test_db", "TestOrder").Scan(&countOrder)
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?",
		"test_db", "TestOrderItem").Scan(&countItem)

	assert.Equal(t, int64(1), countOrder)
	assert.Equal(t, int64(1), countItem)
}

// ========================================
// CRUD 操作测试
// ========================================

func TestMysql_Insert(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	user := &TestUser{
		Name:    "Alice",
		Email:   "alice@example.com",
		Age:     25,
		Balance: 100.50,
	}

	err := mysql.Insert(user)
	assert.NoError(t, err)
	assert.Greater(t, user.ID, int64(0))
}

func TestMysql_Insert_Duplicate(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	user1 := &TestUser{Name: "Bob", Email: "bob@example.com", Age: 30}
	user2 := &TestUser{Name: "Bob2", Email: "bob@example.com", Age: 31}

	err1 := mysql.Insert(user1)
	err2 := mysql.Insert(user2)

	assert.NoError(t, err1)
	assert.Error(t, err2, "应因唯一索引冲突报错")
}

func TestMysql_Update(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	user := &TestUser{Name: "Charlie", Email: "charlie@example.com", Age: 28}
	err = mysql.Insert(user)
	require.NoError(t, err)

	user.Age = 29
	user.Balance = 200.00
	err = mysql.Update(user)
	assert.NoError(t, err)

	// 验证更新
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "ID", Value: user.ID}},
	}
	var result TestUser
	err = mysql.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, 29, result.Age)
	assert.Equal(t, 200.00, result.Balance)
}

func TestMysql_Delete(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	user := &TestUser{Name: "David", Email: "david@example.com", Age: 35}
	err = mysql.Insert(user)
	require.NoError(t, err)

	err = mysql.Delete(user)
	assert.NoError(t, err)

	// 验证删除
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "ID", Value: user.ID}},
	}
	var result TestUser
	err = mysql.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID, "记录应不存在")
}

func TestMysql_Load(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	// 插入测试数据
	users := []*TestUser{
		{Name: "Eve", Email: "eve@example.com", Age: 22, Balance: 50.00},
		{Name: "Frank", Email: "frank@example.com", Age: 27, Balance: 150.00},
		{Name: "Grace", Email: "grace@example.com", Age: 32, Balance: 250.00},
	}
	for _, u := range users {
		require.NoError(t, mysql.Insert(u))
	}

	// 测试查询单条
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Name", Value: "Eve"}},
	}
	var result TestUser
	err = mysql.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, "Eve", result.Name)
	assert.Equal(t, 22, result.Age)

	// 测试查询多条
	item2 := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Age", Symbol: ">", Value: 25}},
	}
	var results []TestUser
	err = mysql.Load(item2, &results)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results))
}

// ========================================
// 事务测试
// ========================================

func TestMysql_Transaction(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	mysql.Transaction()

	user1 := &TestUser{Name: "Henry", Email: "henry@example.com", Age: 40}
	user2 := &TestUser{Name: "Iris", Email: "iris@example.com", Age: 45}

	err1 := mysql.Insert(user1)
	err2 := mysql.Insert(user2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	err = mysql.Commit()
	assert.NoError(t, err)

	// 使用新实例查询
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Name", Value: "Henry"}},
	}
	var result TestUser
	err = mysqlQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, "Henry", result.Name)
	assert.Equal(t, 40, result.Age)
}

func TestMysql_Transaction_Rollback(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	mysql.Transaction()

	user1 := &TestUser{Name: "Jack", Email: "jack@example.com", Age: 50}
	err1 := mysql.Insert(user1)
	assert.NoError(t, err1)

	// 故意插入重复邮箱触发错误
	user2 := &TestUser{Name: "Jack2", Email: "jack@example.com", Age: 51}
	err2 := mysql.Insert(user2)
	assert.Error(t, err2, "应因唯一索引冲突触发回滚")

	// 用新实例查询验证
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Email", Value: "jack@example.com"}},
	}
	var result TestUser
	err = mysqlQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID, "事务应已回滚")
}

func TestMysql_Transaction_Timeout(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	mysql.Transaction()

	user := &TestUser{Name: "TimeoutTest", Email: "timeout@example.com", Age: 30}
	err = mysql.Insert(user)
	assert.NoError(t, err)

	// 模拟超时 - 5秒后手动回滚
	time.Sleep(5 * time.Second)
	err = mysql.Rollback()
	assert.NoError(t, err)

	// 验证数据未提交
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Name", Value: "TimeoutTest"}},
	}
	var result TestUser
	err = mysqlQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID)
}

func TestMysql_Transaction_NestedError(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	// 第一次事务
	mysql.Transaction()
	user1 := &TestUser{Name: "Nested1", Email: "nested1@example.com", Age: 40}
	err = mysql.Insert(user1)
	assert.NoError(t, err)
	err = mysql.Commit()
	assert.NoError(t, err)

	// 第二次事务 - 应该可以正常开启
	mysql.Transaction()
	user2 := &TestUser{Name: "Nested2", Email: "nested2@example.com", Age: 45}
	err = mysql.Insert(user2)
	assert.NoError(t, err)
	err = mysql.Commit()
	assert.NoError(t, err)

	// 验证两条数据都存在
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model: &TestUser{},
	}
	var results []TestUser
	err = mysqlQuery.Load(item, &results)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results), "应该有且仅有2条数据")
}

func TestMysql_ConcurrentTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过并发事务测试")
	}

	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	var wg sync.WaitGroup
	successCount := int32(0)
	count := 5

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			instance := setupTestMySQL(t)

			instance.Transaction()

			user := &TestUser{
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

	assert.GreaterOrEqual(t, int(successCount), count/2, "至少一半事务应成功")

	// 使用新实例查询
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model: &TestUser{},
	}
	var results []TestUser
	err = mysqlQuery.Load(item, &results)
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

func TestMysql_LargeTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大事务测试")
	}

	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	mysql.Transaction()

	// 插入100条数据
	batchSize := 100
	for i := 0; i < batchSize; i++ {
		user := &TestUser{
			Name:    fmt.Sprintf("LargeTx%d", i),
			Email:   fmt.Sprintf("largetx%d@example.com", i),
			Age:     20 + (i % 50),
			Balance: float64(i * 10),
		}
		err := mysql.Insert(user)
		if err != nil {
			t.Fatalf("大事务插入失败 at %d: %v", i, err)
		}
	}

	err = mysql.Commit()
	assert.NoError(t, err)

	// 等待事务完全提交
	time.Sleep(200 * time.Millisecond)

	// 使用新实例查询
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model: &TestUser{},
		Size:  100,
	}
	var results []TestUser
	err = mysqlQuery.Load(item, &results)
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

func TestMysql_Transaction_WithQuery(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	// 先插入一条数据
	user1 := &TestUser{Name: "TxQuery1", Email: "txquery1@example.com", Age: 30}
	err = mysql.Insert(user1)
	require.NoError(t, err)

	// 开启事务
	mysql.Transaction()

	// 在事务中插入新数据
	user2 := &TestUser{Name: "TxQuery2", Email: "txquery2@example.com", Age: 35}
	err = mysql.Insert(user2)
	assert.NoError(t, err)

	// 在事务中查询
	item := &types.SearchItem{
		Model: &TestUser{},
	}
	var results []TestUser
	err = mysql.Load(item, &results)
	assert.NoError(t, err)

	txQueryCount := 0
	for _, r := range results {
		if len(r.Name) >= 7 && r.Name[:7] == "TxQuery" {
			txQueryCount++
		}
	}

	assert.GreaterOrEqual(t, txQueryCount, 2,
		fmt.Sprintf("期望至少 2 条 TxQuery 记录,实际找到 %d 条", txQueryCount))

	err = mysql.Commit()
	assert.NoError(t, err)
}

func TestMysql_Transaction_WithUpdate(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	// 先插入数据
	user := &TestUser{Name: "TxUpdate", Email: "txupdate@example.com", Age: 25}
	err = mysql.Insert(user)
	require.NoError(t, err)

	// 开启事务
	mysql.Transaction()

	// 在事务中更新
	user.Age = 26
	user.Balance = 100.00
	err = mysql.Update(user)
	assert.NoError(t, err)

	err = mysql.Commit()
	assert.NoError(t, err)

	// 验证更新
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Email", Value: "txupdate@example.com"}},
	}
	var result TestUser
	err = mysqlQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, 26, result.Age)
	assert.Equal(t, 100.00, result.Balance)
}

func TestMysql_Transaction_WithDelete(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	// 插入数据
	user := &TestUser{Name: "TxDelete", Email: "txdelete@example.com", Age: 30}
	err = mysql.Insert(user)
	require.NoError(t, err)

	// 开启事务
	mysql.Transaction()

	// 在事务中删除
	err = mysql.Delete(user)
	assert.NoError(t, err)

	err = mysql.Commit()
	assert.NoError(t, err)

	// 验证删除
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "Email", Value: "txdelete@example.com"}},
	}
	var result TestUser
	err = mysqlQuery.Load(item, &result)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.ID)
}

// ========================================
// SQL 操作测试
// ========================================

func TestMysql_Raw(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	user := &TestUser{Name: "Kate", Email: "kate@example.com", Age: 28}
	require.NoError(t, mysql.Insert(user))

	var results []TestUser
	err = mysql.Raw("SELECT * FROM TestUser WHERE Age > 25", &results)
	assert.NoError(t, err)
	assert.Greater(t, len(results), 0)
}

func TestMysql_Exec(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	user := &TestUser{Name: "Leo", Email: "leo@example.com", Age: 33}
	require.NoError(t, mysql.Insert(user))

	// 直接使用GORM的Exec方法
	db, _ := mysql.GetDB()
	result := db.Exec(fmt.Sprintf("UPDATE TestUser SET Age = 34 WHERE ID = %d", user.ID))
	assert.NoError(t, result.Error)

	// 验证更新
	item := &types.SearchItem{
		Model:     &TestUser{},
		WhereList: []*types.WhereItem{{Column: "ID", Value: user.ID}},
	}
	var resultUser TestUser
	err = mysql.Load(item, &resultUser)
	assert.NoError(t, err)
	assert.Equal(t, 34, resultUser.Age)
}

// ========================================
// 连接管理测试
// ========================================

func TestMysql_RecreateConnection(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	// 先获取连接
	db1, _ := mysql.GetDB()

	// 强制重建
	err := mysql.RecreateConnection()
	assert.NoError(t, err)

	db2, _ := mysql.GetDB()
	assert.NotNil(t, db2)
	assert.NotEqual(t, db1, db2, "应创建新连接")
}

// ========================================
// 并发测试
// ========================================

func TestMysql_ConcurrentInsert(t *testing.T) {
	mysql := setupTestMySQL(t)
	cleanupBeforeTest(t, mysql)
	defer cleanupTestData(t, mysql)

	err := mysql.HasTable(&TestUser{})
	require.NoError(t, err)

	// 等待表完全创建
	time.Sleep(200 * time.Millisecond)

	var wg sync.WaitGroup
	errors := make(chan error, 10)
	count := 10

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			mysqlInstance := setupTestMySQL(t)

			user := &TestUser{
				Name:  fmt.Sprintf("User%d", idx),
				Email: fmt.Sprintf("user%d@example.com", idx),
				Age:   20 + idx,
			}

			if err := mysqlInstance.Insert(user); err != nil {
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

	assert.LessOrEqual(t, errorCount, 2, "错误数量应小于2")

	// 验证数据
	mysqlQuery := setupTestMySQL(t)
	item := &types.SearchItem{
		Model: &TestUser{},
	}
	var results []TestUser
	err = mysqlQuery.Load(item, &results)
	assert.NoError(t, err)

	// 只统计 User 开头的记录
	userCount := 0
	for _, r := range results {
		if len(r.Name) >= 4 && r.Name[:4] == "User" {
			userCount++
		}
	}
	assert.Equal(t, count-errorCount, userCount,
		fmt.Sprintf("期望 %d 条,实际 %d 条,总记录 %d 条", count-errorCount, userCount, len(results)))
}

// ========================================
// 测试套件入口
// ========================================

func TestMysqlSuite(t *testing.T) {
	t.Log("========================================")
	t.Log("开始运行 MySQL 完整测试套件")
	t.Log("========================================")

	startTime := time.Now()

	// 基础功能测试
	t.Run("基础功能测试", func(t *testing.T) {
		t.Run("NewMysql创建实例", TestNewMysql)
		t.Run("GetDB获取连接", TestMysql_GetDB)
		t.Run("GetDB连接缓存", TestMysql_GetDB_Cache)
	})

	// 表管理测试
	t.Run("表管理测试", func(t *testing.T) {
		t.Run("HasTable创建表", TestMysql_HasTable)
		t.Run("HasTable表缓存", TestMysql_HasTable_Cache)
		t.Run("HasTable嵌套表", TestMysql_HasTable_NestedTables)
	})

	// CRUD 操作测试
	t.Run("CRUD操作测试", func(t *testing.T) {
		t.Run("Insert插入数据", TestMysql_Insert)
		t.Run("Insert重复数据", TestMysql_Insert_Duplicate)
		t.Run("Update更新数据", TestMysql_Update)
		t.Run("Delete删除数据", TestMysql_Delete)
		t.Run("Load查询数据", TestMysql_Load)
	})

	// 事务测试
	t.Run("事务测试", func(t *testing.T) {
		t.Run("Transaction提交", TestMysql_Transaction)
		t.Run("Transaction回滚", TestMysql_Transaction_Rollback)
		t.Run("Transaction超时", TestMysql_Transaction_Timeout)
		t.Run("Transaction嵌套", TestMysql_Transaction_NestedError)
		t.Run("Transaction并发", TestMysql_ConcurrentTransactions)
		t.Run("Transaction大事务", TestMysql_LargeTransaction)
		t.Run("Transaction查询", TestMysql_Transaction_WithQuery)
		t.Run("Transaction更新", TestMysql_Transaction_WithUpdate)
		t.Run("Transaction删除", TestMysql_Transaction_WithDelete)
	})

	// SQL 操作测试
	t.Run("SQL操作测试", func(t *testing.T) {
		t.Run("Raw原始查询", TestMysql_Raw)
		t.Run("Exec执行SQL", TestMysql_Exec)
	})

	// 连接管理测试
	t.Run("连接管理测试", func(t *testing.T) {
		t.Run("RecreateConnection重建连接", TestMysql_RecreateConnection)
	})

	// 并发测试
	t.Run("并发测试", func(t *testing.T) {
		t.Run("ConcurrentInsert并发插入", TestMysql_ConcurrentInsert)
	})

	duration := time.Since(startTime)
	t.Log("========================================")
	t.Logf("测试套件执行完毕，总耗时: %v", duration)
	t.Log("========================================")
}

// ========================================
// 快速测试
// ========================================

func TestQuick(t *testing.T) {
	t.Log("========================================")
	t.Log("快速测试模式（核心功能）")
	t.Log("========================================")

	t.Run("基础连接", TestMysql_GetDB)
	t.Run("插入数据", TestMysql_Insert)
	t.Run("查询数据", TestMysql_Load)
	t.Run("更新数据", TestMysql_Update)
	t.Run("删除数据", TestMysql_Delete)

	t.Log("快速测试完成")
}

// ========================================
// Benchmark 测试
// ========================================

func BenchmarkMysql_Insert(b *testing.B) {
	config.INITSERVER = false
	mysql := oltp.NewMysql("localhost", "root", "123456Test", 3306, false, true)
	mysql.Name = "test_db"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user := &TestUser{
			Name:  fmt.Sprintf("Bench%d", i),
			Email: fmt.Sprintf("bench%d@example.com", i),
			Age:   i % 100,
		}
		mysql.Insert(user)
	}
}

func BenchmarkMysql_Query(b *testing.B) {
	config.INITSERVER = false
	mysql := oltp.NewMysql("localhost", "root", "123456Test", 3306, false, true)
	mysql.Name = "test_db"

	// 准备数据
	for i := 0; i < 100; i++ {
		user := &TestUser{
			Name:  fmt.Sprintf("QueryBench%d", i),
			Email: fmt.Sprintf("querybench%d@example.com", i),
			Age:   i % 100,
		}
		mysql.Insert(user)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item := &types.SearchItem{
			Model:     &TestUser{},
			WhereList: []*types.WhereItem{{Column: "Age", Symbol: ">", Value: 50}},
		}
		var results []TestUser
		mysql.Load(item, &results)
	}
}
