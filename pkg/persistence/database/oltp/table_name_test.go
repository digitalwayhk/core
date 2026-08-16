package oltp

import (
	"reflect"
	"testing"

	"gorm.io/gorm/schema"
)

type customTableNameRecord struct{}

func (*customTableNameRecord) TableName() string { return "custom_records" }

type defaultTableNameRecord struct{}

// TestModelTableNameHonorsGORMTableName 锁定自动建表、验证和缓存使用同一张真实表。
func TestModelTableNameHonorsGORMTableName(t *testing.T) {
	namer := schema.NamingStrategy{SingularTable: true}
	if got := modelTableName(namer, reflect.TypeOf(customTableNameRecord{})); got != "custom_records" {
		t.Fatalf("自定义 TableName 未生效: got=%q", got)
	}
	if got := modelTableName(namer, reflect.TypeOf(defaultTableNameRecord{})); got != "default_table_name_record" {
		t.Fatalf("默认命名不一致: got=%q", got)
	}
}
