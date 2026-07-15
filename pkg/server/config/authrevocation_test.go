package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthRevocationDefaultsToLocalBadger(t *testing.T) {
	cfg := NewServiceDefaultConfig("auth-test", 18080)

	require.Equal(t, "local", cfg.AuthRevocation.Mode)
	require.Equal(t, filepath.Join("data", "auth-test", "auth-revocation"), cfg.AuthRevocation.BadgerPath)
	require.Equal(t, "core:authrevocation", cfg.AuthRevocation.Redis.Prefix)
}

func TestCasdoorLoadedConfigRejectsClientSecretReuse(t *testing.T) {
	file := writeCasdoorConfigForTest(t, "http://127.0.0.1:18000", "client-secret", "org", "app")
	cfg := CasDoorConfig{Enable: true, YamlFilePath: file, WebhookSecret: "client-secret"}

	require.ErrorContains(t, cfg.ReloadConfig(), "ClientSecret")
}

func TestCasdoorLoadedConfigRejectsInsecureRemoteEndpoint(t *testing.T) {
	file := writeCasdoorConfigForTest(t, "http://casdoor.example.com", "client-secret", "org", "app")
	cfg := CasDoorConfig{Enable: true, YamlFilePath: file, WebhookSecret: "webhook-secret"}

	require.ErrorContains(t, cfg.ReloadConfig(), "HTTPS")
}

func TestCasdoorLoadedConfigAllowsLoopbackHTTP(t *testing.T) {
	file := writeCasdoorConfigForTest(t, "http://127.0.0.1:18000", "client-secret", "org", "app")
	cfg := CasDoorConfig{Enable: true, YamlFilePath: file, WebhookSecret: "webhook-secret"}

	require.NoError(t, cfg.ReloadConfig())
}

func writeCasdoorConfigForTest(t *testing.T, endpoint, clientSecret, organization, application string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "casdoor.yaml")
	content := "certificate: test-certificate\nserver:\n" +
		"  endpoint: " + endpoint + "\n" +
		"  client_id: client-id\n" +
		"  client_secret: " + clientSecret + "\n" +
		"  organization: " + organization + "\n" +
		"  application: " + application + "\n" +
		"  frontend_url: http://localhost:3000\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func TestAuthRevocationSharedRequiresRedis(t *testing.T) {
	cfg := NewServiceDefaultConfig("auth-test", 18080)
	cfg.Auth.CasDoor.Enable = true
	cfg.AuthRevocation.Mode = "shared"
	cfg.AuthRevocation.Redis.Addr = ""

	require.ErrorContains(t, cfg.Validate(), "authRevocation.redis.addr")
}

func TestAuthRevocationRejectsUnknownMode(t *testing.T) {
	cfg := NewServiceDefaultConfig("auth-test", 18080)
	cfg.AuthRevocation.Mode = "fallback"

	require.ErrorContains(t, cfg.Validate(), "authRevocation.mode")
}

func TestCasdoorWebhookSecretsAreIndependent(t *testing.T) {
	cfg := NewServiceDefaultConfig("auth-test", 18080)
	cfg.Auth.CasDoor.Enable = true
	cfg.ManageAuth.CasDoor.Enable = true
	cfg.Auth.CasDoor.WebhookSecret = "auth-webhook"
	cfg.ManageAuth.CasDoor.WebhookSecret = "auth-webhook"

	require.ErrorContains(t, cfg.Validate(), "WebhookSecret")

	cfg.ManageAuth.CasDoor.WebhookSecret = "manage-webhook"
	cfg.Auth.AccessSecret = cfg.Auth.CasDoor.WebhookSecret
	require.ErrorContains(t, cfg.Validate(), "WebhookSecret")
}

func TestServerManageCasdoorWebhookIsRejected(t *testing.T) {
	cfg := NewServiceDefaultConfig("auth-test", 18080)
	cfg.ServerManageAuth.CasDoor.WebhookSecret = "unsupported-webhook"

	require.ErrorContains(t, cfg.Validate(), "ServerManageAuth.CasDoor.WebhookSecret")
}
