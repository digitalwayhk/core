//go:build integration

package oltp

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

type mysqlIntegrationRecord struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"size:128;not null"`
}

// TestMySQLIntegration_ClosedPoolRecovery 锁定运行期连接池关闭后的语义：
// 只读刷新连接并重试一次；写入不盲目重放，但后续幂等重试可使用新连接成功。
func TestMySQLIntegration_ClosedPoolRecovery(t *testing.T) {
	cfg := mysqlIntegrationConfig(t)
	adapter := NewMySQL(cfg)
	adapter.Name = cfg.Database
	if err := adapter.HasTable(&mysqlIntegrationRecord{}); err != nil {
		t.Fatalf("准备测试表失败: %v", err)
	}
	db, err := adapter.GetDB()
	if err != nil {
		t.Fatal(err)
	}
	seed := &mysqlIntegrationRecord{Value: "closed-pool-read"}
	if err := db.Create(seed).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fresh, freshErr := adapter.GetDB()
		if freshErr == nil {
			_ = fresh.Exec("DROP TABLE IF EXISTS " + mysqlIntegrationRecord{}.TableName()).Error
		}
	})

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	query := &types.SearchItem{Page: 1, Size: 1, SkipCount: true, Model: &mysqlIntegrationRecord{}}
	query.AddWhereN("ID", seed.ID)
	var rows []*mysqlIntegrationRecord
	if err := adapter.Load(query, &rows); err != nil {
		t.Fatalf("只读未在连接关闭后恢复: %v", err)
	}
	if len(rows) != 1 || rows[0].Value != seed.Value {
		t.Fatalf("恢复后读取结果=%+v", rows)
	}

	current, err := adapter.GetDB()
	if err != nil {
		t.Fatal(err)
	}
	currentSQL, err := current.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := currentSQL.Close(); err != nil {
		t.Fatal(err)
	}
	write := &mysqlIntegrationRecord{Value: "closed-pool-write"}
	if err := adapter.Insert(write); !isConnectionError(err) {
		t.Fatalf("关闭连接后的写入错误=%v，必须原样返回连接错误", err)
	}
	if err := adapter.Insert(write); err != nil {
		t.Fatalf("上层幂等重试未使用刷新连接成功: %v", err)
	}
}

func (mysqlIntegrationRecord) TableName() string { return "core_integration_mysql_records" }

func mysqlIntegrationConfig(t *testing.T) *Config {
	t.Helper()
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("设置 CORE_TEST_MYSQL=1 后再运行 MySQL 集成测试")
	}
	port, err := strconv.Atoi(envOrDefault("CORE_TEST_MYSQL_PORT", "13306"))
	if err != nil {
		t.Fatalf("CORE_TEST_MYSQL_PORT 无效: %v", err)
	}
	return &Config{
		Host:         envOrDefault("CORE_TEST_MYSQL_HOST", "127.0.0.1"),
		Port:         port,
		Username:     envOrDefault("CORE_TEST_MYSQL_USER", "core_test"),
		Password:     envOrDefault("CORE_TEST_MYSQL_PASSWORD", "core_test_password"),
		Database:     envOrDefault("CORE_TEST_MYSQL_DATABASE", "core_test"),
		Charset:      "utf8mb4",
		ParseTime:    true,
		Loc:          "Local",
		MaxIdleConns: 2,
		MaxOpenConns: 4,
		MaxLifetime:  time.Minute,
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func TestMySQLIntegration_DriverContract(t *testing.T) {
	cfg := mysqlIntegrationConfig(t)
	adapter := NewMySQL(cfg)
	adapter.Name = cfg.Database
	db, err := adapter.GetDB()
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取 MySQL 连接池失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP TABLE IF EXISTS " + mysqlIntegrationRecord{}.TableName()).Error; err != nil {
			t.Errorf("清理 MySQL 测试表失败: %v", err)
		}
		if err := sqlDB.Close(); err != nil {
			t.Errorf("关闭 MySQL 测试连接失败: %v", err)
		}
	})

	if got := sqlDB.Stats().MaxOpenConnections; got != cfg.MaxOpenConns {
		t.Fatalf("最大连接数不一致: got=%d want=%d", got, cfg.MaxOpenConns)
	}
	if err := adapter.HasTable(&mysqlIntegrationRecord{}); err != nil {
		t.Fatalf("Core 首次访问自动创建自定义表名失败: %v", err)
	}
	if !db.Migrator().HasTable(mysqlIntegrationRecord{}.TableName()) {
		t.Fatalf("Core 未创建模型声明的表 %q", mysqlIntegrationRecord{}.TableName())
	}
	if db.Migrator().HasTable("mysql_integration_record") {
		t.Fatal("Core 不应按类型名创建或验证错误的默认表")
	}

	record := &mysqlIntegrationRecord{Value: "created"}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("插入 MySQL 记录失败: %v", err)
	}
	if err := db.Model(record).Update("value", "updated").Error; err != nil {
		t.Fatalf("更新 MySQL 记录失败: %v", err)
	}
	var loaded mysqlIntegrationRecord
	if err := db.First(&loaded, record.ID).Error; err != nil || loaded.Value != "updated" {
		t.Fatalf("查询 MySQL 记录失败: value=%q err=%v", loaded.Value, err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启 MySQL 事务失败: %v", tx.Error)
	}
	rolledBack := &mysqlIntegrationRecord{Value: "rollback"}
	if err := tx.Create(rolledBack).Error; err != nil {
		t.Fatalf("事务内插入失败: %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("回滚 MySQL 事务失败: %v", err)
	}
	var count int64
	if err := db.Model(&mysqlIntegrationRecord{}).Where("id = ?", rolledBack.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("事务回滚未生效: count=%d err=%v", count, err)
	}
	if err := db.Delete(record).Error; err != nil {
		t.Fatalf("删除 MySQL 记录失败: %v", err)
	}
	if err := db.Model(&mysqlIntegrationRecord{}).Where("id = ?", record.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("MySQL 清理未生效: %s", fmt.Sprint(err))
	}
}
