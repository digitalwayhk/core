package casdoorrbacshop_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	authClientID        = "shop-auth-client"
	manageClientID      = "shop-manage-client"
	authAccessSecret    = "shop-integration-auth-access-secret"
	authRefreshSecret   = "shop-integration-auth-refresh-secret"
	manageAccessSecret  = "shop-integration-manage-access-secret"
	manageRefreshSecret = "shop-integration-manage-refresh-secret"
	authWebhookSecret   = "shop-integration-auth-webhook-secret"
	manageWebhookSecret = "shop-integration-manage-webhook-secret"
)

type casdoorConfigurationResponse struct {
	Endpoint              string `json:"Endpoint"`
	ClientID              string `json:"ClientID"`
	Organization          string `json:"Organization"`
	Application           string `json:"Application"`
	BackgroundCallbackURL string `json:"BackgroundCallbackURL"`
}

type fakeCasdoor struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	publicPEM  string
}

type webhookFixture struct {
	authType types.AuthType
	secret   string
	body     map[string]interface{}
}

func newFakeCasdoor() (*fakeCasdoor, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("生成 Fake Casdoor 密钥: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("编码 Fake Casdoor 公钥: %w", err)
	}
	fake := &fakeCasdoor{
		privateKey: privateKey,
		publicPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake, nil
}

func (f *fakeCasdoor) Close() {
	if f != nil && f.server != nil {
		f.server.Close()
	}
}

func (f *fakeCasdoor) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/login/oauth/access_token":
		f.handleToken(w, r)
	case "/api/get-user":
		f.handleUser(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeCasdoor) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	domain := fakeDomainForClient(strings.TrimSpace(r.Form.Get("client_id")))
	subject := strings.TrimSpace(r.Form.Get("code"))
	if subject == "" || domain.clientID == "" || r.Form.Get("client_secret") != domain.clientSecret {
		http.Error(w, "invalid oauth request", http.StatusUnauthorized)
		return
	}
	claims := casdoorsdk.Claims{
		User: casdoorsdk.User{
			Owner: domain.organization, Name: subject, Id: domain.organization + "-" + subject,
			DisplayName: subject, SignupApplication: domain.application,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: subject, IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(f.privateKey)
	if err != nil {
		http.Error(w, "sign token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": signed, "token_type": "Bearer", "expires_in": 300,
	})
}

func (f *fakeCasdoor) handleUser(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimSpace(r.URL.Query().Get("id")), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "invalid user", http.StatusBadRequest)
		return
	}
	domain := fakeDomainForOrganization(parts[0])
	if domain.organization == "" {
		http.Error(w, "unknown organization", http.StatusNotFound)
		return
	}
	user := casdoorsdk.User{
		Owner: parts[0], Name: parts[1], Id: parts[0] + "-" + parts[1],
		DisplayName: parts[1], SignupApplication: domain.application,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": user})
}

type fakeDomain struct {
	clientID     string
	clientSecret string
	organization string
	application  string
}

func fakeDomainForClient(clientID string) fakeDomain {
	switch clientID {
	case authClientID:
		return fakeDomain{clientID: clientID, clientSecret: "shop-auth-client-secret", organization: "shop-auth-org", application: "shop-auth-app"}
	case manageClientID:
		return fakeDomain{clientID: clientID, clientSecret: "shop-manage-client-secret", organization: "shop-manage-org", application: "shop-manage-app"}
	default:
		return fakeDomain{}
	}
}

func fakeDomainForOrganization(organization string) fakeDomain {
	switch organization {
	case "shop-auth-org":
		return fakeDomainForClient(authClientID)
	case "shop-manage-org":
		return fakeDomainForClient(manageClientID)
	default:
		return fakeDomain{}
	}
}

func newWebhookFixture(authType types.AuthType, action, subject string, blocked bool) webhookFixture {
	domain := fakeDomainForClient(authClientID)
	secret := authWebhookSecret
	if authType == types.AuthTypeManage {
		domain = fakeDomainForClient(manageClientID)
		secret = manageWebhookSecret
	}
	now := time.Now().UTC()
	return webhookFixture{
		authType: authType,
		secret:   secret,
		body: map[string]interface{}{
			"name":        fmt.Sprintf("%s-%s-%d", action, subject, now.UnixNano()),
			"createdTime": now.Format(time.RFC3339Nano), "organization": domain.organization,
			"application": domain.application, "user": subject, "action": action,
			"extendedUser": map[string]interface{}{
				"id": domain.organization + "-" + subject, "owner": domain.organization,
				"name": subject, "signupApplication": domain.application, "isForbidden": blocked,
			},
		},
	}
}

// SendWebhook 使用认证域独立 Secret 发送可重放的标准 Casdoor 事件。
func (s *shopSuite) SendWebhook(t testing.TB, fixture webhookFixture) integration.ResponseEnvelope {
	t.Helper()
	data, err := json.Marshal(fixture.body)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost,
		s.BaseURL+"/api/casdoor/webhook?type="+string(fixture.authType), bytes.NewReader(data))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+fixture.secret)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	envelope := integration.ResponseEnvelope{HTTPStatus: response.StatusCode, Body: string(body)}
	require.NoError(t, json.Unmarshal(body, &envelope), string(body))
	require.Equal(t, http.StatusOK, response.StatusCode, envelope.ErrorMessage)
	return envelope
}

