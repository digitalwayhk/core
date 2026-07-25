package run

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func bootstrapAuthority(t *testing.T, casdoor bool) *manageAuthAuthority {
	t.Helper()
	ctx := manageAuthorityContext(t, "Authority", true)
	ctx.Config.ManageAuth.CasDoor.Enable = casdoor
	ctx.Config.ManageAuth.AccessSecret = "access-secret-never-expose"
	ctx.Config.ManageAuth.RefreshSecret = "refresh-secret-never-expose"
	ctx.Config.ManageAuth.CasDoor.WebhookSecret = "webhook-secret-never-expose"
	return &manageAuthAuthority{name: " Authority ", context: ctx, router: ctx.Router}
}

func bootstrapRequest(method, remoteAddr string) *http.Request {
	request := httptest.NewRequest(method, webBootstrapPath, nil)
	request.RemoteAddr = remoteAddr
	return request
}

func TestWebBootstrapModeMatrix(t *testing.T) {
	tests := []struct {
		name       string
		casdoor    bool
		remoteAddr string
		mode       string
	}{
		{"casdoor local", true, "127.0.0.1:9", webAuthModeCasdoor},
		{"casdoor remote", true, "203.0.113.10:9", webAuthModeCasdoor},
		{"test token local", false, "127.0.0.1:9", webAuthModeTestToken},
		{"test token remote", false, "203.0.113.10:9", webAuthModeUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := buildWebBootstrap(
				bootstrapAuthority(t, tc.casdoor),
				bootstrapRequest(http.MethodGet, tc.remoteAddr),
			)
			require.Equal(t, tc.mode, response.Auth.Mode)
			require.Equal(t, webAuthTypeManage, response.Auth.Type)
			require.Equal(t, "authority", response.Auth.AuthorityService)
			require.Equal(t, webBootstrapCallback, response.Endpoints.Callback)
			require.Equal(t, webBootstrapRefresh, response.Endpoints.Refresh)
			require.Equal(t, webBootstrapOpenAPI, response.Endpoints.OpenAPI)
			if tc.mode == webAuthModeTestToken {
				require.Equal(t, webBootstrapAcquireToken, *response.Endpoints.AcquireToken)
			} else {
				require.Nil(t, response.Endpoints.AcquireToken)
			}
			if tc.mode == webAuthModeCasdoor {
				require.Equal(t, webBootstrapCasdoorConfig, *response.Endpoints.CasdoorConfig)
			} else {
				require.Nil(t, response.Endpoints.CasdoorConfig)
			}
		})
	}
}

func TestWebBootstrapUnavailableWithoutAuthority(t *testing.T) {
	response := buildWebBootstrap(nil, bootstrapRequest(http.MethodGet, "127.0.0.1:9"))
	require.Equal(t, webAuthModeUnavailable, response.Auth.Mode)
	require.Empty(t, response.Auth.AuthorityService)
	require.Nil(t, response.Endpoints.AcquireToken)
	require.Nil(t, response.Endpoints.CasdoorConfig)
}

func TestWebBootstrapHandlerIsAnonymousNoStoreAndSecretFree(t *testing.T) {
	authority := bootstrapAuthority(t, true)
	recorder := httptest.NewRecorder()
	newWebBootstrapHandler(authority).ServeHTTP(
		recorder, bootstrapRequest(http.MethodGet, "203.0.113.10:9"),
	)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))

	var response WebBootstrap
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, webAuthModeCasdoor, response.Auth.Mode)
	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{
		"access-secret-never-expose",
		"refresh-secret-never-expose",
		"webhook-secret-never-expose",
		"client_secret",
		"password",
	} {
		require.NotContains(t, body, forbidden)
	}
}

func TestWebBootstrapRejectsNonGETWithNoStore(t *testing.T) {
	recorder := httptest.NewRecorder()
	newWebBootstrapHandler(nil).ServeHTTP(
		recorder, bootstrapRequest(http.MethodPost, "127.0.0.1:9"),
	)
	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	require.Equal(t, http.MethodGet, recorder.Header().Get("Allow"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}
