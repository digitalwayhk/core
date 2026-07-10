package config

import (
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
