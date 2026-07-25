//go:build integration

package oltp

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

type mysqlIntegrationRecord struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"size:128;not null"`
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
	if err := db.AutoMigrate(&mysqlIntegrationRecord{}); err != nil {
		t.Fatalf("迁移 MySQL 测试表失败: %v", err)
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
