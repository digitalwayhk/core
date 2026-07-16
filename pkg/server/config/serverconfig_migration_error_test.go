package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadConfigLoadsLegacySocketWithoutChangingGRPCDefaults(t *testing.T) {
	originalConfigDirPath := CONFIGDIRPATH
	CONFIGDIRPATH = t.TempDir() + string(os.PathSeparator)
	t.Cleanup(func() { CONFIGDIRPATH = originalConfigDirPath })

	const serviceName = "legacy-socket"
	legacy := NewServiceDefaultConfig(serviceName, 8080)
	legacy.Transport.Socket.Enable = true
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	fixDurations(reflect.ValueOf(legacy).Elem(), document)
	transport := document["Transport"].(map[string]any)
	delete(transport, "GRPC")
	data, err = json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(CONFIGDIRPATH, serviceName+".json"), data, 0o600))

	var cfg *ServerConfig
	require.NotPanics(t, func() { cfg = ReadConfig(serviceName) })
	require.NotNil(t, cfg)
	assert.Equal(t, 18080, cfg.SocketPort)
	assert.True(t, cfg.Transport.Socket.Enable)
	assert.Equal(t, 18080, cfg.Transport.GRPC.Port)
	assert.Equal(t, "insecure", cfg.Transport.GRPC.Security.Mode)
	require.NoError(t, cfg.Validate())
}

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
