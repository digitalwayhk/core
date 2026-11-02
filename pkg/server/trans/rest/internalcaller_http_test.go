package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var deniedHTTPParseCount atomic.Int32

type constrainedHTTPRoute struct {
	Value string `json:"value"`
}

func (r *constrainedHTTPRoute) Parse(req types.IRequest) error {
	deniedHTTPParseCount.Add(1)
	return req.Bind(r)
}
func (*constrainedHTTPRoute) Validation(types.IRequest) error { return nil }
func (*constrainedHTTPRoute) Do(types.IRequest) (interface{}, error) {
	return map[string]bool{"ok": true}, nil
}
func (r *constrainedHTTPRoute) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(r, "fixture/api/public", "internal", router.WithServiceName("forbidden-http"), router.WithPath("/api/forbidden-http/internal"), router.WithInternalCallers("trusted-source"))
}

type constrainedHTTPService struct{ name string }

func (s *constrainedHTTPService) ServiceName() string { return s.name }
func (*constrainedHTTPService) Routers() []types.IRouter {
	return []types.IRouter{&constrainedHTTPRoute{}}
}
func TestConstrainedRouteRealHTTPRejectsBeforeMalformedJSONParse(t *testing.T) {
	deniedHTTPParseCount.Store(0)
	name := fmt.Sprintf("forbidden-http-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	sc := router.NewServiceContextWithConfig(&constrainedHTTPService{name}, cfg)
	defer sc.SetRunState(false)
	// 固定路径由RouterInfo显式声明，服务身份仍由真实ServiceContext冻结。
	server := httptest.NewServer(RouteHandler(sc.Router))
	defer server.Close()
	path := "/api/" + name + "/internal"
	for _, body := range []string{`{"value":"x"}`, `{broken`} {
		request, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("SourceService", "trusted-source")
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		var envelope router.Response
		require.NoError(t, json.NewDecoder(response.Body).Decode(&envelope))
		response.Body.Close()
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		require.Equal(t, types.PublicCodeForbidden, envelope.ErrorCode)
		require.False(t, envelope.Success)
	}
	require.Zero(t, deniedHTTPParseCount.Load(), "白名单应早于解析损坏JSON")
	raw := json.RawMessage(`{"value":"trusted"}`)
	payload := &types.PayLoad{SourceService: "trusted-source", TargetService: name, TargetPath: path, Instance: raw, Data: raw, HttpMethod: http.MethodPost}
	data, err := sc.HandleInternalPayload(types.ContextWithTrustedInternalCaller(context.Background(), "trusted-source"), payload)
	require.NoError(t, err)
	var response router.Response
	require.NoError(t, json.Unmarshal(data, &response))
	require.True(t, response.Success)
}
