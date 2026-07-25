package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadConfigRemovesLegacySocketWithoutChangingGRPCDefaults(t *testing.T) {
	originalConfigDirPath := CONFIGDIRPATH
	CONFIGDIRPATH = t.TempDir() + string(os.PathSeparator)
	t.Cleanup(func() { CONFIGDIRPATH = originalConfigDirPath })

	const serviceName = "legacy-socket"
	data, err := os.ReadFile(filepath.Join("testdata", "legacy_socket_config.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(CONFIGDIRPATH, serviceName+".json"), data, 0o600))

	var cfg *ServerConfig
	require.NotPanics(t, func() { cfg = ReadConfig(serviceName) })
	require.NotNil(t, cfg)
	assert.Equal(t, 18080, cfg.Transport.GRPC.Port)
	assert.Equal(t, "insecure", cfg.Transport.GRPC.Security.Mode)
	require.NoError(t, cfg.Validate())

	configFile := filepath.Join(CONFIGDIRPATH, serviceName+".json")
	migrated, err := os.ReadFile(configFile)
	require.NoError(t, err)
	var values map[string]interface{}
	require.NoError(t, json.Unmarshal(migrated, &values))
	assert.NotContains(t, values, "SocketPort")
	transport, ok := values["Transport"].(map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, transport, "Socket")

	before := string(migrated)
	require.NoError(t, migrateConfig(configFile))
	after, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Equal(t, before, string(after), "重复迁移必须保持文件不变")
}

func TestMigrateConfigPreservesUnknownFieldsWhenRemovingSocket(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"SocketPort":18080,"FutureField":{"enabled":true},"Transport":{"Internal":"grpc","Socket":{"Enable":true},"GRPC":{"Enable":true,"Port":19090},"FutureTransport":"keep"}}`)
	require.NoError(t, os.WriteFile(file, data, 0o600))
	require.NoError(t, migrateConfig(file))

	var values map[string]interface{}
	migrated, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(migrated, &values))
	assert.NotContains(t, values, "SocketPort")
	assert.Equal(t, map[string]interface{}{"enabled": true}, values["FutureField"])
	transport := values["Transport"].(map[string]interface{})
	assert.NotContains(t, transport, "Socket")
	assert.Equal(t, "keep", transport["FutureTransport"])
	grpc := transport["GRPC"].(map[string]interface{})
	assert.NotContains(t, grpc, "Enable")
	assert.Equal(t, float64(19090), grpc["Port"])
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
