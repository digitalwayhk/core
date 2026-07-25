package openapidoc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceServerURLUsesServicePortWithIPv6(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	req.Host = "[::1]:48080"

	require.Equal(t, "http://[::1]:21001/", serviceServerURL(req, 21001))
}

func TestSameOriginServerURLPreservesDevelopmentViewAuthority(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	req.Host = "[::1]:48080"
	req.Header.Set("X-Forwarded-Proto", "https")

	require.Equal(t, "https://[::1]:48080/", sameOriginServerURL(req))
}

func TestOpenAPIServerURLRejectsUnsafeHost(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		scheme string
		want   string
	}{
		{name: "missing", host: "", want: "http://127.0.0.1:21001/"},
		{name: "service replaces request port", host: "example:abc", want: "http://example:21001/"},
		{name: "header injection", host: "example.com/path", want: "http://127.0.0.1:21001/"},
		{name: "https", host: "example:abc", scheme: "https", want: "https://example/"},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
			req.Host = item.host
			req.Header.Set("X-Forwarded-Proto", item.scheme)
			require.Equal(t, item.want, serviceServerURL(req, 21001))
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/openapi", nil)
	req.Host = "example:abc"
	require.Equal(t, "http://127.0.0.1/", sameOriginServerURL(req),
		"开发同源模式必须拒绝非法请求端口")
}
