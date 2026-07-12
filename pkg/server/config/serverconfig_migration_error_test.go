package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMigrateConfigReturnsWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("权限语义依赖 Unix")
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, []byte(`{"RetryDelay":1000000000}`), 0o400); err != nil {
		t.Fatalf("创建只读配置失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })

	if err := migrateConfig(file); err == nil {
		t.Fatal("配置迁移写入失败必须返回错误")
	}
}
