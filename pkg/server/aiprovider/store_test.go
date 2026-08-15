package aiprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	require.Equal(t, "", runtime.APIKey)
	require.Equal(t, ProxyBasePath, runtime.BaseURL)
	require.True(t, runtime.ProxyMode)
	require.True(t, runtime.APIKeySet)
	require.Equal(t, "qwen3.5-plus", runtime.Model)

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

func TestChatCompletionsURL(t *testing.T) {
	u, err := chatCompletionsURL("https://example.com/v1/")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/chat/completions", u)
	u, err = chatCompletionsURL("https://example.com/v1/chat/completions")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/chat/completions", u)
	_, err = chatCompletionsURL("not-a-url")
	require.Error(t, err)
}

func TestMergeProbeInputKeepsStoredKey(t *testing.T) {
	SetConfigPathForTest(filepath.Join(t.TempDir(), FileName))
	t.Cleanup(ResetPathHookForTest)
	_, err := Save(Config{
		Enabled: true,
		Model:   "m1",
		BaseURL: "https://example.com/v1",
		APIKey:  "stored",
	})
	require.NoError(t, err)
	merged, err := MergeProbeInput(Config{
		Model:   "m2",
		BaseURL: "https://example.com/v1",
		APIKey:  MaskedAPIKey,
	})
	require.NoError(t, err)
	require.Equal(t, "m2", merged.Model)
	require.Equal(t, "stored", merged.APIKey)
}

func TestProxyChatCompletionsForwardsWithServerKeyAndModel(t *testing.T) {
	var gotAuth string
	var gotBody map[string]interface{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &gotBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	SetConfigPathForTest(filepath.Join(t.TempDir(), FileName))
	t.Cleanup(ResetPathHookForTest)
	_, err := Save(Config{
		Enabled: true,
		Model:   "server-model",
		BaseURL: upstream.URL + "/v1",
		APIKey:  "upstream-secret",
	})
	require.NoError(t, err)

	result, err := ProxyChatCompletions(context.Background(), []byte(`{
		"model":"client-model",
		"messages":[{"role":"user","content":"hi"}],
		"stream":true
	}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Contains(t, string(result.Body), `"ok"`)
	require.Equal(t, "Bearer upstream-secret", gotAuth)
	require.Equal(t, "server-model", gotBody["model"])
	require.Equal(t, false, gotBody["stream"])
}

func TestProxyChatCompletionsRequiresEnabled(t *testing.T) {
	SetConfigPathForTest(filepath.Join(t.TempDir(), FileName))
	t.Cleanup(ResetPathHookForTest)
	_, err := Save(Config{
		Enabled: false,
		Model:   "m",
		BaseURL: "https://example.com/v1",
		APIKey:  "k",
	})
	require.NoError(t, err)
	_, err = ProxyChatCompletions(context.Background(), []byte(`{"messages":[]}`))
	require.Error(t, err)
}
