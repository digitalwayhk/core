package casdoorauthlifecycle_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/api/release"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	authWebhookSecret   = "integration-auth-webhook-secret"
	manageWebhookSecret = "integration-manage-webhook-secret"
)

type lifecycleApp struct {
	t       *testing.T
	name    string
	baseURL string
	wsURL   string
	server  *rest.Server
	context *router.ServiceContext
	casdoor *fakeCasdoor
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type responseEnvelope struct {
	Success      bool            `json:"success"`
	ErrorCode    int             `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	Code         int             `json:"code"`
	Message      string          `json:"message"`
	Data         json.RawMessage `json:"data"`
}

func (r responseEnvelope) publicMessage() string {
	if r.ErrorMessage != "" {
		return r.ErrorMessage
	}
	return r.Message
}

type fakeCasdoor struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	publicPEM  string
}

func newFakeCasdoor(t *testing.T) *fakeCasdoor {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	fake := &fakeCasdoor{
		privateKey: privateKey,
		publicPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
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
	subject := strings.TrimSpace(r.Form.Get("code"))
	clientID := strings.TrimSpace(r.Form.Get("client_id"))
	domain := fakeDomainForClient(clientID)
	if subject == "" || domain.organization == "" {
		http.Error(w, "invalid oauth request", http.StatusBadRequest)
		return
	}
	claims := casdoorsdk.Claims{
		User: casdoorsdk.User{
			Owner: domain.organization, Name: subject, Id: domain.organization + "-" + subject,
			DisplayName: subject, SignupApplication: domain.application,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Second)),
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
	user := casdoorsdk.User{
		Owner: parts[0], Name: parts[1], Id: parts[0] + "-" + parts[1],
		DisplayName: parts[1], SignupApplication: domain.application,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": user})
}

type fakeDomain struct {
	clientID     string
	organization string
	application  string
}

func fakeDomainForClient(clientID string) fakeDomain {
	if clientID == "manage-client" {
		return fakeDomain{clientID: clientID, organization: "manage-org", application: "manage-app"}
	}
	if clientID == "auth-client" {
		return fakeDomain{clientID: clientID, organization: "auth-org", application: "auth-app"}
	}
	return fakeDomain{}
}

func fakeDomainForOrganization(organization string) fakeDomain {
	if organization == "manage-org" {
		return fakeDomainForClient("manage-client")
	}
	if organization == "auth-org" {
		return fakeDomainForClient("auth-client")
	}
	return fakeDomain{}
}

func startLifecycleApp(t *testing.T) *lifecycleApp {
	t.Helper()
	fake := newFakeCasdoor(t)
	port := reservePort(t)
	name := fmt.Sprintf("casdoorlifecycle%d", time.Now().UnixNano())
	root := t.TempDir()
	authYAML := writeCasdoorYAML(t, root, fake, fakeDomainForClient("auth-client"), "auth.yaml")
	manageYAML := writeCasdoorYAML(t, root, fake, fakeDomainForClient("manage-client"), "manage.yaml")
	cfg := config.NewServiceDefaultConfig(name, port)
	cfg.Host = "127.0.0.1"
	cfg.RunIp = "127.0.0.1"
	cfg.Auth.AccessSecret = "integration-auth-access-secret"
	cfg.Auth.RefreshSecret = "integration-auth-refresh-secret"
	cfg.ManageAuth.AccessSecret = "integration-manage-access-secret"
	cfg.ManageAuth.RefreshSecret = "integration-manage-refresh-secret"
	cfg.ServerManageAuth.AccessSecret = "integration-server-manage-secret"
	cfg.Auth.CasDoor = config.CasDoorConfig{Enable: true, YamlFilePath: authYAML, WebhookSecret: authWebhookSecret}
	cfg.ManageAuth.CasDoor = config.CasDoorConfig{Enable: true, YamlFilePath: manageYAML, WebhookSecret: manageWebhookSecret}
	cfg.AuthRevocation.Mode = config.AuthRevocationModeLocal
	cfg.AuthRevocation.BadgerPath = filepath.Join(root, "auth-revocation")
	cfg.ApplyDefaults()
	require.NoError(t, cfg.Validate())

	service := &lifecycleService{name: name}
	sc := router.NewServiceContextWithConfig(service, cfg)
	sc.Router.AddServerRouters(release.Routers()...)
	server, err := rest.NewServer(sc, true, false)
	require.NoError(t, err)
	go server.Start()
	waitForHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	app := &lifecycleApp{
		t: t, name: name, baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		wsURL: fmt.Sprintf("ws://127.0.0.1:%d/ws", port), server: server, context: sc, casdoor: fake,
	}
	t.Cleanup(func() { server.Stop() })
	return app
}

func writeCasdoorYAML(t *testing.T, root string, fake *fakeCasdoor, domain fakeDomain, filename string) string {
	t.Helper()
	data := config.CasDoorConfigData{
		Certificate: fake.publicPEM,
		Server: config.CasDoorServer{
			Endpoint: fake.server.URL, ClientID: domain.clientID, ClientSecret: domain.clientID + "-secret",
			Organization: domain.organization, Application: domain.application, FrontendURL: fake.server.URL,
		},
	}
	encoded, err := yaml.Marshal(data)
	require.NoError(t, err)
	path := filepath.Join(root, filename)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}

func reservePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func waitForHTTP(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("服务未在超时前启动: %s", endpoint)
}

func (a *lifecycleApp) request(t *testing.T, method, path, token string, body interface{}, headers map[string]string) (int, responseEnvelope) {
	t.Helper()
	var payload strings.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		payload = *strings.NewReader(string(data))
	}
	request, err := http.NewRequest(method, a.baseURL+path, &payload)
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var envelope responseEnvelope
	if len(data) > 0 {
		_ = json.Unmarshal(data, &envelope)
	}
	return response.StatusCode, envelope
}

func (a *lifecycleApp) callback(t *testing.T, authType, subject string) tokenPair {
	t.Helper()
	query := url.Values{"type": {authType}, "code": {subject}, "state": {"integration-state"}}
	status, envelope := a.request(t, http.MethodGet, "/api/casdoor/callback?"+query.Encode(), "", nil, nil)
	require.Equal(t, http.StatusOK, status, envelope.publicMessage())
	require.True(t, envelope.Success, envelope.publicMessage())
	var pair tokenPair
	require.NoError(t, json.Unmarshal(envelope.Data, &pair))
	require.NotEmpty(t, pair.AccessToken)
	require.NotEmpty(t, pair.RefreshToken)
	return pair
}

func (a *lifecycleApp) webhook(t *testing.T, authType, action, subject string, blocked bool) responseEnvelope {
	t.Helper()
	domain := fakeDomainForClient("auth-client")
	secret := authWebhookSecret
	if authType == string(types.AuthTypeManage) {
		domain = fakeDomainForClient("manage-client")
		secret = manageWebhookSecret
	}
	now := time.Now().UTC()
	body := map[string]interface{}{
		"name":        fmt.Sprintf("%s-%s-%d", action, subject, now.UnixNano()),
		"createdTime": now.Format(time.RFC3339Nano), "organization": domain.organization,
		"application": domain.application, "user": subject, "action": action,
		"extendedUser": map[string]interface{}{
			"id": domain.organization + "-" + subject, "owner": domain.organization, "name": subject,
			"signupApplication": domain.application, "isForbidden": blocked,
		},
	}
	status, envelope := a.request(t, http.MethodPost, "/api/casdoor/webhook?type="+authType, "", body, map[string]string{"Authorization": "Bearer " + secret})
	require.Equal(t, http.StatusOK, status, envelope.publicMessage())
	require.True(t, envelope.Success, envelope.publicMessage())
	return envelope
}

type lifecycleService struct{ name string }

func (s *lifecycleService) ServiceName() string                  { return s.name }
func (*lifecycleService) SubscribeRouters() []*types.ObserveArgs { return nil }
func (s *lifecycleService) Routers() []types.IRouter {
	return []types.IRouter{&publicProbe{service: s.name}, &privateProbe{service: s.name}, &manageProbe{service: s.name}}
}
func (*lifecycleService) OnAuth(context.Context, *types.AuthHookArgs) error        { return nil }
func (*lifecycleService) OnCasdoorEvent(context.Context, types.CasdoorEvent) error { return nil }
func (*lifecycleService) OnAuthRequest(_ context.Context, args types.AuthRequestArgs) error {
	switch args.Identity.ProviderSubject {
	case "typed":
		return types.NewPublicError(types.ErrorKindForbidden, types.PublicCodeForbidden, "账户已冻结", errors.New("typed rejection"))
	case "internal":
		return errors.New("internal authorization detail")
	default:
		return nil
	}
}

type publicProbe struct{ service string }

func (*publicProbe) Parse(types.IRequest) error      { return nil }
func (*publicProbe) Validation(types.IRequest) error { return nil }
func (*publicProbe) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"scope": "public"}, nil
}
func (p *publicProbe) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(p, p.service+"/api/public", "PublicProbe",
		router.WithPath("/api/"+p.service+"/public"), router.WithPathType(types.PublicType), router.WithMethod(http.MethodGet))
}

type privateProbe struct {
	service string
	userID  string
}

func (*privateProbe) Parse(types.IRequest) error      { return nil }
func (*privateProbe) Validation(types.IRequest) error { return nil }
func (p *privateProbe) Do(req types.IRequest) (interface{}, error) {
	uid, name := req.GetUser()
	return map[string]string{"scope": "private", "uid": uid, "name": name}, nil
}
func (p *privateProbe) SetUserID(uid, _ string) { p.userID = uid }
func (p *privateProbe) GetUserID() string       { return p.userID }
func (p *privateProbe) GetHashKey() uint64      { return utils.HashCode64(p.userID) }
func (p *privateProbe) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(p, p.service+"/api/private", "PrivateProbe",
		router.WithPath("/api/"+p.service+"/private"), router.WithPathType(types.PrivateType), router.WithAuth(true), router.WithMethod(http.MethodGet))
}

type manageProbe struct{ service string }

func (*manageProbe) Parse(types.IRequest) error      { return nil }
func (*manageProbe) Validation(types.IRequest) error { return nil }
func (*manageProbe) Do(req types.IRequest) (interface{}, error) {
	uid, name := req.GetUser()
	return map[string]string{"scope": "manage", "uid": uid, "name": name}, nil
}
func (p *manageProbe) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(p, p.service+"/api/manage", "ManageProbe",
		router.WithPath("/api/"+p.service+"/manage"), router.WithPathType(types.ManageType), router.WithAuth(true), router.WithMethod(http.MethodGet))
}

func connectWebSocket(t *testing.T, app *lifecycleApp, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(app.wsURL, nil)
	require.NoError(t, err)
	require.NoError(t, connection.WriteJSON(map[string]interface{}{"event": "sub", "channel": "logon", "data": map[string]string{"token": token}}))
	message := readWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "success", message.Event, string(message.Data))
	return connection
}

type webSocketMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

func readWebSocket(t *testing.T, connection *websocket.Conn, timeout time.Duration) webSocketMessage {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(timeout)))
	_, data, err := connection.ReadMessage()
	require.NoError(t, err)
	var message webSocketMessage
	require.NoError(t, json.Unmarshal(data, &message))
	return message
}
