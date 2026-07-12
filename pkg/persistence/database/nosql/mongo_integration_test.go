//go:build integration

package nosql

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

type mongoIntegrationRecord struct {
	ID    string `bson:"_id"`
	Value string `bson:"value"`
}

func (m *mongoIntegrationRecord) GetLocalDBName() string  { return "core_test" }
func (m *mongoIntegrationRecord) GetRemoteDBName() string { return "core_test" }

func TestMongoIntegration_DriverContract(t *testing.T) {
	if os.Getenv("CORE_TEST_MONGODB") != "1" {
		t.Skip("设置 CORE_TEST_MONGODB=1 后再运行 MongoDB 集成测试")
	}
	port, err := strconv.Atoi(mongoEnvOrDefault("CORE_TEST_MONGODB_PORT", "27018"))
	if err != nil {
		t.Fatalf("CORE_TEST_MONGODB_PORT 无效: %v", err)
	}
	adapter := NewMongo(
		mongoEnvOrDefault("CORE_TEST_MONGODB_HOST", "127.0.0.1"),
		mongoEnvOrDefault("CORE_TEST_MONGODB_USER", "core_test"),
		mongoEnvOrDefault("CORE_TEST_MONGODB_PASSWORD", "core_test_password"),
		uint(port),
	)
	adapter.Name = mongoEnvOrDefault("CORE_TEST_MONGODB_DATABASE", "core_test")
	db, err := adapter.GetMongo()
	if err != nil {
		t.Fatalf("连接 MongoDB 失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.Client().Ping(ctx, nil); err != nil {
		t.Fatalf("MongoDB Ping 失败: %v", err)
	}
	collection := db.Collection("mongoIntegrationRecord")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := collection.Drop(cleanupCtx); err != nil {
			t.Errorf("清理 MongoDB 测试集合失败: %v", err)
		}
		if err := db.Client().Disconnect(cleanupCtx); err != nil {
			t.Errorf("关闭 MongoDB 测试连接失败: %v", err)
		}
	})

	canceledCtx, cancelCanceledCtx := context.WithCancel(context.Background())
	cancelCanceledCtx()
	if _, err := collection.CountDocuments(canceledCtx, bson.M{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("MongoDB driver 未返回 context 取消错误: %v", err)
	}

	record := &mongoIntegrationRecord{ID: "contract", Value: "created"}
	if err := adapter.Insert(record); err != nil {
		t.Fatalf("插入 MongoDB 记录失败: %v", err)
	}
	record.Value = "updated"
	if err := adapter.Update(record); err != nil {
		t.Fatalf("更新 MongoDB 记录失败: %v", err)
	}
	var loaded mongoIntegrationRecord
	if err := collection.FindOne(ctx, bson.M{"_id": record.ID}).Decode(&loaded); err != nil || loaded.Value != "updated" {
		t.Fatalf("查询 MongoDB 记录失败: value=%q err=%v", loaded.Value, err)
	}
	if err := adapter.Delete(record); err != nil {
		t.Fatalf("删除 MongoDB 记录失败: %v", err)
	}
	count, err := collection.CountDocuments(ctx, bson.M{"_id": record.ID})
	if err != nil || count != 0 {
		t.Fatalf("MongoDB 清理未生效: count=%d err=%v", count, err)
	}

	// 当前 Mongo 适配器的事务方法尚未实现。
}

func mongoEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
