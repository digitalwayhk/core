//go:build integration

package olap

import (
	"os"
	"strconv"
	"testing"
	"time"
)

type clickHouseIntegrationRecord struct {
	ID        uint64    `gorm:"column:id"`
	Value     string    `gorm:"column:value"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (clickHouseIntegrationRecord) TableName() string {
	return "core_integration_clickhouse_records"
}

func TestClickHouseIntegration_DriverContract(t *testing.T) {
	if os.Getenv("CORE_TEST_CLICKHOUSE") != "1" {
		t.Skip("设置 CORE_TEST_CLICKHOUSE=1 后再运行 ClickHouse 集成测试")
	}
	port, err := strconv.Atoi(clickHouseEnvOrDefault("CORE_TEST_CLICKHOUSE_PORT", "19000"))
	if err != nil {
		t.Fatalf("CORE_TEST_CLICKHOUSE_PORT 无效: %v", err)
	}
	cfg := &Config{
		Host:         clickHouseEnvOrDefault("CORE_TEST_CLICKHOUSE_HOST", "127.0.0.1"),
		Port:         port,
		Database:     clickHouseEnvOrDefault("CORE_TEST_CLICKHOUSE_DATABASE", "core_test"),
		Username:     clickHouseEnvOrDefault("CORE_TEST_CLICKHOUSE_USER", "core_test"),
		Password:     clickHouseEnvOrDefault("CORE_TEST_CLICKHOUSE_PASSWORD", "core_test_password"),
		MaxOpenConns: 4,
		MaxIdleConns: 2,
		AutoCreateDB: false,
	}
	adapter, err := NewClickHouse(cfg)
	if err != nil {
		t.Fatalf("连接 ClickHouse 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := adapter.GetDB().Exec("DROP TABLE IF EXISTS " + clickHouseIntegrationRecord{}.TableName()).Error; err != nil {
			t.Errorf("清理 ClickHouse 测试表失败: %v", err)
		}
		if err := adapter.Close(); err != nil {
			t.Errorf("关闭 ClickHouse 测试连接失败: %v", err)
		}
	})
	engine := &TableEngineConfig{Engine: "MergeTree()", OrderBy: []string{"id"}, IndexGranularity: 8192}
	if err := adapter.CreateTable(&clickHouseIntegrationRecord{}, engine); err != nil {
		t.Fatalf("创建 ClickHouse 测试表失败: %v", err)
	}
	record := &clickHouseIntegrationRecord{ID: 1, Value: "created", CreatedAt: time.Now().UTC()}
	if err := adapter.InsertSync(record); err != nil {
		t.Fatalf("同步写入 ClickHouse 失败: %v", err)
	}
	var loaded clickHouseIntegrationRecord
	if err := adapter.GetDB().Where("id = ?", record.ID).First(&loaded).Error; err != nil || loaded.Value != record.Value {
		t.Fatalf("查询 ClickHouse 记录失败: value=%q err=%v", loaded.Value, err)
	}
	if err := adapter.GetDB().Exec("TRUNCATE TABLE " + record.TableName()).Error; err != nil {
		t.Fatalf("清理 ClickHouse 测试表失败: %v", err)
	}
}

func clickHouseEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
