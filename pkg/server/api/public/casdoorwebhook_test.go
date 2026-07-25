package public

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestCasdoorWebhookRejectsWrongSecretBeforeParsingBody(t *testing.T) {
	cfg := webhookTestConfig(t)
	request := webhookTestRequest("auth", "wrong-secret", strings.Repeat("{", 100))

	_, _, err := parseCasdoorWebhookRequest(request, "shop", cfg)

	contract := types.ResolvePublicError(err)
	require.Equal(t, http.StatusUnauthorized, contract.HTTPStatus)
	require.Equal(t, "authentication failed", contract.Message)
}

func TestCasdoorWebhookRejectsBoundaryErrors(t *testing.T) {
	cfg := webhookTestConfig(t)
	tests := []struct {
		name    string
		request *http.Request
	}{
		{name: "oversized", request: webhookTestRequest("auth", "auth-webhook", strings.Repeat("x", maxCasdoorWebhookBody+1))},
		{name: "content type", request: webhookTestRequest("auth", "auth-webhook", `{}`)},
		{name: "unknown domain before JSON", request: webhookTestRequest("servermanage", "auth-webhook", `{`)},
	}
	tests[1].request.Header.Set("Content-Type", "text/plain")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseCasdoorWebhookRequest(tt.request, "shop", cfg)
			require.Equal(t, http.StatusBadRequest, types.ResolvePublicError(err).HTTPStatus)
		})
	}
}

func TestCasdoorWebhookNormalizesAllowedFieldsAndBindsDomain(t *testing.T) {
	cfg := webhookTestConfig(t)
	body := `{
		"id": 17,
		"name": "record-17",
		"createdTime": "2030-03-17T12:30:45.123Z",
		"organization": "auth-org",
		"user": "alice",
		"action": "LOGOUT",
		"object": "must-not-be-persisted",
		"extendedUser": {
			"id": "uid-alice",
			"owner": "auth-org",
			"name": "alice",
			"signupApplication": "auth-app",
			"isForbidden": true,
			"password": "must-not-be-parsed"
		}
	}`

	value, retention, err := parseCasdoorWebhookRequest(webhookTestRequest("auth", "auth-webhook", body), "shop", cfg)

	require.NoError(t, err)
	require.Equal(t, "shop", value.ServiceName)
	require.Equal(t, types.AuthTypeUser, value.AuthType)
	require.Equal(t, types.AuthProviderCasdoor, value.Provider)
	require.Equal(t, "alice", value.ProviderSubject)
	require.Equal(t, "uid-alice", value.UID)
	require.Equal(t, "logout", value.EventType)
	require.True(t, value.Blocked)
	require.Equal(t, time.Date(2030, 3, 17, 12, 30, 45, 123_000_000, time.UTC), value.OccurredAt)
	require.Len(t, value.ID, 64)
	require.Equal(t, 48*time.Hour, retention)
	encoded := fmt.Sprintf("%+v", value)
	require.NotContains(t, encoded, "must-not-be")
}

func TestCasdoorWebhookRejectsOrganizationApplicationAndSubjectMismatch(t *testing.T) {
	cfg := webhookTestConfig(t)
	base := `{"name":"event","createdTime":"2030-03-17T12:30:45Z","organization":"%s","user":"%s","action":"logout","extendedUser":{"id":"uid","owner":"%s","name":"alice","signupApplication":"%s"}}`
	tests := []string{
		fmt.Sprintf(base, "wrong-org", "alice", "auth-org", "auth-app"),
		fmt.Sprintf(base, "auth-org", "alice", "auth-org", "wrong-app"),
		fmt.Sprintf(base, "auth-org", "bob", "auth-org", "auth-app"),
	}
	for _, body := range tests {
		_, _, err := parseCasdoorWebhookRequest(webhookTestRequest("auth", "auth-webhook", body), "shop", cfg)
		require.Equal(t, http.StatusBadRequest, types.ResolvePublicError(err).HTTPStatus)
	}
}

func TestCasdoorWebhookKeepsAuthAndManageSecretsIsolated(t *testing.T) {
	cfg := webhookTestConfig(t)
	body := `{"name":"manage-event","createdTime":"2030-03-17T12:30:45Z","organization":"manage-org","user":"operator","action":"update-user","extendedUser":{"id":"manage-uid","owner":"manage-org","name":"operator","signupApplication":"manage-app"}}`

	_, _, err := parseCasdoorWebhookRequest(webhookTestRequest("manage", "auth-webhook", body), "shop", cfg)
	require.Equal(t, http.StatusUnauthorized, types.ResolvePublicError(err).HTTPStatus)

	value, _, err := parseCasdoorWebhookRequest(webhookTestRequest("manage", "manage-webhook", body), "shop", cfg)
	require.NoError(t, err)
	require.Equal(t, types.AuthTypeManage, value.AuthType)
	require.Equal(t, "operator", value.ProviderSubject)
}

func TestCasdoorWebhookUsesAuthRateLimit(t *testing.T) {
	info := (&CasdoorWebhook{}).RouterInfo()
	require.Equal(t, casdoorWebhookPath, info.GetPath())
	policy := info.GetExternalRateLimit()
	require.NotNil(t, policy)
	require.Equal(t, float64(5), policy.Rate)
	require.Equal(t, 10, policy.Burst)
}

func webhookTestRequest(authType, secret, body string) *http.Request {
	request, _ := http.NewRequest(http.MethodPost, casdoorWebhookPath+"?type="+authType, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Authorization", "Bearer "+secret)
	return request
}

func webhookTestConfig(t *testing.T) *config.ServerConfig {
	t.Helper()
	cfg := config.NewServiceDefaultConfig("shop", 0)
	cfg.Auth.RefreshExpire = int64((48 * time.Hour) / time.Second)
	cfg.Auth.CasDoor.Enable = true
	cfg.Auth.CasDoor.WebhookSecret = "auth-webhook"
	cfg.Auth.CasDoor.YamlFilePath = writeWebhookCasdoorConfig(t, "auth-org", "auth-app")
	cfg.ManageAuth.CasDoor.Enable = true
	cfg.ManageAuth.CasDoor.WebhookSecret = "manage-webhook"
	cfg.ManageAuth.CasDoor.YamlFilePath = writeWebhookCasdoorConfig(t, "manage-org", "manage-app")
	return cfg
}

func writeWebhookCasdoorConfig(t *testing.T, organization, application string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "casdoor.yaml")
	content := "certificate: test\nserver:\n" +
		"  endpoint: http://127.0.0.1:18000\n" +
		"  client_id: client\n" +
		"  client_secret: client-secret\n" +
		"  organization: " + organization + "\n" +
		"  application: " + application + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
