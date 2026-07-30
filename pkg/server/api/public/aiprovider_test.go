package public

import (
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/aiprovider"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestAIProviderRouterInfoIsServerManage(t *testing.T) {
	info := (&AIProvider{}).RouterInfo()
	require.Equal(t, "/api/servermanage/aiprovider", info.GetPath())
	require.Equal(t, types.ServerManagerType, info.GetPathType())

	save := (&SaveAIProvider{}).RouterInfo()
	require.Equal(t, "/api/servermanage/saveaiprovider", save.GetPath())
	require.Equal(t, types.ServerManagerType, save.GetPathType())

	test := (&TestAIProvider{}).RouterInfo()
	require.Equal(t, "/api/servermanage/testaiprovider", test.GetPath())
	require.Equal(t, types.ServerManagerType, test.GetPathType())
}

func TestAIProviderDoAdminAndRuntimeViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), aiprovider.FileName)
	aiprovider.SetConfigPathForTest(path)
	t.Cleanup(aiprovider.ResetPathHookForTest)

	_, err := aiprovider.Save(aiprovider.Config{
		Enabled:  true,
		Provider: "dashscope",
		Model:    "qwen3.5-plus",
		BaseURL:  "https://example.com/v1",
		APIKey:   "k-secret",
		Language: "zh-CN",
	})
	require.NoError(t, err)

	admin := &AIProvider{View: "admin"}
	data, err := admin.Do(nil)
	require.NoError(t, err)
	view, ok := data.(aiprovider.View)
	require.True(t, ok)
	require.Equal(t, aiprovider.MaskedAPIKey, view.APIKey)
	require.True(t, view.APIKeySet)

	runtime := &AIProvider{View: "runtime"}
	data, err = runtime.Do(nil)
	require.NoError(t, err)
	view, ok = data.(aiprovider.View)
	require.True(t, ok)
	require.Equal(t, "k-secret", view.APIKey)
}

func TestSaveAIProviderDo(t *testing.T) {
	path := filepath.Join(t.TempDir(), aiprovider.FileName)
	aiprovider.SetConfigPathForTest(path)
	t.Cleanup(aiprovider.ResetPathHookForTest)

	own := &SaveAIProvider{
		Enabled:  true,
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "new-key",
		Language: "zh-CN",
	}
	data, err := own.Do(nil)
	require.NoError(t, err)
	view := data.(aiprovider.View)
	require.Equal(t, aiprovider.MaskedAPIKey, view.APIKey)
	require.Equal(t, "gpt-test", view.Model)

	cfg, err := aiprovider.Load()
	require.NoError(t, err)
	require.Equal(t, "new-key", cfg.APIKey)
}
