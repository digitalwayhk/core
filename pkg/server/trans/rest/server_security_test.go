package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestRestRunOptionsDisabledCors(t *testing.T) {
	opts, err := restRunOptions(false, nil)

	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestRestRunOptionsRejectsMissingOrigins(t *testing.T) {
	for _, origins := range [][]string{nil, {}, {"", "  "}} {
		_, err := restRunOptions(true, origins)
		require.ErrorContains(t, err, "CORS origin")
	}
}

func TestNormalizeCorsOriginsPreservesExplicitOrigins(t *testing.T) {
	origins := normalizeCorsOrigins([]string{" https://admin.example.com ", "", "*"})

	require.Equal(t, []string{"https://admin.example.com", "*"}, origins)
}

func TestNewLogtoHandlerRejectsInvalidConfig(t *testing.T) {
	_, err := newLogtoHandler(func(http.ResponseWriter, *http.Request) {}, config.LogtoConfig{})

	require.ErrorContains(t, err, "issuer")
}

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", recorder.Header().Get("X-Frame-Options"))
}

func TestSecurityHeadersPreserveExistingValues(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Referrer-Policy", "same-origin")

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, "same-origin", recorder.Header().Get("Referrer-Policy"))
}

func TestRouteHandlerRejectsNilAuthenticatedRequest(t *testing.T) {
	api := &nilRequestTestRouter{info: &types.RouterInfo{
		Path:        "/private",
		Method:      http.MethodGet,
		Auth:        true,
		PathType:    types.PrivateType,
		ServiceName: "nil-request-test",
	}}
	service := &nilRequestTestService{name: "nil-request-test", api: api}
	context := &router.ServiceContext{
		Config:  config.NewServiceDefaultConfig(service.name, 18082),
		Service: &types.Service{Name: service.name, Routers: []types.IRouter{api}},
	}
	context.Router = router.NewServiceRouter(context, service)
	handler := RouteHandler(context.Router)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	recorder := httptest.NewRecorder()

	require.NotPanics(t, func() { handler.ServeHTTP(recorder, req) })
	require.Equal(t, StatusUnauthorized, recorder.Code)
}

type nilRequestTestService struct {
	name string
	api  types.IRouter
}

func (s *nilRequestTestService) ServiceName() string                    { return s.name }
func (s *nilRequestTestService) Routers() []types.IRouter               { return []types.IRouter{s.api} }
func (s *nilRequestTestService) SubscribeRouters() []*types.ObserveArgs { return nil }

type nilRequestTestRouter struct {
	info *types.RouterInfo
}

func (r *nilRequestTestRouter) Parse(types.IRequest) error             { return nil }
func (r *nilRequestTestRouter) Validation(types.IRequest) error        { return nil }
func (r *nilRequestTestRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (r *nilRequestTestRouter) RouterInfo() *types.RouterInfo          { return r.info }
