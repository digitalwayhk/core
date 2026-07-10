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
