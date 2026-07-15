package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConfigCreatesDistinctRefreshSecrets(t *testing.T) {
	cfg := NewServiceDefaultConfig("auth-defaults", 18081)

	require.Equal(t, int64(7200), cfg.Auth.AccessExpire)
	require.Equal(t, int64(2592000), cfg.Auth.RefreshExpire)
	require.NotEmpty(t, cfg.Auth.RefreshSecret)
	require.NotEqual(t, cfg.Auth.AccessSecret, cfg.Auth.RefreshSecret)

	require.Equal(t, int64(7200), cfg.ManageAuth.AccessExpire)
	require.Equal(t, int64(2592000), cfg.ManageAuth.RefreshExpire)
	require.NotEmpty(t, cfg.ManageAuth.RefreshSecret)
	require.NotEqual(t, cfg.ManageAuth.AccessSecret, cfg.ManageAuth.RefreshSecret)
	require.NotEqual(t, cfg.Auth.RefreshSecret, cfg.ManageAuth.RefreshSecret)

	require.Empty(t, cfg.ServerManageAuth.RefreshSecret)
	require.Zero(t, cfg.ServerManageAuth.RefreshExpire)
}

func TestMigrateConfigPersistsRefreshSecretsOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.json")
	legacy := []byte(`{
		"Name":"legacy-auth",
		"Auth":{"AccessSecret":"auth-access","AccessExpire":86400},
		"ManageAuth":{"AccessSecret":"manage-access","AccessExpire":86400},
		"ServerManageAuth":{"AccessSecret":"server-access","AccessExpire":86400}
	}`)
	require.NoError(t, os.WriteFile(file, legacy, 0o600))
	require.NoError(t, os.Chmod(file, 0o666))

	require.NoError(t, migrateConfig(file))
	first, err := os.ReadFile(file)
	require.NoError(t, err)

	var migrated ServerConfig
	require.NoError(t, json.Unmarshal(first, &migrated))
	require.NotEmpty(t, migrated.Auth.RefreshSecret)
	require.NotEqual(t, migrated.Auth.AccessSecret, migrated.Auth.RefreshSecret)
	require.Equal(t, int64(2592000), migrated.Auth.RefreshExpire)
	require.NotEmpty(t, migrated.ManageAuth.RefreshSecret)
	require.NotEqual(t, migrated.ManageAuth.AccessSecret, migrated.ManageAuth.RefreshSecret)
	require.NotEqual(t, migrated.Auth.RefreshSecret, migrated.ManageAuth.RefreshSecret)
	require.Equal(t, int64(2592000), migrated.ManageAuth.RefreshExpire)
	require.Empty(t, migrated.ServerManageAuth.RefreshSecret)
	require.Zero(t, migrated.ServerManageAuth.RefreshExpire)

	info, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, migrateConfig(file))
	second, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, first, second, "重复迁移不得重置 Refresh 密钥")
}
