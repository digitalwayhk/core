package olap

import (
	"strings"
	"testing"
)

func TestClickHouseDSNKeepsAsyncInsertForBufferedWrites(t *testing.T) {
	cfg := &Config{
		Host:     "127.0.0.1",
		Port:     9000,
		Database: "core_test",
		Username: "core_test",
		Password: "secret",
	}

	dsn := cfg.ClickHouseDSN()
	if !strings.Contains(dsn, "async_insert=1") || !strings.Contains(dsn, "wait_for_async_insert=0") {
		t.Fatalf("基础连接串应保留异步写入参数: %s", dsn)
	}
}

func TestClickHouseSyncInsertSettingsDisableAsyncAndWait(t *testing.T) {
	settings := clickHouseSyncInsertSettings()
	if got := settings["async_insert"]; got != 0 {
		t.Fatalf("InsertSync 必须关闭异步插入: got=%v", got)
	}
	if got := settings["wait_for_async_insert"]; got != 1 {
		t.Fatalf("InsertSync 必须等待服务端确认: got=%v", got)
	}
}
