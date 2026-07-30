package aiprovider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSaveLoadRoundTripAndMask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	SetConfigPathForTest(path)
	t.Cleanup(ResetPathHookForTest)

	saved, err := Save(Config{
		Enabled:  true,
		Provider: "dashscope",
		Model:    "qwen3.5-plus",
		BaseURL:  "https://example.com/v1",
		APIKey:   "secret-key",
		Language: "zh-CN",
	})
	require.NoError(t, err)
	require.Equal(t, "secret-key", saved.APIKey)

	loaded, err := Load()
	require.NoError(t, err)
	require.True(t, loaded.Enabled)
	require.Equal(t, "secret-key", loaded.APIKey)
	require.True(t, ReadyForAgent(loaded))

	admin := AdminView(loaded)
	require.Equal(t, MaskedAPIKey, admin.APIKey)
	require.True(t, admin.APIKeySet)

	runtime := RuntimeView(loaded)
	require.Equal(t, "secret-key", runtime.APIKey)

	// 脱敏占位保存应保留旧密钥
	_, err = Save(Config{
		Enabled:  true,
		Provider: "dashscope",
		Model:    "qwen3.5-plus",
		BaseURL:  "https://example.com/v1",
		APIKey:   MaskedAPIKey,
		Language: "zh-CN",
	})
	require.NoError(t, err)
	loaded, err = Load()
	require.NoError(t, err)
	require.Equal(t, "secret-key", loaded.APIKey)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestValidateEnabledRequiresModelAndBaseURL(t *testing.T) {
	dir := t.TempDir()
	SetConfigPathForTest(filepath.Join(dir, FileName))
	t.Cleanup(ResetPathHookForTest)

	_, err := Save(Config{Enabled: true, Model: "", BaseURL: "https://x"})
	require.Error(t, err)
	_, err = Save(Config{Enabled: true, Model: "m", BaseURL: ""})
	require.Error(t, err)
	_, err = Save(Config{Enabled: false, Model: "", BaseURL: ""})
	require.NoError(t, err)
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	SetConfigPathForTest(filepath.Join(t.TempDir(), "missing.json"))
	t.Cleanup(ResetPathHookForTest)
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Equal(t, "qwen3.5-plus", cfg.Model)
}