func configureCasdoorSuite(root string, fake *fakeCasdoor) error {
	if fake == nil || fake.server == nil {
		return fmt.Errorf("Fake Casdoor 未启动")
	}
	authYAML, err := writeCasdoorYAML(root, fake, fakeDomainForClient(authClientID), "auth-casdoor.yaml")
	if err != nil {
		return err
	}
	manageYAML, err := writeCasdoorYAML(root, fake, fakeDomainForClient(manageClientID), "manage-casdoor.yaml")
	if err != nil {
		return err
	}
	configPath := filepath.Join(root, "etc", "casdoorrbacshop.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取商城配置: %w", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("解析商城配置: %w", err)
	}
	auth := configObject(cfg, "Auth")
	auth["AccessSecret"] = authAccessSecret
	auth["AccessExpire"] = 3600
	auth["RefreshSecret"] = authRefreshSecret
	auth["RefreshExpire"] = 7200
	auth["CasDoor"] = map[string]interface{}{
		"Enable": true, "YamlFilePath": authYAML, "WebhookSecret": authWebhookSecret,
	}
	manageAuth := configObject(cfg, "ManageAuth")
	manageAuth["AccessSecret"] = manageAccessSecret
	manageAuth["AccessExpire"] = 3600
	manageAuth["RefreshSecret"] = manageRefreshSecret
	manageAuth["RefreshExpire"] = 7200
	manageAuth["CasDoor"] = map[string]interface{}{
		"Enable": true, "YamlFilePath": manageYAML, "WebhookSecret": manageWebhookSecret,
	}
	authRevocation := configObject(cfg, "AuthRevocation")
	authRevocation["Mode"] = config.AuthRevocationModeLocal
	authRevocation["BadgerPath"] = filepath.Join(root, "auth-revocation")
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("编码商城 Casdoor 配置: %w", err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return fmt.Errorf("写入商城 Casdoor 配置: %w", err)
	}
	return nil
}

func configObject(parent map[string]interface{}, key string) map[string]interface{} {
	value, _ := parent[key].(map[string]interface{})
	if value == nil {
		value = make(map[string]interface{})
		parent[key] = value
	}
	return value
}

func writeCasdoorYAML(root string, fake *fakeCasdoor, domain fakeDomain, filename string) (string, error) {
	data := config.CasDoorConfigData{
		Certificate: fake.publicPEM,
		Server: config.CasDoorServer{
			Endpoint: fake.server.URL, ClientID: domain.clientID, ClientSecret: domain.clientSecret,
			Organization: domain.organization, Application: domain.application, FrontendURL: fake.server.URL,
		},
	}
	encoded, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("编码 Casdoor YAML: %w", err)
	}
	path := filepath.Join(root, filename)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return "", fmt.Errorf("写入 Casdoor YAML: %w", err)
	}
	return path, nil
}
