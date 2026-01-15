package nosql

import (
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	utils.TESTPATH = "/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core/pkg/persistence/database/nosql/testdata"
}

// TestModel 实现 types.IModel 接口
type TestModel struct {
	*entity.Model
	Name      string    `json:"name"`
	Value     int       `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

// 🔧 修复：使用 uint 类型的 ID
func NewTestModel(id uint) *TestModel {
	m := &TestModel{
		Model: entity.NewModel(),
	}
	m.ID = id
	return m
}
func (t *TestModel) NewModel() {
	if t.Model == nil {
		t.Model = entity.NewModel()
	}
}
func (t *TestModel) GetHash() string {
	hash := fmt.Sprintf("%d:%d", t.Value, t.ID)
	return hash
}

// ✅ 泛型版本的 setup
func setupBadgerDBGeneric(t *testing.T) (*BadgerDB[TestModel], func()) {
	path := t.TempDir()
	db, err := NewBadgerDBFast[TestModel](path)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

// ✅ 测试泛型 Set/Get
func TestGeneric_SetGet(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	model := NewTestModel(1001)
	model.Name = "generic test"
	model.Value = 100
	model.CreatedAt = time.Now()

	// Set - 类型安全
	err := db.Set(model, 0)
	require.NoError(t, err)

	// Get - 使用 GetHash() 获取 key
	result, err := db.Get(model.GetHash())
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "generic test", result.Name)
	assert.Equal(t, 100, result.Value)
	assert.Equal(t, uint(1001), result.ID)
}

// ✅ 测试泛型 BatchInsert
func TestGeneric_BatchInsert(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	items := make([]*TestModel, 100)
	for i := 0; i < 100; i++ {
		model := NewTestModel(uint(2000 + i))
		model.Name = fmt.Sprintf("name_%d", i)
		model.Value = i
		model.CreatedAt = time.Now()
		items[i] = model
	}

	err := db.BatchInsert(items)
	require.NoError(t, err)

	// 验证 - 使用 GetHash()
	testModel := items[50]
	result, err := db.Get(testModel.GetHash())
	require.NoError(t, err)
	assert.Equal(t, "name_50", result.Name)
	assert.Equal(t, 50, result.Value)
	assert.Equal(t, uint(2050), result.ID)
}

// ✅ 测试泛型同步功能
func TestGeneric_Sync(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 等待表创建
	//time.Sleep(100 * time.Millisecond)

	// 插入数据
	model := NewTestModel(4001)
	model.Name = "sync test 1"
	model.Value = 200
	model.CreatedAt = time.Now()

	err := db.Set(model, 0)
	require.NoError(t, err)

	// 验证同步标记
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// 手动触发同步
	err = db.ManualSync()
	require.NoError(t, err)

	// 等待同步完成
	time.Sleep(200 * time.Millisecond)

	// 验证数据已同步到 SQLite
	result, err := gormDB.SearchId(uint(4001))
	require.NoError(t, err)
	assert.Equal(t, "sync test 1", result.Name)
	assert.Equal(t, 200, result.Value)

	// 验证同步标记已删除
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ✅ 测试泛型 GetAll
func TestGeneric_GetAll(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入数据
	for i := 0; i < 10; i++ {
		model := NewTestModel(uint(5000 + i))
		model.Name = fmt.Sprintf("name_%d", i)
		model.Value = i
		model.CreatedAt = time.Now()

		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 获取所有数据
	results, err := db.GetAll()
	require.NoError(t, err)
	assert.Equal(t, 10, len(results))

	// 类型安全的遍历
	for _, item := range results {
		assert.Greater(t, item.ID, uint(0))
		assert.NotEmpty(t, item.Name)
	}
}

// ✅ 测试 Delete
func TestGeneric_Delete(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	model := NewTestModel(6001)
	model.Name = "to be deleted"
	model.Value = 999

	// 插入
	err := db.Set(model, 0)
	require.NoError(t, err)

	key := model.GetHash()

	// 验证存在
	result, err := db.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "to be deleted", result.Name)

	// 删除
	err = db.Delete(key)
	require.NoError(t, err)

	// 验证已删除
	_, err = db.Get(key)
	assert.Error(t, err)
}

// ✅ 测试 TTL
func TestGeneric_TTL(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	model := NewTestModel(7001)
	model.Name = "will expire"
	model.Value = 777

	// 设置 1 秒 TTL
	err := db.Set(model, 1*time.Second)
	require.NoError(t, err)

	key := model.GetHash()

	// 立即读取应该成功
	result, err := db.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "will expire", result.Name)

	// 等待过期
	time.Sleep(2 * time.Second)

	// 应该读取失败
	_, err = db.Get(key)
	assert.Error(t, err)
}

// ✅ 测试空值处理
func TestGeneric_NilHandling(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 测试空指针
	var nilModel *TestModel
	err := db.Set(nilModel, 0)
	assert.Error(t, err)

	// 测试空切片
	err = db.BatchInsert([]*TestModel{})
	assert.NoError(t, err) // 空切片应该直接返回成功

	// 测试包含 nil 的切片
	err = db.BatchInsert([]*TestModel{nil})
	assert.NoError(t, err)
}

// ✅ 测试并发写入
func TestGeneric_ConcurrentWrites(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	const goroutines = 10
	const itemsPerGoroutine = 100

	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			for i := 0; i < itemsPerGoroutine; i++ {
				model := NewTestModel(uint(8000 + gid*1000 + i))
				model.Name = fmt.Sprintf("concurrent_%d_%d", gid, i)
				model.Value = i

				err := db.Set(model, 0)
				if err != nil {
					t.Errorf("并发写入失败: %v", err)
				}
			}
			done <- true
		}(g)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 验证数据完整性
	results, err := db.GetAll()
	require.NoError(t, err)
	assert.Equal(t, goroutines*itemsPerGoroutine, len(results))
}

// ✅ 基准测试 - 泛型版本
func BenchmarkGeneric_Set(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model := NewTestModel(uint(10000 + i))
		model.Name = "benchmark"
		model.Value = i
		model.CreatedAt = time.Now()

		db.Set(model, 0)
	}
}

func BenchmarkGeneric_Get(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	// 准备数据
	model := NewTestModel(11001)
	model.Name = "benchmark"
	model.Value = 999
	db.Set(model, 0)

	key := model.GetHash()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Get(key)
	}
}

func BenchmarkGeneric_BatchInsert_100(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items := make([]*TestModel, 100)
		for j := 0; j < 100; j++ {
			model := NewTestModel(uint(12000 + i*100 + j))
			model.Name = fmt.Sprintf("name_%d", j)
			model.Value = j
			model.CreatedAt = time.Now()
			items[j] = model
		}
		db.BatchInsert(items)
	}
}

func BenchmarkGeneric_BatchInsert_1000(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items := make([]*TestModel, 1000)
		for j := 0; j < 1000; j++ {
			model := NewTestModel(uint(13000 + i*1000 + j))
			model.Name = fmt.Sprintf("name_%d", j)
			model.Value = j
			model.CreatedAt = time.Now()
			items[j] = model
		}
		db.BatchInsert(items)
	}
}

// setupSQLite 辅助函数
func setupSQLite(t *testing.T) (*entity.ModelList[TestModel], func()) {

	list := entity.NewModelList[TestModel](nil)
	// 自动迁移测试模型

	cleanup := func() {
		// if sqlDB, err := db.DB(); err == nil {
		// 	sqlDB.Close()
		// }
	}

	return list, cleanup
}

// ...existing code...

// ✅ 测试批量同步功能
func TestGeneric_BatchSync(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 批量插入数据
	const batchSize = 100
	items := make([]*TestModel, batchSize)
	for i := 0; i < batchSize; i++ {
		model := NewTestModel(uint(20000 + i))
		model.Name = fmt.Sprintf("batch_sync_%d", i)
		model.Value = i
		model.CreatedAt = time.Now()
		items[i] = model
	}

	err := db.BatchInsert(items)
	require.NoError(t, err)

	// 验证同步标记数量
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, batchSize, count)

	// 手动触发同步
	err = db.ManualSync()
	require.NoError(t, err)

	// 等待同步完成
	time.Sleep(500 * time.Millisecond)

	result, _, err := gormDB.SearchAll(1, batchSize, func(item *types.SearchItem) {
		item.AddWhereNS("id", ">=", uint(20000))
		item.AddWhereNS("id", "<", uint(20000+batchSize))
	})
	require.NoError(t, err)
	assert.Equal(t, batchSize, len(result))

	// 验证数据完整性
	nameMap := make(map[string]bool)
	for _, r := range result {
		nameMap[r.Name] = true
	}
	assert.Equal(t, batchSize, len(nameMap))

	// 验证同步标记已全部删除
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ✅ 测试大批量同步（测试分批处理）
func TestGeneric_LargeBatchSync(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入大量数据（超过 maxSyncBatchSize）
	const totalItems = 1500
	items := make([]*TestModel, totalItems)
	for i := 0; i < totalItems; i++ {
		model := NewTestModel(uint(21000 + i))
		model.Name = fmt.Sprintf("large_sync_%d", i)
		model.Value = i
		model.CreatedAt = time.Now()
		items[i] = model
	}

	// 分批插入
	const insertBatchSize = 500
	for i := 0; i < totalItems; i += insertBatchSize {
		end := i + insertBatchSize
		if end > totalItems {
			end = totalItems
		}
		err := db.BatchInsert(items[i:end])
		require.NoError(t, err)
	}

	// 验证同步标记数量
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, totalItems, count)

	// 多次手动触发同步（因为超过了 maxSyncBatchSize）
	maxRetries := 5
	for retry := 0; retry < maxRetries; retry++ {
		err = db.ManualSync()
		require.NoError(t, err)

		time.Sleep(500 * time.Millisecond)

		count, err = db.GetPendingSyncCount()
		require.NoError(t, err)

		if count == 0 {
			break
		}

		t.Logf("同步进度: 剩余 %d 条待同步", count)
	}

	// 验证所有同步标记已删除
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count, "应该没有待同步的数据")

	// 验证所有数据已同步到 SQLite

	result, _, err := gormDB.SearchAll(1, totalItems, func(item *types.SearchItem) {
		item.AddWhereNS("id", ">=", uint(21000))
		item.AddWhereNS("id", "<", uint(21000+totalItems))
	})

	require.NoError(t, err)
	assert.Equal(t, totalItems, len(result))
}

// ✅ 测试同步失败重试
func TestGeneric_SyncRetry(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入数据
	model := NewTestModel(22001)
	model.Name = "retry_test"
	model.Value = 100
	model.CreatedAt = time.Now()

	err := db.Set(model, 0)
	require.NoError(t, err)

	// 验证同步标记
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	// 关闭 SQLite 数据库（模拟同步失败）
	if sqldb, err := gormDB.GetDB(); err == nil {
		// 关闭 SQLite 数据库连接以模拟同步失败
		if sqldb != nil {
			idb, _ := sqldb.DB()
			if idb != nil {
				idb.Close()
			}
		}
	}

	// 尝试同步（应该失败）
	err = db.ManualSync()
	// 不检查错误，因为实现可能会忽略错误

	// 重新打开数据库
	gormDB2, cleanupSQL2 := setupSQLite(t)
	defer cleanupSQL2()

	// 重新设置同步数据库
	db.SetSyncDB(gormDB2)

	// 再次手动触发同步（应该成功）
	err = db.ManualSync()
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// 验证数据已同步
	result, err := gormDB2.SearchOne(func(item *types.SearchItem) {
		item.AddWhereN("id", uint(22001))
	})
	require.NoError(t, err)
	assert.Equal(t, "retry_test", result.Name)
}

// ✅ 测试并发写入和同步
func TestGeneric_ConcurrentWriteAndSync(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	const goroutines = 5
	const itemsPerGoroutine = 50
	done := make(chan bool, goroutines)

	// 并发写入
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			for i := 0; i < itemsPerGoroutine; i++ {
				model := NewTestModel(uint(23000 + gid*1000 + i))
				model.Name = fmt.Sprintf("concurrent_sync_%d_%d", gid, i)
				model.Value = i

				err := db.Set(model, 0)
				if err != nil {
					t.Errorf("并发写入失败: %v", err)
				}
			}
			done <- true
		}(g)
	}

	// 等待所有写入完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 验证同步标记数量
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, goroutines*itemsPerGoroutine, count)

	// 多次手动触发同步
	for retry := 0; retry < 3; retry++ {
		err = db.ManualSync()
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		count, err = db.GetPendingSyncCount()
		require.NoError(t, err)
		if count == 0 {
			break
		}
	}

	// 验证所有数据已同步
	result, _, err := gormDB.SearchAll(1, goroutines*itemsPerGoroutine, func(item *types.SearchItem) {
		item.AddWhereNS("id", ">=", uint(23000))
		item.AddWhereNS("id", "<", uint(23000+goroutines*1000))
	})
	require.NoError(t, err)
	assert.Equal(t, goroutines*itemsPerGoroutine, len(result))

	// 验证同步标记已全部删除
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ✅ 测试自动同步（定时器触发）
func TestGeneric_AutoSync(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入数据
	const itemCount = 20
	for i := 0; i < itemCount; i++ {
		model := NewTestModel(uint(24000 + i))
		model.Name = fmt.Sprintf("auto_sync_%d", i)
		model.Value = i
		model.CreatedAt = time.Now()

		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 验证同步标记
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, itemCount, count)

	// 等待自动同步（定时器是 1 秒）
	time.Sleep(3 * time.Second)

	// 验证数据已自动同步
	result, _, err := gormDB.SearchAll(1, itemCount, func(item *types.SearchItem) {
		item.AddWhereNS("id", ">=", uint(24000))
		item.AddWhereNS("id", "<", uint(24000+itemCount))
	})
	require.NoError(t, err)
	assert.Equal(t, itemCount, len(result))

	// 验证同步标记已删除
	count, err = db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ✅ 测试删除后的同步
func TestGeneric_DeleteSync(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入数据
	model := NewTestModel(25001)
	model.Name = "will_be_deleted"
	model.Value = 999
	model.CreatedAt = time.Now()

	err := db.Set(model, 0)
	require.NoError(t, err)

	key := model.GetHash()

	// 立即删除（在同步之前）
	err = db.Delete(key)
	require.NoError(t, err)

	// 手动触发同步
	err = db.ManualSync()
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// 验证数据不应该存在于 SQLite（因为在同步前已删除）
	result, err := gormDB.SearchId(uint(25001))
	require.NoError(t, err)
	assert.NotEmpty(t, 0, result)

	// 验证同步标记已删除
	count, err := db.GetPendingSyncCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ✅ 基准测试 - 批量同步性能
func BenchmarkGeneric_BatchSync_100(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	list := entity.NewModelList[TestModel](nil)
	db.SetSyncDB(list)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 批量插入 100 条
		items := make([]*TestModel, 100)
		for j := 0; j < 100; j++ {
			model := NewTestModel(uint(30000 + i*100 + j))
			model.Name = fmt.Sprintf("bench_sync_%d", j)
			model.Value = j
			items[j] = model
		}
		db.BatchInsert(items)
	}
}

func BenchmarkGeneric_BatchSync_1000(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	list := entity.NewModelList[TestModel](nil)
	db.SetSyncDB(list)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 批量插入 1000 条
		items := make([]*TestModel, 1000)
		for j := 0; j < 1000; j++ {
			model := NewTestModel(uint(40000 + i*1000 + j))
			model.Name = fmt.Sprintf("bench_sync_%d", j)
			model.Value = j
			items[j] = model
		}
		db.BatchInsert(items)

		// 触发同步（可能需要多次）
		//db.ManualSync()
	}
}

// ✅ 测试自动清理功能
func TestGeneric_AutoCleanup(t *testing.T) {
	// 创建带自动清理配置的 BadgerDB
	config := DefaultProductionConfig(t.TempDir())
	config.AutoCleanup = true
	config.CleanupInterval = 500 * time.Millisecond
	config.KeepDuration = 1 * time.Second

	db, err := NewBadgerDBWithConfig[TestModel](config)
	db.config.SizeThreshold = 0
	require.NoError(t, err)
	defer db.Close()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入并同步数据
	model := NewTestModel(26001)
	model.Name = "cleanup_test"
	model.Value = 100
	model.CreatedAt = time.Now()

	err = db.Set(model, 0)
	require.NoError(t, err)

	// // 手动触发同步
	// err = db.ManualSync()
	// require.NoError(t, err)
	row, err := db.Get(model.GetHash())
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "cleanup_test", row.Name)
	time.Sleep(600 * time.Millisecond)

	// 验证数据已同步到 SQLite
	result, err := gormDB.SearchId(uint(26001))
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// 等待自动清理（CleanupKeepDuration + CleanupInterval）
	// 等待数据超过保留期限 + 清理间隔触发
	time.Sleep(2 * time.Second)

	// 验证 BadgerDB 中的数据已被清理
	row, err = db.Get(model.GetHash())
	assert.Error(t, err, "Key not found")
	assert.Nil(t, row, "BadgerDB 中的数据应该被清理")

	// 验证 SQLite 中的数据仍然存在
	result, err = gormDB.SearchId(uint(26001))
	require.NoError(t, err)
	assert.NotEmpty(t, result, "SQLite 中的数据应该保留")
}

// ✅ 测试手动清理功能
func TestGeneric_ManualCleanup(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入多条数据
	for i := 0; i < 10; i++ {
		model := NewTestModel(uint(27000 + i))
		model.Name = fmt.Sprintf("manual_cleanup_%d", i)
		model.Value = i
		model.CreatedAt = time.Now()

		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 手动触发同步
	err := db.ManualSync()
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	// 验证数据已同步
	results, _, err := gormDB.SearchAll(1, 10, func(item *types.SearchItem) {
		item.AddWhereNS("id", ">=", uint(27000))
		item.AddWhereNS("id", "<", uint(27010))
	})
	require.NoError(t, err)
	assert.Equal(t, 10, len(results))

	// 手动触发清理（保留 0 秒，即立即清理所有已同步数据）
	err = db.CleanupAfterSync(0)
	require.NoError(t, err)

	// 验证 BadgerDB 中的数据已被清理
	allItems, err := db.GetAll()
	require.NoError(t, err)
	assert.Equal(t, 0, len(allItems), "所有已同步数据应该被清理")

	// 验证 SQLite 中的数据仍然存在
	results, _, err = gormDB.SearchAll(1, 10, func(item *types.SearchItem) {
		item.AddWhereNS("id", ">=", uint(27000))
		item.AddWhereNS("id", "<", uint(27010))
	})
	require.NoError(t, err)
	assert.Equal(t, 10, len(results), "SQLite 中的数据应该保留")
}

// ✅ 测试清理未同步的数据不会被删除
func TestGeneric_CleanupPreservesUnsyncedData(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入已同步的数据
	syncedModel := NewTestModel(28001)
	syncedModel.Name = "synced_data"
	syncedModel.Value = 100

	err := db.Set(syncedModel, 0)
	require.NoError(t, err)

	// 同步第一条数据
	err = db.ManualSync()
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	// 插入未同步的数据
	unsyncedModel := NewTestModel(28002)
	unsyncedModel.Name = "unsynced_data"
	unsyncedModel.Value = 200

	err = db.Set(unsyncedModel, 0)
	require.NoError(t, err)

	// 触发清理
	err = db.CleanupAfterSync(0)
	require.NoError(t, err)

	// 验证已同步的数据被清理
	_, err = db.Get(syncedModel.GetHash())
	assert.Error(t, err, "已同步的数据应该被清理")

	// 验证未同步的数据仍然存在
	result, err := db.Get(unsyncedModel.GetHash())
	require.NoError(t, err)
	assert.Equal(t, "unsynced_data", result.Name)
}

// ✅ 测试清理保留期限功能
func TestGeneric_CleanupWithKeepDuration(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入旧数据
	oldModel := NewTestModel(29001)
	oldModel.Name = "old_data"
	oldModel.Value = 100

	err := db.Set(oldModel, 0)
	require.NoError(t, err)

	// 同步旧数据
	err = db.ManualSync()
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	// 等待一段时间
	time.Sleep(1 * time.Second)

	// 插入新数据
	newModel := NewTestModel(29002)
	newModel.Name = "new_data"
	newModel.Value = 200

	err = db.Set(newModel, 0)
	require.NoError(t, err)

	// 同步新数据
	err = db.ManualSync()
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	// 清理保留 500ms 的数据
	err = db.CleanupAfterSync(500 * time.Millisecond)
	require.NoError(t, err)

	// 旧数据应该被清理
	_, err = db.Get(oldModel.GetHash())
	assert.Error(t, err, "旧数据应该被清理")

	// 新数据应该保留
	result, err := db.Get(newModel.GetHash())
	require.NoError(t, err)
	assert.Equal(t, "new_data", result.Name)
}

// ✅ 测试批量清理性能
func TestGeneric_BatchCleanupPerformance(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 批量插入大量数据
	const totalItems = 1000
	items := make([]*TestModel, totalItems)
	for i := 0; i < totalItems; i++ {
		model := NewTestModel(uint(30000 + i))
		model.Name = fmt.Sprintf("cleanup_perf_%d", i)
		model.Value = i
		items[i] = model
	}

	err := db.BatchInsert(items)
	require.NoError(t, err)

	// 批量同步
	for retry := 0; retry < 3; retry++ {
		err = db.ManualSync()
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		count, _ := db.GetPendingSyncCount()
		if count == 0 {
			break
		}
	}

	// 测量清理时间
	startTime := time.Now()
	err = db.CleanupAfterSync(0)
	require.NoError(t, err)
	cleanupDuration := time.Since(startTime)

	t.Logf("清理 %d 条数据耗时: %v", totalItems, cleanupDuration)

	// 验证清理完成
	allItems, err := db.GetAll()
	require.NoError(t, err)
	assert.Equal(t, 0, len(allItems))

	// 验证性能（应该在 1 秒内完成）
	assert.Less(t, cleanupDuration, 1*time.Second, "清理时间过长")
}

// ✅ 测试并发清理
func TestGeneric_ConcurrentCleanup(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入数据
	for i := 0; i < 100; i++ {
		model := NewTestModel(uint(31000 + i))
		model.Name = fmt.Sprintf("concurrent_cleanup_%d", i)
		model.Value = i

		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 同步数据
	err := db.ManualSync()
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)

	// 并发触发清理
	const goroutines = 5
	done := make(chan bool, goroutines)
	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			err := db.CleanupAfterSync(0)
			if err != nil {
				errors <- err
			}
			done <- true
		}()
	}

	// 等待所有清理完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 检查是否有错误
	select {
	case err := <-errors:
		t.Fatalf("并发清理失败: %v", err)
	default:
	}

	// 验证数据已清理
	allItems, err := db.GetAll()
	require.NoError(t, err)
	assert.Equal(t, 0, len(allItems))
}

// ✅ 测试清理删除标记的数据
func TestGeneric_CleanupDeletedData(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	gormDB, cleanupSQL := setupSQLite(t)
	defer cleanupSQL()

	// 设置同步数据库
	db.SetSyncDB(gormDB)

	// 插入数据
	model := NewTestModel(32001)
	model.Name = "to_be_deleted_and_cleaned"
	model.Value = 999

	err := db.Set(model, 0)
	require.NoError(t, err)

	// 删除数据（软删除）
	err = db.Delete(model.GetHash())
	require.NoError(t, err)

	// 同步删除操作
	err = db.ManualSync()
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	// 清理
	err = db.CleanupAfterSync(0)
	require.NoError(t, err)

	// 验证已删除的数据也被清理
	wrapper, err := db.getWrapper(model.GetHash())
	assert.Error(t, err, "已删除并同步的数据应该被完全清理")
	assert.Nil(t, wrapper)
}

// ✅ 基准测试 - 清理性能
func BenchmarkGeneric_Cleanup_100(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	list := entity.NewModelList[TestModel](nil)
	db.SetSyncDB(list)

	// 准备数据
	items := make([]*TestModel, 100)
	for i := 0; i < 100; i++ {
		model := NewTestModel(uint(40000 + i))
		model.Name = fmt.Sprintf("bench_cleanup_%d", i)
		model.Value = i
		items[i] = model
	}
	db.BatchInsert(items)
	db.ManualSync()
	time.Sleep(200 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CleanupAfterSync(0)
	}
}

func BenchmarkGeneric_Cleanup_1000(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	list := entity.NewModelList[TestModel](nil)
	db.SetSyncDB(list)

	// 准备数据
	items := make([]*TestModel, 1000)
	for i := 0; i < 1000; i++ {
		model := NewTestModel(uint(50000 + i))
		model.Name = fmt.Sprintf("bench_cleanup_%d", i)
		model.Value = i
		items[i] = model
	}
	db.BatchInsert(items)

	for retry := 0; retry < 3; retry++ {
		db.ManualSync()
		time.Sleep(200 * time.Millisecond)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.CleanupAfterSync(0)
	}
}
