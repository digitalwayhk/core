// Package oltp 验证 MySQL 表迁移时只处理真正的嵌套模型数组。
package oltp

import "testing"

type mysqlNestedPrimitiveArrayModel struct {
	ID      uint
	Payload []byte
}

func TestMySQLProcessNestedTablesSkipsPrimitiveArrays(t *testing.T) {
	adapter := &MySQL{}
	requireNotPanics(t, func() {
		err := adapter.processNestedTablesOptimized(&mysqlNestedPrimitiveArrayModel{}, map[string]bool{}, 0, 2)
		if err != nil {
			t.Fatalf("基础数组字段不应触发嵌套表处理: %v", err)
		}
	})
}

func requireNotPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("不应 panic: %v", recovered)
		}
	}()
	fn()
}
