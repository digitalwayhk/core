package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest"
)

func TestAdminViewRedactsProtectedValues(t *testing.T) {
	cfg := NewServiceDefaultConfig("admin-view", 8080)
	cfg.Auth.AccessSecret = "auth-access"
	cfg.Auth.RefreshSecret = "auth-refresh"
	cfg.ManageAuth.AccessSecret = "manage-access"
	cfg.Auth.CasDoor.WebhookSecret = "webhook"
	cfg.Signature.PrivateKeys = []rest.PrivateKeyConf{{KeyFile: "private-key-material"}}

	view, err := AdminView(cfg)
	require.NoError(t, err)
	require.Equal(t, redactedConfigValue, view["Auth"].(map[string]interface{})["AccessSecret"])
	require.Equal(t, redactedConfigValue, view["Auth"].(map[string]interface{})["RefreshSecret"])
	require.Equal(t, redactedConfigValue, view["ManageAuth"].(map[string]interface{})["AccessSecret"])
	require.Equal(t, redactedConfigValue, view["Auth"].(map[string]interface{})["CasDoor"].(map[string]interface{})["WebhookSecret"])
	require.Empty(t, view["Signature"].(map[string]interface{})["PrivateKeys"])
}

func TestMergeProtectedFieldsKeepsRuntimeCredentials(t *testing.T) {
	existing := NewServiceDefaultConfig("merge", 8080)
	existing.Auth.AccessSecret = "keep-access"
	existing.Auth.RefreshSecret = "keep-refresh"
	incoming := NewServiceDefaultConfig("merge", 9090)
	incoming.Auth.AccessSecret = redactedConfigValue
	incoming.Auth.RefreshSecret = redactedConfigValue

	merged, err := MergeProtectedFields(existing, incoming)
	require.NoError(t, err)
	require.Equal(t, "keep-access", merged.Auth.AccessSecret)
	require.Equal(t, "keep-refresh", merged.Auth.RefreshSecret)
	require.Equal(t, int64(9090), int64(merged.Port))
}
