package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest"
)

func TestAdminViewRedactsProtectedValues(t *testing.T) {
	cfg := NewServiceDefaultConfig("admin-view", 8080)
	cfg.Name = "server"
	cfg.ApplyDefaults()
	cfg.Auth.AccessSecret = "auth-access"
	cfg.Auth.RefreshSecret = "auth-refresh"
	cfg.ManageAuth.AccessSecret = "manage-access"
	cfg.Auth.CasDoor.WebhookSecret = "webhook"
	cfg.Signature.PrivateKeys = []rest.PrivateKeyConf{{KeyFile: "private-key-material"}}
	cfg.RuntimeObservability.Mode = "prometheus"
	cfg.RuntimeObservability.QueryURL = "http://prometheus:9090"
	cfg.ManageStore.Password = "manage-store-password"

	view, err := AdminView(cfg)
	require.NoError(t, err)
	require.Equal(t, redactedConfigValue, view["Auth"].(map[string]interface{})["AccessSecret"])
	require.Equal(t, redactedConfigValue, view["Auth"].(map[string]interface{})["RefreshSecret"])
	require.Equal(t, redactedConfigValue, view["ManageAuth"].(map[string]interface{})["AccessSecret"])
	require.Equal(t, redactedConfigValue, view["Auth"].(map[string]interface{})["CasDoor"].(map[string]interface{})["WebhookSecret"])
	require.Empty(t, view["Signature"].(map[string]interface{})["PrivateKeys"])
	require.Equal(t, redactedConfigValue, view["RuntimeObservability"].(map[string]interface{})["QueryURL"])
	require.Equal(t, redactedConfigValue, view["ManageStore"].(map[string]interface{})["Password"])
}

func TestMergeProtectedFieldsKeepsRuntimeCredentials(t *testing.T) {
	existing := NewServiceDefaultConfig("merge", 8080)
	existing.Name = "server"
	existing.ApplyDefaults()
	existing.Auth.AccessSecret = "keep-access"
	existing.Auth.RefreshSecret = "keep-refresh"
	existing.ManageStore.Password = "keep-password"
	incoming := NewServiceDefaultConfig("merge", 9090)
	incoming.Name = "server"
	incoming.ApplyDefaults()
	incoming.Auth.AccessSecret = redactedConfigValue
	incoming.Auth.RefreshSecret = redactedConfigValue
	incoming.ManageStore.Password = redactedConfigValue

	merged, err := MergeProtectedFields(existing, incoming)
	require.NoError(t, err)
	require.Equal(t, "keep-access", merged.Auth.AccessSecret)
	require.Equal(t, "keep-refresh", merged.Auth.RefreshSecret)
	require.Equal(t, "keep-password", merged.ManageStore.Password)
	require.Equal(t, int64(9090), int64(merged.Port))
}
