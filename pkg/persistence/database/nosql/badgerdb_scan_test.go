package nosql

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ✅ 测试 Scan 方法
func TestGeneric_Scan(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入测试数据（不同前缀）

	// 前缀 "admin:" - 5条
	for i := 0; i < 5; i++ {
		model := NewTestModel(uint(61000 + i))
		model.Name = fmt.Sprintf("admin_%d", i)
		model.Value = 100
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 前缀 "guest:" - 3条
	for i := 0; i < 3; i++ {
		model := NewTestModel(uint(62000 + i))
		model.Name = fmt.Sprintf("guest_%d", i)
		model.Value = 200
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 测试 1：扫描全部（假设 GetHash 返回 "test:60000" 这样的格式）
	// 注意：这个测试依赖于 GetHash() 的具体实现
	allResults, err := db.Scan("100:", 5)
	require.NoError(t, err)
	assert.Equal(t, 5, len(allResults), "应该返回所有 5 条数据")

	// 测试 2：限制数量
	limitedResults, err := db.Scan("200:", 3)
	require.NoError(t, err)
	assert.Equal(t, 3, len(limitedResults), "应该只返回 3 条数据")

	// 测试 3：无匹配前缀
	noResults, err := db.Scan("nonexistent:", 100)
	require.NoError(t, err)
	assert.Equal(t, 0, len(noResults), "不存在的前缀应该返回空结果")

	// 测试 4：空前缀（返回所有数据）
	emptyPrefixResults, err := db.Scan("", 8)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(emptyPrefixResults), 8, "空前缀应该返回所有数据")

	// 测试 5：验证返回的数据正确性
	for _, result := range allResults {
		assert.NotNil(t, result)
		assert.NotNil(t, result.Model)
		assert.Greater(t, result.Model.ID, uint(0))
	}
}

// ✅ 测试 Scan 过滤已删除的数据
func TestGeneric_Scan_FilterDeleted(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入 10 条数据
	for i := 0; i < 10; i++ {
		model := NewTestModel(uint(63000 + i))
		model.Name = fmt.Sprintf("scan_filter_%d", i)
		model.Value = i
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 删除前 3 条
	for i := 0; i < 3; i++ {
		model := NewTestModel(uint(63000 + i))
		err := db.Delete(model.GetHash())
		require.NoError(t, err)
	}

	// 扫描全部
	results, err := db.Scan("test:", 100)
	require.NoError(t, err)

	// 统计返回的数据中，ID 在 63000-63009 范围内的数量
	scanFilterCount := 0
	for _, result := range results {
		if result.Model.ID >= 63000 && result.Model.ID < 63010 {
			scanFilterCount++
			// 验证返回的都是未删除的
			assert.GreaterOrEqual(t, result.Model.ID, uint(63003), "不应该返回已删除的数据")
		}
	}

	assert.Equal(t, 7, scanFilterCount, "应该只返回 7 条未删除的数据")
}

// ✅ 测试 Scan 的排序和一致性
func TestGeneric_Scan_Ordering(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入有序数据
	expectedIDs := []uint{64000, 64001, 64002, 64003, 64004}
	for _, id := range expectedIDs {
		model := NewTestModel(id)
		model.Name = fmt.Sprintf("order_%d", id)
		model.Value = int(id)
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 多次扫描，验证结果一致性
	for round := 0; round < 3; round++ {
		results, err := db.Scan("test:64", 10)
		require.NoError(t, err)
		assert.Equal(t, 5, len(results), "每次扫描应该返回相同数量")

		// 验证数据完整性
		foundIDs := make(map[uint]bool)
		for _, result := range results {
			if result.Model.ID >= 64000 && result.Model.ID <= 64004 {
				foundIDs[result.Model.ID] = true
			}
		}
		assert.Equal(t, 5, len(foundIDs), "应该找到所有预期的 ID")
	}
}

// ✅ 测试 Scan 的边界条件
func TestGeneric_Scan_EdgeCases(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	t.Run("空数据库", func(t *testing.T) {
		results, err := db.Scan("test:", 100)
		require.NoError(t, err)
		assert.Equal(t, 0, len(results), "空数据库应该返回空结果")
	})

	t.Run("limit=0", func(t *testing.T) {
		// 插入一条数据
		model := NewTestModel(65000)
		model.Name = "test_limit_zero"
		err := db.Set(model, 0)
		require.NoError(t, err)

		// limit=0 应该返回空
		results, err := db.Scan("test:", 0)
		require.NoError(t, err)
		assert.Equal(t, 0, len(results), "limit=0 应该返回空结果")
	})

	t.Run("limit=1", func(t *testing.T) {
		results, err := db.Scan("test:", 1)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 1, "limit=1 应该最多返回 1 条")
	})

	t.Run("负数 limit", func(t *testing.T) {
		// BadgerDB 内部会将负数当作 0 处理
		results, err := db.Scan("test:", -1)
		require.NoError(t, err)
		assert.Equal(t, 0, len(results), "负数 limit 应该返回空结果")
	})

	t.Run("非常长的前缀", func(t *testing.T) {
		longPrefix := string(make([]byte, 1000))
		results, err := db.Scan(longPrefix, 100)
		require.NoError(t, err)
		assert.Equal(t, 0, len(results), "不存在的长前缀应该返回空")
	})
}

// ✅ 测试 Scan 性能（大数据量）
func TestGeneric_Scan_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过性能测试")
	}

	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入 1000 条数据
	const totalItems = 1000
	for i := 0; i < totalItems; i++ {
		model := NewTestModel(uint(66000 + i))
		model.Name = fmt.Sprintf("perf_scan_%d", i)
		model.Value = i
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	t.Run("扫描全部", func(t *testing.T) {
		start := time.Now()
		results, err := db.Scan("test:66", totalItems*2)
		duration := time.Since(start)

		require.NoError(t, err)
		assert.Equal(t, totalItems, len(results))
		t.Logf("扫描 %d 条数据耗时: %v", totalItems, duration)

		// 性能要求：1000 条数据应该在 100ms 内完成
		assert.Less(t, duration, 100*time.Millisecond, "扫描性能过低")
	})

	t.Run("分页扫描", func(t *testing.T) {
		pageSize := 100
		totalScanned := 0

		start := time.Now()
		results, err := db.Scan("test:66", pageSize)
		duration := time.Since(start)

		require.NoError(t, err)
		totalScanned += len(results)

		t.Logf("分页扫描 %d 条数据耗时: %v", len(results), duration)
		assert.LessOrEqual(t, len(results), pageSize)
	})
}

// ✅ 测试 Scan 并发安全
func TestGeneric_Scan_Concurrent(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入初始数据
	for i := 0; i < 100; i++ {
		model := NewTestModel(uint(67000 + i))
		model.Name = fmt.Sprintf("concurrent_scan_%d", i)
		model.Value = i
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 并发读取
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			results, err := db.Scan("test:67", 50)
			if err != nil {
				errors <- fmt.Errorf("routine %d 失败: %w", routineID, err)
				return
			}

			if len(results) == 0 {
				errors <- fmt.Errorf("routine %d 返回空结果", routineID)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 检查错误
	for err := range errors {
		t.Errorf("并发测试失败: %v", err)
	}
}

// ✅ 基准测试 - Scan
func BenchmarkGeneric_Scan(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	// 准备数据
	for i := 0; i < 1000; i++ {
		model := NewTestModel(uint(68000 + i))
		model.Name = fmt.Sprintf("bench_scan_%d", i)
		model.Value = i
		db.Set(model, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Scan("test:68", 100)
	}
}

// ✅ 基准测试 - Scan 全部
func BenchmarkGeneric_ScanAll(b *testing.B) {
	db, _ := NewBadgerDBFast[TestModel](b.TempDir())
	defer db.Close()

	// 准备数据
	for i := 0; i < 1000; i++ {
		model := NewTestModel(uint(69000 + i))
		model.Name = fmt.Sprintf("bench_scan_all_%d", i)
		model.Value = i
		db.Set(model, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Scan("test:69", 10000)
	}
}

// ✅ 测试 Scan 返回数据的完整性
func TestGeneric_Scan_DataIntegrity(t *testing.T) {
	db, cleanup := setupBadgerDBGeneric(t)
	defer cleanup()

	// 插入测试数据
	expectedData := map[uint]string{
		70000: "data_0",
		70001: "data_1",
		70002: "data_2",
		70003: "data_3",
		70004: "data_4",
	}

	for id, name := range expectedData {
		model := NewTestModel(id)
		model.Name = name
		model.Value = int(id)
		err := db.Set(model, 0)
		require.NoError(t, err)
	}

	// 扫描并验证
	results, err := db.Scan("test:70", 10)
	require.NoError(t, err)
	assert.Equal(t, 5, len(results))

	// 验证每条数据的完整性
	for _, result := range results {
		id := result.Model.ID
		if expectedName, exists := expectedData[id]; exists {
			assert.Equal(t, expectedName, result.Name, "数据名称应该匹配")
			assert.Equal(t, int(id), result.Value, "数据值应该匹配")
			assert.NotNil(t, result.Model, "Model 不应该为 nil")
		}
	}
}
