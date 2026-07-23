package casdoor

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

func TestClientSetKeepsAuthAndManageIsolated(t *testing.T) {
	auth := casdoorConfigForTest(t, "http://127.0.0.1:18001", "auth-client", "auth-org", "auth-app")
	manage := casdoorConfigForTest(t, "http://127.0.0.1:18002", "manage-client", "manage-org", "manage-app")

	clients, err := NewClientSet(auth, manage)

	require.NoError(t, err)
	require.NotNil(t, clients.Auth())
	require.NotNil(t, clients.Manage())
	require.NotSame(t, clients.Auth().client, clients.Manage().client)
	require.Equal(t, "auth-org", clients.Auth().Organization())
	require.Equal(t, "auth-app", clients.Auth().Application())
	require.Equal(t, "manage-org", clients.Manage().Organization())
	require.Equal(t, "manage-app", clients.Manage().Application())
}

func TestClientSetLeavesDisabledDomainNil(t *testing.T) {
	auth := casdoorConfigForTest(t, "http://127.0.0.1:18001", "auth-client", "auth-org", "auth-app")

	clients, err := NewClientSet(auth, config.CasDoorConfig{})

	require.NoError(t, err)
	require.NotNil(t, clients.Auth())
	require.Nil(t, clients.Manage())
}

func TestClientSetRejectsInvalidCertificate(t *testing.T) {
	auth := casdoorConfigForTest(t, "http://127.0.0.1:18001", "auth-client", "auth-org", "auth-app")
	require.NoError(t, os.WriteFile(auth.YamlFilePath, []byte("certificate: invalid\nserver:\n  endpoint: http://127.0.0.1:18001\n  client_id: auth-client\n  client_secret: client-secret\n  organization: auth-org\n  application: auth-app\n"), 0o600))

	_, err := NewClientSet(auth, config.CasDoorConfig{})

	require.ErrorContains(t, err, "Certificate")
}

func TestClientSetRejectsWebhookSecretReusedByOtherDomainClient(t *testing.T) {
	auth := casdoorConfigForTest(t, "http://127.0.0.1:18001", "auth-client", "auth-org", "auth-app")
	manage := casdoorConfigForTest(t, "http://127.0.0.1:18002", "manage-client", "manage-org", "manage-app")
	auth.WebhookSecret = "manage-client-secret"

	_, err := NewClientSet(auth, manage)

	require.ErrorContains(t, err, "WebhookSecret")
}

func TestVerifyActiveUserRejectsUnsafeCasdoorState(t *testing.T) {
	tests := []struct {
		name string
		user *casdoorsdk.User
	}{
		{name: "missing", user: nil},
		{name: "forbidden", user: &casdoorsdk.User{Owner: "org", Name: "alice", IsForbidden: true}},
		{name: "deleted", user: &casdoorsdk.User{Owner: "org", Name: "alice", IsDeleted: true}},
		{name: "organization mismatch", user: &casdoorsdk.User{Owner: "other", Name: "alice"}},
		{name: "subject mismatch", user: &casdoorsdk.User{Owner: "org", Name: "bob"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, VerifyActiveUser(tt.user, "org", "alice"), ErrIdentityInactive)
		})
	}
}

func TestVerifyActiveUserAcceptsMatchingUser(t *testing.T) {
	user := &casdoorsdk.User{Owner: "org", Name: "alice"}
	require.NoError(t, VerifyActiveUser(user, "org", "alice"))
}

func casdoorConfigForTest(t *testing.T, endpoint, clientID, organization, application string) config.CasDoorConfig {
	t.Helper()
	file := filepath.Join(t.TempDir(), "casdoor.yaml")
	content := "certificate: |\n" + indentYAML(t, casdoorPublicKey(t)) + "server:\n" +
		"  endpoint: " + endpoint + "\n" +
		"  client_id: " + clientID + "\n" +
		"  client_secret: " + clientID + "-secret\n" +
		"  organization: " + organization + "\n" +
		"  application: " + application + "\n" +
		"  frontend_url: http://localhost:3000\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return config.CasDoorConfig{Enable: true, YamlFilePath: file, WebhookSecret: clientID + "-webhook"}
}

func casdoorPublicKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
}

func indentYAML(t *testing.T, value string) string {
	t.Helper()
	result := ""
	for _, line := range strings.Split(value, "\n") {
		if line != "" {
			result += "  " + line + "\n"
		}
	}
	return result
}
