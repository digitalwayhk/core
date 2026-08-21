package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteConfigFileUsesPrivateMode(t *testing.T) {
	file := filepath.Join(t.TempDir(), "service.json")

	require.NoError(t, writeConfigFile(file, []byte(`{"Name":"secure"}`)))

	info, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteConfigFileTightensExistingMode(t *testing.T) {
	file := filepath.Join(t.TempDir(), "service.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"Name":"legacy"}`), 0o600))
	require.NoError(t, os.Chmod(file, 0o666))

	require.NoError(t, writeConfigFile(file, []byte(`{"Name":"secure"}`)))

	info, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestMigrateConfigTightensFileMode(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.json")
	legacy := []byte(`{"Cluster":{"HeartbeatInterval":3000000000}}`)
	require.NoError(t, os.WriteFile(file, legacy, 0o600))
	require.NoError(t, os.Chmod(file, 0o666))

	migrateConfig(file)

	info, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestServerConfigDefaultsTrustedProxiesToEmpty(t *testing.T) {
	cfg := &ServerConfig{}

	cfg.ApplyDefaults()

	require.NotNil(t, cfg.TrustedProxies)
	require.Empty(t, cfg.TrustedProxies)
}

// TestServerConfigAppliesNeutralHMACHeaders 验证 Core 默认使用与消费方品牌无关的 HMAC Header。
func TestServerConfigAppliesNeutralHMACHeaders(t *testing.T) {
	cfg := NewServiceDefaultConfig("hmac-defaults", 18080)

	require.Equal(t, "X-Access-Key", cfg.HMACAuth.AccessKeyHeader)
	require.Equal(t, "X-Timestamp", cfg.HMACAuth.TimestampHeader)
	require.Equal(t, "X-Nonce", cfg.HMACAuth.NonceHeader)
	require.Equal(t, "X-Signature", cfg.HMACAuth.SignatureHeader)
	require.Equal(t, "X-Recv-Window", cfg.HMACAuth.RecvWindowHeader)
	require.Positive(t, cfg.HMACAuth.MaxInFlight)
}

// TestLegacyServerConfigWithoutHMACSectionAppliesDefaults 验证旧配置缺少 HMACAuth 节时仍可无损加载。
func TestLegacyServerConfigWithoutHMACSectionAppliesDefaults(t *testing.T) {
	var cfg ServerConfig
	require.NoError(t, json.Unmarshal([]byte(`{"Name":"legacy","Host":"127.0.0.1","Port":18080}`), &cfg))

	cfg.ApplyDefaults()

	require.NoError(t, cfg.Validate())
	require.Equal(t, "X-Access-Key", cfg.HMACAuth.AccessKeyHeader)
	require.Equal(t, "X-Signature", cfg.HMACAuth.SignatureHeader)
	require.Equal(t, 64, cfg.HMACAuth.MaxInFlight)
}

// TestServerConfigRejectsConflictingHMACHeaders 验证凭证 Header 重名时配置 fail closed。
func TestServerConfigRejectsConflictingHMACHeaders(t *testing.T) {
	cfg := NewServiceDefaultConfig("hmac-conflict", 18081)
	cfg.HMACAuth.SignatureHeader = cfg.HMACAuth.AccessKeyHeader

	require.ErrorContains(t, cfg.Validate(), "HMACAuth")
}

// TestServerConfigRejectsNegativeHMACConcurrency 验证非法并发上限不得进入运行时。
func TestServerConfigRejectsNegativeHMACConcurrency(t *testing.T) {
	cfg := NewServiceDefaultConfig("hmac-negative-concurrency", 18082)
	cfg.HMACAuth.MaxInFlight = -1

	require.ErrorContains(t, cfg.Validate(), "MaxInFlight")
}

func TestServerConfigValidatesTrustedProxies(t *testing.T) {
	cfg := &ServerConfig{TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8", "2001:db8::/32"}}
	cfg.ApplyDefaults()
	require.NoError(t, cfg.Validate())

	cfg.TrustedProxies = []string{"not-an-ip"}
	require.ErrorContains(t, cfg.Validate(), "TrustedProxies")
}

func TestServerConfigPreservesGoZeroLimits(t *testing.T) {
	cfg := NewServiceDefaultConfig("limit-defaults", 18081)

	require.Equal(t, int64(1<<20), cfg.MaxBytes)
	require.Equal(t, 10000, cfg.MaxConns)
	require.True(t, cfg.Middlewares.MaxBytes)
	require.True(t, cfg.Middlewares.MaxConns)
	require.True(t, cfg.Middlewares.Breaker)
	require.True(t, cfg.Middlewares.Shedding)
}
