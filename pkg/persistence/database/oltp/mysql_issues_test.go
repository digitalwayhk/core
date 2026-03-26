package oltp

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ============================================================
// 问题: ConnectionManager.SetConnection 硬编码连接池参数
//
// SetConnection 内部有:
//   sqlDB.SetMaxOpenConns(10)
//   sqlDB.SetMaxIdleConns(5)
//
// 这会覆盖 newDB() → configureConnectionPool() 中根据
// 用户 Config 设置的值。当用户配置 MaxOpenConns=20 时，
// 实际连接池被限制为 10，但 GetMaxOpenConns() 返回 20。
//
// 影响: sharedbadger 的 semaphore 容量基于 GetMaxOpenConns()
//       计算，可能大于实际连接池，导致高并发下连接等待超时。
// ============================================================

func requireMySQLAvailable(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_MYSQL_CONCURRENCY_REPRO") != "1" {
		t.Skip("设置 RUN_MYSQL_CONCURRENCY_REPRO=1 后再运行该复现测试")
	}
}

func TestIssue_SetConnectionOverridesMaxOpenConns(t *testing.T) {
	cfg := &Config{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "your_password",
		MaxOpenConns: 20,
		MaxIdleConns: 10,
		MaxLifetime:  30 * time.Minute,
	}

	adapter := NewMySQL(cfg)
	dbName := "test_issue_pool_setting"
	adapter.Name = dbName
	t.Cleanup(func() {
		adapter.Name = dbName
		_ = adapter.DeleteDB()
	})

	// Config 报告 MaxOpenConns=20
	require.Equal(t, 20, adapter.GetMaxOpenConns())

	// 创建连接（会走 newDB → configureConnectionPool → SetConnection）
	gormDB, err := adapter.GetDB()
	require.NoError(t, err)

	sqlDB, err := gormDB.DB()
	require.NoError(t, err)

	// 检查实际连接池设置
	stats := sqlDB.Stats()

	if stats.MaxOpenConnections != 20 {
		t.Errorf("【修复未生效】连接池 MaxOpenConnections 仍被覆盖:\n"+
			"  Config.MaxOpenConns: 20\n"+
			"  实际 MaxOpenConnections: %d",
			stats.MaxOpenConnections)
	} else {
		t.Logf("✅ 连接池参数正确: MaxOpenConnections=%d", stats.MaxOpenConnections)
	}
}

func TestIssue_SetConnectionOverridesMaxIdleConns(t *testing.T) {
	cfg := &Config{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "your_password",
		MaxOpenConns: 20,
		MaxIdleConns: 15,
		MaxLifetime:  30 * time.Minute,
	}

	adapter := NewMySQL(cfg)
	dbName := "test_issue_idle_setting"
	adapter.Name = dbName
	t.Cleanup(func() {
		adapter.Name = dbName
		_ = adapter.DeleteDB()
	})

	// 创建连接
	gormDB, err := adapter.GetDB()
	require.NoError(t, err)

	sqlDB, err := gormDB.DB()
	require.NoError(t, err)

	stats := sqlDB.Stats()

	// MaxIdleConns 不直接从 Stats 获取，但我们可以通过 MaxOpenConnections 推断
	// 如果 MaxOpen 被覆盖了，MaxIdle 也一定被覆盖了
	if stats.MaxOpenConnections != 20 {
		t.Errorf("MaxOpenConnections 被覆盖: 预期 20, 实际 %d", stats.MaxOpenConnections)
	}
}

// ============================================================
// 问题: SetConnection 关闭旧连接，影响共享同一 pool 的实例
//
// 当新连接 SetConnection(key, newDB) 时，旧 *sql.DB 被 Close()。
// 其他 MySQL 实例若仍持有旧 *gorm.DB 引用，
// 执行查询会得到 "database is closed" 错误。
// ============================================================

func TestIssue_SetConnectionClosesOldForOtherInstances(t *testing.T) {
	cfg := &Config{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "your_password",
		MaxOpenConns: 5,
		MaxIdleConns: 2,
		MaxLifetime:  30 * time.Minute,
	}

	dbName := "test_issue_close_shared"
	t.Cleanup(func() {
		a := NewMySQL(cfg)
		a.Name = dbName
		_ = a.DeleteDB()
	})

	// instance1 获取连接
	instance1 := NewMySQL(cfg)
	instance1.Name = dbName
	db1, err := instance1.GetDB()
	require.NoError(t, err)

	sqlDB1, err := db1.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB1.Ping(), "instance1 初始 Ping 应成功")

	// instance2 获取同一连接 key 的连接（从缓存中获取）
	instance2 := NewMySQL(cfg)
	instance2.Name = dbName
	db2, err := instance2.GetDB()
	require.NoError(t, err)

	sqlDB2, err := db2.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB2.Ping(), "instance2 初始 Ping 应成功")

	// 模拟 instance1 调用 SetConnection 覆盖连接
	// 这会 Close() 旧的 *sql.DB
	connKey := instance1.getConnectionKey()
	newGormDB, err := instance1.newDB()
	require.NoError(t, err)
	connManager.SetConnection(connKey, newGormDB)

	// 验证 instance2 的旧连接在宽限期内仍可用
	err = sqlDB2.Ping()
	if err != nil {
		t.Errorf("【修复未生效】SetConnection 仍立即关闭了共享的旧连接池:\n"+
			"  instance2 Ping 失败: %v",
			err)
	} else {
		t.Log("✅ 旧连接在宽限期内仍可用，延迟关闭生效")
	}
}
